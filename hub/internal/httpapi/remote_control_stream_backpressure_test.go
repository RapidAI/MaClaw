package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
)

func TestFederatedStreamRelayBoundsSlowPeerConcurrency(t *testing.T) {
	started := make(chan struct{}, remoteControlFederationStreamConcurrency*2)
	release := make(chan struct{})
	controllerHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}
		<-release
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer controllerHub.Close()
	defer close(release)
	target, _ := newRemoteControlFederationHandler(t, "user-b", "mac-c", "HOME-C")
	ctx := context.Background()
	grant := remotecontrol.Grant{
		ID: "rc_stream_backpressure", TenantID: "tenant-a", ControllerUserID: "user-a",
		ControllerMachineID: "mac-a", TargetUserID: "user-b", TargetAccount: "bob@example.com",
		ControllerHub: controllerHub.URL, TargetHub: "https://target.example",
		FederationSecret: "cd_0123456789abcdef0123456789abcdef", Permission: remotecontrol.PermView,
	}
	if err := target.svc.ImportFederatedInvite(ctx, grant); err != nil {
		t.Fatal(err)
	}
	if _, err := target.svc.Accept(ctx, "tenant-a", "user-b", "mac-c", grant.ID); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"grant_id":"` + grant.ID + `"}`)

	for range remoteControlFederationStreamConcurrency * 2 {
		target.relayControlledStream("mac-c", payload)
	}
	for range remoteControlFederationStreamConcurrency {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("stream relay did not fill the available worker slots")
		}
	}
	select {
	case <-started:
		t.Fatal("slow federation peer caused more than the configured concurrent relays")
	case <-time.After(100 * time.Millisecond):
	}
}
