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

	"github.com/HoangP8/tokless/internal/util"
)

func grokProxyFiles() (pidFile, logFile string) {
	root := util.HeadroomPathsResolved().Root
	return filepath.Join(root, "grok-proxy.pid"), filepath.Join(root, "grok-proxy.log")
}

func grokProxyArgs(port int) []string {
	return []string{
		"proxy",
		"--port", strconv.Itoa(port),
		"--no-cache",
		"--protect-tool-results", "Bash",
		"--anthropic-api-url", "https://api.anthropic.com",
		"--openai-api-url", "https://cli-chat-proxy.grok.com",
	}
}

func grokProxyLiveZ(timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/livez", util.GrokOAuthProxyPort()))
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

func GrokOAuthProxyRunning() bool { return grokProxyLiveZ(proxyProbeTimeout) }

func GrokOAuthProxyOwned() bool { return GrokOAuthProxyRunning() && grokOwnershipValid() }

func grokOwnershipValid() bool {
	pidFile, _ := grokProxyFiles()
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

func grokRollback(proc *os.Process, pidFile string, cause error) error {
	return grokRollbackWithIdentity(proc, pidFile, nil, cause)
}

func grokRollbackWithIdentity(proc *os.Process, pidFile string, expected *processIdentityInfo, cause error) error {
	recordRaw, recordExists := util.ReadFileSafe(pidFile)
	if expected != nil && expected.Executable != "" {
		current, err := proxyIdentity(proc.Pid)
		if err != nil || !current.equal(*expected) {
			return cause
		}
	}
	if err := proxyKill(proc); err != nil {
		return fmt.Errorf("%w; grok rollback kill for pid %d: %v", cause, proc.Pid, err)
	}
	if err := proxyWait(proc); err != nil && !errors.Is(err, os.ErrProcessDone) {
		util.L.Sub("grok rollback wait: " + err.Error())
	}
	deadline := proxyNow().Add(proxyStopTimeout)
	for proxyNow().Before(deadline) {
		if proxyGone(proc) && !grokProxyLiveZ(proxyProbeTimeout) {
			if recordExists {
				current, ok := util.ReadFileSafe(pidFile)
				if !ok || current != recordRaw {
					return fmt.Errorf("%w; grok proxy ownership record changed during rollback — refusing to remove replacement record", cause)
				}
				if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("%w; grok rollback record removal: %v", cause, err)
				}
			}
			return cause
		}
		proxySleep(proxyPollInterval)
	}
	return fmt.Errorf("%w; grok rollback did not stop pid %d", cause, proc.Pid)
}

func StartGrokOAuthProxy() error {
	release, err := acquireProxyStartLock(proxyNow)
	if err != nil {
		return fmt.Errorf("grok proxy start: %w", err)
	}
	defer release()
	if GrokOAuthProxyRunning() {
		if !grokOwnershipValid() {
			return fmt.Errorf("port %d held by an unverified process — refusing to attach grok proxy", util.GrokOAuthProxyPort())
		}
		return nil
	}
	bin := ResolveHeadroomBin()
	if bin == "" {
		return fmt.Errorf("headroom binary not found — install headroom first")
	}
	port := util.GrokOAuthProxyPort()
	args := grokProxyArgs(port)
	pidFile, logFile := grokProxyFiles()
	if err := util.EnsureDir(filepath.Dir(logFile)); err != nil {
		return err
	}
	log, err := os.Create(logFile)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := proxySpawn(cmd); err != nil {
		return fmt.Errorf("grok proxy failed to start: %w", err)
	}
	identity, err := verifyIdentityWithRetry(cmd.Process.Pid, bin, args)
	if err != nil {
		if identity.Executable == "" {
			return cleanupUnverifiedChild(cmd.Process, fmt.Errorf("grok proxy startup identity unavailable; refusing to signal pid %d: %w", cmd.Process.Pid, err))
		}
		return fmt.Errorf("grok proxy startup identity unavailable; refusing to signal pid %d: %w", cmd.Process.Pid, err)
	}
	if err := proxyWrite(pidFile, proxyOwnership{PID: cmd.Process.Pid, Executable: identity.Executable, Args: identity.Args, Start: identity.Start}); err != nil {
		return grokRollbackWithIdentity(cmd.Process, pidFile, &identity, fmt.Errorf("grok proxy ownership record: %w", err))
	}
	deadline := proxyNow().Add(proxyReadyTimeout)
	for proxyNow().Before(deadline) {
		current, identityErr := proxyIdentity(cmd.Process.Pid)
		if identityErr != nil || !current.equal(identity) {
			return grokRollbackWithIdentity(cmd.Process, pidFile, &identity, fmt.Errorf("grok proxy identity changed during startup"))
		}
		if grokProxyLiveZ(proxyProbeTimeout) {
			return nil
		}
		proxySleep(proxyPollInterval)
	}
	return grokRollbackWithIdentity(cmd.Process, pidFile, &identity, fmt.Errorf("grok proxy did not become ready within %s — see %s", proxyReadyTimeout, logFile))
}

func StopGrokOAuthProxy() error {
	release, err := acquireProxyStartLock(proxyNow)
	if err != nil {
		return fmt.Errorf("grok proxy stop: %w", err)
	}
	defer release()
	pidFile, _ := grokProxyFiles()
	raw, ok := util.ReadFileSafe(pidFile)
	if !ok {
		if GrokOAuthProxyRunning() {
			return fmt.Errorf("Grok OAuth proxy listener is running without tokless ownership — refusing to stop")
		}
		return nil
	}
	var record proxyOwnership
	if err := json.Unmarshal([]byte(raw), &record); err != nil || record.PID <= 0 || record.Executable == "" || len(record.Args) == 0 || record.Start == "" {
		return fmt.Errorf("invalid grok proxy ownership record %s — refusing to stop", pidFile)
	}
	identity, err := proxyIdentity(record.PID)
	if err != nil {
		if proxyGone(&os.Process{Pid: record.PID}) {
			if grokProxyLiveZ(proxyProbeTimeout) {
				return fmt.Errorf("grok proxy pid %d is gone but a healthy listener remains — refusing to remove ownership record", record.PID)
			}
			current, currentOK := util.ReadFileSafe(pidFile)
			if !currentOK || current != raw {
				return fmt.Errorf("grok proxy ownership record changed during stale cleanup — refusing to remove replacement record")
			}
			return os.Remove(pidFile)
		}
		return fmt.Errorf("grok proxy pid %d identity could not be verified — refusing to stop", record.PID)
	}
	if !identity.matchesRecord(record) {
		return fmt.Errorf("grok proxy pid %d identity could not be verified — refusing to stop", record.PID)
	}
	proc, err := os.FindProcess(record.PID)
	if err != nil {
		return fmt.Errorf("failed to find grok proxy pid %d: %w", record.PID, err)
	}
	if err := proxyKill(proc); err != nil {
		return fmt.Errorf("failed to stop grok proxy pid %d: %w", record.PID, err)
	}
	if waitErr := proxyWait(proc); waitErr != nil && !errors.Is(waitErr, os.ErrProcessDone) {
		util.L.Sub("grok proxy wait: " + waitErr.Error())
	}
	deadline := proxyNow().Add(proxyStopTimeout)
	for proxyNow().Before(deadline) {
		if proxyGone(proc) && !grokProxyLiveZ(proxyProbeTimeout) {
			current, currentOK := util.ReadFileSafe(pidFile)
			if !currentOK || current != raw {
				return fmt.Errorf("grok proxy ownership record changed during stop — refusing to remove replacement record")
			}
			return os.Remove(pidFile)
		}
		proxySleep(proxyPollInterval)
	}
	return fmt.Errorf("grok proxy did not stop (pid %d)", record.PID)
}
