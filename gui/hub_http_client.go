package main

import (
	"crypto/tls"
	"net/http"
	"time"
)

// hubHTTPClient is a shared HTTP client that skips TLS certificate verification.
// Hub servers commonly use self-signed certificates, so all HTTP calls to Hub
// (and HubCenter when it may also be HTTPS) should use this client.
var hubHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}
