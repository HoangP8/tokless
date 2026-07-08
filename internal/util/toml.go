package util

import (
	"regexp"
	"strconv"
	"strings"
)

// TomlValue is string | float64 | bool | []string | map[string]string.
type TomlValue any

// TomlBlock is a single [header] table with scalar/array/inline fields.
type TomlBlock struct {
	Header string
	// Fields preserves insertion order for stable output.
	Keys   []string
	Fields map[string]TomlValue
}

// TomlSingleQuoted returns a TOML literal string (single quotes).
func TomlSingleQuoted(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

// TomlDottedTableHeader is the inner name for [prefix.'pathKey'] (no brackets).
func TomlDottedTableHeader(prefix, pathKey string) string {
	return prefix + "." + TomlSingleQuoted(pathKey)
}

// NewTomlBlock builds a block with ordered keys.
func NewTomlBlock(header string) *TomlBlock {
	return &TomlBlock{Header: header, Fields: map[string]TomlValue{}}
}

func (b *TomlBlock) Set(k string, v TomlValue) {
	if _, ok := b.Fields[k]; !ok {
		b.Keys = append(b.Keys, k)
	}
	b.Fields[k] = v
}

func tomlEscapeStr(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func fmtTomlValue(v TomlValue) string {
	switch t := v.(type) {
	case string:
		return `"` + tomlEscapeStr(t) + `"`
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case []string:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = `"` + tomlEscapeStr(x) + `"`
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]string:
		var parts []string
		for k, val := range t {
			parts = append(parts, k+` = "`+tomlEscapeStr(val)+`"`)
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	return ""
}

// RenderBlock serializes a block; an "env" map field becomes a sub-table.
func RenderBlock(block *TomlBlock) string {
	lines := []string{"[" + block.Header + "]"}
	for _, k := range block.Keys {
		v := block.Fields[k]
		if v == nil {
			continue
		}
		if k == "env" {
			if _, ok := v.(map[string]string); ok {
				continue
			}
		}
		lines = append(lines, k+" = "+fmtTomlValue(v))
	}
	if env, ok := block.Fields["env"].(map[string]string); ok {
		lines = append(lines, "")
		lines = append(lines, "["+block.Header+".env]")
		for k, v := range env {
			lines = append(lines, k+` = "`+tomlEscapeStr(v)+`"`)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func escapeRe(s string) string {
	return regexp.QuoteMeta(s)
}

type blockRange struct{ start, end int }

// findBlockRange locates [header] and the slice up to the next sibling header.
func findBlockRange(src, header string) *blockRange {
	reHeader := regexp.MustCompile(`(?m)^\[\s*` + escapeRe(header) + `\s*\]\s*$`)
	loc := reHeader.FindStringIndex(src)
	if loc == nil {
		return nil
	}
	start := loc[0]
	reNext := regexp.MustCompile(`(?m)^\[(\s*[^\]]+)\]\s*$`)
	rest := src[loc[1]:]
	offset := loc[1]
	for _, m := range reNext.FindAllStringSubmatchIndex(rest, -1) {
		candidate := strings.TrimSpace(rest[m[2]:m[3]])
		if candidate == "" {
			continue
		}
		if candidate == header || strings.HasPrefix(candidate, header+".") {
			continue
		}
		return &blockRange{start: start, end: offset + m[0]}
	}
	return &blockRange{start: start, end: len(src)}
}

var reScalarField = regexp.MustCompile(`^\s*([A-Za-z0-9_-]+)\s*=\s*(.+?)\s*$`)

func parseScalarFields(blockText string) *TomlBlock {
	b := NewTomlBlock("")
	for _, line := range strings.Split(blockText, "\n") {
		m := reScalarField.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, raw := m[1], m[2]
		if strings.HasPrefix(raw, "[") || strings.HasPrefix(raw, "{") {
			continue
		}
		switch {
		case raw == "true" || raw == "false":
			b.Set(key, raw == "true")
		case regexp.MustCompile(`^-?\d+(\.\d+)?$`).MatchString(raw):
			f, _ := strconv.ParseFloat(raw, 64)
			b.Set(key, f)
		case strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`):
			b.Set(key, raw[1:len(raw)-1])
		}
	}
	return b
}

// UpsertBlock inserts or replaces a block; merge keeps existing scalar fields.
func UpsertBlock(src string, block *TomlBlock, merge bool) string {
	effective := block
	if merge {
		if r := findBlockRange(src, block.Header); r != nil {
			existing := parseScalarFields(src[r.start:r.end])
			merged := NewTomlBlock(block.Header)
			for _, k := range existing.Keys {
				merged.Set(k, existing.Fields[k])
			}
			for _, k := range block.Keys {
				merged.Set(k, block.Fields[k])
			}
			effective = merged
		}
	}
	rendered := RenderBlock(effective)
	r := findBlockRange(src, block.Header)
	if r == nil {
		sep := "\n\n"
		switch {
		case len(src) == 0 || strings.HasSuffix(src, "\n\n"):
			sep = ""
		case strings.HasSuffix(src, "\n"):
			sep = "\n"
		}
		return src + sep + rendered
	}
	before := src[:r.start]
	after := src[r.end:]
	// Only normalize a trailing newline when there is preceding content.
	if before != "" && !strings.HasSuffix(before, "\n") {
		before += "\n"
	}
	switch {
	case strings.HasPrefix(after, "\n"):
		// keep as-is
	case after == "":
		// keep
	default:
		after = "\n" + after
	}
	return before + rendered + after
}



func RemoveBlock(src, header string) string {
	r := findBlockRange(src, header)
	if r == nil {
		return src
	}
	return src[:r.start] + src[r.end:]
}

func HasBlock(src, header string) bool {
	return findBlockRange(src, header) != nil
}

// SetTomlTopKey sets/replaces a root-level string key, placed before any [section].
func SetTomlTopKey(src, key, value string) string {
	line := key + ` = "` + tomlEscapeStr(value) + `"`
	re := regexp.MustCompile(`(?m)^` + escapeRe(key) + `\s*=.*$`)
	if re.MatchString(src) {
		return re.ReplaceAllString(src, line)
	}
	if src == "" {
		return line + "\n"
	}
	return line + "\n" + src
}

// GetTomlTopKey reads a root-level string key's value ("" if absent).
func GetTomlTopKey(src, key string) string {
	re := regexp.MustCompile(`(?m)^` + escapeRe(key) + `\s*=\s*"([^"]*)"`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		return ""
	}
	return m[1]
}

// RemoveTomlTopKey deletes a root-level string key line. No-op if absent.
func RemoveTomlTopKey(src, key string) string {
	re := regexp.MustCompile(`(?m)^` + escapeRe(key) + `\s*=.*\n?`)
	return re.ReplaceAllString(src, "")
}
