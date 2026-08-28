package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

const (
	antigravityProxyEnvKey    = "GOOGLE_GEMINI_BASE_URL"
	antigravityCloudCodeKey   = "CLOUD_CODE_URL"
	antigravityProxyFenceHead = "# tokless:headroom begin"
	antigravityProxyFenceFoot = "# tokless:headroom end"
	antigravityShellFenceHead = "# >>> tokless antigravity headroom >>>"
	antigravityShellFenceFoot = "# <<< tokless antigravity headroom <<<"
	antigravityWindowsMarker  = "TOKLESS_ANTIGRAVITY_HEADROOM_MANAGED"
)

func antigravityEnvFile() string {
	return filepath.Join(util.Home(), ".gemini", ".env")
}

// antigravityShellEnvFile is the single Unix login-env surface (same idea as copilot).
func antigravityShellEnvFile() string {
	return filepath.Join(util.Home(), ".zshenv")
}

func antigravityURL() string { return ProxyEndpointFor("antigravity") }

// antigravityEnvValue returns the current value of key, preferring the tokless
// fenced block over any un-fenced occurrence.
func antigravityEnvValue(raw, key string) string {
	lines := strings.Split(raw, "\n")
	inFence := false
	fenced, unfenced := "", ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == antigravityProxyFenceHead {
			inFence = true
			continue
		}
		if trimmed == antigravityProxyFenceFoot {
			inFence = false
			continue
		}
		name, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(name) != key {
			continue
		}
		v := strings.TrimSpace(value)
		if inFence {
			fenced = v
		} else if unfenced == "" {
			unfenced = v
		}
	}
	if fenced != "" {
		return fenced
	}
	return unfenced
}

func antigravityDotEnvBlock() string {
	u := antigravityURL()
	return antigravityProxyFenceHead + "\n" +
		antigravityProxyEnvKey + "=" + u + "\n" +
		antigravityCloudCodeKey + "=" + u + "\n" +
		antigravityProxyFenceFoot + "\n"
}

func antigravityCanReplace(raw, key, want string) bool {
	v := antigravityEnvValue(raw, key)
	return v == "" || v == want
}

func antigravityShellBlock() string {
	u := antigravityURL()
	return antigravityShellFenceHead + "\n" +
		"export " + antigravityProxyEnvKey + "=" + u + "\n" +
		"export " + antigravityCloudCodeKey + "=" + u + "\n" +
		antigravityShellFenceFoot + "\n"
}

func antigravityStripFence(raw string) string {
	return antigravityStripMarkedBlock(raw, antigravityProxyFenceHead, antigravityProxyFenceFoot)
}

func antigravityStripShellBlock(raw string) string {
	return antigravityStripMarkedBlock(raw, antigravityShellFenceHead, antigravityShellFenceFoot)
}

func antigravityStripMarkedBlock(raw, head, foot string) string {
	lines := strings.Split(raw, "\n")
	var out []string
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == head {
			inFence = true
			continue
		}
		if trimmed == foot {
			inFence = false
			continue
		}
		if inFence {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func antigravityShellWired(raw string) bool {
	u := antigravityURL()
	inFence := false
	hasProxy, hasCloud := false, false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == antigravityShellFenceHead {
			inFence = true
			continue
		}
		if trimmed == antigravityShellFenceFoot {
			break
		}
		if !inFence {
			continue
		}
		if trimmed == "export "+antigravityProxyEnvKey+"="+u {
			hasProxy = true
		}
		if trimmed == "export "+antigravityCloudCodeKey+"="+u {
			hasCloud = true
		}
	}
	return hasProxy && hasCloud
}

func antigravityUpsertShellBlock(src, block string) string {
	next := antigravityStripShellBlock(src)
	sep := "\n"
	if len(next) == 0 || strings.HasSuffix(next, "\n") {
		sep = ""
	}
	return next + sep + "\n" + block
}

func antigravityWriteDotEnv() (changed bool, err error) {
	file := antigravityEnvFile()
	url := antigravityURL()
	raw, _ := util.ReadFileSafe(file)
	if !antigravityCanReplace(raw, antigravityProxyEnvKey, url) ||
		!antigravityCanReplace(raw, antigravityCloudCodeKey, url) {
		return false, nil
	}
	if antigravityEnvValue(raw, antigravityProxyEnvKey) == url &&
		antigravityEnvValue(raw, antigravityCloudCodeKey) == url {
		return false, nil
	}
	next := antigravityStripFence(raw)
	var sb strings.Builder
	sb.WriteString(strings.TrimSuffix(next, "\n"))
	if sb.Len() > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(antigravityDotEnvBlock())
	return true, util.WriteFile(file, sb.String())
}

// antigravityWriteShellExports puts exports in ~/.zshenv so new Unix shells
// route agy without relying on ~/.gemini/.env alone.
func antigravityWriteShellExports() (changed bool, err error) {
	if util.IsWin {
		return false, nil
	}
	file := antigravityShellEnvFile()
	raw, _ := util.ReadFileSafe(file)
	if !antigravityCanReplaceShell(raw, antigravityProxyEnvKey) ||
		!antigravityCanReplaceShell(raw, antigravityCloudCodeKey) {
		return false, nil
	}
	if antigravityShellWired(raw) {
		return false, nil
	}
	next := antigravityUpsertShellBlock(raw, antigravityShellBlock())
	if next == raw {
		return false, nil
	}
	if err := util.WriteFile(file, next); err != nil {
		return false, err
	}
	return true, nil
}

func antigravityShellEnvValue(raw, key string) string {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "export ")
		name, value, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(name) == key {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func antigravityCanReplaceShell(raw, key string) bool {
	v := antigravityShellEnvValue(raw, key)
	return v == "" || v == antigravityURL()
}

// antigravityLegacyShellCleanupPaths are older multi-rc inject sites; strip only.
func antigravityLegacyShellCleanupPaths() []string {
	h := util.Home()
	return []string{
		filepath.Join(h, ".zshenv"),
		filepath.Join(h, ".zprofile"),
		filepath.Join(h, ".zshrc"),
		filepath.Join(h, ".bash_profile"),
		filepath.Join(h, ".bashrc"),
		filepath.Join(h, ".profile"),
		filepath.Join(h, ".config", "environment.d", "tokless-antigravity.conf"),
	}
}

func antigravityRemoveShellExports() (removed bool) {
	if util.IsWin {
		return false
	}
	for _, f := range antigravityLegacyShellCleanupPaths() {
		src, ok := util.ReadFileSafe(f)
		if !ok {
			continue
		}
		if filepath.Base(f) == "tokless-antigravity.conf" {
			want := antigravityProxyEnvKey + "=" + antigravityURL() + "\n" +
				antigravityCloudCodeKey + "=" + antigravityURL() + "\n"
			if src == want {
				if os.Remove(f) == nil {
					removed = true
				}
			}
			continue
		}
		if !strings.Contains(src, antigravityShellFenceHead) {
			continue
		}
		next := antigravityStripShellBlock(src)
		if next == src {
			continue
		}
		next = strings.TrimSuffix(next, "\n")
		if strings.TrimSpace(next) == "" {
			if util.WriteFile(f, "") == nil {
				removed = true
			}
			continue
		}
		if util.WriteFile(f, next+"\n") == nil {
			removed = true
		}
	}
	return removed
}

func antigravityApplyProcessEnv() {
	u := antigravityURL()
	if v := os.Getenv(antigravityProxyEnvKey); v == "" || v == u {
		_ = os.Setenv(antigravityProxyEnvKey, u)
	}
	if v := os.Getenv(antigravityCloudCodeKey); v == "" || v == u {
		_ = os.Setenv(antigravityCloudCodeKey, u)
	}
}

func antigravityProcessEnvCompatible() bool {
	u := antigravityURL()
	for _, key := range []string{antigravityProxyEnvKey, antigravityCloudCodeKey} {
		if v := os.Getenv(key); v != "" && v != u {
			return false
		}
	}
	return true
}

func antigravityClearProcessEnv() {
	u := antigravityURL()
	if os.Getenv(antigravityCloudCodeKey) == u {
		_ = os.Unsetenv(antigravityCloudCodeKey)
	}
	if os.Getenv(antigravityProxyEnvKey) == u {
		_ = os.Unsetenv(antigravityProxyEnvKey)
	}
}

// antigravityWriteWindowsUserEnv persists both keys to the current-user
// Environment registry so Windows IDE/desktop launches see the proxy.
func antigravityWindowsUserEnvCompatible() bool {
	if !util.IsWin {
		return true
	}
	u := antigravityURL()
	ps := `$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $false)
if ($null -eq $k) { exit 0 }
foreach ($name in @('` + antigravityProxyEnvKey + `','` + antigravityCloudCodeKey + `')) {
  $cur = $k.GetValue($name, $null)
  if ($null -ne $cur -and $cur -ne '` + u + `') { $k.Close(); exit 2 }
}
$k.Close()
`
	return util.Run("powershell", []string{"-NoProfile", "-Command", ps}, util.RunOptions{Capture: true}).Code == 0
}

func antigravityWriteWindowsUserEnv() (bool, bool) {
	if !util.IsWin {
		return false, true
	}
	u := antigravityURL()
	ps := `$ErrorActionPreference='Stop'
$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$want = @{
  '` + antigravityProxyEnvKey + `' = '` + u + `'
  '` + antigravityCloudCodeKey + `' = '` + u + `'
}
$names = @($want.Keys)
foreach ($name in $names) {
  $cur = $k.GetValue($name, $null)
  if ($null -ne $cur -and $cur -ne $want[$name]) { $k.Close(); exit 2 }
}
$old = @{}
foreach ($name in $want.Keys) { $old[$name] = $k.GetValue($name, $null) }
$oldMarker = $k.GetValue('` + antigravityWindowsMarker + `', $null)
$mask = 0
if ($oldMarker -match '(^|,)proxy=(\d+)') { $mask = [int]$Matches[2] }
$changed = $false
try {
  foreach ($name in $want.Keys) {
    $cur = $old[$name]
    if ($cur -ne $want[$name]) {
      $k.SetValue($name, $want[$name], [Microsoft.Win32.RegistryValueKind]::String)
      $changed = $true
      if ($name -eq '` + antigravityProxyEnvKey + `') { $mask = $mask -bor 1 }
      if ($name -eq '` + antigravityCloudCodeKey + `') { $mask = $mask -bor 2 }
    }
  }
  $k.SetValue('` + antigravityWindowsMarker + `', ('proxy=' + $mask), [Microsoft.Win32.RegistryValueKind]::String)
} catch {
  foreach ($name in $want.Keys) {
    if ($null -eq $old[$name]) { $k.DeleteValue($name, $false) }
    else { $k.SetValue($name, $old[$name], [Microsoft.Win32.RegistryValueKind]::String) }
  }
  if ($null -eq $oldMarker) { $k.DeleteValue('` + antigravityWindowsMarker + `', $false) }
  else { $k.SetValue('` + antigravityWindowsMarker + `', $oldMarker, [Microsoft.Win32.RegistryValueKind]::String) }
  $k.Close()
  exit 3
}
$k.Close()
if ($changed) { Write-Output 'changed' }
`
	r := util.Run("powershell", []string{"-NoProfile", "-Command", ps}, util.RunOptions{Capture: true})
	return r.Code == 0 && strings.Contains(r.Stdout, "changed"), r.Code == 0
}

func antigravityClearWindowsUserEnv() bool {
	if !util.IsWin {
		return false
	}
	u := antigravityURL()
	ps := `$ErrorActionPreference='Stop'
$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$names = @('` + antigravityProxyEnvKey + `','` + antigravityCloudCodeKey + `')
$changed = $false
$marker = $k.GetValue('` + antigravityWindowsMarker + `', $null)
if ($marker -notmatch '^proxy=(\d+)$') { $k.Close(); exit 0 }
$mask = [int]$Matches[1]
foreach ($name in $names) {
  $cur = $k.GetValue($name, $null)
  $bit = if ($name -eq '` + antigravityProxyEnvKey + `') { 1 } else { 2 }
  if (($mask -band $bit) -ne 0 -and $cur -eq '` + u + `') {
    $k.DeleteValue($name, $false)
    $changed = $true
  }
}
  $k.DeleteValue('` + antigravityWindowsMarker + `', $false)
$k.Close()
if ($changed) { Write-Output 'changed' }
`
	r := util.Run("powershell", []string{"-NoProfile", "-Command", ps}, util.RunOptions{Capture: true})
	return r.Code == 0 && strings.Contains(r.Stdout, "changed")
}

// ConfigureAntigravityProxy points agy at headroom on every OS:
func ConfigureAntigravityProxy() (changed bool, file string) {
	file = antigravityEnvFile()
	raw, existed := util.ReadFileSafe(file)
	shellRaw, _ := util.ReadFileSafe(antigravityShellEnvFile())
	if !antigravityProcessEnvCompatible() || !antigravityWindowsUserEnvCompatible() ||
		!antigravityCanReplace(raw, antigravityProxyEnvKey, antigravityURL()) ||
		!antigravityCanReplace(raw, antigravityCloudCodeKey, antigravityURL()) ||
		(!util.IsWin && (!antigravityCanReplaceShell(shellRaw, antigravityProxyEnvKey) ||
			!antigravityCanReplaceShell(shellRaw, antigravityCloudCodeKey))) {
		return false, file
	}
	dotChanged, err := antigravityWriteDotEnv()
	if err != nil {
		return false, file
	}
	shellRawBefore := shellRaw
	shellChanged, err := antigravityWriteShellExports()
	if err != nil {
		if existed {
			if rollbackErr := util.WriteFile(file, raw); rollbackErr != nil {
				util.L.Err(fmt.Sprintf("antigravity proxy rollback failed: %v", rollbackErr))
			}
		} else {
			if rollbackErr := os.Remove(file); rollbackErr != nil && !os.IsNotExist(rollbackErr) {
				util.L.Err(fmt.Sprintf("antigravity proxy rollback failed: %v", rollbackErr))
			}
		}
		return false, file
	}
	winChanged, winOK := antigravityWriteWindowsUserEnv()
	if !winOK {
		if existed {
			if rollbackErr := util.WriteFile(file, raw); rollbackErr != nil {
				util.L.Err(fmt.Sprintf("antigravity proxy rollback failed: %v", rollbackErr))
			}
		} else {
			if rollbackErr := os.Remove(file); rollbackErr != nil && !os.IsNotExist(rollbackErr) {
				util.L.Err(fmt.Sprintf("antigravity proxy rollback failed: %v", rollbackErr))
			}
		}
		if !util.IsWin && shellChanged {
			if rollbackErr := util.WriteFile(antigravityShellEnvFile(), shellRawBefore); rollbackErr != nil {
				util.L.Err(fmt.Sprintf("antigravity shell rollback failed: %v", rollbackErr))
			}
		}
		return false, file
	}
	antigravityApplyProcessEnv()
	return dotChanged || shellChanged || winChanged, file
}

// RemoveAntigravityProxy deletes tokless-owned proxy config when it still matches.
func RemoveAntigravityProxy() bool {
	file := antigravityEnvFile()
	raw, ok := util.ReadFileSafe(file)
	url := antigravityURL()
	removed := false
	if ok && antigravityEnvValue(raw, antigravityProxyEnvKey) == url {
		next := antigravityStripFence(raw)
		if next != raw {
			next = strings.TrimSuffix(next, "\n")
			if strings.TrimSpace(next) == "" {
				removed = removeFileIfExists(file)
			} else {
				removed = util.WriteFile(file, next+"\n") == nil
			}
		}
	}
	if antigravityRemoveShellExports() {
		removed = true
	}
	if antigravityClearWindowsUserEnv() {
		removed = true
	}
	antigravityClearProcessEnv()
	return removed
}

// AntigravityProxyWired reports whether .env points at headroom.
func AntigravityProxyWired() bool {
	raw, ok := util.ReadFileSafe(antigravityEnvFile())
	if !ok {
		return false
	}
	return antigravityEnvValue(raw, antigravityProxyEnvKey) == antigravityURL()
}

// AntigravityProxySessionReady is true when process env will route agy through headroom.
func AntigravityProxySessionReady() bool {
	u := antigravityURL()
	return os.Getenv(antigravityCloudCodeKey) == u && os.Getenv(antigravityProxyEnvKey) == u
}

func removeFileIfExists(path string) bool {
	if util.Exists(path) {
		if err := os.Remove(path); err != nil {
			return false
		}
	}
	return true
}
