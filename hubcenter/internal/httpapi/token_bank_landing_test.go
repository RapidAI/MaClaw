package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenBankGiftLandingShowsClaimPage(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "giver@example.test")
	env.seedCredits(t, user.ID, 10)
	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", token, map[string]any{"credits": 5})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
	}
	code, _ := decodeMap(t, rec)["code"].(string)
	if !validGiftLinkCode(code) {
		t.Fatalf("created code %q is not a landing code", code)
	}

	page := env.do(t, http.MethodGet, "/c/"+code, "", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("landing status = %d, body %s", page.Code, page.Body.String())
	}
	if ct := page.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	body := page.Body.String()
	if !strings.Contains(body, `data-code="`+code+`"`) {
		t.Fatalf("landing page missing the code attribute")
	}
	if !strings.Contains(body, "maclaw://credit/"+code) {
		t.Fatalf("landing page missing the desktop deep link")
	}
	if strings.Contains(body, "giver@example.test") {
		t.Fatalf("landing page leaked the sender address")
	}
	if !strings.Contains(body, "/api/v1/credits/share-links/") {
		t.Fatalf("landing page does not call the preview API")
	}
}

func TestTokenBankGiftLandingHidesUnknownAndUnsafeCodes(t *testing.T) {
	env := newTokenBankTestEnv(t)
	for _, path := range []string{"/c/OOOOOOOOOO", "/c/NOT-A-CODE"} {
		rec := env.do(t, http.MethodGet, path, "", nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "NOT-A-CODE") || strings.Contains(rec.Body.String(), "<script") {
			t.Fatalf("%s reflected the request into the page: %s", path, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/c/bad", nil)
	req.SetPathValue("code", "<script>alert(1)</script>")
	rec := httptest.NewRecorder()
	env.handlers.TokenBankGiftLanding(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("script code status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<script>alert") {
		t.Fatalf("unsafe code was reflected: %s", rec.Body.String())
	}
}
