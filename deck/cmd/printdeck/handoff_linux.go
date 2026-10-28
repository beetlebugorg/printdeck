package main

import (
	"log"
	"os"
	"syscall"
)

// runNextDeck runs the deck program of an installed update in place of this one, when the marker
// of a switch says so (see handoff.go). It returns when this deck program runs on.
func runNextDeck() {
	n, ok := nextToRun(handoffDir)
	if !ok {
		return
	}
	log.Printf("deck: running the deck program of %s (%s), start %d", n.Version, n.Exe, n.Starts)
	err := syscall.Exec(n.Exe, []string{n.Exe}, append(os.Environ(), envNextRoot+"="+n.Root))
	failNext(handoffDir, err.Error())
}
