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
	if pl.Label != proxyAutostartLabel {
		t.Fatalf("label mismatch: %q", pl.Label)
	}
	if len(pl.ProgramArguments) != 2 || pl.ProgramArguments[0] != "/Users/u/.local/bin/tokless" || pl.ProgramArguments[1] != "__proxy-run" {
		t.Fatalf("ProgramArguments missing proxy-ensure:\n%s", body)
	}
}

type plistBody struct {
	Label            string   `xml:"dict>Label"`
	ProgramArguments []string `xml:"dict>ProgramArguments>string"`
}
