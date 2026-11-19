package edgelink

import (
	"strings"
	"testing"
	"time"
)

func TestViewerTicketRejectsAChangedByte(t *testing.T) {
	ticket, err := SignViewer("desk-key", ViewerClaims{
		NodeID: "node-1", TenantID: "tenant-a", UserID: "alice",
		UserKey: "0123456789abcdef", Display: "desktop", Control: true,
		Exp: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil || !strings.HasPrefix(ticket, "v1.") || strings.Contains(ticket, "desk-key") {
		t.Fatalf("ticket %q err %v", ticket, err)
	}
	claims, err := VerifyViewer("desk-key", ticket, time.Now())
	if err != nil || claims.UserID != "alice" || !claims.Control || claims.UserKey != "0123456789abcdef" {
		t.Fatalf("claims %+v err %v", claims, err)
	}
	if _, err := VerifyViewer("other-key", ticket, time.Now()); err == nil {
		t.Fatal("another key accepted the ticket")
	}
	flipped := ticket[:len(ticket)-1] + "A"
	if _, err := VerifyViewer("desk-key", flipped, time.Now()); err == nil {
		t.Fatal("a changed ticket was accepted")
	}
	expired, err := SignViewer("desk-key", ViewerClaims{
		NodeID: "node-1", UserKey: "0123456789abcdef", Exp: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyViewer("desk-key", expired, time.Now()); err == nil {
		t.Fatal("an expired ticket was accepted")
	}
}
