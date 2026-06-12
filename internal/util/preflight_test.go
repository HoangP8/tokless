package util

import (
	"testing"
)

func TestIsWSL(t *testing.T) {
	origIsWin := IsWin
	defer func() { IsWin = origIsWin }()

	tests := []struct {
		name       string
		isWin      bool
		wslDistro  string
		wslInterop string
		want       bool
	}{
		{"Linux WSL with WSL_DISTRO_NAME", false, "Ubuntu", "", true},
		{"Linux WSL with WSL_INTEROP", false, "", "some_val", true},
		{"Linux non-WSL", false, "", "", false},
		{"Windows with WSL_DISTRO_NAME", true, "Ubuntu", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			IsWin = tt.isWin
			t.Setenv("WSL_DISTRO_NAME", tt.wslDistro)
			t.Setenv("WSL_INTEROP", tt.wslInterop)
			if got := isWSL(); got != tt.want {
				t.Errorf("isWSL() = %v, want %v", got, tt.want)
			}
		})
	}
}
