//go:build darwin

package headroom

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"encoding/xml"

	"github.com/HoangP8/tokless/internal/util"
)

// macOS autostart: a launchd user agent plist so the proxy starts at login
// and stays up (KeepAlive). Same tokens as the Linux unit: __proxy-run /
// __proxy-stop.
const proxyAutostartLabel = "com.tokless.headroom.proxy"

var runLaunchctl = func(args ...string) ([]byte, error) {
	return exec.Command("launchctl", args...).CombinedOutput()
}

func proxyAutostartPlistPath() string {
	return filepath.Join(util.Home(), "Library", "LaunchAgents", proxyAutostartLabel+".plist")
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func proxyAutostartPlistBody(toklessBin string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>` + xmlEscape(proxyAutostartLabel) + `</string>
    <key>Comment</key>
    <string>tokless-managed</string>
    <key>ProgramArguments</key>
    <array>
        <string>` + xmlEscape(toklessBin) + `</string>
        <string>__proxy-run</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>` + xmlEscape(proxyAutostartLogPath()) + `</string>
    <key>StandardErrorPath</key>
    <string>` + xmlEscape(proxyAutostartLogPath()) + `</string>
</dict>
</plist>
`
}

func proxyAutostartLogPath() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "proxy.log")
}

// macCurrentUID returns the numeric uid of the current user so we can
// construct launchctl domain strings like "gui/501".
func macCurrentUID() string {
	return strconv.Itoa(os.Getuid())
}

func EnableProxyAutostart() (err error) {
	bin := util.ToklessAbs()
	if bin == "" || util.IsGoTestExecutable(bin) {
		return nil
	}
	if _, err := exec.LookPath("launchctl"); err != nil {
		return fmt.Errorf("%w: launchctl not found; keeping proxy running for this session", ErrProxyAutostartUnavailable)
	}
	path := proxyAutostartPlistPath()
	body := proxyAutostartPlistBody(bin)
	if cur, ok := util.ReadFileSafe(path); ok && !strings.Contains(cur, "tokless-managed") {
		return fmt.Errorf("refusing to replace non-tokless launch agent %s", path)
	}
	if cur, ok := util.ReadFileSafe(path); ok && cur == body &&
		ProxyAutostartEnabled() && ProxyRunning() && proxySupervisedArgsMatch(proxyPortFromRuntime()) {
		return nil
	}
	oldPlist, oldPlistExists := util.ReadFileSafe(path)
	if err := util.EnsureDir(filepath.Dir(proxyAutostartLogPath())); err != nil {
		return err
	}
	oldLoaded := false
	oldEnabled := true
	if oldPlistExists {
		output, printErr := exec.Command("launchctl", "print", domain()+"/"+proxyAutostartLabel).CombinedOutput()
		if printErr == nil {
			oldLoaded = true
		} else if !launchctlServiceNotFound(output) {
			return fmt.Errorf("query %s: %w", proxyAutostartLabel, printErr)
		}
		oldEnabled, err = launchctlServiceEnabled()
		if err != nil {
			return err
		}
	}
	proxyWasRunning := ProxyRunning() && (proxyArgsMatchRecorded(proxyPortFromRuntime()) || proxySupervisedArgsMatch(proxyPortFromRuntime()))
	mutated := false
	committed := false
	defer func() {
		if committed || !mutated {
			return
		}
		var rollbackErrs []error
		if rollbackErr := bootoutProxyAgent(); rollbackErr != nil {
			rollbackErrs = append(rollbackErrs, rollbackErr)
		}
		if oldPlistExists {
			if rollbackErr := util.WriteFile(path, oldPlist); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
		} else {
			if rollbackErr := os.Remove(path); rollbackErr != nil && !os.IsNotExist(rollbackErr) {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
		}
		if oldLoaded {
			if rollbackErr := exec.Command("launchctl", "bootstrap", domain(), path).Run(); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
			action := "disable"
			if oldEnabled {
				action = "enable"
			}
			if rollbackErr := exec.Command("launchctl", action, domain()+"/"+proxyAutostartLabel).Run(); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
		} else if !oldEnabled {
			if rollbackErr := exec.Command("launchctl", "disable", domain()+"/"+proxyAutostartLabel).Run(); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
		}
		if !oldLoaded && proxyWasRunning {
			if rollbackErr := restoreProxyAfterAutostartRollback(proxyWasRunning); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
		}
		err = errors.Join(err, errors.Join(rollbackErrs...))
	}()
	if cur, ok := util.ReadFileSafe(path); !ok || cur != body {
		if err := util.EnsureDir(filepath.Dir(path)); err != nil {
			return err
		}
		if err := util.WriteFile(path, body); err != nil {
			return err
		}
		mutated = true
	}
	mutated = true
	if err := bootoutProxyAgent(); err != nil {
		return err
	}
	if err := stopHeadroomDaemonForHandoff(); err != nil {
		return err
	}
	if err := exec.Command("launchctl", "bootstrap", domain(), path).Run(); err != nil {
		return fmt.Errorf("launchctl bootstrap %s: %w", proxyAutostartLabel, err)
	}
	if err := exec.Command("launchctl", "enable", domain()+"/"+proxyAutostartLabel).Run(); err != nil {
		return fmt.Errorf("launchctl enable %s: %w", proxyAutostartLabel, err)
	}
	deadline := proxyNow().Add(proxyReadyTimeout)
	for proxyNow().Before(deadline) {
		if ProxyRunning() && proxySupervisedArgsMatch(proxyPortFromRuntime()) {
			break
		}
		proxySleep(proxyPollInterval)
	}
	if !ProxyRunning() || !proxySupervisedArgsMatch(proxyPortFromRuntime()) {
		return fmt.Errorf("%s loaded but proxy is not ready", proxyAutostartLabel)
	}
	committed = true
	return nil
}

func launchctlServiceEnabled() (bool, error) {
	out, err := exec.Command("launchctl", "print-disabled", domain()).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("query disabled launch agents: %w", err)
	}
	needle := `"` + proxyAutostartLabel + `" => true`
	return !strings.Contains(string(out), needle), nil
}

func domain() string {
	return "gui/" + macCurrentUID()
}

func bootoutProxyAgent() error {
	output, err := runLaunchctl("bootout", domain(), proxyAutostartLabel)
	if err == nil {
		return nil
	}
	if _, printErr := runLaunchctl("print", domain()+"/"+proxyAutostartLabel); printErr == nil {
		return fmt.Errorf("bootout %s: %w", proxyAutostartLabel, err)
	}
	if launchctlServiceNotFound(output) {
		return nil
	}
	return fmt.Errorf("bootout %s: %w", proxyAutostartLabel, err)
}

func launchctlServiceNotFound(output []byte) bool {
	s := strings.ToLower(string(output))
	return strings.Contains(s, "could not find service") ||
		strings.Contains(s, "service not found") ||
		strings.Contains(s, "no such process")
}

func DisableProxyAutostart() error {
	if _, err := exec.LookPath("launchctl"); err != nil {
		return nil
	}
	path := proxyAutostartPlistPath()
	raw, ok := util.ReadFileSafe(path)
	if !ok || !strings.Contains(raw, "tokless-managed") {
		return nil
	}
	if err := bootoutProxyAgent(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		var rollbackErr error
		if writeErr := util.WriteFile(path, raw); writeErr != nil {
			rollbackErr = writeErr
		} else if bootErr := exec.Command("launchctl", "bootstrap", domain(), path).Run(); bootErr != nil {
			rollbackErr = bootErr
		}
		return errors.Join(err, rollbackErr)
	}
	return nil
}

func ProxyAutostartEnabled() bool {
	path := proxyAutostartPlistPath()
	raw, ok := util.ReadFileSafe(path)
	if !ok || !strings.Contains(raw, "__proxy-run") {
		return false
	}
	macUID := macCurrentUID()
	out, err := exec.Command("launchctl", "print", "gui/"+macUID+"/"+proxyAutostartLabel).Output()
	if err != nil {
		return false
	}
	s := string(out)
	return strings.Contains(s, "state = \"running\"")
}

// ProxyAutostartConfigured reports whether Tokless owns a persistent launch
// agent, regardless of whether launchd currently has it loaded.
func ProxyAutostartConfigured() bool {
	raw, ok := util.ReadFileSafe(proxyAutostartPlistPath())
	bin := util.ToklessAbs()
	return ok && bin != "" && !util.IsGoTestExecutable(bin) && raw == proxyAutostartPlistBody(bin)
}
