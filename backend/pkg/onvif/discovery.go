package onvif

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// wsDiscoveryAddr is the WS-Discovery multicast address.
	wsDiscoveryAddr = "239.255.255.250:3702"
	// maxUDPResponse is the maximum size of a single UDP response.
	maxUDPResponse = 65535
)

// DiscoveredDevice represents a camera found via WS-Discovery multicast.
type DiscoveredDevice struct {
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	Name        string `json:"name"`
	Hardware    string `json:"hardware"`
	XAddr       string `json:"xaddr"`
	EndpointRef string `json:"endpoint_ref"`
}

// Discover sends a WS-Discovery Probe via UDP multicast to 239.255.255.250:3702.
// Returns all ONVIF NetworkVideoTransmitter devices that respond within the timeout.
// NOTE: Multicast does NOT work in Docker bridge networking — requires host networking.
func Discover(ctx context.Context, timeout time.Duration) ([]DiscoveredDevice, error) {
	messageID, err := generateUUID()
	if err != nil {
		return nil, fmt.Errorf("onvif discover: generating message ID: %w", err)
	}

	probeXML, err := renderTemplate(probeTemplate, struct{ MessageID string }{MessageID: messageID})
	if err != nil {
		return nil, fmt.Errorf("onvif discover: building probe: %w", err)
	}

	// Resolve multicast address
	addr, err := net.ResolveUDPAddr("udp4", wsDiscoveryAddr)
	if err != nil {
		return nil, fmt.Errorf("onvif discover: resolving multicast addr: %w", err)
	}

	// Bind to any local address for receiving responses.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("onvif discover: binding UDP socket: %w", err)
	}
	defer conn.Close()

	// Send the probe
	if _, err := conn.WriteToUDP(probeXML, addr); err != nil {
		return nil, fmt.Errorf("onvif discover: sending probe: %w", err)
	}
	slog.Debug("onvif: sent WS-Discovery probe", "multicast", wsDiscoveryAddr, "message_id", messageID)

	// Set deadline from context or timeout (whichever is sooner)
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	conn.SetReadDeadline(deadline)

	// Collect responses
	seen := make(map[string]struct{}) // deduplicate by endpoint reference
	var devices []DiscoveredDevice
	buf := make([]byte, maxUDPResponse)

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return devices, nil
		default:
		}

		n, _, readErr := conn.ReadFromUDP(buf)
		if readErr != nil {
			// Timeout is expected — we're done collecting
			if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
				break
			}
			slog.Debug("onvif: error reading probe response", "error", readErr)
			break
		}

		parsed, parseErr := parseProbeMatch(buf[:n])
		if parseErr != nil {
			slog.Debug("onvif: skipping malformed probe response", "error", parseErr)
			continue
		}

		for _, dev := range parsed {
			if dev.EndpointRef == "" {
				continue
			}
			if _, exists := seen[dev.EndpointRef]; exists {
				continue
			}
			seen[dev.EndpointRef] = struct{}{}
			devices = append(devices, dev)
		}
	}

	slog.Info("onvif: discovery complete", "found", len(devices))
	return devices, nil
}

// parseProbeMatch extracts DiscoveredDevice entries from a raw WS-Discovery ProbeMatch response.
func parseProbeMatch(data []byte) ([]DiscoveredDevice, error) {
	var env probeMatchEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parsing ProbeMatch XML: %w", err)
	}

	var devices []DiscoveredDevice
	for _, m := range env.Body.ProbeMatches.Matches {
		dev := DiscoveredDevice{
			EndpointRef: strings.TrimSpace(m.EndpointRef.Address),
		}

		// Parse XAddrs — may contain multiple URLs separated by spaces.
		// Pick the first HTTP/HTTPS one.
		xaddrs := strings.Fields(m.XAddrs)
		if len(xaddrs) > 0 {
			dev.XAddr = xaddrs[0]
			ip, port, err := parseXAddr(xaddrs[0])
			if err == nil {
				dev.IP = ip
				dev.Port = port
			}
		}

		// Parse scopes for name and hardware model
		dev.Name = extractScope(m.Scopes, "onvif://www.onvif.org/name/")
		dev.Hardware = extractScope(m.Scopes, "onvif://www.onvif.org/hardware/")

		// URL-decode name and hardware (cameras often encode spaces as %20)
		if decoded, err := url.PathUnescape(dev.Name); err == nil {
			dev.Name = decoded
		}
		if decoded, err := url.PathUnescape(dev.Hardware); err == nil {
			dev.Hardware = decoded
		}

		if dev.IP != "" {
			devices = append(devices, dev)
		}
	}

	return devices, nil
}

// parseXAddr extracts the IP and port from an ONVIF XAddr URL.
func parseXAddr(xaddr string) (string, int, error) {
	u, err := url.Parse(xaddr)
	if err != nil {
		return "", 0, err
	}

	host := u.Hostname()
	portStr := u.Port()
	port := 80
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return "", 0, fmt.Errorf("invalid port %q: %w", portStr, err)
		}
		port = p
	} else if u.Scheme == "https" {
		port = 443
	}

	return host, port, nil
}

// extractScope finds a scope value matching the given prefix.
// Scopes in WS-Discovery are space-separated URIs.
func extractScope(scopes, prefix string) string {
	for _, scope := range strings.Fields(scopes) {
		if strings.HasPrefix(scope, prefix) {
			return strings.TrimPrefix(scope, prefix)
		}
	}
	return ""
}

// generateUUID creates a UUID v4 string using crypto/rand (no external deps).
func generateUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Set version 4 and variant bits per RFC 4122
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	), nil
}
