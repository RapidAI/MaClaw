package cloudworkspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestWorkspaceShareAcceptReadAndWrite(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "共享区", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	share, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionRead, now)
	if err != nil || share.Token == "" {
		t.Fatalf("share=%+v err=%v", share, err)
	}
	if _, _, err := st.AcceptWorkspaceShare(ctx, "t1", "u1", share.Token, "", now); err != ErrShareSelf {
		t.Fatalf("owner accept err=%v", err)
	}
	got, perm, err := st.AcceptWorkspaceShare(ctx, "t1", "u2", share.Token, "", now)
	if err != nil || got.ID != ws.ID || perm != SharePermissionRead {
		t.Fatalf("accept=%+v perm=%q err=%v", got, perm, err)
	}
	if _, err := requireActiveAccess(ctx, st.db, "t1", "u2", ws.ID, false); err != nil {
		t.Fatalf("read access: %v", err)
	}
	if _, err := requireActiveAccess(ctx, st.db, "t1", "u2", ws.ID, true); err != ErrShareReadOnly {
		t.Fatalf("write access err=%v", err)
	}
	if err := st.UpdateShareRecipientPermission(ctx, "t1", "u1", ws.ID, "u2", SharePermissionWrite); err != nil {
		t.Fatal(err)
	}
	if _, err := requireActiveAccess(ctx, st.db, "t1", "u2", ws.ID, true); err != nil {
		t.Fatalf("write after grant: %v", err)
	}
	if err := st.RemoveShareRecipient(ctx, "t1", "u1", ws.ID, "u2"); err != nil {
		t.Fatal(err)
	}
	if _, err := requireActiveAccess(ctx, st.db, "t1", "u2", ws.ID, false); err != ErrNotFound {
		t.Fatalf("removed recipient err=%v", err)
	}
}

func TestWorkspaceShareRevokeAndList(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "协作", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	share, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionWrite, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AcceptWorkspaceShare(ctx, "t1", "u2", share.Token, "", now); err != nil {
		t.Fatal(err)
	}
	listed, err := st.ListSharedWithUser(ctx, "t1", "u2")
	if err != nil || len(listed) != 1 || listed[0].ID != ws.ID || listed[0].SharePermission != SharePermissionWrite {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	if err := st.RevokeWorkspaceShare(ctx, "t1", "u1", ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AcceptWorkspaceShare(ctx, "t1", "u2", share.Token, "", now); err != ErrNotFound {
		t.Fatalf("revoked token err=%v", err)
	}
	listed, err = st.ListSharedWithUser(ctx, "t1", "u2")
	if err != nil || len(listed) != 0 {
		t.Fatalf("after revoke listed=%+v err=%v", listed, err)
	}
}

func TestWorkspaceShareAcceptCrossTenant(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "跨租户", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	share, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionRead, now)
	if err != nil {
		t.Fatal(err)
	}
	got, perm, err := st.AcceptWorkspaceShare(ctx, "t2", "u-other", share.Token, "", now)
	if err != nil || got.ID != ws.ID || perm != SharePermissionRead {
		t.Fatalf("cross-tenant accept=%+v perm=%q err=%v", got, perm, err)
	}
	if _, err := requireActiveAccess(ctx, st.db, "t2", "u-other", ws.ID, false); err != nil {
		t.Fatalf("cross-tenant read: %v", err)
	}
}

func TestWorkspaceShareClaimIssuesAccessToken(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "联邦", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	share, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionWrite, now)
	if err != nil {
		t.Fatal(err)
	}
	got, perm, access, err := st.ClaimWorkspaceShare(ctx, share.Token, ShareClaim{HomeHub: "https://other.hub", HomeUserID: "u-b", DisplayName: "b@x.com"}, now)
	if err != nil || got.ID != ws.ID || perm != SharePermissionWrite || !strings.HasPrefix(access, ShareAccessTokenPrefix) {
		t.Fatalf("claim=%+v perm=%q access=%q err=%v", got, perm, access, err)
	}
	rec, err := st.ResolveShareAccess(ctx, access, now)
	if err != nil || rec.WorkspaceID != ws.ID || rec.Permission != SharePermissionWrite {
		t.Fatalf("resolve=%+v err=%v", rec, err)
	}
	if _, err := requireActiveAccess(ctx, st.db, rec.WorkspaceTenantID, rec.RecipientKey, ws.ID, true); err != nil {
		t.Fatalf("federated write: %v", err)
	}
}

func TestUpdateShareRecipientWorksAcrossTenants(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "跨租户", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	share, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionRead, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AcceptWorkspaceShare(ctx, "t2", "u-other", share.Token, "", now); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateShareRecipientPermission(ctx, "t1", "u1", ws.ID, "u-other", SharePermissionWrite); err != nil {
		t.Fatalf("update cross-tenant recipient: %v", err)
	}
	if _, err := requireActiveAccess(ctx, st.db, "t2", "u-other", ws.ID, true); err != nil {
		t.Fatalf("write after cross-tenant grant: %v", err)
	}
	if err := st.RemoveShareRecipient(ctx, "t1", "u1", ws.ID, "u-other"); err != nil {
		t.Fatalf("remove cross-tenant recipient: %v", err)
	}
}

func TestClaimWorkspaceShareRejectsOwnerWithHomeHub(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "本人", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	share, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionRead, now)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = st.ClaimWorkspaceShare(ctx, share.Token, ShareClaim{
		HomeHub:      "https://hub.example.test",
		HomeUserID:   "u1",
		HomeTenantID: "t1",
		DisplayName:  "owner",
	}, now)
	if err != ErrShareSelf {
		t.Fatalf("owner claim err=%v", err)
	}
}

func TestAcceptKeepsExistingRecipientPermission(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "保留权限", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	share, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionRead, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, perm, err := st.AcceptWorkspaceShare(ctx, "t1", "u2", share.Token, "", now); err != nil || perm != SharePermissionRead {
		t.Fatalf("first accept perm=%q err=%v", perm, err)
	}
	if err := st.UpdateShareRecipientPermission(ctx, "t1", "u1", ws.ID, "u2", SharePermissionWrite); err != nil {
		t.Fatal(err)
	}
	got, perm, err := st.AcceptWorkspaceShare(ctx, "t1", "u2", share.Token, "", now)
	if err != nil || got.ID != ws.ID || perm != SharePermissionWrite {
		t.Fatalf("reaccept perm=%q err=%v", perm, err)
	}
	if _, err := requireActiveAccess(ctx, st.db, "t1", "u2", ws.ID, true); err != nil {
		t.Fatalf("kept write access: %v", err)
	}
}

func TestClaimWorkspaceShareRejectsUnsafeHomeHub(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "联邦", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	share, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionRead, now)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = st.ClaimWorkspaceShare(ctx, share.Token, ShareClaim{HomeHub: "javascript:alert(1)", HomeUserID: "u-b"}, now)
	if err != ErrInvalidInput {
		t.Fatalf("unsafe hub err=%v", err)
	}
}

func TestServiceShareRoundTrip(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	svc := &Service{
		Workspaces: st,
		Users: shareTestUsers{
			"u2": {ID: "u2", TenantID: "t1", Email: "u2@x.com"},
		},
		Now: func() time.Time { return time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC) },
	}
	ctx := context.Background()
	owner := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}
	guest := auth.MachinePrincipal{TenantID: "t1", UserID: "u2", MachineID: "m2"}
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "课题", Quota: 5, TenantMaxTotalBytes: 1 << 30}, svc.now())
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.CreateOrGetShare(ctx, owner, ws.ID, SharePermissionRead)
	if err != nil || view.Token == "" || view.ShareURL == "" {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if _, perm, err := svc.AcceptShare(ctx, guest, view.Token, ""); err != nil || perm != SharePermissionRead {
		t.Fatalf("accept perm=%q err=%v", perm, err)
	}
	got, err := svc.GetShare(ctx, owner, ws.ID)
	if err != nil || len(got.Recipients) != 1 || got.Recipients[0].Email != "u2@x.com" {
		t.Fatalf("recipients=%+v err=%v", got.Recipients, err)
	}
}

func TestWorkspaceSharePasswordAndExpiry(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "加密区", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	share, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionRead, now, ShareWriteOptions{Password: "secret", TTL: "7d"})
	if err != nil || share.PasswordHash == "" || share.ExpiresAt == "" {
		t.Fatalf("share=%+v err=%v", share, err)
	}
	if _, _, err := st.AcceptWorkspaceShare(ctx, "t1", "u2", share.Token, "", now); err != ErrSharePasswordRequired {
		t.Fatalf("missing password err=%v", err)
	}
	if _, _, err := st.AcceptWorkspaceShare(ctx, "t1", "u2", share.Token, "wrong", now); err != ErrSharePasswordInvalid {
		t.Fatalf("wrong password err=%v", err)
	}
	got, perm, err := st.AcceptWorkspaceShare(ctx, "t1", "u2", share.Token, "secret", now)
	if err != nil || got.ID != ws.ID || perm != SharePermissionRead {
		t.Fatalf("accept=%+v perm=%q err=%v", got, perm, err)
	}

	expired, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionRead, now, ShareWriteOptions{ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339)})
	if err == nil || expired != nil {
		t.Fatalf("past expiry should fail err=%v share=%+v", err, expired)
	}
	share, err = st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionRead, now, ShareWriteOptions{TTL: "1d"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AcceptWorkspaceShare(ctx, "t1", "u3", share.Token, "secret", now.Add(25*time.Hour)); err != ErrShareExpired {
		t.Fatalf("expired err=%v", err)
	}
	if _, _, _, err := st.ClaimWorkspaceShare(ctx, share.Token, ShareClaim{HomeHub: "https://other.hub", HomeUserID: "u-b", Password: "secret"}, now.Add(25*time.Hour)); err != ErrShareExpired {
		t.Fatalf("claim expired err=%v", err)
	}
	if _, _, _, err := st.ClaimWorkspaceShare(ctx, share.Token, ShareClaim{HomeHub: "https://other.hub", HomeUserID: "u-b", Password: "secret"}, now); err != nil {
		t.Fatalf("claim now err=%v", err)
	}
}

type shareTestUsers map[string]*store.User

func (m shareTestUsers) GetByID(ctx context.Context, id string) (*store.User, error) {
	_ = ctx
	return m[id], nil
}
