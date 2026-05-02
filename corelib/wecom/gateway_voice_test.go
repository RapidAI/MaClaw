package wecom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	cim "github.com/RapidAI/CodeClaw/corelib/im"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSendAudioUploadsAMRAndSendsVoiceMessage(t *testing.T) {
	var uploadSeen, sendSeen bool
	g := NewGateway(Config{CorpID: "corp", CorpSecret: "secret", AgentID: 7})
	g.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/cgi-bin/gettoken"):
			return jsonResponse(`{"errcode":0,"access_token":"token"}`), nil
		case strings.Contains(req.URL.Path, "/cgi-bin/media/upload"):
			uploadSeen = true
			if req.URL.Query().Get("type") != "voice" {
				t.Fatalf("upload type = %q, want voice", req.URL.Query().Get("type"))
			}
			if err := req.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse multipart: %v", err)
			}
			if req.MultipartForm.File["media"] == nil {
				t.Fatal("missing media file")
			}
			return jsonResponse(`{"errcode":0,"media_id":"media-1"}`), nil
		case strings.Contains(req.URL.Path, "/cgi-bin/message/send"):
			sendSeen = true
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode send body: %v", err)
			}
			if body["touser"] != "user-1" || body["msgtype"] != "voice" || body["agentid"].(float64) != 7 {
				t.Fatalf("send body = %#v", body)
			}
			voice, _ := body["voice"].(map[string]any)
			if voice["media_id"] != "media-1" {
				t.Fatalf("voice = %#v", voice)
			}
			return jsonResponse(`{"errcode":0}`), nil
		default:
			t.Fatalf("unexpected request URL: %s", req.URL.String())
			return nil, nil
		}
	})}

	if err := g.SendAudio(context.Background(), cim.UserTarget{PlatformUID: "user-1"}, []byte("#!AMR\nvoice"), 0); err != nil {
		t.Fatalf("SendAudio() error = %v", err)
	}
	if !uploadSeen || !sendSeen {
		t.Fatalf("uploadSeen=%v sendSeen=%v", uploadSeen, sendSeen)
	}
}

func TestSendAudioRejectsNonAMR(t *testing.T) {
	g := NewGateway(Config{CorpID: "corp", CorpSecret: "secret", AgentID: 7})
	if err := g.SendAudio(context.Background(), cim.UserTarget{PlatformUID: "user-1"}, []byte("ogg opus"), 0); err == nil {
		t.Fatal("SendAudio(non-AMR) error = nil, want error")
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
