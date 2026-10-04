package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestCodexProjectmemHookTrustHashMatchesVerifiedFormat(t *testing.T) {
	got := codexProjectmemHookTrustHash("/x/hook.sh claude")
	handler := map[string]interface{}{"async": false, "command": "/x/hook.sh claude", "timeout": codexProjectmemHookTimeout, "type": "command"}
	b, _ := json.Marshal(map[string]interface{}{"event_name": "session_start", "hooks": []interface{}{handler}})
	sum := sha256.Sum256(b)
	if want := "sha256:" + hex.EncodeToString(sum[:]); got != want || strings.Contains(string(b), "matcher") {
		t.Fatalf("hash = %s, want %s", got, want)
	}
}
