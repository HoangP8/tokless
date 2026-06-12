package util

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const markStart = "# >>> tokless path >>>"
const markEnd = "# <<< tokless path <<<"

func ExpectedBinDirs() []string {
	h := resolveHome()
	if IsWin {
		return []string{filepath.Join(h, ".local", "bin"), filepath.Join(h, ".bun", "bin")}
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
	return ensurePersistentPathUnix()
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

func ensurePersistentPathUnix() []string {
	h := resolveHome()
	block := renderUnixBlock(ExpectedBinDirs(), h)
	var rcs []string
	for _, f := range candidateRcFiles(h) {
		if Exists(f) {
			rcs = append(rcs, f)
		}
	}
	if len(rcs) == 0 {
		rcs = append(rcs, filepath.Join(h, ".profile"))
	}
	var patched []string
	for _, rc := range rcs {
		before, _ := ReadFileSafe(rc)
		after := upsertShellBlock(before, block)
		if after != before {
			_ = os.WriteFile(rc, []byte(after), 0o644)
			patched = append(patched, rc)
		}
	}
	return patched
}

func renderUnixBlock(dirs []string, home string) string {
	rel := make([]string, len(dirs))
	for i, d := range dirs {
		if strings.HasPrefix(d, home) {
			rel[i] = `"$HOME` + d[len(home):] + `"`
		} else {
			rel[i] = `"` + d + `"`
		}
	}
	lines := []string{
		markStart,
		"# Adds tokless tool bin dirs to PATH (rtk, bun, cargo).",
		"for d in " + strings.Join(rel, " ") + "; do",
		`  [ -d "$d" ] && case ":$PATH:" in *":$d:"*) ;; *) PATH="$d:$PATH" ;; esac`,
		"done",
		"export PATH",
		markEnd,
		"",
	}
	return strings.Join(lines, "\n")
}

func candidateRcFiles(home string) []string {
	shell := os.Getenv("SHELL")
	if strings.HasSuffix(shell, "zsh") {
		return []string{filepath.Join(home, ".zshrc")}
	}
	if strings.HasSuffix(shell, "bash") {
		return []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".bash_profile")}
	}
	return []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".profile"),
	}
}

func upsertShellBlock(src, block string) string {
	re := regexp.MustCompile("(?s)" + regexp.QuoteMeta(markStart) + ".*?" + regexp.QuoteMeta(markEnd) + "\\n?")
	if re.MatchString(src) {
		return re.ReplaceAllString(src, block)
	}
	sep := "\n"
	if len(src) == 0 || strings.HasSuffix(src, "\n") {
		sep = ""
	}
	return src + sep + "\n" + block
}

// SelfHealPath patches the live PATH and persists it to shell rc files.
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
	if len(patched) > 0 {
		short := make([]string, len(patched))
		for i, p := range patched {
			short[i] = strings.Replace(p, resolveHome(), "~", 1)
		}
		msg = append(msg, "persisted to "+strings.Join(short, ", "))
	}
	L.Debug(strings.Join(msg, " · "))
}
