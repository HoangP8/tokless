package commands

import (
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

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
	return ""
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

func itoa(n int) string { return strconv.Itoa(n) }

func joinComma(ss []string) string { return strings.Join(ss, ", ") }

func padEnd(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
