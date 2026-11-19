package agent

import (
	"strings"
	"testing"
)

func TestForwardVisibleStreamTokenKeepsRejectMarker(t *testing.T) {
	var streamed strings.Builder
	streamed.WriteString("forecast")

	got, ok := forwardVisibleStreamToken("\x02", &streamed)
	if !ok || got != "\x02" || streamed.String() != "forecast" {
		t.Fatalf("reject = %q streamed=%q ok=%v", got, streamed.String(), ok)
	}
	got, ok = forwardVisibleStreamToken("\x02kept", &streamed)
	if !ok || got != "\x02kept" || streamed.String() != "forecast" {
		t.Fatalf("prefixed reject = %q streamed=%q ok=%v", got, streamed.String(), ok)
	}
	got, ok = forwardVisibleStreamToken("\x02ke\x01pt", &streamed)
	if !ok || got != "\x02kept" || streamed.String() != "forecast" {
		t.Fatalf("reject suffix = %q streamed=%q ok=%v", got, streamed.String(), ok)
	}
	got, ok = forwardVisibleStreamToken("\x01The\x01 user", &streamed)
	if !ok || got != "\x01The user" || streamed.String() != "forecast" {
		t.Fatalf("reasoning = %q streamed=%q ok=%v", got, streamed.String(), ok)
	}
	got, ok = forwardVisibleStreamToken("desktop\x01d", &streamed)
	if !ok || got != "desktopd" || streamed.String() != "forecastdesktopd" {
		t.Fatalf("content = %q streamed=%q ok=%v", got, streamed.String(), ok)
	}
	if _, ok := forwardVisibleStreamToken("\x01", &streamed); ok {
		t.Fatal("empty reasoning body was forwarded")
	}
	if streamed.String() != "forecastdesktopd" {
		t.Fatalf("empty reasoning wrote into the reply buffer: %q", streamed.String())
	}
}
