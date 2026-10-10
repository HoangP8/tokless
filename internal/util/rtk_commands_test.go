package util

import (
	"reflect"
	"testing"
)

func TestRtkCommandsFromHelp(t *testing.T) {
	help := `Usage: rtk [OPTIONS] <COMMAND>

Commands:
  ls          list files
  git         git helper
  git         duplicate
  help        print help
  completions generate completion
  __complete internal
  __start     internal
  -h, --help  print help
  invalid_name bad
`
	want := []string{"git", "ls"}
	if got := RtkCommandsFromHelp(help); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := RtkCommandsFromHelp("Usage: rtk\nCommands:\n  --help help\n"); len(got) != 0 {
		t.Fatalf("wrapped/empty block returned %v", got)
	}
}
