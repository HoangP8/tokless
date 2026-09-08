package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

const grokShimMarker = "# tokless:grok-launcher"

var removeGrokShimRename = os.Rename

func grokBinFile() string {
	return filepath.Join(grokDir(), "bin", "grok")
}

func grokRealBinFile() string {
	return grokBinFile() + ".real"
}

func isGrokShim(raw string) bool {
	return strings.Contains(string(raw), grokShimMarker)
}

// looksLikeToklessShim reports whether raw matches the structural shape our
// shim generation produced (shebang + marker + REAL + PORT + livez probe).
// Marker-without-shape means unknown provenance; never overwrite that.
func looksLikeToklessShim(raw string) bool {
	s := string(raw)
	return strings.HasPrefix(s, "#!/bin/sh\n") &&
		strings.Contains(s, "REAL=") &&
		strings.Contains(s, "PORT=") &&
		strings.Contains(s, "/livez") &&
		strings.HasSuffix(strings.TrimSpace(s), "exec \"$REAL\" \"$@\"")
}

func grokShimPortLine() string {
	return grokShimMarker + " port=" + strconv.Itoa(util.GrokOAuthProxyPort())
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func renderGrokShim() string {
	port := strconv.Itoa(util.GrokOAuthProxyPort())
	tokless := shQuote(util.ToklessAbsStrict())
	return "#!/bin/sh\n" +
		grokShimPortLine() + "\n" +
		"REAL=" + shQuote(grokRealBinFile()) + "\n" +
		"TOKLESS=" + tokless + "\n" +
		"PORT=\"${TOKLESS_GROK_PROXY_PORT:-" + port + "}\"\n" +
		"if \"$TOKLESS\" __grok-proxy-owned >/dev/null 2>&1 && curl -sfm 1 \"http://127.0.0.1:${PORT}/livez\" 2>/dev/null | grep -q '\"service\":\"headroom-proxy\"'; then\n" +
		"  GROK_MODELS_BASE_URL=\"http://127.0.0.1:${PORT}/v1\" exec \"$REAL\" \"$@\"\n" +
		"fi\n" +
		"exec \"$REAL\" \"$@\"\n"
}

func InstallGrokShim() (bool, error) {
	bin, real := grokBinFile(), grokRealBinFile()
	rendered := renderGrokShim()
	if _, err := os.Stat(bin); err != nil {
		if _, rerr := os.Stat(real); rerr != nil {
			return false, fmt.Errorf("grok CLI not found at %s", bin)
		}
	} else {
		raw, ok := util.ReadFileSafe(bin)
		if !ok {
			return false, fmt.Errorf("grok launcher: cannot read %s — refusing to overwrite", bin)
		}
		if isGrokShim(raw) {
			if string(raw) == rendered {
				return false, nil
			}
			if !looksLikeToklessShim(raw) {
				return false, fmt.Errorf("grok launcher: %s contains tokless marker but unknown shape — refusing to overwrite; remove it manually if stale", bin)
			}
		} else if err := os.Rename(bin, real); err != nil {
			return false, fmt.Errorf("grok launcher: stash real binary: %w", err)
		}
	}
	if err := util.WriteFileAtomic(bin, rendered, 0o755); err != nil {
		return false, fmt.Errorf("grok launcher: install shim: %w", err)
	}
	return true, nil
}

func RemoveGrokShim() bool {
	removed, _ := removeGrokShimChecked()
	return removed
}

func removeGrokShimChecked() (bool, error) {
	bin, real := grokBinFile(), grokRealBinFile()
	raw, ok := util.ReadFileSafe(bin)
	if !ok || !looksLikeToklessShim(raw) || !strings.Contains(raw, grokShimMarker) {
		return false, nil
	}
	if _, err := os.Stat(real); err != nil {
		return false, fmt.Errorf("grok launcher: real binary unavailable: %w", err)
	}
	if err := removeGrokShimRename(real, bin); err != nil {
		return false, fmt.Errorf("grok launcher: restore real binary: %w", err)
	}
	return true, nil
}

func GrokShimWired() bool {
	raw, ok := util.ReadFileSafe(grokBinFile())
	return ok && looksLikeToklessShim(raw) && strings.Contains(raw, grokShimMarker)
}
