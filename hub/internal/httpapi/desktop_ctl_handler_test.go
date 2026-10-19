package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopScreenshotHandlerPassesOptionsAndGeometry(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "desktop-api-token")
	jpeg := []byte("\xff\xd8\xff\xe0fake-jpeg")
	var got map[string]any
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/desktops/ensure" {
			_, _ = w.Write([]byte(`{"status":"running"}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("X-Desktop-Screen-Size", "1280x800")
		w.Header().Set("X-Desktop-Image-Size", "1024x640")
		w.Header().Set("X-Desktop-Shot-Region", "0,0,1280,800")
		w.Header().Set("X-Desktop-Shot-Scale", "0.800000")
		_, _ = w.Write(jpeg)
	}))
	t.Cleanup(docker.Close)
	handler := PostDesktopScreenshotHandler(desktopPoolFake(t, newDesktopSettingsMem(), docker.URL))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/screenshot", strings.NewReader(`{"tenant_id":"tenant-a","user_id":"alice","max_width":1024,"format":"jpeg"}`))
	req.Header.Set("Authorization", "Bearer desktop-api-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["mime"] != "image/jpeg" || out["scale"] != 0.8 || out["screen_width"] != float64(1280) || out["image_width"] != float64(1024) {
		t.Fatalf("out=%v", out)
	}
	if got["max_width"] != float64(1024) || got["format"] != "jpeg" || got["quality"] != float64(80) {
		t.Fatalf("docker saw %v", got)
	}
}

func TestDesktopCtlHandlerForwardsTheRequest(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "desktop-api-token")
	var got map[string]any
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/desktops/ctl" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"ok":true,"text":"你好"}`))
	}))
	t.Cleanup(docker.Close)
	handler := PostDesktopCtlHandler(desktopPoolFake(t, newDesktopSettingsMem(), docker.URL))
	call := func(token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/ctl", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if w := call("wrong-token-wrong-to", `{"user_id":"alice","request":{"op":"windows"}}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status=%d", w.Code)
	}
	w := call("desktop-api-token", `{"tenant_id":"tenant-a","user_id":"alice","request":{"op":"clipboard_get"}}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "你好") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	request, _ := got["request"].(map[string]any)
	if got["user_id"] != "alice" || request["op"] != "clipboard_get" {
		t.Fatalf("docker saw %v", got)
	}
	if w := call("desktop-api-token", `{"user_id":"alice","request":{"op":"shell"}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown op status=%d body=%s", w.Code, w.Body.String())
	}
}
