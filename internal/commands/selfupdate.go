package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

const owner = "HoangP8"
const repo = "tokless"
const installSh = "https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.sh"
const selfUpdateRuleWidth = 52

func RunSelfUpdate() int {
	local := util.ToklessVersion()
	latest := fetchLatestReleaseTagCached()
	if latest == "" {
		util.L.Err("Could not reach latest version.")
		util.L.Raw("  " + util.C.Cyan("curl -fsSL "+installSh+" | bash"))
		return 1
	}
	if util.SemverGte(local, latest) {
		util.L.Raw("  " + util.C.Green(util.Sym.Check) + " tokless " + util.C.Gray("v"+local))
		return 0
	}
	if ok, interrupted := runSelfUpdateWithStatus(latest); interrupted {
		return 130
	} else if ok {
		util.L.Raw("  " + util.C.Green(util.Sym.Check) + " tokless " + util.C.Gray("v"+local+" → v"+latest+" updated"))
		return 0
	}
	return 1
}

func MaybeSelfUpdate(opts InitOptions) {
	if opts.DryRun || strings.HasSuffix(util.ToklessVersion(), "-dev") {
		return
	}
	selfUpdateRule()
	util.L.Raw(util.C.Bold("Tokless"))
	local := util.ToklessVersion()
	latest := fetchLatestReleaseTagWithStatus(local)
	if latest == "" {
		return
	}
	if util.SemverGte(local, latest) {
		util.L.Raw("  " + util.C.Green(util.Sym.Check) + " tokless " + util.C.Gray("v"+local))
		return
	}
	if ok, interrupted := runSelfUpdateWithStatus(latest); interrupted {
		util.RestoreConsoleCP()
		os.Exit(130)
	} else if ok {
		util.L.Raw("  " + util.C.Green(util.Sym.Check) + " tokless " + util.C.Gray("v"+local+" → v"+latest+" updated"))
		reexecAfterSelfUpdate()
	} else {
		util.L.Raw("  " + util.C.Yellow(util.Sym.Warn) + " tokless " + util.C.Gray("v"+local+" → v"+latest+" failed"))
	}
}

func selfUpdateRule() {
	util.L.Raw(util.C.Gray(util.Rule(selfUpdateRuleWidth)))
}

func fetchLatestReleaseTagWithStatus(local string) string {
	var latest string
	runStatus("tokless v"+local+" checking updates…", func() { latest = fetchLatestReleaseTag() })
	return latest
}

func runSelfUpdateWithStatus(latest string) (bool, bool) {
	ok := false
	interrupted := false
	runStatus("tokless updating…", func() { ok, interrupted = runSelfUpdateTo(latest) })
	return ok, interrupted
}

func runStatus(label string, fn func()) {
	if !stdoutTTY() {
		fn()
		return
	}
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	done := make(chan struct{})
	stopped := make(chan struct{})
	prevWidth := 0
	go func() {
		defer close(stopped)
		t := time.NewTicker(80 * time.Millisecond)
		defer t.Stop()
		i := 0
		for {
			line := "  " + util.C.Cyan(frames[i%len(frames)]) + " " + util.C.Gray(label)
			curWidth := util.VisibleLen(line)
			pad := 0
			if prevWidth > curWidth {
				pad = prevWidth - curWidth
			}
			prevWidth = curWidth
			fmt.Print("\r\x1b[2K" + line + strings.Repeat(" ", pad))
			i++
			select {
			case <-done:
				fmt.Print("\r\x1b[2K")
				return
			case <-t.C:
			}
		}
	}()
	fn()
	close(done)
	<-stopped
}

// runSelfUpdateTo swaps the binary and refreshes install.json on success.
func runSelfUpdateTo(latest string) (bool, bool) {
	ok, interrupted := selfUpdateExec(latest)
	if ok && os.Getenv("TOKLESS_TEST") != "1" {
		util.RefreshInstallMarker(latest)
	}
	return ok, interrupted
}

func selfUpdateExec(latest string) (bool, bool) {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return true, false
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := selfUpdateDirect(ctx, latest)
	if err == nil {
		return true, false
	}
	if ctx.Err() != nil {
		return false, true
	}
	util.L.Err("Update failed: " + err.Error())
	if util.IsWin {
		util.L.Raw("  " + util.C.Cyan("Reinstall from https://github.com/"+owner+"/"+repo+"/releases/latest"))
	} else {
		util.L.Raw("  " + util.C.Cyan("curl -fsSL "+installSh+" | bash"))
	}
	return false, false
}

// selfUpdateDirect downloads, verifies, and swaps the release in place.
func selfUpdateDirect(ctx context.Context, latest string) error {
	asset, ok := toklessReleaseAsset()
	if !ok {
		return fmt.Errorf("unsupported platform")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil && real != "" {
		exe = real
	}
	base := fmt.Sprintf("https://github.com/%s/%s/releases/download/v%s", owner, repo, latest)
	download := exe + ".download"
	_ = os.Remove(download)
	if err := downloadFile(ctx, base+"/"+asset, download); err != nil {
		_ = os.Remove(download)
		return err
	}
	want, err := releaseChecksum(ctx, base+"/SHA256SUMS", asset)
	if err != nil {
		_ = os.Remove(download)
		return err
	}
	if err := verifySHA256(download, want); err != nil {
		_ = os.Remove(download)
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(download, 0o755); err != nil {
			_ = os.Remove(download)
			return err
		}
	}
	if err := swapBinary(exe, download); err != nil {
		_ = os.Remove(download)
		return err
	}
	return nil
}

func downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "tokless-self-update")
	resp, err := (&http.Client{Timeout: 300 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download status %s", resp.Status)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func releaseChecksum(ctx context.Context, url, asset string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "tokless-self-update")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("checksum status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if sum, ok := checksumFor(string(body), asset); ok {
		return sum, nil
	}
	return "", fmt.Errorf("checksum for %s not found", asset)
}

func checksumFor(body, asset string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], true
		}
	}
	return "", false
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch: got %s", got)
	}
	return nil
}

// swapBinary replaces the running exe with the verified download.
func swapBinary(exe, download string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(download, exe)
	}
	old := exe + ".old"
	removeOldBinaries(exe)
	if _, err := os.Stat(old); err == nil {
		old = fmt.Sprintf("%s.old.%d", exe, os.Getpid())
		_ = os.Remove(old)
	}
	if err := renameRetry(exe, old); err != nil {
		return err
	}
	if err := renameRetry(download, exe); err != nil {
		if rerr := renameRetry(old, exe); rerr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rerr)
		}
		return err
	}
	// Restore the backup if Defender removed the new image.
	time.Sleep(time.Second)
	if _, err := os.Stat(exe); err != nil {
		if rerr := renameRetry(old, exe); rerr != nil {
			return fmt.Errorf("new binary missing after swap (%v); restore failed: %v", err, rerr)
		}
		return fmt.Errorf("new binary missing after swap; previous version restored")
	}
	// Not removable while mapped; the next update cleans it.
	_ = os.Remove(old)
	return nil
}

// restorePrevious puts this process's backup back, else the first match.
func restorePrevious(exe string) error {
	if err := os.Rename(fmt.Sprintf("%s.old.%d", exe, os.Getpid()), exe); err == nil {
		return nil
	}
	dir, base := filepath.Split(exe)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	prefix := base + ".old"
	var last error
	for _, e := range entries {
		if e.IsDir() || !hasOldPrefix(e.Name(), prefix) {
			continue
		}
		if last = os.Rename(filepath.Join(dir, e.Name()), exe); last == nil {
			return nil
		}
	}
	if last == nil {
		last = fmt.Errorf("previous binary not found")
	}
	return last
}

// hasOldPrefix matches update backups; Windows paths are case-insensitive.
func hasOldPrefix(name, prefix string) bool {
	if len(name) < len(prefix) {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(name[:len(prefix)], prefix)
	}
	return name[:len(prefix)] == prefix
}

// removeOldBinaries clears leftovers from earlier updates.
func removeOldBinaries(exe string) {
	dir, base := filepath.Split(exe)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := base + ".old"
	for _, e := range entries {
		if !e.IsDir() && hasOldPrefix(e.Name(), prefix) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// renameRetry retries on Windows while antivirus may hold the file.
func renameRetry(from, to string) error {
	err := os.Rename(from, to)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	for i := 0; i < 9; i++ {
		time.Sleep(200 * time.Millisecond)
		if err = os.Rename(from, to); err == nil {
			return nil
		}
	}
	return err
}

func toklessReleaseAsset() (string, bool) {
	var osPart string
	switch runtime.GOOS {
	case "linux":
		osPart = "linux"
	case "darwin":
		osPart = "darwin"
	case "windows":
		if runtime.GOARCH != "amd64" {
			return "", false
		}
		return "tokless-windows-x64.exe", true
	default:
		return "", false
	}
	var archPart string
	switch runtime.GOARCH {
	case "amd64":
		archPart = "x64"
	case "arm64":
		archPart = "arm64"
	default:
		return "", false
	}
	return "tokless-" + osPart + "-" + archPart, true
}

func fetchLatestReleaseTag() string {
	if os.Getenv("TOKLESS_TEST") == "1" {
		if v := os.Getenv("TOKLESS_TEST_LATEST"); v != "" {
			return strings.TrimPrefix(v, "v")
		}
		return "0.1.0"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+owner+"/"+repo+"/releases/latest", nil)
	req.Header.Set("User-Agent", "tokless-self-update")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var j struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(resp.Body).Decode(&j) != nil {
		return ""
	}
	return strings.TrimPrefix(j.TagName, "v")
}

// selfUpdateCacheTTL bounds how stale a cached "latest tag" may be.
const selfUpdateCacheTTL = 6 * time.Hour
const selfUpdateCacheNegTTL = 60 * time.Second

// selfUpdateCachePath is where the latest release tag is cached.
func selfUpdateCachePath() string {
	home := util.Home()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "tokless", "selfupdate.json")
}

// selfUpdateCachedTag reads the cached tag and whether it is still fresh.
func selfUpdateCachedTag() (tag string, fresh bool) {
	p := selfUpdateCachePath()
	if p == "" {
		return "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	var c struct {
		Tag string `json:"tag"`
		Ts  int64  `json:"ts"`
	}
	if json.Unmarshal(b, &c) != nil {
		return "", false
	}
	age := time.Since(time.UnixMilli(c.Ts))
	ttl := selfUpdateCacheTTL
	if c.Tag == "" {
		ttl = selfUpdateCacheNegTTL
	}
	return c.Tag, age <= ttl
}

func writeSelfUpdateCache(tag string) {
	p := selfUpdateCachePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	b, _ := json.Marshal(struct {
		Tag string `json:"tag"`
		Ts  int64  `json:"ts"`
	}{Tag: tag, Ts: time.Now().UnixMilli()})
	_ = os.WriteFile(p, b, 0o644)
}

// fetchLatestReleaseTagCached returns the latest release tag, reading a cached
// value when fresh.
func fetchLatestReleaseTagCached() string {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return fetchLatestReleaseTag()
	}
	if tag, fresh := selfUpdateCachedTag(); fresh {
		return tag
	}
	tag := fetchLatestReleaseTag()
	if tag == "" {
		if prev, _ := selfUpdateCachedTag(); prev != "" {
			return prev
		}
	}
	writeSelfUpdateCache(tag)
	return tag
}
