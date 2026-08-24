package proxyutil

import "testing"

func TestNormalizedProtocolAndHost(t *testing.T) {
	got := Config{Enabled: true, Protocol: "SOCKS5H", Host: " 127.0.0.1 ", Port: "1080"}.Normalized()
	if got.Protocol != "socks5" || got.Host != "127.0.0.1" || got.Port != "1080" {
		t.Fatalf("Normalized = %+v", got)
	}
	if u := got.ProxyURL(); u != "socks5://127.0.0.1:1080" {
		t.Fatalf("ProxyURL = %q", u)
	}
	off := EnabledNormalized(Config{Enabled: false, Host: "127.0.0.1", Port: "1080"})
	if off != (Config{}) {
		t.Fatalf("disabled config should zero, got %+v", off)
	}

	userOnly := Config{Protocol: "http", Host: "127.0.0.1", Port: "7890", Username: "alice"}.ProxyURL()
	if userOnly != "http://alice@127.0.0.1:7890" {
		t.Fatalf("username-only ProxyURL = %q", userOnly)
	}
	if err := ApplyToTransport(nil, Config{Enabled: true, Host: "127.0.0.1", Port: "7890"}); err == nil {
		t.Fatal("nil transport should fail")
	}
}
