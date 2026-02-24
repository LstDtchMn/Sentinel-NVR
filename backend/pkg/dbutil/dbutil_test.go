package dbutil

import (
	"testing"
	"time"
)

func TestParseSQLiteTime(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantOK bool
		wantY  int
		wantM  time.Month
		wantD  int
	}{
		{"CURRENT_TIMESTAMP format", "2025-01-15 08:30:00", true, 2025, time.January, 15},
		{"ISO-8601 with Z", "2025-01-15T08:30:00Z", true, 2025, time.January, 15},
		{"ISO-8601 without Z", "2025-01-15T08:30:00", true, 2025, time.January, 15},
		{"RFC3339", "2025-01-15T08:30:00+00:00", true, 2025, time.January, 15},
		{"RFC3339Nano", "2025-01-15T08:30:00.123456789Z", true, 2025, time.January, 15},
		{"invalid", "not-a-timestamp", false, 0, 0, 0},
		{"empty", "", false, 0, 0, 0},
		{"partial", "2025-01-15", false, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSQLiteTime(tt.input)
			if tt.wantOK && err != nil {
				t.Fatalf("ParseSQLiteTime(%q) = error %v, want success", tt.input, err)
			}
			if !tt.wantOK && err == nil {
				t.Fatalf("ParseSQLiteTime(%q) = %v, want error", tt.input, result)
			}
			if tt.wantOK {
				if result.Year() != tt.wantY || result.Month() != tt.wantM || result.Day() != tt.wantD {
					t.Errorf("ParseSQLiteTime(%q) = %v, want %d-%02d-%02d", tt.input, result, tt.wantY, tt.wantM, tt.wantD)
				}
			}
		})
	}
}

func TestParseSQLiteTimeUTC(t *testing.T) {
	// All formats should parse to UTC
	inputs := []string{
		"2025-06-15 12:00:00",
		"2025-06-15T12:00:00Z",
		"2025-06-15T12:00:00",
	}
	for _, input := range inputs {
		result, err := ParseSQLiteTime(input)
		if err != nil {
			t.Fatalf("ParseSQLiteTime(%q) error: %v", input, err)
		}
		if result.Location() != time.UTC {
			t.Errorf("ParseSQLiteTime(%q).Location() = %v, want UTC", input, result.Location())
		}
	}
}
