//go:build linux

package headroom

import (
	"strings"
	"testing"
)

func TestProxyAutostartUnitBody(t *testing.T) {
	body := proxyAutostartUnitBody("/home/u/.local/bin/tokless")
	bin := `/home/u/.local/bin/tokless`
	for _, want := range []string{
		"__proxy-run",
		bin,
		"WantedBy=default.target",
		"Type=simple",
		"Restart=on-failure",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("unit missing %q:\n%s", want, body)
		}
	}
	spaced := proxyAutostartUnitBody(`/tmp/tokless bin/tokless`)
	if !strings.Contains(spaced, `"/tmp/tokless bin/tokless"`) {
		t.Fatalf("spaces not quoted:\n%s", spaced)
	}
}
