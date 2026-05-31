package util

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// OrderedMap preserves JSON object key order so we never reshuffle user config.
type OrderedMap struct {
	keys   []string
	values map[string]any
}

func NewOrderedMap() *OrderedMap {
	return &OrderedMap{values: map[string]any{}}
}

func (m *OrderedMap) Get(k string) (any, bool) { v, ok := m.values[k]; return v, ok }

func (m *OrderedMap) Set(k string, v any) {
	if _, ok := m.values[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.values[k] = v
}

func (m *OrderedMap) Delete(k string) {
	if _, ok := m.values[k]; !ok {
		return
	}
	delete(m.values, k)
	for i, kk := range m.keys {
		if kk == k {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

func (m *OrderedMap) Keys() []string { return m.keys }
func (m *OrderedMap) Len() int       { return len(m.keys) }

// UnmarshalJSON decodes preserving key order via a streaming token reader.
func (m *OrderedMap) UnmarshalJSON(data []byte) error {
	m.values = map[string]any{}
	m.keys = nil
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return json.Unmarshal(data, &m.values)
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		val, err := decodeOrdered(raw)
		if err != nil {
			return err
		}
		m.Set(key, val)
	}
	_, err = dec.Token() // closing }
	return err
}

func decodeOrdered(raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		om := NewOrderedMap()
		if err := om.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		return om, nil
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, err
		}
		out := make([]any, len(arr))
		for i, el := range arr {
			v, err := decodeOrdered(el)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// MarshalJSON writes object keys in preserved order.
func (m *OrderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(m.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

var reTrailingComma = regexp.MustCompile(`,(\s*[}\]])`)

// stripComments removes // and /* */ comments and trailing commas.
func stripComments(s string) string {
	var out strings.Builder
	i, n := 0, len(s)
	inStr, esc := false, false
	var strCh byte
	for i < n {
		ch := s[i]
		if inStr {
			out.WriteByte(ch)
			if esc {
				esc = false
			} else if ch == '\\' {
				esc = true
			} else if ch == strCh {
				inStr = false
			}
			i++
			continue
		}
		if ch == '"' || ch == '\'' {
			inStr = true
			strCh = ch
			out.WriteByte(ch)
			i++
			continue
		}
		if ch == '/' && i+1 < n && s[i+1] == '/' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < n && s[i+1] == '*' {
			i += 2
			for i+1 < n && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		out.WriteByte(ch)
		i++
	}
	return reTrailingComma.ReplaceAllString(out.String(), "$1")
}

// ParseJsonc parses JSON-with-comments into an ordered map.
func ParseJsonc(text string) (*OrderedMap, error) {
	stripped := stripComments(text)
	m := NewOrderedMap()
	if strings.TrimSpace(stripped) == "" {
		return m, nil
	}
	if err := json.Unmarshal([]byte(stripped), m); err != nil {
		return nil, err
	}
	return m, nil
}

// TryParseJsonc returns nil on any parse error.
func TryParseJsonc(text string) *OrderedMap {
	m, err := ParseJsonc(text)
	if err != nil {
		return nil
	}
	return m
}

// StringifyJSON matches JSON.stringify(obj, null, 2) + "\n".
func StringifyJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return buf.String()
}
