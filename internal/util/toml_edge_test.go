package util

import "testing"

func TestTomlMergePreservesKeys(t *testing.T) {
	src := "[features]\nexisting_key = true\nother = \"keep\"\n"
	b := NewTomlBlock("features")
	b.Set("hooks", true)
	out := UpsertBlock(src, b, true)
	for _, want := range []string{"existing_key", "other", "hooks = true"} {
		if !contains(out, want) {
			t.Fatalf("merge dropped %q:\n%s", want, out)
		}
	}
	if out2 := UpsertBlock(out, b, true); out != out2 {
		t.Fatalf("merge not idempotent:\n1:%q\n2:%q", out, out2)
	}
}

func TestTomlEOFAppendIdempotent(t *testing.T) {
	src := "[a]\nx = 1"
	b := NewTomlBlock("b")
	b.Set("y", 2)
	o1 := UpsertBlock(src, b, false)
	o2 := UpsertBlock(o1, b, false)
	if o1 != o2 {
		t.Fatalf("EOF append not idempotent:\n1:%q\n2:%q", o1, o2)
	}
}

func TestTomlArrayFormat(t *testing.T) {
	b := NewTomlBlock("s")
	b.Set("args", []string{"serve", "--mcp"})
	out := UpsertBlock("", b, false)
	if !contains(out, `args = ["serve", "--mcp"]`) {
		t.Fatalf("bad array format:\n%s", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
