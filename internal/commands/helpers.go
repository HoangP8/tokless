package commands

import (
	"os"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	toolsPkg "github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

const repoURL = "https://github.com/HoangP8/tokless"
const issuesURL = repoURL + "/issues"

func toolVersionNote(tool *core.ToolManifest) string {
	if tool.NotTrackable {
		if v := util.LatestVersionFor(tool.ID); v != nil {
			return "v" + *v
		}
		return ""
	}
	if v := util.InstalledVersionFor(tool.ID); v != nil {
		return "v" + *v
	}
	if tool.Channel == core.ChannelNpm {
		if v := util.LatestVersionFor(tool.ID); v != nil {
			return "v" + *v
		}
	}
	return ""
}

func toolNeedsNode(tool *core.ToolManifest) bool {
	if tool.InstructionOnly {
		return false
	}
	if tool.ID == "context-mode" && !toolsPkg.CtxNeedsGlobalCLI() {
		return false
	}
	return tool.NeedsNode || tool.Channel == core.ChannelNpm || tool.MinNodeMajor > 0
}

// ctxNodeShortfall returns the Node floor when a context-mode install would run and no Node meets it.
func ctxNodeShortfall(tools []*core.ToolManifest, upgrade bool) (minNode string, needs bool) {
	if os.Getenv("TOKLESS_TEST") == "1" || !toolsPkg.CtxNeedsGlobalCLI() {
		return "", false
	}
	has := false
	for _, t := range tools {
		has = has || t.ID == "context-mode"
	}
	if !has {
		return "", false
	}
	ver, minNode, err := util.ContextModeLatest()
	if err != nil {
		return "", false
	}
	if !upgrade {
		if rt, err := util.ResolveContextMode(); err == nil && !rt.Degraded && util.VersionMajor(rt.Version) >= util.VersionMajor(ver) {
			return "", false
		}
	}
	if _, err := util.EnsureNodeAtLeast(minNode, false); err == nil {
		return "", false
	}
	return minNode, true
}

// nodeInstallNotice announces a private Node install and reports whether it is allowed.
func nodeInstallNotice(tools []*core.ToolManifest, upgrade, nodeOK bool) bool {
	if !nodeOK {
		return false
	}
	minNode, needs := ctxNodeShortfall(tools, upgrade)
	if !needs {
		return false
	}
	prev := util.Quiet()
	util.SetQuiet(false)
	util.L.Sub("context-mode needs Node >= " + minNode + "; installing a private copy under " + util.PortableRuntimeDir())
	util.SetQuiet(prev)
	return true
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i != -1 {
		return s[:i]
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return "1 tool"
	}
	return itoa(n) + " tool(s)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func joinComma(ss []string) string { return strings.Join(ss, ", ") }

const failureDetailLines = 12

func printFailureDetail(logs map[string]string) {
	for label, out := range logs {
		lines := lastUniqueLines(out, failureDetailLines)
		if len(lines) == 0 {
			continue
		}
		util.TreeLeaf("  " + util.C.Gray(label+":"))
		for _, ln := range lines {
			util.TreeLeaf("    " + util.C.Gray(ln))
		}
	}
}

// treeSub prints a gray bullet row under the tree stem.
func treeSub(msg string) {
	util.TreeLeaf(util.C.Gray(util.Sym.Bullet) + " " + msg)
}

// lastUniqueLines keeps the last n distinct non-empty lines, in original order.
func lastUniqueLines(s string, n int) []string {
	lines := lastNonEmptyLines(s, -1)
	seen := map[string]bool{}
	var keep []string
	for i := len(lines) - 1; i >= 0 && len(keep) < n; i-- {
		if !seen[lines[i]] {
			seen[lines[i]] = true
			keep = append(keep, lines[i])
		}
	}
	for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
		keep[i], keep[j] = keep[j], keep[i]
	}
	return keep
}

func lastNonEmptyLines(s string, n int) []string {
	var keep []string
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(stripAnsi(ln)); t != "" {
			keep = append(keep, t)
		}
	}
	if n >= 0 && len(keep) > n {
		keep = keep[len(keep)-n:]
	}
	return keep
}

// stripAnsi removes ANSI/control sequences so captured progress output reads
// as plain text when reprinted.
func stripAnsi(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i++; i < len(s); i++ {
				c := s[i]
				if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
					break
				}
			}
			continue
		}
		if s[i] == '\r' {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func padEnd(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// cmdHeader prints the standard "tokless <cmd>  subtitle" banner.
func cmdHeader(cmd, subtitle string) {
	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold(util.C.Cyan("tokless "+cmd)) + util.C.Gray("  "+subtitle))
	util.L.Raw("")
}

// treeStatus prints a final "Status" branch with one leaf per line, then closes.
func treeStatus(lines ...string) {
	if len(lines) == 0 {
		return
	}
	util.TreeCorner("Status")
	for _, line := range lines {
		util.TreeLeaf(line)
	}
	util.TreeClose()
}

// treeStatusLast prints the final Status branch without extending the tree.
func treeStatusLast(lines ...string) {
	if len(lines) == 0 {
		return
	}
	util.TreeCornerLast("Status")
	for _, line := range lines {
		util.TreeBottomLeaf(line)
	}
}

// Palette helpers for status / version trees.
func paintVer(v string) string  { return util.C.Soft(v) }
func paintName(s string) string { return util.C.Bold(s) }
func paintPath(s string) string { return util.C.Gray(s) }
func paintKey(s string) string  { return util.C.Cyan(s) }
func paintCmd(s string) string  { return util.C.Bold(util.C.Cyan(s)) }
func paintArrow() string        { return util.C.Dim("→") }

// statusOK / statusWarn / statusInfo — consistent Status leaf styling.
func statusOK(msg string) string {
	return util.C.Green(util.Sym.Check) + " " + util.C.Green(msg)
}
func statusWarn(msg string) string {
	return util.C.Yellow(util.Sym.Warn) + " " + util.C.Yellow(msg)
}
func statusInfo(msg string) string {
	return util.C.Cyan(util.Sym.Info) + " " + msg
}

// statusKV is "✔ key   value" with a fixed key column.
func statusKV(mark, key, value string) string {
	return mark + " " + paintKey(padEnd(key, 8)) + value
}

func printRepoFooter(tree bool) {
	if osEnvTest() {
		return
	}
	if tree {
		util.TreeFooter(52)
		util.L.Raw("  " + util.C.Bold(util.C.Gray("If tokless helps, please star it here: ")) + util.C.Cyan(repoURL))
		util.L.Raw("  " + util.C.Bold(util.C.Gray("If you hit any issue or have ideas, please raise it here: ")) + util.C.Cyan(issuesURL))
		return
	}
	util.L.Raw("")
	util.L.Raw(util.C.Gray(util.Rule(52)))
	util.L.Raw("  " + util.C.Gray("If tokless helps, please star it here: ") + util.C.Blue(repoURL))
	util.L.Raw("  " + util.C.Gray("If you hit any issue or have ideas, please raise it here: ") + util.C.Blue(issuesURL))
}

func osEnvTest() bool { return os.Getenv("TOKLESS_TEST") == "1" }
