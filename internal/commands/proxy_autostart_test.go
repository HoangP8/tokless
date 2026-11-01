package commands

import (
	"fmt"
	"os"
	"strings"
	"testing"

	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

type autostartCalls struct{ start, disable, enable int }

// stubAutostart swaps the proxy lifecycle seams and counts calls.
func stubAutostart(t *testing.T, running, configured, wired bool) *autostartCalls {
	t.Helper()
	proxyCmdTestHome(t)
	c := &autostartCalls{}
	oldStart, oldDisable, oldEnable := startProxy, disableProxyAutostart, enableProxyAutostart
	oldRunning, oldWired := proxyRunning, proxyAgentWired
	t.Cleanup(func() {
		startProxy, disableProxyAutostart, enableProxyAutostart = oldStart, oldDisable, oldEnable
		proxyRunning, proxyAgentWired = oldRunning, oldWired
	})
	startProxy = func() error { c.start++; return nil }
	disableProxyAutostart = func() error { c.disable++; return nil }
	enableProxyAutostart = func() error { c.enable++; return nil }
	proxyRunning = func() bool { return running }
	proxyAutostartConfigured = func() bool { return configured }
	proxyAgentWired = func(id string) bool { return wired && id == "claude" }
	return c
}

func TestRunProxyAutostartOff(t *testing.T) {
	cases := []struct {
		name      string
		running   bool
		wantStart int
	}{
		{"running proxy stays up for this session", true, 1},
		{"stopped proxy stays stopped", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := stubAutostart(t, tc.running, true, true)
			if got := RunProxyAutostart(false, InitOptions{}); got != 0 {
				t.Fatalf("exit = %d, want 0", got)
			}
			if util.ProxyAutostartAllowed() {
				t.Fatal("preference still allowed")
			}
			if c.disable != 1 || c.start != tc.wantStart || c.enable != 0 {
				t.Fatalf("calls = %+v, want disable=1 start=%d enable=0", *c, tc.wantStart)
			}
		})
	}
}

func TestRunProxyAutostartOffFailureRestoresPreference(t *testing.T) {
	t.Run("disable fails", func(t *testing.T) {
		c := stubAutostart(t, true, true, true)
		disableProxyAutostart = func() error { c.disable++; return fmt.Errorf("boom") }
		if got := RunProxyAutostart(false, InitOptions{}); got != 1 {
			t.Fatalf("exit = %d, want 1", got)
		}
		if !util.ProxyAutostartAllowed() || c.start != 0 {
			t.Fatalf("preference not restored or proxy restarted: %+v", *c)
		}
	})
}

func TestRunProxyAutostartOffRestartFailureKeepsPreferenceOff(t *testing.T) {
	c := stubAutostart(t, true, true, true)
	startProxy = func() error { c.start++; return fmt.Errorf("boom") }
	logs, err := util.CaptureLogs(func() error {
		if got := RunProxyAutostart(false, InitOptions{}); got != 0 {
			return fmt.Errorf("exit = %d, want 0", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if util.ProxyAutostartAllowed() || c.enable != 0 || c.start != 1 {
		t.Fatalf("restart failure must keep preference off and not re-enable: allowed=%v %+v", util.ProxyAutostartAllowed(), *c)
	}
	if !strings.Contains(logs, "Proxy stopped. Run: tokless proxy up") {
		t.Fatalf("missing stopped warning: %q", logs)
	}
}

func TestRunProxyAutostartOn(t *testing.T) {
	cases := []struct {
		name       string
		wired      bool
		enableErr  error
		wantExit   int
		wantEnable int
		wantAllow  bool
	}{
		{"wired registers login entry", true, nil, 0, 1, true},
		{"nothing wired only records preference", false, nil, 0, 0, true},
		{"enable failure restores off", true, fmt.Errorf("boom"), 1, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := stubAutostart(t, false, false, tc.wired)
			if err := util.SetProxyAutostartAllowed(false); err != nil {
				t.Fatal(err)
			}
			enableProxyAutostart = func() error { c.enable++; return tc.enableErr }
			if got := RunProxyAutostart(true, InitOptions{}); got != tc.wantExit {
				t.Fatalf("exit = %d, want %d", got, tc.wantExit)
			}
			if c.enable != tc.wantEnable || util.ProxyAutostartAllowed() != tc.wantAllow {
				t.Fatalf("enable=%d allowed=%v, want %d/%v", c.enable, util.ProxyAutostartAllowed(), tc.wantEnable, tc.wantAllow)
			}
		})
	}
}

func TestRunProxyAutostartDryRunChangesNothing(t *testing.T) {
	for _, on := range []bool{true, false} {
		c := stubAutostart(t, true, true, true)
		if got := RunProxyAutostart(on, InitOptions{DryRun: true}); got != 0 {
			t.Fatalf("exit = %d, want 0", got)
		}
		if *c != (autostartCalls{}) || !util.ProxyAutostartAllowed() {
			t.Fatalf("dry-run changed state: %+v", *c)
		}
	}
}

func TestSetProxyAutostartOnSkipsWhenRoutingDisabled(t *testing.T) {
	c := stubAutostart(t, false, false, true)
	if err := util.SetProxyRoutingEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := util.SetProxyAutostartAllowed(false); err != nil {
		t.Fatal(err)
	}
	applied, err := setProxyAutostartOn()
	if err != nil || applied {
		t.Fatalf("applied=%v err=%v, want false/nil", applied, err)
	}
	if !util.ProxyAutostartAllowed() || c.enable != 0 {
		t.Fatalf("preference must be on without registering: allowed=%v %+v", util.ProxyAutostartAllowed(), *c)
	}
}

func TestRunProxyStatusStartsAtLoginBranches(t *testing.T) {
	cases := []struct {
		name       string
		allowed    bool
		configured bool
		legacy     bool // legacy-template entry: enabled but not "configured"
		want       string
	}{
		{"on", true, true, false, "Starts at login: on (" + headroompkg.ProxyAutostartMechanism() + ")"},
		{"on via legacy entry", true, false, true, "Starts at login: on (" + headroompkg.ProxyAutostartMechanism() + ")"},
		{"off", false, false, false, "Starts at login: off (turn on: tokless proxy autostart on)"},
		{"off but configured", false, true, false, "although you turned it off. Fix: tokless proxy autostart off"},
		{"off but legacy entry", false, false, true, "although you turned it off. Fix: tokless proxy autostart off"},
		{"never set up", true, false, false, "Starts at login: off (run: tokless proxy up)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubAutostart(t, false, tc.configured, false)
			oldEnabled := proxyAutostartEnabled
			t.Cleanup(func() { proxyAutostartEnabled = oldEnabled })
			proxyAutostartEnabled = func() bool { return tc.legacy }
			if err := util.SetProxyAutostartAllowed(tc.allowed); err != nil {
				t.Fatal(err)
			}
			logs, err := util.CaptureLogs(func() error {
				RunProxyStatus(InitOptions{Agents: []string{"claude"}})
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(logs, tc.want) {
				t.Fatalf("status = %q, want %q", logs, tc.want)
			}
		})
	}
}

func TestRunProxyDownDisablesConfiguredButStoppedLoginEntry(t *testing.T) {
	c := stubAutostart(t, false, true, false)
	oldStop := stopProxy
	t.Cleanup(func() { stopProxy = oldStop })
	stopProxy = func() error { return nil }
	if got := RunProxyDown(InitOptions{}); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if c.disable != 1 {
		t.Fatalf("disable calls = %d, want 1", c.disable)
	}
}

func seedNoAutostartInit(t *testing.T) *autostartCalls {
	t.Helper()
	seedInitEnv(t)
	c := stubAutostart(t, false, false, false)
	seedAgentInstalled(t, "claude")
	return c
}

func TestRunInitNoAutostart(t *testing.T) {
	opts := InitOptions{Agents: []string{"claude"}, Tools: []string{"principles"}, NoAutostart: true}
	t.Run("turns preference off and removes entry", func(t *testing.T) {
		c := seedNoAutostartInit(t)
		if code := RunInit(opts); code != 0 {
			t.Fatalf("RunInit = %d, want 0", code)
		}
		if util.ProxyAutostartAllowed() || c.disable != 1 {
			t.Fatalf("allowed=%v %+v, want off with one disable", util.ProxyAutostartAllowed(), *c)
		}
	})
	t.Run("disable failure stops before wiring", func(t *testing.T) {
		c := seedNoAutostartInit(t)
		disableProxyAutostart = func() error { c.disable++; return fmt.Errorf("boom") }
		if code := RunInit(opts); code != 1 {
			t.Fatalf("RunInit = %d, want 1", code)
		}
		if !util.ProxyAutostartAllowed() {
			t.Fatal("preference not restored after disable failure")
		}
		if _, err := os.Stat(util.ClaudeCodePaths().Instructions); err == nil {
			t.Fatal("tools were wired despite --no-autostart failure")
		}
	})
	t.Run("restart failure does not fail init", func(t *testing.T) {
		c := seedNoAutostartInit(t)
		proxyRunning = func() bool { return true }
		startProxy = func() error { c.start++; return fmt.Errorf("boom") }
		if code := RunInit(opts); code != 0 {
			t.Fatalf("RunInit = %d, want 0", code)
		}
		if util.ProxyAutostartAllowed() || c.start != 1 || c.enable != 0 {
			t.Fatalf("allowed=%v %+v", util.ProxyAutostartAllowed(), *c)
		}
	})
}

func TestRunInitStartsAtLoginDisclosure(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		seedNoAutostartInit(t)
		proxyAutostartConfigured = func() bool { return true }
		if err := util.SetProxyAutostartAllowed(allowed); err != nil {
			t.Fatal(err)
		}
		logs, err := util.CaptureLogs(func() error {
			if code := RunInit(InitOptions{Agents: []string{"claude"}, Tools: []string{"principles"}}); code != 0 {
				return fmt.Errorf("RunInit = %d", code)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(logs, "Turn off: tokless proxy autostart off"); got != allowed {
			t.Fatalf("allowed=%v: disclosure printed=%v\n%s", allowed, got, logs)
		}
	}
}
