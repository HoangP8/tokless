package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallCursorProjectRules(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "project with spaces")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	ok, err := InstallCursorProjectRules(workspace, false)
	if err != nil || !ok {
		t.Fatalf("install rules: ok=%v err=%v", ok, err)
	}
	for _, spec := range CursorProjectRuleSpecs() {
		path := filepath.Join(workspace, ".cursor", "rules", spec.Filename)
		want := CursorProjectRuleContent(spec)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", spec.Filename, err)
		}
		if string(got) != want {
			t.Errorf("%s content mismatch", spec.Filename)
		}
	}
	if ok, err := InstallCursorProjectRules(workspace, false); err != nil || !ok {
		t.Fatalf("idempotent install: ok=%v err=%v", ok, err)
	}
}

func TestInstallCursorProjectRulesDryRunAndKeepExisting(t *testing.T) {
	workspace := t.TempDir()
	if ok, err := InstallCursorProjectRules(workspace, true); err != nil || !ok {
		t.Fatalf("dry-run: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".cursor")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created project files")
	}

	rulesDir := filepath.Join(workspace, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(rulesDir, CursorProjectRuleSpecs()[0].Filename)
	const authored = "user-authored"
	if err := os.WriteFile(path, []byte(authored), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := InstallCursorProjectRules(workspace, false); err != nil || !ok {
		t.Fatalf("install over existing: ok=%v err=%v", ok, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rule: %v", err)
	}
	if string(got) != authored {
		t.Fatalf("existing rule was overwritten: %q", got)
	}
}

func TestInstallCursorProjectRulesRequiresWorkspace(t *testing.T) {
	if ok, err := InstallCursorProjectRules("", false); err == nil || ok {
		t.Fatal("empty workspace was accepted")
	}
}
