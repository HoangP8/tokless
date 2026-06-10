package tools

import (
	"os"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

func writeIfMissing(path, content string) {
	if !util.Exists(path) {
		_ = util.WriteFile(path, content)
	}
}

// clip trims and caps stderr for log lines (mirrors .slice(0,200)).
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func isTest() bool { return os.Getenv("TOKLESS_TEST") == "1" }
