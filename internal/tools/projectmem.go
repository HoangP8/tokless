package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// EnsureProjectmemProject creates .projectmem/ and restores git hooks; idempotent.
func EnsureProjectmemProject(root string) error {
	if root == "" {
		return nil
	}
	_ = os.Remove(filepath.Join(root, ".projectmem", "AI_INSTRUCTIONS.md"))
	if os.Getenv("TOKLESS_TEST") == "1" {
		return nil
	}
	pjm := util.Which("pjm")
	if pjm == "" {
		return fmt.Errorf("pjm not found on PATH (uv tool install projectmem)")
	}
	if !util.Exists(filepath.Join(root, ".projectmem")) {
		if out, err := runPjm(root, pjm, 90*time.Second, "init", "--no-mcp-config", "--no-claude-md", "--no-watch"); err != nil {
			return fmt.Errorf("pjm init: %w: %s", err, pjmOutputSnippet(out))
		}
		_ = os.Remove(filepath.Join(root, ".projectmem", "AI_INSTRUCTIONS.md"))
	}
	if cd := gitCommonDir(root); cd != "" && !gitHookHasProjectmem(root) {
		runDir := root
		if filepath.Base(cd) == ".git" {
			runDir = filepath.Dir(cd)
		}
		out, err := runPjm(runDir, pjm, 30*time.Second, "hooks", "install")
		if err != nil {
			return fmt.Errorf("pjm hooks install: %w: %s", err, pjmOutputSnippet(out))
		}
		if !gitHookHasProjectmem(root) {
			return fmt.Errorf("pjm hooks install left no projectmem hook: %s", pjmOutputSnippet(out))
		}
	}
	projectmemSeedPurpose(root)
	return nil
}

// projectmemSeedPurpose fills PROJECT_MAP.md's placeholder purpose from the README, then regenerates summary.md.
func projectmemSeedPurpose(root string) {
	mapPath := filepath.Join(root, ".projectmem", "PROJECT_MAP.md")
	raw, ok := util.ReadFileSafe(mapPath)
	if !ok {
		return
	}
	if body, found := projectmemMapPurpose(raw); found && !projectmemPurposePlaceholder(body) {
		return
	}
	desc := projectmemReadmePurpose(root)
	if desc == "" {
		return
	}
	if updated, changed := projectmemSetMapPurpose(raw, desc); changed {
		if util.WriteFile(mapPath, updated) != nil {
			return
		}
		if pjm := util.Which("pjm"); pjm != "" {
			_, _ = runPjm(root, pjm, 30*time.Second, "regenerate")
		}
	}
}

// projectmemPurposeHeading returns the byte range of a line-anchored top-level "## Project purpose" heading.
func projectmemPurposeHeading(text string) (int, int, bool) {
	for i := 0; i < len(text); {
		if i == 0 || text[i-1] == '\n' {
			rest := text[i:]
			if h := "## Project purpose"; strings.HasPrefix(rest, h) &&
				(len(rest) == len(h) || rest[len(h)] == '\n' || rest[len(h)] == '\r') {
				return i, i + len(h), true
			}
		}
		nl := strings.IndexByte(text[i:], '\n')
		if nl < 0 {
			break
		}
		i += nl + 1
	}
	return 0, 0, false
}

// projectmemMapPurpose returns the body of the ## Project purpose section.
func projectmemMapPurpose(text string) (string, bool) {
	_, bodyStart, ok := projectmemPurposeHeading(text)
	if !ok {
		return "", false
	}
	body := text[bodyStart:]
	if j := strings.Index(body, "\n## "); j >= 0 {
		body = body[:j]
	}
	return strings.TrimSpace(strings.TrimPrefix(body, "\r")), true
}

// projectmemPurposePlaceholder reports whether the body is empty or only known placeholder phrases.
func projectmemPurposePlaceholder(body string) bool {
	phrases := []string{
		"Not described yet.", "Replace this placeholder",
		"Short description of what the project does.",
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		phrase := false
		for _, p := range phrases {
			if strings.Contains(line, p) {
				phrase = true
				break
			}
		}
		if !phrase {
			return false
		}
	}
	return true
}

// projectmemSetMapPurpose replaces a placeholder purpose body, or inserts
// the section before the first existing ## heading when it is missing.
func projectmemSetMapPurpose(text, desc string) (string, bool) {
	if _, bodyStart, ok := projectmemPurposeHeading(text); ok {
		end := len(text)
		if j := strings.Index(text[bodyStart:], "\n## "); j >= 0 {
			end = bodyStart + j
		}
		updated := text[:bodyStart] + "\n" + desc + text[end:]
		if updated != text {
			return updated, true
		}
		return text, false
	}
	if len(text) > 0 && text[len(text)-1] != '\n' {
		text += "\n"
	}
	offset := len(text)
	if j := strings.Index(text, "\n## "); j >= 0 {
		offset = j + 1
	}
	section := "## Project purpose\n" + desc + "\n\n"
	updated := text[:offset] + section + text[offset:]
	return updated, true
}

// projectmemReadmePurpose returns the first prose paragraph of the README, skipping badges, headings, tables, and link-only lines.
func projectmemReadmePurpose(root string) string {
	for _, name := range []string{"README.md", "README.rst", "README.txt", "README"} {
		raw, ok := util.ReadFileSafe(filepath.Join(root, name))
		if !ok {
			continue
		}
		raw = strings.ReplaceAll(raw, "\r\n", "\n")
		var lines []string
		inFence := false
		for _, line := range strings.Split(raw, "\n") {
			s := strings.TrimSpace(line)
			if strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~") {
				inFence = !inFence
				continue
			}
			if inFence || s == "" {
				if s == "" && len(lines) > 0 {
					break
				}
				continue
			}
			if projectmemReadmeSkipLine(s) {
				continue
			}
			lines = append(lines, s)
		}
		if len(lines) == 0 {
			continue
		}
		return projectmemTruncateWords(strings.Join(lines, " "), 300)
	}
	return ""
}

// projectmemReadmeSkipLine reports non-prose README lines.
func projectmemReadmeSkipLine(s string) bool {
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "!") ||
		strings.HasPrefix(s, "[![") ||
		strings.HasPrefix(s, "<") || strings.HasPrefix(s, "|") ||
		strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") ||
		strings.HasPrefix(s, "+ ") || strings.HasPrefix(s, ".. ") {
		return true
	}
	if len(s) >= 3 && s[0] >= '0' && s[0] <= '9' && s[1] == '.' && s[2] == ' ' {
		return true
	}
	if strings.HasPrefix(s, "[") {
		return linkOnlyLine.MatchString(s)
	}
	return false
}

var linkOnlyLine = regexp.MustCompile(`^(\[[^\]]*\]\([^)]*\)\s*)+$`)

func projectmemTruncateWords(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const marker = "…"
	if max <= 0 {
		return ""
	}
	if max < len(marker) {
		return marker[:max]
	}
	cut := runeSafeCut(s, max-len(marker))
	if i := strings.LastIndex(s[:cut], " "); i >= 80 {
		cut = i
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

// projectmemTruncateTail keeps the TAIL (newest) blocks of text so the returned
// result (including the leading marker) is <= limit bytes.
func projectmemTruncateTail(text string, limit int) string {
	const marker = "…\n"
	if len(text) <= limit {
		return text
	}
	if limit < len(marker) {
		return text[:runeSafeCut(text, limit)]
	}
	lines := strings.Split(text, "\n")
	for len(lines) > 1 && len(strings.Join(lines, "\n")) > limit-len(marker) {
		lines = lines[1:]
	}
	out := strings.Join(lines, "\n")
	room := limit - len(marker)
	if len(out) > room {
		cut := runeSafeCut(out, room)
		if nl := strings.IndexByte(out[cut:], '\n'); nl >= 0 {
			out = out[cut+nl+1:]
		} else {
			out = ""
		}
	}
	if out == "" {
		return marker
	}
	return marker + out
}

// projectmemSectionBody returns the trimmed body of a top-level "## heading"
// section, stopping at the next "## " heading. "" when the heading is absent.
func projectmemSectionBody(text, heading string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimRight(line, " \t") == heading {
			start = i + 1
			continue
		}
		if start >= 0 && strings.HasPrefix(line, "## ") {
			return strings.TrimSpace(strings.Join(lines[start:i], "\n"))
		}
	}
	if start >= 0 {
		return strings.TrimSpace(strings.Join(lines[start:], "\n"))
	}
	return ""
}

// projectmemPlaceholderBody reports whether body is empty or only italic
// template lines ("_..._"), which is how upstream ships unwritten sections.
func projectmemPlaceholderBody(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) < 2 || !strings.HasPrefix(line, "_") || !strings.HasSuffix(line, "_") {
			return false
		}
	}
	return true
}

// projectmemNoneOnly reports whether every non-empty line is one of upstream's
// seeded placeholder bullets ("- None..." or "- No ... logged yet.").
func projectmemNoneOnly(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "- None") &&
			!(strings.HasPrefix(line, "- No ") && strings.HasSuffix(line, "logged yet.")) {
			return false
		}
	}
	return true
}

// projectmemOpenIssues returns up to five "- [OPEN]" lines from summary's Recent issues.
func projectmemOpenIssues(summary string) string {
	body := projectmemSectionBody(summary, "## Recent issues")
	if body == "" {
		return ""
	}
	var open []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- [OPEN]") {
			continue
		}
		if projectmemJunkIssue(line) {
			continue
		}
		open = append(open, line)
		if len(open) == 5 {
			break
		}
	}
	return strings.Join(open, "\n")
}

// projectmemStackBody returns PROJECT_MAP's "## Stack" body minus the
// "- Detected from" line (the digest already carries it) and any placeholder.
func projectmemStackBody(projectMap string) string {
	body := projectmemSectionBody(projectMap, "## Stack")
	if body == "" {
		return ""
	}
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- Detected from") {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	if out == "" || projectmemPlaceholderBody(out) || projectmemNoneOnly(out) {
		return ""
	}
	return out
}

// projectmemJunkIssue reports whether an open-issue line is a harness artifact
// rather than a real defect.
func projectmemJunkIssue(line string) bool {
	title := strings.TrimSpace(strings.TrimPrefix(line, "- [OPEN]"))
	if i := strings.Index(title, " ["); i >= 0 {
		title = title[:i]
	}
	if i := strings.Index(title, " "); i >= 0 {
		if _, err := strconv.Atoi(strings.TrimPrefix(title[:i], "#")); err == nil {
			title = strings.TrimSpace(title[i+1:])
		}
	}
	title = strings.ToLower(strings.Trim(strings.TrimSpace(title), "\"'`.,"))
	if i := strings.Index(title, " ("); i >= 0 {
		title = strings.TrimSpace(title[:i])
	}
	switch title {
	case "placeholder", "review probe", "probe", "test", "tmp", "temp":
		return true
	}
	return false
}

// projectmemNotesBody drops summary notes the digest already carries, then keeps
// the highest-ranked notes that still fit in budget.
func projectmemNotesBody(notes, digest string, budget int) string {
	if notes == "" || projectmemNoneOnly(notes) {
		return ""
	}
	type candidate struct {
		text  string
		rank  int
		order int
	}
	var candidates []candidate
	for _, line := range strings.Split(notes, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if summary := strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")); summary != "" && projectmemDigestCarries(digest, summary) {
			continue
		}
		candidates = append(candidates, candidate{text: trimmed, rank: projectmemNoteRank(trimmed), order: len(candidates)})
	}
	if len(candidates) == 0 {
		return ""
	}
	order := make([]int, len(candidates))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		if candidates[order[a]].rank != candidates[order[b]].rank {
			return candidates[order[a]].rank > candidates[order[b]].rank
		}
		return candidates[order[a]].order > candidates[order[b]].order
	})
	keep := make([]bool, len(candidates))
	used, dropCount := 0, 0
	for _, i := range order {
		marker := len(fmt.Sprintf("- … %d more in summary.md", dropCount+1))
		if used+len(candidates[i].text)+1 > budget-marker {
			dropCount++
			continue
		}
		keep[i] = true
		used += len(candidates[i].text) + 1
	}
	var kept []string
	for i, c := range candidates {
		if keep[i] {
			kept = append(kept, c.text)
		}
	}
	if dropCount > 0 {
		if marker := fmt.Sprintf("- … %d more in summary.md", dropCount); len(marker) <= budget {
			kept = append(kept, marker)
		}
	}
	return strings.Join(kept, "\n")
}

// projectmemNoteRank scores a note for the injection budget.
func projectmemNoteRank(line string) int {
	l := strings.ToLower(line)
	rank := 0
	if strings.Contains(l, "gotcha:") {
		rank += 2
	}
	if strings.Contains(l, "[") && strings.Contains(l, "]") {
		rank++
	}
	return rank
}

// projectmemDigestCarries reports whether digest contains summary at least down to
// upstream's 60-character File Gotchas rendering.
func projectmemDigestCarries(digest, summary string) bool {
	summary = strings.TrimSpace(summary)
	if digest == "" || summary == "" {
		return false
	}
	for _, rendering := range []string{
		summary,
		projectmemSmartTruncate(summary, 60),
		projectmemSmartTruncate(summary, 60) + "…",
	} {
		if len([]rune(rendering)) >= 8 && strings.Contains(digest, rendering) {
			return true
		}
	}
	return false
}

// projectmemSmartTruncate mirrors upstream commands/context.py _smart_truncate:
// sentence boundary, then word boundary, then hard cut.
func projectmemSmartTruncate(text string, limit int) string {
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	head := string(r[:limit])
	for _, sep := range []string{". ", "! ", "? "} {
		if idx := strings.LastIndex(head, sep); idx >= 0 && utf8.RuneCountInString(head[:idx]) >= max(40, limit/2) {
			return head[:idx+1] + " …"
		}
	}
	if idx := strings.LastIndex(head, " "); idx >= 0 && utf8.RuneCountInString(head[:idx]) >= max(20, limit/3) {
		return strings.TrimRight(head[:idx], ",;:—-") + " …"
	}
	return strings.TrimRight(head, " ") + "…"
}

// projectmemIntentSections renders the intent upstream's `pjm context` never
// surfaces: full purpose, plan, open issues/notes/questions, and the stack.
func projectmemIntentSections(root, digest string) string {
	dir := filepath.Join(root, ".projectmem")
	summary, _ := util.ReadFileSafe(filepath.Join(dir, "summary.md"))
	plan, _ := util.ReadFileSafe(filepath.Join(dir, "plan.md"))
	projectMap, _ := util.ReadFileSafe(filepath.Join(dir, "PROJECT_MAP.md"))

	var sections []string
	if p := projectmemSectionBody(summary, "## Project purpose"); p != "" && !projectmemPlaceholderBody(p) && !projectmemNoneOnly(p) {
		sections = append(sections, "#### Project purpose\n"+projectmemTruncateWords(p, projectmemPurposeBudget))
	}
	var planParts []string
	for _, heading := range []string{"## Ideas", "## Active plans", "## Next", "## Someday / maybe"} {
		if body := projectmemSectionBody(plan, heading); body != "" && !projectmemPlaceholderBody(body) && !projectmemNoneOnly(body) {
			planParts = append(planParts, body)
		}
	}
	if len(planParts) > 0 {
		sections = append(sections, "#### Plan\n"+projectmemTruncateWords(strings.Join(planParts, "\n"), projectmemPlanBudget))
	}
	if open := projectmemOpenIssues(summary); open != "" {
		sections = append(sections, "#### Open issues\n"+projectmemTruncateWords(open, projectmemOpenIssuesBudget))
	}
	if notes := projectmemNotesBody(projectmemSectionBody(summary, "## Notes"), digest, projectmemNotesBudget); notes != "" {
		sections = append(sections, "#### Notes\n"+notes)
	}
	if q := projectmemSectionBody(summary, "## Open questions"); q != "" && !projectmemNoneOnly(q) && !projectmemPlaceholderBody(q) {
		sections = append(sections, "#### Open questions\n"+projectmemTruncateWords(q, projectmemQuestionsBudget))
	}
	if st := projectmemStackBody(projectMap); st != "" {
		sections = append(sections, "#### Stack\n"+projectmemTruncateWords(st, projectmemStackBudget))
	}
	if len(sections) == 0 {
		return ""
	}
	out := "### Intent & Notes\n\n" + strings.Join(sections, "\n\n")
	if len(out) > projectmemIntentBudget {
		// Unreachable while the per-section caps plus headings fit the budget;
		// kept as a backstop so a future section cannot silently overflow.
		out = projectmemTruncateTail(out, projectmemIntentBudget)
	}
	return out
}

// Session-text budgets in bytes. The digest gets its own budget so a large
// `pjm context` cannot evict the project-specific sections only tokless injects.
const (
	projectmemGotchasBudget  = 600
	projectmemIntentBudget   = 6500
	projectmemSessionTextCap = 12000
	// Two "\n\n" separators sit between digest, intent, and gotchas.
	projectmemDigestBudget = projectmemSessionTextCap - projectmemIntentBudget - projectmemGotchasBudget - 4
)

// Per-section budgets inside projectmemIntentBudget.
const (
	projectmemPurposeBudget    = 400
	projectmemPlanBudget       = 1200
	projectmemOpenIssuesBudget = 600
	projectmemNotesBudget      = 3200
	projectmemQuestionsBudget  = 400
	projectmemStackBudget      = 200
)

// ProjectmemSessionText is the session-start project state sent on every channel; "" when root has no memory.
func ProjectmemSessionText(root string) string {
	pjm := util.Which("pjm")
	if pjm == "" || root == "" || !util.Exists(filepath.Join(root, ".projectmem")) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, pjm, "context")
	cmd.Dir = root
	cmd.Env = projectmemChildEnv()
	text := ""
	if out, err := cmd.Output(); err == nil {
		text = strings.TrimRight(string(out), "\n")
		if i := strings.Index(text, "\n"); i >= 0 {
			if strings.HasPrefix(text, "## projectmem context (budget:") && strings.TrimSpace(text[i:]) == "" {
				text = ""
			}
		} else if strings.HasPrefix(text, "## projectmem context (budget:") {
			text = ""
		}
		text = projectmemTruncateHead(text, projectmemDigestBudget)
	}
	if intent := projectmemIntentSections(root, text); intent != "" {
		if text == "" {
			text = intent
		} else {
			text += "\n\n" + intent
		}
	}
	if gotchas := projectmemGlobalGotchas(ctx, root); gotchas != "" {
		text += "\n\n" + projectmemTruncateWords(gotchas, projectmemGotchasBudget)
	}
	text = strings.TrimLeft(text, "\n")
	if len(text) <= projectmemSessionTextCap {
		return text
	}
	return projectmemTruncateHead(text, projectmemSessionTextCap)
}

// projectmemTruncateHead keeps the HEAD (leading) text so the result
// (including the trailing marker) is <= limit bytes, marking dropped
// trailing content with a trailing " …".
func projectmemTruncateHead(text string, limit int) string {
	const marker = " …"
	if len(text) <= limit {
		return text
	}
	if limit < len(marker) {
		return text[:runeSafeCut(text, limit)]
	}
	keep := limit - len(marker)
	cut := runeSafeCut(text, keep)
	if cut < len(text) {
		return text[:cut] + marker
	}
	return text
}

// runeSafeCut returns the largest k <= max such that text[k:] is rune-aligned.
func runeSafeCut(text string, max int) int {
	if len(text) <= max {
		return len(text)
	}
	for max > 0 && !utf8.RuneStart(text[max]) {
		max--
	}
	return max
}

// ProjectmemMcpInstructions replaces upstream's initialize instructions; state reaches agents via session-start instead.
var ProjectmemMcpInstructions = "projectmem writer tools: " + strings.Join(agents.ProjectmemMcpToolNames, ", ") + "."

// projectmemGlobalGotchasScript prints cross-project gotchas matching root's stack.
const projectmemGlobalGotchasScript = `import sys
from pathlib import Path
from projectmem.global_memory import detect_stack, get_relevant_entries
gotchas = get_relevant_entries(detect_stack(Path(sys.argv[1])))["gotchas"][-10:]
if gotchas:
    print("### Global gotchas")
    for g in gotchas:
        print(f"- {g.get('library', '')}: {g.get('gotcha', '')}")
`

// projectmemGlobalGotchas replaces the hidden get_global_gotchas tool; "" when none match or the interpreter is unavailable.
func projectmemGlobalGotchas(ctx context.Context, root string) string {
	uv := util.Which("uv")
	if uv == "" {
		return ""
	}
	dir, err := exec.CommandContext(ctx, uv, "tool", "dir").Output()
	if err != nil {
		return ""
	}
	py := filepath.Join(strings.TrimSpace(string(dir)), "projectmem", "bin", "python")
	if runtime.GOOS == "windows" {
		py = filepath.Join(strings.TrimSpace(string(dir)), "projectmem", "Scripts", "python.exe")
	}
	if !util.Exists(py) {
		return ""
	}
	cmd := exec.CommandContext(ctx, py, "-c", projectmemGlobalGotchasScript, root)
	cmd.Env = projectmemChildEnv()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// gitCommonDir resolves root/.git to the shared git dir, handling worktree .git files.
func gitCommonDir(root string) string {
	dot := filepath.Join(root, ".git")
	fi, err := os.Stat(dot)
	if err != nil {
		return ""
	}
	if fi.IsDir() {
		return dot
	}
	raw, err := os.ReadFile(dot)
	if err != nil {
		return ""
	}
	line, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
	if !ok {
		return ""
	}
	gd := strings.TrimSpace(line)
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(root, gd)
	}
	gd = filepath.Clean(gd)
	if cd, ok := util.ReadFileSafe(filepath.Join(gd, "commondir")); ok {
		if dir := strings.TrimSpace(cd); dir != "" {
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(gd, dir)
			}
			return filepath.Clean(dir)
		}
	}
	return gd
}

func gitHookHasProjectmem(root string) bool {
	cd := gitCommonDir(root)
	if cd == "" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(cd, "hooks", "post-commit"))
	return err == nil && bytes.Contains(raw, []byte("projectmem"))
}

func runPjm(dir, pjm string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, pjm, args...)
	cmd.Dir = dir
	cmd.Env = projectmemChildEnv()
	return cmd.CombinedOutput()
}

// projectmemChildEnv drops PROJECTMEM_ROOT so pjm resolves the root from cwd.
func projectmemChildEnv() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "PROJECTMEM_ROOT=") {
			out = append(out, e)
		}
	}
	return out
}

func pjmOutputSnippet(out []byte) string {
	s := strings.TrimSpace(string(out))
	if len(s) > 400 {
		s = s[:400]
	}
	return s
}

// --- manifest ---

var projectmemTools = agents.ProjectmemMcpToolNames

func projectmemEnsureInstalled(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would install projectmem (uv tool install projectmem)")
		return true, nil
	}
	if util.Which("pjm") != "" && util.Which("pjm-mcp") != "" && !opts.Upgrade {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	uv, err := util.EnsureUV(ctx)
	if err != nil {
		return false, fmt.Errorf("%w; install uv, then: uv tool install projectmem", err)
	}
	cmd := exec.CommandContext(ctx, uv, "tool", "install", "projectmem")
	cmd.Env = projectmemChildEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("uv tool install projectmem: %w: %s", err, pjmOutputSnippet(out))
	}
	return util.Which("pjm-mcp") != "", nil
}

// projectmemConfigureMcp writes the MCP entry spawning through `tokless run-mcp --tool projectmem`.
func projectmemConfigureMcp(agent string) bool {
	switch agent {
	case "claude":
		agents.ConfigureClaudeMcp("projectmem")
	case "opencode":
		agents.ConfigureOpenCodeMcp("projectmem")
	case "codex":
		agents.ConfigureCodexMcp("projectmem")
	case "antigravity":
		agents.ConfigureAntigravityMcp("projectmem")
	case "copilot":
		agents.ConfigureCopilotMcp("projectmem")
		agents.ConfigureCopilotIdeMcp("projectmem")
	case "droid":
		agents.ConfigureDroidMcp("projectmem")
	case "pi":
		if !agents.PiRemoveMcpAdapter() {
			return false
		}
		agents.ConfigurePiMcp("projectmem")
	case "omp":
		agents.ConfigureOmpMcp("projectmem")
	case "grok":
		if _, _, err := agents.ConfigureGrokMcp("projectmem"); err != nil {
			return false
		}
	case "cursor":
		agents.ConfigureCursorMcp("projectmem")
		if !agents.CursorMcpHas("projectmem") {
			return false
		}
		return agents.ConfigureCursorMcpPermissions("projectmem")
	case "kilo":
		spawn := util.PickMcpSpawn("projectmem")
		expected := append([]string{spawn.Command}, spawn.Args...)
		if _, _, err := agents.ConfigureKiloMcpSafe("projectmem", expected); err != nil {
			return false
		}
		kiloWriteOwner("projectmem")
	case "cline":
		spawn := util.PickMcpSpawn("projectmem")
		expected := append([]string{spawn.Command}, spawn.Args...)
		if _, _, err := agents.ConfigureClineMcpSafe("projectmem", expected); err != nil {
			return false
		}
	}
	return true
}

// piProjectmemExtension injects project memory once before the first agent turn (Pi and OMP).
const piProjectmemExtension = `import { execFileSync } from "node:child_process"

// projectmem-context-v1
const HOOK = %s

export default function (pi: any) {
  let sent = false
  pi.on("session_start", () => { sent = false })
  pi.on("before_agent_start", async (event: any) => {
    if (sent) return
    sent = true
    try {
      const cwd = event?.systemPromptOptions?.cwd ?? process.cwd()
      const content = execFileSync(HOOK, ["projectmem-hook", "text"], { cwd, input: "", encoding: "utf8", timeout: 15000 }).trim()
      if (content) return { message: { customType: "projectmem", content, display: false } }
    } catch {}
  })
}
`

// kiloProjectmemPlugin (Kilo and OpenCode) appends project memory to the system
// prompt, computed once per session.
const kiloProjectmemPlugin = `import { execFileSync } from "node:child_process"

// projectmem-context-v1
const HOOK = %s
const cache = new Map<string, string>()

const server = async ({ directory }: any) => ({
  "experimental.chat.system.transform": async (input: any, output: any) => {
    const key = input?.sessionID ?? "no-session"
    let content = cache.get(key)
    if (content === undefined) {
      try {
        content = execFileSync(HOOK, ["projectmem-hook", "text"], { cwd: directory ?? process.cwd(), input: "", encoding: "utf8", timeout: 15000 }).trim()
      } catch {
        content = ""
      }
      cache.set(key, content)
    }
    if (content && Array.isArray(output?.system)) output.system.push(content)
  },
})

export default server
`

const projectmemExtensionMarker = "projectmem-context-v1"

func projectmemExtensionPath(agent string) string {
	switch agent {
	case "pi":
		return filepath.Join(agents.PiAgentDirResolved(), "extensions", "projectmem-context.ts")
	case "omp":
		return filepath.Join(agents.OmpAgentDirResolved(), "extensions", "projectmem-context.ts")
	case "kilo":
		return filepath.Join(util.KiloPathsResolved().PluginsDir, "projectmem-context.ts")
	case "opencode":
		return filepath.Join(util.OpenCodePathsResolved().PluginsDir, "projectmem-context.ts")
	}
	return ""
}

func writeProjectmemExtension(agent string) bool {
	path := projectmemExtensionPath(agent)
	exe := util.ToklessPersistedAbs()
	if path == "" || exe == "" || util.EnsureDir(filepath.Dir(path)) != nil {
		return false
	}
	_ = os.Remove(filepath.Join(filepath.Dir(path), "tokless-projectmem.ts"))
	tmpl := piProjectmemExtension
	if agent == "kilo" || agent == "opencode" {
		tmpl = kiloProjectmemPlugin
	}
	return util.WriteFile(path, fmt.Sprintf(tmpl, strconv.Quote(exe))) == nil
}

func hasProjectmemExtension(agent string) bool {
	raw, ok := util.ReadFileSafe(projectmemExtensionPath(agent))
	return ok && strings.Contains(raw, projectmemExtensionMarker)
}

func removeProjectmemExtension(agent string) {
	if hasProjectmemExtension(agent) {
		_ = os.Remove(projectmemExtensionPath(agent))
	}
	if path := projectmemExtensionPath(agent); path != "" {
		_ = os.Remove(filepath.Join(filepath.Dir(path), "tokless-projectmem.ts"))
	}
}

// projectmemRuleFile is the per-project rule file for agents with no session-push channel (Grok, Cline, Cursor).
type projectmemRuleFile struct {
	agent, path, header string
}

func projectmemRuleFiles() []projectmemRuleFile {
	return []projectmemRuleFile{
		{"grok", filepath.Join(".grok", "rules", "projectmem-context.md"), ""},
		{"cline", filepath.Join(".clinerules", "projectmem-context.md"), ""},
		{"cursor", filepath.Join(".cursor", "rules", "projectmem-context.mdc"), "---\ndescription: Project memory state from projectmem.\nalwaysApply: true\n---\n"},
	}
}

// RefreshProjectmemRuleFiles rewrites wired agents' rule files with current state and excludes them from git; runs on MCP server start and exit.
var projectmemRefreshMu sync.Mutex

func RefreshProjectmemRuleFiles(root string) {
	if root == "" || isTest() {
		return
	}
	projectmemRefreshMu.Lock()
	defer projectmemRefreshMu.Unlock()
	var files []projectmemRuleFile
	for _, f := range projectmemRuleFiles() {
		if projectmemRuleFileWired(f.agent) {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return
	}
	text := ProjectmemSessionText(root)
	if text == "" {
		for _, f := range files {
			projectmemRemoveRuleFile(root, f)
		}
		return
	}
	for _, f := range files {
		path := filepath.Join(root, f.path)
		if util.EnsureDir(filepath.Dir(path)) != nil {
			continue
		}
		sweepProjectmemTemps(filepath.Dir(path))
		projectmemRemoveLegacyRuleFile(root, f)
		_ = util.WriteFile(path, f.header+text+"\n")
		projectmemGitExclude(root, filepath.ToSlash(f.path))
	}
}

// sweepProjectmemTemps removes .tokless-write-* files from dead processes; files under a minute old may be active writes.
func sweepProjectmemTemps(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, ".tokless-write-") {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < time.Minute {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// projectmemRemoveRuleFile deletes a tokless-written rule file; foreign content stays.
func projectmemRemoveRuleFile(root string, f projectmemRuleFile) {
	path := filepath.Join(root, f.path)
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return
	}
	if !strings.Contains(raw, "## projectmem context (budget:") {
		return
	}
	_ = os.Remove(path)
}

// projectmemRemoveLegacyRuleFile drops pre-rename tokless-projectmem files next to the current rule file.
func projectmemRemoveLegacyRuleFile(root string, f projectmemRuleFile) {
	dir := filepath.Dir(filepath.Join(root, f.path))
	_ = os.Remove(filepath.Join(dir, "tokless-projectmem"+filepath.Ext(f.path)))
}

func projectmemRuleFileWired(agent string) bool {
	if agent == "cursor" {
		return agents.CursorMcpHas("projectmem")
	}
	return HasOwner(agent, "projectmem")
}

// projectmemGitExclude adds rel to .git/info/exclude when missing.
func projectmemGitExclude(root, rel string) {
	cd := gitCommonDir(root)
	if cd == "" {
		return
	}
	info := filepath.Join(cd, "info")
	if util.EnsureDir(info) != nil {
		return
	}
	path := filepath.Join(info, "exclude")
	raw, _ := util.ReadFileSafe(path)
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "/"+rel {
			return
		}
	}
	if raw != "" && !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	_ = util.WriteFile(path, raw+"/"+rel+"\n")
}

// claudeProjectmemHookArgs identify the Claude Code SessionStart hook.
var claudeProjectmemHookArgs = []string{"projectmem-hook", "claude"}

func claudeProjectmemHookOwned(command string) bool {
	return strings.HasSuffix(strings.TrimSpace(command), strings.Join(claudeProjectmemHookArgs, " "))
}

// claudeProjectmemSessionStart returns ~/.claude/settings.json SessionStart groups minus our hook, plus the parsed config.
func claudeProjectmemSessionStart() (*util.OrderedMap, *util.OrderedMap, []any, bool, bool) {
	raw, _ := util.ReadFileSafe(util.ClaudeCodePaths().Settings)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if strings.TrimSpace(raw) != "" {
			return nil, nil, nil, false, false
		}
		cfg = util.NewOrderedMap()
	}
	hooks, _ := cfg.Get("hooks")
	hm, ok := hooks.(*util.OrderedMap)
	if !ok {
		if hooks != nil {
			return nil, nil, nil, false, false
		}
		hm = util.NewOrderedMap()
	}
	v, _ := hm.Get("SessionStart")
	groups, _ := v.([]any)
	kept := make([]any, 0, len(groups))
	found := false
	for _, g := range groups {
		gm, ok := g.(*util.OrderedMap)
		if !ok {
			kept = append(kept, g)
			continue
		}
		hv, _ := gm.Get("hooks")
		list, _ := hv.([]any)
		if len(list) == 1 {
			if h, ok := list[0].(*util.OrderedMap); ok {
				c, _ := h.Get("command")
				if cmd, _ := c.(string); claudeProjectmemHookOwned(cmd) {
					found = true
					continue
				}
			}
		}
		kept = append(kept, g)
	}
	return cfg, hm, kept, found, true
}

func installClaudeProjectmemHook() bool {
	cfg, hm, kept, _, ok := claudeProjectmemSessionStart()
	if !ok {
		return false
	}
	command := util.PersistedToklessCommand(util.ToklessPersistedAbs(), claudeProjectmemHookArgs...)
	if command == "" {
		return false
	}
	hook := util.NewOrderedMap()
	hook.Set("type", "command")
	hook.Set("command", command)
	hook.Set("timeout", 15)
	group := util.NewOrderedMap()
	group.Set("hooks", []any{hook})
	hm.Set("SessionStart", append(kept, group))
	cfg.Set("hooks", hm)
	return util.WriteFile(util.ClaudeCodePaths().Settings, util.StringifyJSON(cfg)) == nil && hasClaudeProjectmemHook()
}

func hasClaudeProjectmemHook() bool {
	_, _, _, found, ok := claudeProjectmemSessionStart()
	return ok && found
}

func removeClaudeProjectmemHook() {
	cfg, hm, kept, found, ok := claudeProjectmemSessionStart()
	if !ok || !found {
		return
	}
	if len(kept) == 0 {
		hm.Delete("SessionStart")
	} else {
		hm.Set("SessionStart", kept)
	}
	if hm.Len() == 0 {
		cfg.Delete("hooks")
	} else {
		cfg.Set("hooks", hm)
	}
	_ = util.WriteFile(util.ClaudeCodePaths().Settings, util.StringifyJSON(cfg))
}

// projectmemInstallSessionPush wires the session-start channel; Grok, Cline, Cursor load rule files instead.
func projectmemInstallSessionPush(agent string) bool {
	switch agent {
	case "claude":
		return installClaudeProjectmemHook()
	case "codex":
		return agents.InstallCodexProjectmemHook()
	case "droid":
		return agents.InstallDroidProjectmemHook()
	case "copilot":
		return agents.InstallCopilotProjectmemHookSafe() == nil &&
			agents.InstallCopilotIdeProjectmemHookSafe() == nil
	case "antigravity":
		return agents.InstallAntigravityProjectmemHook()
	case "pi", "omp", "kilo", "opencode":
		return writeProjectmemExtension(agent)
	}
	return true
}

func projectmemHasSessionPush(agent string) bool {
	switch agent {
	case "claude":
		return hasClaudeProjectmemHook()
	case "codex":
		return agents.HasCodexProjectmemHook()
	case "droid":
		return agents.HasDroidProjectmemHook()
	case "copilot":
		return agents.HasCopilotProjectmemHook() && agents.HasCopilotIdeProjectmemHook()
	case "antigravity":
		return agents.HasAntigravityProjectmemHook()
	case "pi", "omp", "kilo", "opencode":
		return hasProjectmemExtension(agent)
	}
	return true
}

func projectmemRemoveSessionPush(agent string) {
	switch agent {
	case "claude":
		removeClaudeProjectmemHook()
	case "codex":
		agents.RemoveCodexProjectmemHook()
	case "droid":
		agents.RemoveDroidProjectmemHook()
	case "copilot":
		_ = agents.RemoveCopilotProjectmemHookSafe()
		_ = agents.RemoveCopilotIdeProjectmemHookSafe()
	case "antigravity":
		agents.RemoveAntigravityProjectmemHook()
	case "pi", "omp", "kilo", "opencode":
		removeProjectmemExtension(agent)
	}
}

func projectmemVerify(agent string) bool {
	switch agent {
	case "claude":
		return claudeMcpBounded("projectmem")
	case "opencode":
		return openCodeMcpBounded("projectmem")
	case "codex":
		cx := util.CodexPathsResolved()
		raw, ok := util.ReadFileSafe(cx.Config)
		if !ok {
			return false
		}
		blockText, hasBlock := util.BlockText(raw, "mcp_servers.projectmem")
		if !hasBlock {
			return false
		}
		b := parseCodexContextModeBlock(blockText)
		spawn := util.McpSpawnFor("projectmem")
		return b.command == spawn.Command && mcpStrArrEq(b.args, spawn.Args)
	case "antigravity":
		return agents.AntigravityProjectmemMcpBounded()
	case "copilot":
		return agents.CopilotProjectmemMcpBounded(false) && agents.CopilotProjectmemMcpBounded(true)
	case "droid":
		return agents.DroidProjectmemMcpBounded()
	case "pi":
		return agents.PiProjectmemMcpBounded()
	case "omp":
		return agents.OmpMcpHas("projectmem")
	case "grok":
		return agents.GrokProjectmemMcpHas() && HasOwner("grok", "projectmem")
	case "cursor":
		return agents.CursorMcpHas("projectmem") && agents.HasCursorMcpPermissions("projectmem")
	case "kilo":
		spawn := util.PickMcpSpawn("projectmem")
		return agents.KiloMcpMatches("projectmem", append([]string{spawn.Command}, spawn.Args...)) && kiloHasOwner("projectmem")
	case "cline":
		spawn := util.PickMcpSpawn("projectmem")
		return agents.ClineMcpMatches("projectmem", append([]string{spawn.Command}, spawn.Args...)) && HasOwner("cline", "projectmem")
	}
	return false
}

func claudeMcpBounded(toolID string) bool {
	raw, ok := util.ReadFileSafe(util.ClaudeCodePaths().GlobalJSON)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if s, ok := cfg.Get("mcpServers"); ok {
		if sm, ok := s.(*util.OrderedMap); ok {
			v, has := sm.Get(toolID)
			em, isMap := v.(*util.OrderedMap)
			if !has || !isMap {
				return false
			}
			spawn := util.McpSpawnFor(toolID)
			cmd, _ := em.Get("command")
			args, _ := em.Get("args")
			return cmd == spawn.Command && mcpStrArrEq(args, spawn.Args)
		}
	}
	return false
}

func openCodeMcpBounded(toolID string) bool {
	raw, ok := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if m, ok := cfg.Get("mcp"); ok {
		if mm, ok := m.(*util.OrderedMap); ok {
			v, has := mm.Get(toolID)
			em, isMap := v.(*util.OrderedMap)
			if !has || !isMap {
				return false
			}
			if en, exists := em.Get("enabled"); exists {
				if b, isBool := en.(bool); isBool && !b {
					return false
				}
			}
			spawn := util.McpSpawnFor(toolID)
			cmd, _ := em.Get("command")
			return mcpStrArrEq(cmd, append([]string{spawn.Command}, spawn.Args...))
		}
	}
	return false
}

// mcpStrArrEq reports whether v is a string array equal to want.
func mcpStrArrEq(v any, want []string) bool {
	arr, ok := v.([]any)
	if !ok || len(arr) != len(want) {
		return false
	}
	for i, x := range arr {
		s, isStr := x.(string)
		if !isStr || s != want[i] {
			return false
		}
	}
	return true
}

var projectmem = &core.ToolManifest{
	ID:           "projectmem",
	Label:        "ProjectMem",
	Description:  "Per-project memory: issues, attempts, fixes, decisions, auto-captured from git.",
	Homepage:     "https://github.com/riponcm/projectmem",
	InstallHint:  "uv tool install projectmem",
	Channel:      core.ChannelUV,
	Install:      projectmemEnsureInstalled,
	IndexProject: projectmemIndexProject,
	IndexReady:   func() bool { return isTest() || util.Which("pjm") != "" },
	WireFor:      map[string]core.AgentFn{},
	UnwireFor:    map[string]core.AgentFn{},
	VerifyFor:    map[string]core.VerifyFn{},
}

func projectmemIndexProject(dir string, opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would: pjm init --no-mcp-config --no-claude-md")
		return true, nil
	}
	if err := EnsureProjectmemProject(dir); err != nil {
		return false, err
	}
	if isTest() {
		return true, nil
	}
	return util.Exists(filepath.Join(dir, ".projectmem")), nil
}

func init() {
	for _, agent := range []string{"claude", "opencode", "codex", "cursor", "antigravity", "copilot", "droid", "grok", "pi", "omp", "kilo", "cline"} {
		a := agent
		projectmem.WireFor[a] = func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				util.L.Sub("[dry-run] would add projectmem MCP to " + a)
				return true, nil
			}
			if !projectmemConfigureMcp(a) || !projectmemInstallSessionPush(a) {
				return false, nil
			}
			if a != "kilo" {
				WriteOwner(a, "projectmem")
				if a == "copilot" && HasOwner("copilot", "projectmem") {
					agents.SyncCopilotIdeInstructions()
				}
			}
			if !projectmemVerify(a) {
				util.L.Sub("projectmem MCP missing or foreign for " + a + "; remove any existing projectmem entry (or fix a malformed config) and re-run tokless")
				return false, nil
			}
			return true, nil
		}
		projectmem.UnwireFor[a] = func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				return true, nil
			}
			switch a {
			case "claude":
				agents.RemoveClaudeMcp("projectmem")
			case "opencode":
				agents.RemoveOpenCodeMcp("projectmem")
			case "codex":
				cx := util.CodexPathsResolved()
				raw, _ := util.ReadFileSafe(cx.Config)
				if next := util.RemoveBlock(raw, "mcp_servers.projectmem"); next != raw {
					_ = util.WriteFile(cx.Config, next)
				}
			case "antigravity":
				agents.RemoveAntigravityMcp("projectmem")
			case "copilot":
				agents.RemoveCopilotMcp("projectmem")
				agents.RemoveCopilotIdeMcp("projectmem")
			case "droid":
				agents.RemoveDroidMcp("projectmem")
			case "pi":
				agents.RemovePiMcp("projectmem")
			case "omp":
				agents.RemoveOmpMcp("projectmem")
			case "grok":
				_, _ = agents.RemoveGrokMcp("projectmem")
			case "cursor":
				agents.RemoveCursorMcp("projectmem")
				agents.RemoveCursorMcpPermissions("projectmem")
			case "kilo":
				agents.RemoveKiloMcp("projectmem")
			case "cline":
				agents.RemoveClineMcp("projectmem")
			}
			projectmemRemoveSessionPush(a)
			projectmemRemoveAgentRuleFile(a)
			if a == "kilo" {
				kiloRemoveOwner("projectmem")
			} else {
				RemoveOwner(a, "projectmem")
				if a == "copilot" {
					agents.SyncCopilotIdeInstructions()
				}
			}
			return true, nil
		}
		agentID := a
		projectmem.VerifyFor[agentID] = func() *bool {
			// Copilot's IDE side is project-scoped; outside a project only user-level config is checked.
			if agentID == "copilot" && !agents.IdeInProject() {
				return core.BoolPtr(agents.CopilotProjectmemMcpBounded(false) && agents.HasCopilotProjectmemHook())
			}
			return core.BoolPtr(projectmemVerify(agentID) && projectmemHasSessionPush(agentID))
		}
	}
}

// projectmemRemoveAgentRuleFile deletes only tokless-written rule files, walking up to the project root.
func projectmemRemoveAgentRuleFile(agent string) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	for dir := cwd; ; {
		for _, f := range projectmemRuleFiles() {
			if f.agent != agent {
				continue
			}
			if raw, ok := util.ReadFileSafe(filepath.Join(dir, f.path)); ok &&
				strings.Contains(raw, "## projectmem context (budget:") {
				_ = os.Remove(filepath.Join(dir, f.path))
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}
