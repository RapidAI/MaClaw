package weixin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizeQRLoginStatusAcceptsScannedSpellings(t *testing.T) {
	for _, raw := range []QRLoginStatus{"scaned", "scanned", " scan ", "SCANNED"} {
		if got := NormalizeQRLoginStatus(raw); got != QRLoginStatusScanned {
			t.Fatalf("NormalizeQRLoginStatus(%q) = %q, want %q", raw, got, QRLoginStatusScanned)
		}
	}
}

func TestNormalizeQRLoginStatusAcceptsConfirmAlias(t *testing.T) {
	for _, raw := range []QRLoginStatus{"confirm", "success", "connected", "done", "ok"} {
		if got := NormalizeQRLoginStatus(raw); got != QRLoginStatusConfirmed {
			t.Fatalf("NormalizeQRLoginStatus(%q) = %q, want %q", raw, got, QRLoginStatusConfirmed)
		}
	}
}

func TestDecodeQRStatusResponseAcceptsNestedCamelCaseFields(t *testing.T) {
	status, err := decodeQRStatusResponse([]byte(`{"data":{"state":"success","botToken":"bot-token","accountId":"bot-id","baseUrl":"https://example.com","userId":"user-id","msg":"ok"}}`))
	if err != nil {
		t.Fatalf("decodeQRStatusResponse() error = %v", err)
	}
	if NormalizeQRLoginStatus(status.Status) != QRLoginStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", status.Status)
	}
	if status.BotToken != "bot-token" || status.ILinkBotID != "bot-id" || status.BaseURL != "https://example.com" || status.ILinkUserID != "user-id" || status.Message != "ok" {
		t.Fatalf("decoded status = %+v", status)
	}
}

func TestDecodeQRStatusResponseAcceptsDeepNestedFields(t *testing.T) {
	status, err := decodeQRStatusResponse([]byte(`{"data":{"login":{"state":"confirm","bot":{"botToken":"bot-token","accountId":123,"baseUrl":"https://example.com"},"user":{"userId":456}},"message":"ok"}}`))
	if err != nil {
		t.Fatalf("decodeQRStatusResponse() error = %v", err)
	}
	if NormalizeQRLoginStatus(status.Status) != QRLoginStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", status.Status)
	}
	if status.BotToken != "bot-token" || status.ILinkBotID != "123" || status.BaseURL != "https://example.com" || status.ILinkUserID != "456" || status.Message != "ok" {
		t.Fatalf("decoded status = %+v", status)
	}
}

func TestDecodeQRStatusResponseAcceptsILinkBotTokenAlias(t *testing.T) {
	status, err := decodeQRStatusResponse([]byte(`{"data":{"state":"confirmed","ilinkBotToken":"bot-token","ilinkBotId":"bot-id"}}`))
	if err != nil {
		t.Fatalf("decodeQRStatusResponse() error = %v", err)
	}
	if status.BotToken != "bot-token" || status.ILinkBotID != "bot-id" {
		t.Fatalf("decoded status = %+v", status)
	}
}

func TestDecodeQRStatusResponseInfersConfirmedFromAccount(t *testing.T) {
	status, err := decodeQRStatusResponse([]byte(`{"result":{"accessToken":"bot-token","ilinkBotId":"bot-id"}}`))
	if err != nil {
		t.Fatalf("decodeQRStatusResponse() error = %v", err)
	}
	if NormalizeQRLoginStatus(status.Status) != QRLoginStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", status.Status)
	}
	if status.BotToken != "bot-token" || status.ILinkBotID != "bot-id" {
		t.Fatalf("decoded status = %+v", status)
	}
}

func TestQRLoginResultRequiresBotTokenWhenConfirmed(t *testing.T) {
	result, status, err := qrLoginResultFromStatus(qrStatusResponse{
		Status:     QRLoginStatusConfirmed,
		ILinkBotID: "bot-id",
		Message:    "confirmed without token",
	}, QRLoginStatusConfirmed)
	if err != nil {
		t.Fatalf("qrLoginResultFromStatus() error = %v", err)
	}
	if status != QRLoginStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", status)
	}
	if result == nil || result.Connected {
		t.Fatalf("confirmed QR without bot token must not be connected: %+v", result)
	}
	if result.AccountID != "bot-id" {
		t.Fatalf("account id should be preserved for diagnostics, got %q", result.AccountID)
	}
}

func TestQRStatusServerErrorIsNotTreatedAsWait(t *testing.T) {
	status, err := decodeQRStatusResponse([]byte(`{"ret":400,"errmsg":"bad qrcode"}`))
	if err != nil {
		t.Fatalf("decodeQRStatusResponse() error = %v", err)
	}
	if !qrStatusHasServerError(status) {
		t.Fatalf("status should be server error: %+v", status)
	}
}

func TestQRLoginRetryableErrorClassification(t *testing.T) {
	if IsQRLoginRetryableError(ErrQRCodeTokenEmpty) {
		t.Fatal("empty token error should not be retryable")
	}
	if IsQRLoginRetryableError(&qrLoginServerError{Op: "get_qrcode_status", Message: "bad qrcode"}) {
		t.Fatal("server error should not be retryable")
	}
	if !IsQRLoginRetryableError(context.DeadlineExceeded) {
		t.Fatal("transport/context errors should be retryable")
	}
}

func TestStartQRLoginRejectsIncompleteResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ret":0,"qrcode":"token"}`))
	}))
	t.Cleanup(server.Close)

	_, _, err := StartQRLogin(context.Background(), server.URL, DefaultBotType)
	if err == nil {
		t.Fatal("StartQRLogin() succeeded with incomplete response")
	}
}

func TestStartQRLoginRejectsServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ret":400,"errmsg":"bad bot type"}`))
	}))
	t.Cleanup(server.Close)

	_, _, err := StartQRLogin(context.Background(), server.URL, DefaultBotType)
	if err == nil {
		t.Fatal("StartQRLogin() succeeded with server error")
	}
}

func TestPollQRStatusInternalTimeoutKeepsWaiting(t *testing.T) {
	oldClient := qrHTTPClient
	oldTimeout := qrPollTimeout
	qrHTTPClient = &http.Client{Transport: blockingRoundTripper{}}
	qrPollTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		qrHTTPClient = oldClient
		qrPollTimeout = oldTimeout
	})

	result, status, err := PollQRStatus(context.Background(), "https://example.invalid", "token")
	if err != nil {
		t.Fatalf("PollQRStatus returned err on poll timeout: %v", err)
	}
	if status != QRLoginStatusWait {
		t.Fatalf("status = %q, want %q", status, QRLoginStatusWait)
	}
	if result == nil || result.Message != "timeout" {
		t.Fatalf("result = %+v, want timeout wait result", result)
	}
}

type blockingRoundTripper struct{}

func (blockingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}
