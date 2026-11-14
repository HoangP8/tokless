package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserConfigDirMirrorsDirsCrate(t *testing.T) {
	oldGoos := goosForUserConfig
	defer func() { goosForUserConfig = oldGoos }()

	home := t.TempDir()
	SetHomeOverride(home)
	defer SetHomeOverride("")

	goosForUserConfig = "windows"
	if got, want := UserConfigDir(), filepath.Join(home, "AppData", "Roaming"); got != want {
		t.Fatalf("windows = %q want %q", got, want)
	}
	goosForUserConfig = "darwin"
	if got, want := UserConfigDir(), filepath.Join(home, "Library", "Application Support"); got != want {
		t.Fatalf("darwin = %q want %q", got, want)
	}
	goosForUserConfig = "linux"
	if got, want := UserConfigDir(), filepath.Join(home, ".config"); got != want {
		t.Fatalf("linux = %q want %q", got, want)
	}
}

func TestUserConfigDirIgnoresRelativeXDG(t *testing.T) {
	oldGoos := goosForUserConfig
	defer func() { goosForUserConfig = oldGoos }()
	goosForUserConfig = "linux"
	t.Setenv("XDG_CONFIG_HOME", "relative/path")

	home := t.TempDir()
	SetHomeOverride(home)
	defer SetHomeOverride("")

	if got, want := UserConfigDir(), filepath.Join(home, ".config"); got != want {
		t.Fatalf("relative XDG honoured: %q want %q", got, want)
	}
}

func TestUserConfigDirUsesXDGWhenAbsolute(t *testing.T) {
	oldGoos := goosForUserConfig
	defer func() { goosForUserConfig = oldGoos }()
	goosForUserConfig = "linux"

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	SetHomeOverride("")
	if info, err := os.Stat(xdg); err != nil || !info.IsDir() {
		t.Fatalf("xdg dir: %v", err)
	}
	if got := UserConfigDir(); got != xdg {
		t.Fatalf("XDG_CONFIG_HOME = %q want %q", got, xdg)
	}
}

func TestUserConfigDirWindowsUsesAppData(t *testing.T) {
	oldGoos := goosForUserConfig
	defer func() { goosForUserConfig = oldGoos }()
	goosForUserConfig = "windows"

	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	SetHomeOverride("")
	if got := UserConfigDir(); got != appData {
		t.Fatalf("APPDATA = %q want %q", got, appData)
	}
}

func TestUserConfigDirDarwinIgnoresXDG(t *testing.T) {
	oldGoos := goosForUserConfig
	defer func() { goosForUserConfig = oldGoos }()
	goosForUserConfig = "darwin"
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	home := t.TempDir()
	SetHomeOverride(home)
	defer SetHomeOverride("")

	if got, want := UserConfigDir(), filepath.Join(home, "Library", "Application Support"); got != want {
		t.Fatalf("darwin honoured XDG: %q want %q", got, want)
	}
}
