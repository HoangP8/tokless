package headroom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPatchCloudCodeGeminiAlreadyPatchedIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gemini.py")
	input := "existing\n" + cloudCodePatchMarker + "\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := patchCloudCodeGemini(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("already patched file reported changed")
	}
	output, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != input {
		t.Fatalf("file changed: %q", output)
	}
}

func TestPatchCloudCodeGeminiMissingAnchorLeavesFileUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gemini.py")
	input := "        contents = request_payload.get(\"contents\", [])\n        headers = dict(request.headers.items())\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := patchCloudCodeGemini(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("missing-anchor file reported changed")
	}
	output, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != input {
		t.Fatalf("file changed: %q", output)
	}
}
