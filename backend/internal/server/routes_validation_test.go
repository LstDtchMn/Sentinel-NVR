package server

import "testing"

func TestValidateWebhookURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{
			name:    "valid public https URL",
			rawURL:  "https://example.com/hook",
			wantErr: false,
		},
		{
			name:    "reject non-http scheme",
			rawURL:  "ftp://example.com/hook",
			wantErr: true,
		},
		{
			name:    "reject localhost host",
			rawURL:  "http://localhost/hook",
			wantErr: true,
		},
		{
			name:    "reject loopback IPv4",
			rawURL:  "http://127.0.0.1/hook",
			wantErr: true,
		},
		{
			name:    "reject private IPv4",
			rawURL:  "http://192.168.1.10/hook",
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

