package util

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// minGitAsset matches the portable MinGit zip for 64-bit Windows.
var (
	minGitBusybox = regexp.MustCompile(`^MinGit-.*-busybox-64-bit\.zip$`)
	minGitPlain   = regexp.MustCompile(`^MinGit-.*-64-bit\.zip$`)
)

const gitReleasesURL = "https://api.github.com/repos/git-for-windows/git/releases/latest"

type ghRelease struct {
	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// minGitDownloadURL resolves the latest MinGit zip from git-for-windows.
func minGitDownloadURL() (string, bool) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, gitReleasesURL, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", "tokless")
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", false
	}
	return pickMinGitAsset(assetList(rel))
}

type ghAsset struct{ Name, URL string }

func assetList(rel ghRelease) []ghAsset {
	out := make([]ghAsset, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		out = append(out, ghAsset{Name: a.Name, URL: a.BrowserDownloadURL})
	}
	return out
}

// pickMinGitAsset prefers the busybox MinGit zip, falls back to plain 64-bit.
func pickMinGitAsset(assets []ghAsset) (string, bool) {
	for _, a := range assets {
		if minGitBusybox.MatchString(a.Name) {
			return a.URL, true
		}
	}
	for _, a := range assets {
		if minGitPlain.MatchString(a.Name) {
			return a.URL, true
		}
	}
	return "", false
}

// gitInstallDir is the user-local home for the zip-installed MinGit.
func gitInstallDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(Home(), "AppData", "Local")
	}
	return filepath.Join(base, "tokless", "git")
}

// installGitWindowsZip installs MinGit without winget/UAC.
func installGitWindowsZip() bool {
	url, ok := minGitDownloadURL()
	if !ok {
		L.Err("couldn't resolve a MinGit download from git-for-windows releases")
		return false
	}
	L.Info("Downloading MinGit from github.com/git-for-windows…")
	zipPath, err := downloadToTemp(url)
	if err != nil {
		L.Err("MinGit download failed: " + err.Error())
		return false
	}
	defer os.Remove(zipPath)

	dest := gitInstallDir()
	_ = os.RemoveAll(dest)
	if err := extractZipFlat(zipPath, dest); err != nil {
		L.Err("MinGit extract failed: " + err.Error())
		return false
	}
	bin := filepath.Join(dest, "cmd")
	if !Exists(filepath.Join(bin, "git.exe")) {
		L.Err("MinGit zip didn't contain cmd/git.exe")
		return false
	}
	PrependProcessPath(bin)
	persistWindowsPathDirs([]string{bin})
	return true
}
