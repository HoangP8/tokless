package util

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPickMinGitAsset(t *testing.T) {
	tests := []struct {
		name     string
		assets   []ghAsset
		wantURL  string
		wantBool bool
	}{
		{
			name: "has busybox",
			assets: []ghAsset{
				{Name: "Git-2.47.0-64-bit.exe", URL: "exe_url"},
				{Name: "MinGit-2.47.0-64-bit.zip", URL: "zip_url"},
				{Name: "MinGit-2.47.0-busybox-64-bit.zip", URL: "busybox_url"},
			},
			wantURL:  "busybox_url",
			wantBool: true,
		},
		{
			name: "only plain zip",
			assets: []ghAsset{
				{Name: "Git-2.47.0-64-bit.exe", URL: "exe_url"},
				{Name: "MinGit-2.47.0-64-bit.zip", URL: "zip_url"},
			},
			wantURL:  "zip_url",
			wantBool: true,
		},
		{
			name: "neither",
			assets: []ghAsset{
				{Name: "Git-2.47.0-64-bit.exe", URL: "exe_url"},
			},
			wantURL:  "",
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, ok := pickMinGitAsset(tt.assets)
			if url != tt.wantURL || ok != tt.wantBool {
				t.Errorf("pickMinGitAsset() = (%v, %v), want (%v, %v)", url, ok, tt.wantURL, tt.wantBool)
			}
		})
	}
}

func TestGitInstallDir(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("LOCALAPPDATA", temp)
	want := filepath.Join(temp, "tokless", "git")
	got := gitInstallDir()
	if got != want {
		t.Errorf("gitInstallDir() = %v, want %v", got, want)
	}
}

func TestExtractZipFlat(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "test.zip")
	destDir := filepath.Join(t.TempDir(), "dest")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	w1, err := zw.Create("cmd/git.exe")
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w1, "g")

	w2, err := zw.Create("mingw64/bin/x")
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w2, "y")

	w3, err := zw.Create("../evil")
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w3, "bad")

	zw.Close()
	f.Close()

	err = extractZipFlat(zipPath, destDir)
	if err != nil {
		t.Fatalf("extractZipFlat failed: %v", err)
	}

	gitPath := filepath.Join(destDir, "cmd", "git.exe")
	b, err := os.ReadFile(gitPath)
	if err != nil || string(b) != "g" {
		t.Errorf("cmd/git.exe missing or invalid content")
	}

	xPath := filepath.Join(destDir, "mingw64", "bin", "x")
	b, err = os.ReadFile(xPath)
	if err != nil || string(b) != "y" {
		t.Errorf("mingw64/bin/x missing or invalid content")
	}

	evilPath := filepath.Join(destDir, "..", "evil")
	if _, err := os.Stat(evilPath); err == nil {
		t.Errorf("Directory traversal vulnerability: ../evil was extracted")
	}
	evilDestPath := filepath.Join(destDir, "evil")
	if _, err := os.Stat(evilDestPath); err == nil {
		t.Errorf("evil was extracted inside dest but shouldn't be")
	}
}
