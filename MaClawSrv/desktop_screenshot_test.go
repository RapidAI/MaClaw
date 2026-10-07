package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func desktopTestPNG(t *testing.T, width, height int, noisy bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	rng := rand.New(rand.NewSource(1))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := color.RGBA{R: 30, G: 60, B: 90, A: 255}
			if noisy {
				c = color.RGBA{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256)), A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDesktopScreenshotAttachesTheImageForTheModel(t *testing.T) {
	shot := desktopTestPNG(t, 1440, 900, false)
	var gotUser, gotDisplay string
	previous := desktopRemoteScreenshot
	desktopRemoteScreenshot = func(_ context.Context, _, userID, display string) ([]byte, error) {
		gotUser, gotDisplay = userID, display
		return shot, nil
	}
	t.Cleanup(func() { desktopRemoteScreenshot = previous })
	out, err := desktopScreenshot(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20")
	if err != nil {
		t.Fatal(err)
	}
	text, images := agentruntime.ExtractModelImages(out)
	if gotUser != "alice" || gotDisplay != ":20" {
		t.Fatalf("user=%q display=%q", gotUser, gotDisplay)
	}
	if !strings.Contains(text, "1440x900") || !strings.Contains(text, "image/png") || strings.Contains(text, base64.StdEncoding.EncodeToString(shot)[:40]) {
		t.Fatalf("text=%q", text)
	}
	if len(images) != 1 || images[0].MIME != "image/png" {
		t.Fatalf("images=%d", len(images))
	}
	decoded, err := base64.StdEncoding.DecodeString(images[0].Base64)
	if err != nil || !bytes.Equal(decoded, shot) {
		t.Fatal("attached image is not the screenshot")
	}
}

func TestLargeDesktopScreenshotIsSentAsJPEG(t *testing.T) {
	shot := desktopTestPNG(t, 1000, 700, true)
	if base64.StdEncoding.EncodedLen(len(shot)) <= desktopScreenshotMaxBase64 {
		t.Skip("test image is not large enough")
	}
	previous := desktopRemoteScreenshot
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) { return shot, nil }
	t.Cleanup(func() { desktopRemoteScreenshot = previous })
	out, err := desktopScreenshot(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20")
	if err != nil {
		t.Fatal(err)
	}
	text, images := agentruntime.ExtractModelImages(out)
	if len(images) != 1 || images[0].MIME != "image/jpeg" || !strings.Contains(text, "1000x700") {
		t.Fatalf("text=%q images=%d", text, len(images))
	}
}

func TestDesktopScreenshotRejectsNonImages(t *testing.T) {
	previous := desktopRemoteScreenshot
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) { return []byte("<html>"), nil }
	t.Cleanup(func() { desktopRemoteScreenshot = previous })
	if _, err := desktopScreenshot(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20"); err == nil {
		t.Fatal("non-PNG screenshot was accepted")
	}
}

func TestLocalDesktopScreenshotUsesTheDisplay(t *testing.T) {
	shot := desktopTestPNG(t, 8, 6, false)
	var seen string
	previous := desktopLocalScreenshotRunner
	desktopLocalScreenshotRunner = func(display string) (string, error) {
		seen = display
		return base64.StdEncoding.EncodeToString(shot) + "\n", nil
	}
	t.Cleanup(func() { desktopLocalScreenshotRunner = previous })
	data, err := desktopLocalScreenshot(":20")
	if err != nil || !bytes.Equal(data, shot) || seen != ":20" {
		t.Fatalf("seen=%q err=%v", seen, err)
	}
	if _, err := desktopLocalScreenshot(":20;id"); err == nil {
		t.Fatal("invalid display was accepted")
	}
}

func TestHubClientFetchesTheScreenshot(t *testing.T) {
	shot := desktopTestPNG(t, 8, 6, false)
	var path, auth, user string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		user = in["user_id"]
		_ = json.NewEncoder(w).Encode(map[string]any{"mime": "image/png", "image_base64": base64.StdEncoding.EncodeToString(shot)})
	}))
	defer srv.Close()
	client := &desktopHubClient{baseURL: srv.URL, token: "hub-token", http: srv.Client()}
	data, err := client.Screenshot(context.Background(), "tenant", "alice", ":20")
	if err != nil || !bytes.Equal(data, shot) {
		t.Fatalf("err=%v", err)
	}
	if path != "/api/v1/desktop-services/screenshot" || auth != "Bearer hub-token" || user != "alice" {
		t.Fatalf("path=%q auth=%q user=%q", path, auth, user)
	}
}

func TestDesktopToolAdvertisesScreenshot(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9222/")
	tools, err := desktopRuntimeModule{}.Tools(context.Background(), agentruntime.TurnRequest{})
	if err != nil || len(tools) != 1 {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	raw, _ := json.Marshal(tools[0])
	if !strings.Contains(tools[0].Description, "action=screenshot") || !strings.Contains(string(raw), "screenshot") {
		t.Fatalf("tool does not offer screenshot: %s", raw)
	}
}
