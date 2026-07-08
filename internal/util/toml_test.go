package util

import (
	"strings"
	"testing"
)

// Path-keyed TOML table sites.
var tomlPathKeyedSites = []struct{ prefix, path string }{
	{"hooks.state", `C:\Users\user\.codex\hooks.json:pre_tool_use:0:0`},
}

func TestTomlPathKeyedBlocksSingleQuoted(t *testing.T) {
	for _, c := range tomlPathKeyedSites {
		hdr := TomlDottedTableHeader(c.prefix, c.path)
		if strings.Contains(hdr, `"`) {
			t.Fatalf("dotted header must be single-quoted, got %q", hdr)
		}
		block := NewTomlBlock(hdr)
		block.Set("trusted_hash", "deadbeef")
		out := UpsertBlock("", block, false)
		if strings.Contains(out, c.prefix+`."`) {
			t.Fatalf("double-quoted path key in TOML for %q:\n%s", c.prefix, out)
		}
	}
}

func TestUpsertBlockInsert(t *testing.T) {
	block := NewTomlBlock("mcp_servers.codegraph")
	block.Set("command", "codegraph")
	block.Set("args", []string{"serve", "--mcp"})

	got := UpsertBlock("", block, false)
	if !strings.Contains(got, "[mcp_servers.codegraph]") {
		t.Errorf("expected header [mcp_servers.codegraph], got: %q", got)
	}
	if !strings.Contains(got, `command = "codegraph"`) {
		t.Errorf("expected command field, got: %q", got)
	}
	if !strings.Contains(got, `args = ["serve", "--mcp"]`) {
		t.Errorf("expected args field, got: %q", got)
	}
}

func TestUpsertBlockIdempotent(t *testing.T) {
	b1 := NewTomlBlock("mcp_servers.codegraph")
	b1.Set("command", "codegraph")
	b1.Set("args", []string{"serve", "--mcp"})

	b2 := NewTomlBlock("features")
	b2.Set("hooks", true)

	b3 := NewTomlBlock("mcp_servers.context-mode")
	b3.Set("command", "context-mode")

	doc := UpsertBlock("", b1, false)
	doc = UpsertBlock(doc, b2, true)
	doc = UpsertBlock(doc, b3, false)

	doc2 := UpsertBlock(doc, b1, false)
	doc2 = UpsertBlock(doc2, b2, true)
	doc2 = UpsertBlock(doc2, b3, false)

	if doc != doc2 {
		t.Errorf("Expected idempotency, but doc changed:\nOriginal:\n%s\nSecond:\n%s", doc, doc2)
	}
}

func TestRemoveBlock(t *testing.T) {
	b1 := NewTomlBlock("mcp_servers.codegraph")
	b1.Set("command", "codegraph")
	b2 := NewTomlBlock("mcp_servers.context-mode")
	b2.Set("command", "context-mode")

	doc := UpsertBlock("", b1, false)
	doc = UpsertBlock(doc, b2, false)

	removed := RemoveBlock(doc, "mcp_servers.codegraph")
	if strings.Contains(removed, "[mcp_servers.codegraph]") {
		t.Errorf("expected [mcp_servers.codegraph] to be removed, got:\n%s", removed)
	}
	if !strings.Contains(removed, "[mcp_servers.context-mode]") {
		t.Errorf("expected [mcp_servers.context-mode] to remain, got:\n%s", removed)
	}
}

func TestHasBlock(t *testing.T) {
	doc := "[mcp_servers.codegraph]\ncommand = \"codegraph\""
	if !HasBlock(doc, "mcp_servers.codegraph") {
		t.Error("expected to have block mcp_servers.codegraph")
	}
	if HasBlock(doc, "mcp_servers.context-mode") {
		t.Error("expected to not have block mcp_servers.context-mode")
	}
}

func TestUpsertBlockMerge(t *testing.T) {
	b := NewTomlBlock("mcp_servers.codegraph")
	b.Set("command", "codegraph")
	doc := UpsertBlock("", b, false)

	b2 := NewTomlBlock("mcp_servers.codegraph")
	b2.Set("args", []string{"serve"})
	merged := UpsertBlock(doc, b2, true)

	if !strings.Contains(merged, `command = "codegraph"`) {
		t.Errorf("expected pre-existing command to be preserved, got:\n%s", merged)
	}
	if !strings.Contains(merged, `args = ["serve"]`) {
		t.Errorf("expected new args to be added, got:\n%s", merged)
	}
}
