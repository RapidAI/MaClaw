package desktopd

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

func TestIntranetPublicBaseIgnoresAdvertiseHost(t *testing.T) {
	if !IntranetEnv("1") || !IntranetEnv("true") || IntranetEnv("") || IntranetEnv("0") {
		t.Fatal("IntranetEnv")
	}
	link := &HubLink{Self: "127.0.0.1:18081"}
	link.mu.Lock()
	link.creds = edgelink.Credentials{NodeID: "node-1"}
	link.mu.Unlock()

	got, err := (&Service{
		Intranet: true, AdvertiseHost: "203.0.113.8", PublicURL: "https://desk.example", HubLink: link,
	}).publicBase()
	if err != nil || got != "http://127.0.0.1:18081" {
		t.Fatalf("intranet base = %q %v", got, err)
	}
	t.Setenv("DESKTOPD_ADVERTISE_HOST", "203.0.113.8")
	if got := (&Service{Intranet: true}).advertiseHostOrEmpty(); got != "" {
		t.Fatalf("intranet advertise host = %q", got)
	}
	got, err = (&Service{AdvertiseHost: "desk.example"}).publicBase()
	if err != nil || got != "https://desk.example" {
		t.Fatalf("public base = %q %v", got, err)
	}
}
