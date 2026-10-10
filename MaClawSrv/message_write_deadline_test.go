package main

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestMessageResponseWriteBudgetCoversHubWait(t *testing.T) {
	if messageResponseWriteBudget < 30*time.Minute {
		t.Fatalf("budget %s is shorter than Hub's message wait", messageResponseWriteBudget)
	}
}

func TestAllowMessageResponseWriteOutlivesServerWriteTimeout(t *testing.T) {
	body := getAfterSilentHandler(t, true)
	if body != "{\"ok\":\"1\"}\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestServerWriteTimeoutDropsSilentHandler(t *testing.T) {
	_, err := getAfterSilentHandlerResult(t, false)
	if err == nil {
		t.Fatal("silent handler wrote a body after WriteTimeout")
	}
}

func TestAllowMessageResponseWriteExtendsAnExpiredDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler: withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(400 * time.Millisecond)
			allowMessageResponseWrite(w)
			writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
		})),
		ReadHeaderTimeout: time.Second,
		WriteTimeout:      150 * time.Millisecond,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(payload) != "{\"ok\":\"1\"}\n" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, payload)
	}
}

func getAfterSilentHandler(t *testing.T, extend bool) string {
	t.Helper()
	resp, err := getAfterSilentHandlerResult(t, extend)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func getAfterSilentHandlerResult(t *testing.T, extend bool) (*http.Response, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler: withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if extend {
				allowMessageResponseWrite(w)
			}
			time.Sleep(400 * time.Millisecond)
			writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
		})),
		ReadHeaderTimeout: time.Second,
		WriteTimeout:      150 * time.Millisecond,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	client := &http.Client{Timeout: 3 * time.Second}
	return client.Get("http://" + ln.Addr().String() + "/")
}
