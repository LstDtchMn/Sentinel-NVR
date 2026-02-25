package server

import "testing"

// TestValidateWebhookURL_AdditionalCases extends the existing webhook URL validation
// tests with additional SSRF edge cases.
func TestValidateWebhookURL_AdditionalCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{
			name:    "valid public http URL",
			rawURL:  "http://example.com/webhook",
			wantErr: false,
		},
		{
			name:    "reject IPv6 loopback",
			rawURL:  "http://[::1]/hook",
			wantErr: true,
		},
		{
			name:    "reject 10.x.x.x private range",
			rawURL:  "http://10.0.0.1/hook",
			wantErr: true,
		},
		{
			name:    "reject 172.16.x.x private range",
			rawURL:  "http://172.16.0.1/hook",
			wantErr: true,
		},
		{
			name:    "reject empty host",
			rawURL:  "http:///hook",
			wantErr: true,
		},
		{
			name:    "reject data URI scheme",
			rawURL:  "data:text/html,<script>alert(1)</script>",
			wantErr: true,
		},
		{
			name:    "reject file URI scheme",
			rawURL:  "file:///etc/passwd",
			wantErr: true,
		},
		{
			name:    "valid URL with port",
			rawURL:  "https://example.com:8443/webhook",
			wantErr: false,
		},
		{
			name:    "reject localhost with port",
			rawURL:  "http://localhost:8080/hook",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWebhookURL(tt.rawURL)
			if tt.wantErr && err == nil {
				t.Fatalf("validateWebhookURL(%q) expected error, got nil", tt.rawURL)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateWebhookURL(%q) unexpected error: %v", tt.rawURL, err)
			}
		})
	}
}
