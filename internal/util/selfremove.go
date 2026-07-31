package util

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Last step of `tokless uninstall`: delete tokless itself. This runs while
// tokless is still running. Unix allows that; Windows doesn't, so there we
// hand the delete to a background shell that waits for us to exit.

// RemoveToklessPathBlock undoes the PATH edit tokless made. Shell rc files on
// Linux and macOS; the user Environment key on Windows.
func RemoveToklessPathBlock() []string {
	if IsWin {
		return removeWindowsPathDirs(ExpectedBinDirs())
	}
	h := resolveHome()
	re := regexp.MustCompile("(?s)\n?" + regexp.QuoteMeta(markStart) + ".*?" + regexp.QuoteMeta(markEnd) + "\n?")
	var cleaned []string
	for _, rc := range candidateRcFiles(h) {
		before, ok := ReadFileSafe(rc)
		if !ok || !strings.Contains(before, markStart) {
			continue
		}
		after := re.ReplaceAllString(before, "\n")
		if after != before {
			if os.WriteFile(rc, []byte(after), 0o644) == nil {
				cleaned = append(cleaned, rc)
			}
		}
	}
	return cleaned
}

// removeWindowsPathDirs drops the dirs tokless added from the user's PATH.
// Mirrors persistWindowsPathDirs in pathsetup.go.
func removeWindowsPathDirs(dirs []string) []string {
	if len(dirs) == 0 {
		return nil
	}
	ps := `$ErrorActionPreference='Stop'
$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
if ($null -eq $k.GetValue('Path')) { exit }
$cur = $k.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
$parts = $cur -split ';' | Where-Object { $_ -ne '' }
$drop = @(` + psQuoteList(dirs) + `)
$new = $parts | Where-Object {
  $e = [Environment]::ExpandEnvironmentVariables($_).TrimEnd('\')
  $drop -notcontains $e -and $drop.TrimEnd('\') -notcontains $e
}
if ($new.Count -ne $parts.Count) {
  $k.SetValue('Path', ($new -join ';'), [Microsoft.Win32.RegistryValueKind]::ExpandString)
  Write-Output 'changed'
}
$k.Close()`
	r := Run("powershell", []string{"-NoProfile", "-Command", ps}, RunOptions{Capture: true})
	if r.Code != 0 || !strings.Contains(r.Stdout, "changed") {
		return nil
	}
	return []string{"user PATH"}
}

func RemoveToklessDataDir() {
	_ = os.RemoveAll(ToklessDataDir())
	_ = os.RemoveAll(filepath.Join(Home(), ".cache", "tokless"))
}

// RemoveSelf deletes the tokless binary. false means the caller should tell
// the user to delete it by hand.
func RemoveSelf() bool {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	// Only follow an actual symlink. EvalSymlinks also rewrites /var to
	// /private/var on macOS, which would look like a link and get deleted twice.
	if fi, err := os.Lstat(exe); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if real, err := filepath.EvalSymlinks(exe); err == nil && real != "" {
			_ = os.Remove(exe) // drop the link as well as its target
			exe = real
		}
	}
	if IsWin {
		return scheduleWindowsSelfDelete(exe)
	}
	if err := os.Remove(exe); err != nil {
		return os.IsNotExist(err) // already gone counts as removed
	}
	return true
}

// scheduleWindowsSelfDelete retries the delete in the background until this
// process exits and releases the file.
func scheduleWindowsSelfDelete(exe string) bool {
	script := "for /l %i in (1,1,20) do (ping -n 2 127.0.0.1 >nul & del /f /q \"" + exe + "\" && exit)"
	cmd := exec.Command("cmd", "/c", "start", "/b", "", "cmd", "/c", script)
	return cmd.Start() == nil
}

func ToklessSelfPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil && real != "" {
		return real
	}
	return exe
}
