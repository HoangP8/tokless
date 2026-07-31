package agents

import (
	"os"
	"testing"
)

// Agents land in the registry through Register(), the same call main makes.
func TestMain(m *testing.M) {
	Register()
	os.Exit(m.Run())
}
