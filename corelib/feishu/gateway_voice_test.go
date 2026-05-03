package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	cim "github.com/RapidAI/CodeClaw/corelib/im"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSendAudioUploadsOpusAndSendsAudioMessage(t *testing.T) {
	var uploadSeen, sendSeen bool
	g := NewGateway(Config{AppID: "app", AppSecret: "secret"})
	g.tenantToken = "tenant-token"
	g.tokenExpiry = nowPlusHour()
	g.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/im/v1/files"):
			uploadSeen = true
			if req.Method != http.MethodPost {
				t.Fatalf("upload method = %s", req.Method)
			}
			if err := req.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse multipart: %v", err)
			}
			if got := req.MultipartForm.Value["file_type"]; len(got) != 1 || got[0] != "opus" {
				t.Fatalf("file_type = %v, want opus", got)
			}
			return jsonResponse(`{"code":0,"data":{"file_key":"file-key-1"}}`), nil
		case strings.Contains(req.URL.Path, "/im/v1/messages"):
			sendSeen = true
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode send body: %v", err)
			}
			if body["receive_id"] != "open-id-1" || body["msg_type"] != "audio" {
				t.Fatalf("send body = %#v", body)
			}
			content, _ := body["content"].(string)
			if !strings.Contains(content, "file-key-1") || !strings.Contains(content, "duration") {
				t.Fatalf("content = %q", content)
			}
			return jsonResponse(`{"code":0}`), nil
		default:
			t.Fatalf("unexpected request URL: %s", req.URL.String())
			return nil, nil
		}
	})}

	if err := g.SendAudio(context.Background(), cim.UserTarget{PlatformUID: "open-id-1"}, []byte("OggS\x00\x02OpusHead"), 1200); err != nil {
		t.Fatalf("SendAudio() error = %v", err)
	}
	if !uploadSeen || !sendSeen {
		t.Fatalf("uploadSeen=%v sendSeen=%v", uploadSeen, sendSeen)
	}
}

func TestSendAudioRejectsNonOpus(t *testing.T) {
	g := NewGateway(Config{AppID: "app", AppSecret: "secret"})
	g.tenantToken = "tenant-token"
	g.tokenExpiry = nowPlusHour()
	if err := g.SendAudio(context.Background(), cim.UserTarget{PlatformUID: "open-id-1"}, []byte("not opus"), 1200); err == nil {
		t.Fatal("SendAudio(non-opus) error = nil, want error")
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func nowPlusHour() time.Time { return time.Now().Add(time.Hour) }
