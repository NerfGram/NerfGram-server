//go:build windows

package files

import "golang.org/x/sys/windows"

func localDiskFreeBytes(path string) (free, total int64, err error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeAvailable, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &freeAvailable, &totalBytes, &totalFree); err != nil {
		return 0, 0, err
	}
	return int64(freeAvailable), int64(totalBytes), nil
}
