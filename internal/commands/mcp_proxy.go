package commands

import (
	"bufio"
	"io"
	"os"
	"os/exec"
)

// runMcpProxy spawns the MCP server as a child and proxies stdio.
func runMcpProxy(agent, path string, argv, env []string) int {
	cmd := exec.Command(path, argv[1:]...)
	cmd.Env = env
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
	proxyMcpStdout(stdout, os.Stdout)
	return waitExit(cmd)
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

func proxyMcpStdout(src io.Reader, dst io.Writer) {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		_, _ = dst.Write(line)
		_, _ = dst.Write([]byte("\n"))
	}
}
