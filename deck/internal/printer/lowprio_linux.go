package printer

import (
	"runtime"
	"syscall"
)

// runLow runs f on a thread of its own at the lowest priority (nice 19), and waits for it. The
// goroutine of f does not unlock its thread, so the Go runtime ends the thread with the
// goroutine, and no other goroutine runs at the low priority later.
func runLow(f func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		syscall.Setpriority(syscall.PRIO_PROCESS, syscall.Gettid(), 19)
		f()
	}()
	<-done
}
