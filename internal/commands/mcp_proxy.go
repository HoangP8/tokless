package commands

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// runMcpProxy spawns the MCP server as a child and proxies stdio.
func runMcpProxy(agent, path string, argv, env []string) int {
	exe, args := resolveMcpCommand(path, argv)
	cmd := exec.Command(exe, args...)
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

// resolveMcpCommand inspects the binary's shebang.
func resolveMcpCommand(path string, argv []string) (string, []string) {
	if isNodeShebangScript(path) {
		if nodePath := util.ResolveNodeBinary(); nodePath != "" {
			return nodePath, append([]string{path}, argv[1:]...)
		}
	}
	return path, argv[1:]
}

// isNodeShebangScript checks if the file starts with #!/usr/bin/env node.
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

func proxyMcpStdout(src io.Reader, dst io.Writer) {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		_, _ = dst.Write(line)
		_, _ = dst.Write([]byte("\n"))
	}
}
