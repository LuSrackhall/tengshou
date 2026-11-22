//go:build windows

package service

import "golang.org/x/sys/windows"

// diskSpace reports the free and total bytes of the volume holding path.
func diskSpace(path string) (free, total int64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeToCaller, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &totalBytes, &totalFree); err != nil {
		return 0, 0, err
	}
	return int64(freeToCaller), int64(totalBytes), nil
}
