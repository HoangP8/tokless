package util

import (
	"strings"
	"testing"
)

func TestSetTomlBlockKeyCreatesTable(t *testing.T) {
	out, created, ok := SetTomlBlockKey("", "hooks", "suppress_hook_warning", "true")
	if !ok || !created {
		t.Fatalf("ok=%v created=%v", ok, created)
	}
	if out != "[hooks]\nsuppress_hook_warning = true\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestSetTomlBlockKeyKeepsOtherContent(t *testing.T) {
	src := "# rtk config\ntelemetry = false\n\n[tracking]\nenabled = true\n\n[hooks]\n# user comment\nexclude_commands = [\"curl\"]\n\n[tee]\nmode = \"never\"\n"
	out, created, ok := SetTomlBlockKey(src, "hooks", "suppress_hook_warning", "true")
	if !ok || created {
		t.Fatalf("ok=%v created=%v", ok, created)
	}
	if !strings.Contains(out, "# user comment\nexclude_commands = [\"curl\"]") {
		t.Fatalf("comment or field lost:\n%s", out)
	}
	if !strings.Contains(out, "[hooks]\n") {
		t.Fatalf("missing header:\n%s", out)
	}
	if strings.Count(out, "[hooks]") != 1 {
		t.Fatalf("duplicate header:\n%s", out)
	}
	if !strings.Contains(out, "\n[tee]\nmode = \"never\"\n") {
		t.Fatalf("following table lost:\n%s", out)
	}
	if !strings.HasSuffix(out, "mode = \"never\"\n") {
		t.Fatalf("trailing content changed:\n%s", out)
	}
}

func TestSetTomlBlockKeyReplacesOnlyKeyLine(t *testing.T) {
	src := "[hooks]\nexclude_commands = [\"curl\"]\nsuppress_hook_warning = false\nwrap_fish_scripts = false\n"
	out, created, ok := SetTomlBlockKey(src, "hooks", "suppress_hook_warning", "true")
	if !ok || created {
		t.Fatalf("ok=%v created=%v", ok, created)
	}
	want := "[hooks]\nexclude_commands = [\"curl\"]\nsuppress_hook_warning = true\nwrap_fish_scripts = false\n"
	if out != want {
		t.Fatalf("out = %q want %q", out, want)
	}
}

func TestSetTomlBlockKeyPreservesBOMAndCRLF(t *testing.T) {
	src := "\uFEFF[hooks]\r\nexclude_commands = []\r\n\r\n[tee]\r\nmode = \"never\"\r\n"
	out, _, ok := SetTomlBlockKey(src, "hooks", "suppress_hook_warning", "true")
	if !ok {
		t.Fatal("edit refused")
	}
	if !strings.HasPrefix(out, "\uFEFF") {
		t.Fatalf("BOM lost: %q", out)
	}
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Fatalf("bare LF introduced: %q", out)
	}
	if !strings.Contains(out, "[hooks]\r\nsuppress_hook_warning = true\r\n") {
		t.Fatalf("key not inserted with CRLF: %q", out)
	}
}

func TestSetTomlBlockKeyRefusesUnsafeShapes(t *testing.T) {
	cases := map[string]string{
		"inline table":                      "hooks = { exclude_commands = [] }\n",
		"dotted root key":                   "hooks.exclude_commands = []\n",
		"array of tables":                   "[[hooks]]\nexclude_commands = []\n",
		"quoted header":                     "[\"hooks\"]\nexclude_commands = []\n",
		"duplicate header":                  "[hooks]\na = 1\n\n[hooks]\nb = 2\n",
		"multi-line string":                 "[other]\ntext = \"\"\"\n[hooks]\n\"\"\"\n",
		"array of arrays before dotted key": "a = [\n  [1, 2],\n  [3, 4]\n]\nhooks.suppress_hook_warning = true\n",
	}
	for name, src := range cases {
		out, created, ok := SetTomlBlockKey(src, "hooks", "suppress_hook_warning", "true")
		if ok {
			t.Fatalf("%s: expected refusal, got %q (created=%v)", name, out, created)
		}
		if out != src {
			t.Fatalf("%s: source changed on refusal:\n%q", name, out)
		}
	}
}

func TestSetTomlBlockKeyAllowsChildTables(t *testing.T) {
	src := "[hooks]\nexclude_commands = []\n\n[hooks.state]\nlast = \"x\"\n"
	out, _, ok := SetTomlBlockKey(src, "hooks", "suppress_hook_warning", "true")
	if !ok {
		t.Fatal("child table must not block the edit")
	}
	if !strings.Contains(out, "[hooks]\nsuppress_hook_warning = true\nexclude_commands = []\n\n[hooks.state]") {
		t.Fatalf("key not placed in direct body:\n%s", out)
	}
}

func TestTomlBlockRawField(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
		ok   bool
	}{
		{"table", "[hooks]\nsuppress_hook_warning = true\n", "true", true},
		{"table quoted key", "[hooks]\n\"suppress_hook_warning\" = false\n", "false", true},
		{"table commented", "[hooks]\nsuppress_hook_warning = true # keep\n", "true", true},
		{"dotted root", "hooks.suppress_hook_warning = true\n[tee]\nmode = \"x\"\n", "true", true},
		{"dotted quoted segments", "\"hooks\".'suppress_hook_warning' = true\n", "true", true},
		{"child table only", "[hooks.state]\nsuppress_hook_warning = true\n", "", false},
		{"missing", "[hooks]\nexclude_commands = []\n", "", false},
		{"quoted header ignored", "[\"hooks\"]\nsuppress_hook_warning = true\n", "", false},
	}
	for _, c := range cases {
		got, ok := TomlBlockRawField(c.src, "hooks", "suppress_hook_warning")
		if got != c.want || ok != c.ok {
			t.Fatalf("%s: got (%q,%v) want (%q,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestRemoveTomlBlockKey(t *testing.T) {
	src := "[hooks]\nexclude_commands = [\"curl\"]\nsuppress_hook_warning = true\n\n[tee]\nmode = \"never\"\n"
	out, removed, empty, ok := RemoveTomlBlockKey(src, "hooks", "suppress_hook_warning")
	if !ok || !removed || empty {
		t.Fatalf("ok=%v removed=%v empty=%v", ok, removed, empty)
	}
	want := "[hooks]\nexclude_commands = [\"curl\"]\n\n[tee]\nmode = \"never\"\n"
	if out != want {
		t.Fatalf("out = %q want %q", out, want)
	}
}

func TestRemoveTomlBlockKeyReportsEmptyTable(t *testing.T) {
	src := "[hooks]\nsuppress_hook_warning = true\n\n[tee]\nmode = \"never\"\n"
	out, removed, empty, ok := RemoveTomlBlockKey(src, "hooks", "suppress_hook_warning")
	if !ok || !removed || !empty {
		t.Fatalf("ok=%v removed=%v empty=%v", ok, removed, empty)
	}
	if strings.Contains(out, "suppress_hook_warning") {
		t.Fatalf("key still present:\n%s", out)
	}
	if got := RemoveBlock(out, "hooks"); strings.Contains(got, "[hooks]") {
		t.Fatalf("RemoveBlock left header:\n%s", got)
	}
}

func TestRemoveTomlBlockKeyMissing(t *testing.T) {
	src := "[hooks]\nexclude_commands = []\n"
	out, removed, _, ok := RemoveTomlBlockKey(src, "hooks", "suppress_hook_warning")
	if !ok || removed || out != src {
		t.Fatalf("ok=%v removed=%v out=%q", ok, removed, out)
	}
}

// rtkConfigDefault matches `rtk config --create` output.
const rtkConfigDefault = `[tracking]
enabled = true
history_days = 7

[display]
colors = true
emoji = true
max_width = 120

[telemetry]
enabled = true

[hooks]
exclude_commands = []
transparent_prefixes = []
suppress_hook_warning = false
wrap_fish_scripts = false
`

func TestSetTomlBlockKeyOnRtkDefaultConfig(t *testing.T) {
	out, created, ok := SetTomlBlockKey(rtkConfigDefault, "hooks", "suppress_hook_warning", "true")
	if !ok || created {
		t.Fatalf("ok=%v created=%v", ok, created)
	}
	if !strings.Contains(out, "suppress_hook_warning = true\n") {
		t.Fatalf("value not flipped:\n%s", out)
	}
	for _, keep := range []string{"[telemetry]\nenabled = true", "exclude_commands = []", "transparent_prefixes = []", "wrap_fish_scripts = false", "[tracking]", "history_days = 7", "[display]"} {
		if !strings.Contains(out, keep) {
			t.Fatalf("lost %q:\n%s", keep, out)
		}
	}
	if strings.Count(out, "suppress_hook_warning") != 1 {
		t.Fatalf("duplicate key:\n%s", out)
	}
}

func TestRemoveTomlBlock(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"lf", "[hooks]\nsuppress_hook_warning = true\n\n[tee]\nmode = \"never\"\n", "[tee]\nmode = \"never\"\n"},
		{"crlf", "[hooks]\r\nsuppress_hook_warning = true\r\n\r\n[tee]\r\nmode = \"never\"\r\n", "[tee]\r\nmode = \"never\"\r\n"},
		{"bom", "\uFEFF[hooks]\nsuppress_hook_warning = true\n", "\uFEFF"},
		{"keeps child table", "[hooks]\nsuppress_hook_warning = true\n\n[hooks.state]\nlast = \"x\"\n", "[hooks.state]\nlast = \"x\"\n"},
	}
	for _, c := range cases {
		out, ok := RemoveTomlBlock(c.src, "hooks")
		if !ok {
			t.Fatalf("%s: removal refused", c.name)
		}
		if out != c.want {
			t.Fatalf("%s: out = %q want %q", c.name, out, c.want)
		}
	}
}

func TestRemoveTomlBlockRefusesUnsafeShapes(t *testing.T) {
	for name, src := range map[string]string{
		"inline table":  "hooks = { a = 1 }\n",
		"quoted header": "[\"hooks\"]\na = 1\n",
		"array table":   "[[hooks]]\na = 1\n",
	} {
		out, ok := RemoveTomlBlock(src, "hooks")
		if ok || out != src {
			t.Fatalf("%s: ok=%v out=%q", name, ok, out)
		}
	}
	if out, ok := RemoveTomlBlock("[tee]\nmode = \"x\"\n", "hooks"); ok || out != "[tee]\nmode = \"x\"\n" {
		t.Fatalf("missing table reported as removed")
	}
}

// A header on the last line with no trailing newline must not swallow the key.
func TestSetTomlBlockKeyHeaderAtEOFWithoutNewline(t *testing.T) {
	cases := map[string]string{
		"header only":         "[hooks]",
		"after a root key":    "a = 1\n[hooks]",
		"crlf header at eof":  "[display]\r\ncolors = true\r\n[hooks]",
		"header with comment": "[hooks] # c",
	}
	for name, src := range cases {
		out, created, ok := SetTomlBlockKey(src, "hooks", "suppress_hook_warning", "true")
		if !ok {
			t.Fatalf("%s: edit refused", name)
		}
		if created {
			t.Fatalf("%s: table reported as created", name)
		}
		if got, has := TomlBlockRawField(out, "hooks", "suppress_hook_warning"); !has || got != "true" {
			t.Fatalf("%s: key does not read back (%q, %v): %q", name, got, has, out)
		}
		if strings.Contains(out, "[hooks]suppress") || strings.Contains(out, "# csuppress") {
			t.Fatalf("%s: key spliced into the header line: %q", name, out)
		}
	}
}
