package commands

import "testing"

func TestCodegraphMcpCommand(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{"direct", []string{"codegraph", "serve"}, "codegraph"},
		{"direct executable", []string{"C:/bin/codegraph.exe", "serve"}, "C:/bin/codegraph.exe"},
		{"cmd wrapper", []string{"cmd", "/c", `C:\bin\codegraph.cmd`, "serve"}, `C:\bin\codegraph.cmd`},
		{"cmd exe wrapper", []string{`C:\Windows\System32\cmd.exe`, "/c", `C:\bin\codegraph.cmd`, "serve"}, `C:\bin\codegraph.cmd`},
		{"cmd bat", []string{"CMD", "/C", `C:\bin\codegraph.bat`, "serve"}, `C:\bin\codegraph.bat`},
		{"other", []string{"context-mode", "serve"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codegraphMcpCommand(tt.argv); got != tt.want {
				t.Fatalf("codegraphMcpCommand(%q) = %q, want %q", tt.argv, got, tt.want)
			}
		})
	}
}
