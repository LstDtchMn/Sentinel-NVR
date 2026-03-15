// Package diskutil provides cross-platform filesystem capacity queries.
package diskutil

// DiskUsage holds filesystem capacity information for a path.
type DiskUsage struct {
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}
