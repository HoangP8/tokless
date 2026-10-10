package util

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

var rtkCommandPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var RtkCommandsRunner = exec.Command

// RtkCommandsFromHelp parses supported commands from rtk's Commands: block.
func RtkCommandsFromHelp(help string) []string {
	commands := map[string]bool{}
	inCommands := false
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Commands:" {
			inCommands = true
			continue
		}
		if !inCommands {
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			break
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "-") {
			continue
		}
		name := fields[0]
		if !rtkCommandPattern.MatchString(name) || name == "help" || name == "completions" || strings.HasPrefix(name, "__") {
			continue
		}
		commands[name] = true
	}
	out := make([]string, 0, len(commands))
	for name := range commands {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// RtkCommands resolves tokless's rtk binary and lists its supported commands.
func RtkCommands() ([]string, error) {
	bin := ResolveRtkBin()
	if bin == "" {
		bin, _ = exec.LookPath("rtk")
	}
	if bin == "" {
		return nil, fmt.Errorf("rtk binary not found")
	}
	for _, args := range [][]string{{"--help"}, {"help"}} {
		out, err := RtkCommandsRunner(bin, args...).CombinedOutput()
		if commands := RtkCommandsFromHelp(string(out)); len(commands) != 0 {
			return commands, nil
		}
		if err != nil {
			continue
		}
	}
	return nil, fmt.Errorf("rtk help did not list commands")
}
