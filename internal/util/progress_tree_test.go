package util

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestTreeCornerAndLeaf(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	TreeTop("Tools")
	TreeLeaf(C.Green(Sym.Check) + " RTK")
	TreeClose()
	TreeCorner("Agents")
	TreeLeaf(C.Green(Sym.Check) + " Claude Code")
	TreeClose()
	TreeCornerStyled(C.Bold("Run ") + C.Cyan("tokless update"))
	TreeLeaf(C.Yellow("↑") + " codegraph")
	TreeFooter(8)
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	s := stripTestANSI(string(out))
	for _, want := range []string{"+- Tools", "|   ", "RTK", "+- Agents", "Claude Code", "Run ", "tokless update", "codegraph", "+--------"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

func TestRootSectionProgressNonTTYUsesTreeTop(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	p := NewRootSectionProgress("Tools")
	p.tty = false
	p.Start(1)
	p.Begin("RTK")
	p.Complete("")
	p.Done("")
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	s := stripTestANSI(string(out))
	if !strings.Contains(s, "+- Tools") || !strings.Contains(s, "|   ") {
		t.Fatalf("expected root corner + stem:\n%s", s)
	}
}

func stripTestANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i++; i < len(s); i++ {
				if s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' {
					break
				}
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestSectionProgressNonTTYUsesTreeCorner(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	p := NewSectionProgress("Tools")
	p.tty = false
	p.Start(1)
	p.Begin("RTK")
	p.Complete("")
	p.Done("")
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	s := stripTestANSI(string(out))
	if !strings.Contains(s, "+- Tools") || !strings.Contains(s, "|   ") || strings.Count(s, "|") < 2 {
		t.Fatalf("expected corner + stem trunk:\n%s", s)
	}
}
func TestTreeNodeGlyphFirstIsRoot(t *testing.T) {
	oldGlyphs, oldOpened := glyphsEnabled, treeOpened
	defer func() { glyphsEnabled, treeOpened = oldGlyphs, oldOpened }()
	glyphsEnabled, treeOpened = true, false
	if got := treeNodeGlyph(); got != "┌─ " {
		t.Fatalf("first node = %q, want ┌─", got)
	}
	if got := treeNodeGlyph(); got != "├─ " {
		t.Fatalf("second node = %q, want ├─", got)
	}
}

func TestSectionProgressFirstNodeIsRoot(t *testing.T) {
	oldGlyphs, oldOpened := glyphsEnabled, treeOpened
	defer func() { glyphsEnabled, treeOpened = oldGlyphs, oldOpened }()
	glyphsEnabled, treeOpened = true, false
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	for _, title := range []string{"Tools", "Agents"} {
		p := NewSectionProgress(title)
		p.tty = false
		p.Start(0)
		p.Done("")
	}
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	s := stripTestANSI(string(out))
	if !strings.Contains(s, "┌─ Tools") || !strings.Contains(s, "├─ Agents") || strings.Count(s, "┌─") != 1 {
		t.Fatalf("expected one root then mid node:\n%s", s)
	}
}
