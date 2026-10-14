package util

import (
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
		})
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
