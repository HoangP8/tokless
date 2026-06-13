package util

import "testing"

// Reproduces the HPC-network bug: a forced refetch (tokless update) must not
// regress a known-good cached "latest" to nil when the network fetch fails.
func TestCachedLatestStaleFallbackOnFetchFailure(t *testing.T) {
	SetHomeOverride(t.TempDir())
	defer SetHomeOverride("")
	t.Setenv("TOKLESS_TEST", "")

	orig := latestFetcher
	defer func() { latestFetcher = orig }()

	// 1) Seed cache with a known-good codegraph latest.
	latestFetcher = func(id string) *string {
		if id == "codegraph" {
			return strp("1.0.0")
		}
		return nil
	}
	if got := cachedLatest(true)["codegraph"]; got == nil || *got != "1.0.0" {
		t.Fatalf("seed: codegraph latest = %v, want 1.0.0", got)
	}

	// 2) npm registry unreachable: every fetch fails. Forced refetch must keep
	//    the stale cached value rather than returning nil.
	latestFetcher = func(id string) *string { return nil }
	got := cachedLatest(true)["codegraph"]
	if got == nil {
		t.Fatal("forced refetch with failing network returned nil; want stale 1.0.0 fallback")
	}
	if *got != "1.0.0" {
		t.Fatalf("stale fallback = %v, want 1.0.0", *got)
	}
}

// A fresh (non-forced) read must serve cached values without hitting the network.
func TestCachedLatestServesFreshFromCacheWithoutFetch(t *testing.T) {
	SetHomeOverride(t.TempDir())
	defer SetHomeOverride("")
	t.Setenv("TOKLESS_TEST", "")

	orig := latestFetcher
	defer func() { latestFetcher = orig }()

	latestFetcher = func(id string) *string {
		if id == "context-mode" {
			return strp("1.0.162")
		}
		return nil
	}
	if got := cachedLatest(false)["context-mode"]; got == nil || *got != "1.0.162" {
		t.Fatalf("seed: context-mode = %v, want 1.0.162", got)
	}

	// Cache is fresh now; a non-forced read must not re-fetch an id that is
	// already cached (it may still fetch other ids whose latest is unknown).
	latestFetcher = func(id string) *string {
		if id == "context-mode" {
			t.Fatal("re-fetched an already-cached id on a fresh non-forced read")
		}
		return nil
	}
	if got := cachedLatest(false)["context-mode"]; got == nil || *got != "1.0.162" {
		t.Fatalf("fresh read = %v, want 1.0.162 from cache", got)
	}
}
