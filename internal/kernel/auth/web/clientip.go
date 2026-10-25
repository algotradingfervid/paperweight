package authweb

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP is the last X-Forwarded-For entry across all its header lines when
// trustProxy is set (the one the proxy added) and it is an IP, else the host
// part of RemoteAddr.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if lines := r.Header.Values("X-Forwarded-For"); len(lines) > 0 {
			parts := strings.Split(lines[len(lines)-1], ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
