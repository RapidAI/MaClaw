package desktopd

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

func TestHubLinkRoutesCarryTheLocalSrv(t *testing.T) {
	got := hubLinkRoutes("127.0.0.1:18081", "127.0.0.1:18080")
	if got[edgelink.TargetDesktopd] != "127.0.0.1:18081" || got[edgelink.TargetSrv] != "127.0.0.1:18080" || len(got) != 2 {
		t.Fatalf("routes = %v", got)
	}
	plain := hubLinkRoutes("127.0.0.1:18081", "  ")
	if _, ok := plain[edgelink.TargetSrv]; ok || plain[edgelink.TargetDesktopd] != "127.0.0.1:18081" {
		t.Fatalf("desktop-only routes = %v", plain)
	}
	fromURL := hubLinkRoutes("127.0.0.1:18081", "http://127.0.0.1:18080/v1")
	if fromURL[edgelink.TargetSrv] != "127.0.0.1:18080" {
		t.Fatalf("url routes = %v", fromURL)
	}
	if hubLinkDialAddr("://") != "" {
		t.Fatal("a broken url must not become a dial target")
	}
}
