package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func writeHealthyCodegraph(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo 1.2.3; exit 0; fi
if [ "$1" = "serve" ] && [ "$2" = "--mcp" ]; then
  while IFS= read -r line; do
    case "$line" in
      *'"id":1'*|*'"id": 1'*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"codegraph","version":"1.2.3"}}}' ;;
      *'"id":2'*|*'"id": 2'*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"codegraph_explore"}]}}'; exit 0 ;;
    esac
  done
fi
exit 1
`
	os.WriteFile(path, []byte(script), 0755)
}

func TestCodegraphConfigureMcp(t *testing.T) {
	tempDir := t.TempDir()
	binDir := t.TempDir()
	writeHealthyCodegraph(t, filepath.Join(binDir, "codegraph"))
	t.Setenv("PATH", binDir)
	t.Setenv("OPENCODE_CONFIG_DIR", tempDir)
	t.Setenv("TOKLESS_TEST", "1")

	codegraphConfigureMcp("opencode")
	op := util.OpenCodePathsResolved()

	if !codegraphVerify("opencode") {
		t.Errorf("codegraphVerify() returned false after configuration")
	}

	if !util.Exists(op.Config) {
		t.Errorf("config file was not created at %q", op.Config)
	}
}
