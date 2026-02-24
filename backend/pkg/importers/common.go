// Package importers provides parsers for migrating camera configurations from
// Blue Iris and Frigate NVR into Sentinel NVR (Phase 14, R15).
package importers

// ImportedCamera represents a camera parsed from an external NVR config file.
// All fields are plain strings/values — validation and encryption happen
// at the caller level (via camera.ValidateCameraInput and auth.EncryptCredential).
type ImportedCamera struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	MainStream string `json:"main_stream"`
	SubStream  string `json:"sub_stream"`
	Record     bool   `json:"record"`
	Detect     bool   `json:"detect"`
	ONVIFHost  string `json:"onvif_host,omitempty"`
	ONVIFPort  int    `json:"onvif_port,omitempty"`
	ONVIFUser  string `json:"onvif_user,omitempty"`
	ONVIFPass  string `json:"-"` // never sent in API preview responses; used only during import execution
}

// ImportResult is the summary returned after a parse or dry-run import.
type ImportResult struct {
	Format   string           `json:"format"`   // "blue_iris" or "frigate"
	Cameras  []ImportedCamera `json:"cameras"`  // successfully parsed cameras
	Warnings []string         `json:"warnings"` // non-fatal issues (e.g. missing sub-stream)
	Errors   []string         `json:"errors"`   // fatal parse errors per camera
}
