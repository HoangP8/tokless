package util

import (
	"regexp"
	"strings"
)

const tomlBOM = "\uFEFF"

type tomlStyle struct {
	bom  bool
	crlf bool
}

func tomlNormalize(src string) (string, tomlStyle) {
	var st tomlStyle
	if strings.HasPrefix(src, tomlBOM) {
		st.bom = true
		src = strings.TrimPrefix(src, tomlBOM)
	}
	if strings.Contains(src, "\r\n") {
		st.crlf = true
		src = strings.ReplaceAll(src, "\r\n", "\n")
	}
	return src, st
}

func tomlRestore(src string, st tomlStyle) string {
	if st.crlf {
		src = strings.ReplaceAll(src, "\n", "\r\n")
	}
	if st.bom {
		src = tomlBOM + src
	}
	return src
}

func tomlKeyPattern(key string) string {
	k := regexp.QuoteMeta(key)
	return `(?:` + k + `|"` + k + `"|'` + k + `')`
}

func tomlHeaderRegexp(header string) *regexp.Regexp {
	return regexp.MustCompile(`^\[\s*` + regexp.QuoteMeta(header) + `\s*\]$`)
}

func tomlQuotedHeaderRegexp(header string) *regexp.Regexp {
	h := regexp.QuoteMeta(header)
	return regexp.MustCompile(`^\[\s*(?:"` + h + `"|'` + h + `')\s*\]$`)
}

// tomlLine spans one source line.
type tomlLine struct {
	start, end int
	next       int
	text       string
}

func tomlWalk(src string, fn func(tomlLine)) {
	for off := 0; off < len(src); {
		nl := strings.IndexByte(src[off:], '\n')
		end := len(src)
		next := len(src)
		if nl >= 0 {
			end = off + nl
			next = end + 1
		}
		fn(tomlLine{start: off, end: end, next: next, text: src[off:end]})
		if next <= off {
			return
		}
		off = next
	}
}

// tomlRootEnd is where root keys end and the first table starts.
func tomlRootEnd(src string) int {
	rootEnd := len(src)
	tomlWalk(src, func(l tomlLine) {
		if rootEnd != len(src) {
			return
		}
		t := strings.TrimSpace(stripTomlComment(l.text))
		if isHeaderLine(t) {
			rootEnd = l.start
		}
	})
	return rootEnd
}

// tomlBlockSpan locates the single [header] table and its direct body.
func tomlBlockSpan(src, header string) (headerEnd, bodyEnd, headers, quoted, arrays int, ok bool) {
	hdr := tomlHeaderRegexp(header)
	quotedRE := tomlQuotedHeaderRegexp(header)
	arr := regexp.MustCompile(`^\[\[\s*` + regexp.QuoteMeta(header) + `\s*\]\]$`)
	headerEnd = -1
	bodyEnd = len(src)
	seenHeader := false
	tomlWalk(src, func(l tomlLine) {
		t := strings.TrimSpace(stripTomlComment(l.text))
		switch {
		case hdr.MatchString(t):
			headers++
			if !seenHeader {
				seenHeader = true
				headerEnd = l.next
			}
		case quotedRE.MatchString(t):
			quoted++
		case arr.MatchString(t):
			arrays++
		default:
			if seenHeader && bodyEnd == len(src) && isHeaderLine(t) {
				bodyEnd = l.start
			}
		}
	})
	if headers == 0 {
		return headerEnd, bodyEnd, headers, quoted, arrays, false
	}
	return headerEnd, bodyEnd, headers, quoted, arrays, true
}

// tomlUnsafeReason reports why a block edit would risk corrupting the file.
func tomlUnsafeReason(src, header string) string {
	if strings.Contains(src, `"""`) || strings.Contains(src, "'''") {
		return "multi-line string"
	}
	if _, _, headers, quoted, arrays, _ := tomlBlockSpan(src, header); quoted > 0 || arrays > 0 || headers > 1 {
		return "ambiguous table shape"
	}
	inline := regexp.MustCompile(`(?m)^[ \t]*` + tomlKeyPattern(header) + `[ \t]*=`)
	if inline.MatchString(src) {
		return "inline table"
	}
	dotted := regexp.MustCompile(`(?m)^[ \t]*` + tomlKeyPattern(header) + `[ \t]*\.`)
	if dotted.MatchString(src) {
		return "dotted keys"
	}
	return ""
}

func tomlKeyLineRegexp(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[ \t]*` + tomlKeyPattern(key) + `[ \t]*=[ \t]*(.*)$`)
}

func tomlDottedKeyLineRegexp(header, key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[ \t]*` + tomlKeyPattern(header) + `[ \t]*\.[ \t]*` + tomlKeyPattern(key) + `[ \t]*=[ \t]*(.*)$`)
}

// TomlBlockRawField reads a raw value from [header] or the root dotted form.
func TomlBlockRawField(src, header, key string) (string, bool) {
	norm, _ := tomlNormalize(src)
	rootEnd := tomlRootEnd(norm)
	if m := tomlDottedKeyLineRegexp(header, key).FindStringSubmatch(norm[:rootEnd]); m != nil {
		if v := strings.TrimSpace(stripTomlComment(m[1])); v != "" {
			return v, true
		}
	}
	headerEnd, bodyEnd, _, _, _, ok := tomlBlockSpan(norm, header)
	if !ok {
		return "", false
	}
	body := norm[headerEnd:bodyEnd]
	if m := tomlKeyLineRegexp(key).FindStringSubmatch(body); m != nil {
		if v := strings.TrimSpace(stripTomlComment(m[1])); v != "" {
			return v, true
		}
	}
	return "", false
}

// SetTomlBlockKey sets key in [header], editing only that line.
func SetTomlBlockKey(src, header, key, rawValue string) (out string, tableCreated, ok bool) {
	norm, st := tomlNormalize(src)
	if tomlUnsafeReason(norm, header) != "" {
		return src, false, false
	}
	if norm != "" && !strings.HasSuffix(norm, "\n") {
		norm += "\n"
	}
	line := key + " = " + rawValue
	headerEnd, bodyEnd, headers, _, _, _ := tomlBlockSpan(norm, header)
	if headers == 0 {
		if strings.TrimSpace(norm) == "" {
			return tomlVerified(src, "["+header+"]\n"+line+"\n", st, header, key, rawValue, true)
		}
		return tomlVerified(src, norm+"\n["+header+"]\n"+line+"\n", st, header, key, rawValue, true)
	}
	body := norm[headerEnd:bodyEnd]
	if loc := tomlKeyLineRegexp(key).FindStringIndex(body); loc != nil {
		start := headerEnd + loc[0]
		end := headerEnd + loc[1]
		return tomlVerified(src, norm[:start]+line+norm[end:], st, header, key, rawValue, false)
	}
	return tomlVerified(src, norm[:headerEnd]+line+"\n"+norm[headerEnd:], st, header, key, rawValue, false)
}

// tomlVerified rejects edits that do not read back as intended.
func tomlVerified(src, next string, st tomlStyle, header, key, rawValue string, tableCreated bool) (string, bool, bool) {
	out := tomlRestore(next, st)
	if v, ok := TomlBlockRawField(out, header, key); !ok || v != rawValue {
		return src, false, false
	}
	return out, tableCreated, true
}

// RemoveTomlBlockKey deletes key from [header].
func RemoveTomlBlockKey(src, header, key string) (out string, removed, tableEmpty, ok bool) {
	norm, st := tomlNormalize(src)
	if tomlUnsafeReason(norm, header) != "" {
		return src, false, false, false
	}
	headerEnd, bodyEnd, _, _, _, found := tomlBlockSpan(norm, header)
	if !found {
		return src, false, false, true
	}
	body := norm[headerEnd:bodyEnd]
	loc := tomlKeyLineRegexp(key).FindStringIndex(body)
	if loc == nil {
		return src, false, false, true
	}
	start := headerEnd + loc[0]
	end := headerEnd + loc[1]
	if end < len(norm) && norm[end] == '\n' {
		end++
	}
	removedLen := end - start
	next := norm[:start] + norm[end:]
	for _, l := range strings.Split(next[headerEnd:bodyEnd-removedLen], "\n") {
		if t := strings.TrimSpace(stripTomlComment(l)); t != "" {
			return tomlRestore(next, st), true, false, true
		}
	}
	return tomlRestore(next, st), true, true, true
}

// RemoveTomlBlock deletes [header] and its body, keeping child tables.
func RemoveTomlBlock(src, header string) (string, bool) {
	norm, st := tomlNormalize(src)
	if tomlUnsafeReason(norm, header) != "" {
		return src, false
	}
	hdr := tomlHeaderRegexp(header)
	start, end := -1, -1
	tomlWalk(norm, func(l tomlLine) {
		t := strings.TrimSpace(stripTomlComment(l.text))
		switch {
		case start < 0 && hdr.MatchString(t):
			start = l.start
		case start >= 0 && end < 0 && isHeaderLine(t):
			end = l.start
		}
	})
	if start < 0 {
		return src, false
	}
	if end < 0 {
		end = len(norm)
	}
	if end == len(norm) && start >= 2 && norm[start-1] == '\n' && norm[start-2] == '\n' {
		start--
	}
	return tomlRestore(norm[:start]+norm[end:], st), true
}
