package system

import (
	"strings"
	"testing"
)

func TestNewPassword(t *testing.T) {
	if n := len(strings.Fields(words)); n != 1295 {
		t.Fatalf("the word list has %d words, want 1295", n)
	}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		p, err := NewPassword()
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(p, "-")
		if len(parts) != passwordWords {
			t.Fatalf("%q has %d words, want %d", p, len(parts), passwordWords)
		}
		for _, w := range parts {
			if w == "" || strings.Trim(w, "abcdefghijklmnopqrstuvwxyz") != "" {
				t.Fatalf("%q has a word that is not lowercase letters: %q", p, w)
			}
		}
		seen[p] = true
	}
	if len(seen) < 100 {
		t.Errorf("only %d different passwords in 100", len(seen))
	}
}
