package system

import (
	"path/filepath"
	"syscall"
)

// mountReadOnly mounts the EROFS partition dev at dir, read-only. Tests change it.
var mountReadOnly = func(dev, dir string) error {
	real, err := filepath.EvalSymlinks(dev)
	if err != nil {
		return err
	}
	return syscall.Mount(real, dir, "erofs", syscall.MS_RDONLY|syscall.MS_NOSUID|syscall.MS_NODEV, "")
}

// unmount unmounts dir. It fails while a file of the mount is in use. Tests change it.
var unmount = func(dir string) error { return syscall.Unmount(dir, 0) }

// lowerThreadPriority gives the thread of the caller the lowest CPU priority (nice 19) and the
// idle I/O class. The caller locks its goroutine to the thread (runtime.LockOSThread) and does
// not unlock it, so the thread ends with the goroutine.
func lowerThreadPriority() {
	tid := syscall.Gettid()
	syscall.Setpriority(syscall.PRIO_PROCESS, tid, 19)
	const ioprioWhoProcess, ioprioClassIdle, ioprioClassShift = 1, 3, 13
	syscall.Syscall(syscall.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(tid), ioprioClassIdle<<ioprioClassShift)
}
