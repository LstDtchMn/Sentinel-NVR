// Package dbutil provides shared database utility functions used across
// multiple repository packages in Sentinel NVR.
package dbutil

import (
	"fmt"
	"time"
)

// ParseSQLiteTime parses a timestamp string produced by SQLite in any of the
// known formats: CURRENT_TIMESTAMP ("2006-01-02 15:04:05"), ISO-8601 variants,
// RFC 3339, and RFC 3339 with nanoseconds. All formats are parsed as UTC, which
// matches SQLite's CURRENT_TIMESTAMP behaviour.
func ParseSQLiteTime(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse timestamp %q", s)
}
