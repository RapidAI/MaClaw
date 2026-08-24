package browser

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// cdpServerThatNeverAnswers accepts the websocket, optionally reads one
// command, and then either sits idle or hangs up.
func cdpServerThatNeverAnswers(t *testing.T, readFirst, hangUp bool) *CDPClient {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if readFirst {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
		if hangUp {
			return
		}
		time.Sleep(2 * time.Second)
	}))
	t.Cleanup(srv.Close)

	client, err := ConnectCDP("ws" + strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatalf("ConnectCDP: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestCDPSendMarksAWrittenCommandWithNoAnswerAsUnobserved(t *testing.T) {
	client := cdpServerThatNeverAnswers(t, true, false)

	_, err := client.Send("Page.navigate", map[string]interface{}{"url": "https://example.com/"}, 100*time.Millisecond)
	if err == nil {
		t.Fatal("an unanswered command must fail")
	}
	if !IsOutcomeUnobserved(err) {
		t.Fatalf("a command already on the wire was reported as a definite outcome: %v", err)
	}
	if !strings.Contains(err.Error(), "cdp timeout: Page.navigate") {
		t.Fatalf("message changed: %v", err)
	}
}

// The two sides of the write can end with the very same sentence. That is the
// whole reason the fact cannot be recovered from the text downstream.
func TestCDPSendSeparatesTheTwoConnectionClosedFacts(t *testing.T) {
	neverSent := cdpServerThatNeverAnswers(t, false, false)
	_ = neverSent.Close()
	_, beforeWrite := neverSent.Send("Page.navigate", map[string]interface{}{"url": "https://example.com/"}, time.Second)
	if beforeWrite == nil {
		t.Fatal("a closed client must refuse the command")
	}

	sentThenLost := cdpServerThatNeverAnswers(t, true, true)
	_, afterWrite := sentThenLost.Send("Page.navigate", map[string]interface{}{"url": "https://example.com/"}, 5*time.Second)
	if afterWrite == nil {
		t.Fatal("a hung-up connection must fail the pending command")
	}

	if beforeWrite.Error() != afterWrite.Error() {
		t.Skipf("the two closures no longer share a message (%q vs %q); this test only has a point while they do",
			beforeWrite, afterWrite)
	}
	if IsOutcomeUnobserved(beforeWrite) {
		t.Fatalf("a command that was never written must stay definite: %v", beforeWrite)
	}
	if !IsOutcomeUnobserved(afterWrite) {
		t.Fatalf("a command lost after it was written must be unobserved: %v", afterWrite)
	}
}
