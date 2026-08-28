//go:build linux

package headroom

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

const proxyAutostartUnit = "tokless-proxy.service"

func proxyAutostartUnitPath() string {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(util.Home(), ".config")
	}
	return filepath.Join(cfg, "systemd", "user", proxyAutostartUnit)
}

func proxyAutostartUnitBody(toklessBin string) string {
	q := shellQuote(toklessBin)
	return fmt.Sprintf(`[Unit]
Description=tokless headroom proxy (compression + BYOK route)
# tokless-managed
After=default.target

[Service]
Type=simple
ExecStart=%[1]s __proxy-run
Restart=on-failure

[Install]
WantedBy=default.target
`, q)
}

func shellQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '%':
			b.WriteString("%%")
			continue
		case '\\', '"', '$', '`':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func EnableProxyAutostart() (err error) {
	bin := util.ToklessAbs()
	if bin == "" || util.IsGoTestExecutable(bin) {
		return nil
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		if isWSL() {
			return fmt.Errorf("WSL proxy autostart requires systemd user services; enable systemd in /etc/wsl.conf")
		}
		return fmt.Errorf("systemctl not found; keeping proxy running for this session")
	}
	if _, err := exec.Command("systemctl", "--user", "show-environment").Output(); err != nil {
		if isWSL() {
			return fmt.Errorf("WSL proxy autostart requires a running systemd user bus; enable systemd in /etc/wsl.conf")
		}
		return fmt.Errorf("systemd user bus unavailable; keeping proxy running for this session")
	}
	path := proxyAutostartUnitPath()
	oldUnit, oldUnitExists := util.ReadFileSafe(path)
	if raw, ok := util.ReadFileSafe(path); ok && !strings.Contains(raw, "tokless-managed") {
		return fmt.Errorf("refusing to replace non-tokless systemd unit %s", path)
	}
	user := os.Getenv("USER")
	if user == "" {
		return fmt.Errorf("cannot enable linger: USER is not set")
	}
	if _, err := exec.LookPath("loginctl"); err != nil {
		return fmt.Errorf("loginctl not found; cannot enable proxy linger")
	}
	lingerWasEnabled := loginctlLinger(user)
	oldEnabled, oldActive := false, false
	if oldUnitExists {
		oldEnabled = systemdUserState("is-enabled")
		oldActive = systemdUserState("is-active")
	}
	mutated := false
	committed := false
	defer func() {
		if committed || !mutated {
			return
		}
		var rollbackErrs []error
		if rollbackErr := exec.Command("systemctl", "--user", "disable", "--now", proxyAutostartUnit).Run(); rollbackErr != nil {
			rollbackErrs = append(rollbackErrs, rollbackErr)
		}
		if oldUnitExists {
			if rollbackErr := util.WriteFile(path, oldUnit); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
		} else {
			if rollbackErr := os.Remove(path); rollbackErr != nil && !os.IsNotExist(rollbackErr) {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
		}
		if rollbackErr := exec.Command("systemctl", "--user", "daemon-reload").Run(); rollbackErr != nil {
			rollbackErrs = append(rollbackErrs, rollbackErr)
		}
		if oldEnabled {
			if rollbackErr := exec.Command("systemctl", "--user", "enable", proxyAutostartUnit).Run(); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
		}
		if oldActive {
			if rollbackErr := exec.Command("systemctl", "--user", "start", proxyAutostartUnit).Run(); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
		}
		if !lingerWasEnabled {
			if rollbackErr := exec.Command("loginctl", "disable-linger", user).Run(); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr)
			}
		}
		err = errors.Join(err, errors.Join(rollbackErrs...))
	}()
	body := proxyAutostartUnitBody(bin)
	if cur, ok := util.ReadFileSafe(path); !ok || cur != body {
		if err := util.EnsureDir(filepath.Dir(path)); err != nil {
			return err
		}
		if err := util.WriteFile(path, body); err != nil {
			return err
		}
		mutated = true
		if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
			return fmt.Errorf("reload systemd user units: %w", err)
		}
	}
	mutated = true
	if err := stopHeadroomDaemon(); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "--user", "enable", "--now", proxyAutostartUnit).Run(); err != nil {
		return fmt.Errorf("enable %s: %w", proxyAutostartUnit, err)
	}
	if !ProxyRunning() || !proxySupervisedArgsMatch(ProxyPort()) {
		return fmt.Errorf("%s enabled but proxy is not ready", proxyAutostartUnit)
	}
	if err := exec.Command("loginctl", "enable-linger", user).Run(); err != nil {
		return fmt.Errorf("enable user linger: %w", err)
	}
	committed = true
	return nil
}

func loginctlLinger(user string) bool {
	out, err := exec.Command("loginctl", "show-user", user, "-p", "Linger", "--value").Output()
	return err == nil && strings.TrimSpace(string(out)) == "yes"
}

func systemdUserState(action string) bool {
	out, err := exec.Command("systemctl", "--user", action, proxyAutostartUnit).Output()
	return err == nil && strings.TrimSpace(string(out)) == map[string]string{"is-enabled": "enabled", "is-active": "active"}[action]
}

func DisableProxyAutostart() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}
	path := proxyAutostartUnitPath()
	raw, ok := util.ReadFileSafe(path)
	if !ok || !strings.Contains(raw, "tokless-managed") {
		return nil
	}
	wasEnabled, wasActive := systemdUserState("is-enabled"), systemdUserState("is-active")
	rollback := func() error {
		var errs []error
		if err := util.WriteFile(path, raw); err != nil {
			errs = append(errs, err)
		}
		if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
			errs = append(errs, err)
		}
		if wasEnabled {
			if err := exec.Command("systemctl", "--user", "enable", proxyAutostartUnit).Run(); err != nil {
				errs = append(errs, err)
			}
		}
		if wasActive {
			if err := exec.Command("systemctl", "--user", "start", proxyAutostartUnit).Run(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	if err := exec.Command("systemctl", "--user", "disable", "--now", proxyAutostartUnit).Run(); err != nil {
		return errors.Join(fmt.Errorf("disable %s: %w", proxyAutostartUnit, err), rollback())
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return errors.Join(err, rollback())
	}
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return errors.Join(fmt.Errorf("reload systemd user units: %w", err), rollback())
	}
	return nil
}

func isWSL() bool {
	if os.Getenv("WSL_INTEROP") != "" || os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

func ProxyAutostartEnabled() bool {
	path := proxyAutostartUnitPath()
	raw, ok := util.ReadFileSafe(path)
	if !ok || !strings.Contains(raw, "__proxy-run") {
		return false
	}
	out, err := exec.Command("systemctl", "--user", "is-enabled", proxyAutostartUnit).Output()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(out))
	return s == "enabled" || s == "static" || s == "indirect"
}
