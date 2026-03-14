package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testMailer struct {
	called  bool
	to      []string
	subject string
	body    string
}

func (m *testMailer) Send(ctx context.Context, to []string, subject string, body string) error {
	_ = ctx
	m.called = true
	m.to = append([]string(nil), to...)
	m.subject = subject
	m.body = body
	return nil
}

func (m *testMailer) SendHubRegistrationConfirmation(ctx context.Context, to string, confirmURL string, hubName string) error {
	return nil
}

func TestAdminSendTestMailHandlerRequiresMailer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/mail/test", bytes.NewBufferString(`{"email":"admin@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	AdminSendTestMailHandler(nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAdminSendTestMailHandlerSendsMail(t *testing.T) {
	mailer := &testMailer{}
	payload, _ := json.Marshal(map[string]string{"email": "admin@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/mail/test", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	AdminSendTestMailHandler(mailer).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !mailer.called {
		t.Fatal("expected mailer to be called")
	}
	if len(mailer.to) != 1 || mailer.to[0] != "admin@example.com" {
		t.Fatalf("unexpected recipients: %#v", mailer.to)
	}
}
