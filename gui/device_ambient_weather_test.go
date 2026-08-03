package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestFetchOpenMeteoWeatherIncludesCurrentTemperature(t *testing.T) {
	originalTransport := http.DefaultTransport
	http.DefaultTransport = ambientRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Host {
		case "geocoding-api.open-meteo.com":
			body = `{"results":[{"name":"上海","latitude":31.23,"longitude":121.47}]}`
		case "api.open-meteo.com":
			if !strings.Contains(req.URL.RawQuery, "current=temperature_2m%2Cweather_code") && !strings.Contains(req.URL.RawQuery, "current=temperature_2m,weather_code") {
				t.Fatalf("current temperature not requested: %s", req.URL.RawQuery)
			}
			body = `{"current":{"temperature_2m":30.6,"weather_code":2}}`
		default:
			t.Fatalf("unexpected weather host: %s", req.URL.Host)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	got, err := fetchOpenMeteoWeather(context.Background(), "上海")
	if err != nil {
		t.Fatalf("fetchOpenMeteoWeather: %v", err)
	}
	if got.Summary != "多云" || got.TemperatureC != 31 || got.Location != "上海" {
		t.Fatalf("unexpected current weather: %#v", got)
	}
}

type ambientRoundTripFunc func(*http.Request) (*http.Response, error)

func (f ambientRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestParseDeviceAmbientWeatherJSON(t *testing.T) {
	got, err := parseDeviceAmbientWeatherJSON("```json\n{\"summary\":\"多云\",\"temperatureC\":31,\"location\":\"上海\"}\n```")
	if err != nil {
		t.Fatalf("parseDeviceAmbientWeatherJSON: %v", err)
	}
	if got.Summary != "多云" || got.TemperatureC != 31 || got.Location != "上海" {
		t.Fatalf("unexpected weather: %#v", got)
	}
}

func TestNormalizeDeviceAmbientWeather(t *testing.T) {
	got, err := normalizeDeviceAmbientWeather(deviceAmbientWeather{Summary: "Partly Cloudy", TemperatureC: 22}, "北京")
	if err != nil {
		t.Fatalf("normalizeDeviceAmbientWeather: %v", err)
	}
	if got.Summary != "多云" || got.Location != "北京" {
		t.Fatalf("unexpected normalized weather: %#v", got)
	}
	if _, err := normalizeDeviceAmbientWeather(deviceAmbientWeather{Summary: "晴", TemperatureC: 99}, "北京"); err == nil {
		t.Fatal("out-of-range temperature should fail")
	}
}

func TestOpenMeteoWeatherSummary(t *testing.T) {
	tests := map[int]string{0: "晴", 2: "多云", 45: "雾", 61: "雨", 73: "雪", 81: "阵雨", 95: "雷雨"}
	for code, want := range tests {
		if got := openMeteoWeatherSummary(code); got != want {
			t.Errorf("code %d = %q, want %q", code, got, want)
		}
	}
}

func TestSendDeviceGatewayAmbientPayload(t *testing.T) {
	messageCh := make(chan []byte, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, payload, err := conn.ReadMessage()
		if err == nil {
			messageCh <- payload
		}
	}))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientWS, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer clientWS.Close()
	client := &RemoteHubClient{conn: clientWS, connected: true, machineID: "machine-1"}
	expiresAt := time.Unix(1785661200, 0)
	if err := client.SendDeviceGatewayAmbient("多云", 31, "上海", expiresAt); err != nil {
		t.Fatalf("SendDeviceGatewayAmbient: %v", err)
	}
	var payload []byte
	select {
	case payload = <-messageCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ambient payload")
	}
	var message struct {
		Type    string `json:"type"`
		Payload struct {
			ClientID       string `json:"clientId"`
			ConversationID string `json:"conversationId"`
			Reply          struct {
				ReplyType string `json:"reply_type"`
				Ambient   struct {
					Weather   deviceAmbientWeather `json:"weather"`
					Glyphs    map[string]string    `json:"glyphs"`
					ExpiresAt int64                `json:"expiresAt"`
				} `json:"ambient"`
			} `json:"reply"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	if message.Type != "im.device_gateway_reply" || message.Payload.ClientID != "*" || message.Payload.ConversationID != "system" {
		t.Fatalf("unexpected routing envelope: %#v", message)
	}
	if message.Payload.Reply.ReplyType != "ambient" || message.Payload.Reply.Ambient.Weather.Summary != "多云" || message.Payload.Reply.Ambient.ExpiresAt != expiresAt.UnixMilli() {
		t.Fatalf("unexpected ambient payload: %#v", message.Payload.Reply)
	}
	if len(message.Payload.Reply.Ambient.Glyphs) < 4 {
		t.Fatalf("ambient did not carry dynamic glyphs: %#v", message.Payload.Reply.Ambient.Glyphs)
	}
	for key, encoded := range message.Payload.Reply.Ambient.Glyphs {
		bitmap, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(bitmap) != deviceGlyphBytes {
			t.Fatalf("invalid glyph %s: %v len=%d", key, err, len(bitmap))
		}
	}
}
