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
	"time"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/util"
)

func copilotProxyFiles() (pidFile, logFile string) {
	root := util.HeadroomPathsResolved().Root
	return filepath.Join(root, "copilot-vscode.pid"), filepath.Join(root, "copilot-vscode.log")
}

func copilotProxyArgs() []string {
	return []string{"wrap", "vscode", "--port", strconv.Itoa(agents.CopilotProxyPort()), "--no-configure"}
}

func copilotProxyLiveZ(timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(agents.CopilotProxyPort()) + "/livez")
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

var copilotProxyLiveProbe = copilotProxyLiveZ

var copilotVSCodeWired = agents.CopilotVSCodeProxyWired
var acquireCopilotLifecycleLock = AcquireProxyLifecycleLock

// EnsureCopilotProxyUp starts Headroom's OAuth-aware VS Code wrapper when needed.
// It fails open because session hooks must not block Copilot startup.
// Skips start when VS Code is not Tokless-wired so `proxy down` stays down.
func EnsureCopilotProxyUp() {
	if !util.ProxyRoutingEnabled() {
		return
	}
	release, err := acquireCopilotLifecycleLock()
	if err != nil {
		return
	}
	defer release()
	if !copilotVSCodeWired() {
		return
	}
	util.WithQuiet(func() { _ = StartCopilotProxy() })
}

func StartCopilotProxy() error {
	release, err := acquireProxyStartLock(proxyNow)
	if err != nil {
		return fmt.Errorf("copilot proxy start: %w", err)
	}
	defer release()
	if copilotProxyLiveProbe(proxyProbeTimeout) {
		if copilotProxyOwnershipValid() {
			return nil
		}
		return fmt.Errorf("port %d held by an unverified process — refusing to attach Copilot proxy", agents.CopilotProxyPort())
	}
	bin := ResolveHeadroomBin()
	if bin == "" {
		return fmt.Errorf("headroom binary not found — install headroom first")
	}
	args := copilotProxyArgs()
	pidFile, logFile := copilotProxyFiles()
	if err := util.EnsureDir(filepath.Dir(logFile)); err != nil {
		return err
	}
	log, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := proxySpawn(cmd); err != nil {
		return fmt.Errorf("copilot proxy failed to start: %w", err)
	}
	identity, err := verifyIdentityWithRetry(cmd.Process.Pid, bin, args)
	if err != nil {
		if identity.Executable == "" {
			return cleanupUnverifiedChild(cmd.Process, fmt.Errorf("copilot proxy startup identity unavailable; refusing to signal pid %d: %w", cmd.Process.Pid, err))
		}
		return fmt.Errorf("copilot proxy startup identity unavailable; refusing to signal pid %d: %w", cmd.Process.Pid, err)
	}
	if err := proxyWrite(pidFile, proxyOwnership{PID: cmd.Process.Pid, Executable: identity.Executable, Args: identity.Args, Start: identity.Start}); err != nil {
		return rollbackCopilotProxy(cmd.Process, pidFile, &identity, fmt.Errorf("copilot proxy ownership record: %w", err))
	}
	deadline := proxyNow().Add(proxyReadyTimeout)
	for proxyNow().Before(deadline) {
		current, identityErr := proxyIdentity(cmd.Process.Pid)
		if identityErr != nil || !current.equal(identity) {
			return rollbackCopilotProxy(cmd.Process, pidFile, &identity, fmt.Errorf("copilot proxy identity changed during startup"))
		}
		if copilotProxyLiveProbe(proxyProbeTimeout) {
			return nil
		}
		proxySleep(proxyPollInterval)
	}
	return rollbackCopilotProxy(cmd.Process, pidFile, &identity, fmt.Errorf("copilot proxy did not become ready within %s — see %s", proxyReadyTimeout, logFile))
}

func rollbackCopilotProxy(proc *os.Process, pidFile string, expected *processIdentityInfo, cause error) error {
	owned := false
	var recordRaw string
	recordExists := false
	if raw, ok := util.ReadFileSafe(pidFile); ok {
		recordRaw, recordExists = raw, true
		var record proxyOwnership
		if json.Unmarshal([]byte(raw), &record) == nil && record.PID == proc.Pid {
			if identity, err := proxyIdentity(proc.Pid); err == nil && identity.matchesRecord(record) {
				owned = true
			}
		}
	}
	// Process was spawned by this call, so ownership-file failure must not leak it.
	if owned || expected == nil || expected.Executable == "" {
		if expected != nil && expected.Executable != "" {
			current, err := proxyIdentity(proc.Pid)
			if err != nil || !current.equal(*expected) {
				return cause
			}
		}
		if err := proxyKill(proc); err != nil {
			return fmt.Errorf("%w; rollback kill for pid %d: %v", cause, proc.Pid, err)
		}
		if err := proxyWait(proc); err != nil && !errors.Is(err, os.ErrProcessDone) {
			util.L.Sub("copilot proxy rollback wait: " + err.Error())
		}
	}
	deadline := proxyNow().Add(proxyStopTimeout)
	for proxyNow().Before(deadline) {
		if proxyGone(proc) && !copilotProxyLiveProbe(proxyProbeTimeout) {
			if recordExists {
				current, ok := util.ReadFileSafe(pidFile)
				if !ok || current != recordRaw {
					return fmt.Errorf("%w; copilot proxy ownership record changed during rollback — refusing to remove replacement record", cause)
				}
				if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("%w; rollback record removal: %v", cause, err)
				}
			}
			return cause
		}
		proxySleep(proxyPollInterval)
	}
	return fmt.Errorf("%w; copilot proxy rollback did not stop pid %d", cause, proc.Pid)
}

func copilotProxyOwnershipValid() bool {
	pidFile, _ := copilotProxyFiles()
	raw, ok := util.ReadFileSafe(pidFile)
	if !ok {
		return false
	}
	var record proxyOwnership
	if err := json.Unmarshal([]byte(raw), &record); err != nil || record.PID <= 0 || record.Executable == "" || len(record.Args) == 0 || record.Start == "" {
		return false
	}
	identity, err := proxyIdentity(record.PID)
	return err == nil && identity.matchesRecord(record)
}

func CopilotProxyRunning() bool { return copilotProxyLiveProbe(proxyProbeTimeout) }
func CopilotProxyOwned() bool   { return CopilotProxyRunning() && copilotProxyOwnershipValid() }

func StopCopilotProxy() error {
	release, err := acquireProxyStartLock(proxyNow)
	if err != nil {
		return fmt.Errorf("copilot proxy stop: %w", err)
	}
	defer release()
	pidFile, _ := copilotProxyFiles()
	raw, ok := util.ReadFileSafe(pidFile)
	if !ok {
		if CopilotProxyRunning() {
			return fmt.Errorf("Copilot proxy listener is running without tokless ownership — refusing to stop")
		}
		return nil
	}
	var record proxyOwnership
	if err := json.Unmarshal([]byte(raw), &record); err != nil || record.PID <= 0 || record.Executable == "" || len(record.Args) == 0 || record.Start == "" {
		return fmt.Errorf("invalid Copilot proxy ownership record %s — refusing to stop", pidFile)
	}
	identity, err := proxyIdentity(record.PID)
	if err != nil || !identity.matchesRecord(record) {
		if proxyGone(&os.Process{Pid: record.PID}) {
			if copilotProxyLiveProbe(proxyProbeTimeout) {
				return fmt.Errorf("Copilot proxy pid %d is gone but a healthy listener remains — refusing to remove ownership record", record.PID)
			}
			current, currentOK := util.ReadFileSafe(pidFile)
			if !currentOK || current != raw {
				return fmt.Errorf("Copilot proxy ownership record changed during stale cleanup — refusing to remove replacement record")
			}
			return os.Remove(pidFile)
		}
		return fmt.Errorf("Copilot proxy pid %d identity could not be verified — refusing to stop", record.PID)
	}
	proc, err := os.FindProcess(record.PID)
	if err != nil {
		return fmt.Errorf("failed to find Copilot proxy pid %d: %w", record.PID, err)
	}
	if err := proxyKill(proc); err != nil {
		return fmt.Errorf("failed to stop Copilot proxy pid %d: %w", record.PID, err)
	}

	_ = proxyWait(proc)
	deadline := proxyNow().Add(proxyStopTimeout)
	for proxyNow().Before(deadline) {
		if proxyGone(proc) && !copilotProxyLiveProbe(proxyProbeTimeout) {
			current, currentOK := util.ReadFileSafe(pidFile)
			if !currentOK || current != raw {
				return fmt.Errorf("Copilot proxy ownership record changed during stop — refusing to remove replacement record")
			}
			return os.Remove(pidFile)
		}
		proxySleep(proxyPollInterval)
	}
	return fmt.Errorf("Copilot proxy did not stop (pid %d)", record.PID)
}
