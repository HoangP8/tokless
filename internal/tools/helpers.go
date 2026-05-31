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

func getOrCreateMapT(m *util.OrderedMap, key string) *util.OrderedMap {
	if v, ok := m.Get(key); ok {
		if om, ok := v.(*util.OrderedMap); ok {
			return om
		}
	}
	om := util.NewOrderedMap()
	m.Set(key, om)
	return om
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
