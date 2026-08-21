//go:build linux

package headroom

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	q := strconv.Quote(toklessBin)
	return fmt.Sprintf(`[Unit]
Description=tokless headroom proxy (compression + BYOK route)
After=default.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=%s __proxy-ensure
ExecStop=%s __proxy-stop

[Install]
WantedBy=default.target
`, q, q)
}

func EnableProxyAutostart() error {
	bin := util.ToklessAbs()
	if bin == "" || util.IsGoTestExecutable(bin) {
		return nil
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}
	path := proxyAutostartUnitPath()
	body := proxyAutostartUnitBody(bin)
	if cur, ok := util.ReadFileSafe(path); !ok || cur != body {
		if err := util.EnsureDir(filepath.Dir(path)); err != nil {
			return err
		}
		if err := util.WriteFile(path, body); err != nil {
			return err
		}
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	}
	if err := exec.Command("systemctl", "--user", "enable", "--now", proxyAutostartUnit).Run(); err != nil {
		return fmt.Errorf("enable %s: %w", proxyAutostartUnit, err)
	}
	if u := os.Getenv("USER"); u != "" {
		_ = exec.Command("loginctl", "enable-linger", u).Run()
	}
	return nil
}

func DisableProxyAutostart() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}
	_ = exec.Command("systemctl", "--user", "disable", "--now", proxyAutostartUnit).Run()
	path := proxyAutostartUnitPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

func ProxyAutostartEnabled() bool {
	path := proxyAutostartUnitPath()
	raw, ok := util.ReadFileSafe(path)
	if !ok || !strings.Contains(raw, "__proxy-ensure") {
		return false
	}
	out, err := exec.Command("systemctl", "--user", "is-enabled", proxyAutostartUnit).Output()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(out))
	return s == "enabled" || s == "static" || s == "indirect"
}
