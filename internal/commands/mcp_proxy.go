package commands

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// runMcpProxy spawns the MCP server as a child and proxies stdio.
func runMcpProxy(agent, path string, argv, env []string) int {
	exe, args := resolveMcpCommand(path, argv)
	cmd := exec.Command(exe, args...)
	cmd.Env = mcpChildEnv(env)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cmd.Stdout = os.Stdout
		_ = cmd.Start()
		return waitExit(cmd)
	}
	if err := cmd.Start(); err != nil {
		return 1
	}
	io.Copy(os.Stdout, stdout)
	return waitExit(cmd)
}

func mcpChildEnv(env []string) []string {
	return env
}

// --- non-antigravity pass-through ---

func resolveMcpCommand(path string, argv []string) (string, []string) {
	if isNodeShebangScript(path) {
		if nodePath, err := exec.LookPath("node"); err == nil {
			return nodePath, append([]string{path}, argv[1:]...)
		}
	}
	return path, normalizedCmdBatchArgs(path, argv[1:], runtime.GOOS == "windows")
}

func normalizedCmdBatchArgs(command string, args []string, windows bool) []string {
	out := append([]string(nil), args...)
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(command, "\\", "/")))
	if !windows || (base != "cmd" && base != "cmd.exe") || len(out) < 2 || !strings.EqualFold(out[0], "/c") {
		return out
	}
	ext := strings.ToLower(filepath.Ext(strings.ReplaceAll(out[1], "\\", "/")))
	if ext == ".cmd" || ext == ".bat" {
		out[1] = strings.ReplaceAll(out[1], "/", "\\")
	}
	return out
}

func isNodeShebangScript(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 32)
	n, _ := f.Read(buf)
	return strings.HasPrefix(string(buf[:n]), "#!/usr/bin/env node")
}

func waitExit(cmd *exec.Cmd) int {
	err := cmd.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}
