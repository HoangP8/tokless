package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func setOmpTestHome(t *testing.T) {
	t.Helper()
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func TestOmpPathsAndManifest(t *testing.T) {
	setOmpTestHome(t)
	want := filepath.Join(util.Home(), ".omp", "agent")
	if ompAgentDir() != want || ompMcpFile() != filepath.Join(want, "mcp.json") || ompExtensionsDir() != filepath.Join(want, "extensions") {
		t.Fatal("wrong OMP paths")
	}
	a := core.GetAgent("omp")
	if a == nil || a.Label != "Oh My Pi" || a.CLIBin != "omp" || a.Homepage != "https://github.com/can1357/oh-my-pi" {
		t.Fatal("wrong OMP manifest")
	}
}

func TestOmpAgentDirPrecedenceAndProfile(t *testing.T) {
	setOmpTestHome(t)
	t.Setenv("PI_CODING_AGENT_DIR", "~/pi-agent")
	t.Setenv("PI_CONFIG_DIR", "pi-config")
	t.Setenv("OMP_PROFILE", "work")
	if got, want := ompAgentDir(), filepath.Join(util.Home(), "pi-config", "profiles", "work", "agent"); got != want {
		t.Fatalf("named profile must override PI_CODING_AGENT_DIR: %q", got)
	}
	t.Setenv("OMP_PROFILE", "default")
	if got, want := ompAgentDir(), filepath.Join(util.Home(), "pi-agent"); got != want {
		t.Fatalf("default profile uses PI_CODING_AGENT_DIR: %q", got)
	}
	t.Setenv("PI_CODING_AGENT_DIR", "")
	if got, want := ompAgentDir(), filepath.Join(util.Home(), "pi-config", "agent"); got != want {
		t.Fatalf("default profile path: %q", got)
	}
	t.Setenv("OMP_PROFILE", "")
	t.Setenv("PI_PROFILE", "personal")
	if got, want := ompAgentDir(), filepath.Join(util.Home(), "pi-config", "agent"); got != want {
		t.Fatalf("set empty OMP_PROFILE must override PI_PROFILE: %q", got)
	}
	t.Setenv("OMP_PROFILE", "personal")
	if got, want := ompAgentDir(), filepath.Join(util.Home(), "pi-config", "profiles", "personal", "agent"); got != want {
		t.Fatalf("PI_PROFILE: %q", got)
	}
	if err := os.Unsetenv("OMP_PROFILE"); err != nil {
		t.Fatal(err)
	}
	if got, want := ompAgentDir(), filepath.Join(util.Home(), "pi-config", "profiles", "personal", "agent"); got != want {
		t.Fatalf("PI_PROFILE fallback: %q", got)
	}
	t.Setenv("OMP_PROFILE", "../unsafe")
	if got, want := ompAgentDir(), filepath.Join(util.Home(), "pi-config", "agent"); got != want {
		t.Fatalf("invalid profile must use default: %q", got)
	}
	t.Setenv("OMP_PROFILE", "default")
	t.Setenv("PI_CONFIG_DIR", ".config/omp")
	if got, want := ompAgentDir(), filepath.Join(util.Home(), ".config", "omp", "agent"); got != want {
		t.Fatalf("nested config root: %q", got)
	}
	t.Setenv("PI_CONFIG_DIR", "/opt/omp")
	if got, want := ompAgentDir(), filepath.Join("/opt/omp", "agent"); got != want {
		t.Fatalf("absolute config root: %q", got)
	}
	t.Setenv("PI_CONFIG_DIR", "~/.config/omp")
	if got, want := ompAgentDir(), filepath.Join(util.Home(), ".config", "omp", "agent"); got != want {
		t.Fatalf("tilde config root: %q", got)
	}
}

func TestConfigureOmpMcpPreservesConfigAndBoundsContextMode(t *testing.T) {
	setOmpTestHome(t)
	_ = util.EnsureDir(ompAgentDir())
	_ = util.WriteFile(ompMcpFile(), `{"keep":true,"mcpServers":{"user":{"command":"keep"}}}`)
	if changed, _ := ConfigureOmpMcp("codegraph"); !changed {
		t.Fatal("codegraph not configured")
	}
	if changed, _ := ConfigureOmpMcp("codegraph"); changed {
		t.Fatal("codegraph not idempotent")
	}
	if changed, _ := ConfigureOmpMcp("context-mode"); !changed {
		t.Fatal("context-mode not configured")
	}
	raw, _ := util.ReadFileSafe(ompMcpFile())
	for _, want := range []string{"\"keep\"", "\"user\"", "\"type\": \"stdio\"", "run-mcp", "--agent", "omp", "--context-mode"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing %q: %s", want, raw)
		}
	}
	if strings.Contains(raw, "enabledTools") {
		t.Fatalf("OMP MCP schema must not contain enabledTools: %s", raw)
	}
	cfg := util.TryParseJsonc(raw)
	servers, _ := cfg.Get("mcpServers")
	codegraph, _ := servers.(*util.OrderedMap).Get("codegraph")
	contextMode, _ := servers.(*util.OrderedMap).Get("context-mode")
	for _, tc := range []struct {
		name  string
		entry any
		want  []string
	}{
		{"codegraph", codegraph, []string{"run-mcp", "--agent", "omp"}},
		{"context-mode", contextMode, []string{"run-mcp", "--context-mode"}},
	} {
		em := tc.entry.(*util.OrderedMap)
		args, _ := em.Get("args")
		got, ok := ompStrings(args)
		if !ok {
			t.Fatalf("%s invalid proxy args", tc.name)
		}
		if len(got) < len(tc.want) {
			t.Fatalf("%s proxy args: %v", tc.name, got)
		}
		for i, want := range tc.want {
			if got[i] != want {
				t.Fatalf("%s proxy args: %v", tc.name, got)
			}
		}
	}
	if !RemoveOmpMcp("codegraph") || OmpMcpHas("codegraph") || !OmpMcpHas("context-mode") {
		t.Fatal("surgical removal failed")
	}
}

func TestOmpProfileRejectsWindowsReservedNames(t *testing.T) {
	setOmpTestHome(t)
	for _, profile := range []string{"work.", "CON", "prn.md", "AUX", "NUL.txt", "COM0", "com0.json", "COM1", "com9.json", "LPT0", "lpt0.cfg", "LPT1", "lpt9.cfg"} {
		t.Run(profile, func(t *testing.T) {
			t.Setenv("OMP_PROFILE", profile)
			if got := ompProfile(); got != "" {
				t.Fatalf("invalid profile %q accepted as %q", profile, got)
			}
		})
	}
}

func TestOmpMcpRepairsDisabledManagedEntry(t *testing.T) {
	setOmpTestHome(t)
	if changed, _ := ConfigureOmpMcp("context-mode"); !changed {
		t.Fatal("configure")
	}
	raw, _ := util.ReadFileSafe(ompMcpFile())
	cfg := util.TryParseJsonc(raw)
	servers, _ := cfg.Get("mcpServers")
	entry, _ := servers.(*util.OrderedMap).Get("context-mode")
	entry.(*util.OrderedMap).Set("enabled", false)
	_ = util.WriteFile(ompMcpFile(), util.StringifyJSON(cfg))
	if OmpMcpHas("context-mode") {
		t.Fatal("disabled entry must not verify")
	}
	if changed, _ := ConfigureOmpMcp("context-mode"); !changed || !OmpMcpHas("context-mode") {
		t.Fatal("disabled managed entry not repaired")
	}
}

func TestOmpMcpPreservesMalformedArgs(t *testing.T) {
	setOmpTestHome(t)
	_ = util.EnsureDir(ompAgentDir())
	raw := `{"mcpServers":{"codegraph":{"type":"stdio","command":"tokless","args":["run-mcp","--agent","omp",7,"serve","--mcp"]}}}`
	_ = util.WriteFile(ompMcpFile(), raw)
	if changed, _ := ConfigureOmpMcp("codegraph"); changed {
		t.Fatal("must not overwrite malformed user target")
	}
	if RemoveOmpMcp("codegraph") {
		t.Fatal("must not remove malformed user target")
	}
	got, _ := util.ReadFileSafe(ompMcpFile())
	if got != raw {
		t.Fatalf("malformed user target changed: %s", got)
	}
}

func TestOmpMcpPreservesUserServer(t *testing.T) {
	setOmpTestHome(t)
	_ = util.EnsureDir(ompAgentDir())
	user := `{"type":"stdio","command":"user-server","args":["x"]}`
	_ = util.WriteFile(ompMcpFile(), `{"mcpServers":{"codegraph":`+user+`}}`)
	if changed, _ := ConfigureOmpMcp("codegraph"); changed {
		t.Fatal("must not overwrite user server")
	}
	if RemoveOmpMcp("codegraph") {
		t.Fatal("must not remove user server")
	}
	raw, _ := util.ReadFileSafe(ompMcpFile())
	if !strings.Contains(raw, "user-server") {
		t.Fatalf("user server lost: %s", raw)
	}
}

func TestOmpMcpPreservesGenericToklessCollision(t *testing.T) {
	setOmpTestHome(t)
	_ = util.EnsureDir(ompAgentDir())
	for _, tc := range []struct {
		name   string
		toolID string
		args   string
	}{
		{"codegraph", "codegraph", `["run-mcp","--agent","omp","other-server"]`},
		{"context-mode", "context-mode", `["run-mcp","--context-mode","other-server"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = util.WriteFile(ompMcpFile(), `{"mcpServers":{"`+tc.toolID+`":{"type":"stdio","command":"tokless","args":`+tc.args+`}}}`)
			if changed, _ := ConfigureOmpMcp(tc.toolID); changed {
				t.Fatal("must not overwrite generic tokless collision")
			}
			if RemoveOmpMcp(tc.toolID) {
				t.Fatal("must not remove generic tokless collision")
			}
		})
	}
}
