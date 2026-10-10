package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var screenshotPNG = []byte("\x89PNG\r\n\x1a\nfake-image-data")

func TestDesktopScreenshotHandlerRelaysThePNG(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "desktop-api-token")
	var gotUser, gotDisplay, gotAuth string
	reply := screenshotPNG
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/desktops/screenshot" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		var in struct {
			UserID  string `json:"user_id"`
			Display string `json:"display"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		gotUser, gotDisplay = in.UserID, in.Display
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(reply)
	}))
	t.Cleanup(docker.Close)
	pool := desktopPoolFake(t, newDesktopSettingsMem(), docker.URL)
	handler := PostDesktopScreenshotHandler(pool)

	call := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/screenshot", strings.NewReader(`{"tenant_id":"tenant-a","user_id":"alice","display":":20"}`))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if w := call(""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token status=%d", w.Code)
	}
	if w := call("wrong-token-wrong-to"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status=%d", w.Code)
	}
	w := call("desktop-api-token")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		MIME  string `json:"mime"`
		Image string `json:"image_base64"`
		Bytes int    `json:"bytes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(out.Image)
	if err != nil || out.MIME != "image/png" || !bytes.Equal(decoded, screenshotPNG) || out.Bytes != len(screenshotPNG) {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if gotUser != "alice" || gotDisplay != ":20" || gotAuth != "Bearer tok" {
		t.Fatalf("docker saw user=%q display=%q auth=%q", gotUser, gotDisplay, gotAuth)
	}

	reply = []byte("<html>not an image</html>")
	if w := call("desktop-api-token"); w.Code != http.StatusBadGateway {
		t.Fatalf("non-PNG reply status=%d", w.Code)
	}
}

func TestDesktopScreenshotHandlerReturnsTheSavedPath(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "desktop-api-token")
	var gotName string
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		gotName = in.Name
		if in.Name != "" {
			w.Header().Set("X-Desktop-Saved-Path", "/home/desktop/Desktop/"+in.Name)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(screenshotPNG)
	}))
	t.Cleanup(docker.Close)
	handler := PostDesktopScreenshotHandler(desktopPoolFake(t, newDesktopSettingsMem(), docker.URL))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/screenshot", strings.NewReader(`{"tenant_id":"tenant-a","user_id":"alice","display":":20","name":"baidu_screenshot.png"}`))
	req.Header.Set("Authorization", "Bearer desktop-api-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || gotName != "baidu_screenshot.png" {
		t.Fatalf("status=%d name=%q body=%s", w.Code, gotName, w.Body.String())
	}
	var out struct {
		Saved string `json:"saved_path"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Saved != "/home/desktop/Desktop/baidu_screenshot.png" {
		t.Fatalf("saved=%q err=%v", out.Saved, err)
	}
}
