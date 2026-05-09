package lansenger

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// Run with: go test -v -run TestLiveAuth ./corelib/lansenger/ -lansenger-token "AppID:Secret:https://..."
// Or set env: LANSENGER_TOKEN=AppID:Secret:https://...

func getTestToken(t *testing.T) string {
	token := os.Getenv("LANSENGER_TOKEN")
	if token == "" {
		t.Skip("LANSENGER_TOKEN not set, skipping live test")
	}
	return token
}

func TestLiveParseToken(t *testing.T) {
	token := getTestToken(t)
	cfg, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}
	t.Logf("AppID:         %s", cfg.AppID)
	t.Logf("AppSecret:     %s...%s", cfg.AppSecret[:4], cfg.AppSecret[len(cfg.AppSecret)-4:])
	t.Logf("ApiGatewayURL: %s", cfg.ApiGatewayURL)

	if cfg.AppID == "" || cfg.AppSecret == "" || cfg.ApiGatewayURL == "" {
		t.Fatal("parsed config has empty fields")
	}
}

func TestLiveAuth(t *testing.T) {
	token := getTestToken(t)
	cfg, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	gw := NewGateway(cfg, nil)
	appToken, err := gw.getAppToken(context.Background())
	if err != nil {
		t.Fatalf("getAppToken failed: %v", err)
	}
	t.Logf("appToken: %s...%s (len=%d)", appToken[:8], appToken[len(appToken)-4:], len(appToken))

	// Second call should hit cache.
	appToken2, err := gw.getAppToken(context.Background())
	if err != nil {
		t.Fatalf("getAppToken (cached) failed: %v", err)
	}
	if appToken != appToken2 {
		t.Error("cached token mismatch")
	}
	t.Log("token cache OK")
}

func TestLiveWebSocketURL(t *testing.T) {
	token := getTestToken(t)
	cfg, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	gw := NewGateway(cfg, nil)
	wsURL, err := gw.getWebSocketURL(context.Background())
	if err != nil {
		t.Fatalf("getWebSocketURL failed: %v", err)
	}
	t.Logf("WebSocket URL: %s", wsURL)
}

func TestLiveConnect(t *testing.T) {
	token := getTestToken(t)
	cfg, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	received := make(chan IncomingMessage, 10)
	gw := NewGateway(cfg, func(msg IncomingMessage) {
		t.Logf("RECEIVED: from=%s type=%s text=%q", msg.FromUserID, msg.MessageType, msg.Text)
		received <- msg
	})

	statusCh := make(chan string, 10)
	gw.SetStatusCallback(func(status string) {
		t.Logf("STATUS: %s", status)
		statusCh <- status
	})

	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer gw.Stop()

	// Wait for connected status.
	timeout := time.After(15 * time.Second)
	for {
		select {
		case s := <-statusCh:
			if s == "connected" {
				t.Log("WebSocket connected successfully!")
				goto connected
			}
			if s == "error" {
				t.Fatal("got error status")
			}
		case <-timeout:
			t.Fatal("timeout waiting for connected status")
		}
	}

connected:
	t.Log("Listening for 10 seconds... send a message to the bot in Lansenger to test receive")
	listenTimeout := time.After(10 * time.Second)
	for {
		select {
		case msg := <-received:
			t.Logf("Got message: from=%s text=%q type=%s chat=%s",
				msg.FromUserID, msg.Text, msg.MessageType, msg.ChatType)
		case <-listenTimeout:
			t.Log("Listen period ended")
			return
		}
	}
}

func TestLiveSendText(t *testing.T) {
	token := getTestToken(t)
	cfg, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	targetUserID := os.Getenv("LANSENGER_TARGET_USER")
	if targetUserID == "" {
		t.Skip("LANSENGER_TARGET_USER not set, skipping send test")
	}

	gw := NewGateway(cfg, nil)

	// Just need auth, no WS connection needed for sending.
	err = gw.SendText(context.Background(), OutgoingText{
		ToUserID: targetUserID,
		Text:     fmt.Sprintf("🤖 MaClaw 蓝信网关测试消息 — %s", time.Now().Format("15:04:05")),
	})
	if err != nil {
		t.Fatalf("SendText failed: %v", err)
	}
	t.Log("SendText OK — check Lansenger for the message")
}

func TestLiveSendImage(t *testing.T) {
	token := getTestToken(t)
	cfg, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	targetUserID := os.Getenv("LANSENGER_TARGET_USER")
	if targetUserID == "" {
		t.Skip("LANSENGER_TARGET_USER not set")
	}

	gw := NewGateway(cfg, nil)

	// Create a small 1x1 red PNG image (67 bytes).
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, // 8-bit RGB
		0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, 0x54, // IDAT chunk
		0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00, // compressed data
		0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc, 0x33, // ...
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, // IEND chunk
		0xae, 0x42, 0x60, 0x82,
	}

	err = gw.SendMedia(context.Background(), OutgoingMedia{
		ToUserID:  targetUserID,
		FileData:  pngData,
		FileName:  "test_image.png",
		MediaType: "image",
	})
	if err != nil {
		t.Fatalf("SendMedia(image) failed: %v", err)
	}
	t.Log("SendMedia(image) OK — check Lansenger for the image")
}

func TestLiveSendFile(t *testing.T) {
	token := getTestToken(t)
	cfg, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	targetUserID := os.Getenv("LANSENGER_TARGET_USER")
	if targetUserID == "" {
		t.Skip("LANSENGER_TARGET_USER not set")
	}

	gw := NewGateway(cfg, nil)

	fileContent := []byte("Hello from MaClaw! This is a test file.\nTimestamp: " + time.Now().Format(time.RFC3339))

	err = gw.SendMedia(context.Background(), OutgoingMedia{
		ToUserID:  targetUserID,
		FileData:  fileContent,
		FileName:  "maclaw_test.txt",
		MediaType: "file",
	})
	if err != nil {
		t.Fatalf("SendMedia(file) failed: %v", err)
	}
	t.Log("SendMedia(file) OK — check Lansenger for the file")
}

func TestLiveReceiveMedia(t *testing.T) {
	token := getTestToken(t)
	cfg, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	received := make(chan IncomingMessage, 10)
	gw := NewGateway(cfg, func(msg IncomingMessage) {
		t.Logf("RECEIVED: from=%s type=%s chat=%s text=%q media=%s groupID=%s msgID=%s",
			msg.FromUserID, msg.MessageType, msg.ChatType, msg.Text,
			msg.MediaType, msg.GroupID, msg.MessageID)
		received <- msg
	})

	// Also log raw WS messages for debugging media format.
	origHandler := gw.handler
	gw.handler = func(msg IncomingMessage) {
		origHandler(msg)
	}

	statusCh := make(chan string, 10)
	gw.SetStatusCallback(func(status string) {
		t.Logf("STATUS: %s", status)
		statusCh <- status
	})

	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer gw.Stop()

	// Wait for connected.
	timeout := time.After(15 * time.Second)
	for {
		select {
		case s := <-statusCh:
			if s == "connected" {
				goto connected
			}
			if s == "error" {
				t.Fatal("got error status")
			}
		case <-timeout:
			t.Fatal("timeout waiting for connected")
		}
	}

connected:
	t.Log("Connected! Send images/files to the bot now. Listening for 60 seconds...")
	listenTimeout := time.After(60 * time.Second)
	count := 0
	for {
		select {
		case msg := <-received:
			count++
			t.Logf("[%d] Message: from=%s type=%s text=%q media=%s",
				count, msg.FromUserID, msg.MessageType, msg.Text, msg.MediaType)
		case <-listenTimeout:
			t.Logf("Listen ended. Received %d messages total.", count)
			return
		}
	}
}

func TestLiveSendImageDirect(t *testing.T) {
	token := getTestToken(t)
	cfg, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	targetUserID := os.Getenv("LANSENGER_TARGET_USER")
	if targetUserID == "" {
		t.Skip("LANSENGER_TARGET_USER not set")
	}

	gw := NewGateway(cfg, nil)
	appToken, err := gw.getAppToken(context.Background())
	if err != nil {
		t.Fatalf("getAppToken: %v", err)
	}

	pngData := createTestPNG()
	b64 := base64.StdEncoding.EncodeToString(pngData)

	// Try different msgType values to find what works.
	for _, msgType := range []string{"image", "file"} {
		t.Logf("Trying msgType=%q with base64 content (b64_len=%d)", msgType, len(b64))

		var msgData map[string]any
		if msgType == "image" {
			msgData = map[string]any{
				"image": map[string]any{
					"content":  b64,
					"filename": "test.png",
				},
			}
		} else {
			msgData = map[string]any{
				"file": map[string]any{
					"content":  b64,
					"filename": "test.txt",
				},
			}
		}

		apiURL := fmt.Sprintf("%s/v1/bot/messages/create?app_token=%s",
			strings.TrimRight(cfg.ApiGatewayURL, "/"), appToken)

		body, _ := json.Marshal(map[string]any{
			"userIdList": []string{targetUserID},
			"msgType":    msgType,
			"msgData":    msgData,
		})

		bodyStr := string(body)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		t.Logf("  Body preview: %s", bodyStr)

		resp, err := gw.client.Post(apiURL, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Logf("  HTTP error: %v", err)
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Logf("  Response %d: %s", resp.StatusCode, string(respBody))
	}
}

func createTestPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde,
		0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, 0x54,
		0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00,
		0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc, 0x33,
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44,
		0xae, 0x42, 0x60, 0x82,
	}
}

func TestLiveSendRealImage(t *testing.T) {
	token := getTestToken(t)
	cfg, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	targetUserID := os.Getenv("LANSENGER_TARGET_USER")
	if targetUserID == "" {
		t.Skip("LANSENGER_TARGET_USER not set")
	}

	gw := NewGateway(cfg, nil)

	// Generate a proper 100x100 red PNG using Go's image library.
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	red := color.RGBA{255, 0, 0, 255}
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	t.Logf("Generated PNG: %d bytes", buf.Len())

	err = gw.SendMedia(context.Background(), OutgoingMedia{
		ToUserID:  targetUserID,
		FileData:  buf.Bytes(),
		FileName:  "red_square.png",
		MediaType: "image",
	})
	if err != nil {
		t.Fatalf("SendMedia(image) failed: %v", err)
	}
	t.Log("SendMedia(real image) OK — check Lansenger for a red square image")
}
