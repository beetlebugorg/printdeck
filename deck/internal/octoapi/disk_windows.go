package octoapi

import "golang.org/x/sys/windows"

// diskSpace is the free and the total size of the disk of dir, in bytes.
func diskSpace(dir string) (free, total uint64) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, 0
	}
	if windows.GetDiskFreeSpaceEx(p, &free, &total, nil) != nil {
		return 0, 0
	}
	return free, total
}
