package edgelink

import (
	"strings"
	"testing"
	"time"
)

func TestLocalTicketRejectsAViewerTicketAndAChangedByte(t *testing.T) {
	ticket, err := SignLocal("desk-key", LocalClaims{
		NodeID: "node-1", TenantID: "tenant-a", UserID: "alice", Action: "screenshot",
		Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ticket, "desk-key") {
		t.Fatal("the desktopd key was written into the ticket")
	}
	claims, err := VerifyLocal("desk-key", ticket, time.Now())
	if err != nil || claims.Action != "screenshot" || claims.UserID != "alice" {
		t.Fatal(err)
	}
	parts := strings.Split(ticket, ".")
	mac := []byte(parts[2])
	if mac[0] == 'A' {
		mac[0] = 'B'
	} else {
		mac[0] = 'A'
	}
	parts[2] = string(mac)
	if _, err := VerifyLocal("desk-key", strings.Join(parts, "."), time.Now()); err == nil {
		t.Fatal("a changed ticket was accepted")
	}
	viewer, err := SignViewer("desk-key", ViewerClaims{
		NodeID: "node-1", TenantID: "tenant-a", UserID: "alice", UserKey: "user-key",
		Display: "desktop", Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyLocal("desk-key", viewer, time.Now()); err == nil {
		t.Fatal("a viewer ticket opened a local data path")
	}
}
