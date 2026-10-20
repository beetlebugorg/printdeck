package main

import "testing"

func TestSourceURL(t *testing.T) {
	const repo = "https://github.com/beetlebugorg/printdeck"
	for version, want := range map[string]string{
		"v2.2.0":            repo + "/tree/v2.2.0",
		"v2.2.0-3-g1a2b3c4": repo + "/tree/1a2b3c4",
		"1a2b3c4":           repo + "/tree/1a2b3c4",
		"1a2b3c4-dirty":     repo,
		"":                  repo,
		"unknown":           repo,
	} {
		if got := sourceURL(version); got != want {
			t.Errorf("%q: %q, want %q", version, got, want)
		}
	}
}
