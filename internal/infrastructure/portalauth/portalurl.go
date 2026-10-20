package portalauth

import (
	"net"
	"net/url"
	"strings"
)

// IsPortalURL dice si raw apunta al mismo origen que el portal configurado en la compilación.
// Falla cerrado: un portal vacío o inválido nunca coincide con nada.
func IsPortalURL(raw, portal string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	p, err := url.Parse(portal)
	if err != nil || p.Host == "" {
		return false
	}
	if u.Scheme != p.Scheme || !(u.Scheme == "https" || (u.Scheme == "http" && isLoopback(p.Hostname()))) {
		return false
	}
	return hostPort(u) == hostPort(p)
}

func hostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
