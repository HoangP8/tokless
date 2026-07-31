package util

// Most agents keep MCP servers as a map under one key in a JSONC file. Only
// the path and the key differ.

func mcpServers(path, rootKey string) (*OrderedMap, *OrderedMap, bool) {
	raw, ok := ReadFileSafe(path)
	if !ok {
		return nil, nil, false
	}
	cfg := TryParseJsonc(raw)
	if cfg == nil {
		return nil, nil, false
	}
	v, ok := cfg.Get(rootKey)
	if !ok {
		return nil, nil, false
	}
	servers, ok := v.(*OrderedMap)
	if !ok {
		return nil, nil, false
	}
	return cfg, servers, true
}

func McpEntryHas(path, rootKey, toolID string) bool {
	_, servers, ok := mcpServers(path, rootKey)
	if !ok {
		return false
	}
	_, has := servers.Get(toolID)
	return has
}

// RemoveMcpEntry reports whether it removed anything.
func RemoveMcpEntry(path, rootKey, toolID string) bool {
	cfg, servers, ok := mcpServers(path, rootKey)
	if !ok {
		return false
	}
	if _, has := servers.Get(toolID); !has {
		return false
	}
	servers.Delete(toolID)
	_ = WriteFile(path, StringifyJSON(cfg))
	return true
}
