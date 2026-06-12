package util

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Direct Node.js LTS install for Windows machines without winget.

const nodeDistIndexURL = "https://nodejs.org/dist/index.json"

type nodeDistEntry struct {
	Version string          `json:"version"`
	Files   []string        `json:"files"`
	LTS     json.RawMessage `json:"lts"`
}

// nodeWinArch maps GOARCH to the nodejs.org dist arch token ("" = unsupported).
func nodeWinArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	case "386":
		return "x86"
	}
	return ""
}

// nodeLTSVersion picks the newest LTS release that ships a zip for arch.
func nodeLTSVersion(entries []nodeDistEntry, arch string) (string, bool) {
	want := "win-" + arch + "-zip"
	for _, e := range entries {
		lts := strings.TrimSpace(string(e.LTS))
		if lts == "" || lts == "false" || lts == "null" {
			continue
		}
		for _, f := range e.Files {
			if f == want {
				return e.Version, true
			}
		}
	}
	return "", false
}

func fetchNodeIndex() ([]nodeDistEntry, bool) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(nodeDistIndexURL)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var entries []nodeDistEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, false
	}
	return entries, true
}

func downloadToTemp(url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &os.PathError{Op: "download", Path: url, Err: os.ErrNotExist}
	}
	f, err := os.CreateTemp("", "tokless-node-*.zip")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// extractZipStripRoot unpacks zipPath into dest, dropping the archive's
// single root directory (node-vX-win-x64/...) so binaries land in dest.
func extractZipStripRoot(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		idx := strings.Index(name, "/")
		if idx < 0 {
			continue // root dir entry itself
		}
		rel := name[idx+1:]
		if rel == "" || !filepath.IsLocal(filepath.FromSlash(rel)) {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// nodeInstallDir is the user-local home for the zip-installed Node.
func nodeInstallDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(Home(), "AppData", "Local")
	}
	return filepath.Join(base, "tokless", "node")
}

// installNodeWindowsZip installs Node LTS without winget: official zip from
// nodejs.org, unpacked user-locally, PATH patched live + persisted.
func installNodeWindowsZip() bool {
	arch := nodeWinArch()
	if arch == "" {
		L.Err("unsupported Windows architecture for Node: " + runtime.GOARCH)
		return false
	}
	entries, ok := fetchNodeIndex()
	if !ok {
		L.Err("couldn't reach nodejs.org to resolve the Node LTS version")
		return false
	}
	v, ok := nodeLTSVersion(entries, arch)
	if !ok {
		L.Err("no Node LTS zip available for win-" + arch)
		return false
	}
	url := "https://nodejs.org/dist/" + v + "/node-" + v + "-win-" + arch + ".zip"
	L.Info("Downloading Node.js " + v + " from nodejs.org…")
	tmp, err := downloadToTemp(url)
	if err != nil {
		L.Err("Node download failed: " + err.Error())
		return false
	}
	defer os.Remove(tmp)
	dest := nodeInstallDir()
	_ = os.RemoveAll(dest)
	if err := extractZipStripRoot(tmp, dest); err != nil {
		L.Err("Node unpack failed: " + err.Error())
		return false
	}
	PrependProcessPath(dest)
	persistWindowsPathDirs([]string{dest})
	return Which("node") != "" && Which("npm") != ""
}
