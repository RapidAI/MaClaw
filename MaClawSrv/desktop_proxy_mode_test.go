package main

import (
	"context"
	"strings"
	"testing"
)

func TestDesktopProxyModeIsReadOnlyAction(t *testing.T) {
	if !desktopActionAllowed("proxy_mode") || !desktopPlanActionAllowed("proxy_mode") {
		t.Fatal("proxy_mode should be offered in both phases")
	}
	old := desktopRemoteProxyMode
	t.Cleanup(func() { desktopRemoteProxyMode = old })
	var gotUser string
	desktopRemoteProxyMode = func(_ context.Context, tenantID, userID string) (DesktopProxyMode, error) {
		gotUser = tenantID + "/" + userID
		return DesktopProxyMode{Mode: "smart", Locked: true}, nil
	}
	text, err := desktopProxyModeText(context.Background(), "t1", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if gotUser != "t1/alice" || !strings.Contains(text, "smart") || !strings.Contains(text, "locked") || !strings.Contains(text, "Only the person can change it") {
		t.Fatalf("text=%q user=%q", text, gotUser)
	}
}
