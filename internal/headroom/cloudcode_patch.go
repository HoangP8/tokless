package headroom

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/util"
)

const cloudCodePatchMarker = "gemini:tool_schema_compaction"

func cloudCodePatchRoots() []string {
	tools := util.HeadroomPathsResolved().Tools
	return []string{
		filepath.Join(tools, "headroom-ai", "lib", "python*", "site-packages", "headroom", "proxy", "handlers", "gemini.py"),
		filepath.Join(tools, "headroom-ai", "Lib", "site-packages", "headroom", "proxy", "handlers", "gemini.py"),
	}
}

func patchCloudCodeGemini(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if bytes.Contains(data, []byte(cloudCodePatchMarker)) {
		return false, nil
	}

	type replacement struct{ old, new []byte }
	replacements := []replacement{
		{[]byte("        contents = request_payload.get(\"contents\", [])\n        headers = dict(request.headers.items())\n"), []byte("        contents = request_payload.get(\"contents\", [])\n        tools_before = request_payload.get(\"tools\")\n        tools_after = tools_before\n        tools_modified = False\n        headers = dict(request.headers.items())\n")},
		{[]byte("        if not messages:\n            original_tokens = 0\n        optimized_messages = messages\n"), []byte("        if not messages:\n            original_tokens = 0\n        _tool_tokens_before = 0\n        _tool_tokens_after = 0\n        optimized_messages = messages\n")},
		{[]byte("            if not is_antigravity:\n                if optimized_system:\n                    request_payload[\"systemInstruction\"] = optimized_system\n                elif \"systemInstruction\" in request_payload:\n                    del request_payload[\"systemInstruction\"]\n\n        tokens_saved = original_tokens - optimized_tokens\n        optimization_latency = (time.time() - start_time) * 1000\n        base_url = self._resolve_cloudcode_base_url(is_antigravity)\n"), []byte("            if optimized_system:\n                request_payload[\"systemInstruction\"] = optimized_system\n\n        if not _decision.bypass_header_set and isinstance(tools_before, list) and tools_before:\n            try:\n                from headroom.proxy.tool_schema_compaction import compact_tools\n\n                compacted_payload, tools_modified, _, _ = compact_tools(\n                    {\"tools\": tools_before}\n                )\n                tools_after = compacted_payload.get(\"tools\", tools_before)\n                if tools_modified:\n                    request_payload[\"tools\"] = tools_after\n            except Exception:\n                tools_modified = False\n                tools_after = tools_before\n                logger.debug(\"Cloud Code tool schema compaction skipped\", exc_info=True)\n            if tools_modified:\n                try:\n                    _tool_tokens_before = tokenizer.count_text(\n                        json.dumps(tools_before, default=str)\n                    )\n                    _tool_tokens_after = tokenizer.count_text(\n                        json.dumps(tools_after, default=str)\n                    )\n                except Exception:\n                    logger.debug(\"Cloud Code tool token recount skipped\", exc_info=True)\n\n        if 0 < _tool_tokens_after < _tool_tokens_before:\n            original_tokens += _tool_tokens_before\n            optimized_tokens += _tool_tokens_after\n            transforms_applied.append(\"gemini:tool_schema_compaction\")\n        tokens_saved = max(0, original_tokens - optimized_tokens)\n        optimization_latency = (time.time() - start_time) * 1000\n        base_url = self._resolve_cloudcode_base_url(is_antigravity)\n")},
	}

	patched := data
	for _, r := range replacements {
		if bytes.Count(patched, r.old) != 1 {
			return false, nil
		}
		patched = bytes.Replace(patched, r.old, r.new, 1)
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if err := util.WriteFileAtomic(path, string(patched), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func applyCloudCodePatch() bool {
	changed := false
	for _, pattern := range cloudCodePatchRoots() {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range paths {
			patched, err := patchCloudCodeGemini(path)
			if err != nil {
				util.L.Sub("headroom Cloud Code patch skipped: " + err.Error())
				continue
			}
			changed = changed || patched
		}
	}
	return changed
}
