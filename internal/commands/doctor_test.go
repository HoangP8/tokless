package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	toolindex "github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

func TestDoctorConfigLocation(t *testing.T) {
	path, key := toolConfigLocation("zcode", "rtk")
	if path == "" || key != "mcp.servers.rtk" {
		t.Fatalf("toolConfigLocation(zcode, rtk) = (%q, %q)", path, key)
	}
}

func TestDoctorToolDisplayIncludesPathAndDependencies(t *testing.T) {
	tool := &core.ToolManifest{ID: "rtk", Label: "RTK", NeedsGit: true}
	line := listToolVersionLine(tool, util.VersionInfo{Present: true, Installed: strPtr("1.2.3")})
	if !strings.Contains(line, "v1.2.3") || !strings.Contains(line, tildePath(util.InstalledPathFor("rtk"))) || !strings.Contains(line, "git ") {
		t.Fatalf("tool line lacks version/path/dependency: %q", line)
	}
	if strings.Contains(line, "no extra dependencies") {
		t.Fatalf("misleading dependency claim: %q", line)
	}
}

func strPtr(s string) *string { return &s }

func TestDoctorProjectmemProjectStatus(t *testing.T) {
	project := t.TempDir()
	if projectmemProjectStatus(project) {
		t.Fatal("fresh project unexpectedly has projectmem marker")
	}
	if err := os.Mkdir(filepath.Join(project, ".projectmem"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !projectmemProjectStatus(project) {
		t.Fatal("projectmem marker not detected")
	}
}

func TestDoctorProxyDetailsRedactsTokensAndIncludesConfiguredEndpoints(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	token := "secret-do-not-display"
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{{ID: "codex:demo", Token: token, Protocol: "openai-chat", Upstream: "https://api.example.test/v1"}}); err != nil {
		t.Fatal(err)
	}
	rows := doctorProxyDetails([]agentReport{{id: "codex", label: "Codex"}})
	joined := strings.Join(rows, "\n")
	for _, expected := range []string{"BYOK gateway", "18787", "BYOK demo", "https://api.example.test/v1", "token set", util.Sym.Check} {
		if !strings.Contains(joined, expected) {
			t.Errorf("doctor proxy rows missing %q: %s", expected, joined)
		}
	}
	if strings.Contains(joined, token) {
		t.Fatal("doctor output exposed BYOK token")
	}
}

func TestDoctorProxyDetailsDeduplicatesRoutesAndMarksWrapperAsWired(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	routes := []util.BYOKRoute{
		{ID: "codex:shared", Token: "token", Protocol: "openai-chat", Upstream: "https://api.example.test/v1"},
		{ID: "claude:shared", Token: "token", Protocol: "openai-chat", Upstream: "https://api.example.test/v1"},
	}
	if err := util.SaveBYOKRoutes(routes); err != nil {
		t.Fatal(err)
	}
	rows := doctorProxyDetails([]agentReport{{id: "codex"}, {id: "claude"}})
	joined := strings.Join(rows, "\n")
	if got := strings.Count(joined, "BYOK shared"); got != 1 {
		t.Fatalf("shared route appears %d times: %s", got, joined)
	}
}

func TestDoctorWrapperWiringDoesNotClaimLiveHealth(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	rows := doctorProxyDetails([]agentReport{{id: "copilot"}})
	for _, row := range rows {
		if strings.Contains(row, "wrapper wired") && strings.Contains(row, util.Sym.Check) {
			t.Fatalf("wiring-only wrapper presented as live health: %q", row)
		}
	}
}

func TestDoctorAgentRowsShowHeadroomBranchesAndProjectmemLocation(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{{ID: "codex:demo", Protocol: "openai-chat", Token: "token", Upstream: "https://api.example.test/v1"}}); err != nil {
		t.Fatal(err)
	}
	codexConfig := util.CodexPathsResolved().Config
	if err := os.MkdirAll(filepath.Dir(codexConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexConfig, []byte("model_provider = \"demo\"\n\n[model_providers.demo]\nname = \"demo\"\nbase_url = \"https://api.example.test/v1\"\napi_key = \"token\"\nwire_api = \"responses\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !agents.ConfigureProxyAgent("codex") {
		t.Fatal("failed to configure Codex proxy")
	}
	rows := doctorAgentRows(agentReport{id: "codex", label: "Codex", installed: true})
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "BYOK") || strings.Contains(joined, "https://") || !strings.Contains(joined, "RTK → ") || !strings.Contains(joined, "(PreToolUse hook)") {
		t.Fatalf("RTK row should show verified hook location, Headroom should stay port-only: %s", joined)
	}
	if strings.Contains(joined, "session hook; location varies") {
		t.Fatalf("ProjectMem uses old placeholder: %s", joined)
	}
	path, key := toolConfigLocation("codex", "projectmem")
	if !strings.Contains(joined, tildePath(path)) || !strings.Contains(joined, key) {
		t.Fatalf("ProjectMem config location missing: %s", joined)
	}
}

func TestDoctorToolRowsIncludeDependencyLabel(t *testing.T) {
	line := listToolVersionLine(&core.ToolManifest{ID: "rtk"}, util.VersionInfo{Present: true})
	if !strings.Contains(line, "[dependencies:") {
		t.Fatalf("dependency label missing: %q", line)
	}
}

func TestDoctorStatsPresentationPreservesLabelsAndRows(t *testing.T) {
	if got := stripDoctorANSI(statsToolLabel("context-mode")); got != "context-mode" {
		t.Fatalf("stats tool label changed: %q", got)
	}
	for _, row := range []string{"session (pid 123): 12 calls, 1.0K tokens", "unavailable"} {
		if got := stripDoctorANSI(paintStatsRow(row)); got != row {
			t.Errorf("stats row changed: got %q want %q", got, row)
		}
	}
}

func TestDoctorRTKStatsIncludesV1ExcludedMetric(t *testing.T) {
	rows := rtkStats("Total commands: 12\nTokens saved: 1200\nEfficiency meter: 90%\nExcluded: 3 oversized outputs\n")
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"Total commands: 12", "Excluded: 3 oversized outputs"} {
		if !strings.Contains(joined, want) {
			t.Errorf("RTK stats missing %q: %s", want, joined)
		}
	}
	legacy := strings.Join(rtkStats("Total commands: 12\nTokens saved: 1200\nEfficiency meter: 90%\n"), "\n")
	if strings.Contains(legacy, "Excluded:") {
		t.Fatalf("legacy RTK stats unexpectedly include Excluded: %s", legacy)
	}
}

func TestDoctorRTKStatsRecognizesV1JSONExcludedMetric(t *testing.T) {
	var got struct {
		Summary struct {
			OversizedExcluded int `json:"oversized_excluded"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(`{"summary":{"oversized_excluded":7}}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary.OversizedExcluded != 7 {
		t.Fatalf("oversized_excluded = %d, want 7", got.Summary.OversizedExcluded)
	}
}

func TestDoctorHeadroomStatsRowsExcludeModelsAndHarnesses(t *testing.T) {
	savings := `{"lifetime":{"tokens_saved":44257598,"cost_usd":139.88591,"calls":18250,"savings_percent":2.3},"windows":{"today":{"tokens_saved":500,"cost_usd":0.5,"calls":2,"savings_percent":2},"last_7_days":{"tokens_saved":1000,"cost_usd":1.25,"calls":4,"savings_percent":3},"last_30_days":{"tokens_saved":9000,"cost_usd":9,"calls":30,"savings_percent":4}},"by_model":[{"model":"opus","tokens_saved":100,"cost_usd":71.04,"calls":371,"savings_percent":18.3}],"by_client":[{"client":"opencode","tokens_saved":100,"cost_usd":71.04,"calls":371,"savings_percent":18.3}]}`
	perf := "noise before json\n{\"by_model\":[{\"model\":\"opus\",\"requests\":371,\"tokens_before\":100,\"tokens_saved\":100,\"savings_pct\":18.3}],\"cache_read_tokens\":40,\"cache_write_tokens\":10,\"cache_hit_pct\":25,\"total_requests\":18250,\"total_tokens_saved\":44257598}"
	joined := strings.Join(headroomStatsRows(savings, perf), "\n")
	for _, want := range []string{"lifetime: 44.3M", "$139.89", "18.2K calls", "2.3%", "today:", "7d:", "30d:", "perf: 18.2K requests", "per-port stats unavailable"} {
		if !strings.Contains(joined, want) {
			t.Errorf("headroom stats rows missing %q: %s", want, joined)
		}
	}
	for _, unwanted := range []string{"opus", "opencode", "Model", "Harness", "Tokens in", "$ avoided", "cache read", "→"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("headroom stats rows must not include model/harness breakdown, found %q: %s", unwanted, joined)
		}
	}
}

func TestDoctorHeadroomStatsRowsTolerateMissingPerf(t *testing.T) {
	savings := `{"lifetime":{"tokens_saved":1000,"cost_usd":1.25,"calls":4,"savings_percent":3}}`
	joined := strings.Join(headroomStatsRows(savings, ""), "\n")
	if !strings.Contains(joined, "lifetime: 1.0K") || strings.Contains(joined, "perf:") {
		t.Fatalf("unexpected rows without perf: %s", joined)
	}
	if rows := headroomStatsRows("not json", ""); len(rows) != 1 || !strings.Contains(rows[0], "unavailable") {
		t.Fatalf("bad savings JSON should render unavailable: %v", rows)
	}
}

func TestDoctorProxyPortsGroupConfiguredRoutes(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{
		{ID: "codex:demo", Token: "secret", Protocol: "openai-chat", Upstream: "https://api.example.test/v1"},
		{ID: "claude:demo", Token: "secret", Protocol: "openai-chat", Upstream: "https://api.example.test/v1"},
		{ID: "codex:solo", Token: "secret", Protocol: "openai-chat", Upstream: "https://solo.example.test/v1"},
	}); err != nil {
		t.Fatal(err)
	}
	ports := doctorProxyPorts([]agentReport{{id: "codex", label: "Codex"}, {id: "claude", label: "Claude"}}, false)
	joined := fmt.Sprint(ports)
	for _, want := range []string{"8787", "18787"} {
		if !strings.Contains(joined, want) {
			t.Errorf("port branches missing %q: %s", want, joined)
		}
	}
	for _, unwanted := range []string{"demo →", "Model", "Harness", "cache read"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("minimal proxy section contains %q: %s", unwanted, joined)
		}
	}
	for _, port := range ports {
		if port.url == "" || len(port.direct) == 0 {
			t.Errorf("port missing location or status: %+v", port)
		}
		if !strings.Contains(port.label, port.url) {
			t.Errorf("port title missing URL/port: %+v", port)
		}
	}
	if got := stripDoctorANSI(paintProxyLabel("8787 Main proxy — http://127.0.0.1:8787", "http://127.0.0.1:8787")); got != "8787 Main proxy — http://127.0.0.1:8787" {
		t.Fatalf("proxy label text changed: %q", got)
	}
	if got := stripDoctorANSI(agentTreeRow(agentReport{label: "DeepSeek Harness"})); !strings.HasPrefix(got, util.Sym.Check+" DeepSeek Harness") {
		t.Fatalf("harness heading text changed: %q", got)
	}
	if strings.Contains(joined, "secret") {
		t.Fatal("route token leaked")
	}
}

func stripDoctorANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestDoctorAgentHeadroomIndentIsNestedWithoutDuplicateConnectors(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{{ID: "codex:demo", Token: "token", Protocol: "openai-chat", Upstream: "https://api.example.test/v1"}}); err != nil {
		t.Fatal(err)
	}
	rows := doctorAgentRows(agentReport{id: "codex", label: "Codex", installed: true})
	for _, row := range rows {
		if strings.Contains(row, "+-   └─") {
			t.Fatalf("malformed nested connector: %q", row)
		}
	}
}

func TestDoctorContextModeStoreStatsCountsOnlyDBFiles(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".config", "opencode", "context-mode")
	for _, dir := range []string{"sessions", "content"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := contextModeStoreStats(home); got != "no indexed data" {
		t.Fatalf("empty store = %q", got)
	}
	for _, file := range []string{"sessions/a.db", "content/b.db", "content/b.db-wal"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte("1234"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := contextModeStoreStats(home); got != "store 2 DBs, 0.0 MB" {
		t.Fatalf("store stats = %q", got)
	}
}

func TestDoctorContextModeSidecarStats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats-pid-91919193.json")
	if err := os.WriteFile(path, []byte(`{"total_calls":12,"tokens_saved_lifetime":3456,"dollars_saved_lifetime":1.25,"reduction_pct":42.5}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := contextModeSidecarStats(dir)
	for _, want := range []string{"12 calls", "lifetime: 3.5K tokens / $1.25", "42.5% reduction"} {
		if !strings.Contains(got, want) {
			t.Errorf("sidecar stats missing %q: %s", want, got)
		}
	}
}

func TestDoctorContextModeChoosesNewestUpdatedSidecar(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "stats-pid-91919191.json")
	newest := filepath.Join(dir, "stats-pid-91919192.json")
	if err := os.WriteFile(stale, []byte(`{"updated_at":1791968300000,"total_calls":754,"tokens_saved_lifetime":10}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newest, []byte(`{"updated_at":1791968400000,"total_calls":12,"tokens_saved_lifetime":3456}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := contextModeSidecarStats(dir)
	if !strings.Contains(got, "12 calls") || strings.Contains(got, "754 calls") {
		t.Fatalf("selected stale sidecar: %s", got)
	}
}

func TestDoctorCodegraphStatsAcceptsLanguagesArray(t *testing.T) {
	raw := `{"initialized":true,"fileCount":294,"nodeCount":5985,"edgeCount":23510,` +
		`"dbSizeBytes":1048576,"walSizeBytes":524288,"lastIndexed":"2026-10-14T05:00:00Z",` +
		`"pendingChanges":{"added":1,"modified":2,"removed":0},` +
		`"nodesByKind":{"function":3000,"class":1200,"file":294},` +
		`"languages":["go","javascript","yaml","python"]}`
	got := strings.Join(codegraphStats(raw), "\n")
	if strings.Contains(got, "unavailable") {
		t.Fatalf("codegraph JSON rejected: %s", got)
	}
	for _, want := range []string{"Files 294", "Nodes 6.0K", "Pending: +1 ~2 -0", "Languages: go, javascript, yaml"} {
		if !strings.Contains(got, want) {
			t.Errorf("codegraph stats missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "python") {
		t.Errorf("languages not capped at 3: %s", got)
	}
}

func TestDoctorManualHeadroomDoesNotInventEndpoint(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	for _, id := range []string{"amp", "cursor"} {
		rows := strings.Join(doctorAgentRows(agentReport{id: id, label: id, installed: true}), "\n")
		if strings.Contains(rows, "8787") || !strings.Contains(rows, "not supported") {
			t.Fatalf("%s fabricated Headroom route: %s", id, rows)
		}
	}
}

func TestDoctorKimiRTKNotWired(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	rows := strings.Join(doctorAgentRows(agentReport{id: "kimi", label: "Kimi Code", installed: true}), "\n")
	if !strings.Contains(rows, "○ RTK — not wired for this harness") {
		t.Fatalf("missing Kimi RTK not-wired row: %s", rows)
	}
}

func TestDoctorGrokHeadroomUsesOAuthLaneCopy(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	rows := strings.Join(doctorAgentRows(agentReport{id: "grok", label: "Grok", installed: true}), "\n")
	if !strings.Contains(rows, "served by its own OAuth lane (28787)") {
		t.Fatalf("missing Grok OAuth lane copy: %s", rows)
	}
}

func TestDoctorStashOnlyProvidersRender(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("PI_CONFIG_DIR", filepath.Join(home, ".omp"))
	if err := os.MkdirAll(filepath.Join(home, ".omp", "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".omp", "agent", "models.yml"), []byte(`{"providers":{"cliproxy":{},"mustc":{},"nvidia_nim":{},"qwencoder":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stash := filepath.Join(util.HeadroomPathsResolved().Root, "omp.byok.stash.json")
	if err := os.MkdirAll(filepath.Dir(stash), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stash, []byte(`{"providers":{"cliproxy":{"provider":"cliproxy","base_url":"https://cliproxy.example"},"mustc":{"provider":"mustc","base_url":"https://mustc.example"},"nvidia_nim":{"provider":"nvidia_nim","base_url":"https://nvidia.example"},"qwencoder":{"provider":"qwencoder","base_url":"https://qwen.example"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := strings.Join(doctorAgentRows(agentReport{id: "omp", label: "OMP", installed: true}), "\n")
	if !strings.Contains(rows, "Headroom → http://127.0.0.1:8787") || strings.Contains(rows, "cliproxy") || strings.Contains(rows, "https://") {
		t.Fatalf("provider rows should collapse to Headroom port: %s", rows)
	}
}

func TestDoctorStaleProviderShowsNotAppliedWithoutEndpoint(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{{ID: "dsh:phantom", Token: "secret", Protocol: "openai-chat", Upstream: "https://api.example.test/v1"}}); err != nil {
		t.Fatal(err)
	}
	rows := strings.Join(doctorAgentRows(agentReport{id: "dsh", label: "DSH", installed: true}), "\n")
	if !strings.Contains(rows, "Headroom — BYOK registered but not applied") || strings.Contains(rows, "phantom") || strings.Contains(rows, "8787") {
		t.Fatalf("stale provider showed endpoint or lacked warning: %s", rows)
	}
}

func TestDoctorRegisteredRouteWithoutConfigReferenceIsWarning(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{{ID: "cline:demo", Token: "secret", Protocol: "openai-chat", Upstream: "https://api.example.test/v1"}}); err != nil {
		t.Fatal(err)
	}
	if changed, _ := agents.ConfigureClineProxy(); !changed || !agents.ClineProxyWired() {
		t.Fatal("failed to configure Cline proxy")
	}
	rows := strings.Join(doctorAgentRows(agentReport{id: "cline", label: "Cline", installed: true}), "\n")
	if !strings.Contains(rows, "Headroom — BYOK registered but not applied") || strings.Contains(rows, "demo") || strings.Contains(rows, "8787") {
		t.Fatalf("registered-but-unreferenced Cline route must warn, not fabricate: %s", rows)
	}
}

func TestDoctorHeadroomUsesZcodeWiredPredicate(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{
		{ID: "zcode:qwencoder", Token: "secret", Protocol: "openai-chat", Upstream: "https://api.example.test/v1"},
		{ID: "zcode:tokless", Token: "secret", Protocol: "openai-chat", Upstream: "https://api.example.test/v1"},
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".zcode", "v2", "provider_config.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"schemaVersion":1,"config":{"providerConfigRules":{"providerRules":[{"providerId":"qwencoder","config":{"api":{"type":"openai-chat-completions","baseUrl":"https://up.example/v1"},"access":{"apiKey":"secret"}}},{"providerId":"tokless","config":{"api":{"type":"openai-chat-completions","baseUrl":"https://up.example/v1"},"access":{"apiKey":"secret"}}}]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, _ := agents.ConfigureZcodeProxy(); !changed {
		t.Fatal("failed to configure ZCode proxy")
	}
	rows := strings.Join(doctorAgentRows(agentReport{id: "zcode", label: "ZCode", installed: true}), "\n")
	if !strings.Contains(rows, "Headroom → http://127.0.0.1:8787") || strings.Contains(rows, "qwencoder") || strings.Contains(rows, "tokless") {
		t.Fatalf("ZCode Headroom rows should show port only: %s", rows)
	}
}

func TestDoctorIndexStatus(t *testing.T) {
	project := t.TempDir()
	if !looksLikeProject(project) {
		if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if toolindex.HasCodegraphIndex(project) {
		t.Fatal("fresh project unexpectedly has CodeGraph index")
	}
}

func TestDoctorSummaryLineSeparatesLongestAgentName(t *testing.T) {
	longest := agentTreeRow(agentReport{label: "DeepSeek Harness", installed: true, wired: true})
	short := agentTreeRow(agentReport{label: "Amp", installed: true, wired: true})
	stripAnsi := func(s string) string {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == 0x1b {
				for i < len(s) && s[i] != 'm' {
					i++
				}
				continue
			}
			b.WriteByte(s[i])
		}
		return b.String()
	}
	if len(stripAnsi(longest)) != len(stripAnsi(short)) {
		t.Fatalf("agent name columns misaligned: longest=%d short=%d (%q / %q)",
			len(stripAnsi(longest)), len(stripAnsi(short)), longest, short)
	}
}

func TestInitAgentFullyWiredExcludesInstructionOnly(t *testing.T) {
	verified := false
	tools := []*core.ToolManifest{
		{
			InstructionOnly: true,
			VerifyFor: map[string]core.VerifyFn{
				"agent": func() *bool {
					verified = true
					return core.BoolPtr(false)
				},
			},
		},
	}
	if !initAgentFullyWired("agent", tools) {
		t.Fatal("instruction-only tool made agent appear unwired")
	}
	if verified {
		t.Fatal("instruction-only verifier must not run")
	}
}

func TestDoctorMissingToolsExcludesInstructionOnly(t *testing.T) {
	missing := doctorMissingTools([]*core.ToolManifest{
		{
			Label:           "Caveman",
			InstructionOnly: true,
			VerifyFor: map[string]core.VerifyFn{
				"agent": func() *bool { return core.BoolPtr(false) },
			},
		},
		{
			Label: "RTK",
			VerifyFor: map[string]core.VerifyFn{
				"agent": func() *bool { return core.BoolPtr(false) },
			},
		},
	}, "agent")
	if len(missing) != 1 || missing[0] != "RTK" {
		t.Fatalf("missing = %v", missing)
	}
}
