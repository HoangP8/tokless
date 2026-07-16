package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

func instructionPath(agent string) string {
	switch agent {
	case "claude":
		return util.ClaudeCodePaths().Instructions
	case "opencode":
		return util.OpenCodePathsResolved().Instructions
	case "codex":
		return util.CodexPathsResolved().Instructions
	case "antigravity":
		return util.AntigravityPathsResolved().Instructions
	case "copilot":
		return util.CopilotPathsResolved().Instructions
	case "droid":
		return filepath.Join(util.Home(), ".factory", "AGENTS.md")
	}
	return ""
}

// legacyBlockHeadings lists old sections removed by unified-body migration.
var legacyBlockHeadings = []string{"## Process Noise"}

var legacyFences = [][2]string{
	{"<!-- caveman-begin -->", "<!-- caveman-end -->"},
	{"<!-- CODEGRAPH_START -->", "<!-- CODEGRAPH_END -->"},
	{"<!-- CONTEXT-MODE_START -->", "<!-- CONTEXT-MODE_END -->"},
	{"<!-- tokless:owners=", ""},
}

var instructionConflict = struct {
	autoAppend bool
	appendAll  bool
	skipped    map[string]bool
}{skipped: map[string]bool{}}

func ConfigureInstructionConflicts(autoAppend bool) {
	instructionConflict.autoAppend = autoAppend
	instructionConflict.appendAll = false
	instructionConflict.skipped = map[string]bool{}
}

func stripLegacy(raw string) string {
	for _, f := range legacyFences {
		if f[1] == "" {
			for {
				i := strings.Index(raw, f[0])
				if i < 0 {
					break
				}
				j := strings.Index(raw[i:], " -->")
				if j < 0 {
					raw = raw[:i]
					break
				}
				j += i + len(" -->")
				for j < len(raw) && raw[j] == '\n' {
					j++
				}
				raw = raw[:i] + raw[j:]
			}
			continue
		}
		for {
			i := strings.Index(raw, f[0])
			if i < 0 {
				break
			}
			j := strings.Index(raw[i:], f[1])
			if j < 0 {
				break
			}
			j = i + j + len(f[1])
			if i > 0 && raw[i-1] == '\n' {
				i--
			}
			for j < len(raw) && raw[j] == '\n' {
				j++
			}
			raw = raw[:i] + raw[j:]
		}
	}
	for _, h := range legacyBlockHeadings {
		raw = stripLegacyHeading(raw, h)
	}
	return raw
}

// stripLegacyHeading removes a `## Heading` block and surrounding blank lines.
func stripLegacyHeading(raw, heading string) string {
	for {
		i := strings.Index(raw, heading)
		if i < 0 {
			return raw
		}
		// back up to start of line
		start := i
		if start > 0 && raw[start-1] != '\n' {
			// not at line start; skip — likely a mid-line mention
			return raw
		}
		// forward to end of block: next `## ` line or EOF
		end := len(raw)
		rest := raw[i+len(heading):]
		for k := 0; k < len(rest); k++ {
			if rest[k] == '\n' {
				peek := rest[k+1:]
				if strings.HasPrefix(peek, "## ") {
					end = i + len(heading) + k + 1
					break
				}
			}
		}
		// trim trailing blank lines from the removed range
		for end > start && (raw[end-1] == '\n' || raw[end-1] == ' ') {
			if raw[end-1] == '\n' {
				end--
			} else {
				// collapse spaces into a single trim pass
				break
			}
		}
		// also trim one preceding blank line so we don't leave a gap
		if start > 0 && raw[start-1] == '\n' {
			peek := start - 2
			for peek > 0 && raw[peek] == '\n' {
				peek--
			}
			if peek >= 0 {
				start = peek + 1
			}
		}
		raw = raw[:start] + raw[end:]
	}
}

// fileParts splits raw into head, managed sections, and tail.
func fileParts(raw string) (head []string, blocks []managedSection, tail []string) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n")
	ownerIdx := make([]int, 0)
	for i, line := range lines {
		if ownerOf(line) != "" {
			ownerIdx = append(ownerIdx, i)
		}
	}
	if len(ownerIdx) == 0 {
		return lines, nil, nil
	}
	head = append([]string(nil), lines[:ownerIdx[0]]...)
	// Last block runs to EOF; earlier blocks run to the next owner heading.
	for i, start := range ownerIdx {
		end := len(lines)
		if i+1 < len(ownerIdx) {
			end = ownerIdx[i+1]
		}
		bs := blocksFromLines(lines[start:end])
		blocks = append(blocks, bs...)
	}
	return
}

type managedSection struct {
	owner string
	lines []string
}

func blocksFromLines(lines []string) []managedSection {
	var out []managedSection
	var cur *managedSection
	for _, line := range lines {
		if o := ownerOf(line); o != "" {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &managedSection{owner: o, lines: []string{line}}
			continue
		}
		if cur != nil {
			cur.lines = append(cur.lines, line)
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

// ownerOf returns owner id for a known heading.
func ownerOf(line string) string {
	line = strings.TrimRight(line, "\r")
	for _, o := range util.ToklessOwners {
		for _, marker := range util.SectionMarkers(o) {
			if line == marker {
				return o
			}
		}
	}
	return ""
}

// WriteOwner wires owner into the agent's unified body. Idempotent.
func WriteOwner(agent, owner string) bool {
	path := instructionPath(agent)
	if path == "" {
		return false
	}
	_ = util.EnsureDir(filepath.Dir(path))
	cur, _ := util.ReadFileSafe(path)
	return writeOwnerInPath(path, cur, owner)
}

// RemoveOwner removes owner's section; removes file when empty.
func RemoveOwner(agent, owner string) {
	path := instructionPath(agent)
	if path == "" {
		return
	}
	cur, ok := util.ReadFileSafe(path)
	if !ok {
		return
	}
	removeOwnerInPath(path, cur, owner)
}

// HasOwner reports whether owner appears in the managed body.
func HasOwner(agent, owner string) bool {
	path := instructionPath(agent)
	if path == "" {
		return false
	}
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return false
	}
	return hasOwnerInRaw(raw, owner)
}

func writeOwnerInPath(path, cur, owner string) bool {
	cleaned := stripLegacy(cur)
	head, blocks, tail := fileParts(cleaned)
	head = stripIndexPreamble(head)
	owners := ownersFromBlocks(blocks)
	if len(owners) == 0 && strings.TrimSpace(cleaned) != "" {
		switch instructionConflictChoice(path) {
		case "skip":
			return false
		case "overwrite":
			cleaned = ""
			head, blocks, tail = fileParts(cleaned)
			owners = ownersFromBlocks(blocks)
		}
	}
	if containsOwner(owners, owner) {
		sortOwnersByRegistry(owners)
		want := strings.TrimRight(util.ToklessAgentBody(owners), "\n")
		current := joinManagedBlocks(blocks)
		if current == want {
			return false
		}
		return util.WriteFile(path, joinFile(head, want, tail)) == nil
	}
	owners = append(owners, owner)
	sortOwnersByRegistry(owners)
	body := strings.TrimRight(util.ToklessAgentBody(owners), "\n")
	return util.WriteFile(path, joinFile(head, body, tail)) == nil
}

func instructionConflictChoice(path string) string {
	if instructionConflict.skipped[path] {
		return "skip"
	}
	if instructionConflict.autoAppend || instructionConflict.appendAll || !util.IsInteractive() || !util.StdoutIsTTY() || os.Getenv("TOKLESS_TEST") == "1" {
		return "append"
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "  "+util.C.Yellow(util.Sym.Warn)+" "+path+" already has content.")
	switch util.SelectOne("Handle existing instructions", []util.SelectOption{
		{Value: "overwrite", Label: "Overwrite", Hint: "recommended", Selected: true},
		{Value: "append", Label: "Append"},
	}) {
	case "overwrite":
		return "overwrite"
	}
	return "append"
}

// joinManagedBlocks reconstructs the rendered text of existing managed blocks.
func joinManagedBlocks(blocks []managedSection) string {
	var b strings.Builder
	for i, blk := range blocks {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.Join(blk.lines, "\n"))
	}
	return b.String()
}

func sortOwnersByRegistry(owners []string) {
	order := make(map[string]int, len(util.ToklessOwners))
	for i, o := range util.ToklessOwners {
		order[o] = i
	}
	sort.Slice(owners, func(i, j int) bool {
		return order[owners[i]] < order[owners[j]]
	})
}

func removeOwnerInPath(path, cur, owner string) {
	cleaned := stripLegacy(cur)
	head, blocks, tail := fileParts(cleaned)
	head = stripIndexPreamble(head)
	owners := ownersFromBlocks(blocks)
	if !containsOwner(owners, owner) {
		return
	}
	kept := make([]string, 0, len(owners))
	for _, o := range owners {
		if o != owner {
			kept = append(kept, o)
		}
	}
	sortOwnersByRegistry(kept)
	if len(kept) == 0 {
		trimmed := stripIndexPreamble(head)
		s := joinFile(trimmed, "", tail)
		if strings.TrimSpace(s) == "" {
			_ = os.Remove(path)
			return
		}
		_ = util.WriteFile(path, strings.TrimRight(s, "\n")+"\n")
		return
	}
	body := strings.TrimRight(util.ToklessAgentBody(kept), "\n")
	_ = util.WriteFile(path, joinFile(head, body, tail))
}

// stripIndexPreamble drops overview when last owner is removed.
func stripIndexPreamble(head []string) []string {
	for i, line := range head {
		trimmed := strings.TrimSpace(line)
		if (trimmed == "# Agent Instructions" || trimmed == "# Agent Operating System" || trimmed == "## Index" || trimmed == "## Index →") && isToklessIndexPreamble(head[i:]) {
			return head[:i]
		}
	}
	return head
}

func isToklessIndexPreamble(lines []string) bool {
	body := strings.Join(lines, "\n")
	return strings.Contains(body, "- **Principles**") || strings.Contains(body, "- **Response Style") || strings.Contains(body, "- **Code Index")
}

func writeOwnerAtPath(path, owner string) {
	cur, _ := util.ReadFileSafe(path)
	writeOwnerInPath(path, cur, owner)
}

func hasOwnerAtPath(path, owner string) bool {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return false
	}
	return hasOwnerInRaw(raw, owner)
}

func removeOwnerAtPath(path, owner string) {
	cur, ok := util.ReadFileSafe(path)
	if !ok {
		return
	}
	removeOwnerInPath(path, cur, owner)
}

func hasOwnerInRaw(raw, owner string) bool {
	cleaned := stripLegacy(raw)
	_, blocks, _ := fileParts(cleaned)
	for _, b := range blocks {
		if b.owner == owner {
			return true
		}
	}
	return false
}

func ownersFromBlocks(blocks []managedSection) []string {
	var out []string
	for _, b := range blocks {
		if b.owner == "principles" {
			continue
		}
		out = append(out, b.owner)
	}
	return out
}

func containsOwner(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// joinFile renders head + body + tail into a single markdown file with a
// single blank line between each region.
func joinFile(head []string, body string, tail []string) string {
	headStr := strings.TrimRight(strings.Join(head, "\n"), "\n")
	tailStr := strings.TrimLeft(strings.Join(tail, "\n"), "\n")
	switch {
	case headStr == "" && body == "" && tailStr == "":
		return ""
	case body == "":
		return joinEmpty(headStr, tailStr)
	case headStr == "" && tailStr == "":
		return body + "\n"
	case headStr == "":
		return body + "\n\n" + tailStr
	case tailStr == "":
		return headStr + "\n\n" + body + "\n"
	default:
		return headStr + "\n\n" + body + "\n\n" + tailStr
	}
}

func joinEmpty(head, tail string) string {
	switch {
	case head == "" && tail == "":
		return ""
	case head == "":
		return tail
	case tail == "":
		return head
	default:
		return head + "\n\n" + tail
	}
}
