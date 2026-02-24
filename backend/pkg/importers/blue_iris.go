package importers

import (
	"bufio"
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Blue Iris exports camera configuration in Windows Registry (.reg) format.
// Each camera lives under a GUID-named subkey beneath:
//   HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{GUID}
//
// Relevant values per camera:
//   "shortname"="Front Door"          — camera display name
//   "ip"="192.168.1.100"              — IP address or hostname
//   "port"=dword:00000230             — HTTP/RTSP port (hex DWORD, e.g. 554 = 0x230)
//   "main_url"="/Streaming/Channels/101"  — main stream path
//   "sub_url"="/Streaming/Channels/102"   — sub stream path
//   "user"="admin"                     — RTSP/ONVIF username
//   "pw"="..."                         — password (may be encoded)
//   "enable"=dword:00000001            — 0 or 1
//   "record"=dword:00000001            — 0 or 1
//
// Password handling: Blue Iris may obfuscate passwords. If the value looks
// hashed or unrecognizable, we emit a warning and leave ONVIFPass empty.

var (
	// Match only direct camera keys (not sub-keys) — [^\\]+ prevents matching nested paths.
	regKeyRE   = regexp.MustCompile(`^\[.+\\Cameras\\([^\\]+)\]$`)
	// Allow double quotes in values — match value between first "=" and final trailing quote.
	regStrRE   = regexp.MustCompile(`^"([^"]+)"="(.*)"$`)
	regDwordRE = regexp.MustCompile(`^"([^"]+)"=dword:([0-9a-fA-F]+)$`)
)

// ParseBlueIris parses a Blue Iris .reg export and returns imported cameras.
func ParseBlueIris(data []byte) *ImportResult {
	result := &ImportResult{Format: "blue_iris"}

	// Normalise line endings (Windows CRLF → LF).
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

	scanner := bufio.NewScanner(bytes.NewReader(data))
	type camEntry struct {
		shortname string
		ip        string
		port      int
		mainURL   string
		subURL    string
		user      string
		pw        string
		enabled   int // -1 = not set, 0, 1
		record    int
	}

	cameras := make(map[string]*camEntry) // keyed by registry GUID
	var currentGUID string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "Windows Registry Editor") {
			continue
		}

		// Check for new registry key section.
		if m := regKeyRE.FindStringSubmatch(line); m != nil {
			currentGUID = m[1]
			if _, ok := cameras[currentGUID]; !ok {
				cameras[currentGUID] = &camEntry{enabled: -1, record: -1, port: 554}
			}
			continue
		}

		if currentGUID == "" {
			continue
		}

		entry := cameras[currentGUID]

		// Try string value match.
		if m := regStrRE.FindStringSubmatch(line); m != nil {
			key, val := strings.ToLower(m[1]), m[2]
			switch key {
			case "shortname", "short":
				entry.shortname = val
			case "ip", "address":
				entry.ip = val
			case "main_url", "rtsp_path":
				entry.mainURL = val
			case "sub_url", "rtsp_path_alt":
				entry.subURL = val
			case "user", "username":
				entry.user = val
			case "pw", "password":
				entry.pw = val
			}
			continue
		}

		// Try DWORD value match.
		if m := regDwordRE.FindStringSubmatch(line); m != nil {
			key := strings.ToLower(m[1])
			hexVal := m[2]
			intVal, err := strconv.ParseInt(hexVal, 16, 64)
			if err != nil {
				continue
			}
			switch key {
			case "port":
				entry.port = int(intVal)
			case "enable", "enabled":
				entry.enabled = int(intVal)
			case "record":
				entry.record = int(intVal)
			}
			continue
		}
	}

	// Convert parsed entries to ImportedCamera structs.
	for guid, e := range cameras {
		if e.shortname == "" && e.ip == "" {
			// Skip entries with no useful data (parent keys, etc.)
			continue
		}

		name := e.shortname
		if name == "" {
			name = e.ip
		}
		if name == "" {
			result.Errors = append(result.Errors,
				fmt.Sprintf("camera %s: no name or IP found, skipping", guid))
			continue
		}

		if e.ip == "" {
			result.Errors = append(result.Errors,
				fmt.Sprintf("camera %q: no IP address found, skipping", name))
			continue
		}

		// Build RTSP URLs from IP + port + path.
		mainStream := buildRTSPURL(e.user, e.pw, e.ip, e.port, e.mainURL)
		if mainStream == "" {
			result.Errors = append(result.Errors,
				fmt.Sprintf("camera %q: no main stream path found, skipping", name))
			continue
		}

		var subStream string
		if e.subURL != "" {
			subStream = buildRTSPURL(e.user, e.pw, e.ip, e.port, e.subURL)
		}

		enabled := e.enabled != 0 // default to true if not set (-1)
		record := e.record == 1   // explicit opt-in: only true when explicitly enabled (not -1 unset)

		cam := ImportedCamera{
			Name:       sanitizeCameraName(name),
			Enabled:    enabled,
			MainStream: mainStream,
			SubStream:  subStream,
			Record:     record,
			Detect:     false, // Blue Iris detection doesn't map to Sentinel
			ONVIFHost:  e.ip,
			ONVIFPort:  80, // Blue Iris ONVIF port typically separate; default to 80
			ONVIFUser:  e.user,
		}

		// Only set ONVIFPass if the password looks like plaintext (not hashed/encoded).
		if e.pw != "" && len(e.pw) < 128 && !strings.HasPrefix(e.pw, "$") {
			cam.ONVIFPass = e.pw
		} else if e.pw != "" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("camera %q: password appears encoded — you'll need to re-enter it", name))
		}

		if subStream == "" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("camera %q: no sub-stream path found", name))
		}

		result.Cameras = append(result.Cameras, cam)
	}

	if len(result.Cameras) == 0 && len(result.Errors) == 0 {
		result.Errors = append(result.Errors, "no camera entries found in .reg file")
	}

	return result
}

// buildRTSPURL constructs an RTSP URL from components.
// Credentials are percent-encoded via url.UserPassword so special characters
// (e.g. "@", ":", "/") in the username or password do not break URL parsing.
func buildRTSPURL(user, pass, host string, port int, path string) string {
	if host == "" || path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := &url.URL{
		Scheme: "rtsp",
		Host:   host,
		Path:   path,
	}
	if port > 0 && port != 554 {
		u.Host = fmt.Sprintf("%s:%d", host, port)
	}
	if user != "" {
		if pass != "" {
			u.User = url.UserPassword(user, pass)
		} else {
			u.User = url.User(user)
		}
	}
	return u.String()
}

// sanitizeCameraName cleans a camera name to match Sentinel's validation regex:
//
//	^[a-zA-Z0-9]([a-zA-Z0-9 _-]{0,62}[a-zA-Z0-9_-])?$
//
// Replaces invalid characters with underscores and trims to 64 chars total.
func sanitizeCameraName(name string) string {
	// Replace any character not in [a-zA-Z0-9 _-] with underscore.
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == ' ' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	result := b.String()

	// Ensure it doesn't end with a space (must trim before length check).
	result = strings.TrimRight(result, " ")

	// Ensure it starts with alphanumeric — prepend "cam_" if needed.
	if len(result) > 0 && !((result[0] >= 'a' && result[0] <= 'z') ||
		(result[0] >= 'A' && result[0] <= 'Z') ||
		(result[0] >= '0' && result[0] <= '9')) {
		result = "cam_" + result
	}

	// Trim to 64 chars AFTER prepend to guarantee the result fits the regex.
	if len(result) > 64 {
		result = result[:64]
	}

	// Re-trim trailing spaces that the slice may have introduced.
	result = strings.TrimRight(result, " ")

	if result == "" {
		result = "imported_camera"
	}
	return result
}
