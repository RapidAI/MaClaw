package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

func TestDecodeThirdPartyMediaSupportsBase64(t *testing.T) {
	raw := []byte("image-bytes")
	data, name, mimeType, err := decodeThirdPartyMedia(thirdPartyMessagePayload{
		Type:     "image",
		FileName: "screen.png",
		MimeType: "image/png",
		Data:     base64.StdEncoding.EncodeToString(raw),
	})
	if err != nil {
		t.Fatalf("decode base64 media: %v", err)
	}
	if string(data) != string(raw) || name != "screen.png" || mimeType != "image/png" {
		t.Fatalf("unexpected base64 media: data=%q name=%q mime=%q", string(data), name, mimeType)
	}
}

func TestThirdPartyServerMediaRequestFromURLRejectsUnsafeInputs(t *testing.T) {
	if _, _, err := thirdPartyServerMediaRequestFromURL("ftp://example.test/secret.txt"); err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("expected file URL rejection, got %v", err)
	}
	if _, _, err := thirdPartyServerMediaRequestFromURL("https://third-party.example.test/file.pdf"); err == nil || !strings.Contains(err.Error(), "server media URL") {
		t.Fatalf("expected external URL rejection, got %v", err)
	}
	id, req, err := thirdPartyServerMediaRequestFromURL("http://127.0.0.1:18777/api/im-gateway/v1/media/media-123?mediaToken=token")
	if err != nil {
		t.Fatalf("parse server media URL: %v", err)
	}
	if id != "media-123" || req.URL.Query().Get("mediaToken") != "token" {
		t.Fatalf("unexpected server media request: id=%q url=%s", id, req.URL.String())
	}
}

func TestThirdPartyGatewayServerMediaUploadFlow(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	prepared, err := m.prepareMedia(coreim.ThirdPartyMediaPrepareRequest{
		ClientID:  "client-a",
		Type:      "file",
		FileName:  "report.pdf",
		MimeType:  "application/pdf",
		SizeBytes: 8,
	}, "http://127.0.0.1:18777/api/im-gateway/v1")
	if err != nil {
		t.Fatalf("prepareMedia: %v", err)
	}
	if prepared.Media.URL == "" || prepared.Upload.URL == "" || prepared.Upload.Method != http.MethodPut {
		t.Fatalf("unexpected prepared media: %#v", prepared)
	}

	uploadReq := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, strings.NewReader("pdf-body"))
	uploadReq.Header.Set("Content-Type", "application/pdf")
	if err := m.storeMediaUpload(uploadReq, prepared.Media.ID); err != nil {
		t.Fatalf("storeMediaUpload: %v", err)
	}
	downloadReq := httptest.NewRequest(http.MethodGet, prepared.Download.URL, nil)
	media, err := m.mediaForDownload(downloadReq, prepared.Media.ID)
	if err != nil {
		t.Fatalf("mediaForDownload: %v", err)
	}
	if string(media.Data) != "pdf-body" || media.MimeType != "application/pdf" || media.FileName != "report.pdf" {
		t.Fatalf("unexpected media: %#v", media)
	}
}

func TestThirdPartyGatewayRejectsUploadSizeMismatch(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	prepared, err := m.prepareMedia(coreim.ThirdPartyMediaPrepareRequest{
		ClientID:  "client-a",
		Type:      "file",
		FileName:  "report.pdf",
		MimeType:  "application/pdf",
		SizeBytes: 8,
	}, "http://127.0.0.1:18777/api/im-gateway/v1")
	if err != nil {
		t.Fatalf("prepareMedia: %v", err)
	}
	uploadReq := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, strings.NewReader("short"))
	if err := m.storeMediaUpload(uploadReq, prepared.Media.ID); err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("expected size mismatch error, got %v", err)
	}
}

func TestThirdPartyGatewayLocalMessageCanReadServerMediaIDOnly(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	prepared, err := m.prepareMedia(coreim.ThirdPartyMediaPrepareRequest{
		ClientID: "client-a",
		Type:     "file",
		FileName: "report.txt",
		MimeType: "text/plain",
	}, "http://127.0.0.1:18777/api/im-gateway/v1")
	if err != nil {
		t.Fatalf("prepareMedia: %v", err)
	}
	uploadReq := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, strings.NewReader("report-body"))
	if err := m.storeMediaUpload(uploadReq, prepared.Media.ID); err != nil {
		t.Fatalf("storeMediaUpload: %v", err)
	}

	data, name, mimeType, err := m.decodeThirdPartyMedia(thirdPartyMessagePayload{
		Type:        "file",
		Attachments: []coreim.ThirdPartyMediaReference{{ID: prepared.Media.ID, Type: "file"}},
	})
	if err != nil {
		t.Fatalf("decode server media by id: %v", err)
	}
	if string(data) != "report-body" || name != "report.txt" || mimeType != "text/plain" {
		t.Fatalf("unexpected server media decode: data=%q name=%q mime=%q", string(data), name, mimeType)
	}
}

func TestThirdPartyGatewayLocalMessageCanReadServerMediaURL(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	prepared, err := m.prepareMedia(coreim.ThirdPartyMediaPrepareRequest{
		ClientID: "client-a",
		Type:     "file",
		FileName: "report.txt",
		MimeType: "text/plain",
	}, "http://127.0.0.1:18777/api/im-gateway/v1")
	if err != nil {
		t.Fatalf("prepareMedia: %v", err)
	}
	uploadReq := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, strings.NewReader("report-body"))
	if err := m.storeMediaUpload(uploadReq, prepared.Media.ID); err != nil {
		t.Fatalf("storeMediaUpload: %v", err)
	}

	data, name, mimeType, err := m.decodeThirdPartyMedia(thirdPartyMessagePayload{
		Type:        "file",
		Attachments: []coreim.ThirdPartyMediaReference{{Type: "file", URL: prepared.Media.URL}},
	})
	if err != nil {
		t.Fatalf("decode server media by url: %v", err)
	}
	if string(data) != "report-body" || name != "report.txt" || mimeType != "text/plain" {
		t.Fatalf("unexpected server media decode: data=%q name=%q mime=%q", string(data), name, mimeType)
	}
}

func TestThirdPartyGatewayRejectsExternalMediaURL(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	_, _, _, err := m.decodeThirdPartyMedia(thirdPartyMessagePayload{
		Type:        "file",
		Attachments: []coreim.ThirdPartyMediaReference{{Type: "file", FileName: "report.pdf", URL: "https://third-party.example.test/report.pdf"}},
	})
	if err == nil || !strings.Contains(err.Error(), "server media URL") {
		t.Fatalf("expected external media URL rejection, got %v", err)
	}
}

func TestThirdPartyGatewayValidateIncomingRejectsExternalMediaURL(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	req := thirdPartyIncomingRequest{
		ClientID:       "client-a",
		EventID:        "evt-url",
		MessageID:      "msg-url",
		ConversationID: "room-a",
		User:           thirdPartyUserRef{ID: "user-a"},
		Message: thirdPartyMessagePayload{
			Type:        "file",
			Attachments: []coreim.ThirdPartyMediaReference{{Type: "file", FileName: "report.pdf", URL: "https://third-party.example.test/report.pdf"}},
		},
	}
	if err := normalizeIncomingRequest(&req); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := m.validateIncomingMediaReferences(&req); err == nil || !strings.Contains(err.Error(), "server media URL") {
		t.Fatalf("expected server media URL error, got %v", err)
	}
}

func TestThirdPartyGatewayValidateIncomingAcceptsServerMediaURL(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	prepared, err := m.prepareMedia(coreim.ThirdPartyMediaPrepareRequest{
		ClientID: "client-a",
		Type:     "file",
		FileName: "report.txt",
		MimeType: "text/plain",
	}, "http://127.0.0.1:18777/api/im-gateway/v1")
	if err != nil {
		t.Fatalf("prepareMedia: %v", err)
	}
	uploadReq := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, strings.NewReader("report-body"))
	if err := m.storeMediaUpload(uploadReq, prepared.Media.ID); err != nil {
		t.Fatalf("storeMediaUpload: %v", err)
	}
	req := thirdPartyIncomingRequest{
		ClientID:       "client-a",
		EventID:        "evt-url",
		MessageID:      "msg-url",
		ConversationID: "room-a",
		User:           thirdPartyUserRef{ID: "user-a"},
		Message: thirdPartyMessagePayload{
			Type:        "file",
			Attachments: []coreim.ThirdPartyMediaReference{{Type: "file", URL: prepared.Media.URL}},
		},
	}
	if err := normalizeIncomingRequest(&req); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := m.validateIncomingMediaReferences(&req); err != nil {
		t.Fatalf("validate incoming media: %v", err)
	}
	if req.Message.Attachments[0].ID != prepared.Media.ID || req.Message.Attachments[0].FileName != "report.txt" {
		t.Fatalf("expected server URL normalized to media metadata: %#v", req.Message.Attachments[0])
	}
}

func TestThirdPartyGatewayRequestBaseURLSanitizesForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/im-gateway/v1/media/upload-url", nil)
	req.Host = "gateway.example.test"
	req.Header.Set("X-Forwarded-Proto", "javascript")
	req.Header.Set("X-Forwarded-Host", "evil.example\\@bad")
	if got := thirdPartyGatewayRequestBaseURL(req); got != "http://127.0.0.1/api/im-gateway/v1" {
		t.Fatalf("bad forwarded headers should be sanitized, got %q", got)
	}

	req.Header.Set("X-Forwarded-Proto", "https, http")
	req.Header.Set("X-Forwarded-Host", "maclaw.example.test:18443, proxy.local")
	if got := thirdPartyGatewayRequestBaseURL(req); got != "https://maclaw.example.test:18443/api/im-gateway/v1" {
		t.Fatalf("safe forwarded headers should be preserved, got %q", got)
	}
}

func TestThirdPartyGatewayAckSuppressesRedelivery(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.enqueue("client-a", thirdPartyOutgoingMessage{ID: "out-1", Type: "text", Text: "one"})
	m.enqueue("client-a", thirdPartyOutgoingMessage{ID: "out-2", Type: "text", Text: "two"})

	req := thirdPartyAckRequest{ClientID: "client-a", MessageIDs: []string{"missing", "out-1"}, Status: "ok"}
	if err := coreim.NormalizeThirdPartyAckRequest(&req, thirdPartyMaxAckIDs); err != nil {
		t.Fatalf("NormalizeThirdPartyAckRequest: %v", err)
	}
	m.ack(req.ClientID, req)

	msgs, next, hasMore := m.messagesAfter("client-a", 0, 20)
	if len(msgs) != 1 || msgs[0].ID != "out-2" || next != 2 || hasMore {
		t.Fatalf("acked message should not be redelivered: msgs=%#v next=%d hasMore=%v", msgs, next, hasMore)
	}
	if _, ok := m.clients["client-a"].Acked["missing"]; ok {
		t.Fatalf("unknown ack id should not be stored")
	}
}
