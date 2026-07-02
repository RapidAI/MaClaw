package httpapi

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "modernc.org/sqlite"
)

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
		Store:   store,
		UserSvc: skillmarket.NewUserService(store, nil),
		AuthSvc: skillmarket.NewAuthService(store, nil, ""),
	}), store
}

func TestSkillMarketMachineLoginAcceptsPhoneAccount(t *testing.T) {
	handlers, store := newSkillMarketAuthTestHandlers(t)
	account := "phone:17000000000"
	body := `{"email":"` + account + `","machine_id":"machine-phone","viewer_token":"` + strings.Repeat("a", 32) + `"}`
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
	if user.Status != "verified" || user.VerifyMethod != "machine_login" {
		t.Fatalf("user verification = %s/%s, want verified/machine_login", user.Status, user.VerifyMethod)
	}
}
