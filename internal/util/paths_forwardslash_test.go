package util

import "testing"

func TestPersistedToklessCommand(t *testing.T) {
	origIsWin := IsWin
	defer func() { IsWin = origIsWin }()

	IsWin = true
	if got, want := PersistedToklessCommand(`C:\Users\user\tokless.exe`, "rtk-hook", "droid"), "C:/Users/user/tokless.exe rtk-hook droid"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
	if got := PersistedToklessCommand(`C:\Program Files\tokless\tokless.exe`, "rtk-hook", "droid"); got != "tokless rtk-hook droid" {
		t.Fatalf("spaced command = %q", got)
	}

	IsWin = false
	if got, want := PersistedToklessCommand(`/opt/tokless`, "rtk-hook", "droid"), "/opt/tokless rtk-hook droid"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}
