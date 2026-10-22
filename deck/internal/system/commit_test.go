//go:build linux

package system

import "testing"

// The same text as the printf of printdeck-commit, and as os/board/autoboot.txt for slot a.
func TestAutoboot(t *testing.T) {
	want := "[all]\ntryboot_a_b=1\nboot_partition=2\n[tryboot]\nboot_partition=3\n"
	if got := autoboot(2, 3); got != want {
		t.Errorf("autoboot(2, 3) = %q", got)
	}
}
