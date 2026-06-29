package tools

import (
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
	}
	return ""
}

// legacyBlockHeadings lists old `## Heading` sections that no longer belong
// in the unified body. The rtk section was removed in the unified-body
// migration; rtk is hook-only (PreToolUse), no body text needed.
var legacyBlockHeadings = []string{"## Process Noise"}

var legacyFences = [][2]string{
	{"<!-- caveman-begin -->", "<!-- caveman-end -->"},
	{"<!-- CODEGRAPH_START -->", "<!-- CODEGRAPH_END -->"},
	{"<!-- CONTEXT-MODE_START -->", "<!-- CONTEXT-MODE_END -->"},
	{"<!-- tokless:owners=", ""},
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

// stripLegacyHeading removes a `## Heading` block (the heading line plus
// body until the next `## ` heading or end of file) and trims surrounding
// blank lines.
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

// fileParts splits raw into the lines preceding any managed section
// (head), the contiguous managed sections (each anchored by a known
// `## Heading`), and the trailing lines (tail). Lines are kept verbatim.
//
// A managed block is the contiguous run of lines starting at an owner
// heading and ending at the next owner heading (or EOF). All body lines
// of the last owner belong to that block — not to tail.
func fileParts(raw string) (head []string, blocks []managedSection, tail []string) {
	lines := strings.Split(raw, "\n")
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

// ownerOf returns the owner id for a `## Heading` line that matches a
// known section heading, or "" otherwise.
func ownerOf(line string) string {
	if !strings.HasPrefix(line, "## ") {
		return ""
	}
	for o, m := range util.SectionsByOwner {
		if m != "" && line == m {
			return o
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

// RemoveOwner removes owner's section. When no owners remain, removes file.
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
	owners := ownersFromBlocks(blocks)
	if containsOwner(owners, owner) {
		return false
	}
	owners = append(owners, owner)
	sort.Strings(owners)
	body := strings.TrimRight(util.ToklessBody(owners), "\n")
	return util.WriteFile(path, joinFile(head, body, tail)) == nil
}

func removeOwnerInPath(path, cur, owner string) {
	cleaned := stripLegacy(cur)
	head, blocks, tail := fileParts(cleaned)
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
	if len(kept) == 0 {
		s := joinFile(head, "", tail)
		if strings.TrimSpace(s) == "" || strings.TrimSpace(s) == "# Notes\n\nkeep me" {
			_ = os.Remove(path)
			return
		}
		_ = util.WriteFile(path, strings.TrimRight(s, "\n")+"\n")
		return
	}
	body := strings.TrimRight(util.ToklessBody(kept), "\n")
	_ = util.WriteFile(path, joinFile(head, body, tail))
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
