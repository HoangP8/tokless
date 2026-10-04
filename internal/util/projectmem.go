package util

import (
	"context"
	"strings"
	"time"
)

// ProjectmemInstalled reports whether the projectmem MCP entrypoint exists.
func ProjectmemInstalled() bool {
	return Which("pjm-mcp") != ""
}

// ProjectmemInstalledVersion parses `uv tool list projectmem`.
func ProjectmemInstalledVersion() *string {
	uv := Which("uv")
	if uv == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res := Run(uv, []string{"tool", "list"}, RunOptions{Capture: true, Ctx: ctx})
	if res.Code != 0 {
		return nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "projectmem" {
			version := strings.TrimPrefix(fields[1], "v")
			return &version
		}
	}
	return nil
}
