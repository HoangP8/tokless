package headroom

import (
	"encoding/json"
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
	proxyReadyTimeout = 15 * time.Second
	proxyStopTimeout  = 5 * time.Second
	proxyProbeTimeout = 2 * time.Second
	proxyPollInterval = 300 * time.Millisecond
)

type proxyOwnership struct {
	PID        int      `json:"pid"`
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	Start      string   `json:"start_fingerprint"`
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

func ProxyUpstreamURLs() (anthropic, openai string) {
	anthropic, openai = "https://api.anthropic.com", "https://api.openai.com"
	if v := strings.TrimSpace(os.Getenv("TOKLESS_HEADROOM_ANTHROPIC_URL")); v != "" {
		anthropic = v
	}
	if v := strings.TrimSpace(os.Getenv("TOKLESS_HEADROOM_OPENAI_URL")); v != "" {
		openai = v
	}
	return
}

func ResolveHeadroomBin() string {
	if util.HeadroomInstalled() {
		return util.HeadroomBin()
	}
	return util.Which("headroom")
}

func ProxyRunning() bool { return proxyLiveZProbe(proxyProbeTimeout) }

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

func StartProxy() error {
	if ProxyRunning() {
		util.L.Sub("headroom proxy already running on " + ProxyURL())
		return nil
	}
	bin := ResolveHeadroomBin()
	if bin == "" {
		return fmt.Errorf("headroom binary not found — install headroom first (run `tokless`)")
	}
	if !processIdentitySupported() {
		return fmt.Errorf("headroom proxy lifecycle is unsupported on this platform: trustworthy process identity is unavailable")
	}
	port := ProxyPort()
	anthropic, openai := ProxyUpstreamURLs()
	args := []string{"proxy", "--port", strconv.Itoa(port), "--anthropic-api-url", anthropic, "--openai-api-url", openai}
	pidFile, logFile := proxyFiles()
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
	identity, err := proxyIdentity(pid)
	if err != nil {
		return rollbackProxy(cmd.Process, pidFile, fmt.Errorf("headroom proxy identity could not be verified: %w", err))
	}
	if !identity.matches(bin, args) {
		return rollbackProxy(cmd.Process, pidFile, fmt.Errorf("headroom proxy identity could not be verified"))
	}
	if err := proxyWrite(pidFile, proxyOwnership{PID: pid, Executable: identity.Executable, Args: identity.Args, Start: identity.Start}); err != nil {
		return rollbackProxy(cmd.Process, pidFile, fmt.Errorf("headroom proxy ownership record: %w", err))
	}
	util.L.Sub("headroom proxy on " + ProxyURL() + " (cache mode; log: " + logFile + ")")
	deadline := proxyNow().Add(proxyReadyTimeout)
	for proxyNow().Before(deadline) {
		if proxyLiveZProbe(proxyProbeTimeout) {
			util.L.Ok("headroom proxy ready")
			return nil
		}
		proxySleep(proxyPollInterval)
	}
	return rollbackProxy(cmd.Process, pidFile, fmt.Errorf("headroom proxy did not become ready within %s — see %s", proxyReadyTimeout, logFile))
}

func StopProxy() error {
	pidFile, _ := proxyFiles()
	raw, ok := util.ReadFileSafe(pidFile)
	if !ok {
		return nil
	}
	var record proxyOwnership
	if err := json.Unmarshal([]byte(raw), &record); err != nil || record.PID <= 0 || record.Executable == "" || len(record.Args) == 0 || record.Start == "" {
		return fmt.Errorf("invalid proxy ownership record %s — refusing to stop", pidFile)
	}
	identity, err := proxyIdentity(record.PID)
	if err != nil || !identity.matchesRecord(record) {
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
			if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("headroom proxy stopped but ownership record could not be removed: %w", err)
			}
			util.L.Ok("headroom proxy stopped")
			return nil
		}
		proxySleep(proxyPollInterval)
	}
	return fmt.Errorf("headroom proxy did not stop (pid %d)", record.PID)
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
	return cause
}
