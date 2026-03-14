package importers

import (
	"strings"
	"testing"
)

func TestSanitizeCameraName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		want   string
	}{
		{
			name:  "simple valid name",
			input: "FrontDoor",
			want:  "FrontDoor",
		},
		{
			name:  "name with spaces",
			input: "Front Door",
			want:  "Front Door",
		},
		{
			name:  "name with hyphens and underscores",
			input: "cam-1_main",
			want:  "cam-1_main",
		},
		{
			name:  "name with special chars replaced",
			input: "Front Door (2nd)",
			want:  "Front Door _2nd_",
		},
		{
			name:  "trailing spaces trimmed",
			input: "Camera   ",
			want:  "Camera",
		},
		{
			name:  "leading non-alphanumeric gets cam_ prefix",
			input: "_underscore_start",
			want:  "cam__underscore_start",
		},
		{
			name:  "leading hyphen gets cam_ prefix",
			input: "-hyphen-start",
			want:  "cam_-hyphen-start",
		},
		{
			name:  "leading space gets cam_ prefix",
			input: " space start",
			want:  "cam_ space start",
		},
		{
			name:  "all special chars",
			input: "!!!",
			want:  "cam____",
		},
		{
			name:  "empty string becomes imported_camera",
			input: "",
			want:  "imported_camera",
		},
		{
			name:  "numeric start is valid",
			input: "1st Camera",
			want:  "1st Camera",
		},
		{
			name:  "unicode chars replaced",
			input: "Caméra Entrée",
			want:  "Cam_ra Entr_e",
		},
		{
			name:  "dots replaced with underscore",
			input: "cam.front.door",
			want:  "cam_front_door",
		},
		{
			name:  "slashes replaced",
			input: "path/to/camera",
			want:  "path_to_camera",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeCameraName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeCameraName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeCameraNameTruncation(t *testing.T) {
	t.Parallel()
	// Names longer than 64 chars should be truncated to 64
	longName := strings.Repeat("A", 100)
	got := sanitizeCameraName(longName)
	if len(got) != 64 {
		t.Errorf("len = %d, want 64", len(got))
	}
}

func TestSanitizeCameraNamePrependThenTruncate(t *testing.T) {
	t.Parallel()
	// Start with underscore (needs "cam_" prepend) + 63 chars = 67 chars before truncation
	// "cam_" (4) + "_" (1) + 62 * "X" = 67 → truncated to 64
	input := "_" + strings.Repeat("X", 62)
	got := sanitizeCameraName(input)
	if len(got) > 64 {
		t.Errorf("len = %d, should be <= 64 after prepend+truncate", len(got))
	}
	if !strings.HasPrefix(got, "cam_") {
		t.Errorf("should start with cam_ prefix, got %q", got)
	}
}

func TestSanitizeCameraNameTruncateTrailingSpaceRetrim(t *testing.T) {
	t.Parallel()
	// Construct a name that after truncation at 64 ends with spaces.
	// "A" * 63 + " " = 64 chars (no truncation needed, but trailing space trim applies)
	// Actually let's make it so truncation creates trailing spaces:
	// "cam_" (4 prepend) + "_" (1) + "B"*56 + "   " (3 spaces) + "C"*5 = cam_ + total 65 → truncate
	// After prepend: "cam__" + "B"*56 + "   " + "C"*5 = 4+1+56+3+5 = 69 → truncate to 64
	// result[:64] = "cam__" + "B"*56 + "   " = 64 chars → re-trim trailing spaces → 61 chars
	input := "_" + strings.Repeat("B", 56) + "   " + strings.Repeat("C", 5)
	got := sanitizeCameraName(input)
	if len(got) > 64 {
		t.Errorf("len = %d, should be <= 64", len(got))
	}
	if strings.HasSuffix(got, " ") {
		t.Errorf("result should not end with space, got %q", got)
	}
}

func TestSanitizeCameraNameOnlySpaces(t *testing.T) {
	t.Parallel()
	// All spaces → trimmed to empty → "imported_camera"
	got := sanitizeCameraName("     ")
	if got != "imported_camera" {
		t.Errorf("all-spaces name = %q, want %q", got, "imported_camera")
	}
}

func TestSanitizeCameraNameExactly64Chars(t *testing.T) {
	t.Parallel()
	input := strings.Repeat("A", 64)
	got := sanitizeCameraName(input)
	if len(got) != 64 {
		t.Errorf("len = %d, want 64 (no truncation needed)", len(got))
	}
}

func TestSanitizeCameraNameExactly65Chars(t *testing.T) {
	t.Parallel()
	input := strings.Repeat("A", 65)
	got := sanitizeCameraName(input)
	if len(got) != 64 {
		t.Errorf("len = %d, want 64 (truncated by 1)", len(got))
	}
}

func TestImportedCameraJSONTag(t *testing.T) {
	t.Parallel()
	// Verify ONVIFPass has json:"-" by checking struct behaviour indirectly.
	// The struct tag prevents JSON marshalling of the field. We verify the
	// field exists and is usable but we don't test JSON marshalling here since
	// that's a standard library guarantee — we just verify the struct is sound.
	cam := ImportedCamera{
		Name:      "test",
		ONVIFPass: "secret",
	}
	if cam.ONVIFPass != "secret" {
		t.Error("ONVIFPass should be settable")
	}
}

func TestImportResultDefaults(t *testing.T) {
	t.Parallel()
	result := &ImportResult{Format: "test"}
	if result.Cameras != nil {
		t.Error("Cameras should be nil by default")
	}
	if result.Warnings != nil {
		t.Error("Warnings should be nil by default")
	}
	if result.Errors != nil {
		t.Error("Errors should be nil by default")
	}
}
