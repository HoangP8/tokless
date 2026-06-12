package commands

import (
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

func printFailureDetail(logs map[string]string) {
	for label, out := range logs {
		lines := lastNonEmptyLines(out, 4)
		if len(lines) == 0 {
			continue
		}
		util.L.Raw("      " + util.C.Gray(label+":"))
		for _, ln := range lines {
			util.L.Raw("        " + util.C.Gray(ln))
		}
	}
}

func lastNonEmptyLines(s string, n int) []string {
	var keep []string
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(stripAnsi(ln)); t != "" {
			keep = append(keep, t)
		}
	}
	if len(keep) > n {
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
