package awake

import (
	"runtime"

	"golang.org/x/sys/windows"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// hold calls SetThreadExecutionState on a thread that stays until the release. The state belongs
// to the thread, so the same thread must clear it.
func hold(string) (func(), error) {
	if err := setThreadExecutionState.Find(); err != nil {
		return nil, err
	}
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		setThreadExecutionState.Call(esContinuous | esSystemRequired)
		<-release
		setThreadExecutionState.Call(esContinuous)
		close(done)
	}()
	return func() {
		close(release)
		<-done
	}, nil
}
