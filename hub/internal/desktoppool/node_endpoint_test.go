package desktoppool

import (
	"net/url"
	"strings"
	"testing"
)

func TestHandTypedVirtualHostIsRejected(t *testing.T) {
	for _, raw := range []string{
		"http://desktopd.abc.node.invalid",
		"https://desktopd.abc.NODE.INVALID/v1",
		"http://node.invalid",
		"http://srv.abc.node.invalid.",
	} {
		if _, err := normalizeServer(Server{Name: "lab", BaseURL: raw, AccessToken: "tok"}, ""); err == nil {
			t.Fatalf("%s was accepted", raw)
		}
	}
	got, err := normalizeServer(Server{Name: "lab", BaseURL: "https://desk.example", AccessToken: "tok"}, "")
	if err != nil || got.BaseURL != "https://desk.example" {
		t.Fatalf("public url %+v %v", got, err)
	}
	if _, err := normalizeServer(Server{Name: "lab", BaseURL: "http://desktopd.abc.node.invalid", AccessToken: "tok", NodeID: "abc"}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestNodeEndpointRewritesOnlyANodeTunnel(t *testing.T) {
	raw := "http://desktop:gate@10.0.0.8:6080/v1/desktops/tunnel/alice/vnc/vnc.html?autoconnect=1"
	got := nodeEndpoint(Server{NodeID: "abc"}, raw)
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Hostname() != "desktopd.abc.node.invalid" || parsed.Scheme != "http" {
		t.Fatalf("rewritten origin %s", got)
	}
	user := parsed.User.Username()
	password, _ := parsed.User.Password()
	if user != "desktop" || password != "gate" {
		t.Fatalf("gate userinfo %q %q", user, password)
	}
	if !strings.Contains(parsed.EscapedPath(), "/v1/desktops/tunnel/alice/vnc/vnc.html") || parsed.Query().Get("autoconnect") != "1" {
		t.Fatalf("path or query changed: %s", got)
	}
	if nodeEndpoint(Server{}, raw) != raw {
		t.Fatal("a public service was rewritten")
	}
	plain := "http://10.0.0.8:6080/vnc.html"
	if nodeEndpoint(Server{NodeID: "abc"}, plain) != plain {
		t.Fatal("a non-tunnel page was rewritten")
	}
}
