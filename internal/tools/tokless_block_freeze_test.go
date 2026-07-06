package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

func TestWriteOwnerDuringCaptureLogs(t *testing.T) {
	ConfigureInstructionConflicts(false)

	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	userContent := "# My Project\n\nMy custom instructions for ponytail.\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var wrote bool
	var logs string
	go func() {
		logs, _ = util.CaptureLogs(func() error {
			cur, _ := util.ReadFileSafe(path)
			wrote = writeOwnerInPath(path, cur, "caveman")
			return nil
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writeOwnerInPath blocked inside CaptureLogs")
	}

	if !wrote {
		t.Fatalf("writeOwnerInPath returned false; expected append. logs=%q", logs)
	}

	got, _ := os.ReadFile(path)
	gotStr := string(got)

	if !strings.HasPrefix(gotStr, "# My Project") {
		t.Fatalf("user content overwritten. got head:\n%s", gotStr[:min(80, len(gotStr))])
	}
	if !strings.Contains(gotStr, "My custom instructions for ponytail.") {
		t.Fatal("user content lost")
	}
	if !strings.Contains(gotStr, "caveman") {
		t.Fatal("caveman block not appended")
	}
}

func TestWriteOwnerSecondToolDoesNotReprompt(t *testing.T) {
	ConfigureInstructionConflicts(false)

	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	userContent := "# Project\n\nExisting user notes.\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _ = util.CaptureLogs(func() error {
		cur, _ := util.ReadFileSafe(path)
		writeOwnerInPath(path, cur, "caveman")
		return nil
	})

	done := make(chan struct{})
	var wrote bool
	go func() {
		_, _ = util.CaptureLogs(func() error {
			cur, _ := util.ReadFileSafe(path)
			wrote = writeOwnerInPath(path, cur, "ponytail")
			return nil
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("second writeOwnerInPath blocked")
	}

	if !wrote {
		t.Fatal("second write returned false")
	}

	got, _ := os.ReadFile(path)
	gotStr := string(got)
	if !strings.HasPrefix(gotStr, "# Project") {
		t.Fatal("user head lost on second write")
	}
	if !strings.Contains(gotStr, "caveman") || !strings.Contains(gotStr, "ponytail") {
		t.Fatal("both owner blocks not present")
	}
	if strings.Count(gotStr, "## Caveman") > 1 {
		t.Fatal("caveman block duplicated")
	}
}