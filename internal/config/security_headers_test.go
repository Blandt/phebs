package config

import "testing"

func TestSecurityHeadersEnabled(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name   string
		flag   *bool
		parsed string
		want   bool
	}{
		{name: "unset defaults to enabled", flag: nil, want: true},
		{name: "explicit true", flag: &yes, want: true},
		{name: "explicit false opts out", flag: &no, want: false},
		{name: "parsed true", parsed: "server: {security_headers: true}", want: true},
		{name: "parsed false", parsed: "server: {security_headers: false}", want: false},
		{name: "parsed absent defaults to enabled", parsed: "{}", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server Server
			if tt.parsed != "" {
				cfg, err := Parse([]byte(tt.parsed))
				if err != nil {
					t.Fatal(err)
				}
				server = cfg.Server
			} else {
				server.SecurityHeaders = tt.flag
			}
			if got := server.SecurityHeadersEnabled(); got != tt.want {
				t.Errorf("SecurityHeadersEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
