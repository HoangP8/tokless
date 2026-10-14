package tools

import "testing"

func TestRTKLocationFor(t *testing.T) {
	for _, id := range []string{"amp", "zcode", "dsh", "claude", "opencode", "codex", "cursor", "antigravity", "copilot", "droid", "pi", "omp", "kilo", "cline", "grok"} {
		path, label := RTKLocationFor(id)
		if path == "" || label == "" {
			t.Errorf("RTKLocationFor(%q) = (%q, %q), want location", id, path, label)
		}
	}
	if path, label := RTKLocationFor("kimi"); path != "" || label != "" {
		t.Fatalf("RTKLocationFor(kimi) = (%q, %q), want empty location", path, label)
	}
}
