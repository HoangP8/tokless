package util

import (
	"encoding/json"
	"testing"
)

// nodeUnixDist: both OS branches (darwin unreachable on a linux host otherwise).
func TestNodeUnixDist(t *testing.T) {
	cases := []struct {
		goos, arch   string
		wantIndex    string
		wantDownload string
	}{
		{"linux", "x64", "linux-x64", "linux"},
		{"linux", "arm64", "linux-arm64", "linux"},
		{"darwin", "x64", "osx-x64-tar", "darwin"},
		{"darwin", "arm64", "osx-arm64-tar", "darwin"},
	}
	for _, c := range cases {
		idx, dl := nodeUnixDist(c.goos, c.arch)
		if idx != c.wantIndex || dl != c.wantDownload {
			t.Errorf("nodeUnixDist(%q,%q) = (%q,%q); want (%q,%q)",
				c.goos, c.arch, idx, dl, c.wantIndex, c.wantDownload)
		}
	}
}

// macOS must download a darwin tarball, not linux.
func TestNodeUnixArtifact(t *testing.T) {
	cases := []struct {
		goos, arch, v string
		wantToken     string
		wantURL       string
	}{
		{"darwin", "arm64", "v22.11.0", "osx-arm64-tar",
			"https://nodejs.org/dist/v22.11.0/node-v22.11.0-darwin-arm64.tar.gz"},
		{"darwin", "x64", "v18.20.4", "osx-x64-tar",
			"https://nodejs.org/dist/v18.20.4/node-v18.20.4-darwin-x64.tar.gz"},
		{"linux", "x64", "v22.11.0", "linux-x64",
			"https://nodejs.org/dist/v22.11.0/node-v22.11.0-linux-x64.tar.gz"},
		{"linux", "arm64", "v20.18.0", "linux-arm64",
			"https://nodejs.org/dist/v20.18.0/node-v20.18.0-linux-arm64.tar.gz"},
	}
	for _, c := range cases {
		token, _, url := nodeUnixArtifact(c.goos, c.arch, c.v)
		if token != c.wantToken {
			t.Errorf("nodeUnixArtifact(%q,%q,%q) token = %q; want %q", c.goos, c.arch, c.v, token, c.wantToken)
		}
		if url != c.wantURL {
			t.Errorf("nodeUnixArtifact(%q,%q,%q) url = %q; want %q", c.goos, c.arch, c.v, url, c.wantURL)
		}
	}
}

// newest-first index: non-LTS head + current LTS + old LTS.
func realisticNodeIndex(t *testing.T) []nodeDistEntry {
	t.Helper()
	raw := `[
	  {"version":"v23.1.0","lts":false,
	   "files":["linux-x64","linux-arm64","osx-arm64-tar","win-x64-zip"]},
	  {"version":"v22.11.0","lts":"Jod",
	   "files":["linux-x64","linux-arm64","osx-x64-tar","osx-arm64-tar","win-x64-zip"]},
	  {"version":"v20.18.0","lts":"Iron",
	   "files":["linux-x64","linux-arm64","osx-x64-tar","osx-arm64-tar","win-x64-zip"]},
	  {"version":"v18.20.4","lts":"Hydrogen",
	   "files":["linux-x64","osx-x64-tar","win-x64-zip","linux-armv7l"]}
	]`
	var entries []nodeDistEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	return entries
}

// newest LTS with the artifact; old-only artifact still resolves to old LTS.
func TestNodeLTSVersion_Selection(t *testing.T) {
	idx := realisticNodeIndex(t)
	cases := []struct {
		token   string
		wantVer string
		wantOk  bool
	}{
		{"linux-x64", "v22.11.0", true},     // newest LTS, NOT the v23 non-LTS head
		{"linux-arm64", "v22.11.0", true},   // present on v22/v20, newest wins
		{"osx-arm64-tar", "v22.11.0", true}, // mac arm: newest LTS with it
		{"osx-x64-tar", "v22.11.0", true},   // mac intel
		{"win-x64-zip", "v22.11.0", true},   // windows
		{"linux-armv7l", "v18.20.4", true},  // ONLY on old LTS → old system still served
		{"linux-s390x", "", false},          // absent everywhere → no false positive
	}
	for _, c := range cases {
		v, ok := nodeLTSVersion(idx, c.token)
		if v != c.wantVer || ok != c.wantOk {
			t.Errorf("nodeLTSVersion(%q) = (%q,%v); want (%q,%v)", c.token, v, ok, c.wantVer, c.wantOk)
		}
	}
}

// non-LTS-only artifact must never be selected.
func TestNodeLTSVersion_SkipsNonLTSOnly(t *testing.T) {
	entries := []nodeDistEntry{
		{Version: "v23.1.0", Files: []string{"linux-ppc64le"}, LTS: json.RawMessage(`false`)},
	}
	if v, ok := nodeLTSVersion(entries, "linux-ppc64le"); ok {
		t.Fatalf("non-LTS-only token must not resolve, got %q", v)
	}
}

func TestNodeUnixArch_Supported(t *testing.T) {
	got := nodeUnixArch()
	switch got {
	case "x64", "arm64":
	case "":
		t.Skipf("unsupported test arch; nodeUnixArch returned empty")
	default:
		t.Fatalf("unexpected arch token %q", got)
	}
}
