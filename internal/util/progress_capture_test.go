package util

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCaptureLogs(t *testing.T) {
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	wantErr := errors.New("some error")
	logs, err := CaptureLogs(func() error {
		fmt.Println("hello")
		fmt.Print("err line")
		return wantErr
	})

	if os.Stdout != oldStdout {
		t.Errorf("CaptureLogs did not restore os.Stdout")
	}

	if err != wantErr {
		t.Errorf("CaptureLogs() error = %v, wantErr %v", err, wantErr)
	}

	if !strings.Contains(logs, "hello\n") || !strings.Contains(logs, "err line") {
		t.Errorf("CaptureLogs() logs = %q, want 'hello\\n' and 'err line'", logs)
	}
}
