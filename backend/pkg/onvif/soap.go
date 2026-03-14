// Package onvif provides ONVIF camera auto-discovery and device querying
// using only Go stdlib — no external dependencies.
package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"text/template"
	"time"
)

// ---------- SOAP envelope templates ----------

// probeTemplate is the WS-Discovery Probe message for finding ONVIF devices.
var probeTemplate = template.Must(template.New("probe").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
  <s:Header>
    <a:MessageID>uuid:{{.MessageID}}</a:MessageID>
    <a:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</a:To>
    <a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</a:Action>
  </s:Header>
  <s:Body>
    <d:Probe>
      <d:Types>dn:NetworkVideoTransmitter</d:Types>
    </d:Probe>
  </s:Body>
</s:Envelope>`))

// getDeviceInfoTemplate is the SOAP body for GetDeviceInformation (no auth needed).
var getDeviceInfoTemplate = template.Must(template.New("devinfo").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <s:Header/>
  <s:Body>
    <tds:GetDeviceInformation/>
  </s:Body>
</s:Envelope>`))

// getProfilesTemplate is the SOAP body for GetProfiles (requires auth).
var getProfilesTemplate = template.Must(template.New("profiles").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <s:Header>
    {{.AuthHeader}}
  </s:Header>
  <s:Body>
    <trt:GetProfiles/>
  </s:Body>
</s:Envelope>`))

// getStreamURITemplate is the SOAP body for GetStreamUri (requires auth).
var getStreamURITemplate = template.Must(template.New("streamuri").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:trt="http://www.onvif.org/ver10/media/wsdl"
            xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Header>
    {{.AuthHeader}}
  </s:Header>
  <s:Body>
    <trt:GetStreamUri>
      <trt:StreamSetup>
        <tt:Stream>RTP-Unicast</tt:Stream>
        <tt:Transport>
          <tt:Protocol>RTSP</tt:Protocol>
        </tt:Transport>
      </trt:StreamSetup>
      <trt:ProfileToken>{{.ProfileToken}}</trt:ProfileToken>
    </trt:GetStreamUri>
  </s:Body>
</s:Envelope>`))

// ---------- WS-Security UsernameToken ----------

// buildAuthHeader creates a WS-Security UsernameToken XML fragment with
// PasswordDigest authentication: Base64(SHA1(nonce + created + password)).
func buildAuthHeader(username, password string) (string, error) {
	// Generate 16-byte random nonce
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	created := time.Now().UTC().Format(time.RFC3339)

	// Digest = Base64(SHA1(nonce + created + password))
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(password))
	digest := base64.StdEncoding.EncodeToString(h.Sum(nil))

	nonceB64 := base64.StdEncoding.EncodeToString(nonce)

	header := fmt.Sprintf(`<Security xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">
      <UsernameToken>
        <Username>%s</Username>
        <Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</Password>
        <Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</Nonce>
        <Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">%s</Created>
      </UsernameToken>
    </Security>`, xmlEscape(username), digest, nonceB64, created)

	return header, nil
}

// xmlEscape escapes special XML characters in a string for safe embedding
// in XML templates (prevents injection via username).
func xmlEscape(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// ---------- SOAP HTTP helper ----------

// soapCall sends a SOAP request to the given URL and returns the raw XML response body.
// The caller is responsible for parsing the response.
func soapCall(ctx context.Context, url string, body []byte, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating SOAP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SOAP request to %s: %w", url, err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	// Read body first, then check status — some cameras return useful fault info in non-200 responses.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("reading SOAP response from %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SOAP %s returned HTTP %d: %s", url, resp.StatusCode, truncate(string(respBody), 200))
	}

	return respBody, nil
}

// renderTemplate executes a text/template into a byte slice.
func renderTemplate(tmpl *template.Template, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering SOAP template: %w", err)
	}
	return buf.Bytes(), nil
}

// truncate limits a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ---------- XML response structures ----------

// probeMatchEnvelope represents a WS-Discovery ProbeMatch response.
type probeMatchEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		ProbeMatches struct {
			Matches []probeMatch `xml:"ProbeMatch"`
		} `xml:"ProbeMatches"`
	} `xml:"Body"`
}

type probeMatch struct {
	EndpointRef struct {
		Address string `xml:"Address"`
	} `xml:"EndpointReference"`
	Types  string `xml:"Types"`
	Scopes string `xml:"Scopes"`
	XAddrs string `xml:"XAddrs"`
}

// deviceInfoEnvelope represents a GetDeviceInformationResponse SOAP envelope.
type deviceInfoEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Response struct {
			Manufacturer    string `xml:"Manufacturer"`
			Model           string `xml:"Model"`
			FirmwareVersion string `xml:"FirmwareVersion"`
			SerialNumber    string `xml:"SerialNumber"`
			HardwareId      string `xml:"HardwareId"`
		} `xml:"GetDeviceInformationResponse"`
	} `xml:"Body"`
}

// getProfilesEnvelope represents a GetProfilesResponse SOAP envelope.
type getProfilesEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Response struct {
			Profiles []onvifProfile `xml:"Profiles"`
		} `xml:"GetProfilesResponse"`
	} `xml:"Body"`
}

type onvifProfile struct {
	Token string `xml:"token,attr"`
	Name  string `xml:"Name"`
	Video struct {
		Resolution struct {
			Width  int `xml:"Width"`
			Height int `xml:"Height"`
		} `xml:"Resolution"`
		Encoding string `xml:"Encoding"`
	} `xml:"VideoEncoderConfiguration"`
}

// getStreamURIEnvelope represents a GetStreamUriResponse SOAP envelope.
type getStreamURIEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Response struct {
			MediaURI struct {
				URI string `xml:"Uri"`
			} `xml:"MediaUri"`
		} `xml:"GetStreamUriResponse"`
	} `xml:"Body"`
}
