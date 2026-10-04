package tools

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/util"
)

func TestProjectmemPickMcpSpawnIsBoundedProxy(t *testing.T) {
	spawn := util.PickMcpSpawn("projectmem")
	joined := strings.Join(append([]string{spawn.Command}, spawn.Args...), " ")
	if !strings.Contains(joined, "run-mcp --tool projectmem") || !strings.Contains(joined, "pjm-mcp") {
		t.Fatalf("spawn is not the bounded proxy: %s", joined)
	}
	if !strings.HasSuffix(strings.Fields(joined)[len(strings.Fields(joined))-1], "pjm-mcp") {
		t.Fatalf("pjm-mcp is not the proxied server: %s", joined)
	}
}

func TestProjectmemConfigureVerifyAllAgents(t *testing.T) {
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(tmp)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
	})

	for _, agent := range []string{
		"claude", "opencode", "codex", "cursor", "antigravity", "copilot",
		"droid", "grok", "pi", "omp", "kilo", "cline",
	} {
		a := agent
		t.Run(a, func(t *testing.T) {
			if !projectmemConfigureMcp(a) {
				t.Fatalf("%s: projectmemConfigureMcp failed", a)
			}
			WriteOwner(a, "projectmem")
			if !projectmemVerify(a) {
				t.Fatalf("%s: verify false right after configure", a)
			}
			switch a {
			case "kilo":
				if !kiloHasOwner("projectmem") {
					t.Fatal("kilo: projectmem owner missing")
				}
			case "cursor":
				if _, err := util.InstallCursorProjectRules(tmp, false); err != nil {
					t.Fatalf("cursor rules install: %v", err)
				}
				raw, ok := util.ReadFileSafe(filepath.Join(tmp, ".cursor", "rules", "project-memory.mdc"))
				if !ok || !strings.Contains(string(raw), "## Project Memory (projectmem)") {
					t.Fatalf("cursor project-memory.mdc missing or lacks section: %s", raw)
				}
			default:
				if !HasOwner(a, "projectmem") {
					t.Fatalf("%s: projectmem owner section missing after WriteOwner", a)
				}
			}
		})
	}
}

func TestProjectmemDroidEnabledToolsCap(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(tmp)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
	})

	agents.ConfigureDroidMcp("projectmem")
	raw, ok := util.ReadFileSafe(filepath.Join(tmp, ".factory", "mcp.json"))
	if !ok {
		t.Fatal("droid mcp.json not written")
	}
	for _, name := range projectmemTools {
		if !strings.Contains(raw, `"`+name+`"`) {
			t.Fatalf("droid enabledTools missing %s: %s", name, raw)
		}
	}
	for _, hidden := range []string{"get_project_map", "get_context", "precheck_file", "list_projects"} {
		if strings.Contains(raw, `"`+hidden+`"`) {
			t.Fatalf("droid enabledTools leaked %s: %s", hidden, raw)
		}
	}
}

func TestProjectmemClaudePermissionNamesCap(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(tmp)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
	})

	agents.ConfigureClaudeMcp("projectmem")
	raw, ok := util.ReadFileSafe(util.ClaudeCodePaths().Settings)
	if !ok {
		t.Fatal("claude settings.json not written")
	}
	for _, name := range projectmemTools {
		if !strings.Contains(raw, "mcp__projectmem__"+name) {
			t.Fatalf("claude allow missing mcp__projectmem__%s: %s", name, raw)
		}
	}
	if strings.Contains(raw, "mcp__projectmem__get_project_map") {
		t.Fatalf("claude allow leaked get_project_map: %s", raw)
	}
}

func TestProjectmemGlobalGotchasMatchStack(t *testing.T) {
	if util.Which("pjm") == "" {
		t.Skip("pjm not installed")
	}
	home := t.TempDir()
	t.Setenv("PROJECTMEM_HOME", home)
	global := filepath.Join(home, "global")
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	entries := `{"id":"g1","library":"fastapi","gotcha":"gotcha: fastapi drops trailing slash redirects behind proxies"}` + "\n" +
		`{"id":"g2","library":"django","gotcha":"gotcha: django lesson for another stack"}` + "\n"
	if err := os.WriteFile(filepath.Join(global, "library_gotchas.jsonl"), []byte(entries), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirements.txt"), []byte("fastapi==0.110\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got := projectmemGlobalGotchas(ctx, root)
	if got == "" {
		t.Skip("gotchas unavailable in this environment (uv interpreter not reachable)")
	}
	if !strings.Contains(got, "### Global gotchas") || !strings.Contains(got, "fastapi drops trailing slash") {
		t.Fatalf("relevant gotcha missing: %q", got)
	}
	if strings.Contains(got, "django") {
		t.Fatalf("gotcha for another stack leaked: %q", got)
	}
	if other := projectmemGlobalGotchas(ctx, t.TempDir()); other != "" {
		t.Fatalf("project without matching stack got gotchas: %q", other)
	}
}

func TestProjectmemSessionTextEmptyWithoutMemory(t *testing.T) {
	root := t.TempDir()
	if got := ProjectmemSessionText(root); got != "" {
		t.Fatalf("session text without .projectmem must be empty: %q", got)
	}
}

func TestProjectmemIntentSectionsRendersReal(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := "# p\n\n## Project purpose\n**A unified pipeline for efficient and effective coding agents.**\n\n" +
		"## Recent issues\n- [DONE] thing\n\n## Notes\n- uses rtk\n\n## Key files\n- internal/tools/projectmem.go\n\n" +
		"## Open questions\n- None logged yet.\n\n## Decisions\n- digest-owned decision text\n"
	plan := "# p plan\n\n## Ideas\n_None yet._\n\n## Active plans\n- [ ] ship M2\n\n" +
		"## Next\n- [ ] queued item\n\n## Someday / maybe\n- [ ] someday item\n\n## Shipped\n_Completed.\n"
	projectMap := "# p map\n\n## Stack\n- Tags: go, github-actions\n- Detected from: go.mod, .github/workflows\n\n" +
		"## Structure\n- `assets/` — images / fonts / static assets\n"
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), summary); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "plan.md"), plan); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "PROJECT_MAP.md"), projectMap); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	for _, want := range []string{
		"### Intent & Notes",
		"#### Project purpose",
		"**A unified pipeline for efficient and effective coding agents.**",
		"#### Plan",
		"- [ ] ship M2",
		"- [ ] queued item",
		"- [ ] someday item",
		"#### Notes",
		"- uses rtk",
		"#### Stack",
		"- Tags: go, github-actions",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	for _, absent := range []string{
		"## Open questions", "## Recent issues", "## Shipped", "## Ideas", "_Move completed",
		"#### Decisions", "digest-owned decision text", "- purpose:",
		"- Detected from", "#### Structure", "`assets/`",
	} {
		if strings.Contains(got, absent) {
			t.Fatalf("unexpected %q in:\n%s", absent, got)
		}
	}
}

func TestProjectmemIntentSectionsSkipsPlaceholders(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := "# p\n\n## Project purpose\n**A unified pipeline for efficient and effective coding agents.**\n\n" +
		"## Key files\n- None yet.\n\n## Open questions\n- None logged yet.\n"
	plan := "# p plan\n\n## Ideas\n_Loose thoughts, not yet committed to._\n\n" +
		"## Active plans\n_What we're working toward now. Use `- [ ]` / `- [x]` checklists._\n\n" +
		"## Next\n_Queued, but not started._\n\n" +
		"## Someday / maybe\n_Ideas with no home yet._\n\n" +
		"## Shipped\n_Move completed plans here._\n"
	projectMap := "# p map\n\n## Stack\n_Detected automatically._\n"
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), summary); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "plan.md"), plan); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "PROJECT_MAP.md"), projectMap); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	if !strings.Contains(got, "#### Project purpose") || !strings.Contains(got, "A unified pipeline") {
		t.Fatalf("real purpose must survive:\n%s", got)
	}
	for _, absent := range []string{
		"#### Plan", "#### Key files", "#### Open questions", "#### Stack",
		"working toward now", "Loose thoughts", "Queued, but not started",
		"Ideas with no home", "Move completed plans", "None yet.", "None logged yet.",
		"_Detected automatically._",
	} {
		if strings.Contains(got, absent) {
			t.Fatalf("placeholder %q must be skipped:\n%s", absent, got)
		}
	}
}

func TestProjectmemIntentSectionsSkipsFreshSeededPlaceholders(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := "# projectmem - p\n\n_Last updated: 2026-10-03_\n\n## Project purpose\nReal purpose text here.\n\n" +
		"## Recent issues\n- No issues logged yet.\n\n## Decisions\n- No decisions logged yet.\n\n" +
		"## Notes\n- No notes logged yet.\n\n## Key files\n- No key files logged yet.\n\n" +
		"## Open questions\n- None logged yet.\n"
	plan := "# p — plan\n\n## Ideas\n_Loose thoughts, not yet committed to._\n\n" +
		"## Active plans\n_What we're working toward now. Use `- [ ]` / `- [x]` checklists._\n\n" +
		"## Next\n_Queued, but not started._\n"
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), summary); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "plan.md"), plan); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	if !strings.Contains(got, "#### Project purpose") {
		t.Fatalf("real purpose must survive:\n%s", got)
	}
	for _, absent := range []string{
		"#### Notes", "- No notes logged yet.",
		"#### Key files", "- No key files logged yet.",
		"- No issues logged yet.", "#### Open issues", "- None logged yet.",
		"working toward now.", "Loose thoughts",
		"#### Decisions", "- No decisions logged yet.",
		"#### Open questions", "#### Stack",
	} {
		if strings.Contains(got, absent) {
			t.Fatalf("seeded placeholder %q must be skipped:\n%s", absent, got)
		}
	}
}

func TestProjectmemIntentSectionsTruncates(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	notes := "# p\n\n## Project purpose\n" + strings.Repeat("purpose padding text ", 60) +
		"\n\n## Notes\n" + strings.Repeat("- a long note line with padding text\n", 120)
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), notes); err != nil {
		t.Fatal(err)
	}
	projectMap := "# p map\n\n## Stack\n- Tags: " + strings.Repeat("tag, ", 100) + "\n- Detected from: go.mod\n"
	if err := util.WriteFile(filepath.Join(dir, "PROJECT_MAP.md"), projectMap); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	if len(got) > projectmemIntentBudget {
		t.Fatalf("intent block exceeds intent-budget cap: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated block must end with an ellipsis:\n%s", got)
	}
}

func TestProjectmemIntentOpenQuestionsSurvivesHugeNotes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := "# p\n\n## Project purpose\n**A unified pipeline for efficient and effective coding agents.**\n\n## Notes\n" +
		strings.Repeat("- a long note line with padding text\n", 120) +
		"\n## Open questions\n- is the retry budget right?\n"
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), summary); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	if !strings.Contains(got, "#### Open questions") {
		t.Fatalf("Open questions section evicted by huge Notes:\n%s", got)
	}
	if !strings.Contains(got, "is the retry budget right?") {
		t.Fatalf("open question body evicted:\n%s", got)
	}
	if n, k := strings.Index(got, "#### Notes"), strings.Index(got, "#### Open questions"); n < 0 || k < n {
		t.Fatalf("Open questions must follow Notes:\n%s", got)
	}
	if len(got) > projectmemIntentBudget {
		t.Fatalf("intent block exceeds its budget: %d bytes", len(got))
	}
}

func TestProjectmemNotesKeepsNewest(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("# p\n\n## Notes\n")
	b.WriteString("- OLDEST_SENTINEL " + strings.Repeat("old ", 40) + "\n")
	for i := 0; i < 100; i++ {
		b.WriteString("- middle note line with padding text\n")
	}
	b.WriteString("- NEWEST_SENTINEL " + strings.Repeat("new ", 40) + "\n")
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), b.String()); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	if !strings.Contains(got, "NEWEST_SENTINEL") {
		t.Fatalf("newest note dropped by tail-keep:\n%s", got)
	}
	if strings.Contains(got, "OLDEST_SENTINEL") {
		t.Fatalf("oldest note should be dropped:\n%s", got)
	}
	if !strings.Contains(got, "more in summary.md") {
		t.Fatalf("dropped notes must be reported honestly:\n%s", got)
	}
	n, newest := strings.Index(got, "#### Notes"), strings.Index(got, "NEWEST_SENTINEL")
	if n < 0 || newest < n {
		t.Fatalf("newest note must sit under Notes:\n%s", got)
	}
}

func TestProjectmemIntentNoDigestDup(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := "# p\n\n## Project purpose\nReal purpose here.\n\n" +
		"## Decisions\n- DECISION_SENTINEL digest carried decision text\n"
	projectMap := "# p map\n\n## Structure\n- `assets/` — images / fonts / static assets\n"
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), summary); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "PROJECT_MAP.md"), projectMap); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	if !strings.Contains(got, "Real purpose here.") {
		t.Fatalf("purpose should survive:\n%s", got)
	}
	for _, absent := range []string{"#### Decisions", "DECISION_SENTINEL", "#### Structure", "`assets/`"} {
		if strings.Contains(got, absent) {
			t.Fatalf("digest-owned %q must not be duplicated:\n%s", absent, got)
		}
	}
}

func TestProjectmemIntentG3Gate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := "# p\n\n## Project purpose\n**A unified pipeline for efficient and effective coding agents.**\n\n" +
		"## Recent issues\n- [DONE] old\n- [OPEN] #2 live one\n- [OPEN] #3 live two\n\n" +
		"## Decisions\n- digest-owned decision text\n\n" +
		"## Notes\n- uses rtk\n- uses projectmem\n\n" +
		"## Key files\n- internal/tools/projectmem.go\n- internal/util/agent_instructions.md\n\n" +
		"## Open questions\n- how to keep intent tight?\n"
	plan := "# p plan\n\n## Ideas\n_Loose thoughts, not yet committed to._\n\n" +
		"## Active plans\n- [ ] ship G3-B\n\n## Next\n- [ ] verify mirrors\n\n" +
		"## Someday / maybe\n_Unpromoted ideas live here._\n\n## Shipped\n- [x] earlier work\n" +
		"## Shipped details\n- completed item not shown\n"
	projectMap := "# p map\n\n## Stack\n- Tags: github-actions, go\n- Detected from: go.mod, .github/workflows\n\n" +
		"## Structure\n- `assets/` — images\n"
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), summary); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "plan.md"), plan); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "PROJECT_MAP.md"), projectMap); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	for _, h := range []string{
		"#### Project purpose", "#### Plan", "#### Open issues", "#### Notes",
		"#### Open questions", "#### Stack",
	} {
		if !strings.Contains(got, h) {
			t.Fatalf("gate: missing %s in:\n%s", h, got)
		}
	}
	if strings.Contains(got, "…") {
		t.Fatalf("gate: fitting fixture must not truncate:\n%s", got)
	}
	for _, junk := range []string{
		"Loose thoughts", "working toward now", "Queued, but not started",
		"Unpromoted ideas", "Move completed plans here",
		"- [x] earlier work", "- completed item not shown",
		"## Shipped", "_Move completed",
		"- No notes logged yet.", "- No key files logged yet.", "- No decisions logged yet.",
		"- Detected from", "#### Decisions", "#### Structure", "`assets/`",
	} {
		if strings.Contains(got, junk) {
			t.Fatalf("gate: placeholder/digest %q leaked:\n%s", junk, got)
		}
	}
	if len(got) > projectmemIntentBudget {
		t.Fatalf("gate: intent length %d exceeds its budget", len(got))
	}
}

func TestProjectmemIntentSectionsOpenIssues(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := "# p\n\n## Project purpose\n**A unified pipeline for efficient and effective coding agents.**\n\n" +
		"## Recent issues\n- [DONE] #1 closed thing (fixed)\n- [OPEN] #2 still broken\n- [OPEN] #3 also broken\n\n" +
		"## Notes\n- uses rtk\n"
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), summary); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "plan.md"), "## Active plans\n- [ ] ship M2\n"); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	if !strings.Contains(got, "#### Open issues") {
		t.Fatalf("open issues section missing:\n%s", got)
	}
	if !strings.Contains(got, "- [OPEN] #2 still broken") || !strings.Contains(got, "- [OPEN] #3 also broken") {
		t.Fatalf("open issues not injected:\n%s", got)
	}
	if strings.Contains(got, "[DONE]") || strings.Contains(got, "closed thing") {
		t.Fatalf("closed issue must not leak:\n%s", got)
	}
	p, o, n := strings.Index(got, "#### Project purpose"), strings.Index(got, "#### Open issues"), strings.Index(got, "#### Notes")
	if p < 0 || o < 0 || n < 0 || !(p < o && o < n) {
		t.Fatalf("order must be purpose < open issues < notes:\n%s", got)
	}
}

func TestProjectmemIntentSectionsOpenIssuesCapsFive(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := "# p\n\n## Recent issues\n" + strings.Repeat("- [OPEN] #9 open thing\n", 7)
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), summary); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	if n := strings.Count(got, "- [OPEN]"); n != 5 {
		t.Fatalf("open issues must cap at 5, got %d:\n%s", n, got)
	}
}

func TestProjectmemSessionTextAppendsIntent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is unix-only")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), "# p\n\n## Project purpose\n**A unified pipeline for efficient and effective coding agents.**\n\n## Notes\n- uses rtk\n"); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "plan.md"), "## Active plans\n- [ ] ship M2\n"); err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	fake := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fake(filepath.Join(bin, "pjm"),
		"#!/bin/sh\nprintf '## projectmem context (budget: 2000 tokens)\\n- [DONE] #1 did a thing\\n'\n")
	if err := os.MkdirAll(filepath.Join(bin, "projectmem", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	bigGotchas := "#!/bin/sh\nprintf '### Global gotchas\\n" +
		strings.Repeat("- fastapi: test gotcha line with padding\\n", 80) + "'\n"
	fake(filepath.Join(bin, "projectmem", "bin", "python"), bigGotchas)
	fake(filepath.Join(bin, "uv"), "#!/bin/sh\necho "+bin+"\n")
	t.Setenv("PATH", bin)

	got := ProjectmemSessionText(root)
	ctxIdx := strings.Index(got, "## projectmem context")
	intentIdx := strings.Index(got, "### Intent & Notes")
	gotchasIdx := strings.Index(got, "### Global gotchas")
	if ctxIdx < 0 || intentIdx < 0 || gotchasIdx < 0 {
		t.Fatalf("expected ctx, intent, and gotchas sections; got:\n%s", got)
	}
	if !(ctxIdx < intentIdx && intentIdx < gotchasIdx) {
		t.Fatalf("order must be context < intent < gotchas; got:\n%s", got)
	}
	if !strings.Contains(got, "A unified pipeline") || !strings.Contains(got, "- [ ] ship M2") {
		t.Fatalf("intent content missing:\n%s", got)
	}
	if g := got[gotchasIdx:]; len(g) > 603 || !strings.HasSuffix(g, "…") {
		t.Fatalf("gotchas must be capped to 600 bytes, got %d bytes:\n%s", len(g), g)
	}
}

func TestProjectmemSessionPushWiring(t *testing.T) {
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	t.Cleanup(func() { util.SetHomeOverride("") })

	for _, a := range []string{"claude", "opencode", "codex", "droid", "copilot", "antigravity", "pi", "omp", "kilo"} {
		if !projectmemInstallSessionPush(a) {
			t.Fatalf("%s: install failed", a)
		}
		if !projectmemInstallSessionPush(a) {
			t.Fatalf("%s: second install not idempotent", a)
		}
		if !projectmemHasSessionPush(a) {
			t.Fatalf("%s: push missing after install", a)
		}
		projectmemRemoveSessionPush(a)
		if projectmemHasSessionPush(a) {
			t.Fatalf("%s: push still present after remove", a)
		}
	}
}

func TestProjectmemGitExcludeAddsOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	projectmemGitExclude(root, ".grok/rules/projectmem-context.md")
	projectmemGitExclude(root, ".grok/rules/projectmem-context.md")
	raw, _ := util.ReadFileSafe(filepath.Join(root, ".git", "info", "exclude"))
	if strings.Count(raw, "/.grok/rules/projectmem-context.md\n") != 1 {
		t.Fatalf("exclude = %q", raw)
	}
	nogit := t.TempDir()
	projectmemGitExclude(nogit, "x")
	if util.Exists(filepath.Join(nogit, ".git")) {
		t.Fatal("must not create .git in a non-repo")
	}
}

func TestProjectmemForeignMcpEntryFailsVerify(t *testing.T) {
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(tmp)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
	})

	piDir := filepath.Join(tmp, "pi")
	t.Setenv("PI_CODING_AGENT_DIR", piDir)
	for _, tc := range []struct {
		agent  string
		paths  []string
		config string
	}{
		{"claude", []string{util.ClaudeCodePaths().GlobalJSON}, `{"mcpServers":{"projectmem":{"command":"/usr/bin/foreign-mcp","args":["--stdio"]}}}`},
		{"opencode", []string{util.OpenCodePathsResolved().Config}, `{"mcp":{"projectmem":{"command":["/usr/bin/foreign-mcp","--stdio"]}}}`},
		{"copilot", []string{util.CopilotPathsResolved().McpConfig}, `{"mcpServers":{"projectmem":{"command":"/usr/bin/foreign-mcp","args":["--stdio"]}}}`},
		{"antigravity", []string{util.AntigravityPathsResolved().McpConfigCLI, filepath.Join(util.Home(), ".gemini", "antigravity-cli", "mcp_config.json")}, `{"mcpServers":{"projectmem":{"command":"/usr/bin/foreign-mcp","trust":true}}}`},
		{"droid", []string{filepath.Join(tmp, ".factory", "mcp.json")}, `{"mcpServers":{"projectmem":{"command":"/usr/bin/foreign-mcp","args":["--stdio"]}}}`},
		{"pi", []string{filepath.Join(piDir, "mcp.json")}, `{"mcpServers":{"projectmem":{"command":"/usr/bin/foreign-mcp","args":["--stdio"],"exposure":"direct"}}}`},
	} {
		tc := tc
		t.Run(tc.agent, func(t *testing.T) {
			for _, p := range tc.paths {
				if err := util.EnsureDir(filepath.Dir(p)); err != nil {
					t.Fatal(err)
				}
				if err := util.WriteFile(p, tc.config); err != nil {
					t.Fatal(err)
				}
			}
			projectmemConfigureMcp(tc.agent)
			if projectmemVerify(tc.agent) {
				t.Fatalf("%s: foreign projectmem MCP entry passed verify", tc.agent)
			}
		})
	}
}

// TestProjectmemCodexVerifyShape proves codex verify is shape-exact: a foreign
// [mcp_servers.projectmem] block (or a missing one) must not pass, and the
// bounded tokless run-mcp spawn must.
func TestProjectmemCodexVerifyShape(t *testing.T) {
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	t.Cleanup(func() { util.SetHomeOverride("") })

	cfgPath := util.CodexPathsResolved().Config
	if err := util.EnsureDir(filepath.Dir(cfgPath)); err != nil {
		t.Fatal(err)
	}

	// (c) missing block.
	if projectmemVerify("codex") {
		t.Fatal("missing projectmem block must not verify")
	}

	// (a) foreign block: direct pjm-mcp, not through the tokless proxy.
	if err := util.WriteFile(cfgPath, "[mcp_servers.projectmem]\ncommand = \"pjm-mcp\"\nargs = []\n"); err != nil {
		t.Fatal(err)
	}
	if projectmemVerify("codex") {
		t.Fatal("foreign pjm-mcp block must not verify")
	}

	// (b) bounded spawn block.
	spawn := util.McpSpawnFor("projectmem")
	block := util.NewTomlBlock("mcp_servers.projectmem")
	block.Set("command", spawn.Command)
	block.Set("args", spawn.Args)
	block.Set("enabled", true)
	if err := util.WriteFile(cfgPath, util.UpsertBlock("", block, false)); err != nil {
		t.Fatal(err)
	}
	if !projectmemVerify("codex") {
		t.Fatalf("bounded projectmem block must verify:\n%s", mustRead(t, cfgPath))
	}
}

func TestProjectmemForeignCopilotIdeFailsVerify(t *testing.T) {
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(tmp)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
	})
	if err := util.WriteFile(filepath.Join(tmp, ".vscode", "mcp.json"), `{"servers":{"projectmem":{"command":"/usr/bin/foreign-mcp","args":["--stdio"],"type":"stdio"}}}`); err != nil {
		t.Fatal(err)
	}
	projectmemConfigureMcp("copilot")
	if projectmemVerify("copilot") {
		t.Fatal("foreign copilot IDE MCP entry passed verify")
	}
}

func TestGitCommonDirWorktree(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()
	if err := os.MkdirAll(filepath.Join(main, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := gitCommonDir(main); got != filepath.Join(main, ".git") {
		t.Fatalf("plain repo: got %q", got)
	}
	gd := filepath.Join(main, ".git", "worktrees", "w1")
	if err := os.MkdirAll(gd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(gd, "commondir"), "../..\n"); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wt, gd)
	if err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(wt, ".git"), "gitdir: "+rel+"\n"); err != nil {
		t.Fatal(err)
	}
	if got := gitCommonDir(wt); got != filepath.Join(main, ".git") {
		t.Fatalf("worktree: got %q", got)
	}
	if gitCommonDir(t.TempDir()) != "" {
		t.Fatal("no .git must resolve to empty")
	}
	if err := util.WriteFile(filepath.Join(main, ".git", "hooks", "post-commit"), "#!/bin/sh\n# projectmem\n"); err != nil {
		t.Fatal(err)
	}
	if !gitHookHasProjectmem(wt) {
		t.Fatal("worktree must see shared post-commit hook")
	}
	projectmemGitExclude(wt, ".grok/rules/projectmem-context.md")
	raw, _ := util.ReadFileSafe(filepath.Join(main, ".git", "info", "exclude"))
	if !strings.Contains(raw, "/.grok/rules/projectmem-context.md") {
		t.Fatalf("exclude not written to shared git dir: %q", raw)
	}
}

func TestEnsureProjectmemProjectWorktreeHooks(t *testing.T) {
	if util.Which("git") == "" || util.Which("pjm") == "" {
		t.Skip("git/pjm not installed")
	}
	base := t.TempDir()
	main := filepath.Join(base, "main")
	wt := filepath.Join(base, "wt")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	run(main, "init", "-q")
	run(main, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	run(main, "worktree", "add", "-q", wt, "-b", "testbranch")
	if err := os.MkdirAll(filepath.Join(wt, ".projectmem"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOKLESS_TEST", "")
	if err := EnsureProjectmemProject(wt); err != nil {
		t.Fatalf("EnsureProjectmemProject in worktree: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(main, ".git", "hooks", "post-commit"))
	if err != nil || !bytes.Contains(raw, []byte("projectmem")) {
		t.Fatalf("shared post-commit hook missing after ensure: %v", err)
	}
	if err := EnsureProjectmemProject(wt); err != nil {
		t.Fatalf("second ensure not idempotent: %v", err)
	}
}

// intentSectionBody extracts the rendered body of a "#### heading" section.
func intentSectionBody(got, heading string) (string, bool) {
	marker := "#### " + heading + "\n"
	i := strings.Index(got, marker)
	if i < 0 {
		return "", false
	}
	rest := got[i+len(marker):]
	if j := strings.Index(rest, "\n\n#### "); j >= 0 {
		rest = rest[:j]
	}
	return rest, true
}

func TestProjectmemTruncateHelpersExactCap(t *testing.T) {
	long := strings.Repeat("- note line with padding text here\n", 200)
	for _, lim := range []int{0, 1, 2, 3, 4, 5, 50, 101} {
		if got := projectmemTruncateTail(long, lim); len(got) > lim {
			t.Fatalf("truncateTail limit %d: len %d", lim, len(got))
		}
	}
	got := projectmemTruncateTail(long, 100)
	if len(got) > 100 || !strings.HasPrefix(got, "…\n") {
		t.Fatalf("truncateTail must keep marker and cap: len=%d %q", len(got), got)
	}
	if !strings.Contains(got, "padding text here") {
		t.Fatalf("truncateTail dropped the newest tail:\n%q", got)
	}
	if g := projectmemTruncateTail("small", 100); g != "small" {
		t.Fatalf("fitting text altered: %q", g)
	}

	words := strings.Repeat("word ", 400)
	for _, lim := range []int{200, 300} {
		got := projectmemTruncateWords(words, lim)
		if len(got) > lim {
			t.Fatalf("truncateWords limit %d: len %d", lim, len(got))
		}
	}
	// No word boundary and no rune boundary must both stay within the cap.
	if got := projectmemTruncateWords(strings.Repeat("x", 500), 200); len(got) > 200 {
		t.Fatalf("space-free input exceeds cap: %d", len(got))
	}
	if got := projectmemTruncateWords(strings.Repeat("日", 500), 200); len(got) > 200 || !utf8.ValidString(got) {
		t.Fatalf("multibyte input exceeds cap or split a rune: %d %q", len(got), got)
	}
	for _, lim := range []int{0, 1, 2, 3} {
		if got := projectmemTruncateWords("abcdef", lim); len(got) != lim {
			t.Fatalf("tiny limit %d: len=%d %q", lim, len(got), got)
		}
	}
	if g := projectmemTruncateWords("short", 100); g != "short" {
		t.Fatalf("fitting words altered: %q", g)
	}
	if got := projectmemTruncateHead(long, 100); len(got) > 100 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncateHead must cap with trailing marker: len=%d %q", len(got), got)
	}
}

func TestProjectmemNotesBodyDropsDigestDuplicates(t *testing.T) {
	carried := "rtk git diff truncates output at ~602 lines, use plain git diff"
	unique := "unlocated note the digest never renders"
	notes := "- " + carried + "\n- " + unique
	digest := "### File Gotchas\n- `internal/x.go`: NOTE (worked): " + projectmemSmartTruncate(carried, 60) + "\n"

	got := projectmemNotesBody(notes, digest, projectmemNotesBudget)
	if strings.Contains(got, "rtk git diff") {
		t.Fatalf("note already carried by the digest was duplicated:\n%s", got)
	}
	if !strings.Contains(got, unique) {
		t.Fatalf("note the digest misses was dropped:\n%s", got)
	}
	if got := projectmemNotesBody(notes, "", projectmemNotesBudget); !strings.Contains(got, "rtk git diff") {
		t.Fatalf("notes dropped without a digest to dedupe against:\n%s", got)
	}
	if got := projectmemNotesBody("- None logged yet.", digest, projectmemNotesBudget); got != "" {
		t.Fatalf("placeholder notes must stay empty: %q", got)
	}
	// Upstream may cut a note at a sentence or word boundary, so only a short anchor matches.
	long := "gotcha: pi1.0 extensions install to ~/.pi/agent/npm only when npm is on PATH otherwise silent failure"
	rendered := projectmemSmartTruncate(long, 60)
	if got := projectmemNotesBody("- "+long, "### File Gotchas\n- `a.go`: NOTE: "+rendered+"\n", projectmemNotesBudget); strings.Contains(got, "extensions install") {
		t.Fatalf("word-boundary rendering not matched, note duplicated:\n%s", got)
	}
	if got := projectmemNotesBody("- "+long, "", projectmemNotesBudget); !strings.Contains(got, "extensions install") {
		t.Fatalf("digest-less render dropped a real note:\n%s", got)
	}
	// Notes sharing a conventional prefix must survive when the digest carries only one.
	a := "feat(agents): route native agents through Headroom transport for all channels"
	b := "feat(agents): route kilo via headroom transport plugin with pinned version"
	digestA := "### File Gotchas\n- `internal/agents/claude.go`: NOTE: " + projectmemSmartTruncate(a, 60) + "\n"
	kept := projectmemNotesBody("- "+a+"\n- "+b, digestA, projectmemNotesBudget)
	if strings.Contains(kept, "native agents") {
		t.Fatalf("note the digest already carries was duplicated:\n%s", kept)
	}
	if !strings.Contains(kept, "kilo") {
		t.Fatalf("note the digest does not carry was dropped:\n%s", kept)
	}
}

func TestProjectmemIntentSectionsExcludesShipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := "# p plan\n\n## Active plans\n- [ ] live work\n\n" +
		"## Shipped\n- [x] SHIPPED_SENTINEL completed long ago\n"
	if err := util.WriteFile(filepath.Join(dir, "plan.md"), plan); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	if !strings.Contains(got, "#### Plan") || !strings.Contains(got, "- [ ] live work") {
		t.Fatalf("authored active plan must survive:\n%s", got)
	}
	for _, absent := range []string{"SHIPPED_SENTINEL", "Shipped", "completed long ago"} {
		if strings.Contains(got, absent) {
			t.Fatalf("shipped content %q must be excluded:\n%s", absent, got)
		}
	}
}

func TestProjectmemIntentSectionsRespectCaps(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := "# p\n\n## Project purpose\n" + strings.Repeat("purpose word ", 120) + "\n\n" +
		"## Recent issues\n" + strings.Repeat("- [OPEN] #9 open item with padding text\n", 5) + "\n" +
		"## Notes\n" + strings.Repeat("- note line with padding text here\n", 120) + "\n" +
		"## Open questions\n" + strings.Repeat("- question with padding text here\n", 60) + "\n"
	plan := "# p plan\n\n## Active plans\n" + strings.Repeat("- [ ] plan item with padding text\n", 120)
	projectMap := "# p map\n\n## Stack\n- Tags: " + strings.Repeat("tag, ", 150) + "\n"
	if err := util.WriteFile(filepath.Join(dir, "summary.md"), summary); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "plan.md"), plan); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "PROJECT_MAP.md"), projectMap); err != nil {
		t.Fatal(err)
	}

	got := projectmemIntentSections(root, "")
	if len(got) > projectmemIntentBudget {
		t.Fatalf("intent exceeds intent-budget aggregate: %d", len(got))
	}
	caps := []struct {
		heading string
		cap     int
	}{
		{"Project purpose", projectmemPurposeBudget},
		{"Plan", projectmemPlanBudget},
		{"Open issues", projectmemOpenIssuesBudget},
		{"Notes", projectmemNotesBudget},
		{"Open questions", projectmemQuestionsBudget},
		{"Stack", projectmemStackBudget},
	}
	for _, c := range caps {
		body, ok := intentSectionBody(got, c.heading)
		if !ok {
			t.Fatalf("section %s missing:\n%s", c.heading, got)
		}
		if len(body) > c.cap {
			t.Fatalf("section %s body %d exceeds cap %d:\n%s", c.heading, len(body), c.cap, got)
		}
	}
	for _, h := range []string{"#### Open questions", "#### Stack"} {
		if !strings.Contains(got, h) {
			t.Fatalf("lower-priority %s evicted under max content:\n%s", h, got)
		}
	}
	if q, s := strings.Index(got, "#### Open questions"), strings.Index(got, "#### Stack"); q < 0 || s < q {
		t.Fatalf("order must keep Open questions before Stack:\n%s", got)
	}
}

func TestProjectmemSessionTextHardCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is unix-only")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "summary.md"),
		"# p\n\n## Project purpose\nPURPOSE_SENTINEL real purpose text.\n"); err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '## projectmem context (budget: 2000 tokens)\\n'\n" +
		"printf '%s\\n' '- [DONE] #1 CORE_SENTINEL did a thing'\n" +
		"i=0\n" +
		"while [ $i -lt 400 ]; do\n" +
		"  printf '%s\\n' '- [DONE] filler line padding padding padding padding padding padding'\n" +
		"  i=$((i+1))\n" +
		"done\n"
	if err := os.WriteFile(filepath.Join(bin, "pjm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	got := ProjectmemSessionText(root)
	if len(got) > projectmemSessionTextCap {
		t.Fatalf("session text %d exceeds hard cap %d", len(got), projectmemSessionTextCap)
	}
	if !strings.Contains(got, "## projectmem context (budget:") {
		t.Fatalf("core digest header dropped:\n%s", got)
	}
	if !strings.Contains(got, "CORE_SENTINEL") {
		t.Fatalf("core digest body dropped:\n%s", got)
	}
}

func TestProjectmemSessionTextReservesIntentBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is unix-only")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".projectmem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "summary.md"),
		"# p\n\n## Project purpose\nPURPOSE_SENTINEL real purpose text.\n\n"+
			"## Key files\n- `internal/tools/projectmem.go` — session state\n"); err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	// Digest alone is larger than the whole session budget: intent must still survive.
	script := "#!/bin/sh\nprintf '## projectmem context (budget: 2000 tokens)\\n'\n" +
		"printf '%s\\n' '- [DONE] #1 CORE_SENTINEL did a thing'\n" +
		"i=0\n" +
		"while [ $i -lt 900 ]; do\n" +
		"  printf '%s\\n' '- [DONE] filler line padding padding padding padding padding padding'\n" +
		"  i=$((i+1))\n" +
		"done\n"
	if err := os.WriteFile(filepath.Join(bin, "pjm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	got := ProjectmemSessionText(root)
	if len(got) > projectmemSessionTextCap {
		t.Fatalf("session text %d exceeds hard cap %d", len(got), projectmemSessionTextCap)
	}
	if !strings.Contains(got, "### Intent & Notes") || !strings.Contains(got, "PURPOSE_SENTINEL") {
		t.Fatalf("reserved intent dropped when digest overflowed:\n%s", got)
	}
	digest := got[:strings.Index(got, "\n\n### Intent & Notes")]
	if len(digest) > projectmemDigestBudget {
		t.Fatalf("digest %d exceeds budget %d", len(digest), projectmemDigestBudget)
	}
	if !strings.Contains(got, "CORE_SENTINEL") {
		t.Fatalf("core digest body dropped:\n%s", got)
	}
}

func TestProjectmemJunkIssueFiltersHarnessArtifacts(t *testing.T) {
	junk := []string{
		"- [OPEN] #0001 placeholder (open)",
		"- [OPEN] #0004 review probe (open)",
		"- [OPEN] placeholder",
		"- [OPEN] #0007 probe [internal/x.go]",
		"- [OPEN] tmp",
	}
	for _, line := range junk {
		if !projectmemJunkIssue(line) {
			t.Fatalf("harness artifact not filtered: %q", line)
		}
	}
	// A real issue survives, even when its title contains a junk word.
	real := []string{
		"- [OPEN] #0017 G3 injection includes stale Shipped plan entries [internal/tools/projectmem.go] (open)",
		"- [OPEN] test harness races on concurrent wire",
		"- [OPEN] temp file cleanup leaks descriptors",
		"- [OPEN] placeholder page renders wrong selector",
		"- [OPEN] #0006 codex BYOK rollback failed",
	}
	for _, line := range real {
		if projectmemJunkIssue(line) {
			t.Fatalf("real issue wrongly filtered: %q", line)
		}
	}
}

func TestProjectmemNotesRankKeepsValuableAfterTailCut(t *testing.T) {
	low := "- lesson: projectmem internals churn, oracle LOWs ignored"
	mid := "- gotcha: bare context-mode entry blocks cursor wire"
	high := "- gotcha: `tokless update` wipes 5 manual patches [internal/util/uv.go]"

	if !(projectmemNoteRank(high) > projectmemNoteRank(mid) && projectmemNoteRank(mid) > projectmemNoteRank(low)) {
		t.Fatalf("rank ordering wrong: high=%d mid=%d low=%d",
			projectmemNoteRank(high), projectmemNoteRank(mid), projectmemNoteRank(low))
	}

	// Value decides admission; survivors keep their original store order.
	tight := len(high) + len(mid) + len(low) + 3
	got := projectmemNotesBody(low+"\n"+high+"\n"+mid, "", tight)
	iLow, iHigh := strings.Index(got, "lesson:"), strings.Index(got, "tokless update")
	if iLow >= 0 || iHigh < 0 {
		t.Fatalf("highest-ranked note must win a tight budget, lowest must lose:\n%s", got)
	}
	iMid := strings.Index(got, "cursor wire")
	if iMid < 0 {
		t.Fatalf("second-ranked note must still fit:\n%s", got)
	}
	// Store order was low,high,mid; low lost, so survivors must read high,mid.
	if iHigh > iMid {
		t.Fatalf("survivors must keep store order, not rank order:\n%s", got)
	}
	// Over budget, the loss is stated rather than silently half-printed, and the
	// section still respects the cap it was given.
	tightCap := len(high) + 10
	full := projectmemNotesBody(high+"\n"+mid+"\n"+low, "", tightCap)
	if !strings.Contains(full, "more in summary.md") {
		t.Fatalf("expected an overflow notice:\n%s", full)
	}
	if len(full) > tightCap {
		t.Fatalf("notes body %d exceeds cap %d:\n%s", len(full), tightCap, full)
	}
	// The notice must count only what was really dropped.
	if !strings.Contains(full, "2 more") {
		t.Fatalf("overflow notice must count both dropped notes:\n%s", full)
	}
}

func TestProjectmemNotesNeverExceedAnyBudget(t *testing.T) {
	notes := "- gotcha: first note with a location [internal/x.go]\n" +
		"- gotcha: second note, also located [internal/y.go]\n" +
		"- lesson: third note without a location\n" +
		"- gotcha: fourth note that is quite long indeed and located [internal/z.go]\n"
	for _, budget := range []int{0, 1, 3, 5, 20, 21, 25, 26, 27, 40, 64, 100, 200, 400, 3200} {
		got := projectmemNotesBody(notes, "", budget)
		if len(got) > budget {
			t.Fatalf("budget %d: body %d bytes exceeds it:\n%s", budget, len(got), got)
		}
		// No line may be cut in half, and no count may be invented or inverted.
		for _, line := range strings.Split(got, "\n") {
			if line == "" {
				continue
			}
			if !strings.Contains(notes, line) && !strings.Contains(line, "more in summary.md") {
				t.Fatalf("budget %d: fabricated line %q", budget, line)
			}
		}
		if strings.Contains(got, "… 0 more") || strings.Contains(got, "… -1 more") {
			t.Fatalf("budget %d: inverted or zero drop count:\n%s", budget, got)
		}
	}
	if got := projectmemNotesBody(notes, "", projectmemNotesBudget); strings.Contains(got, "more in summary.md") {
		t.Fatalf("all notes fit, yet a drop notice was emitted:\n%s", got)
	}
}
