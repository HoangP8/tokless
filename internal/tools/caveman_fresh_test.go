package tools

import (
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestRegisterCavemanOpencode(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("OPENCODE_CONFIG_DIR", tempDir)

	registerCavemanOpencode()

	op := util.OpenCodePathsResolved()
	raw, ok := util.ReadFileSafe(op.Config)
	if !ok {
		t.Fatalf("config file was not created at %q", op.Config)
	}

	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("could not parse created config")
	}

	pv, ok := cfg.Get("plugin")
	if !ok {
		t.Fatalf("config missing 'plugin' array")
	}

	arr, ok := pv.([]any)
	if !ok {
		t.Fatalf("'plugin' is not an array")
	}

	has := false
	for _, p := range arr {
		if s, ok := p.(string); ok && strings.Contains(strings.ToLower(s), "caveman") {
			has = true
			break
		}
	}
	if !has {
		t.Errorf("plugin array missing caveman entry")
	}
}
