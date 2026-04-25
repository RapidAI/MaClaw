package httpapi

import (
	"net"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
)

func clientIPFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}

	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		for _, part := range parts {
			if ip := strings.TrimSpace(part); ip != "" {
				return ip
			}
		}
	}

	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}

	return strings.TrimSpace(r.RemoteAddr)
}

func requestHAFQDNHint(r *http.Request) string {
	if r == nil {
		return ""
	}
	candidates := []string{
		strings.TrimSpace(r.Header.Get("X-Forwarded-Host")),
		strings.TrimSpace(r.Header.Get("X-Original-Host")),
		strings.TrimSpace(r.Host),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		parts := strings.Split(candidate, ",")
		for _, part := range parts {
			if fqdn := config.NormalizeHAFQDN(part); fqdn != "" {
				return fqdn
			}
		}
	}
	return ""
}
