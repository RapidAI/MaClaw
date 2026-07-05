package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type fakeRankingUsageSummarizer struct {
	tokenRows    []store.UserTokenSummary
	durationRows []store.UserDurationSummary
}

func (f fakeRankingUsageSummarizer) SummarizeUserTokenUsage(_ context.Context, _ string, _, _ time.Time) ([]store.UserTokenSummary, error) {
	return f.tokenRows, nil
}

func (f fakeRankingUsageSummarizer) SummarizeUserDurations(_ context.Context, _ string, _, _, _ time.Time) ([]store.UserDurationSummary, error) {
	return f.durationRows, nil
}

type recordingRankingUsageSummarizer struct {
	tokenTenantID    string
	durationTenantID string
	tokenContext     string
	durationContext  string
	tokenRows        []store.UserTokenSummary
	durationRows     []store.UserDurationSummary
}

func (f *recordingRankingUsageSummarizer) SummarizeUserTokenUsage(ctx context.Context, tenantID string, _, _ time.Time) ([]store.UserTokenSummary, error) {
	f.tokenTenantID = tenantID
	f.tokenContext = store.TenantIDFromContext(ctx)
	return f.tokenRows, nil
}

func (f *recordingRankingUsageSummarizer) SummarizeUserDurations(ctx context.Context, tenantID string, _, _, _ time.Time) ([]store.UserDurationSummary, error) {
	f.durationTenantID = tenantID
	f.durationContext = store.TenantIDFromContext(ctx)
	return f.durationRows, nil
}

func TestUserRankingEmailFilterRejectsUIDs(t *testing.T) {
	for _, tc := range []struct {
		email string
		want  bool
	}{
		{email: "user@example.com", want: true},
		{email: " User@Example.com ", want: true},
		{email: "u_1774182684297100200", want: false},
		{email: "", want: false},
	} {
		if got := isUserRankingEmail(tc.email); got != tc.want {
			t.Fatalf("isUserRankingEmail(%q) = %v, want %v", tc.email, got, tc.want)
		}
	}
}

func TestUserRankingAccountFilterAllowsPhoneAccounts(t *testing.T) {
	for _, tc := range []struct {
		account string
		want    bool
	}{
		{account: "user@example.com", want: true},
		{account: "phone:19900001111", want: true},
		{account: " PHONE:19900001111 ", want: true},
		{account: "phone:12345", want: false},
		{account: "phone:19900 001111", want: false},
		{account: "u_1774182684297100200", want: false},
		{account: "", want: false},
	} {
		if got := isUserRankingAccount(tc.account); got != tc.want {
			t.Fatalf("isUserRankingAccount(%q) = %v, want %v", tc.account, got, tc.want)
		}
	}
}

func TestMaskEmailMasksPhoneAccounts(t *testing.T) {
	if got := maskEmail("phone:19900001111"); got != "phone:199****1111" {
		t.Fatalf("masked phone = %q, want phone:199****1111", got)
	}
}

func TestPublicUserRankingsHandlerIncludesPhoneAccounts(t *testing.T) {
	sessions := fakeRankingUsageSummarizer{
		tokenRows: []store.UserTokenSummary{
			{UserEmail: "phone:19900001111", Usage: store.UserTokenUsage{InputTokens: 20, OutputTokens: 5}},
			{UserEmail: "u_1774182684297100200", Usage: store.UserTokenUsage{InputTokens: 99}},
		},
		durationRows: []store.UserDurationSummary{
			{UserEmail: "phone:19900001111", DurationSeconds: 240, OnlineSeconds: 300},
			{UserEmail: "u_1774182684297100200", DurationSeconds: 999},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/public/user-rankings?period=monthly&dimension=tokens", nil)
	rec := httptest.NewRecorder()

	GetPublicUserRankingsHandler(sessions, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp publicUserRankingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 1 || len(resp.Rows) != 1 {
		t.Fatalf("public rows = total:%d rows:%#v, want one phone account only", resp.Total, resp.Rows)
	}
	row := resp.Rows[0]
	if row.MaskedEmail != "phone:199****1111" || row.TotalTokens != 25 || row.DurationSeconds != 240 || row.OnlineSeconds != 300 {
		t.Fatalf("unexpected public row: %#v", row)
	}
}

func TestPublicUserRankingsHandlerMergesBoundEmailAndPhoneByUserID(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	ctx := context.Background()
	user, err := identity.ManualBindForTenant(ctx, store.DefaultTenantID, "phone:17090134628")
	if err != nil {
		t.Fatalf("ManualBindForTenant: %v", err)
	}
	now := time.Now().UTC()
	if err := identity.UsersRepo().UpsertIdentity(ctx, &store.UserIdentity{
		ID:        user.ID + "_email",
		TenantID:  store.DefaultTenantID,
		UserID:    user.ID,
		Type:      "email",
		Value:     "ztest@163.com",
		Verified:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	sessions := fakeRankingUsageSummarizer{
		tokenRows: []store.UserTokenSummary{
			{UserEmail: "phone:17090134628", Usage: store.UserTokenUsage{InputTokens: 20, OutputTokens: 5}},
			{UserEmail: "ztest@163.com", Usage: store.UserTokenUsage{InputTokens: 100, OutputTokens: 10}},
		},
		durationRows: []store.UserDurationSummary{
			{UserEmail: "phone:17090134628", DurationSeconds: 240, OnlineSeconds: 300},
			{UserEmail: "ztest@163.com", DurationSeconds: 60, OnlineSeconds: 80},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/public/user-rankings?period=monthly&dimension=tokens", nil)
	rec := httptest.NewRecorder()

	GetPublicUserRankingsHandler(sessions, identity.UsersRepo()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp publicUserRankingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 1 || len(resp.Rows) != 1 {
		t.Fatalf("public rows = total:%d rows:%#v, want one bound account", resp.Total, resp.Rows)
	}
	row := resp.Rows[0]
	if row.MaskedEmail != "z***t@163.com" || row.TotalTokens != 135 || row.DurationSeconds != 300 || row.OnlineSeconds != 380 {
		t.Fatalf("unexpected merged public row: %#v", row)
	}
}

func TestMyRankingHandlerIncludesPhoneViewer(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	user, err := identity.ManualBindForTenant(context.Background(), store.DefaultTenantID, "phone:19900001111")
	if err != nil {
		t.Fatalf("ManualBindForTenant: %v", err)
	}
	viewerToken, err := identity.IssueViewerTokenForUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("IssueViewerTokenForUser: %v", err)
	}
	sessions := fakeRankingUsageSummarizer{
		tokenRows: []store.UserTokenSummary{
			{UserEmail: "other@example.com", Usage: store.UserTokenUsage{InputTokens: 100}},
			{UserEmail: "phone:19900001111", Usage: store.UserTokenUsage{InputTokens: 20, OutputTokens: 5}},
			{UserEmail: "u_1774182684297100200", Usage: store.UserTokenUsage{InputTokens: 99}},
		},
		durationRows: []store.UserDurationSummary{
			{UserEmail: "phone:19900001111", DurationSeconds: 240},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/my-ranking", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()

	GetMyRankingHandler(identity, sessions).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp myRankingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TotalUsers != 2 || resp.TotalTokens != 25 || resp.DurationSeconds != 240 || resp.TokenRank != 2 || resp.DurationRank != 1 {
		t.Fatalf("unexpected my ranking response: %#v", resp)
	}
}

func TestMyRankingHandlerRanksWithinViewerTenant(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	user, err := identity.ManualBindForTenant(context.Background(), "tenant-acme", "alice@example.com")
	if err != nil {
		t.Fatalf("ManualBindForTenant: %v", err)
	}
	viewerToken, err := identity.IssueViewerTokenForUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("IssueViewerTokenForUser: %v", err)
	}
	sessions := &recordingRankingUsageSummarizer{
		tokenRows: []store.UserTokenSummary{
			{UserEmail: "bob@example.com", Usage: store.UserTokenUsage{InputTokens: 100}},
			{UserEmail: "alice@example.com", Usage: store.UserTokenUsage{InputTokens: 40}},
		},
		durationRows: []store.UserDurationSummary{
			{UserEmail: "alice@example.com", DurationSeconds: 600},
			{UserEmail: "bob@example.com", DurationSeconds: 60},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/my-ranking?tenant_id=other-tenant", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()

	GetMyRankingHandler(identity, sessions).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if sessions.tokenTenantID != "tenant-acme" || sessions.durationTenantID != "tenant-acme" {
		t.Fatalf("ranking tenant = token:%q duration:%q, want tenant-acme", sessions.tokenTenantID, sessions.durationTenantID)
	}
	if sessions.tokenContext != "tenant-acme" || sessions.durationContext != "tenant-acme" {
		t.Fatalf("ranking context tenant = token:%q duration:%q, want tenant-acme", sessions.tokenContext, sessions.durationContext)
	}
	var resp myRankingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TokenRank != 2 || resp.DurationRank != 1 || resp.TotalUsers != 2 {
		t.Fatalf("unexpected tenant-scoped ranking response: %#v", resp)
	}
}

func TestMyRankingHandlerUsesOnlineSecondsTieBreaker(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	user, err := identity.ManualBindForTenant(context.Background(), store.DefaultTenantID, "alice@example.com")
	if err != nil {
		t.Fatalf("ManualBindForTenant: %v", err)
	}
	viewerToken, err := identity.IssueViewerTokenForUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("IssueViewerTokenForUser: %v", err)
	}
	sessions := fakeRankingUsageSummarizer{
		tokenRows: []store.UserTokenSummary{
			{UserEmail: "alice@example.com", Usage: store.UserTokenUsage{InputTokens: 100}},
			{UserEmail: "bob@example.com", Usage: store.UserTokenUsage{InputTokens: 100}},
		},
		durationRows: []store.UserDurationSummary{
			{UserEmail: "alice@example.com", DurationSeconds: 300, OnlineSeconds: 600},
			{UserEmail: "bob@example.com", DurationSeconds: 300, OnlineSeconds: 60},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/my-ranking", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()

	GetMyRankingHandler(identity, sessions).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp myRankingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TokenRank != 1 || resp.DurationRank != 1 {
		t.Fatalf("unexpected tie-break ranking response: %#v", resp)
	}
}

func TestSortUserRankingRowsByDuration(t *testing.T) {
	rows := []userRankingRow{
		{UserEmail: "fast@example.com", TotalTokens: 100, DurationSeconds: 60},
		{UserEmail: "slow@example.com", TotalTokens: 10, DurationSeconds: 3600},
	}
	assignUserRankingRanks(rows)
	sortUserRankingRows(rows, "duration")

	if rows[0].UserEmail != "slow@example.com" || rows[0].DurationRank != 1 || rows[0].TokenRank != 2 {
		t.Fatalf("unexpected first row: %#v", rows[0])
	}
	if rows[1].UserEmail != "fast@example.com" || rows[1].DurationRank != 2 || rows[1].TokenRank != 1 {
		t.Fatalf("unexpected second row: %#v", rows[1])
	}
}

func TestAssignUserRankingRanksUsesUserIDKey(t *testing.T) {
	rows := []userRankingRow{
		{UserID: "u_one", UserEmail: "shared@example.com", TotalTokens: 100, DurationSeconds: 60},
		{UserID: "u_two", UserEmail: "shared@example.com", TotalTokens: 10, DurationSeconds: 600},
	}
	assignUserRankingRanks(rows)

	if rows[0].TokenRank != 1 || rows[0].DurationRank != 2 {
		t.Fatalf("first row ranks = token:%d duration:%d", rows[0].TokenRank, rows[0].DurationRank)
	}
	if rows[1].TokenRank != 2 || rows[1].DurationRank != 1 {
		t.Fatalf("second row ranks = token:%d duration:%d", rows[1].TokenRank, rows[1].DurationRank)
	}
}

func TestSortUserRankingRowsUsesUserIDTieBreaker(t *testing.T) {
	rows := []userRankingRow{
		{UserID: "u_two", UserEmail: "shared@example.com", TotalTokens: 100, DurationSeconds: 60},
		{UserID: "u_one", UserEmail: "shared@example.com", TotalTokens: 100, DurationSeconds: 60},
	}
	sortUserRankingRows(rows, "tokens")

	if rows[0].UserID != "u_one" || rows[1].UserID != "u_two" {
		t.Fatalf("sorted rows = %#v, want stable user_id tie-breaker", rows)
	}
}

func TestUserRankingEmailFilterRejectsMalformedEmails(t *testing.T) {
	for _, email := range []string{"foo@", "@example.com", "foo @example.com", "foo@@example.com"} {
		if isUserRankingEmail(email) {
			t.Fatalf("isUserRankingEmail(%q) = true, want false", email)
		}
	}
}
