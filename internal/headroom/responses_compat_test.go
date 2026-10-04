package headroom

import (
	"io"
	"strings"
	"testing"
)

func TestFlattenResponsesTools(t *testing.T) {
	raw := []byte(`{"model":"m","tools":[
		{"type":"function","name":"exec_command","parameters":{}},
		{"type":"namespace","name":"mcp__srv","tools":[{"type":"function","name":"tool","parameters":{}}]},
		{"type":"web_search","external_web_access":false}]}`)
	out, nsMap, ok := flattenResponsesTools(raw)
	if !ok {
		t.Fatal("tools array not found")
	}
	got := string(out)
	for _, want := range []string{`"exec_command"`, `"mcp__srv__tool"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	for _, gone := range []string{"web_search", `"namespace"`} {
		if strings.Contains(got, gone) {
			t.Fatalf("leftover %s in %s", gone, got)
		}
	}
	if pair := nsMap["mcp__srv__tool"]; pair != [2]string{"mcp__srv", "tool"} {
		t.Fatalf("nsMap entry = %v", pair)
	}
	if _, _, ok := flattenResponsesTools([]byte(`{"model":"m"}`)); ok {
		t.Fatal("tools absent should not report rewrite")
	}
}

func fixupStream(t *testing.T, sse string) string {
	t.Helper()
	b := &sseFixupBody{
		inner: io.NopCloser(strings.NewReader(sse)),
		nsMap: map[string][2]string{"mcp__srv__tool": {"mcp__srv", "tool"}},
	}
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestSSEFixupSparseTextStream(t *testing.T) {
	out := fixupStream(t,
		"event: response.created\ndata: {\"type\":\"response.created\"}\n\n"+
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"delta\":\"OK\"}\n\n"+
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"output\":[]}}\n\n")
	created := strings.Index(out, "response.created")
	added := strings.Index(out, "response.output_item.added")
	part := strings.Index(out, "response.content_part.added")
	delta := strings.Index(out, `"delta":"OK"`)
	done := strings.Index(out, "response.output_item.done")
	completed := strings.Index(out, "event: response.completed")
	for name, i := range map[string]int{"created": created, "item.added": added, "part.added": part, "delta": delta, "item.done": done, "completed": completed} {
		if i < 0 {
			t.Fatalf("missing %s in output:\n%s", name, out)
		}
	}
	if !(created < added && added < part && part < delta && delta < done && done < completed) {
		t.Fatalf("wrong event order:\n%s", out)
	}
	if !strings.Contains(out, `"text":"OK"`) {
		t.Fatalf("completed output not filled:\n%s", out)
	}
}

func TestSSEFixupRestoresFunctionCallNamespace(t *testing.T) {
	out := fixupStream(t,
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"id\":\"fc1\",\"name\":\"mcp__srv__tool\",\"arguments\":\"{}\"}}\n\n"+
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"id\":\"fc1\",\"name\":\"mcp__srv__tool\",\"arguments\":\"{}\"}}\n\n"+
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"output\":[{\"type\":\"function_call\",\"id\":\"fc1\",\"name\":\"mcp__srv__tool\",\"arguments\":\"{}\"}]}}\n\n")
	if strings.Count(out, `"namespace":"mcp__srv"`) != 3 {
		t.Fatalf("namespace not restored on every function_call:\n%s", out)
	}
	if strings.Contains(out, "output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\"") {
		t.Fatalf("must not inject message item when stream already has items:\n%s", out)
	}
	if strings.Contains(out, `"text":""}`) {
		t.Fatalf("must not inject empty message:\n%s", out)
	}
}
