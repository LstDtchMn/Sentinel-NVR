//go:build windows

package diskutil

import "golang.org/x/sys/windows"

// GetDiskUsage returns the total and available bytes on the filesystem containing path.
func GetDiskUsage(path string) (DiskUsage, error) {
	var freeBytesAvail, totalBytes, totalFreeBytes uint64
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return DiskUsage{}, err
	}
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeBytesAvail, &totalBytes, &totalFreeBytes); err != nil {
		return DiskUsage{}, err
	}
	return DiskUsage{
		TotalBytes:     totalBytes,
		AvailableBytes: freeBytesAvail,
	}, nil
}
