package onvif

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"time"
)

const (
	// defaultProbeTimeout is the HTTP timeout for device information queries.
	defaultProbeTimeout = 10 * time.Second
	// defaultStreamTimeout is the HTTP timeout for stream profile queries.
	defaultStreamTimeout = 10 * time.Second
)

// DeviceInfo from GetDeviceInformation.
type DeviceInfo struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	FirmwareVer  string `json:"firmware_version"`
	SerialNumber string `json:"serial_number"`
}

// StreamProfile from GetProfiles + GetStreamUri.
type StreamProfile struct {
	Name       string `json:"name"`
	Token      string `json:"token"`
	Resolution string `json:"resolution"` // e.g. "1920x1080"
	Encoding   string `json:"encoding"`   // e.g. "H264"
	StreamURI  string `json:"stream_uri"` // RTSP URL
}

// ProbeDevice queries a specific ONVIF device by IP/port without authentication.
// Returns basic device info. This works through Docker bridge networking (unicast HTTP).
func ProbeDevice(ctx context.Context, host string, port int) (*DeviceInfo, error) {
	deviceURL := fmt.Sprintf("http://%s:%d/onvif/device_service", host, port)

	body, err := renderTemplate(getDeviceInfoTemplate, nil)
	if err != nil {
		return nil, fmt.Errorf("onvif probe: building request: %w", err)
	}

	respBody, err := soapCall(ctx, deviceURL, body, defaultProbeTimeout)
	if err != nil {
		return nil, fmt.Errorf("onvif probe %s:%d: %w", host, port, err)
	}

	var env deviceInfoEnvelope
	if err := xml.Unmarshal(respBody, &env); err != nil {
		return nil, fmt.Errorf("onvif probe %s:%d: parsing response: %w", host, port, err)
	}

	info := &DeviceInfo{
		Manufacturer: env.Body.Response.Manufacturer,
		Model:        env.Body.Response.Model,
		FirmwareVer:  env.Body.Response.FirmwareVersion,
		SerialNumber: env.Body.Response.SerialNumber,
	}

	slog.Debug("onvif: probed device", "host", host, "port", port,
		"manufacturer", info.Manufacturer, "model", info.Model)

	return info, nil
}

// GetStreamProfiles queries an authenticated ONVIF device for its media profiles
// and their RTSP stream URIs.
func GetStreamProfiles(ctx context.Context, xaddr, username, password string) ([]StreamProfile, error) {
	// Step 1: Get media profiles
	authHeader, err := buildAuthHeader(username, password)
	if err != nil {
		return nil, fmt.Errorf("onvif streams: building auth: %w", err)
	}

	// Derive media service URL from the device xaddr.
	// ONVIF devices typically expose /onvif/media_service at the same host:port.
	mediaURL, err := deriveMediaURL(xaddr)
	if err != nil {
		return nil, fmt.Errorf("onvif streams: deriving media URL from %q: %w", xaddr, err)
	}

	profilesBody, err := renderTemplate(getProfilesTemplate, struct{ AuthHeader string }{AuthHeader: authHeader})
	if err != nil {
		return nil, fmt.Errorf("onvif streams: building GetProfiles request: %w", err)
	}

	profilesResp, err := soapCall(ctx, mediaURL, profilesBody, defaultStreamTimeout)
	if err != nil {
		return nil, fmt.Errorf("onvif streams: GetProfiles: %w", err)
	}

	var profilesEnv getProfilesEnvelope
	if err := xml.Unmarshal(profilesResp, &profilesEnv); err != nil {
		return nil, fmt.Errorf("onvif streams: parsing GetProfiles response: %w", err)
	}

	profiles := profilesEnv.Body.Response.Profiles
	if len(profiles) == 0 {
		slog.Debug("onvif: device returned 0 profiles", "xaddr", xaddr)
		return nil, nil
	}

	// Step 2: For each profile, get the stream URI
	var results []StreamProfile
	for _, p := range profiles {
		streamURI, uriErr := getStreamURI(ctx, mediaURL, p.Token, username, password)
		if uriErr != nil {
			slog.Debug("onvif: failed to get stream URI for profile",
				"profile", p.Name, "token", p.Token, "error", uriErr)
			// Skip this profile but continue with others
			continue
		}

		sp := StreamProfile{
			Name:      p.Name,
			Token:     p.Token,
			Encoding:  p.Video.Encoding,
			StreamURI: streamURI,
		}
		if p.Video.Resolution.Width > 0 && p.Video.Resolution.Height > 0 {
			sp.Resolution = fmt.Sprintf("%dx%d", p.Video.Resolution.Width, p.Video.Resolution.Height)
		}

		results = append(results, sp)
	}

	slog.Debug("onvif: retrieved stream profiles", "xaddr", xaddr, "count", len(results))
	return results, nil
}

// getStreamURI fetches the RTSP stream URI for a single profile token.
func getStreamURI(ctx context.Context, mediaURL, profileToken, username, password string) (string, error) {
	// Each call needs a fresh nonce/created for the digest
	authHeader, err := buildAuthHeader(username, password)
	if err != nil {
		return "", fmt.Errorf("building auth for GetStreamUri: %w", err)
	}

	body, err := renderTemplate(getStreamURITemplate, struct {
		AuthHeader   string
		ProfileToken string
	}{
		AuthHeader:   authHeader,
		ProfileToken: xmlEscape(profileToken),
	})
	if err != nil {
		return "", fmt.Errorf("building GetStreamUri request: %w", err)
	}

	respBody, err := soapCall(ctx, mediaURL, body, defaultStreamTimeout)
	if err != nil {
		return "", fmt.Errorf("GetStreamUri for %q: %w", profileToken, err)
	}

	var env getStreamURIEnvelope
	if err := xml.Unmarshal(respBody, &env); err != nil {
		return "", fmt.Errorf("parsing GetStreamUri response for %q: %w", profileToken, err)
	}

	return env.Body.Response.MediaURI.URI, nil
}

// deriveMediaURL converts a device service XAddr to the media service URL.
// E.g. "http://192.168.1.100:80/onvif/device_service" → "http://192.168.1.100:80/onvif/media_service"
// If the xaddr doesn't contain /onvif/device_service, we append /onvif/media_service to the host:port.
func deriveMediaURL(xaddr string) (string, error) {
	if xaddr == "" {
		return "", fmt.Errorf("empty xaddr")
	}

	// Parse and rebuild with media_service path
	ip, port, err := parseXAddr(xaddr)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%s:%d/onvif/media_service", ip, port), nil
}
