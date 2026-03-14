package onvif

import (
	"testing"
)

// --- WS-Discovery ProbeMatch XML fixtures ---

const singleDeviceProbeMatch = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
  <s:Header>
    <a:MessageID>uuid:00000000-0000-0000-0000-000000000001</a:MessageID>
    <a:RelatesTo>uuid:request-001</a:RelatesTo>
    <a:To>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:To>
    <a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</a:Action>
  </s:Header>
  <s:Body>
    <d:ProbeMatches>
      <d:ProbeMatch>
        <a:EndpointReference>
          <a:Address>urn:uuid:abcd1234-5678-90ab-cdef-1234567890ab</a:Address>
        </a:EndpointReference>
        <d:Types>dn:NetworkVideoTransmitter</d:Types>
        <d:Scopes>onvif://www.onvif.org/type/video_encoder onvif://www.onvif.org/name/Hikvision%20DS-2CD2032 onvif://www.onvif.org/hardware/DS-2CD2032 onvif://www.onvif.org/location/city/Anywhere</d:Scopes>
        <d:XAddrs>http://192.168.1.100:80/onvif/device_service</d:XAddrs>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`

const multiDeviceProbeMatch = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
  <s:Header>
    <a:MessageID>uuid:00000000-0000-0000-0000-000000000002</a:MessageID>
  </s:Header>
  <s:Body>
    <d:ProbeMatches>
      <d:ProbeMatch>
        <a:EndpointReference>
          <a:Address>urn:uuid:aaaa1111-2222-3333-4444-555566667777</a:Address>
        </a:EndpointReference>
        <d:Types>dn:NetworkVideoTransmitter</d:Types>
        <d:Scopes>onvif://www.onvif.org/name/FrontDoor onvif://www.onvif.org/hardware/DS-2CD2042</d:Scopes>
        <d:XAddrs>http://192.168.1.101:8080/onvif/device_service</d:XAddrs>
      </d:ProbeMatch>
      <d:ProbeMatch>
        <a:EndpointReference>
          <a:Address>urn:uuid:bbbb2222-3333-4444-5555-666677778888</a:Address>
        </a:EndpointReference>
        <d:Types>dn:NetworkVideoTransmitter</d:Types>
        <d:Scopes>onvif://www.onvif.org/name/BackYard onvif://www.onvif.org/hardware/IPC-HDW4631C</d:Scopes>
        <d:XAddrs>http://10.0.0.50:80/onvif/device_service</d:XAddrs>
      </d:ProbeMatch>
      <d:ProbeMatch>
        <a:EndpointReference>
          <a:Address>urn:uuid:cccc3333-4444-5555-6666-777788889999</a:Address>
        </a:EndpointReference>
        <d:Types>dn:NetworkVideoTransmitter</d:Types>
        <d:Scopes>onvif://www.onvif.org/name/Garage%20Cam onvif://www.onvif.org/hardware/RLC-810A</d:Scopes>
        <d:XAddrs>http://192.168.1.200:2020/onvif/device_service</d:XAddrs>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`

const emptyScopesProbeMatch = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
  <s:Header/>
  <s:Body>
    <d:ProbeMatches>
      <d:ProbeMatch>
        <a:EndpointReference>
          <a:Address>urn:uuid:dddd4444-5555-6666-7777-888899990000</a:Address>
        </a:EndpointReference>
        <d:Types>dn:NetworkVideoTransmitter</d:Types>
        <d:Scopes></d:Scopes>
        <d:XAddrs>http://192.168.1.55:80/onvif/device_service</d:XAddrs>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`

const noEndpointRefProbeMatch = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
  <s:Header/>
  <s:Body>
    <d:ProbeMatches>
      <d:ProbeMatch>
        <a:EndpointReference>
          <a:Address></a:Address>
        </a:EndpointReference>
        <d:Scopes>onvif://www.onvif.org/name/TestCam</d:Scopes>
        <d:XAddrs>http://192.168.1.77:80/onvif/device_service</d:XAddrs>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`

const malformedXML = `<?xml version="1.0"?><broken><unclosed`

const httpsXAddrProbeMatch = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
  <s:Header/>
  <s:Body>
    <d:ProbeMatches>
      <d:ProbeMatch>
        <a:EndpointReference>
          <a:Address>urn:uuid:eeee5555-6666-7777-8888-999900001111</a:Address>
        </a:EndpointReference>
        <d:Scopes>onvif://www.onvif.org/name/SecureCam onvif://www.onvif.org/hardware/SecModel</d:Scopes>
        <d:XAddrs>https://192.168.1.200/onvif/device_service</d:XAddrs>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`

// ---------- Tests for parseProbeMatch ----------

func TestParseProbeMatch(t *testing.T) {
	t.Parallel()

	devices, err := parseProbeMatch([]byte(singleDeviceProbeMatch))
	if err != nil {
		t.Fatalf("parseProbeMatch returned error: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}

	dev := devices[0]

	if dev.XAddr != "http://192.168.1.100:80/onvif/device_service" {
		t.Errorf("XAddr = %q, want %q", dev.XAddr, "http://192.168.1.100:80/onvif/device_service")
	}
	if dev.IP != "192.168.1.100" {
		t.Errorf("IP = %q, want %q", dev.IP, "192.168.1.100")
	}
	if dev.Port != 80 {
		t.Errorf("Port = %d, want 80", dev.Port)
	}
	if dev.Name != "Hikvision DS-2CD2032" {
		t.Errorf("Name = %q, want %q", dev.Name, "Hikvision DS-2CD2032")
	}
	if dev.Hardware != "DS-2CD2032" {
		t.Errorf("Hardware = %q, want %q", dev.Hardware, "DS-2CD2032")
	}
	if dev.EndpointRef != "urn:uuid:abcd1234-5678-90ab-cdef-1234567890ab" {
		t.Errorf("EndpointRef = %q, want %q", dev.EndpointRef, "urn:uuid:abcd1234-5678-90ab-cdef-1234567890ab")
	}
}

func TestParseProbeMatch_MultipleDevices(t *testing.T) {
	t.Parallel()

	devices, err := parseProbeMatch([]byte(multiDeviceProbeMatch))
	if err != nil {
		t.Fatalf("parseProbeMatch returned error: %v", err)
	}
	if len(devices) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(devices))
	}

	// Verify each device parsed correctly (table-driven checks)
	tests := []struct {
		idx      int
		ip       string
		port     int
		name     string
		hardware string
		endpoint string
	}{
		{0, "192.168.1.101", 8080, "FrontDoor", "DS-2CD2042", "urn:uuid:aaaa1111-2222-3333-4444-555566667777"},
		{1, "10.0.0.50", 80, "BackYard", "IPC-HDW4631C", "urn:uuid:bbbb2222-3333-4444-5555-666677778888"},
		{2, "192.168.1.200", 2020, "Garage Cam", "RLC-810A", "urn:uuid:cccc3333-4444-5555-6666-777788889999"},
	}
	for _, tc := range tests {
		dev := devices[tc.idx]
		if dev.IP != tc.ip {
			t.Errorf("device[%d].IP = %q, want %q", tc.idx, dev.IP, tc.ip)
		}
		if dev.Port != tc.port {
			t.Errorf("device[%d].Port = %d, want %d", tc.idx, dev.Port, tc.port)
		}
		if dev.Name != tc.name {
			t.Errorf("device[%d].Name = %q, want %q", tc.idx, dev.Name, tc.name)
		}
		if dev.Hardware != tc.hardware {
			t.Errorf("device[%d].Hardware = %q, want %q", tc.idx, dev.Hardware, tc.hardware)
		}
		if dev.EndpointRef != tc.endpoint {
			t.Errorf("device[%d].EndpointRef = %q, want %q", tc.idx, dev.EndpointRef, tc.endpoint)
		}
	}
}

func TestParseProbeMatch_MalformedXML(t *testing.T) {
	t.Parallel()

	_, err := parseProbeMatch([]byte(malformedXML))
	if err == nil {
		t.Fatal("expected error for malformed XML, got nil")
	}
}

func TestParseProbeMatch_EmptyInput(t *testing.T) {
	t.Parallel()

	_, err := parseProbeMatch([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
}

func TestParseProbeMatch_EmptyScopes(t *testing.T) {
	t.Parallel()

	devices, err := parseProbeMatch([]byte(emptyScopesProbeMatch))
	if err != nil {
		t.Fatalf("parseProbeMatch returned error: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}

	dev := devices[0]
	if dev.XAddr != "http://192.168.1.55:80/onvif/device_service" {
		t.Errorf("XAddr = %q, want %q", dev.XAddr, "http://192.168.1.55:80/onvif/device_service")
	}
	if dev.IP != "192.168.1.55" {
		t.Errorf("IP = %q, want %q", dev.IP, "192.168.1.55")
	}
	if dev.Port != 80 {
		t.Errorf("Port = %d, want 80", dev.Port)
	}
	// Name and Hardware should be empty strings when scopes are empty
	if dev.Name != "" {
		t.Errorf("Name = %q, want empty string", dev.Name)
	}
	if dev.Hardware != "" {
		t.Errorf("Hardware = %q, want empty string", dev.Hardware)
	}
	// EndpointRef should still be present
	if dev.EndpointRef != "urn:uuid:dddd4444-5555-6666-7777-888899990000" {
		t.Errorf("EndpointRef = %q, want %q", dev.EndpointRef, "urn:uuid:dddd4444-5555-6666-7777-888899990000")
	}
}

func TestParseProbeMatch_HTTPSXAddr(t *testing.T) {
	t.Parallel()

	devices, err := parseProbeMatch([]byte(httpsXAddrProbeMatch))
	if err != nil {
		t.Fatalf("parseProbeMatch returned error: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}

	dev := devices[0]
	// HTTPS with no explicit port should default to 443
	if dev.Port != 443 {
		t.Errorf("Port = %d, want 443 for HTTPS without explicit port", dev.Port)
	}
	if dev.Name != "SecureCam" {
		t.Errorf("Name = %q, want %q", dev.Name, "SecureCam")
	}
	if dev.Hardware != "SecModel" {
		t.Errorf("Hardware = %q, want %q", dev.Hardware, "SecModel")
	}
}

// ---------- Tests for extractScope ----------

func TestExtractScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scopes   string
		prefix   string
		expected string
	}{
		{
			name:     "name scope found",
			scopes:   "onvif://www.onvif.org/type/video_encoder onvif://www.onvif.org/name/MyCamera onvif://www.onvif.org/hardware/Model1",
			prefix:   "onvif://www.onvif.org/name/",
			expected: "MyCamera",
		},
		{
			name:     "hardware scope found",
			scopes:   "onvif://www.onvif.org/name/Cam1 onvif://www.onvif.org/hardware/DS-2CD2032",
			prefix:   "onvif://www.onvif.org/hardware/",
			expected: "DS-2CD2032",
		},
		{
			name:     "scope not found",
			scopes:   "onvif://www.onvif.org/type/video_encoder onvif://www.onvif.org/name/TestCam",
			prefix:   "onvif://www.onvif.org/hardware/",
			expected: "",
		},
		{
			name:     "empty scopes",
			scopes:   "",
			prefix:   "onvif://www.onvif.org/name/",
			expected: "",
		},
		{
			name:     "URL-encoded value",
			scopes:   "onvif://www.onvif.org/name/My%20Camera%20Name",
			prefix:   "onvif://www.onvif.org/name/",
			expected: "My%20Camera%20Name", // extractScope returns raw; URL-decoding happens in caller
		},
		{
			name:     "multiple spaces between scopes",
			scopes:   "onvif://www.onvif.org/type/video_encoder   onvif://www.onvif.org/name/TestCam   onvif://www.onvif.org/hardware/HW1",
			prefix:   "onvif://www.onvif.org/name/",
			expected: "TestCam",
		},
		{
			name:     "first match wins",
			scopes:   "onvif://www.onvif.org/name/First onvif://www.onvif.org/name/Second",
			prefix:   "onvif://www.onvif.org/name/",
			expected: "First",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractScope(tc.scopes, tc.prefix)
			if got != tc.expected {
				t.Errorf("extractScope(%q, %q) = %q, want %q", tc.scopes, tc.prefix, got, tc.expected)
			}
		})
	}
}

// ---------- Tests for parseXAddr ----------

func TestParseXAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		xaddr    string
		wantIP   string
		wantPort int
		wantErr  bool
	}{
		{
			name:     "standard HTTP with port",
			xaddr:    "http://192.168.1.100:80/onvif/device_service",
			wantIP:   "192.168.1.100",
			wantPort: 80,
		},
		{
			name:     "HTTP with non-standard port",
			xaddr:    "http://192.168.1.100:8080/onvif/device_service",
			wantIP:   "192.168.1.100",
			wantPort: 8080,
		},
		{
			name:     "HTTP without explicit port",
			xaddr:    "http://192.168.1.100/onvif/device_service",
			wantIP:   "192.168.1.100",
			wantPort: 80,
		},
		{
			name:     "HTTPS without explicit port",
			xaddr:    "https://192.168.1.100/onvif/device_service",
			wantIP:   "192.168.1.100",
			wantPort: 443,
		},
		{
			name:     "HTTPS with explicit port",
			xaddr:    "https://192.168.1.100:8443/onvif/device_service",
			wantIP:   "192.168.1.100",
			wantPort: 8443,
		},
		{
			name:    "invalid port",
			xaddr:   "http://192.168.1.100:notaport/onvif/device_service",
			wantErr: true,
		},
		{
			name:     "hostname instead of IP",
			xaddr:    "http://camera.local:80/onvif/device_service",
			wantIP:   "camera.local",
			wantPort: 80,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ip, port, err := parseXAddr(tc.xaddr)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseXAddr(%q) expected error, got nil", tc.xaddr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseXAddr(%q) returned error: %v", tc.xaddr, err)
			}
			if ip != tc.wantIP {
				t.Errorf("parseXAddr(%q) IP = %q, want %q", tc.xaddr, ip, tc.wantIP)
			}
			if port != tc.wantPort {
				t.Errorf("parseXAddr(%q) port = %d, want %d", tc.xaddr, port, tc.wantPort)
			}
		})
	}
}

// ---------- Tests for generateUUID ----------

func TestGenerateUUID(t *testing.T) {
	t.Parallel()

	uuid, err := generateUUID()
	if err != nil {
		t.Fatalf("generateUUID returned error: %v", err)
	}

	// UUID v4 format: 8-4-4-4-12 hex chars
	if len(uuid) != 36 {
		t.Errorf("UUID length = %d, want 36", len(uuid))
	}

	// Check dashes at correct positions
	if uuid[8] != '-' || uuid[13] != '-' || uuid[18] != '-' || uuid[23] != '-' {
		t.Errorf("UUID %q has incorrect dash positions", uuid)
	}

	// Version nibble (position 14) should be '4'
	if uuid[14] != '4' {
		t.Errorf("UUID version nibble = %c, want '4'", uuid[14])
	}

	// Variant nibble (position 19) should be '8', '9', 'a', or 'b'
	v := uuid[19]
	if v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Errorf("UUID variant nibble = %c, want one of '8','9','a','b'", v)
	}

	// Uniqueness: two calls should produce different UUIDs
	uuid2, err := generateUUID()
	if err != nil {
		t.Fatalf("second generateUUID returned error: %v", err)
	}
	if uuid == uuid2 {
		t.Errorf("two consecutive UUIDs are identical: %q", uuid)
	}
}

// ---------- Tests for BuildProbeXML ----------

func TestBuildProbeXML(t *testing.T) {
	t.Parallel()

	xml, err := renderTemplate(probeTemplate, struct{ MessageID string }{MessageID: "test-uuid-1234"})
	if err != nil {
		t.Fatalf("renderTemplate returned error: %v", err)
	}

	body := string(xml)

	// Verify the probe contains required elements
	mustContain := []string{
		"uuid:test-uuid-1234",
		"urn:schemas-xmlsoap-org:ws:2005:04:discovery",
		"http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe",
		"NetworkVideoTransmitter",
		`<?xml version="1.0" encoding="UTF-8"?>`,
	}
	for _, s := range mustContain {
		if !contains(body, s) {
			t.Errorf("probe XML missing expected string: %q", s)
		}
	}
}

// contains is a test helper checking substring presence.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
