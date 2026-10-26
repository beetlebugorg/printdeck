//go:build !linux

package printer

// runLow runs f. Only Linux sets the priority of a thread.
func runLow(f func()) { f() }
