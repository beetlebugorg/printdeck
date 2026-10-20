// Package reftest compares the command stream of the Go Klipper host with the reference stream of
// the real Klipper host (testdata, from tools/klipper-reference).
//
// klippy itself does not give the same bytes on each run: its serial queue packs the commands
// into blocks when its background thread wakes, so commands of different objects can change
// places. The commands of one command queue always keep their order. So two streams are equal here
// when, for each object (oid=N) and for the commands with no oid, the commands are the same and in
// the same order. A command with trsync_oid=N goes in the group of that trigger sync object.
package reftest

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	oidField    = regexp.MustCompile(`\boid=(\d+)\b`)
	trsyncField = regexp.MustCompile(`\btrsync_oid=(\d+)\b`)
)

// Groups splits the text of a decoded stream (one command on each line) into the commands of each
// object. The commands with no oid are in the group "-".
func Groups(text string) map[string][]string {
	out := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == "" {
			continue
		}
		key := "-"
		if m := oidField.FindStringSubmatch(line); m != nil {
			key = m[1]
		}
		// Commands of a trigger sync (homing and probing) go through the queue of the trsync
		// object, also when they name a stepper (stepper_stop_on_trigger oid=9 trsync_oid=8).
		if m := trsyncField.FindStringSubmatch(line); m != nil {
			key = m[1]
		}
		out[key] = append(out[key], line)
	}
	return out
}

// Compare returns nil when the two streams are equal by object, or an error with the first
// difference.
func Compare(got, want string) error {
	g, w := Groups(got), Groups(want)
	keys := map[string]bool{}
	for k := range g {
		keys[k] = true
	}
	for k := range w {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		gl, wl := g[k], w[k]
		for i := 0; i < len(gl) || i < len(wl); i++ {
			switch {
			case i >= len(gl):
				return fmt.Errorf("oid %s: command %d is missing: want %q", k, i+1, wl[i])
			case i >= len(wl):
				return fmt.Errorf("oid %s: command %d is extra: %q", k, i+1, gl[i])
			case gl[i] != wl[i]:
				return fmt.Errorf("oid %s: command %d:\n got %q\nwant %q", k, i+1, gl[i], wl[i])
			}
		}
	}
	return nil
}
