package agents

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

const antigravityShimMarker = "# tokless:antigravity-launcher"

type antigravityShimState struct {
	Bin        string `json:"bin"`
	Real       string `json:"real"`
	RealDigest string `json:"realDigest"`
}

var removeAntigravityShimRename = os.Rename

func antigravityBinFile() string {
	name := "agy"
	if util.IsWin {
		name += ".cmd"
	}
	for _, dir := range agyKnownBinDirs() {
		bin := filepath.Join(dir, name)
		if util.Exists(bin) || util.Exists(antigravityRealBinIn(dir)) || util.Exists(antigravityVendorBinIn(dir)) {
			return bin
		}
	}
	if len(agyKnownBinDirs()) == 0 {
		return ""
	}
	return filepath.Join(agyKnownBinDirs()[0], name)
}

func antigravityVendorBinIn(dir string) string {
	if util.IsWin {
		return filepath.Join(dir, "agy.exe")
	}
	return filepath.Join(dir, "agy")
}

func antigravityRealBinIn(dir string) string {
	if util.IsWin {
		return filepath.Join(dir, "agy.real.exe")
	}
	return filepath.Join(dir, "agy.real")
}

func antigravityVendorBinFile() string {
	return antigravityVendorBinIn(filepath.Dir(antigravityBinFile()))
}

func antigravityRealBinFile() string { return antigravityRealBinIn(filepath.Dir(antigravityBinFile())) }

func antigravityShimStateFile() string {
	return filepath.Join(util.ToklessDataDir(), "antigravity-cli-proxy.json")
}

func readAntigravityShimState() (antigravityShimState, bool) {
	var state antigravityShimState
	raw, ok := util.ReadFileSafe(antigravityShimStateFile())
	if !ok || json.Unmarshal([]byte(raw), &state) != nil {
		return state, false
	}
	return state, filepath.Clean(state.Bin) == filepath.Clean(antigravityBinFile()) &&
		filepath.Clean(state.Real) == filepath.Clean(antigravityRealBinFile())
}

func antigravityFileDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:]), nil
}

func looksLikeAntigravityShim(raw string) bool {
	if util.IsWin {
		return strings.HasPrefix(raw, "@echo off\r\n") &&
			strings.Contains(raw, antigravityShimMarker) &&
			strings.Contains(raw, "__proxy-ensure") &&
			strings.Contains(raw, antigravityProxyEnvKey) &&
			strings.Contains(raw, antigravityCloudCodeKey) &&
			strings.Contains(raw, "\"%REAL%\" %*")
	}
	return strings.HasPrefix(raw, "#!/bin/sh\n") &&
		strings.Contains(raw, antigravityShimMarker) &&
		strings.Contains(raw, "TOKLESS=") &&
		strings.Contains(raw, "__proxy-ensure") &&
		strings.Contains(raw, "GOOGLE_GEMINI_BASE_URL=") &&
		strings.Contains(raw, "CLOUD_CODE_URL=") &&
		strings.HasSuffix(strings.TrimSpace(raw), "exec \"$REAL\" \"$@\"")
}

func readAntigravityLauncher(path string) (raw string, script bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	header := make([]byte, 2)
	n, err := io.ReadFull(f, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", false, err
	}
	if n < 2 || string(header) != "#!" {
		return "", false, nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return "", true, err
	}
	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	return string(b), true, err
}

func renderAntigravityShim() string {
	u := antigravityURL()
	if util.IsWin {
		return "@echo off\r\n" +
			"rem " + antigravityShimMarker + "\r\n" +
			"set \"REAL=" + antigravityRealBinFile() + "\"\r\n" +
			"set \"TOKLESS=" + util.ToklessAbsStrict() + "\"\r\n" +
			"set \"URL=" + u + "\"\r\n" +
			"\"%TOKLESS%\" __proxy-ensure >nul 2>&1\r\n" +
			"if errorlevel 1 (\r\n" +
			"  echo agy: Headroom proxy unavailable; run tokless proxy up --agents antigravity 1>&2\r\n" +
			"  exit /b 1\r\n" +
			")\r\n" +
			"set \"" + antigravityProxyEnvKey + "=%URL%\"\r\n" +
			"set \"" + antigravityCloudCodeKey + "=%URL%\"\r\n" +
			"\"%REAL%\" %*\r\n" +
			"exit /b %errorlevel%\r\n"
	}
	return "#!/bin/sh\n" +
		antigravityShimMarker + "\n" +
		"REAL=" + shQuote(antigravityRealBinFile()) + "\n" +
		"TOKLESS=" + shQuote(util.ToklessAbsStrict()) + "\n" +
		"URL=" + shQuote(u) + "\n" +
		"if ! \"$TOKLESS\" __proxy-ensure >/dev/null 2>&1; then\n" +
		"  echo 'agy: Headroom proxy unavailable; run tokless proxy up --agents antigravity' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"GOOGLE_GEMINI_BASE_URL=\"$URL\" CLOUD_CODE_URL=\"$URL\" exec \"$REAL\" \"$@\"\n"
}

func antigravityShimApplicable() bool {
	bin := antigravityBinFile()
	return bin != "" && (util.Exists(bin) || util.Exists(antigravityVendorBinFile()) || util.Exists(antigravityRealBinFile()))
}

func antigravityShimCompatible() bool {
	if !antigravityShimApplicable() {
		return true
	}
	if util.IsWin {
		raw, exists := util.ReadFileSafe(antigravityBinFile())
		if !exists {
			return util.Exists(antigravityVendorBinFile()) && !util.Exists(antigravityRealBinFile())
		}
		if strings.Contains(raw, antigravityShimMarker) {
			_, owned := readAntigravityShimState()
			return owned && looksLikeAntigravityShim(raw)
		}
		return false
	}
	raw, script, err := readAntigravityLauncher(antigravityBinFile())
	if err != nil {
		return util.Exists(antigravityRealBinFile())
	}
	if script && strings.Contains(raw, antigravityShimMarker) {
		_, owned := readAntigravityShimState()
		return owned && looksLikeAntigravityShim(raw)
	}
	// agy is a native executable. Refuse to replace another script/wrapper.
	_, owned := readAntigravityShimState()
	return !script && (!util.Exists(antigravityRealBinFile()) || owned)
}

func installAntigravityShim() (bool, error) {
	if !antigravityShimApplicable() {
		return false, nil
	}
	bin, vendor, real := antigravityBinFile(), antigravityVendorBinFile(), antigravityRealBinFile()
	rendered := renderAntigravityShim()
	_, owned := readAntigravityShimState()
	if util.IsWin {
		if raw, exists := util.ReadFileSafe(bin); exists {
			if !owned || !looksLikeAntigravityShim(raw) {
				return false, fmt.Errorf("antigravity launcher: %s is not a Tokless-owned launcher", bin)
			}
			if raw == rendered {
				return false, nil
			}
		} else if util.Exists(bin) {
			return false, fmt.Errorf("antigravity launcher: cannot read %s", bin)
		}
	} else if raw, script, readErr := readAntigravityLauncher(bin); readErr == nil {
		if script && strings.Contains(raw, antigravityShimMarker) {
			if !owned || !looksLikeAntigravityShim(raw) {
				return false, fmt.Errorf("antigravity launcher: %s contains tokless marker but unknown shape; refusing to overwrite", bin)
			}
			if raw == rendered {
				return false, nil
			}
		} else if script {
			return false, fmt.Errorf("antigravity launcher: %s is an existing script; refusing to overwrite", bin)
		} else {
			if util.Exists(real) && !owned {
				return false, fmt.Errorf("antigravity launcher: %s already exists without Tokless ownership; refusing to overwrite", real)
			}
			if util.Exists(real) && owned {
				state, ok := readAntigravityShimState()
				if !ok || state.RealDigest == "" {
					return false, fmt.Errorf("antigravity launcher: missing real-binary ownership digest")
				}
				digest, err := antigravityFileDigest(real)
				if err != nil || digest != state.RealDigest {
					return false, fmt.Errorf("antigravity launcher: real binary changed outside Tokless; refusing upgrade")
				}
				if err := os.Remove(real); err != nil {
					return false, fmt.Errorf("antigravity launcher: replace old real binary: %w", err)
				}
			}
			if err := os.Rename(bin, real); err != nil {
				return false, fmt.Errorf("antigravity launcher: stash real binary: %w", err)
			}
		}
	} else if !os.IsNotExist(readErr) {
		return false, fmt.Errorf("antigravity launcher: read %s: %w", bin, readErr)
	}
	if util.IsWin && util.Exists(vendor) {
		if util.Exists(real) && !owned {
			return false, fmt.Errorf("antigravity launcher: %s already exists without Tokless ownership; refusing to overwrite", real)
		}
		if err := os.Rename(vendor, real); err != nil {
			return false, fmt.Errorf("antigravity launcher: stash real binary: %w", err)
		}
	}
	if !util.Exists(real) {
		return false, fmt.Errorf("antigravity CLI not found at %s", vendor)
	}
	digest, err := antigravityFileDigest(real)
	if err != nil {
		return false, fmt.Errorf("antigravity launcher: hash real binary: %w", err)
	}
	state, err := json.Marshal(antigravityShimState{Bin: bin, Real: real, RealDigest: digest})
	if err != nil {
		return false, err
	}
	if err := util.WriteFile(antigravityShimStateFile(), string(state)); err != nil {
		if !owned && util.Exists(real) && !util.Exists(bin) {
			_ = os.Rename(real, bin)
		}
		return false, fmt.Errorf("antigravity launcher: record ownership: %w", err)
	}
	if err := util.WriteFileAtomic(bin, rendered, 0o755); err != nil {
		if util.Exists(real) && !util.Exists(vendor) {
			_ = os.Rename(real, vendor)
		}
		if !owned {
			_ = os.Remove(antigravityShimStateFile())
		}
		return false, fmt.Errorf("antigravity launcher: install shim: %w", err)
	}
	return true, nil
}

func removeAntigravityShim() bool {
	bin, vendor, real := antigravityBinFile(), antigravityVendorBinFile(), antigravityRealBinFile()
	if _, owned := readAntigravityShimState(); !owned {
		return false
	}
	if util.IsWin {
		raw, exists := util.ReadFileSafe(bin)
		if !exists || !looksLikeAntigravityShim(raw) || !util.Exists(real) {
			return false
		}
		if util.Exists(vendor) || os.Remove(bin) != nil || removeAntigravityShimRename(real, vendor) != nil {
			return false
		}
	} else {
		raw, _, err := readAntigravityLauncher(bin)
		if err != nil || !looksLikeAntigravityShim(raw) || !util.Exists(real) {
			return false
		}
		if removeAntigravityShimRename(real, bin) != nil {
			return false
		}
	}
	return os.Remove(antigravityShimStateFile()) == nil
}

func AntigravityShimWired() bool {
	if _, owned := readAntigravityShimState(); !owned {
		return false
	}
	if util.IsWin {
		raw, exists := util.ReadFileSafe(antigravityBinFile())
		return exists && looksLikeAntigravityShim(raw) && raw == renderAntigravityShim()
	}
	raw, _, err := readAntigravityLauncher(antigravityBinFile())
	return err == nil && looksLikeAntigravityShim(raw) && raw == renderAntigravityShim()
}
