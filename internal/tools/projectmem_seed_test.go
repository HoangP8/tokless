package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestProjectmemSetMapPurposeInsertsBeforeSection(t *testing.T) {
	in := "# Project Map - x\n\nStatus: auto-detected.\n\n## Stack\n- Tags: go\n"
	got, changed := projectmemSetMapPurpose(in, "Real purpose.")
	if !changed {
		t.Fatal("expected change")
	}
	want := "# Project Map - x\n\nStatus: auto-detected.\n\n## Project purpose\nReal purpose.\n\n## Stack\n- Tags: go\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestProjectmemSetMapPurposeReplacesPlaceholder(t *testing.T) {
	in := "# Map\n\n## Project purpose\nNot described yet.\n\n## Structure\n- `a`\n"
	got, changed := projectmemSetMapPurpose(in, "Desc.")
	if !changed {
		t.Fatal("expected change")
	}
	body, ok := projectmemMapPurpose(got)
	if !ok || body != "Desc." {
		t.Fatalf("body=%q ok=%v", body, ok)
	}
}

func TestProjectmemPurposePlaceholder(t *testing.T) {
	for _, s := range []string{"Not described yet.", "", "  ", "Replace this placeholder with a concise description."} {
		if !projectmemPurposePlaceholder(s) {
			t.Fatalf("%q should be placeholder", s)
		}
	}
	if projectmemPurposePlaceholder("Authored by human.") {
		t.Fatal("authored text flagged placeholder")
	}
}

func TestProjectmemReadmePurposeSkipsBadges(t *testing.T) {
	dir := t.TempDir()
	readme := "# T\n\n[![ci](https://x/y.svg)](https://x)\n\nFirst prose line.\nMore prose.\n\n## Install\nrun it\n"
	if err := util.WriteFile(filepath.Join(dir, "README.md"), readme); err != nil {
		t.Fatal(err)
	}
	got := projectmemReadmePurpose(dir)
	if got != "First prose line. More prose." {
		t.Fatalf("got %q", got)
	}
}

func TestProjectmemTruncateWords(t *testing.T) {
	long := "word "
	for i := 0; i < 400; i++ {
		long += "w "
	}
	got := projectmemTruncateWords(long, 300)
	if len(got) > 302 || !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q len=%d", got, len(got))
	}
}

func TestProjectmemSetMapPurposeAppendAddsNewline(t *testing.T) {
	in := "# Map\n\nStatus: auto-detected."
	got, changed := projectmemSetMapPurpose(in, "Desc.")
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(got, "Status: auto-detected.\n## Project purpose\n") {
		t.Fatalf("heading glued to previous line: %q", got)
	}
}

func TestProjectmemSeedPurposeKeepsMixedContent(t *testing.T) {
	body := "Not described yet.\n\nHuman wrote this extra line."
	if projectmemPurposePlaceholder(body) {
		t.Fatal("placeholder + authored line must not be placeholder")
	}
}

func TestProjectmemSetMapPurposeKeepsRemainder(t *testing.T) {
	in := "# Map\n\n## Project purpose\nNot described yet.\n\n## Structure\n- `a`\n"
	got, _ := projectmemSetMapPurpose(in, "Desc.")
	if !strings.Contains(got, "## Structure\n- `a`") {
		t.Fatalf("remainder lost: %q", got)
	}
}

func TestProjectmemReadmePurposeSkipsFenceAndBullets(t *testing.T) {
	dir := t.TempDir()
	readme := "# T\n\n- fast\n- small\n\n```bash\nnpm install\n```\n\nIntro sentence here.\n\n"
	if err := util.WriteFile(filepath.Join(dir, "README.md"), readme); err != nil {
		t.Fatal(err)
	}
	if got := projectmemReadmePurpose(dir); got != "Intro sentence here." {
		t.Fatalf("got %q", got)
	}
}

func TestProjectmemReadmePurposeKeepsLinkLedProse(t *testing.T) {
	dir := t.TempDir()
	readme := "# T\n\n[project](https://x) is the tool that does things.\nSecond prose line.\n\n"
	if err := util.WriteFile(filepath.Join(dir, "README.md"), readme); err != nil {
		t.Fatal(err)
	}
	want := "[project](https://x) is the tool that does things. Second prose line."
	if got := projectmemReadmePurpose(dir); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestProjectmemReadmePurposeCRLF(t *testing.T) {
	dir := t.TempDir()
	readme := "# T\r\n\r\nProse with CRLF.\r\n\r\n"
	if err := util.WriteFile(filepath.Join(dir, "README.md"), readme); err != nil {
		t.Fatal(err)
	}
	if got := projectmemReadmePurpose(dir); got != "Prose with CRLF." {
		t.Fatalf("got %q", got)
	}
}

func TestProjectmemTruncateWordsRuneSafe(t *testing.T) {
	s := strings.Repeat("héllö wörld ", 60)
	got := projectmemTruncateWords(s, 300)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q", got)
	}
	for _, r := range got {
		if r == '�' {
			t.Fatalf("rune split: %q", got)
		}
	}
}

func TestProjectmemPurposeHeadingNotNested(t *testing.T) {
	in := "# Map\n\n### Project purpose\nnested body\n\n## Project purpose\nreal body\n"
	body, ok := projectmemMapPurpose(in)
	if !ok || body != "real body" {
		t.Fatalf("body=%q ok=%v", body, ok)
	}
}
