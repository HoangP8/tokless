package commands

import (
	"fmt"
	"os"
	"os/exec"
)

func RunCodexSessionStart() int {
	cmd := exec.Command("context-mode", "hook", "codex", "sessionstart")
	cmd.Stdin = os.Stdin
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()

	fmt.Println(`{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"Use ctx_execute/ctx_search/ctx_batch_execute/ctx_execute_file for code analysis, ctx_fetch_and_index for web docs, ctx_index to store content. Prefer these over Bash/Read/Grep to keep context small. Run 'tokless doctor' to diagnose."}}`)
	return 0
}