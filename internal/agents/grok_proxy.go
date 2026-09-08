package agents

import (
	"encoding/json"
	"errors"
	"fmt"
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
	Providers       map[string]grokStashEntry `json:"providers"`
	OAuthHeaderRaw  string                    `json:"oauth_header_raw,omitempty"`
	OAuthHeaderSet  bool                      `json:"oauth_header_set,omitempty"`
	OAuthModelsMade bool                      `json:"oauth_models_made,omitempty"`
}

const grokOAuthUpstream = "https://cli-chat-proxy.grok.com"

func grokProxyEndpoint() string { return util.HeadroomProxyOpenAIURL() }

func grokStashPath() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "grok.proxy.stash.json")
}

func loadGrokStash() map[string]grokStashEntry {
	f, ok := loadGrokStashFile()
	if !ok {
		return map[string]grokStashEntry{}
	}
	return f.Providers
}

func loadGrokStashFile() (grokStashFile, bool) {
	raw, ok := util.ReadFileSafe(grokStashPath())
	if !ok {
		return grokStashFile{}, false
	}
	var f grokStashFile
	if json.Unmarshal([]byte(raw), &f) != nil || f.Providers == nil {
		return grokStashFile{}, false
	}
	return f, true
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
	f, _ := loadGrokStashFile()
	f.Providers = m
	return saveGrokStashFile(f)
}

func saveGrokStashFile(f grokStashFile) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	path := grokStashPath()
	return util.WriteFileAtomic(path, string(b), 0o600)
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
	endpoint := grokProxyEndpoint()
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

func grokExistingTableHeader(raw, table string) (string, bool) {
	body, ok := grokParentBody(raw, table)
	if ok {
		if sp, inlineOK := grokFindInline(body); inlineOK {
			m := reGrokHeaderKVValue.FindStringSubmatch(body[sp.open+1 : sp.close])
			if m == nil {
				return "", false
			}
			if m[1] != "" {
				if value, err := strconv.Unquote(`"` + m[1] + `"`); err == nil {
					return value, true
				}
			}
			return m[2], true
		}
	}
	if cs, ce, childOK := grokChildSectionFor(raw, table); childOK {
		m := reGrokHeaderKVValue.FindStringSubmatch(raw[cs:ce])
		if m != nil {
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

func grokExistingTableHeaderRaw(raw, table string) string {
	body, ok := grokParentBody(raw, table)
	if ok {
		if sp, inlineOK := grokFindInline(body); inlineOK {
			return reGrokHeaderKV.FindString(body[sp.open+1 : sp.close])
		}
	}
	if cs, ce, childOK := grokChildSectionFor(raw, table); childOK {
		for _, line := range strings.Split(raw[cs:ce], "\n") {
			if reGrokHeaderKVLine.MatchString(line) {
				return line
			}
		}
	}
	return ""
}

func grokSetTableHeaderValue(raw, table, origin string) string {
	if current, ok := grokExistingTableHeader(raw, table); ok && current == origin {
		return raw
	}
	entry := headroomBaseURLHeader + ` = ` + util.TomlQuoted(origin)
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
		return b[:sp.open+1] + inner + b[sp.close:]
	})
	if withInline != raw {
		return withInline
	}
	if cstart, cend, hasChild := grokChildSectionFor(raw, table); hasChild {
		lines := strings.Split(raw[cstart:cend], "\n")
		replaced := false
		for i, line := range lines {
			if reGrokHeaderKVLine.MatchString(line) {
				lines[i] = reGrokHeaderIndent.FindString(line) + entry
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
	return grokEditParent(raw, table, func(b string) string {
		line := "extra_headers = { " + entry + " }\n"
		trimmed := strings.TrimRight(b, "\n")
		return trimmed + "\n" + line + "\n"
	})
}

func grokRemoveTableHeaderValue(raw, table string) string {
	withInline := grokEditParent(raw, table, func(b string) string {
		sp, ok := grokFindInline(b)
		if !ok || !reGrokHeaderKV.MatchString(b[sp.open+1:sp.close]) {
			return b
		}
		inner := reGrokHeaderKV.ReplaceAllString(b[sp.open+1:sp.close], "")
		if i := strings.Index(inner, ","); i >= 0 && strings.TrimSpace(inner[:i]) == "" {
			inner = inner[:i] + strings.TrimPrefix(inner[i+1:], " ")
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
	cstart, cend, hasChild := grokChildSectionFor(raw, table)
	if !hasChild {
		return raw
	}
	lines := strings.Split(raw[cstart:cend], "\n")
	var kept []string
	for _, line := range lines {
		if !reGrokHeaderKVLine.MatchString(line) {
			kept = append(kept, line)
		}
	}
	if len(kept) <= 2 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		return raw[:cstart] + raw[cend:]
	}
	return raw[:cstart] + strings.Join(kept, "\n") + raw[cend:]
}

func grokSetOAuthHeader(raw string, stash *grokStashFile) (string, bool) {
	const table = "models"
	if stash.Providers == nil {
		stash.Providers = map[string]grokStashEntry{}
	}
	if !util.HasBlock(raw, table) {
		raw = strings.TrimRight(raw, "\n") + "\n\n[models]\n"
		stash.OAuthModelsMade = true
	}
	if current, ok := grokExistingTableHeader(raw, table); ok {
		if current == grokOAuthUpstream {
			return raw, false
		}
		if !stash.OAuthHeaderSet {
			stash.OAuthHeaderRaw = grokExistingTableHeaderRaw(raw, table)
		}
	}
	next := grokSetTableHeaderValue(raw, table, grokOAuthUpstream)
	if next != raw {
		stash.OAuthHeaderSet = true
	}
	return next, next != raw
}

func grokOAuthApplicable(raw string) bool {
	defaultModel := util.TomlBlockField(raw, "models", "default")
	if defaultModel == "" {
		return false
	}
	modelTable := "model." + defaultModel
	provider := util.TomlBlockField(raw, modelTable, "model_provider")
	return provider == "" || !util.HasBlock(raw, "model_providers."+provider)
}

func grokRemoveEmptyModelsTable(raw string) string {
	body, ok := util.BlockText(raw, "models")
	if !ok {
		return raw
	}
	lines := strings.Split(body, "\n")
	if len(lines) > 0 {
		lines = lines[1:]
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return raw
		}
	}
	next := util.RemoveBlock(raw, "models")
	if next != raw && strings.HasSuffix(next, "\n\n") {
		next = strings.TrimSuffix(next, "\n")
	}
	return next
}

func grokRemoveOAuthHeader(raw string, stash grokStashFile) (string, bool) {
	if !stash.OAuthHeaderSet && !stash.OAuthModelsMade {
		return raw, false
	}
	if current, ok := grokExistingTableHeader(raw, "models"); !ok || current != grokOAuthUpstream {
		return raw, false
	}
	if stash.OAuthHeaderSet && stash.OAuthHeaderRaw != "" {
		next := grokEditParent(raw, "models", func(body string) string {
			sp, ok := grokFindInline(body)
			if !ok {
				return body
			}
			inner := body[sp.open+1 : sp.close]
			loc := reGrokHeaderKV.FindStringIndex(inner)
			if loc == nil {
				return body
			}
			return body[:sp.open+1] + inner[:loc[0]] + stash.OAuthHeaderRaw + inner[loc[1]:] + body[sp.close:]
		})
		if next == raw {
			if cs, ce, childOK := grokChildSectionFor(raw, "models"); childOK {
				lines := strings.Split(raw[cs:ce], "\n")
				for i, line := range lines {
					if reGrokHeaderKVLine.MatchString(line) {
						lines[i] = stash.OAuthHeaderRaw
						break
					}
				}
				next = raw[:cs] + strings.Join(lines, "\n") + raw[ce:]
			}
		}
		if stash.OAuthModelsMade {
			next = grokRemoveEmptyModelsTable(next)
		}
		return next, next != raw
	}
	next := grokRemoveTableHeaderValue(raw, "models")
	if stash.OAuthModelsMade {
		next = grokRemoveEmptyModelsTable(next)
	}
	return next, next != raw
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
	return grokChildSectionFor(raw, "model_providers."+id)
}

func grokChildSectionFor(raw, parent string) (int, int, bool) {
	re := regexp.MustCompile(`(?m)^\[` + regexp.QuoteMeta(parent) + `\.extra_headers\][ \t]*(?:#.*)?$`)
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
	changed, file, err := ConfigureGrokProxyChecked()
	if err != nil {
		util.L.Err("grok proxy configuration failed: " + err.Error())
	}
	return changed, file
}

func ConfigureGrokProxyChecked() (bool, string, error) {
	changed := false
	file := grokConfigFile()
	var configureErr error
	if err := withProxyRouteStashLock(func() error {
		configRaw, configExists := util.ReadFileSafe(file)
		stashRaw, stashExists := util.ReadFileSafe(grokStashPath())
		shimRaw, shimExists := util.ReadFileSafe(grokBinFile())
		realRaw, realExists := util.ReadFileSafe(grokRealBinFile())
		shimPresent := shimExists || realExists
		shimMode := os.FileMode(0o755)
		if info, err := os.Lstat(grokBinFile()); err == nil {
			shimMode = info.Mode().Perm()
		}
		configMode := os.FileMode(0o600)
		if info, err := os.Lstat(file); err == nil {
			configMode = info.Mode().Perm()
		}
		realMode := os.FileMode(0o755)
		if info, err := os.Lstat(grokRealBinFile()); err == nil {
			realMode = info.Mode().Perm()
		}
		var err error
		changed, file, err = configureGrokProxyLocked()
		if err != nil {
			if rollbackErr := restoreGrokProxyState(file, configRaw, configExists, configMode, stashRaw, stashExists); rollbackErr != nil {
				return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
			}
			return err
		}
		if shimPresent {
			shimChanged, err := InstallGrokShim()
			if err != nil {
				if rollbackErr := restoreGrokProxyAll(file, configRaw, configExists, configMode, stashRaw, stashExists, shimRaw, shimExists, shimMode, realRaw, realExists, realMode); rollbackErr != nil {
					return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
				}
				return err
			}
			changed = changed || shimChanged
		}
		return nil
	}); err != nil {
		util.L.Err("grok proxy lock failed: " + err.Error())
		configureErr = err
	}
	return changed, file, configureErr
}

func restoreGrokProxyState(file, configRaw string, configExists bool, configMode os.FileMode, stashRaw string, stashExists bool) error {
	return errors.Join(
		restoreGrokProxyFile(file, configRaw, configExists, configMode),
		restoreGrokProxyFile(grokStashPath(), stashRaw, stashExists, 0o600),
	)
}

func restoreGrokProxyAll(file, configRaw string, configExists bool, configMode os.FileMode, stashRaw string, stashExists bool, shimRaw string, shimExists bool, shimMode os.FileMode, realRaw string, realExists bool, realMode os.FileMode) error {
	return errors.Join(
		restoreGrokProxyState(file, configRaw, configExists, configMode, stashRaw, stashExists),
		restoreGrokProxyFile(grokBinFile(), shimRaw, shimExists, shimMode),
		restoreGrokProxyFile(grokRealBinFile(), realRaw, realExists, realMode),
	)
}

func restoreGrokProxyFile(path, raw string, exists bool, mode os.FileMode) error {
	if !exists {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return util.WriteFileMode(path, raw, mode)
}

func configureGrokProxyLocked() (bool, string, error) {
	if !grokStashValid() {
		return false, grokConfigFile(), fmt.Errorf("grok proxy stash is invalid")
	}
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false, grokConfigFile(), nil
	}
	stripped := false
	if next := stripGrokBuildBlocks(raw); next != raw {
		raw = next
		stripped = true
	}
	stashFile, _ := loadGrokStashFile()
	if stashFile.Providers == nil {
		stashFile.Providers = map[string]grokStashEntry{}
	}
	stash := stashFile.Providers
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
		if sameProxyBase(current, grokProxyEndpoint()) {
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
		next := grokSwapBaseURL(raw, table, grokProxyEndpoint())
		next = grokSetHeaderValue(next, id, current)
		next = grokSetOriginalPath(next, id)
		if next != raw {
			raw = next
			wired = true
		}
		stash[id] = grokStashEntry{BaseURL: current, BaseLine: baseLine, Header: userHeader, HeaderRaw: headerRaw, HeaderChild: headerChild, HeaderSet: userHeaderSet, PathRaw: pathRaw, PathChild: pathChild, PathSet: true}
	}
	if grokOAuthApplicable(raw) {
		if next, oauthChanged := grokSetOAuthHeader(raw, &stashFile); oauthChanged {
			raw = next
			wired = true
		}
	}
	stashFile.Providers = stash
	changed := wired || stripped
	if changed {
		if err := saveGrokStashFile(stashFile); err != nil {
			return false, grokConfigFile(), fmt.Errorf("save grok proxy stash: %w", err)
		}
	}
	if changed {
		if err := util.WriteFile(grokConfigFile(), raw); err != nil {
			return false, grokConfigFile(), fmt.Errorf("write grok proxy config: %w", err)
		}
	}
	return changed, grokConfigFile(), nil
}

func RemoveGrokProxy() bool {
	removed, err := RemoveGrokProxyChecked()
	if err != nil {
		util.L.Err("grok proxy removal failed: " + err.Error())
	}
	return removed
}

func RemoveGrokProxyChecked() (bool, error) {
	removed := false
	if err := withProxyRouteStashLock(func() error {
		file := grokConfigFile()
		configRaw, configExists := util.ReadFileSafe(file)
		stashRaw, stashExists := util.ReadFileSafe(grokStashPath())
		shimRaw, shimExists := util.ReadFileSafe(grokBinFile())
		realRaw, realExists := util.ReadFileSafe(grokRealBinFile())
		configMode := os.FileMode(0o600)
		if info, err := os.Lstat(file); err == nil {
			configMode = info.Mode().Perm()
		}
		shimMode := os.FileMode(0o755)
		if info, err := os.Lstat(grokBinFile()); err == nil {
			shimMode = info.Mode().Perm()
		}
		realMode := os.FileMode(0o755)
		if info, err := os.Lstat(grokRealBinFile()); err == nil {
			realMode = info.Mode().Perm()
		}
		var err error
		removed, err = removeGrokProxyLocked()
		if err != nil {
			if rollbackErr := restoreGrokProxyAll(file, configRaw, configExists, configMode, stashRaw, stashExists, shimRaw, shimExists, shimMode, realRaw, realExists, realMode); rollbackErr != nil {
				return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
			}
			return err
		}
		shimRemoved, err := removeGrokShimChecked()
		if err != nil {
			if rollbackErr := restoreGrokProxyAll(file, configRaw, configExists, configMode, stashRaw, stashExists, shimRaw, shimExists, shimMode, realRaw, realExists, realMode); rollbackErr != nil {
				return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
			}
			return err
		}
		if shimRemoved {
			removed = true
		}
		return nil
	}); err != nil {
		return false, err
	}
	return removed, nil
}

func removeGrokProxyLocked() (bool, error) {
	if !grokStashValid() {
		return false, fmt.Errorf("grok proxy stash is invalid")
	}
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false, nil
	}
	stashFile, _ := loadGrokStashFile()
	if len(stashFile.Providers) == 0 && !stashFile.OAuthHeaderSet && !stashFile.OAuthModelsMade {
		return removeGrokBuildProxyLocked(), nil
	}
	removed := false
	stash := stashFile.Providers
	for id, s := range stash {
		table := "model_providers." + id
		current := util.TomlBlockField(raw, table, "base_url")
		if s.BaseURL == "" || !util.HasBlock(raw, table) || !sameProxyBase(current, grokProxyEndpoint()) {
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
	if next, oauthRemoved := grokRemoveOAuthHeader(raw, stashFile); oauthRemoved {
		raw = next
		removed = true
		stashFile.OAuthHeaderRaw = ""
		stashFile.OAuthHeaderSet = false
		stashFile.OAuthModelsMade = false
	}
	if !removed {
		return false, nil
	}
	if err := util.WriteFile(grokConfigFile(), raw); err != nil {
		return false, fmt.Errorf("write restored grok config: %w", err)
	}
	stashFile.Providers = stash
	if len(stash) > 0 || stashFile.OAuthHeaderSet || stashFile.OAuthModelsMade {
		if err := saveGrokStashFile(stashFile); err != nil {
			return false, fmt.Errorf("save remaining grok proxy stash: %w", err)
		}
	} else {
		if err := clearGrokStash(); err != nil {
			return false, fmt.Errorf("clear grok proxy stash: %w", err)
		}
	}
	return true, nil
}

func GrokProxyWired() bool {
	if GrokShimWired() {
		return true
	}
	stashFile, _ := loadGrokStashFile()
	stash := stashFile.Providers
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if len(stash) == 0 {
		if ok {
			if (stashFile.OAuthHeaderSet || stashFile.OAuthModelsMade) && func() bool {
				upstream, exists := grokExistingTableHeader(raw, "models")
				return exists && upstream == grokOAuthUpstream
			}() {
				return true
			}
		}
		return false
	}
	raw, ok = util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false
	}
	for id := range stash {
		s := stash[id]
		upstream, upstreamOK := grokExistingHeader(raw, id)
		path, pathOK := grokExistingPath(raw, id)
		if sameProxyBase(util.TomlBlockField(raw, "model_providers."+id, "base_url"), grokProxyEndpoint()) &&
			s.BaseURL != "" && upstreamOK && upstream == s.BaseURL && pathOK && path == "/chat/completions" {
			return true
		}
	}
	return false
}

func GrokOAuthProxyWired() bool {
	if GrokShimWired() {
		return true
	}
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false
	}
	stash, _ := loadGrokStashFile()
	if !stash.OAuthHeaderSet && !stash.OAuthModelsMade {
		return false
	}
	upstream, exists := grokExistingTableHeader(raw, "models")
	return exists && upstream == grokOAuthUpstream
}

func GrokProxyUsesHeadroom() bool {
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false
	}
	return len(grokLocalBYOK(raw)) > 0 || len(loadGrokStash()) > 0
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
			sameProxyBase(util.TomlBlockField(raw, "model_providers."+id, "base_url"), grokProxyEndpoint()) {
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
	if GrokShimWired() {
		return proxyDetection(cap.ID, "OAuth grok launcher installed", ProxyStateManaged)
	}
	if upstream, exists := grokExistingTableHeader(raw, "models"); exists && upstream == grokOAuthUpstream {
		return proxyDetection(cap.ID, "native OAuth Grok requests use xAI CLI endpoint", ProxyStateManaged)
	}
	if strings.Contains(raw, grokBuildMarkerStart) || util.HasBlock(raw, "model.grok-build") {
		return proxyDetection(cap.ID, "[model.grok-build] legacy marker present — rerun init to convert to launcher", ProxyStateUnconfigured)
	}
	return proxyDetection(cap.ID, "no BYOK model providers configured — run `tokless init --agents grok`", ProxyStateUnconfigured)
}
