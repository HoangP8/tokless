package tools

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// TOKLESS_TEST pins ContextModeLatest to 1.0.169.
const ctxTestSpec = "context-mode@1.0.169"

const userMcp = `{"type":"local","command":["context-mode"],"enabled":true}`

func writeOC(t *testing.T, path, body string) {
	t.Helper()
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ocPath(v2 bool) string {
	if v2 {
		return util.OpenCodePathsV2().Config
	}
	return util.OpenCodePathsResolved().Config
}

func ocSetup(t *testing.T, v2 bool, body string) string {
	t.Helper()
	tmp := opencode2TestHome(t)
	if v2 {
		openCodeSeparateV2PluginsDir(t, tmp)
	}
	path := ocPath(v2)
	if body != "" {
		writeOC(t, path, strings.ReplaceAll(body, "@STALE@", staleMcpEntry(t)))
	}
	return path
}

func staleMcpEntry(t *testing.T) string {
	t.Helper()
	spawn := util.PickMcpSpawn("context-mode")
	return `{"type":"local","command":["` + spawn.Command + `","run-mcp","--context-mode"],"enabled":true}`
}

func ocCfg(t *testing.T, path string) *util.OrderedMap {
	t.Helper()
	cfg := util.TryParseJsonc(mustRead(t, path))
	if cfg == nil {
		t.Fatalf("parse %s:\n%s", path, mustRead(t, path))
	}
	return cfg
}

func ocStrings(cfg *util.OrderedMap, key string) []string {
	var out []string
	for _, p := range getArr(cfg, key) {
		if s, ok := p.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func ocPerm(t *testing.T, cfg *util.OrderedMap) map[string]any {
	t.Helper()
	v, ok := cfg.Get("permission")
	if !ok {
		return nil
	}
	om, ok := v.(*util.OrderedMap)
	if !ok {
		t.Fatalf("permission not an object: %v", v)
	}
	out := map[string]any{}
	for _, k := range om.Keys() {
		out[k], _ = om.Get(k)
	}
	return out
}

func allDeny() map[string]any {
	m := map[string]any{}
	for _, n := range agents.OpenCodeHiddenCtxTools {
		m[n] = "deny"
	}
	return m
}

func hasKey(cfg *util.OrderedMap, key string) bool {
	_, ok := cfg.Get(key)
	return ok
}

func TestCtxWireOpenCode(t *testing.T) {
	for _, tc := range []struct {
		name  string
		v2    bool
		in    string   // "" = no config file
		want  []string // plugin specs after wire, under the shape's key
		same  bool     // file must be byte-identical (already wired)
		check func(t *testing.T, cfg *util.OrderedMap)
	}{
		{"replaces pins, keeps others", false, `{
  "model": "x/y",
  "plugin": ["other@1.0.0", "context-mode@0.0.1", "oh-my-opencode@2.0.0", "context-mode"]
}`, []string{"other@1.0.0", ctxTestSpec, "oh-my-opencode@2.0.0"}, false, func(t *testing.T, cfg *util.OrderedMap) {
			if v, _ := cfg.Get("model"); v != "x/y" {
				t.Fatalf("model lost: %v", v)
			}
		}},
		{"new v1 config has $schema", false, "", []string{ctxTestSpec}, false, func(t *testing.T, cfg *util.OrderedMap) {
			if !hasKey(cfg, "$schema") {
				t.Fatal("missing $schema")
			}
		}},
		{"new v2 config has plugins, no $schema", true, "", []string{ctxTestSpec}, false, func(t *testing.T, cfg *util.OrderedMap) {
			if hasKey(cfg, "$schema") {
				t.Fatal("v2 config must not get the v1 $schema")
			}
		}},
		{"adds missing key", false, `{"model":"a/b"}`, []string{ctxTestSpec}, false, func(t *testing.T, cfg *util.OrderedMap) {
			if v, _ := cfg.Get("model"); v != "a/b" {
				t.Fatalf("model lost: %v", v)
			}
		}},
		{"fills empty array", false, `{"plugin":[]}`, []string{ctxTestSpec}, false, nil},
		{"v2 touches plugins only", true, `{"plugins":["keep@1","context-mode@0.9.0"],"plugin":["v1-only@1"]}`,
			[]string{"keep@1", ctxTestSpec}, false, func(t *testing.T, cfg *util.OrderedMap) {
				if got := getArr(cfg, "plugin"); !reflect.DeepEqual(got, []any{"v1-only@1"}) {
					t.Fatalf("v1 plugin key changed: %v", got)
				}
			}},
		{"removes stale tokless mcp (v1 root)", false, `{"mcp":{"context-mode":@STALE@,"codegraph":{"type":"local","command":["cg"]}}}`,
			[]string{ctxTestSpec}, false, func(t *testing.T, cfg *util.OrderedMap) {
				mcp, _ := cfg.Get("mcp")
				m := mcp.(*util.OrderedMap)
				if hasKey(m, "context-mode") || !hasKey(m, "codegraph") {
					t.Fatalf("mcp = %v", m.Keys())
				}
			}},
		{"removes stale tokless mcp (v2 servers)", false, `{"mcp":{"servers":{"context-mode":@STALE@,"codegraph":{"type":"local","command":["cg"]}}}}`,
			[]string{ctxTestSpec}, false, func(t *testing.T, cfg *util.OrderedMap) {
				if agents.OpenCodeToklessMcpHas(false, "context-mode") || !strings.Contains(util.StringifyJSON(cfg), "codegraph") {
					t.Fatalf("unexpected mcp: %s", util.StringifyJSON(cfg))
				}
			}},
		{"drops emptied mcp container", false, `{"model":"a/b","mcp":{"context-mode":@STALE@}}`,
			[]string{ctxTestSpec}, false, func(t *testing.T, cfg *util.OrderedMap) {
				if hasKey(cfg, "mcp") {
					t.Fatal("empty mcp object left behind")
				}
			}},
		{"keeps user-owned mcp entry", false, `{"mcp":{"context-mode":` + userMcp + `}}`,
			[]string{ctxTestSpec}, false, func(t *testing.T, cfg *util.OrderedMap) {
				if !agents.OpenCodeMcpHas(cfg, "context-mode") {
					t.Fatal("user-owned mcp entry removed")
				}
			}},
		{"commented config already wired is a no-op", false,
			"{\n  // keep me\n  \"plugin\": [\"" + ctxTestSpec + "\"],\n  \"permission\": {\"ctx_resume\":\"deny\",\"ctx_stats\":\"deny\",\"ctx_doctor\":\"deny\",\"ctx_upgrade\":\"deny\",\"ctx_purge\":\"deny\"}\n}\n",
			[]string{ctxTestSpec}, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := ocSetup(t, tc.v2, tc.in)
			before := ""
			if tc.in != "" {
				before = mustRead(t, path)
			}
			if ok, err := ctxWireOpenCodeShape(core.RunOpts{}, tc.v2); err != nil || !ok {
				t.Fatalf("wire ok=%v err=%v", ok, err)
			}
			first := mustRead(t, path)
			if tc.same && first != before {
				t.Fatalf("file rewritten:\n%s", first)
			}
			cfg := ocCfg(t, path)
			if got := ocStrings(cfg, agents.OpenCodePluginKey(tc.v2)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("plugins = %v, want %v", got, tc.want)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
			if !ctxVerifyOpenCodeShape(tc.v2) || !ctxWiredAnyOpenCodeShape(tc.v2) {
				t.Fatal("verify / wiredAny must hold after wire")
			}
			if ok, err := ctxWireOpenCodeShape(core.RunOpts{}, tc.v2); err != nil || !ok || mustRead(t, path) != first {
				t.Fatalf("second wire not idempotent: ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestCtxWireOpenCodePermission(t *testing.T) {
	for _, tc := range []struct {
		name string
		v2   bool
		in   string
		want map[string]any
		raw  any // non-object permission left untouched
	}{
		{"v1 adds deny block", false, "", allDeny(), nil},
		{"v2 adds deny block", true, "", allDeny(), nil},
		{"keeps user values", false, `{"permission":{"bash":"ask","ctx_stats":"allow"}}`,
			map[string]any{"bash": "ask", "ctx_stats": "allow", "ctx_doctor": "deny", "ctx_upgrade": "deny", "ctx_purge": "deny", "ctx_resume": "deny"}, nil},
		{"non-object permission untouched", false, `{"permission":"ask"}`, nil, "ask"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := ocSetup(t, tc.v2, tc.in)
			if ok, err := ctxWireOpenCodeShape(core.RunOpts{}, tc.v2); err != nil || !ok {
				t.Fatalf("wire ok=%v err=%v", ok, err)
			}
			cfg := ocCfg(t, path)
			if tc.raw != nil {
				if v, _ := cfg.Get("permission"); v != tc.raw || agents.OpenCodeHidingMissing(tc.v2, agents.OpenCodeHiddenCtxTools) {
					t.Fatalf("permission = %v", v)
				}
				return
			}
			if got := ocPerm(t, cfg); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("permission = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCtxWireOpenCodeRefuses(t *testing.T) {
	for _, tc := range []struct{ name, in, errHas string }{
		{"commented config needing the plugin", "{\n  // keep me\n  \"plugin\": [\"other@1\"]\n}\n", "comments"},
		{"commented config needing the deny block", "{\n  // mine\n  \"plugin\": [\"" + ctxTestSpec + "\"]\n}\n", "comments"},
		{"commented config with duplicate tokless mcp entry", "{\n  // keep me\n  \"mcp\": {\"context-mode\": @STALE@}\n}\n", `remove the tokless-owned "context-mode" entry under "mcp"`},
		{"non-array plugin", `{"plugin":"oops"}`, ""},
		{"unparseable config", `{ this is not json`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := ocSetup(t, false, tc.in)
			ok, err := ctxWireOpenCodeShape(core.RunOpts{}, false)
			if ok || err == nil || !strings.Contains(err.Error(), tc.errHas) {
				t.Fatalf("must refuse: ok=%v err=%v", ok, err)
			}
			if mustRead(t, path) != strings.ReplaceAll(tc.in, "@STALE@", staleMcpEntry(t)) {
				t.Fatal("config was modified")
			}
		})
	}
}

func TestCtxUnwireOpenCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		v2     bool
		owned  bool
		in     string
		errHas string // non-empty: unwire must fail with this (empty err text = any error)
		fail   bool
		check  func(t *testing.T, cfg *util.OrderedMap)
	}{
		{name: "removes only context-mode specs", owned: true, in: `{"plugin":["context-mode","oh-my-opencode@1.1.1","context-mode@2.0.2","context-mode-helper@1",["context-mode-tuple",{"a":1}]]}`,
			check: func(t *testing.T, cfg *util.OrderedMap) {
				if got := ocStrings(cfg, "plugin"); !reflect.DeepEqual(got, []string{"oh-my-opencode@1.1.1", "context-mode-helper@1"}) || len(getArr(cfg, "plugin")) != 3 {
					t.Fatalf("plugin = %v", getArr(cfg, "plugin"))
				}
				if ctxWiredAnyOpenCodeShape(false) {
					t.Fatal("still wired")
				}
			}},
		{name: "drops empty plugin key and owned mcp", owned: true, in: `{"model":"a/b","plugin":["context-mode@2.0.2"],"mcp":{"context-mode":@STALE@}}`,
			check: func(t *testing.T, cfg *util.OrderedMap) {
				if v, _ := cfg.Get("model"); hasKey(cfg, "plugin") || hasKey(cfg, "mcp") || v != "a/b" {
					t.Fatalf("unexpected config: %s", util.StringifyJSON(cfg))
				}
			}},
		{name: "v2 leaves the v1 key", v2: true, owned: true, in: `{"plugins":["keep@1","context-mode@1.0.169"],"plugin":["v1-only@1"]}`,
			check: func(t *testing.T, cfg *util.OrderedMap) {
				if !reflect.DeepEqual(ocStrings(cfg, "plugins"), []string{"keep@1"}) || !reflect.DeepEqual(ocStrings(cfg, "plugin"), []string{"v1-only@1"}) {
					t.Fatalf("unexpected config: %s", util.StringifyJSON(cfg))
				}
			}},
		{name: "keeps user-owned spec", in: `{"plugin":["context-mode","other@1"]}`,
			check: func(t *testing.T, cfg *util.OrderedMap) {
				if got := ocStrings(cfg, "plugin"); !reflect.DeepEqual(got, []string{"context-mode", "other@1"}) {
					t.Fatalf("plugin = %v", got)
				}
			}},
		{name: "user-owned spec stays, stale tokless mcp goes", in: `{"plugin":["context-mode"],"mcp":{"context-mode":@STALE@}}`,
			check: func(t *testing.T, cfg *util.OrderedMap) {
				if agents.OpenCodeToklessMcpHas(false, "context-mode") || !reflect.DeepEqual(ocStrings(cfg, "plugin"), []string{"context-mode"}) {
					t.Fatalf("unexpected config: %s", util.StringifyJSON(cfg))
				}
			}},
		{name: "keeps user-owned mcp entry", in: `{"mcp":{"context-mode":` + userMcp + `}}`,
			check: func(t *testing.T, cfg *util.OrderedMap) {
				if !agents.OpenCodeMcpHas(cfg, "context-mode") {
					t.Fatal("user-owned mcp entry removed")
				}
			}},
		{name: "removes only deny keys", owned: true, in: `{"plugin":["context-mode@2.0.2"],"permission":{"ctx_stats":"deny","ctx_doctor":"allow","ctx_upgrade":"deny","bash":"ask"}}`,
			check: func(t *testing.T, cfg *util.OrderedMap) {
				if got := ocPerm(t, cfg); !reflect.DeepEqual(got, map[string]any{"ctx_doctor": "allow", "bash": "ask"}) {
					t.Fatalf("permission = %v", got)
				}
			}},
		{name: "drops emptied permission", owned: true, in: `{"plugin":["context-mode@2.0.2"],"permission":{"ctx_stats":"deny","ctx_purge":"deny"}}`,
			check: func(t *testing.T, cfg *util.OrderedMap) {
				if hasKey(cfg, "permission") {
					t.Fatal("empty permission left")
				}
			}},
		{name: "keeps user-owned deny keys", in: `{"plugin":["context-mode"],"permission":{"ctx_stats":"deny"}}`,
			check: func(t *testing.T, cfg *util.OrderedMap) {
				if got := ocPerm(t, cfg); got["ctx_stats"] != "deny" {
					t.Fatalf("permission = %v", got)
				}
			}},
		{name: "refuses non-array plugin", owned: true, in: `{"plugin":"oops"}`, fail: true},
		{name: "commented config error says remove", owned: true, in: "{\n  // note\n  \"plugin\": [\"context-mode@1.0.169\"]\n}\n", fail: true, errHas: "remove"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := ocSetup(t, tc.v2, tc.in)
			before := mustRead(t, path)
			if tc.owned {
				WriteOwner(ctxOpenCodeAgentID(tc.v2), "context-mode")
			}
			ok, err := ctxUnwireOpenCodeShape(core.RunOpts{}, tc.v2)
			if tc.fail {
				if ok || err == nil || !strings.Contains(err.Error(), tc.errHas) || strings.Contains(err.Error(), `add ""`) {
					t.Fatalf("must refuse: ok=%v err=%v", ok, err)
				}
				if mustRead(t, path) != before {
					t.Fatal("config was modified")
				}
				return
			}
			if err != nil || !ok {
				t.Fatalf("unwire ok=%v err=%v", ok, err)
			}
			tc.check(t, ocCfg(t, path))
		})
	}
}

func TestCtxOpenCodeWiredStates(t *testing.T) {
	for _, tc := range []struct {
		name         string
		in           string
		owned        bool
		wired, valid bool
	}{
		{"user-owned mcp", `{"mcp":{"context-mode":` + userMcp + `}}`, false, false, false},
		{"tokless mcp only", `{"mcp":{"context-mode":@STALE@}}`, false, true, false},
		{"owned pinned plugin", `{"plugin":["` + ctxTestSpec + `"]}`, true, true, true},
		{"user-owned plugin", `{"plugin":["` + ctxTestSpec + `"]}`, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ocSetup(t, false, tc.in)
			if tc.owned {
				WriteOwner("opencode", "context-mode")
			}
			if got := ctxWiredAnyOpenCode(); got != tc.wired {
				t.Fatalf("wiredAny = %v", got)
			}
			if got := ctxVerifyOpenCode(); got != tc.valid {
				t.Fatalf("verify = %v", got)
			}
		})
	}
}

func TestCtxOpenCodePinStale(t *testing.T) {
	for _, tc := range []struct {
		name, in, latest string
		owned, stale     bool
	}{
		{"user-owned never stale", `{"plugin":["context-mode@1.0.0"]}`, "1.0.169", false, false},
		{"older pin", `{"plugin":["context-mode@1.0.0"]}`, "1.0.169", true, true},
		{"equal pin", `{"plugin":["context-mode@1.0.0"]}`, "1.0.0", true, false},
		{"unpinned", `{"plugin":["context-mode","other@1"]}`, "1.0.169", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ocSetup(t, false, tc.in)
			if tc.owned {
				WriteOwner("opencode", "context-mode")
			}
			if got := CtxOpenCodePinStale(tc.latest); got != tc.stale {
				t.Fatalf("stale = %v", got)
			}
		})
	}
}

func TestCtxOpenCodeHideStale(t *testing.T) {
	ocSetup(t, false, `{"plugin":["context-mode@2.0.2"]}`)
	if CtxOpenCodeHideStale() {
		t.Fatal("user-owned plugin must never be stale")
	}
	WriteOwner("opencode", "context-mode")
	if !CtxOpenCodeHideStale() {
		t.Fatal("missing deny block must be stale")
	}
	if _, err := ctxWireOpenCode(core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if CtxOpenCodeHideStale() {
		t.Fatal("wired config must not be stale")
	}
}

func TestCtxOpenCodeHideStaleOldDenyBlock(t *testing.T) {
	path := ocSetup(t, false, `{"plugin":["context-mode@2.0.2"],"permission":{"ctx_stats":"deny","ctx_doctor":"deny","ctx_upgrade":"deny","ctx_purge":"deny"}}`)
	WriteOwner("opencode", "context-mode")
	if !CtxOpenCodeHideStale() {
		t.Fatal("old 4-key deny block must be stale")
	}
	if _, err := ctxWireOpenCode(core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if CtxOpenCodeHideStale() || ocPerm(t, ocCfg(t, path))["ctx_resume"] != "deny" {
		t.Fatal("ctx_resume deny not added")
	}
}

func TestCtxCLIReadyAndGate(t *testing.T) {
	t.Cleanup(func() { ctxResolveRuntime, ctxResolveLatest = nil, nil })
	for _, tc := range []struct {
		name      string
		rt        util.CtxRuntime
		rtErr     error
		latest    string
		latestErr error
		ready     bool
		whyHas    []string
	}{
		{"degraded", util.CtxRuntime{MinNode: "24.15.0", Degraded: true}, nil, "1.0.169", nil, false, []string{"Node >= 24.15.0"}},
		{"not installed", util.CtxRuntime{}, os.ErrNotExist, "1.0.169", nil, false, []string{"not usable"}},
		{"healthy", util.CtxRuntime{Node: "node", Version: "1.0.169"}, nil, "1.0.169", nil, true, nil},
		{"installed major behind latest", util.CtxRuntime{Node: "node", Version: "1.0.169", MinNode: "22.5.0"}, nil, "2.0.2", nil, false, []string{"1.0.169 installed", "Node >= 24.15.0", "skipped"}},
		{"current major", util.CtxRuntime{Node: "node", Version: "2.0.2", Major2: true}, nil, "2.0.2", nil, true, nil},
		{"latest unreachable", util.CtxRuntime{Node: "node", Version: "1.0.169"}, nil, "", os.ErrNotExist, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctxResolveRuntime = func() (util.CtxRuntime, error) { return tc.rt, tc.rtErr }
			ctxResolveLatest = func() (string, string, error) { return tc.latest, "24.15.0", tc.latestErr }
			ok, why := ctxCLIReady()
			if ok != tc.ready {
				t.Fatalf("ready = %v (%q)", ok, why)
			}
			for _, s := range tc.whyHas {
				if !strings.Contains(why, s) {
					t.Fatalf("why %q lacks %q", why, s)
				}
			}
			called := false
			fn := ctxGated(func(core.RunOpts) (bool, error) { called = true; return true, nil })
			if _, err := fn(core.RunOpts{}); err != nil || called != tc.ready {
				t.Fatalf("gated wire ran = %v, want %v (err=%v)", called, tc.ready, err)
			}
			called = false
			if ok, _ := fn(core.RunOpts{DryRun: true}); !ok || !called {
				t.Fatal("dry-run must pass through the gate")
			}
		})
	}
}

func TestCtxGateCLIWrapsOnlyNonOpenCode(t *testing.T) {
	t.Cleanup(func() { ctxResolveRuntime = nil })
	t.Setenv("TOKLESS_TEST", "1")
	ctxResolveRuntime = func() (util.CtxRuntime, error) {
		return util.CtxRuntime{Degraded: true, MinNode: "24.15.0"}, nil
	}
	opencode2TestHome(t)
	var ok bool
	var err error
	logs, _ := util.CaptureLogs(func() error {
		ok, err = contextMode.WireFor["codex"](core.RunOpts{})
		return nil
	})
	if ok || err != nil || !strings.Contains(logs, "Node >= 24.15.0; skipped") {
		t.Fatalf("codex wire must be gated: ok=%v err=%v logs=%q", ok, err, logs)
	}
	if ok, err := contextMode.WireFor["opencode"](core.RunOpts{}); !ok || err != nil {
		t.Fatalf("opencode wire must not be gated: ok=%v err=%v", ok, err)
	}
}
