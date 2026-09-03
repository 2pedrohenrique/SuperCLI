package main

import "testing"

func TestEffectiveVersionPrefersReleaseBuildValue(t *testing.T) {
	previous := version
	version = "0.3.1-alpha"
	t.Cleanup(func() { version = previous })
	if got := effectiveVersion(); got != version {
		t.Fatalf("effectiveVersion() = %q, want %q", got, version)
	}
}
