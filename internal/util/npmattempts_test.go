package util

import (
	"strings"
	"testing"
)

// Locks the install-attempt contract: the user's configured registry/mirror is
// honored first; the public registry is forced only as a LAST resort.
func TestBuildNpmAttemptsRegistryOrder(t *testing.T) {
	attempts := buildNpmAttempts("pkg", "1.2.3", "https://mirror/pkg/-/pkg-1.2.3.tgz", "")
	if len(attempts) < 2 {
		t.Fatalf("expected several attempts, got %d", len(attempts))
	}

	joined := func(a []string) string { return strings.Join(a, " ") }

	// First attempt must NOT force a registry (uses the user's npm config).
	if strings.Contains(joined(attempts[0]), "--registry") {
		t.Errorf("first attempt forces a registry, should honor user config: %v", attempts[0])
	}

	// Exactly the LAST attempt may force the public registry, and it must.
	last := attempts[len(attempts)-1]
	if !strings.Contains(joined(last), "--registry "+defaultRegistryURL) {
		t.Errorf("last attempt should force public registry as last resort, got: %v", last)
	}
	for i := 0; i < len(attempts)-1; i++ {
		if strings.Contains(joined(attempts[i]), defaultRegistryURL) {
			t.Errorf("attempt %d (not last) hardcodes public registry: %v", i, attempts[i])
		}
	}
}
