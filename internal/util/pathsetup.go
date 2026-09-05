package util

import (
	"os"
	"path/filepath"
	"strings"
)

func ExpectedBinDirs() []string {
	h := resolveHome()
	if IsWin {
		dirs := []string{filepath.Join(h, ".local", "bin"), filepath.Join(h, ".bun", "bin")}
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			dirs = append(dirs, filepath.Join(la, "Programs", "tokless"))
		}
		return dirs
	}
	return []string{
		filepath.Join(h, ".local", "bin"),
		filepath.Join(h, ".bun", "bin"),
		filepath.Join(h, ".cargo", "bin"),
	}
}

// runtimeBinDirs are process-PATH-only candidate.
func runtimeBinDirs() []string {
	if !IsWin {
		return nil
	}
	dirs := []string{nodeInstallDir()}
	if ad := os.Getenv("APPDATA"); ad != "" {
		dirs = append(dirs, filepath.Join(ad, "npm"))
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		dirs = append(dirs, filepath.Join(pf, "nodejs"))
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		dirs = append(dirs, filepath.Join(la, "Programs", "nodejs"))
	}
	return dirs
}

// EnsureProcessPath prepends existing expected dirs to PATH for this process.
func EnsureProcessPath() []string {
	sep := ":"
	if IsWin {
		sep = ";"
	}
	current := strings.Split(os.Getenv("PATH"), sep)
	inPath := map[string]bool{}
	for _, d := range current {
		inPath[d] = true
	}
	var added []string
	for _, dir := range append(ExpectedBinDirs(), runtimeBinDirs()...) {
		if !inPath[dir] && Exists(dir) {
			current = append([]string{dir}, current...)
			added = append(added, dir)
		}
	}
	if len(added) > 0 {
		os.Setenv("PATH", strings.Join(current, sep))
	}
	return added
}

func EnsurePersistentPath() []string {
	if IsWin {
		return ensurePersistentPathWindows()
	}
	return nil
}

func ensurePersistentPathWindows() []string {
	var missing []string
	for _, dir := range ExpectedBinDirs() {
		if Exists(dir) {
			missing = append(missing, dir)
		}
	}
	return persistWindowsPathDirs(missing)
}

// persistWindowsPathDirs appends dirs to the user PATH via the raw registry.
func persistWindowsPathDirs(dirs []string) []string {
	if len(dirs) == 0 {
		return nil
	}
	ps := `$ErrorActionPreference='Stop'
$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$cur = ''
if ($null -ne $k.GetValue('Path')) {
  $cur = $k.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
}
$parts = $cur -split ';' | Where-Object { $_ -ne '' }
$expanded = $parts | ForEach-Object { [Environment]::ExpandEnvironmentVariables($_).TrimEnd('\') }
$add = @(` + psQuoteList(dirs) + `)
$new = $parts
$changed = $false
foreach ($d in $add) {
  if ($expanded -notcontains $d.TrimEnd('\')) { $new += $d; $changed = $true }
}
if ($changed) {
  $k.SetValue('Path', ($new -join ';'), [Microsoft.Win32.RegistryValueKind]::ExpandString)
  Write-Output 'changed'
}
$k.Close()`
	r := Run("powershell", []string{"-NoProfile", "-Command", ps}, RunOptions{Capture: true})
	if r.Code != 0 || !strings.Contains(r.Stdout, "changed") {
		return nil
	}
	return dirs
}

func psQuoteList(dirs []string) string {
	quoted := make([]string, len(dirs))
	for i, d := range dirs {
		quoted[i] = "'" + strings.ReplaceAll(d, "'", "''") + "'"
	}
	return strings.Join(quoted, ",")
}

// SelfHealPath patches the live PATH without editing shell startup files.
func SelfHealPath() {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return
	}
	added := EnsureProcessPath()
	patched := EnsurePersistentPath()
	if len(added) == 0 && len(patched) == 0 {
		return
	}
	var msg []string
	if len(added) > 0 {
		msg = append(msg, "PATH updated for this session")
	}
	L.Debug(strings.Join(msg, " · "))
}
