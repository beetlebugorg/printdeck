// Package awake keeps the computer of the desktop app awake while a print runs. The Klipper host
// sends the moves to the printer just in time, so a computer that sleeps stops the print.
package awake

import (
	"log"
	"sync"
)

// Lock keeps the computer awake while it is set.
type Lock struct {
	mu   sync.Mutex
	held bool
	stop func() // releases the hold of the OS
}

// Set holds the lock when on is true, and releases it when on is false. A Set with the same
// value does nothing.
func (l *Lock) Set(on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if on == l.held {
		return
	}
	if on {
		stop, err := hold("Printdeck: a print runs")
		if err != nil {
			log.Printf("awake: the computer can sleep during the print: %v", err)
			return
		}
		l.stop, l.held = stop, true
		log.Print("awake: the computer stays awake until the print ends")
		return
	}
	l.stop()
	l.stop, l.held = nil, false
	log.Print("awake: the computer can sleep again")
}
