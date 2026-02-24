package importers

import (
	"fmt"
	"net/url"

	"gopkg.in/yaml.v3"
)

// Frigate NVR stores its configuration in a YAML file. The camera structure is:
//
//	cameras:
//	  front_door:
//	    enabled: true
//	    ffmpeg:
//	      inputs:
//	        - path: rtsp://user:pass@192.168.1.100:554/stream1
//	          roles:
//	            - record
//	        - path: rtsp://user:pass@192.168.1.100:554/stream2
//	          roles:
//	            - detect
//	    detect:
//	      enabled: true
//	    record:
//	      enabled: true
//	      retain:
//	        default: 3

// frigateConfig is the top-level Frigate config structure (we only parse the
// fields relevant to camera migration).
type frigateConfig struct {
	Cameras map[string]frigateCamera `yaml:"cameras"`
}

type frigateCamera struct {
	Enabled *bool         `yaml:"enabled"` // pointer: nil means not set (default true)
	FFmpeg  frigateFFmpeg `yaml:"ffmpeg"`
	Detect  frigateDetect `yaml:"detect"`
	Record  frigateRecord `yaml:"record"`
}

type frigateFFmpeg struct {
	Inputs []frigateInput `yaml:"inputs"`
}

type frigateInput struct {
	Path  string   `yaml:"path"`
	Roles []string `yaml:"roles"`
}

type frigateDetect struct {
	Enabled *bool `yaml:"enabled"`
}

type frigateRecord struct {
	Enabled *bool `yaml:"enabled"`
}

// ParseFrigate parses a Frigate config.yml and returns imported cameras.
func ParseFrigate(data []byte) *ImportResult {
	result := &ImportResult{Format: "frigate"}

	var cfg frigateConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("YAML parse error: %v", err))
		return result
	}

	if len(cfg.Cameras) == 0 {
		result.Errors = append(result.Errors, "no cameras found in Frigate config")
		return result
	}

	for name, fcam := range cfg.Cameras {
		cam := ImportedCamera{
			Name:    sanitizeCameraName(name),
			Enabled: fcam.Enabled == nil || *fcam.Enabled, // default true
			Record:  fcam.Record.Enabled != nil && *fcam.Record.Enabled,
			Detect:  fcam.Detect.Enabled != nil && *fcam.Detect.Enabled,
		}

		// Find the main stream (record role) and sub-stream (detect role).
		for _, input := range fcam.FFmpeg.Inputs {
			if input.Path == "" {
				continue
			}
			for _, role := range input.Roles {
				switch role {
				case "record":
					if cam.MainStream == "" {
						cam.MainStream = input.Path
					}
				case "detect":
					if cam.SubStream == "" {
						cam.SubStream = input.Path
					}
				}
			}
		}

		// If no role-based distinction, use the first input as main stream.
		if cam.MainStream == "" && len(fcam.FFmpeg.Inputs) > 0 {
			cam.MainStream = fcam.FFmpeg.Inputs[0].Path
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("camera %q: no 'record' role found — using first input as main stream", name))
		}

		if cam.MainStream == "" {
			result.Errors = append(result.Errors,
				fmt.Sprintf("camera %q: no stream URL found, skipping", name))
			continue
		}

		// If sub-stream equals main stream, clear it (Sentinel uses sub-stream for
		// lower-resolution detection; same URL means no benefit).
		if cam.SubStream == cam.MainStream {
			cam.SubStream = ""
		}

		// Try to extract ONVIF host from the main stream URL — only for RTSP URLs.
		// Non-RTSP URLs (HTTP, etc.) rarely share credentials with ONVIF.
		if u, err := url.Parse(cam.MainStream); err == nil && u.Host != "" &&
			(u.Scheme == "rtsp" || u.Scheme == "rtsps") {
			cam.ONVIFHost = u.Hostname()
			if u.User != nil {
				cam.ONVIFUser = u.User.Username()
				cam.ONVIFPass, _ = u.User.Password()
			}
		}

		if cam.SubStream == "" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("camera %q: no 'detect' role sub-stream found", name))
		}

		result.Cameras = append(result.Cameras, cam)
	}

	if len(result.Cameras) == 0 && len(result.Errors) == 0 {
		result.Errors = append(result.Errors, "no valid cameras found in Frigate config")
	}

	return result
}
