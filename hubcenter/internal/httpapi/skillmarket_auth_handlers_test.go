package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "modernc.org/sqlite"
)

type testHubViewerMachineVerifier struct {
	principal *hubs.ViewerMachinePrincipal
	err       error
}

func (v testHubViewerMachineVerifier) AuthenticateViewerMachine(_ context.Context, _ string, _ string, _ string) (*hubs.ViewerMachinePrincipal, error) {
	return v.principal, v.err
}

func newSkillMarketAuthTestHandlers(t *testing.T) (*SkillMarketHandlers, *skillmarket.Store) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	return NewSkillMarketHandlers(SkillMarketConfig{
		Store:       store,
		UserSvc:     skillmarket.NewUserService(store, nil),
		AuthSvc:     skillmarket.NewAuthService(store, nil, ""),
		HubVerifier: testHubViewerMachineVerifier{principal: &hubs.ViewerMachinePrincipal{UserID: "hub-user", Email: "user@example.com", MachineID: "machine"}},
	}), store
}

func skillMarketTestHandlersForViewer(t *testing.T, principal *hubs.ViewerMachinePrincipal) (*SkillMarketHandlers, *skillmarket.Store) {
	t.Helper()
	handlers, store := newSkillMarketAuthTestHandlers(t)
	handlers.hubVerifier = testHubViewerMachineVerifier{principal: principal}
	return handlers, store
}

func TestSkillMarketMachineLoginAcceptsPhoneAccount(t *testing.T) {
	handlers, store := skillMarketTestHandlersForViewer(t, &hubs.ViewerMachinePrincipal{UserID: "hub-phone", Email: "phone:17000000000", MachineID: "machine-phone"})
	account := "phone:17000000000"
	body := `{"hub_id":"hub-1","email":"` + account + `","machine_id":"machine-phone","viewer_token":"` + strings.Repeat("a", 32) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/machine-login", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handlers.MachineLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("machine-login status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"session_token"`) || !strings.Contains(rec.Body.String(), `"email":"`+account+`"`) {
		t.Fatalf("unexpected body=%s", rec.Body.String())
	}
	user, err := store.GetUserByEmail(req.Context(), account)
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}
	if user.Status != "verified" || user.VerifyMethod != "session" {
		t.Fatalf("user verification = %s/%s, want verified/session", user.Status, user.VerifyMethod)
	}
}

func TestSkillMarketMachineLoginUsesUserIDAsPrincipalAndAccountAsContact(t *testing.T) {
	userID := "usr_authenticated"
	handlers, store := skillMarketTestHandlersForViewer(t, &hubs.ViewerMachinePrincipal{UserID: userID, Email: "verified@example.com", MachineID: "machine-userid"})
	account := "usr_sms_123"
	body := `{"hub_id":"hub-1","account":"` + account + `","user_id":"` + userID + `","email":"legacy@example.com","machine_id":"machine-userid","viewer_token":"` + strings.Repeat("b", 32) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/machine-login", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handlers.MachineLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("machine-login status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"email":"`+account+`"`) || !strings.Contains(rec.Body.String(), `"user_id":"`+userID+`"`) {
		t.Fatalf("machine-login should keep the supplied user ID and account contact, body=%s", rec.Body.String())
	}
	user, err := store.GetUserByEmail(req.Context(), account)
	if err != nil || user.ID != userID {
		t.Fatalf("GetUserByEmail(account) error = %v", err)
	}
	if _, err := store.GetUserByEmail(req.Context(), "legacy@example.com"); err == nil {
		t.Fatal("legacy email should not be used when account is present")
	}
}

func TestSkillMarketMachineLoginKeepsTheSameUserIDAcrossBoundContacts(t *testing.T) {
	userID := "usr_bound_contact"
	handlers, store := skillMarketTestHandlersForViewer(t, &hubs.ViewerMachinePrincipal{UserID: userID, Email: "user@example.com", MachineID: "machine-bound"})
	first := httptest.NewRequest(http.MethodPost, "/api/v1/auth/machine-login", strings.NewReader(`{"hub_id":"hub-1","user_id":"`+userID+`","account":"user@example.com","machine_id":"machine-bound","viewer_token":"`+strings.Repeat("c", 32)+`"}`))
	firstRec := httptest.NewRecorder()
	handlers.MachineLogin(firstRec, first)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first machine-login status=%d body=%s", firstRec.Code, firstRec.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/api/v1/auth/machine-login", strings.NewReader(`{"hub_id":"hub-1","user_id":"`+userID+`","account":"phone:17000000000","machine_id":"machine-bound","viewer_token":"`+strings.Repeat("d", 32)+`"}`))
	secondRec := httptest.NewRecorder()
	handlers.MachineLogin(secondRec, second)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second machine-login status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}

	var session struct {
		Token  string `json:"session_token"`
		UserID string `json:"user_id"`
		Email  string `json:"email"`
	}
	if err := json.Unmarshal(secondRec.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.UserID != userID || session.Email != "user@example.com" {
		t.Fatalf("second session=%+v, want stable user ID and stored contact", session)
	}
	stored, err := store.GetUserByID(second.Context(), userID)
	if err != nil || stored.Email != "user@example.com" {
		t.Fatalf("stored user=%+v err=%v", stored, err)
	}
	if _, err := store.GetUserByEmail(second.Context(), "phone:17000000000"); err == nil {
		t.Fatal("phone contact created a second market account")
	}
}

func TestSkillMarketMachineLoginRejectsMismatchedUserID(t *testing.T) {
	handlers, _ := skillMarketTestHandlersForViewer(t, &hubs.ViewerMachinePrincipal{UserID: "authenticated-user", Email: "user@example.com", MachineID: "machine-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/machine-login", strings.NewReader(`{"hub_id":"hub-1","user_id":"victim-user","account":"victim@example.com","machine_id":"machine-1","viewer_token":"valid-viewer-token"}`))
	rec := httptest.NewRecorder()

	handlers.MachineLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("machine-login status=%d body=%s, want unauthorized", rec.Code, rec.Body.String())
	}
}

func TestSkillMarketMachineLoginRejectsUnverifiedViewerToken(t *testing.T) {
	handlers, _ := newSkillMarketAuthTestHandlers(t)
	handlers.hubVerifier = testHubViewerMachineVerifier{err: errors.New("unknown viewer token")}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/machine-login", strings.NewReader(`{"hub_id":"hub-1","user_id":"victim-user","account":"victim@example.com","machine_id":"machine-1","viewer_token":"forged-token-that-is-long-enough"}`))
	rec := httptest.NewRecorder()

	handlers.MachineLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("machine-login status=%d body=%s, want unauthorized", rec.Code, rec.Body.String())
	}
}
