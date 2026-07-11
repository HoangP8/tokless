package commands

import (
	"context"
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
const installPs1 = "https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1"
const selfUpdateRuleWidth = 52

func RunSelfUpdate() int {
	local := util.ToklessVersion()
	latest := fetchLatestReleaseTag()
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
	go func() {
		defer close(stopped)
		t := time.NewTicker(80 * time.Millisecond)
		defer t.Stop()
		i := 0
		for {
			fmt.Print("\r\x1b[2K  " + util.C.Cyan(frames[i%len(frames)]) + " " + util.C.Gray(label))
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

func runSelfUpdateTo(latest string) (bool, bool) {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return true, false
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if util.IsWin {
		if shell := windowsPowerShell(); shell != "" {
			r := util.Run(shell, []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm " + installPs1 + " | iex"}, util.RunOptions{Capture: false, Env: []string{"CI=1"}, Ctx: ctx})
			if ctx.Err() != nil {
				return false, true
			}
			if r.Code == 0 {
				return true, false
			}
		}
		util.L.Raw("  " + util.C.Cyan("powershell -NoProfile -ExecutionPolicy Bypass -Command \"irm "+installPs1+" | iex\""))
		return false, false
	}
	if err := selfUpdateUnixDirect(ctx, latest); err == nil {
		return true, false
	} else if ctx.Err() != nil {
		return false, true
	} else {
		util.L.Err("Update failed.")
	}
	util.L.Raw("  " + util.C.Cyan("curl -fsSL "+installSh+" | bash"))
	return false, false
}

func selfUpdateUnixDirect(ctx context.Context, latest string) error {
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
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, "tokless-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/v%s/%s", owner, repo, latest, asset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "tokless-self-update")
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download status %s", resp.Status)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		return err
	}
	return nil
}

func toklessReleaseAsset() (string, bool) {
	var osPart string
	switch runtime.GOOS {
	case "linux":
		osPart = "linux"
	case "darwin":
		osPart = "darwin"
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

func windowsPowerShell() string {
	if util.Which("pwsh") != "" {
		return "pwsh"
	}
	if util.Which("powershell") != "" {
		return "powershell"
	}
	return ""
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
