package util

import (
	"os"
	"testing"
)

func TestLegacyWinConsole(t *testing.T) {
	originalIsWin := IsWin
	defer func() { IsWin = originalIsWin }()

	tests := []struct {
		name         string
		isWin        bool
		wtSession    string
		termProgram  string
		term         string
		conEmuANSI   string
		want         bool
	}{
		{
			name:  "IsWin=false",
			isWin: false,
			want:  false,
		},
		{
			name:        "IsWin=true, all blank",
			isWin:       true,
			wtSession:   "",
			termProgram: "",
			term:        "",
			conEmuANSI:  "",
			want:        true,
		},
		{
			name:        "IsWin=true, WT_SESSION set",
			isWin:       true,
			wtSession:   "abc",
			termProgram: "",
			term:        "",
			conEmuANSI:  "",
			want:        false,
		},
		{
			name:        "IsWin=true, TERM_PROGRAM set",
			isWin:       true,
			wtSession:   "",
			termProgram: "vscode",
			term:        "",
			conEmuANSI:  "",
			want:        false,
		},
		{
			name:        "IsWin=true, TERM set",
			isWin:       true,
			wtSession:   "",
			termProgram: "",
			term:        "xterm-256color",
			conEmuANSI:  "",
			want:        false,
		},
		{
			name:        "IsWin=true, ConEmuANSI set",
			isWin:       true,
			wtSession:   "",
			termProgram: "",
			term:        "",
			conEmuANSI:  "ON",
			want:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			IsWin = tc.isWin

			if tc.isWin {
				// Set or unset environment variables based on test case
				if tc.wtSession != "" {
					t.Setenv("WT_SESSION", tc.wtSession)
				} else {
					t.Setenv("WT_SESSION", "")
					os.Unsetenv("WT_SESSION")
				}

				if tc.termProgram != "" {
					t.Setenv("TERM_PROGRAM", tc.termProgram)
				} else {
					t.Setenv("TERM_PROGRAM", "")
					os.Unsetenv("TERM_PROGRAM")
				}

				if tc.term != "" {
					t.Setenv("TERM", tc.term)
				} else {
					t.Setenv("TERM", "")
					os.Unsetenv("TERM")
				}

				if tc.conEmuANSI != "" {
					t.Setenv("ConEmuANSI", tc.conEmuANSI)
				} else {
					t.Setenv("ConEmuANSI", "")
					os.Unsetenv("ConEmuANSI")
				}
			}

			got := legacyWinConsole()
			if got != tc.want {
				t.Errorf("legacyWinConsole() = %v, want %v", got, tc.want)
			}
		})
	}
}
