package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

const proxyProviderEnv = "TOKLESS_PROXY_PROVIDER"

// ProviderModel is the model metadata a provider-block spec carries.
type ProviderModel struct {
	ID        string
	Display   string
	Context   int
	Output    int
	Reasoning bool
	ToolCall  bool
}

// ProviderSpec is one selectable proxy backend identity.
type ProviderSpec struct {
	ID     string
	Key    string
	Npm    string
	Name   string
	KeyEnv string
	Models []ProviderModel
}

func DefaultProviderSpec() ProviderSpec {
	return ProviderSpec{
		ID:   "headroom",
		Key:  "headroom",
		Npm:  "@ai-sdk/openai-compatible",
		Name: "Headroom Proxy",
		Models: []ProviderModel{
			{ID: "gpt-4o", Display: "GPT-4o", Context: 128000, Output: 16384},
			{ID: "gpt-4.1", Display: "GPT-4.1", Context: 1048576, Output: 32768},
		},
	}
}

// ProviderSpecActive returns the managed headroom proxy identity.
func ProviderSpecActive() ProviderSpec {
	return DefaultProviderSpec()
}

// proxyWireModel returns the model id tokless pins into OpenAI-compatible
// agent configs.
func proxyWireModel() string {
	if v := strings.TrimSpace(os.Getenv("TOKLESS_PROXY_MODEL")); v != "" {
		return v
	}
	return "headroom"
}

// proxyWireKey returns the API key tokless pins into agent configs that have
// no environment-key mechanism.
func proxyWireKey() string {
	if v := strings.TrimSpace(os.Getenv("TOKLESS_PROXY_KEY")); v != "" {
		return v
	}
	return "tokless"
}

type proxyRouteStashEntry struct {
	File          string `json:"file"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"base_url"`
	Upstream      string `json:"upstream,omitempty"`
	BaseKey       string `json:"base_key,omitempty"`
	HadBaseKey    bool   `json:"had_base_key,omitempty"`
	HadAuth       bool   `json:"had_auth_token,omitempty"`
	MovedBase     bool   `json:"moved_base_key,omitempty"`
	HadHeader     bool   `json:"had_header,omitempty"`
	Header        string `json:"header,omitempty"`
	BaseLine      string `json:"base_line,omitempty"`
	HeaderLine    string `json:"header_line,omitempty"`
	ManagedNative bool   `json:"managed_native,omitempty"`
	Original      []byte `json:"original,omitempty"`
	Managed       []byte `json:"managed,omitempty"`
}

type proxyRouteStashFile struct {
	Providers map[string]proxyRouteStashEntry `json:"providers"`
}

func proxyRouteStashPath(agent string) string {
	return filepath.Join(util.HeadroomPathsResolved().Root, agent+".byok.stash.json")
}

func proxyRouteStashLock() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "byok.stash.lock")
}

// withProxyRouteStashLock runs fn with an exclusive advisory lock held for the
// duration of the call.
func withProxyRouteStashLock(fn func() error) error {
	if err := util.EnsureDir(util.HeadroomPathsResolved().Root); err != nil {
		return err
	}
	path := proxyRouteStashLock()
	token := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, writeErr := f.Write([]byte(token)); writeErr != nil {
				_ = f.Close()
				_ = os.Remove(path)
				return writeErr
			}
			if closeErr := f.Close(); closeErr != nil {
				_ = os.Remove(path)
				return closeErr
			}
			fnErr := fn()
			if content, readErr := os.ReadFile(path); readErr == nil && string(content) == token {
				_ = os.Remove(path)
			}
			return fnErr
		}
		if !os.IsExist(err) {
			return err
		}
		st, serr := os.Stat(path)
		if serr != nil {
			continue
		}
		if time.Since(st.ModTime()) > 10*time.Second {
			content, rerr := os.ReadFile(path)
			parts := strings.SplitN(strings.TrimSpace(string(content)), "-", 2)
			pid, perr := strconv.Atoi(parts[0])
			if rerr == nil && perr == nil && pid > 0 && pid == os.Getpid() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			live := false
			if rerr == nil && perr == nil && pid > 0 {
				live = proxyProcessAlive(pid)
			}
			if !live {
				stale := fmt.Sprintf("%s.stale.%d", path, time.Now().UnixNano())
				if os.Rename(path, stale) == nil {
					_ = os.Remove(stale)
					continue
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// stashTxn runs fn with exclusive access, providing the current stash for mutation.
// The entire read-modify-write is atomic under the advisory lock.
func stashTxn(agent string, fn func(map[string]proxyRouteStashEntry) error) error {
	return withProxyRouteStashLock(func() error {
		all := loadProxyRouteStashLocked(agent)
		return fn(all)
	})
}

func loadProxyRouteStashLocked(agent string) map[string]proxyRouteStashEntry {
	raw, ok := util.ReadFileSafe(proxyRouteStashPath(agent))
	if !ok {
		return map[string]proxyRouteStashEntry{}
	}
	var f proxyRouteStashFile
	if err := json.Unmarshal([]byte(raw), &f); err != nil || f.Providers == nil {
		return map[string]proxyRouteStashEntry{}
	}
	return f.Providers
}

func proxyRouteStashValid(agent string) bool {
	raw, ok := util.ReadFileSafe(proxyRouteStashPath(agent))
	if !ok {
		return true
	}
	var f proxyRouteStashFile
	return json.Unmarshal([]byte(raw), &f) == nil && f.Providers != nil
}

func restoreProxyRouteStash(agent, raw string, exists bool) error {
	path := proxyRouteStashPath(agent)
	if exists {
		return util.WriteFileMode(path, raw, 0o600)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// saveProxyRouteStash writes the stash state atomically.
func saveProxyRouteStash(agent string, providers map[string]proxyRouteStashEntry) error {
	if len(providers) == 0 {
		path := proxyRouteStashPath(agent)
		if raw, ok := util.ReadFileSafe(path); ok {
			var f proxyRouteStashFile
			if err := json.Unmarshal([]byte(raw), &f); err != nil || f.Providers == nil {
				return fmt.Errorf("refusing to replace malformed proxy stash %s", path)
			}
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	b, err := json.Marshal(proxyRouteStashFile{Providers: providers})
	if err != nil {
		return err
	}
	path := proxyRouteStashPath(agent)
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err = f.Write(b); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

// loadProxyRouteStash is deprecated: it bypasses the lock.
func loadProxyRouteStash(agent string) map[string]proxyRouteStashEntry {
	return loadProxyRouteStashLocked(agent)
}

func normalizedHeadroomUpstream(baseURL, api string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if api == "openai-completions" && strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		return baseURL[:len(baseURL)-len("/v1")]
	}
	if api == "anthropic-messages" && strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		return baseURL[:len(baseURL)-len("/v1")]
	}
	return baseURL
}

func proxyEndpointForAPI(api string) string {
	if api == "anthropic-messages" {
		return util.HeadroomProxyURL()
	}
	if api == "openai-completions" {
		return util.HeadroomProxyOpenAIURL()
	}
	return ""
}
