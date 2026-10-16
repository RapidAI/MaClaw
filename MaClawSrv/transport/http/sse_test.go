package transporthttp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testFlusher struct{ flushed bool }

func (f *testFlusher) Flush() { f.flushed = true }

var _ http.Flusher = (*testFlusher)(nil)

func TestParseLastEventID(t *testing.T) {
	if sequence, opaque := ParseLastEventID("42"); sequence != 42 || opaque != "" {
		t.Fatalf("numeric id = %d/%q", sequence, opaque)
	}
	if sequence, opaque := ParseLastEventID("run:abc:event"); sequence != 0 || opaque != "run:abc:event" {
		t.Fatalf("opaque id = %d/%q", sequence, opaque)
	}
	if sequence, opaque := ParseLastEventID(""); sequence != 0 || opaque != "" {
		t.Fatalf("empty id = %d/%q", sequence, opaque)
	}
}

func TestWriteSSEEventSanitizesProtocolFields(t *testing.T) {
	var out bytes.Buffer
	flusher := &testFlusher{}
	if err := WriteSSEEvent(&out, flusher, "id\nforged", "run\r\nstarted", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if out.String() != "id: idforged\nevent: runstarted\ndata: {\"ok\":true}\n\n" || !flusher.flushed {
		t.Fatalf("SSE output = %q flushed=%v", out.String(), flusher.flushed)
	}
	if err := WriteSSEEvent(&out, flusher, "id", "", nil); err == nil {
		t.Fatal("empty event name must be rejected")
	}
}

func TestParsePageQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "/?limit=12&before=2026-09-03T00:00:00Z", nil)
	page, err := ParsePageQuery(req)
	if err != nil || page.Limit != 12 || page.Before.IsZero() {
		t.Fatalf("page = %#v err=%v", page, err)
	}
	bad := httptest.NewRequest("GET", "/?limit=nope", nil)
	if _, err := ParsePageQuery(bad); err == nil {
		t.Fatal("invalid limit must be rejected")
	}
	limit, err := ParsePageLimit(httptest.NewRequest("GET", "/?limit=2&before=bravo", nil))
	if err != nil || limit != 2 {
		t.Fatalf("ParsePageLimit name cursor = %d err=%v", limit, err)
	}
}
