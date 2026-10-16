package guiapp

import (
	"crypto/tls"
	"net/http"
	"time"
)

// hubHTTPClient is a shared HTTP client that skips TLS certificate verification.
// Hub servers commonly use self-signed certificates, so all HTTP calls to Hub
// (and HubCenter when it may also be HTTPS) should use this client.
// MaxIdleConnsPerHost is raised above the zero-value default (2): hub traffic
// is bursty and concurrent, and an undersized idle pool turns every call into
// a fresh TCP+TLS handshake.
var hubHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     90 * time.Second,
	},
}
