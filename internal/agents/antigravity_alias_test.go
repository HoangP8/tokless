package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// A legacy MCP path aliasing the primary/mirror file must not delete what was just written.
func TestConfigureAntigravityMcpSurvivesAliasedLegacyFile(t *testing.T) {
	cases := map[string]func(t *testing.T, p util.AntigravityPaths, mirror string){
		"file symlink to primary": func(t *testing.T, p util.AntigravityPaths, mirror string) {
			mustLink(t, p.McpConfigCLI, p.McpConfig)
		},
		"file symlink to mirror": func(t *testing.T, p util.AntigravityPaths, mirror string) {
			mustLink(t, mirror, p.McpConfig)
		},
		"dir symlink": func(t *testing.T, p util.AntigravityPaths, mirror string) {
			if err := os.RemoveAll(p.Dir); err != nil {
				t.Fatal(err)
			}
			mustLink(t, filepath.Dir(p.McpConfigCLI), p.Dir)
		},
		"settings symlink to primary": func(t *testing.T, p util.AntigravityPaths, mirror string) {
			mustLink(t, p.McpConfigCLI, p.Settings)
		},
	}
	for name, alias := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			util.SetHomeOverride(home)
			defer util.SetHomeOverride("")
			t.Setenv("PATH", t.TempDir())

			p := util.AntigravityPathsResolved()
			mirror := antigravityMcpMirrorFile()
			for _, f := range []string{p.McpConfigCLI, mirror} {
				if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f, []byte(`{"mcpServers":{"user":{"command":"keep"}}}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(p.Dir, 0o755); err != nil {
				t.Fatal(err)
			}
			alias(t, p, mirror)

			tools := []string{"codegraph", "context-mode", "projectmem"}
			var first [2]string
			for run := 0; run < 2; run++ {
				for _, tool := range tools {
					ConfigureAntigravityMcp(tool)
				}
				for i, f := range []string{p.McpConfigCLI, mirror} {
					raw, _ := os.ReadFile(f)
					if run == 0 {
						first[i] = string(raw)
					} else if string(raw) != first[i] {
						t.Fatalf("second run changed %s:\n%s\n---\n%s", f, first[i], raw)
					}
				}
			}
			for i, f := range []string{p.McpConfigCLI, mirror} {
				for _, want := range append([]string{"user"}, tools...) {
					if !strings.Contains(first[i], `"`+want+`"`) {
						t.Fatalf("%s lost %s through aliased legacy file:\n%s", f, want, first[i])
					}
				}
			}
		})
	}
}

func mustLink(t *testing.T, target, link string) {
	t.Helper()
	_ = os.Remove(link)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
}
