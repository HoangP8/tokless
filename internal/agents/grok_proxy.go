package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// Grok BYOK wiring mirrors the OpenCode harness: every user-declared
// [model_providers.*] block keeps its identity, model names, keys, and backend
// fields verbatim.

func GrokProxyApplicable() bool {
	st, err := os.Stat(grokConfigFile())
	return err == nil && st.Mode().IsRegular()
}

// --- stash: original base_url per provider id (no secrets) ---

type grokStashEntry struct {
	BaseURL     string `json:"base_url"`
	BaseLine    string `json:"base_line,omitempty"`
	Header      string `json:"header,omitempty"`
	HeaderRaw   string `json:"header_raw,omitempty"`
	HeaderChild bool   `json:"header_child,omitempty"`
	HeaderSet   bool   `json:"header_set,omitempty"`
	PathRaw     string `json:"path_raw,omitempty"`
	PathChild   bool   `json:"path_child,omitempty"`
	PathSet     bool   `json:"path_set,omitempty"`
}

type grokStashFile struct {
	Providers map[string]grokStashEntry `json:"providers"`
}

func grokStashPath() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "grok.proxy.stash.json")
}

func loadGrokStash() map[string]grokStashEntry {
	raw, ok := util.ReadFileSafe(grokStashPath())
	if !ok {
		return map[string]grokStashEntry{}
	}
	var f grokStashFile
	if json.Unmarshal([]byte(raw), &f) != nil || f.Providers == nil {
		return map[string]grokStashEntry{}
	}
	return f.Providers
}

func grokStashValid() bool {
	raw, ok := util.ReadFileSafe(grokStashPath())
	if !ok {
		return true
	}
	var f grokStashFile
	return json.Unmarshal([]byte(raw), &f) == nil && f.Providers != nil
}

func saveGrokStash(m map[string]grokStashEntry) error {
	b, err := json.Marshal(grokStashFile{Providers: m})
	if err != nil {
		return err
	}
	path := grokStashPath()
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func clearGrokStash() error {
	err := os.Remove(grokStashPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// --- discovery ---

var reGrokProviderHeader = regexp.MustCompile(`(?m)^\[model_providers\.("[^"]+"|'[^']+'|[A-Za-z0-9_-]+)\][ \t]*(?:#.*)?$`)

// grokLocalBYOK lists user-declared provider ids that carry an absolute http(s)
// base_url distinct from the proxy plus an api_key.
func grokLocalBYOK(raw string) []string {
	endpoint := ProxyEndpointFor("grok")
	var ids []string
	for _, m := range reGrokProviderHeader.FindAllStringSubmatch(raw, -1) {
		id := m[1]
		table := "model_providers." + id
		base := util.TomlBlockField(raw, table, "base_url")
		key := util.TomlBlockField(raw, table, "api_key")
		if base == "" || key == "" || !isAbsoluteHTTP(base) || sameProxyBase(base, endpoint) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// --- route edit primitives ---

var (
	reGrokBaseURLLine   = regexp.MustCompile(`(?m)^([ \t]*)(base_url|"base_url"|'base_url')([ \t]*=[ \t]*)(?:"[^"]*"|'[^']*')`)
	reGrokBaseURLFull   = regexp.MustCompile(`(?m)^[ \t]*(?:base_url|"base_url"|'base_url')[ \t]*=[^\r\n]*(?:\r?\n|$)`)
	reGrokHeaderKV      = regexp.MustCompile(`(?:"x-headroom-base-url"|'x-headroom-base-url'|x-headroom-base-url)[ \t]*=[ \t]*(?:"(?:[^"\\]|\\.)*"|'[^']*')`)
	reGrokHeaderKVLine  = regexp.MustCompile(`^[ \t]*(?:"x-headroom-base-url"|'x-headroom-base-url'|x-headroom-base-url)[ \t]*=`)
	reGrokHeaderKVValue = regexp.MustCompile(`(?:"x-headroom-base-url"|'x-headroom-base-url'|x-headroom-base-url)[ \t]*=[ \t]*(?:"((?:[^"\\]|\\.)*)"|'([^']*)')`)
	reGrokHeaderIndent  = regexp.MustCompile(`^[ \t]*`)
	reGrokPathKV        = regexp.MustCompile(`(?:"x-headroom-original-path"|'x-headroom-original-path'|x-headroom-original-path)[ \t]*=[ \t]*(?:"(?:[^"\\]|\\.)*"|'[^']*')`)
	reGrokPathKVLine    = regexp.MustCompile(`^[ \t]*(?:"x-headroom-original-path"|'x-headroom-original-path'|x-headroom-original-path)[ \t]*=`)
	reGrokPathKVValue   = regexp.MustCompile(`(?:"x-headroom-original-path"|'x-headroom-original-path'|x-headroom-original-path)[ \t]*=[ \t]*(?:"((?:[^"\\]|\\.)*)"|'([^']*)')`)
)

// grokParentBody isolates only the parent table's own lines — its [header]
// line and any child tables ([id.*]) are excluded so inline edits can never
// touch nested content.
func grokParentBody(raw, table string) (string, bool) {
	body, ok := util.BlockText(raw, table)
	if !ok || body == "" {
		return "", false
	}
	lines := splitLinesKeepEnds(body)
	cut := len(lines[0])
	for _, line := range lines[1:] {
		t := grokTrimComment(line)
		if t != "" && t[0] == '[' && t[len(t)-1] == ']' {
			return body[:cut], true
		}
		cut += len(line)
	}
	return body, true
}

func grokEditParent(raw, table string, fn func(string) string) string {
	body, ok := grokParentBody(raw, table)
	if !ok || body == "" {
		return raw
	}
	return strings.Replace(raw, body, fn(body), 1)
}

func splitLinesKeepEnds(s string) []string {
	return strings.SplitAfter(s, "\n")
}

func grokSwapBaseURL(raw, table, to string) string {
	return grokEditParent(raw, table, func(b string) string {
		return reGrokBaseURLLine.ReplaceAllString(b, `${1}${2}${3}"`+to+`"`)
	})
}

func grokBaseURLLine(raw, id string) string {
	body, ok := grokParentBody(raw, "model_providers."+id)
	if !ok {
		return ""
	}
	return reGrokBaseURLFull.FindString(body)
}

func grokRestoreBaseURL(raw, id, line, fallback string) string {
	if line == "" {
		return grokSwapBaseURL(raw, "model_providers."+id, fallback)
	}
	return grokEditParent(raw, "model_providers."+id, func(body string) string {
		return reGrokBaseURLFull.ReplaceAllString(body, line)
	})
}

// grokInlineTable locates an extra_headers inline table inside the parent body
// using a quote-aware scanner.
type grokInlineSpan struct{ start, open, close, end int }

func grokFindInline(b string) (grokInlineSpan, bool) {
	reKey := regexp.MustCompile(`(?m)^[ \t]*(?:extra_headers|"extra_headers"|'extra_headers')[ \t]*=[ \t]*\{`)
	loc := reKey.FindStringIndex(b)
	if loc == nil {
		return grokInlineSpan{}, false
	}
	open := loc[1] - 1
	inStr, inLiteral, esc := false, false, false
	for i := open + 1; i < len(b); i++ {
		c := b[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if inLiteral {
			if c == '\'' {
				inLiteral = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '\'':
			inLiteral = true
		case '}':
			return grokInlineSpan{start: loc[0], open: open, close: i, end: i + 1}, true
		case '#':
			return grokInlineSpan{}, false
		}
	}
	return grokInlineSpan{}, false
}

// grokSetHeaderValue injects or replaces x-headroom-base-url for one provider.
func grokSetHeaderValue(raw, id, origin string) string {
	if current, ok := grokExistingHeader(raw, id); ok && current == origin {
		return raw
	}
	entry := headroomBaseURLHeader + ` = ` + util.TomlQuoted(origin)
	table := "model_providers." + id

	withInline := grokEditParent(raw, table, func(b string) string {
		sp, ok := grokFindInline(b)
		if !ok {
			return b
		}
		inner := b[sp.open+1 : sp.close]
		if reGrokHeaderKV.MatchString(inner) {
			inner = reGrokHeaderKV.ReplaceAllString(inner, entry)
		} else if strings.TrimSpace(inner) == "" {
			inner = " " + entry + " "
		} else {
			trimmed := strings.TrimLeft(inner, " \t")
			lead := inner[:len(inner)-len(trimmed)]
			inner = lead + entry + ", " + trimmed
		}
		return b[:sp.start] + b[sp.start:sp.open+1] + inner + b[sp.close:]
	})
	if withInline != raw {
		return withInline
	}

	cstart, cend, hasChild := grokChildSection(raw, id)
	if !hasChild {
		return grokEditParent(raw, table, func(b string) string {
			line := "extra_headers = { " + entry + " }\n"
			trimmed := strings.TrimRight(b, "\n")
			return trimmed + "\n" + line + "\n"
		})
	}
	lines := strings.Split(raw[cstart:cend], "\n")
	replaced := false
	for i, l := range lines {
		if reGrokHeaderKVLine.MatchString(l) {
			lines[i] = reGrokHeaderIndent.FindString(l) + entry
			replaced = true
			break
		}
	}
	if !replaced {
		insert := 1
		indent := ""
		for insert < len(lines) && strings.TrimSpace(lines[insert]) != "" {
			indent = reGrokHeaderIndent.FindString(lines[insert])
			insert++
		}
		lines = append(lines[:insert], append([]string{indent + entry}, lines[insert:]...)...)
	}
	return raw[:cstart] + strings.Join(lines, "\n") + raw[cend:]
}

// grokRemoveHeaderValue strips our header key back out, collapsing emptied
// inline tables or child tables.
func grokRemoveHeaderValue(raw, id string) string {
	table := "model_providers." + id

	withInline := grokEditParent(raw, table, func(b string) string {
		sp, ok := grokFindInline(b)
		if !ok || !reGrokHeaderKV.MatchString(b[sp.open+1:sp.close]) {
			return b
		}
		inner := reGrokHeaderKV.ReplaceAllString(b[sp.open+1:sp.close], "")
		if i := strings.Index(inner, ","); i >= 0 && strings.TrimSpace(inner[:i]) == "" {
			rest := inner[i+1:]
			rest = strings.TrimPrefix(rest, " ")
			inner = inner[:i] + rest
		}
		if strings.TrimSpace(inner) == "" {
			start, end := sp.start, sp.end
			if end < len(b) && b[end] == '\n' {
				end++
			} else if start > 0 && b[start-1] == '\n' {
				start--
			}
			return b[:start] + b[end:]
		}
		return b[:sp.open+1] + inner + b[sp.close:]
	})
	if withInline != raw {
		return withInline
	}

	cstart, cend, hasChild := grokChildSection(raw, id)
	if !hasChild {
		return raw
	}
	lines := strings.Split(raw[cstart:cend], "\n")
	var kept []string
	for _, l := range lines {
		if !reGrokHeaderKVLine.MatchString(l) {
			kept = append(kept, l)
		}
	}
	cleaned := strings.Join(kept, "\n")
	if len(kept) <= 2 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		return raw[:cstart] + raw[cend:]
	}
	return raw[:cstart] + cleaned + raw[cend:]
}

// grokChildSection returns the [start,end) span of an [id.extra_headers]
// child table.
func grokChildSection(raw, id string) (int, int, bool) {
	re := regexp.MustCompile(`(?m)^\[model_providers\.` + regexp.QuoteMeta(id) + `\.extra_headers\][ \t]*(?:#.*)?$`)
	loc := re.FindStringIndex(raw)
	if loc == nil {
		return 0, 0, false
	}
	end := len(raw)
	pos := 0
	for _, line := range splitLinesKeepEnds(raw[loc[1]:]) {
		t := grokTrimComment(line)
		if t != "" && strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			end = loc[1] + pos
			break
		}
		pos += len(line)
	}
	return loc[0], end, true
}

// grokTrimComment strips a trailing comment outside quoted strings.
func grokTrimComment(line string) string {
	inStr, inLiteral, esc := false, false, false
	cut := -1
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if inLiteral {
			if c == '\'' {
				inLiteral = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '\'':
			inLiteral = true
		case '#':
			cut = i
		}
	}
	if cut >= 0 {
		return strings.TrimRight(line[:cut], " \t\r\n")
	}
	return strings.TrimRight(line, "\r\n")
}

// grokExistingHeader returns the current x-headroom-base-url value for a
// provider block ("" when absent).
func grokExistingHeader(raw, id string) (string, bool) {
	if body, ok := grokParentBody(raw, "model_providers."+id); ok {
		if sp, found := grokFindInline(body); found {
			if m := reGrokHeaderKVValue.FindStringSubmatch(body[sp.open+1 : sp.close]); m != nil {
				if m[1] != "" {
					if value, err := strconv.Unquote(`"` + m[1] + `"`); err == nil {
						return value, true
					}
				}
				return m[2], true
			}
		}
	}
	if cs, ce, ok := grokChildSection(raw, id); ok {
		if m := reGrokHeaderKVValue.FindStringSubmatch(raw[cs:ce]); m != nil {
			if m[1] != "" {
				if value, err := strconv.Unquote(`"` + m[1] + `"`); err == nil {
					return value, true
				}
			}
			return m[2], true
		}
	}
	return "", false
}

func grokExistingHeaderRaw(raw, id string) (string, bool) {
	if body, ok := grokParentBody(raw, "model_providers."+id); ok {
		if sp, found := grokFindInline(body); found {
			if header := reGrokHeaderKV.FindString(body[sp.open+1 : sp.close]); header != "" {
				return header, false
			}
		}
	}
	if cs, ce, ok := grokChildSection(raw, id); ok {
		for _, line := range strings.Split(raw[cs:ce], "\n") {
			if reGrokHeaderKVLine.MatchString(line) {
				return line, true
			}
		}
	}
	return "", false
}

func grokRestoreHeaderRaw(raw, id, header string, child bool) string {
	if !child {
		return grokEditParent(raw, "model_providers."+id, func(body string) string {
			sp, ok := grokFindInline(body)
			if !ok {
				return body
			}
			inner := body[sp.open+1 : sp.close]
			loc := reGrokHeaderKV.FindStringIndex(inner)
			if loc == nil {
				return body
			}
			return body[:sp.open+1] + inner[:loc[0]] + header + inner[loc[1]:] + body[sp.close:]
		})
	}
	cstart, cend, ok := grokChildSection(raw, id)
	if !ok {
		return raw
	}
	lines := strings.Split(raw[cstart:cend], "\n")
	for i, line := range lines {
		if reGrokHeaderKVLine.MatchString(line) {
			lines[i] = header
			break
		}
	}
	return raw[:cstart] + strings.Join(lines, "\n") + raw[cend:]
}

func grokExistingPathRaw(raw, id string) (string, bool) {
	if body, ok := grokParentBody(raw, "model_providers."+id); ok {
		if sp, found := grokFindInline(body); found {
			if header := reGrokPathKV.FindString(body[sp.open+1 : sp.close]); header != "" {
				return header, false
			}
		}
	}
	if cs, ce, ok := grokChildSection(raw, id); ok {
		for _, line := range strings.Split(raw[cs:ce], "\n") {
			if reGrokPathKVLine.MatchString(line) {
				return line, true
			}
		}
	}
	return "", false
}

func grokSetOriginalPath(raw, id string) string {
	const path = "/chat/completions"
	if current, ok := grokExistingPath(raw, id); ok && current == path {
		return raw
	}
	entry := `x-headroom-original-path = ` + util.TomlQuoted(path)
	table := "model_providers." + id

	withInline := grokEditParent(raw, table, func(b string) string {
		sp, ok := grokFindInline(b)
		if !ok {
			return b
		}
		inner := b[sp.open+1 : sp.close]
		if reGrokPathKV.MatchString(inner) {
			inner = reGrokPathKV.ReplaceAllString(inner, entry)
		} else if strings.TrimSpace(inner) == "" {
			inner = " " + entry + " "
		} else {
			trimmed := strings.TrimLeft(inner, " \t")
			lead := inner[:len(inner)-len(trimmed)]
			inner = lead + entry + ", " + trimmed
		}
		return b[:sp.open+1] + inner + b[sp.close:]
	})
	if withInline != raw {
		return withInline
	}

	cstart, cend, hasChild := grokChildSection(raw, id)
	if !hasChild {
		return grokEditParent(raw, table, func(b string) string {
			line := "extra_headers = { " + entry + " }\n"
			trimmed := strings.TrimRight(b, "\n")
			return trimmed + "\n" + line + "\n"
		})
	}
	lines := strings.Split(raw[cstart:cend], "\n")
	for i, line := range lines {
		if reGrokPathKVLine.MatchString(line) {
			lines[i] = reGrokHeaderIndent.FindString(line) + entry
			return raw[:cstart] + strings.Join(lines, "\n") + raw[cend:]
		}
	}
	insert := 1
	indent := ""
	for insert < len(lines) && strings.TrimSpace(lines[insert]) != "" {
		indent = reGrokHeaderIndent.FindString(lines[insert])
		insert++
	}
	lines = append(lines[:insert], append([]string{indent + entry}, lines[insert:]...)...)
	return raw[:cstart] + strings.Join(lines, "\n") + raw[cend:]
}

func grokExistingPath(raw, id string) (string, bool) {
	if body, ok := grokParentBody(raw, "model_providers."+id); ok {
		if sp, found := grokFindInline(body); found {
			if m := reGrokPathKVValue.FindStringSubmatch(body[sp.open+1 : sp.close]); m != nil {
				if m[1] != "" {
					if value, err := strconv.Unquote(`"` + m[1] + `"`); err == nil {
						return value, true
					}
				}
				return m[2], true
			}
		}
	}
	if cs, ce, ok := grokChildSection(raw, id); ok {
		if m := reGrokPathKVValue.FindStringSubmatch(raw[cs:ce]); m != nil {
			if m[1] != "" {
				if value, err := strconv.Unquote(`"` + m[1] + `"`); err == nil {
					return value, true
				}
			}
			return m[2], true
		}
	}
	return "", false
}

func grokRestoreOriginalPath(raw, id, pathRaw string, child bool) string {
	if pathRaw != "" {
		if !child {
			return grokEditParent(raw, "model_providers."+id, func(body string) string {
				sp, ok := grokFindInline(body)
				if !ok {
					return body
				}
				inner := body[sp.open+1 : sp.close]
				loc := reGrokPathKV.FindStringIndex(inner)
				if loc == nil {
					return body
				}
				return body[:sp.open+1] + inner[:loc[0]] + pathRaw + inner[loc[1]:] + body[sp.close:]
			})
		}
		cstart, cend, ok := grokChildSection(raw, id)
		if !ok {
			return raw
		}
		lines := strings.Split(raw[cstart:cend], "\n")
		for i, line := range lines {
			if reGrokPathKVLine.MatchString(line) {
				lines[i] = pathRaw
				break
			}
		}
		return raw[:cstart] + strings.Join(lines, "\n") + raw[cend:]
	}
	return grokRemoveOriginalPath(raw, id)
}

func grokRemoveOriginalPath(raw, id string) string {
	table := "model_providers." + id
	withInline := grokEditParent(raw, table, func(b string) string {
		sp, ok := grokFindInline(b)
		if !ok {
			return b
		}
		inner := reGrokPathKV.ReplaceAllString(b[sp.open+1:sp.close], "")
		if i := strings.Index(inner, ","); i >= 0 && strings.TrimSpace(inner[:i]) == "" {
			rest := strings.TrimPrefix(inner[i+1:], " ")
			inner = inner[:i] + rest
		}
		if strings.TrimSpace(inner) == "" {
			start, end := sp.start, sp.end
			if end < len(b) && b[end] == '\n' {
				end++
			} else if start > 0 && b[start-1] == '\n' {
				start--
			}
			return b[:start] + b[end:]
		}
		return b[:sp.open+1] + inner + b[sp.close:]
	})
	if withInline != raw {
		return withInline
	}

	cstart, cend, ok := grokChildSection(raw, id)
	if !ok {
		return raw
	}
	lines := strings.Split(raw[cstart:cend], "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !reGrokPathKVLine.MatchString(line) {
			kept = append(kept, line)
		}
	}
	if len(kept) <= 2 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		return raw[:cstart] + raw[cend:]
	}
	return raw[:cstart] + strings.Join(kept, "\n") + raw[cend:]
}

// --- configure / remove / status ---

func ConfigureGrokProxy() (bool, string) {
	changed := false
	file := grokConfigFile()
	if err := withProxyRouteStashLock(func() error {
		changed, file = configureGrokProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("grok proxy lock failed: " + err.Error())
	}
	return changed, file
}

func configureGrokProxyLocked() (bool, string) {
	if !grokStashValid() {
		return false, grokConfigFile()
	}
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false, grokConfigFile()
	}
	stash := loadGrokStash()
	stashRaw, stashExists := util.ReadFileSafe(grokStashPath())
	wired := false
	ids := grokLocalBYOK(raw)
	seen := make(map[string]bool, len(ids)+len(stash))
	for _, id := range ids {
		seen[id] = true
	}
	for id := range stash {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		table := "model_providers." + id
		current := util.TomlBlockField(raw, table, "base_url")
		if sameProxyBase(current, ProxyEndpointFor("grok")) {
			if s, ok := stash[id]; ok && s.BaseURL != "" {
				if next := grokSetHeaderValue(raw, id, s.BaseURL); next != raw {
					raw = next
					wired = true
				}
				if !s.PathSet {
					s.PathRaw, s.PathChild = grokExistingPathRaw(raw, id)
					s.PathSet = true
					stash[id] = s
				}
				if next := grokSetOriginalPath(raw, id); next != raw {
					raw = next
					wired = true
				}
			}
			continue
		}
		userHeader, userHeaderSet := grokExistingHeader(raw, id)
		headerRaw, headerChild := grokExistingHeaderRaw(raw, id)
		pathRaw, pathChild := grokExistingPathRaw(raw, id)
		baseLine := grokBaseURLLine(raw, id)
		next := grokSwapBaseURL(raw, table, ProxyEndpointFor("grok"))
		next = grokSetHeaderValue(next, id, current)
		next = grokSetOriginalPath(next, id)
		if next != raw {
			raw = next
			wired = true
		}
		stash[id] = grokStashEntry{BaseURL: current, BaseLine: baseLine, Header: userHeader, HeaderRaw: headerRaw, HeaderChild: headerChild, HeaderSet: userHeaderSet, PathRaw: pathRaw, PathChild: pathChild, PathSet: true}
	}
	changed := wired
	if changed {
		if len(stash) > 0 {
			if err := saveGrokStash(stash); err != nil {
				return false, grokConfigFile()
			}
		} else if err := clearGrokStash(); err != nil {
			return false, grokConfigFile()
		}
	}
	if changed {
		if err := util.WriteFile(grokConfigFile(), raw); err != nil {
			_ = restoreProxyRouteStash("grok", stashRaw, stashExists)
			return false, grokConfigFile()
		}
	}
	return changed, grokConfigFile()
}

func RemoveGrokProxy() bool {
	removed := false
	if err := withProxyRouteStashLock(func() error {
		removed = removeGrokProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("grok proxy lock failed: " + err.Error())
	}
	return removed
}

func removeGrokProxyLocked() bool {
	if !grokStashValid() {
		return false
	}
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false
	}
	removed := false
	original := raw
	stashRaw, stashExists := util.ReadFileSafe(grokStashPath())
	stash := loadGrokStash()
	for id, s := range stash {
		table := "model_providers." + id
		current := util.TomlBlockField(raw, table, "base_url")
		if s.BaseURL == "" || !util.HasBlock(raw, table) || !sameProxyBase(current, ProxyEndpointFor("grok")) {
			continue
		}
		next := grokRestoreBaseURL(raw, id, s.BaseLine, s.BaseURL)
		if s.PathSet {
			next = grokRestoreOriginalPath(next, id, s.PathRaw, s.PathChild)
		}
		if s.HeaderRaw != "" {
			next = grokRestoreHeaderRaw(next, id, s.HeaderRaw, s.HeaderChild)
		} else if s.HeaderSet || s.Header != "" {
			next = grokSetHeaderValue(next, id, s.Header)
		} else {
			next = grokRemoveHeaderValue(next, id)
		}
		if next != raw {
			raw = next
			removed = true
		}
		delete(stash, id)
	}
	if !removed {
		return false
	}
	if err := util.WriteFile(grokConfigFile(), raw); err != nil {
		return false
	}
	if len(stash) > 0 {
		if err := saveGrokStash(stash); err != nil {
			_ = util.WriteFile(grokConfigFile(), original)
			_ = restoreProxyRouteStash("grok", stashRaw, stashExists)
			return false
		}
	} else {
		if err := clearGrokStash(); err != nil {
			_ = util.WriteFile(grokConfigFile(), original)
			_ = restoreProxyRouteStash("grok", stashRaw, stashExists)
			return false
		}
	}
	return true
}

func GrokProxyWired() bool {
	stash := loadGrokStash()
	if len(stash) == 0 {
		return false
	}
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false
	}
	for id := range stash {
		s := stash[id]
		upstream, upstreamOK := grokExistingHeader(raw, id)
		path, pathOK := grokExistingPath(raw, id)
		if sameProxyBase(util.TomlBlockField(raw, "model_providers."+id, "base_url"), ProxyEndpointFor("grok")) &&
			s.BaseURL != "" && upstreamOK && upstream == s.BaseURL && pathOK && path == "/chat/completions" {
			return true
		}
	}
	return false
}

func detectGrokProxy(cap ProxyCapability) ProxyDetection {
	raw, err := readProxyConfig(grokConfigFile())
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "config file absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "config unreadable", ProxyStateUnreadable)
	}
	stash := loadGrokStash()
	routed := 0
	for id := range stash {
		if util.HasBlock(raw, "model_providers."+id) &&
			sameProxyBase(util.TomlBlockField(raw, "model_providers."+id, "base_url"), ProxyEndpointFor("grok")) {
			routed++
		}
	}
	unwired := grokLocalBYOK(raw)
	if len(stash) > 0 && routed == 0 && len(unwired) == 0 {
		return proxyDetection(cap.ID, "stashed providers missing from config", ProxyStateConflict)
	}
	if routed > 0 {
		return proxyDetection(cap.ID, "BYOK "+strconv.Itoa(routed)+" provider(s) routed through headroom", ProxyStateManaged)
	}
	if len(unwired) > 0 {
		return proxyDetection(cap.ID, "BYOK providers found but not routed — rerun init", ProxyStateUnconfigured)
	}
	return proxyDetection(cap.ID, "no BYOK model providers configured", ProxyStateUnconfigured)
}
