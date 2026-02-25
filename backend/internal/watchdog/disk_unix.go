//go:build !windows

package watchdog

import "syscall"

// diskUsage returns the available and total bytes for the filesystem at path.
func diskUsage(path string) (available, total uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	// Bsize is int64 on Linux and int32 on Darwin — both are safe to cast to uint64
	// for typical block sizes (512 B – 64 KiB), which are always positive.
	blockSize := uint64(stat.Bsize) //nolint:unconvert
	return stat.Bavail * blockSize, stat.Blocks * blockSize, nil
}
