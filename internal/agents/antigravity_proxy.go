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

// antigravityLegacyShellCleanupPaths covers older shell injection surfaces.
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
	for _, file := range antigravityLegacyShellCleanupPaths() {
		raw, ok := util.ReadFileSafe(file)
		if !ok {
			continue
		}
		if filepath.Base(file) == "tokless-antigravity.conf" {
			want := antigravityProxyEnvKey + "=" + antigravityURL() + "\n" +
				antigravityCloudCodeKey + "=" + antigravityURL() + "\n"
			if raw == want && os.Remove(file) == nil {
				removed = true
			}
			continue
		}
		if !strings.Contains(raw, antigravityShellFenceHead) {
			continue
		}
		next := antigravityRemoveShellBlock(raw)
		if next == raw {
			continue
		}
		next = strings.TrimSuffix(next, "\n")
		if strings.TrimSpace(next) == "" {
			if util.WriteFile(file, "") == nil {
				removed = true
			}
		} else if util.WriteFile(file, next+"\n") == nil {
			removed = true
		}
	}
	return removed
}

func antigravityRemoveShellBlock(raw string) string {
	u := antigravityURL()
	lines := strings.Split(raw, "\n")
	var out []string
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == antigravityShellFenceHead {
			inFence = true
			continue
		}
		if trimmed == antigravityShellFenceFoot {
			inFence = false
			continue
		}
		if inFence && (trimmed == "export "+antigravityProxyEnvKey+"="+u ||
			trimmed == "export "+antigravityCloudCodeKey+"="+u) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func antigravityStripFence(raw string) string {
	return antigravityStripMarkedBlock(raw, antigravityProxyFenceHead, antigravityProxyFenceFoot)
}

func antigravityStripShellBlock(raw string) string {
	return antigravityStripMarkedBlock(raw, antigravityShellFenceHead, antigravityShellFenceFoot)
}

func antigravityStripMarkedBlock(raw, head, foot string) string {
	lines := strings.Split(raw, "\n")
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == head {
			if inFence {
				return raw
			}
			inFence = true
		} else if trimmed == foot {
			if !inFence {
				return raw
			}
			inFence = false
		}
	}
	if inFence {
		return raw
	}

	var out []string
	inFence = false
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
	if !existed && util.Exists(file) {
		return false, file
	}
	if !antigravityProcessEnvCompatible() || !antigravityWindowsUserEnvCompatible() ||
		!antigravityCanReplace(raw, antigravityProxyEnvKey, antigravityURL()) ||
		!antigravityCanReplace(raw, antigravityCloudCodeKey, antigravityURL()) {
		return false, file
	}
	dotChanged, err := antigravityWriteDotEnv()
	if err != nil {
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
		return false, file
	}
	antigravityApplyProcessEnv()
	return dotChanged || winChanged, file
}

// RemoveAntigravityProxy deletes tokless-owned proxy config when it still matches.
func RemoveAntigravityProxy() bool {
	file := antigravityEnvFile()
	raw, ok := util.ReadFileSafe(file)
	url := antigravityURL()
	removed := false
	if ok && antigravityEnvValue(raw, antigravityProxyEnvKey) == url && antigravityEnvValue(raw, antigravityCloudCodeKey) == url {
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
	return antigravityEnvValue(raw, antigravityProxyEnvKey) == antigravityURL() &&
		antigravityEnvValue(raw, antigravityCloudCodeKey) == antigravityURL()
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
