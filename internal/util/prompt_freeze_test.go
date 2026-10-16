package util

import (
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPromptNonBlockingWhenStdoutPiped(t *testing.T) {
	realOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = realOut
		w.Close()
		r.Close()
	}()

	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := r.Read(buf); err != nil {
				break
			}
		}
		close(done)
	}()

	got := SelectOne("pick", []SelectOption{
		{Value: "a", Label: "A"},
		{Value: "b", Label: "B", Selected: true},
	})
	if got != "b" {
		t.Fatalf("SelectOne = %q, want %q", got, "b")
	}

	ms := MultiSelect("pick", []MultiSelectOption{
		{Value: "x", Label: "X", Selected: true},
		{Value: "y", Label: "Y"},
	})
	if len(ms) != 1 || ms[0] != "x" {
		t.Fatalf("MultiSelect = %v, want [x]", ms)
	}

	if !Confirm("ok?", true) {
		t.Fatal("Confirm(true) = false, want true")
	}
	if Confirm("ok?", false) {
		t.Fatal("Confirm(false) = true, want false")
	}

	os.Stdout = realOut
	w.Close()
	<-done
}

func TestSettleMultiSelectTagColumn(t *testing.T) {
	realOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	write := func() {
		settleMultiSelect("Select agents", []MultiSelectOption{
			{Value: "claude", Label: "Claude Code", Selected: true},
			{Value: "opencode", Label: "OpenCode"},
			{Value: "codex", Label: "Codex", Disabled: true},
		}, true)
	}

	done := make(chan string)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()

	write()
	w.Close()
	out := <-done
	os.Stdout = realOut
	r.Close()

	ansi := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	var tagCols []int
	for _, line := range strings.Split(out, "\n") {
		plain := ansi.ReplaceAllString(line, "")
		if i := strings.Index(plain, "[READY]"); i >= 0 {
			tagCols = append(tagCols, utf8.RuneCountInString(plain[:i]))
		}
		if i := strings.Index(plain, "[MISSING]"); i >= 0 {
			tagCols = append(tagCols, utf8.RuneCountInString(plain[:i]))
		}
	}
	if len(tagCols) != 3 {
		t.Fatalf("want 3 tag rows, got %d in:\n%s", len(tagCols), out)
	}
	for i := 1; i < len(tagCols); i++ {
		if tagCols[i] != tagCols[0] {
			t.Fatalf("tag columns misaligned: %v (col 0=%d)\n%s", tagCols, tagCols[0], out)
		}
	}

	plain := ansi.ReplaceAllString(out, "")
	if !strings.Contains(plain, "Claude Code") {
		t.Fatalf("selected label missing:\n%s", plain)
	}
	before := plain[:strings.Index(plain, "Claude Code")]
	if !strings.HasSuffix(strings.TrimRight(before, " "), "✓") {
		t.Fatalf("checkmark not adjacent to label; prefix=%q", before)
	}
}

func TestMultiSelectStatusOption(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func()
		want bool
	}{
		{"tool selection", func() {
			multiSelectLine("Select tools to install", []MultiSelectOption{{Value: "x", Label: "Tool", Selected: true}}, false)
		}, false},
		{"regular prompt", func() {
			multiSelectLine("Select agents", []MultiSelectOption{{Value: "x", Label: "Agent", Selected: true}}, true)
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := capturePromptOutput(t, tc.call)
			if got := strings.Contains(out, "[READY]"); got != tc.want {
				t.Fatalf("READY tag present = %v, want %v; output:\n%s", got, tc.want, out)
			}
		})
	}
}

func capturePromptOutput(t *testing.T, call func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	call()
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestToolSelectionRowsHideReadyAndNestUnderSelected(t *testing.T) {
	items := []MultiSelectOption{{Value: "rtk", Label: "RTK", Selected: true}, {Value: "context", Label: "Context"}}
	out := renderToolSelectionRows(items, 1)
	plain := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`).ReplaceAllString(out, "")
	if strings.Contains(plain, "[READY]") || !strings.Contains(plain, "Full (Default)") || !strings.Contains(plain, "Selected") || !strings.Contains(plain, "RTK") || !strings.Contains(plain, "Context") || strings.Index(plain, "RTK") > strings.Index(plain, "Context") {
		t.Fatalf("tool rows should nest under Selected without READY labels: %q", plain)
	}
}

func TestRenderSelectOneUsesStableCRLFLayout(t *testing.T) {
	options := []SelectOption{
		{Value: "full", Label: "Full install", Hint: "default"},
		{Value: "selected", Label: "Selected tools"},
	}
	out := renderSelectOne("Install tools", options, 1, "*", "-", 8, ">", "up/down move · enter confirm", true)
	plain := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`).ReplaceAllString(out, "")
	lines := strings.Split(strings.TrimSuffix(plain, "\r\n"), "\r\n")
	if len(lines) != len(options)+3 {
		t.Fatalf("got %d lines, want %d: %q", len(lines), len(options)+3, plain)
	}
	for _, line := range lines {
		if strings.Contains(line, "\n") {
			t.Fatalf("line contains bare LF: %q", line)
		}
	}
	if !strings.Contains(lines[2], ">") || !strings.Contains(lines[2], "Selected tools") {
		t.Fatalf("active choice not clear: %q", lines[2])
	}
	if !strings.Contains(lines[0], "Install tools") || !strings.Contains(lines[4], "up/down move · enter confirm") {
		t.Fatalf("title or key instructions missing: %q", plain)
	}
}

func TestRenderSelectOneRedrawAndSettleControls(t *testing.T) {
	options := []SelectOption{{Label: "A"}, {Label: "B"}}
	menu := renderSelectOne("Pick", options, 0, "*", "-", 8, ">", "enter confirm", true)
	if !strings.HasPrefix(menu, "\x1b[0J") {
		t.Fatalf("redraw missing clear-to-end: %q", menu)
	}
	if !selectOneFitsTerminal("Pick", options, 8) {
		t.Fatal("short menu should fit terminal")
	}

	realOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	settleSelectOne("Pick", options, 1, true)
	w.Close()
	os.Stdout = realOut
	b, _ := io.ReadAll(r)
	r.Close()
	if !strings.HasPrefix(string(b), "\r\x1b[6A\x1b[0J") || !strings.Contains(string(b), "B") {
		t.Fatalf("settle did not replace menu with selected output: %q", b)
	}
}
