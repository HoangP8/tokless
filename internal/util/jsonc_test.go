package util

import (
	"encoding/json"
	"reflect"
	"testing"
	"unicode/utf16"
)

func TestParseJsoncPreservesStrings(t *testing.T) {
	for _, value := range []string{
		",]",
		",}",
		"a, ]",
		"a,\t}",
		`escaped quote: ",]`,
		`escaped backslash: \,}`,
		`ends with backslash: \`,
		`https://example.test/a,] /* text,} */`,
	} {
		t.Run(value, func(t *testing.T) {
			input, err := json.Marshal(map[string]string{value: value})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := ParseJsonc(string(input))
			if err != nil {
				t.Fatal(err)
			}
			got, ok := cfg.Get(value)
			if !ok || got != value {
				t.Fatalf("Get(%q) = %#v, %v; want %q, true", value, got, ok, value)
			}
		})
	}
}

func TestParseJsoncPreservesMCPArgsWithTrailingCommas(t *testing.T) {
	input := `{
		"mcpServers": {
			"custom": {
				"command": "printf",
				"args": ["%s", ",]", ",}", // keep these arguments intact
				],
			}, /* object trailing comma */
		},
	}`
	cfg, err := ParseJsonc(input)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(StringifyJSON(cfg)), &got); err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(`{"mcpServers":{"custom":{"command":"printf","args":["%s",",]",",}"]}}}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed config = %#v, want %#v", got, want)
	}
}

func TestParseJsoncTrailingCommaSeparators(t *testing.T) {
	for _, tt := range []struct {
		name      string
		separator string
	}{
		{"none", ""},
		{"space", " "},
		{"tab", "\t"},
		{"carriage return", "\r"},
		{"newline", "\n"},
		{"mixed whitespace", " \t\r\n"},
		{"block comment", "/* comment */"},
		{"line comment", "// comment\n"},
		{"line comment CRLF", "// comment\r\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := `{"values":[1,` + tt.separator + `],` + tt.separator + `}`
			cfg, err := ParseJsonc(input)
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != `{"values":[1]}` {
				t.Fatalf("parsed config = %s; want {\"values\":[1]}", got)
			}
		})
	}
}

func TestParseJsoncRejectsInvalidCommas(t *testing.T) {
	for _, input := range []string{
		`{"values":[1,,]}`,
		`{"values":[1,,2]}`,
		`{"value":1,,}`,
		`{"value":1,,"other":2}`,
		`{"values":[1,/* comment */,]}`,
		`{"value":1,/* comment */,}`,
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseJsonc(input); err == nil {
				t.Fatal("expected invalid commas to produce a parse error")
			}
		})
	}
}

func TestParseJsoncStripsComments(t *testing.T) {
	input := `{
		// line comment
		"key": "value",
		/* block
		   comment */
		"number": 123,
	}`
	om, err := ParseJsonc(input)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if om == nil {
		t.Fatal("expected ordered map, got nil")
	}
	v, ok := om.Get("key")
	if !ok || v != "value" {
		t.Errorf("expected key=value, got %v (ok=%v)", v, ok)
	}
	n, ok := om.Get("number")
	if !ok {
		t.Errorf("expected number key, not found")
	} else {
		// encoding/json decodes numbers into json.Number under UseNumber()
		// or float64. Let's see if we can convert it or match string representation.
		if s, ok := n.(interface{ String() string }); ok {
			if s.String() != "123" {
				t.Errorf("expected number 123, got: %s", s.String())
			}
		} else if f, ok := n.(float64); ok {
			if f != 123 {
				t.Errorf("expected number 123, got: %f", f)
			}
		} else {
			t.Errorf("unexpected type for number: %T", n)
		}
	}
}

func TestParseJsoncAcceptsCommentsAfterValues(t *testing.T) {
	om, err := ParseJsonc(`{
		"mcpServers": {
			"context-mode": {"command": "tokless"}, // keep user comments parseable
		},
		/* trailing file comment */
	}`)
	if err != nil {
		t.Fatalf("expected valid JSONC, got: %v", err)
	}
	servers, ok := om.Get("mcpServers")
	if !ok {
		t.Fatal("expected mcpServers")
	}
	if _, ok := servers.(*OrderedMap); !ok {
		t.Fatalf("mcpServers type = %T, want *OrderedMap", servers)
	}
}

func TestOrderedMapPreservesOrder(t *testing.T) {
	input := `{"z":1,"a":2,"m":3}`
	om, err := ParseJsonc(input)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	keys := om.Keys()
	expected := []string{"z", "a", "m"}
	if !reflect.DeepEqual(keys, expected) {
		t.Errorf("expected keys %v, got %v", expected, keys)
	}

	om.Set("b", 4)
	keys = om.Keys()
	expectedWithB := []string{"z", "a", "m", "b"}
	if !reflect.DeepEqual(keys, expectedWithB) {
		t.Errorf("expected keys %v after Set, got %v", expectedWithB, keys)
	}

	serialized := StringifyJSON(om)
	// Serialized should have insertion order: z, a, m, b
	// We can check if z appears before a, before m, before b
	zIdx := reflect.ValueOf(serialized).Interface().(string)
	// We just parse it again or check string contents
	expectedStr := `{"z":1,"a":2,"m":3,"b":4}`
	// Strip spaces and newlines for comparison
	compacted := ""
	for _, char := range zIdx {
		if char != ' ' && char != '\n' && char != '\t' && char != '\r' {
			compacted += string(char)
		}
	}
	if compacted != expectedStr {
		t.Errorf("expected compacted %q, got %q", expectedStr, compacted)
	}
}

func TestOrderedMapDelete(t *testing.T) {
	om := NewOrderedMap()
	om.Set("z", 1)
	om.Set("a", 2)
	om.Set("m", 3)

	om.Delete("a")
	keys := om.Keys()
	expected := []string{"z", "m"}
	if !reflect.DeepEqual(keys, expected) {
		t.Errorf("expected keys %v, got %v", expected, keys)
	}
	if _, ok := om.Get("a"); ok {
		t.Error("expected key 'a' to be deleted")
	}
}

func TestStringifyJSON(t *testing.T) {
	om := NewOrderedMap()
	om.Set("key", "val")
	got := StringifyJSON(om)
	// Should end with newline and contain "\n  " for 2-space indent
	if got == "" || got[len(got)-1] != '\n' {
		t.Errorf("expected output to end with newline, got %q", got)
	}
	// With 2 space indent it should look like:
	// {\n  "key": "val"\n}\n
	// So containing "\n  " is expected
	expectedSubstring := "\n  "
	found := false
	for i := 0; i <= len(got)-len(expectedSubstring); i++ {
		if got[i:i+len(expectedSubstring)] == expectedSubstring {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected output to contain %q, got %q", expectedSubstring, got)
	}
}

func TestTryParseJsoncBadInput(t *testing.T) {
	got := TryParseJsonc("{ not json")
	if got != nil {
		t.Errorf("expected nil for bad json, got: %v", got)
	}
}

func TestHasJSONCComments(t *testing.T) {
	if HasJSONCComments(`{"url":"https://example.test/a//b", "x":"/* not comment */"}`) {
		t.Fatal("string markers must not count as comments")
	}
	if !HasJSONCComments("{\"url\":\"https://example.test/a\" // comment\n}") {
		t.Fatal("expected comment marker outside string")
	}
}

func utf16Bytes(s string, bigEndian bool, bom bool) string {
	var out []byte
	put := func(u uint16) {
		if bigEndian {
			out = append(out, byte(u>>8), byte(u))
		} else {
			out = append(out, byte(u), byte(u>>8))
		}
	}
	if bom {
		put(0xFEFF)
	}
	for _, u := range utf16.Encode([]rune(s)) {
		put(u)
	}
	return string(out)
}

func TestParseJsoncNormalizesEncodings(t *testing.T) {
	const doc = `{"a":"b"} `
	cases := map[string]string{
		"utf8 bom":      "\xEF\xBB\xBF" + doc,
		"utf16le bom":   utf16Bytes(doc, false, true),
		"utf16be bom":   utf16Bytes(doc, true, true),
		"utf16le nobom": utf16Bytes(doc, false, false),
		"utf16be nobom": utf16Bytes(doc, true, false),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := ParseJsonc(input)
			if err != nil {
				t.Fatalf("ParseJsonc: %v", err)
			}
			if v, _ := cfg.Get("a"); v != "b" {
				t.Fatalf("a = %v, want b", v)
			}
		})
	}
}

func TestParseJsoncEmptyAndBomOnly(t *testing.T) {
	for name, input := range map[string]string{
		"empty":         "",
		"utf8 bom":      "\xEF\xBB\xBF",
		"utf16le bom":   "\xFF\xFE",
		"utf16be bom":   "\xFE\xFF",
		"odd utf16 bom": "\xFF\xFE{",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := ParseJsonc(input)
			if err != nil {
				t.Fatalf("ParseJsonc: %v", err)
			}
			if cfg.Len() != 0 {
				t.Fatalf("want empty map, got %d keys", cfg.Len())
			}
		})
	}
}

func TestParseJsoncOddLengthUTF16DoesNotPanic(t *testing.T) {
	input := utf16Bytes(`{"a":1}`, false, true) + "\x00"
	cfg, err := ParseJsonc(input)
	if err != nil {
		t.Fatalf("ParseJsonc: %v", err)
	}
	if _, ok := cfg.Get("a"); !ok {
		t.Fatal("expected key a")
	}
}

func TestParseJsoncKeepsFEFFInsideString(t *testing.T) {
	fe := string(rune(0xFEFF))
	cfg, err := ParseJsonc("\xEF\xBB\xBF{\"a\":\"x" + fe + "y\"}")
	if err != nil {
		t.Fatalf("ParseJsonc: %v", err)
	}
	if v, _ := cfg.Get("a"); v != "x"+fe+"y" {
		t.Fatalf("a = %q, want embedded U+FEFF preserved", v)
	}
}

func TestHasJSONCCommentsUTF16(t *testing.T) {
	if !HasJSONCComments(utf16Bytes("{\"a\":1 // c\n}", false, true)) {
		t.Fatal("expected comment in UTF-16 input")
	}
	if HasJSONCComments(utf16Bytes(`{"a":"//x"}`, true, true)) {
		t.Fatal("string marker in UTF-16 input must not count")
	}
}
