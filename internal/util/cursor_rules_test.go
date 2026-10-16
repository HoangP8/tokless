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

func TestInstallCursorProjectRulesRespectsSelection(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Cleanup(func() { SetHomeOverride("") })
	if err := SaveSelection("selected", []string{"principles"}); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if ok, err := InstallCursorProjectRules(workspace, false); err != nil || !ok {
		t.Fatalf("install selected rule: ok=%v err=%v", ok, err)
	}
	rulesDir := filepath.Join(workspace, ".cursor", "rules")
	for _, spec := range CursorProjectRuleSpecs() {
		_, err := os.Stat(filepath.Join(rulesDir, spec.Filename))
		if spec.Owner == "principles" && err != nil {
			t.Errorf("selected rule missing: %v", err)
		}
		if spec.Owner != "principles" && !os.IsNotExist(err) {
			t.Errorf("unselected rule %s was installed", spec.Owner)
		}
	}
	generated := filepath.Join(rulesDir, "response-style.mdc")
	if err := os.WriteFile(generated, []byte(CursorProjectRuleContent(CursorProjectRuleSpecs()[1])), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(rulesDir, "user-rule.mdc")
	if err := os.WriteFile(foreign, []byte("user authored"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSelection("selected", []string{"principles"}); err != nil {
		t.Fatal(err)
	}
	if ok, err := InstallCursorProjectRules(workspace, false); err != nil || !ok {
		t.Fatalf("remove deselected rule: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatal("generated deselected rule should be removed")
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != "user authored" {
		t.Fatalf("foreign rule changed: %q, %v", got, err)
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
