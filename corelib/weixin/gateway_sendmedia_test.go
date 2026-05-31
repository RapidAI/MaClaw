package weixin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSendMediaVoiceSendsWechatCompatibleSilkItem(t *testing.T) {
	var uploadSeen atomic.Bool
	var sendSeen atomic.Bool
	handlerErrs := make(chan string, 8)
	failHTTP := func(w http.ResponseWriter, format string, args ...any) {
		handlerErrs <- fmt.Sprintf(format, args...)
		http.Error(w, "test handler failed", http.StatusInternalServerError)
	}

	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			failHTTP(w, "upload method = %s, want POST", r.Method)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			failHTTP(w, "read upload body: %v", err)
			return
		}
		if len(body) == 0 {
			failHTTP(w, "upload body is empty")
			return
		}
		uploadSeen.Store(true)
		w.Header().Set("X-Encrypted-Param", "download-param")
		w.WriteHeader(http.StatusOK)
	}))
	defer uploadServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/ilink/bot/getuploadurl":
			var req getUploadURLReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				failHTTP(w, "decode getuploadurl: %v", err)
				return
			}
			if req.MediaType != UploadMediaVoice {
				failHTTP(w, "media_type = %d, want voice", req.MediaType)
				return
			}
			if req.Rawsize == 0 || req.Filesize == 0 || req.Rawfilemd5 == "" || req.AESKey == "" {
				failHTTP(w, "incomplete upload request: %+v", req)
				return
			}
			_ = json.NewEncoder(w).Encode(getUploadURLResp{UploadFullURL: uploadServer.URL})
		case "/ilink/bot/sendmessage":
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				failHTTP(w, "read sendmessage body: %v", err)
				return
			}
			if string(raw) == "" {
				failHTTP(w, "sendmessage body is empty")
				return
			}
			var req sendMessageReq
			if err := json.Unmarshal(raw, &req); err != nil {
				failHTTP(w, "decode sendmessage: %v", err)
				return
			}
			if len(req.Msg.ItemList) != 1 || req.Msg.ItemList[0].Type != ItemTypeVoice || req.Msg.ItemList[0].VoiceItem == nil {
				failHTTP(w, "sendmessage item = %+v, want one voice item", req.Msg.ItemList)
				return
			}
			voice := req.Msg.ItemList[0].VoiceItem
			if voice.EncodeType != 4 || voice.SampleRate != weixinVoiceSampleRate || voice.Playtime != 1000 {
				failHTTP(w, "voice metadata encode=%d sample_rate=%d playtime=%d", voice.EncodeType, voice.SampleRate, voice.Playtime)
				return
			}
			if voice.BitsPerSample != 0 {
				failHTTP(w, "voice bits_per_sample = %d, want omitted/zero for SILK", voice.BitsPerSample)
				return
			}
			if voice.Media == nil || voice.Media.EncryptQueryParam != "download-param" || voice.Media.AESKey == "" || voice.Media.EncryptType != 0 {
				failHTTP(w, "voice media = %+v", voice.Media)
				return
			}
			if voice.Len != "" || voice.Size != "" || voice.VoiceMD5 != "" || voice.MD5 != "" || voice.Format != "" || voice.MimeType != "" {
				failHTTP(w, "unexpected outbound-only voice fields len=%q size=%q voice_md5=%q md5=%q format=%q mime=%q", voice.Len, voice.Size, voice.VoiceMD5, voice.MD5, voice.Format, voice.MimeType)
				return
			}
			sendSeen.Store(true)
			_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
		default:
			failHTTP(w, "unexpected API path %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	gw := NewGateway(Config{Token: "token", BaseURL: apiServer.URL}, func(IncomingMessage) {})
	err := gw.SendMedia(context.Background(), OutgoingMedia{
		ToUserID:     "user-1",
		ContextToken: "ctx",
		FileName:     "voice.wav",
		MediaType:    "voice",
		FileData:     makeTestWAV(16000, 2, 16000*2),
	})
	if err != nil {
		t.Fatalf("SendMedia voice: %v", err)
	}
	select {
	case handlerErr := <-handlerErrs:
		t.Fatal(handlerErr)
	default:
	}
	if !uploadSeen.Load() || !sendSeen.Load() {
		t.Fatalf("uploadSeen=%v sendSeen=%v", uploadSeen.Load(), sendSeen.Load())
	}
}
