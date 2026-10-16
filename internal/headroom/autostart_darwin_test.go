//go:build darwin

package headroom

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestProxyAutostartPlistBody(t *testing.T) {
	body := proxyAutostartPlistBody("/Users/u/.local/bin/tokless")
	for _, want := range []string{
		"__proxy-run",
		"/Users/u/.local/bin/tokless",
		"<key>Label</key>",
		"com.tokless.headroom.proxy",
		"<key>KeepAlive</key>",
		"<key>RunAtLoad</key>",
		"<true/>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plist missing %q:\n%s", want, body)
		}
	}
	x := proxyAutostartPlistBody(`/Users/u/bin & tokless <x>"y'>z/tokless`)
	for _, bad := range []string{"<x>", "&tokless", `"y'`, ">z"} {
		if strings.Contains(x, bad) {
			t.Fatalf("XML-sensitive path not escaped (%q):\n%s", bad, x)
		}
	}
	var px plistBody
	if err := xml.Unmarshal([]byte(x), &px); err != nil {
		t.Fatalf("escaped plist XML invalid: %v", err)
	}
	var pl plistBody
	if err := xml.Unmarshal([]byte(body), &pl); err != nil {
		t.Fatalf("plist XML invalid: %v", err)
	}
	// xml:"dict>string" on a plain string field keeps only the last match, so
	// assert the Label key/value pair by content instead of decoding.
	if !strings.Contains(body, "<key>Label</key>") ||
		!strings.Contains(body, "<string>"+proxyAutostartLabel+"</string>") {
		t.Fatalf("label mismatch:\n%s", body)
	}
	if len(pl.ProgramArguments) != 2 || pl.ProgramArguments[0] != "/Users/u/.local/bin/tokless" || pl.ProgramArguments[1] != "__proxy-run" {
		t.Fatalf("ProgramArguments missing proxy-ensure:\n%s", body)
	}
}

func TestLaunchctlServiceNotFoundRequiresExplicitError(t *testing.T) {
	for _, test := range []struct {
		output string
		want   bool
	}{
		{"Could not find service", true},
		{"service not found", true},
		{"Input/output error", false},
	} {
		if got := launchctlServiceNotFound([]byte(test.output)); got != test.want {
			t.Fatalf("launchctlServiceNotFound(%q) = %v, want %v", test.output, got, test.want)
		}
	}
}

type plistBody struct {
	Label            string   `xml:"dict>string"`
	ProgramArguments []string `xml:"dict>array>string"`
}
