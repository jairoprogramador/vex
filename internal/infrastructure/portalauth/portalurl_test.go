package portalauth

import "testing"

func TestIsPortalURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		raw    string
		portal string
		want   bool
	}{
		{"mismo host", "https://vexja.com/cli", "https://vexja.com", true},
		{"mayúsculas", "HTTPS://VEXJA.COM/cli", "https://vexja.com", true},
		{"puerto por defecto explícito", "https://vexja.com:443/cli", "https://vexja.com", true},
		{"subdominio engañoso", "https://vexja.com.evil.com/cli", "https://vexja.com", false},
		{"otro host", "https://evil.com/cli", "https://vexja.com", false},
		{"userinfo", "https://vexja.com@evil.com/cli", "https://vexja.com", false},
		{"degradación a http", "http://vexja.com/cli", "https://vexja.com", false},
		{"otro puerto", "https://vexja.com:8443/cli", "https://vexja.com", false},
		{"homoglifo cirílico", "https://vеxja.com/cli", "https://vexja.com", false},
		{"sin esquema", "//vexja.com/cli", "https://vexja.com", false},
		{"javascript", "javascript:alert(1)", "https://vexja.com", false},
		{"file", "file:///etc/passwd", "https://vexja.com", false},
		{"vacía", "", "https://vexja.com", false},
		{"loopback http en desarrollo", "http://localhost:4200/cli", "http://localhost:4200", true},
		{"http no loopback aunque el portal sea http", "http://vexja.com/cli", "http://vexja.com", false},
		{"portal vacío falla cerrado", "https://vexja.com/cli", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsPortalURL(tt.raw, tt.portal); got != tt.want {
				t.Fatalf("IsPortalURL(%q, %q) = %v, want %v", tt.raw, tt.portal, got, tt.want)
			}
		})
	}
}
