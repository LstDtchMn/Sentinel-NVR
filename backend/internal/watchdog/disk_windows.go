//go:build windows

package watchdog

import (
	"syscall"
	"unsafe"
)

// diskUsage returns the available and total bytes for the filesystem at path.
func diskUsage(path string) (available, total uint64, err error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")

	lpPath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}

	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	ret, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(lpPath)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)
	if ret == 0 {
		return 0, 0, callErr
	}
	return freeBytesAvailable, totalNumberOfBytes, nil
}
