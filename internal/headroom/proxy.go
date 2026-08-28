package headroom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

const (
	proxyReadyTimeout    = 15 * time.Second
	proxyStopTimeout     = 5 * time.Second
	proxyProbeTimeout    = 2 * time.Second
	proxyPollInterval    = 300 * time.Millisecond
	proxyIdentityTimeout = 2 * time.Second
	proxyStartLockWait   = 30 * time.Second
	proxyStartLockStale  = 60 * time.Second
)

type proxyOwnership struct {
	PID        int      `json:"pid"`
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	Start      string   `json:"start_fingerprint"`
}

type proxySupervisedState struct {
	PID         int      `json:"pid"`
	Executable  string   `json:"executable"`
	Args        []string `json:"args"`
	ManagerArgs []string `json:"manager_args,omitempty"`
}

var (
	proxyLiveZProbe = proxyLiveZ
	proxySpawn      = spawnDetached
	proxyIdentity   = identifyProcess
	proxyKill       = killProcess
	proxyWrite      = writeProxyOwnership
	proxyGone       = processGone
	proxyWait       = func(proc *os.Process) error { _, err := proc.Wait(); return err }
	proxySleep      = time.Sleep
	proxyNow        = time.Now
)

func ProxyPort() int         { return util.HeadroomProxyPort() }
func ProxyURL() string       { return util.HeadroomProxyURL() }
func ProxyOpenAIURL() string { return util.HeadroomProxyOpenAIURL() }

// realUpstreamURLs are the provider hosts Headroom should reach.
func realUpstreamURLs() (anthropic, openai string) {
	anthropic, openai = "https://api.anthropic.com", "https://api.openai.com"
	if v := strings.TrimSpace(os.Getenv("TOKLESS_HEADROOM_ANTHROPIC_URL")); v != "" {
		anthropic = v
	} else if st, ok := util.ReadProxyRuntime(); ok && st.AnthropicURL != "" {
		anthropic = st.AnthropicURL
	}
	if v := strings.TrimSpace(os.Getenv("TOKLESS_HEADROOM_OPENAI_URL")); v != "" {
		openai = v
	} else if st, ok := util.ReadProxyRuntime(); ok && st.OpenAIURL != "" {
		openai = st.OpenAIURL
	}
	return
}

// ProxyUpstreamURLs returns the URLs passed to `headroom proxy --*-api-url`.
func ProxyUpstreamURLs() (anthropic, openai string) {
	return realUpstreamURLs()
}

// ProxyUpstreamGeminiURLs returns optional upstream overrides for the Gemini
// (`--gemini-api-url`) and CloudCode (`--cloudcode-api-url`) provider routes,
// which tokless wires only when the environment pins them.
func ProxyUpstreamGeminiURLs() (gemini, cloudcode string) {
	gemini = strings.TrimSpace(os.Getenv("TOKLESS_HEADROOM_GEMINI_URL"))
	cloudcode = strings.TrimSpace(os.Getenv("TOKLESS_HEADROOM_CLOUDCODE_URL"))
	if st, ok := util.ReadProxyRuntime(); ok {
		if gemini == "" {
			gemini = st.GeminiURL
		}
		if cloudcode == "" {
			cloudcode = st.CloudCodeURL
		}
	}
	return
}

func proxyArgs(port int) []string {
	anthropic, openai := ProxyUpstreamURLs()
	gemini, cloudcode := ProxyUpstreamGeminiURLs()
	args := []string{
		"proxy",
		"--port", strconv.Itoa(port),
		// Tokless keeps the provider-prefix cache but disables semantic
		// response caching (see ProxyCachePolicy): the proxy never re-runs or
		// re-serves a semantically-similar prompt.
		"--no-cache",
		"--anthropic-api-url", anthropic,
		"--openai-api-url", openai,
	}
	if gemini != "" {
		args = append(args, "--gemini-api-url", gemini)
	}
	if cloudcode != "" {
		args = append(args, "--cloudcode-api-url", cloudcode)
	}
	return args
}

func ResolveHeadroomBin() string {
	if util.HeadroomInstalled() {
		return util.HeadroomBin()
	}
	return util.Which("headroom")
}

func ProxyRunning() bool { return proxyLiveZProbe(proxyProbeTimeout) }

// EnsureProxyUp starts the daemon when it is not already running, silently.
// Reloads the BYOK route table so a warm daemon still matches current keys.
// Quiet so hook/MCP stdout (JSON protocols) stays clean.
func EnsureProxyUp() {
	util.WithQuiet(func() { _ = StartProxy() })
}

// RunProxyForeground lets an OS service manager own the real Headroom process.
// Runtime state supplies URLs when no interactive shell environment exists.
func RunProxyForeground() error {
	bin := ResolveHeadroomBin()
	if bin == "" {
		return fmt.Errorf("headroom binary not found — install headroom first (run `tokless`)")
	}
	port := ProxyPort()
	args := proxyArgs(port)
	if err := persistProxyRuntime(port); err != nil {
		return fmt.Errorf("headroom proxy runtime record: %w", err)
	}
	return runHeadroomForeground(bin, args)
}

func proxyLiveZ(timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(ProxyURL() + "/livez")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var live struct {
		Service string `json:"service"`
	}
	return json.NewDecoder(resp.Body).Decode(&live) == nil && live.Service == "headroom-proxy"
}

func proxyFiles() (pidFile, logFile string) {
	root := util.HeadroomPathsResolved().Root
	return filepath.Join(root, "proxy.pid"), filepath.Join(root, "proxy.log")
}

func proxySupervisedFile() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "proxy.supervised.json")
}

func writeProxySupervisedState(pid int, bin string, args, managerArgs []string) error {
	data, err := json.Marshal(proxySupervisedState{PID: pid, Executable: bin, Args: args, ManagerArgs: managerArgs})
	if err != nil {
		return err
	}
	return writeProxyFileAtomic(proxySupervisedFile(), data, 0o600)
}

func clearProxySupervisedState() error {
	err := os.Remove(proxySupervisedFile())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func clearProxyState(pidFile string) error {
	var errs []error
	if pidFile != "" {
		if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	if err := clearProxySupervisedState(); err != nil {
		errs = append(errs, err)
	}
	if err := clearProxyRuntime(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func proxySupervisedArgsMatch(port int) bool {
	raw, ok := util.ReadFileSafe(proxySupervisedFile())
	if !ok {
		return false
	}
	var state proxySupervisedState
	if json.Unmarshal([]byte(raw), &state) != nil || state.PID <= 0 || state.Executable == "" {
		return false
	}
	identity, err := proxyIdentity(state.PID)
	checkArgs := state.Args
	if len(state.ManagerArgs) > 0 {
		checkArgs = state.ManagerArgs
	}
	return err == nil && identity.matches(state.Executable, checkArgs) && equalStrings(state.Args, proxyArgs(port))
}

// proxyStartLock returns the cross-process start-lock path. Multiple opencode
// MCP workers may call StartProxy concurrently (separate OS processes); this
// lock makes exactly one of them own the spawn+readiness critical section.
func proxyStartLock() string {
	root := util.HeadroomPathsResolved().Root
	return filepath.Join(root, "proxy.start.lock")
}

// acquireProxyStartLock takes the start lock, waiting (bounded) for a
// concurrent caller to finish, and stealing the lock when it looks stale
// (crashed holder). It returns a release func for the caller.
func acquireProxyStartLock(now func() time.Time) (func(), error) {
	if err := util.EnsureDir(util.HeadroomPathsResolved().Root); err != nil {
		return nil, err
	}
	path := proxyStartLock()
	deadline := now().Add(proxyStartLockWait)
	token := fmt.Sprintf("%d-%d", os.Getpid(), now().UnixNano())
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, writeErr := f.Write([]byte(token)); writeErr != nil {
				_ = f.Close()
				_ = os.Remove(path)
				return nil, writeErr
			}
			if closeErr := f.Close(); closeErr != nil {
				_ = os.Remove(path)
				return nil, closeErr
			}
			return func() {
				if content, readErr := os.ReadFile(path); readErr == nil && string(content) == token {
					_ = os.Remove(path)
				}
			}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if st, serr := os.Stat(path); serr == nil && now().Sub(st.ModTime()) > proxyStartLockStale {
			stale := fmt.Sprintf("%s.stale.%d", path, now().UnixNano())
			if os.Rename(path, stale) == nil {
				_ = os.Remove(stale)
				continue
			}
		}
		if !now().Before(deadline) {
			return nil, fmt.Errorf("timed out waiting for another proxy start to finish")
		}
		proxySleep(proxyPollInterval)
	}
}

// proxyArgsMatchRecorded reports whether the persisted ownership record (when
// present) lists the argv tokless would launch today.
func proxyArgsMatchRecorded(port int) bool {
	pidFile, _ := proxyFiles()
	raw, ok := util.ReadFileSafe(pidFile)
	if !ok {
		return false
	}
	var record proxyOwnership
	if err := json.Unmarshal([]byte(raw), &record); err != nil || record.PID <= 0 || record.Executable == "" || record.Start == "" || len(record.Args) == 0 {
		return false
	}
	identity, err := proxyIdentity(record.PID)
	if err != nil || !identity.matchesRecord(record) {
		return false
	}
	want := proxyArgs(port)
	if equalStrings(record.Args, want) {
		return true
	}
	return len(record.Args) > len(want) && equalStrings(record.Args[len(record.Args)-len(want):], want)
}

func StartProxy() error {
	release, err := acquireProxyStartLock(proxyNow)
	if err != nil {
		return fmt.Errorf("headroom proxy start: %w", err)
	}
	defer release()
	cleanupLegacyRouteState()
	port := ProxyPort()
	args := proxyArgs(port)
	pidFile, _ := proxyFiles()
	stopRequested, err := proxyStopRequested()
	if err != nil {
		return fmt.Errorf("headroom proxy stop state: %w", err)
	}
	if stopRequested {
		return fmt.Errorf("headroom proxy stop is in progress")
	}
	if ProxyRunning() {
		if proxyArgsMatchRecorded(port) || proxySupervisedArgsMatch(port) {
			if err := persistProxyRuntime(port); err != nil {
				return fmt.Errorf("headroom proxy runtime record: %w", err)
			}
			util.L.Sub("headroom proxy already running on " + ProxyURL())
			return nil
		}
		if _, ok := util.ReadFileSafe(pidFile); !ok {
			return fmt.Errorf("headroom proxy is running without tokless ownership — refusing to replace")
		}
		util.L.Sub("headroom proxy running with stale args — restarting (" + ProxyURL() + ")")
		if err := stopHeadroomDaemon(); err != nil {
			return err
		}
		args = proxyArgs(port)
	}
	bin := ResolveHeadroomBin()
	if bin == "" {
		return fmt.Errorf("headroom binary not found — install headroom first (run `tokless`)")
	}
	if !processIdentitySupported() {
		return fmt.Errorf("headroom proxy lifecycle is unsupported on this platform: trustworthy process identity is unavailable")
	}
	_, logFile := proxyFiles()
	if err := util.EnsureDir(filepath.Dir(logFile)); err != nil {
		return err
	}
	log, err := os.Create(logFile)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := proxySpawn(cmd); err != nil {
		_ = log.Close()
		return fmt.Errorf("headroom proxy failed to start: %w", err)
	}
	_ = log.Close()
	if cmd.Process == nil {
		return fmt.Errorf("headroom proxy started without a process")
	}
	pid := cmd.Process.Pid
	identity, err := verifyIdentityWithRetry(pid, bin, args)
	if err != nil {
		return rollbackProxy(cmd.Process, pidFile, err)
	}
	if err := proxyWrite(pidFile, proxyOwnership{PID: pid, Executable: identity.Executable, Args: identity.Args, Start: identity.Start}); err != nil {
		return rollbackProxy(cmd.Process, pidFile, fmt.Errorf("headroom proxy ownership record: %w", err))
	}
	util.L.Sub("headroom proxy on " + ProxyURL() + " (semantic cache off; log: " + logFile + ")")
	deadline := proxyNow().Add(proxyReadyTimeout)
	for proxyNow().Before(deadline) {
		if proxyLiveZProbe(proxyProbeTimeout) {
			if err := persistProxyRuntime(port); err != nil {
				return rollbackProxy(cmd.Process, pidFile, fmt.Errorf("headroom proxy runtime record: %w", err))
			}
			util.L.Ok("headroom proxy ready")
			return nil
		}
		proxySleep(proxyPollInterval)
	}
	return rollbackProxy(cmd.Process, pidFile, fmt.Errorf("headroom proxy did not become ready within %s — see %s", proxyReadyTimeout, logFile))
}

// verifyIdentityWithRetry captures the spawned process identity, tolerating the
// brief window right after exec where /proc is not yet fully populated (lookup
// errors), but failing fast on a verified-but-mismatched identity.
func verifyIdentityWithRetry(pid int, bin string, args []string) (processIdentityInfo, error) {
	var lastErr error
	deadline := proxyNow().Add(proxyIdentityTimeout)
	for proxyNow().Before(deadline) {
		identity, err := proxyIdentity(pid)
		if err != nil {
			lastErr = err
			proxySleep(proxyPollInterval)
			continue
		}
		if !identity.matches(bin, args) {
			return processIdentityInfo{}, fmt.Errorf("headroom proxy identity could not be verified")
		}
		return identity, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("identity never surfaced")
	}
	return processIdentityInfo{}, fmt.Errorf("headroom proxy identity could not be verified: %w", lastErr)
}

// persistProxyRuntime saves the effective daemon runtime (port + upstreams +
// selected provider) so a later `tokless proxy` invocation reproduces the same
// setup without requiring the original env vars.
func persistProxyRuntime(port int) error {
	anthropic, openai := realUpstreamURLs()
	gemini, cloudcode := ProxyUpstreamGeminiURLs()
	p := util.ProxyRuntime{
		Port:         port,
		AnthropicURL: anthropic,
		OpenAIURL:    openai,
		GeminiURL:    gemini,
		CloudCodeURL: cloudcode,
	}
	if v := strings.TrimSpace(os.Getenv(providerEnvVar())); v != "" {
		p.Provider = v
	} else if st, ok := util.ReadProxyRuntime(); ok {
		p.Provider = st.Provider
	}
	return util.SaveHeadroomProxyRuntime(p)
}

// providerEnvVar is the module-literal for the BYOK provider selector, kept
// string-stable here so headroom's proxy never depends on the agents package.
func providerEnvVar() string { return "TOKLESS_PROXY_PROVIDER" }

// stopHeadroomDaemon stops the Headroom process.
func stopHeadroomDaemon() error {
	pidFile, _ := proxyFiles()
	raw, ok := util.ReadFileSafe(pidFile)
	if !ok {
		return stopSupervisedDaemon()
	}
	var record proxyOwnership
	if err := json.Unmarshal([]byte(raw), &record); err != nil || record.PID <= 0 || record.Executable == "" || len(record.Args) == 0 || record.Start == "" {
		return fmt.Errorf("invalid proxy ownership record %s — refusing to stop", pidFile)
	}
	identity, err := proxyIdentity(record.PID)
	if err != nil {
		if proxyGone(&os.Process{Pid: record.PID}) {
			util.L.Sub("headroom proxy already stopped — removing stale ownership record")
			if err := clearProxyState(pidFile); err != nil {
				return fmt.Errorf("headroom proxy already stopped but state could not be cleared: %w", err)
			}
			return nil
		}
		return fmt.Errorf("proxy pid %d identity could not be verified — refusing to stop", record.PID)
	}
	if !identity.matchesRecord(record) {
		return fmt.Errorf("proxy pid %d identity could not be verified — refusing to stop", record.PID)
	}
	proc, err := os.FindProcess(record.PID)
	if err != nil {
		return fmt.Errorf("failed to find headroom proxy pid %d: %w", record.PID, err)
	}
	if err := proxyKill(proc); err != nil {
		return fmt.Errorf("failed to stop headroom proxy pid %d: %w", record.PID, err)
	}
	deadline := proxyNow().Add(proxyStopTimeout)
	for proxyNow().Before(deadline) {
		if proxyGone(proc) && !proxyLiveZProbe(proxyProbeTimeout) {
			if err := clearProxyState(pidFile); err != nil {
				return fmt.Errorf("headroom proxy stopped but state could not be cleared: %w", err)
			}
			util.L.Ok("headroom proxy stopped")
			return nil
		}
		proxySleep(proxyPollInterval)
	}
	return fmt.Errorf("headroom proxy did not stop (pid %d)", record.PID)
}

func stopSupervisedDaemon() error {
	raw, ok := util.ReadFileSafe(proxySupervisedFile())
	if !ok {
		return nil
	}
	var state proxySupervisedState
	if err := json.Unmarshal([]byte(raw), &state); err != nil || state.PID <= 0 || state.Executable == "" || len(state.Args) == 0 {
		return fmt.Errorf("invalid supervised proxy record — refusing to stop")
	}
	identity, err := proxyIdentity(state.PID)
	if err != nil || !identity.matches(state.Executable, state.Args) {
		if proxyGone(&os.Process{Pid: state.PID}) {
			if err := clearProxyState(""); err != nil {
				return fmt.Errorf("supervised proxy stopped but state could not be cleared: %w", err)
			}
			return nil
		}
		return fmt.Errorf("supervised proxy pid %d identity could not be verified — refusing to stop", state.PID)
	}
	proc, err := os.FindProcess(state.PID)
	if err != nil {
		return err
	}
	if err := proxyKill(proc); err != nil {
		return fmt.Errorf("failed to stop supervised proxy pid %d: %w", state.PID, err)
	}
	deadline := proxyNow().Add(proxyStopTimeout)
	for proxyNow().Before(deadline) {
		if proxyGone(proc) && !proxyLiveZProbe(proxyProbeTimeout) {
			if err := clearProxyState(""); err != nil {
				return fmt.Errorf("supervised proxy stopped but state could not be cleared: %w", err)
			}
			return nil
		}
		proxySleep(proxyPollInterval)
	}
	return fmt.Errorf("supervised proxy did not stop (pid %d)", state.PID)
}

// StopProxy stops the Headroom daemon.
func StopProxy() error {
	release, err := acquireProxyStartLock(proxyNow)
	if err != nil {
		return fmt.Errorf("headroom proxy stop: %w", err)
	}
	defer release()
	if err := DisableProxyAutostart(); err != nil {
		return fmt.Errorf("headroom proxy autostart stop: %w", err)
	}
	if err := requestProxyStop(); err != nil {
		return fmt.Errorf("headroom proxy stop request: %w", err)
	}
	err = stopHeadroomDaemon()
	if err == nil {
		err = clearProxyStopRequest()
	}
	cleanupLegacyRouteState()
	return err
}

// clearProxyRuntime drops the persisted runtime after daemon resources are gone.
func clearProxyRuntime() error { return util.ClearHeadroomProxyRuntime() }

// cleanupLegacyRouteState removes the retired per-key route proxy state.
func cleanupLegacyRouteState() {
	root := util.HeadroomPathsResolved().Root
	pidFile := filepath.Join(root, "proxy.route.pid")
	raw, ok := util.ReadFileSafe(pidFile)
	if ok {
		if pid, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && pid > 0 && processIdentitySupported() {
			if identity, err := proxyIdentity(pid); err == nil && len(identity.Args) > 0 && identity.Args[0] == "__route-proxy-serve" {
				if proc, err := os.FindProcess(pid); err == nil {
					_ = proxyKill(proc)
				}
			}
		}
		_ = os.Remove(pidFile)
	}
	_ = os.Remove(filepath.Join(root, "proxy.routes.json"))
	_ = os.Remove(filepath.Join(root, "proxy.route.log"))
}

func (p processIdentityInfo) matches(executable string, args []string) bool {
	want, err := normalizedExecutable(executable)
	if err != nil {
		return false
	}
	if p.Executable == want && equalStrings(p.Args, args) {
		return true
	}
	if len(p.Args) == 0 || !equalStrings(p.Args[1:], args) {
		return false
	}
	launcher, err := normalizedExecutable(p.Args[0])
	return err == nil && launcher == want
}
func normalizedExecutable(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
}
func (p processIdentityInfo) matchesRecord(r proxyOwnership) bool {
	return p.Start == r.Start && p.Executable == r.Executable && equalStrings(p.Args, r.Args)
}
func (p processIdentityInfo) equal(other processIdentityInfo) bool {
	return p.Start == other.Start && p.Executable == other.Executable && equalStrings(p.Args, other.Args)
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func writeProxyOwnership(path string, record proxyOwnership) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return writeProxyFileAtomic(path, data, 0o600)
}

func rollbackProxy(proc *os.Process, pidFile string, cause error) error {
	if err := proxyKill(proc); err != nil {
		return fmt.Errorf("%w; rollback kill for pid %d: %v", cause, proc.Pid, err)
	}
	if err := proxyWait(proc); err != nil {
		return fmt.Errorf("%w; rollback wait for pid %d: %v", cause, proc.Pid, err)
	}
	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w; rollback record removal: %v", cause, err)
	}
	return errors.Join(cause, clearProxySupervisedState())
}
