package headroom

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Wrap the gateway transport at init time so byok_gateway.go stays untouched:
// only Responses API paths see the adapter, and the gateway already rejects
// paths that do not match the route protocol.
func init() {
	inner := byokTransportFactory
	byokTransportFactory = func() http.RoundTripper {
		return &responsesCompatTransport{inner: inner()}
	}
}

// responsesCompatTransport adapts OpenAI Responses API traffic for upstreams
// that only understand plain chat payloads: codex namespace tools become flat
// functions, unsupported tool types are dropped, and sparse SSE streams gain
// the item events codex requires.
type responsesCompatTransport struct {
	inner http.RoundTripper
}

func (t *responsesCompatTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || (req.URL.Path != "/v1/responses" && req.URL.Path != "/responses") {
		return t.inner.RoundTrip(req)
	}
	var nsMap map[string][2]string
	if req.Body != nil {
		raw, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		body := raw
		if rewritten, m, ok := flattenResponsesTools(raw); ok {
			body, nsMap = rewritten, m
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		req.Header.Del("Content-Length")
	}
	resp, err := t.inner.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if resp.StatusCode == http.StatusOK && strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		resp.Body = &sseFixupBody{inner: resp.Body, nsMap: nsMap}
		resp.ContentLength = -1
		resp.Header.Del("Content-Length")
	}
	return resp, nil
}

// flattenResponsesTools rewrites the tools array for chat-only upstreams:
// namespace tools become flat functions named "<namespace>__<name>" and
// unsupported tool types are dropped. Reports whether a tools array existed.
func flattenResponsesTools(raw []byte) ([]byte, map[string][2]string, bool) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, false
	}
	tools, ok := payload["tools"].([]any)
	if !ok {
		return nil, nil, false
	}
	nsMap := make(map[string][2]string)
	flat := make([]any, 0, len(tools))
	for _, entry := range tools {
		tool, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		switch tool["type"] {
		case "function":
			flat = append(flat, tool)
		case "namespace":
			name, _ := tool["name"].(string)
			sub, _ := tool["tools"].([]any)
			for _, s := range sub {
				st, ok := s.(map[string]any)
				if !ok || st["type"] != "function" {
					continue
				}
				orig, _ := st["name"].(string)
				if name == "" || orig == "" {
					continue
				}
				clone := make(map[string]any, len(st)+1)
				for k, v := range st {
					clone[k] = v
				}
				flatName := name + "__" + orig
				clone["name"] = flatName
				nsMap[flatName] = [2]string{name, orig}
				flat = append(flat, clone)
			}
		}
	}
	payload["tools"] = flat
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, false
	}
	return out, nsMap, true
}

// sseFixupBody repairs sparse Responses API SSE streams while they stream:
// item/content part events get injected around text deltas, namespace fields
// are restored on flattened function calls, and the stream ends with a
// completed output item.
type sseFixupBody struct {
	inner       io.ReadCloser
	nsMap       map[string][2]string
	out         bytes.Buffer
	acc         []byte
	event       []string
	readErr     error
	sawItem     bool
	sawItemDone bool
	injected    bool
	accText     strings.Builder
	itemID      string
}

func (b *sseFixupBody) Close() error {
	return b.inner.Close()
}

func (b *sseFixupBody) Read(p []byte) (int, error) {
	for b.out.Len() == 0 {
		if b.readErr != nil {
			if len(b.event) > 0 {
				b.transform(b.event)
				b.event = nil
			}
			if b.out.Len() == 0 {
				return 0, b.readErr
			}
			break
		}
		chunk := make([]byte, 32*1024)
		n, err := b.inner.Read(chunk)
		if n > 0 {
			b.feed(chunk[:n])
		}
		if err != nil {
			if len(b.acc) > 0 {
				b.feedLine(string(b.acc))
				b.acc = nil
			}
			b.readErr = err
		}
	}
	return b.out.Read(p)
}

func (b *sseFixupBody) feed(chunk []byte) {
	b.acc = append(b.acc, chunk...)
	for {
		i := bytes.IndexByte(b.acc, '\n')
		if i < 0 {
			return
		}
		line := string(bytes.TrimRight(b.acc[:i], "\r"))
		b.acc = append([]byte(nil), b.acc[i+1:]...)
		b.feedLine(line)
	}
}

func (b *sseFixupBody) feedLine(line string) {
	if strings.TrimSpace(line) == "" {
		if len(b.event) > 0 {
			b.transform(b.event)
			b.event = nil
		}
		return
	}
	b.event = append(b.event, line)
}

func (b *sseFixupBody) transform(ev []string) {
	dataIdx := -1
	payload := ""
	for i, ln := range ev {
		if strings.HasPrefix(ln, "data:") {
			dataIdx = i
			payload = strings.TrimSpace(strings.TrimPrefix(ln, "data:"))
			break
		}
	}
	var data map[string]any
	if dataIdx < 0 || json.Unmarshal([]byte(payload), &data) != nil {
		b.emit(ev)
		return
	}
	typ, _ := data["type"].(string)
	switch typ {
	case "response.output_text.delta":
		if !b.sawItem && !b.injected {
			b.injected = true
			b.itemID = stringField(data, "item_id")
			if b.itemID == "" {
				b.itemID = "msg_compat"
			}
			idx := intField(data, "output_index")
			b.emitItemAdded(idx, b.itemID)
			b.emitContentPartAdded(idx, b.itemID)
		}
		if d, ok := data["delta"].(string); ok {
			b.accText.WriteString(d)
		}
		b.emitData(ev, dataIdx, data)
	case "response.output_item.added", "response.output_item.done", "response.content_part.added":
		b.sawItem = true
		if typ == "response.output_item.done" {
			b.sawItemDone = true
		}
		item, _ := data["item"].(map[string]any)
		if item == nil {
			if iid, ok := data["item_id"].(string); ok && iid != "" && b.itemID == "" {
				b.itemID = iid
			}
			b.emit(ev)
			return
		}
		if iid, ok := item["id"].(string); ok && iid != "" && b.itemID == "" {
			b.itemID = iid
		}
		b.restoreFunctionCall(item)
		b.emitData(ev, dataIdx, data)
	case "response.completed":
		resp, _ := data["response"].(map[string]any)
		var output []any
		if resp != nil {
			output, _ = resp["output"].([]any)
			for _, it := range output {
				if m, ok := it.(map[string]any); ok {
					b.restoreFunctionCall(m)
				}
			}
		}
		if !b.sawItemDone && (b.accText.Len() > 0 || len(output) == 0) {
			b.emitItemDone(b.accText.String())
		}
		if resp != nil && len(output) == 0 {
			resp["output"] = []any{b.messageItem("completed", b.accText.String())}
		}
		b.emitData(ev, dataIdx, data)
	default:
		b.emit(ev)
	}
}

func (b *sseFixupBody) restoreFunctionCall(item map[string]any) {
	if item["type"] != "function_call" {
		return
	}
	name, _ := item["name"].(string)
	if pair, ok := b.nsMap[name]; ok {
		item["namespace"] = pair[0]
		item["name"] = pair[1]
	}
}

func (b *sseFixupBody) messageItem(status, text string) map[string]any {
	id := b.itemID
	if id == "" {
		id = "msg_compat"
	}
	return map[string]any{
		"type":   "message",
		"id":     id,
		"role":   "assistant",
		"status": status,
		"content": []any{map[string]any{
			"type": "output_text", "text": text, "annotations": []any{},
		}},
	}
}

func (b *sseFixupBody) emitItemAdded(idx int, id string) {
	b.emitTyped(map[string]any{
		"type": "response.output_item.added", "output_index": idx,
		"item": map[string]any{
			"type": "message", "id": id, "role": "assistant",
			"status": "in_progress", "content": []any{},
		},
	})
}

func (b *sseFixupBody) emitContentPartAdded(idx int, id string) {
	b.emitTyped(map[string]any{
		"type": "response.content_part.added", "item_id": id,
		"output_index": idx, "content_index": 0,
		"part": map[string]any{
			"type": "output_text", "text": "", "annotations": []any{},
		},
	})
}

func (b *sseFixupBody) emitItemDone(text string) {
	b.emitTyped(map[string]any{
		"type": "response.output_item.done", "output_index": 0,
		"item": b.messageItem("completed", text),
	})
}

func (b *sseFixupBody) emitData(ev []string, idx int, data map[string]any) {
	payload, err := json.Marshal(data)
	if err != nil {
		b.emit(ev)
		return
	}
	cp := append([]string(nil), ev...)
	cp[idx] = "data: " + string(payload)
	b.emit(cp)
}

func (b *sseFixupBody) emit(lines []string) {
	for _, ln := range lines {
		b.out.WriteString(ln)
		b.out.WriteByte('\n')
	}
	b.out.WriteByte('\n')
}

func (b *sseFixupBody) emitTyped(data map[string]any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	typ, _ := data["type"].(string)
	b.emit([]string{"event: " + typ, "data: " + string(payload)})
}

func stringField(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return s
}

func intField(data map[string]any, key string) int {
	if f, ok := data[key].(float64); ok {
		return int(f)
	}
	return 0
}
