package util

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNodeLTSVersion(t *testing.T) {
	entries := []nodeDistEntry{
		{
			Version: "v23.0.0",
			Files:   []string{"win-x64-zip", "win-arm64-zip"},
			LTS:     json.RawMessage("false"),
		},
		{
			Version: "v22.1.0",
			Files:   []string{"win-x64-zip"},
			LTS:     json.RawMessage(`"Jod"`),
		},
		{
			Version: "v20.0.0",
			Files:   []string{"win-x64-zip", "win-arm64-zip"},
			LTS:     json.RawMessage(`"Iron"`),
		},
		{
			Version: "v18.0.0",
			Files:   []string{"win-x64-zip"},
			LTS:     json.RawMessage("null"),
		},
	}

	tests := []struct {
		name     string
		arch     string
		expected string
		ok       bool
	}{
		{
			name:     "x64 gets newest LTS",
			arch:     "x64",
			expected: "v22.1.0",
			ok:       true,
		},
		{
			name:     "arm64 falls back to older LTS",
			arch:     "arm64",
			expected: "v20.0.0",
			ok:       true,
		},
		{
			name:     "unsupported arch",
			arch:     "x86",
			expected: "",
			ok:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := nodeLTSVersion(entries, tc.arch)
			if ok != tc.ok {
				t.Errorf("nodeLTSVersion() ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.expected {
				t.Errorf("nodeLTSVersion() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestExtractZipStripRoot(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")
	destDir := filepath.Join(tmpDir, "dest")

	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	files := []struct {
		name    string
		content string
	}{
		{"node-v22.0.0-win-x64/node.exe", "x"},
		{"node-v22.0.0-win-x64/node_modules/npm/bin/npm.cmd", "y"},
		{"node-v22.0.0-win-x64/", ""},
		{"node-v22.0.0-win-x64/../evil.txt", "evil"},
	}

	for _, f := range files {
		fw, err := zw.CreateHeader(&zip.FileHeader{
			Name:   f.name,
			Method: zip.Deflate,
		})
		if err != nil {
			t.Fatalf("Failed to create zip header for %s: %v", f.name, err)
		}
		
		if f.content != "" {
			fw.Write([]byte(f.content))
		}
	}
	zw.Close()

	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("Failed to write zip file: %v", err)
	}

	if err := extractZipStripRoot(zipPath, destDir); err != nil {
		t.Fatalf("extractZipStripRoot failed: %v", err)
	}

	expectedFiles := []struct {
		path    string
		content string
	}{
		{filepath.Join(destDir, "node.exe"), "x"},
		{filepath.Join(destDir, "node_modules", "npm", "bin", "npm.cmd"), "y"},
	}

	for _, ef := range expectedFiles {
		content, err := os.ReadFile(ef.path)
		if err != nil {
			t.Errorf("Expected file %s not found: %v", ef.path, err)
			continue
		}
		if string(content) != ef.content {
			t.Errorf("File %s content = %q, want %q", ef.path, string(content), ef.content)
		}
	}

	evilPath := filepath.Join(tmpDir, "evil.txt")
	if _, err := os.Stat(evilPath); !os.IsNotExist(err) {
		t.Errorf("Evil file was extracted to %s", evilPath)
	}
}
