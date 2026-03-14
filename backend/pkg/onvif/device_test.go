package onvif

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- SOAP response XML fixtures ---

const deviceInfoSOAPResponse = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <s:Body>
    <tds:GetDeviceInformationResponse>
      <tds:Manufacturer>Hikvision</tds:Manufacturer>
      <tds:Model>DS-2CD2032</tds:Model>
      <tds:FirmwareVersion>5.4.5</tds:FirmwareVersion>
      <tds:SerialNumber>DS-2CD2032020160105</tds:SerialNumber>
      <tds:HardwareId>88</tds:HardwareId>
    </tds:GetDeviceInformationResponse>
  </s:Body>
</s:Envelope>`

const getProfilesSOAPResponse = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:trt="http://www.onvif.org/ver10/media/wsdl"
            xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <trt:GetProfilesResponse>
      <trt:Profiles token="Profile_1" fixed="true">
        <tt:Name>mainStream</tt:Name>
        <tt:VideoEncoderConfiguration>
          <tt:Encoding>H264</tt:Encoding>
          <tt:Resolution>
            <tt:Width>1920</tt:Width>
            <tt:Height>1080</tt:Height>
          </tt:Resolution>
        </tt:VideoEncoderConfiguration>
      </trt:Profiles>
      <trt:Profiles token="Profile_2" fixed="true">
        <tt:Name>subStream</tt:Name>
        <tt:VideoEncoderConfiguration>
          <tt:Encoding>H264</tt:Encoding>
          <tt:Resolution>
            <tt:Width>640</tt:Width>
            <tt:Height>480</tt:Height>
          </tt:Resolution>
        </tt:VideoEncoderConfiguration>
      </trt:Profiles>
    </trt:GetProfilesResponse>
  </s:Body>
</s:Envelope>`

const getStreamURISOAPResponseTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:trt="http://www.onvif.org/ver10/media/wsdl"
            xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <trt:GetStreamUriResponse>
      <trt:MediaUri>
        <tt:Uri>%s</tt:Uri>
      </trt:MediaUri>
    </trt:GetStreamUriResponse>
  </s:Body>
</s:Envelope>`

const invalidXMLResponse = `not xml at all <><><`

const soapFaultResponse = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <s:Fault>
      <s:Code><s:Value>s:Sender</s:Value></s:Code>
      <s:Reason><s:Text>Not Authorized</s:Text></s:Reason>
    </s:Fault>
  </s:Body>
</s:Envelope>`

// ---------- Tests for ProbeDevice ----------

func TestProbeDevice_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify SOAP content type
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "soap+xml") {
			t.Errorf("Content-Type = %q, want soap+xml", ct)
		}
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(deviceInfoSOAPResponse))
	}))
	defer srv.Close()

	// Extract host and port from test server URL
	host, port := parseTestServerAddr(t, srv)

	info, err := ProbeDevice(context.Background(), host, port)
	if err != nil {
		t.Fatalf("ProbeDevice returned error: %v", err)
	}

	if info.Manufacturer != "Hikvision" {
		t.Errorf("Manufacturer = %q, want %q", info.Manufacturer, "Hikvision")
	}
	if info.Model != "DS-2CD2032" {
		t.Errorf("Model = %q, want %q", info.Model, "DS-2CD2032")
	}
	if info.FirmwareVer != "5.4.5" {
		t.Errorf("FirmwareVer = %q, want %q", info.FirmwareVer, "5.4.5")
	}
	if info.SerialNumber != "DS-2CD2032020160105" {
		t.Errorf("SerialNumber = %q, want %q", info.SerialNumber, "DS-2CD2032020160105")
	}
}

func TestProbeDevice_Timeout(t *testing.T) {
	t.Parallel()

	// Server that sleeps longer than the client context timeout
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, port := parseTestServerAddr(t, srv)

	// Use a very short context timeout so the test runs fast
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := ProbeDevice(ctx, host, port)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestProbeDevice_InvalidXML(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(invalidXMLResponse))
	}))
	defer srv.Close()

	host, port := parseTestServerAddr(t, srv)

	_, err := ProbeDevice(context.Background(), host, port)
	if err == nil {
		t.Fatal("expected error for invalid XML response, got nil")
	}
	if !strings.Contains(err.Error(), "parsing response") {
		t.Errorf("error = %q, want it to contain 'parsing response'", err.Error())
	}
}

func TestProbeDevice_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	host, port := parseTestServerAddr(t, srv)

	_, err := ProbeDevice(context.Background(), host, port)
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to contain '500'", err.Error())
	}
}

// ---------- Tests for GetStreamProfiles ----------

func TestGetStreamProfiles_Success(t *testing.T) {
	t.Parallel()

	reqCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++

		// Read the request body to determine which SOAP action is being called
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		bodyStr := string(body[:n])

		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		if strings.Contains(bodyStr, "GetProfiles") {
			w.Write([]byte(getProfilesSOAPResponse))
		} else if strings.Contains(bodyStr, "GetStreamUri") {
			// Return different URIs based on which profile token was requested
			if strings.Contains(bodyStr, "Profile_1") {
				w.Write([]byte(fmt.Sprintf(getStreamURISOAPResponseTmpl, "rtsp://192.168.1.100:554/Streaming/Channels/101")))
			} else if strings.Contains(bodyStr, "Profile_2") {
				w.Write([]byte(fmt.Sprintf(getStreamURISOAPResponseTmpl, "rtsp://192.168.1.100:554/Streaming/Channels/102")))
			}
		}
	}))
	defer srv.Close()

	// GetStreamProfiles takes an xaddr and derives /onvif/media_service from it
	xaddr := srv.URL + "/onvif/device_service"

	profiles, err := GetStreamProfiles(context.Background(), xaddr, "admin", "password123")
	if err != nil {
		t.Fatalf("GetStreamProfiles returned error: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}

	// Verify first profile (mainStream)
	if profiles[0].Name != "mainStream" {
		t.Errorf("profiles[0].Name = %q, want %q", profiles[0].Name, "mainStream")
	}
	if profiles[0].Token != "Profile_1" {
		t.Errorf("profiles[0].Token = %q, want %q", profiles[0].Token, "Profile_1")
	}
	if profiles[0].Resolution != "1920x1080" {
		t.Errorf("profiles[0].Resolution = %q, want %q", profiles[0].Resolution, "1920x1080")
	}
	if profiles[0].Encoding != "H264" {
		t.Errorf("profiles[0].Encoding = %q, want %q", profiles[0].Encoding, "H264")
	}
	if profiles[0].StreamURI != "rtsp://192.168.1.100:554/Streaming/Channels/101" {
		t.Errorf("profiles[0].StreamURI = %q, want %q", profiles[0].StreamURI, "rtsp://192.168.1.100:554/Streaming/Channels/101")
	}

	// Verify second profile (subStream)
	if profiles[1].Name != "subStream" {
		t.Errorf("profiles[1].Name = %q, want %q", profiles[1].Name, "subStream")
	}
	if profiles[1].Token != "Profile_2" {
		t.Errorf("profiles[1].Token = %q, want %q", profiles[1].Token, "Profile_2")
	}
	if profiles[1].Resolution != "640x480" {
		t.Errorf("profiles[1].Resolution = %q, want %q", profiles[1].Resolution, "640x480")
	}
	if profiles[1].StreamURI != "rtsp://192.168.1.100:554/Streaming/Channels/102" {
		t.Errorf("profiles[1].StreamURI = %q, want %q", profiles[1].StreamURI, "rtsp://192.168.1.100:554/Streaming/Channels/102")
	}
}

func TestGetStreamProfiles_AuthRequired(t *testing.T) {
	t.Parallel()

	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 8192)
		n, _ := r.Body.Read(body)
		capturedBody = string(body[:n])

		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		if strings.Contains(capturedBody, "GetProfiles") {
			w.Write([]byte(getProfilesSOAPResponse))
		} else if strings.Contains(capturedBody, "GetStreamUri") {
			w.Write([]byte(fmt.Sprintf(getStreamURISOAPResponseTmpl, "rtsp://192.168.1.100:554/stream")))
		}
	}))
	defer srv.Close()

	xaddr := srv.URL + "/onvif/device_service"

	_, err := GetStreamProfiles(context.Background(), xaddr, "admin", "secret")
	if err != nil {
		t.Fatalf("GetStreamProfiles returned error: %v", err)
	}

	// Verify WS-Security elements were present in the captured request
	securityElements := []string{
		"<Security",
		"<UsernameToken>",
		"<Username>admin</Username>",
		"PasswordDigest",
		"<Nonce",
		"<Created",
	}
	for _, elem := range securityElements {
		if !strings.Contains(capturedBody, elem) {
			t.Errorf("request body missing WS-Security element: %q", elem)
		}
	}
}

func TestGetStreamProfiles_EmptyXAddr(t *testing.T) {
	t.Parallel()

	_, err := GetStreamProfiles(context.Background(), "", "admin", "pass")
	if err == nil {
		t.Fatal("expected error for empty xaddr, got nil")
	}
}

func TestGetStreamProfiles_InvalidXML(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(invalidXMLResponse))
	}))
	defer srv.Close()

	xaddr := srv.URL + "/onvif/device_service"

	_, err := GetStreamProfiles(context.Background(), xaddr, "admin", "pass")
	if err == nil {
		t.Fatal("expected error for invalid XML response, got nil")
	}
}

// ---------- Tests for buildAuthHeader ----------

func TestBuildAuthHeader(t *testing.T) {
	t.Parallel()

	header, err := buildAuthHeader("admin", "secret123")
	if err != nil {
		t.Fatalf("buildAuthHeader returned error: %v", err)
	}

	// Verify required elements are present
	required := []string{
		"<Security",
		"<UsernameToken>",
		"<Username>admin</Username>",
		"PasswordDigest",
		"<Nonce",
		"EncodingType=",
		"<Created",
	}
	for _, s := range required {
		if !strings.Contains(header, s) {
			t.Errorf("auth header missing %q", s)
		}
	}

	// Verify two calls produce different nonces/digests
	header2, err := buildAuthHeader("admin", "secret123")
	if err != nil {
		t.Fatalf("second buildAuthHeader returned error: %v", err)
	}
	if header == header2 {
		t.Error("two consecutive auth headers are identical (nonce should differ)")
	}
}

func TestBuildAuthHeader_XMLEscaping(t *testing.T) {
	t.Parallel()

	header, err := buildAuthHeader("admin<>&\"'", "pass")
	if err != nil {
		t.Fatalf("buildAuthHeader returned error: %v", err)
	}

	// Username with special XML chars should be escaped
	if strings.Contains(header, "admin<>") {
		t.Error("auth header contains unescaped XML special characters in username")
	}
	// Should contain the XML-escaped version
	if !strings.Contains(header, "admin&lt;&gt;&amp;") {
		t.Error("auth header missing XML-escaped username")
	}
}

// ---------- Tests for deriveMediaURL ----------

func TestDeriveMediaURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		xaddr   string
		want    string
		wantErr bool
	}{
		{
			name:  "standard device service URL",
			xaddr: "http://192.168.1.100:80/onvif/device_service",
			want:  "http://192.168.1.100:80/onvif/media_service",
		},
		{
			name:  "non-standard port",
			xaddr: "http://10.0.0.5:8080/onvif/device_service",
			want:  "http://10.0.0.5:8080/onvif/media_service",
		},
		{
			name:  "no explicit port HTTP",
			xaddr: "http://192.168.1.100/onvif/device_service",
			want:  "http://192.168.1.100:80/onvif/media_service",
		},
		{
			name:  "HTTPS without port defaults to 443",
			xaddr: "https://192.168.1.100/onvif/device_service",
			want:  "http://192.168.1.100:443/onvif/media_service",
		},
		{
			name:    "empty xaddr",
			xaddr:   "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := deriveMediaURL(tc.xaddr)
			if tc.wantErr {
				if err == nil {
					t.Errorf("deriveMediaURL(%q) expected error, got nil", tc.xaddr)
				}
				return
			}
			if err != nil {
				t.Fatalf("deriveMediaURL(%q) returned error: %v", tc.xaddr, err)
			}
			if got != tc.want {
				t.Errorf("deriveMediaURL(%q) = %q, want %q", tc.xaddr, got, tc.want)
			}
		})
	}
}

// ---------- Tests for xmlEscape ----------

func TestXmlEscape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"<script>", "&lt;script&gt;"},
		{"a&b", "a&amp;b"},
		{`say "hi"`, "say &#34;hi&#34;"},
		{"it's", "it&#39;s"},
		{"", ""},
		{"normal text 123", "normal text 123"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := xmlEscape(tc.input)
			if got != tc.want {
				t.Errorf("xmlEscape(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------- Tests for renderTemplate ----------

func TestRenderTemplate(t *testing.T) {
	t.Parallel()

	// Test with the device info template (nil data)
	result, err := renderTemplate(getDeviceInfoTemplate, nil)
	if err != nil {
		t.Fatalf("renderTemplate returned error: %v", err)
	}
	body := string(result)
	if !strings.Contains(body, "GetDeviceInformation") {
		t.Error("rendered template missing GetDeviceInformation")
	}
	if !strings.Contains(body, `<?xml version="1.0"`) {
		t.Error("rendered template missing XML declaration")
	}
}

// ---------- Tests for truncate ----------

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"abc", 0, "..."},
	}

	for _, tc := range tests {
		name := fmt.Sprintf("%q/%d", tc.input, tc.maxLen)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := truncate(tc.input, tc.maxLen)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}

// ---------- Test helper ----------

// parseTestServerAddr extracts host and port from an httptest.Server URL.
func parseTestServerAddr(t *testing.T, srv *httptest.Server) (string, int) {
	t.Helper()
	// srv.URL is like "http://127.0.0.1:PORT"
	url := srv.URL
	// Remove scheme
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")

	parts := strings.SplitN(url, ":", 2)
	if len(parts) != 2 {
		t.Fatalf("failed to parse test server address: %q", srv.URL)
	}

	var port int
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
		t.Fatalf("failed to parse port from %q: %v", parts[1], err)
	}

	return parts[0], port
}
