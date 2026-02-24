// Package pathutil provides shared filesystem path utility functions.
package pathutil

import (
	"path/filepath"
	"strings"
)

// IsUnderPath reports whether cleanPath is strictly inside basePath
// (i.e. cleanPath is a descendant of basePath, not basePath itself).
// Both paths should be cleaned/resolved (e.g. via filepath.EvalSymlinks)
// before calling to avoid symlink-bypass issues.
func IsUnderPath(cleanPath, basePath string) bool {
	base := filepath.Clean(basePath)
	rel, err := filepath.Rel(base, cleanPath)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}
