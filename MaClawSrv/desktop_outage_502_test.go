package main

import (
	"errors"
	"testing"
)

// desktopd restarting behind Hub's proxy answers 502 Bad Gateway for a
// second (papers 2026-11-12 06:38): that is an outage to wait out.
func TestDesktopOutage502IsWaitedOut(t *testing.T) {
	err := errors.New("docker service rejected the desktop request (HTTP 502): <html> <head><title>502 Bad Gateway</title></head>")
	if !desktopOutageFailure(err) {
		t.Fatal("502 not an outage")
	}
	if desktopOutageFailure(errors.New("docker service rejected the desktop request (HTTP 400): invalid desktop request: path is invalid")) {
		t.Fatal("400 taken as outage")
	}
}
