package util

import "testing"

// Proves latest-version resolution goes through npm (honoring the user's
// registry/mirror/proxy), not a hardcoded npmjs.org GET. Skips offline.
func TestNpmViewLatestResolvesViaNpm(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "")
	if Which("npm") == "" {
		t.Skip("npm not on PATH")
	}
	v := npmViewLatest("@colbymchenry/codegraph")
	if v == nil {
		t.Skip("npm view returned nil (offline/registry unreachable in CI)")
	}
	if !reSemver.MatchString(*v) {
		t.Fatalf("npmViewLatest returned non-semver %q", *v)
	}
	t.Logf("npmViewLatest(@colbymchenry/codegraph) = %s", *v)
}
