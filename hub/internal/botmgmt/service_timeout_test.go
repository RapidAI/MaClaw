package botmgmt

import (
	"testing"
	"time"
)

func TestMessageClientWaitsAsLongAsTheDesktopTurn(t *testing.T) {
	svc := &Service{}
	messages := svc.httpClient("/api/v1/instances/inst/messages")
	if messages.Timeout != 30*time.Minute {
		t.Fatalf("message timeout = %s", messages.Timeout)
	}
	other := svc.httpClient("/api/v1/bots")
	if other.Timeout != 15*time.Second {
		t.Fatalf("non-message timeout = %s", other.Timeout)
	}
}
