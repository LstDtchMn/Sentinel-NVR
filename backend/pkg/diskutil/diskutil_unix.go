//go:build !windows

package diskutil

import "golang.org/x/sys/unix"

// GetDiskUsage returns the total and available bytes on the filesystem containing path.
func GetDiskUsage(path string) (DiskUsage, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return DiskUsage{}, err
	}
	return DiskUsage{
		TotalBytes:     stat.Blocks * uint64(stat.Bsize),
		AvailableBytes: stat.Bavail * uint64(stat.Bsize),
	}, nil
}
