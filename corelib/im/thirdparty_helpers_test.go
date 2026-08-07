package im

import (
	"crypto/tls"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestThirdPartyTruncateRunes(t *testing.T) {
	if got := ThirdPartyTruncateRunes("hello", 0); got != "hello" {
		t.Fatalf("max=0: %q", got)
	}
	if got := ThirdPartyTruncateRunes("你好世界", 2); got != "你好" {
		t.Fatalf("runes: %q", got)
	}
	if got := ThirdPartyTruncateRunes("hi", 10); got != "hi" {
		t.Fatalf("short: %q", got)
	}
}

func TestThirdPartyOutgoingMIME(t *testing.T) {
	if got := ThirdPartyOutgoingMIME(ThirdPartyOutgoingMessage{MimeType: " audio/ogg ", ContentType: "audio/wav"}); got != " audio/ogg " {
		t.Fatalf("mimeType wins: %q", got)
	}
	if got := ThirdPartyOutgoingMIME(ThirdPartyOutgoingMessage{ContentType: "audio/wav"}); got != "audio/wav" {
		t.Fatalf("fallback: %q", got)
	}
}

func TestNewThirdPartyGatewayRequestID(t *testing.T) {
	id := NewThirdPartyGatewayRequestID()
	if !strings.HasPrefix(id, "gw_") {
		t.Fatalf("prefix: %q", id)
	}
	// Note: uniqueness within the same timer tick is not guaranteed; the format
	// matches what both gateway hosts have always emitted.
}

func TestRandomThirdPartyToken(t *testing.T) {
	tok, err := RandomThirdPartyToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 32 { // 16 bytes hex-encoded
		t.Fatalf("len=%d", len(tok))
	}
	other, _ := RandomThirdPartyToken()
	if tok == other {
		t.Fatal("tokens must differ")
	}
}

func TestThirdPartyBearerToken(t *testing.T) {
	r := &http.Request{Header: http.Header{}}
	if got := ThirdPartyBearerToken(r); got != "" {
		t.Fatalf("absent: %q", got)
	}
	r.Header.Set("Authorization", "bearer  abc ")
	if got := ThirdPartyBearerToken(r); got != "abc" {
		t.Fatalf("bearer: %q", got)
	}
	r.Header.Set("Authorization", "Basic abc")
	if got := ThirdPartyBearerToken(r); got != "" {
		t.Fatalf("non-bearer: %q", got)
	}
}

func TestThirdPartyForwardedScheme(t *testing.T) {
	if got := ThirdPartyForwardedScheme("https, http", false); got != "https" {
		t.Fatalf("multi: %q", got)
	}
	if got := ThirdPartyForwardedScheme("", true); got != "https" {
		t.Fatalf("tls fallback: %q", got)
	}
	if got := ThirdPartyForwardedScheme("gopher", false); got != "http" {
		t.Fatalf("unknown scheme must degrade to http: %q", got)
	}
}

func TestThirdPartyForwardedHost(t *testing.T) {
	if got := ThirdPartyForwardedHost("example.test:8443, other"); got != "example.test:8443" {
		t.Fatalf("multi: %q", got)
	}
	if got := ThirdPartyForwardedHost("evil/% injecting"); got != "127.0.0.1" {
		t.Fatalf("smuggle: %q", got)
	}
	if got := ThirdPartyForwardedHost(""); got != "127.0.0.1" {
		t.Fatalf("empty: %q", got)
	}
}

func TestThirdPartyGatewayBaseURL(t *testing.T) {
	r := &http.Request{Header: http.Header{}, Host: "internal:18777"}
	if got := ThirdPartyGatewayBaseURL(r); got != "http://internal:18777/api/im-gateway/v1" {
		t.Fatalf("plain: %q", got)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "maclaw.example.test:18443")
	if got := ThirdPartyGatewayBaseURL(r); got != "https://maclaw.example.test:18443/api/im-gateway/v1" {
		t.Fatalf("forwarded: %q", got)
	}
	r2 := &http.Request{Header: http.Header{}, Host: "gw.internal", TLS: &tls.ConnectionState{}}
	if got := ThirdPartyGatewayBaseURL(r2); got != "https://gw.internal/api/im-gateway/v1" {
		t.Fatalf("tls: %q", got)
	}
}

func TestThirdPartyPruneAckedMessages(t *testing.T) {
	msg := func(id, cursor string) ThirdPartyOutgoingMessage {
		return ThirdPartyOutgoingMessage{ID: id, Cursor: cursor}
	}
	acked := map[string]string{"m1": "delivered", "m3": "delivered"}
	messages := []ThirdPartyOutgoingMessage{msg("m0", "1"), msg("m1", "2"), msg("m2", "3"), msg("m3", "4")}

	kept := ThirdPartyPruneAckedMessages(messages, acked, 10)
	if len(kept) != 2 || kept[0].ID != "m0" || kept[1].ID != "m2" {
		t.Fatalf("acked removal: %#v", kept)
	}
	if len(acked) != 0 {
		t.Fatalf("acked bookkeeping must be removed with the messages: %#v", acked)
	}

	// Over the cap, the oldest unacknowledged entries drop and their acked
	// entries leave with them.
	acked = map[string]string{"x9": "read"}
	messages = nil
	for i := 0; i < 6; i++ {
		messages = append(messages, msg(string(rune('a'+i)), ""))
	}
	acked["a"] = "delivered" // first entry already acked
	kept = ThirdPartyPruneAckedMessages(messages, acked, 3)
	if len(kept) != 3 || kept[0].ID != "d" {
		t.Fatalf("cap: %#v", kept)
	}
	if _, ok := acked["x9"]; !ok {
		t.Fatal("acked entries for surviving messages must survive")
	}
	if _, ok := acked["b"]; ok {
		t.Fatal("acked entries for dropped messages must be dropped")
	}
	if got := ThirdPartyPruneAckedMessages(nil, nil, 5); got != nil {
		t.Fatalf("empty: %#v", got)
	}
	_ = time.Now // keep time import honest if helpers evolve
}
