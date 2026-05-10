package skillmarket

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthServiceSignedSessionValidatesWithoutLocalSessionRow(t *testing.T) {
	ctx := context.Background()
	issuerStore := newTestStore(t)
	validatorStore := newTestStore(t)
	user := createTestUser(t, issuerStore, "Uploader@Example.com", 0)
	user.Status = "verified"

	issuer := NewAuthService(issuerStore, nil, "")
	issuer.SetSessionSigningSecret("cluster-secret")
	sess, err := issuer.createSession(ctx, user)
	if err != nil {
		t.Fatalf("createSession() error = %v", err)
	}
	if !strings.HasPrefix(sess.Token, signedSessionTokenPrefix) {
		t.Fatalf("token = %q, want signed session prefix", sess.Token)
	}

	validator := NewAuthService(validatorStore, nil, "")
	validator.SetSessionSigningSecret("cluster-secret")
	got, err := validator.ValidateSession(ctx, sess.Token)
	if err != nil {
		t.Fatalf("ValidateSession() error = %v", err)
	}
	if got.Email != "uploader@example.com" || got.UserID != user.ID {
		t.Fatalf("session = %+v", got)
	}
}

func TestAuthServiceSignedSessionRejectsWrongSecretAndExpiredToken(t *testing.T) {
	user := createTestUser(t, newTestStore(t), "uploader@example.com", 0)
	issuer := NewAuthService(newTestStore(t), nil, "")
	issuer.SetSessionSigningSecret("cluster-secret")
	token := issuer.newSessionToken(user, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))

	validator := NewAuthService(newTestStore(t), nil, "")
	validator.SetSessionSigningSecret("cluster-secret")
	if _, err := validator.ValidateSession(context.Background(), token); err == nil {
		t.Fatal("ValidateSession() succeeded for expired signed token")
	}

	token = issuer.newSessionToken(user, time.Now(), time.Now().Add(time.Hour))
	validator.SetSessionSigningSecret("other-secret")
	if _, err := validator.ValidateSession(context.Background(), token); err == nil {
		t.Fatal("ValidateSession() succeeded with wrong secret")
	}
}

func TestAuthServiceRejectsMalformedSignedSessionSignature(t *testing.T) {
	user := createTestUser(t, newTestStore(t), "uploader@example.com", 0)
	auth := NewAuthService(newTestStore(t), nil, "")
	auth.SetSessionSigningSecret("cluster-secret")
	token := auth.newSessionToken(user, time.Now(), time.Now().Add(time.Hour))
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("signed token parts = %d, want 3", len(parts))
	}
	token = parts[0] + "." + parts[1] + ".not-base64!"
	if _, err := auth.ValidateSession(context.Background(), token); err == nil {
		t.Fatal("ValidateSession() succeeded with malformed signed session signature")
	}
}

func TestAuthServiceLogoutEmptyTokenDoesNotCreateRevocation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	auth := NewAuthService(store, nil, "")
	if err := auth.Logout(ctx, " "); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if revoked, err := store.IsSessionRevoked(ctx, ""); err != nil {
		t.Fatalf("IsSessionRevoked() error = %v", err)
	} else if revoked {
		t.Fatal("empty token was revoked")
	}
}

func TestAuthServiceLogoutRevokesSignedSessionLocally(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	user := createTestUser(t, store, "uploader@example.com", 0)
	auth := NewAuthService(store, nil, "")
	auth.SetSessionSigningSecret("cluster-secret")
	sess, err := auth.createSession(ctx, user)
	if err != nil {
		t.Fatalf("createSession() error = %v", err)
	}
	if err := auth.Logout(ctx, sess.Token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := auth.ValidateSession(ctx, sess.Token); err == nil {
		t.Fatal("ValidateSession() succeeded after logout")
	}
}

func TestAuthServiceSignedSessionRevocationWinsOverSessionRow(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	user := createTestUser(t, store, "uploader@example.com", 0)
	auth := NewAuthService(store, nil, "")
	auth.SetSessionSigningSecret("cluster-secret")
	sess, err := auth.createSession(ctx, user)
	if err != nil {
		t.Fatalf("createSession() error = %v", err)
	}
	if err := store.RevokeSession(ctx, sess.Token, sess.ExpiresAt); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if _, err := auth.ValidateSession(ctx, sess.Token); err == nil {
		t.Fatal("ValidateSession() succeeded for signed token with both session row and revocation row")
	}
}

func TestAuthServiceLogoutRevokesLegacySessionToken(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	user := createTestUser(t, store, "legacy@example.com", 0)
	auth := NewAuthService(store, nil, "")
	sess, err := auth.createSession(ctx, user)
	if err != nil {
		t.Fatalf("createSession() error = %v", err)
	}
	if strings.HasPrefix(sess.Token, signedSessionTokenPrefix) {
		t.Fatalf("token = %q, want legacy token", sess.Token)
	}
	if err := auth.Logout(ctx, sess.Token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if revoked, err := store.IsSessionRevoked(ctx, sess.Token); err != nil {
		t.Fatalf("IsSessionRevoked() error = %v", err)
	} else if !revoked {
		t.Fatal("legacy session token was not revoked")
	}
	if _, err := auth.ValidateSession(ctx, sess.Token); err == nil {
		t.Fatal("ValidateSession() succeeded after legacy logout")
	}
}

func TestStoreDeleteAndRevokeSessionRemovesSessionRow(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	sess := &Session{
		Token:     "legacy-token",
		UserID:    "user-1",
		Email:     "user@example.com",
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := store.DeleteAndRevokeSession(ctx, sess.Token, sess.ExpiresAt); err != nil {
		t.Fatalf("DeleteAndRevokeSession() error = %v", err)
	}
	if _, err := store.GetSession(ctx, sess.Token); err == nil {
		t.Fatal("GetSession() succeeded after DeleteAndRevokeSession")
	}
	if revoked, err := store.IsSessionRevoked(ctx, sess.Token); err != nil {
		t.Fatalf("IsSessionRevoked() error = %v", err)
	} else if !revoked {
		t.Fatal("session was not revoked")
	}
}

func TestSkillMarketLoadSnapshotMergesWithoutDeletingLocalRows(t *testing.T) {
	ctx := context.Background()
	local := newTestStore(t)
	remote := newTestStore(t)
	now := time.Now()
	if err := local.CreateSubmission(ctx, &SkillSubmission{
		ID:        "local-sub",
		Email:     "local@example.com",
		Status:    "pending",
		ZipPath:   "/tmp/local.zip",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("local CreateSubmission() error = %v", err)
	}
	if err := remote.CreateSubmission(ctx, &SkillSubmission{
		ID:        "remote-sub",
		Email:     "remote@example.com",
		Status:    "pending",
		ZipPath:   "/tmp/remote.zip",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("remote CreateSubmission() error = %v", err)
	}
	snap, err := remote.DumpSnapshot(ctx)
	if err != nil {
		t.Fatalf("DumpSnapshot() error = %v", err)
	}
	if err := local.LoadSnapshot(ctx, snap); err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	for _, id := range []string{"local-sub", "remote-sub"} {
		if _, err := local.GetSubmissionByID(ctx, id); err != nil {
			t.Fatalf("GetSubmissionByID(%q) error = %v", id, err)
		}
	}
}

func TestSkillMarketLoadSnapshotDoesNotOverwriteNewerSubmission(t *testing.T) {
	ctx := context.Background()
	local := newTestStore(t)
	remote := newTestStore(t)
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	if err := local.CreateSubmission(ctx, &SkillSubmission{
		ID:        "sub-1",
		Email:     "uploader@example.com",
		Status:    "success",
		SkillID:   "skill-ok",
		ZipPath:   "/tmp/local.zip",
		CreatedAt: older,
		UpdatedAt: newer,
	}); err != nil {
		t.Fatalf("local CreateSubmission() error = %v", err)
	}
	if err := remote.CreateSubmission(ctx, &SkillSubmission{
		ID:        "sub-1",
		Email:     "uploader@example.com",
		Status:    "pending",
		ZipPath:   "/tmp/remote.zip",
		CreatedAt: older,
		UpdatedAt: older,
	}); err != nil {
		t.Fatalf("remote CreateSubmission() error = %v", err)
	}
	snap, err := remote.DumpSnapshot(ctx)
	if err != nil {
		t.Fatalf("DumpSnapshot() error = %v", err)
	}
	if err := local.LoadSnapshot(ctx, snap); err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	got, err := local.GetSubmissionByID(ctx, "sub-1")
	if err != nil {
		t.Fatalf("GetSubmissionByID() error = %v", err)
	}
	if got.Status != "success" || got.SkillID != "skill-ok" {
		t.Fatalf("submission was overwritten by older snapshot: %+v", got)
	}
}

func TestSkillMarketLoadSnapshotDoesNotDowngradePurchaseTerminalState(t *testing.T) {
	ctx := context.Background()
	local := newTestStore(t)
	remote := newTestStore(t)
	createdAt := time.Now().Add(-time.Hour)
	localRec := &PurchaseRecord{
		ID:               "purchase-1",
		BuyerEmail:       "buyer@example.com",
		BuyerID:          "buyer-1",
		SkillID:          "skill-1",
		PurchasedVersion: 1,
		PurchaseType:     "purchase",
		AmountPaid:       100,
		SellerID:         "seller-1",
		KeyStatus:        "key_delivered",
		APIKeyID:         "key-1",
		Status:           "refunded",
		CreatedAt:        createdAt,
	}
	if err := local.CreatePurchase(ctx, localRec); err != nil {
		t.Fatalf("local CreatePurchase() error = %v", err)
	}
	remoteRec := *localRec
	remoteRec.KeyStatus = "pending_key"
	remoteRec.APIKeyID = ""
	remoteRec.Status = "active"
	if err := remote.CreatePurchase(ctx, &remoteRec); err != nil {
		t.Fatalf("remote CreatePurchase() error = %v", err)
	}
	snap, err := remote.DumpSnapshot(ctx)
	if err != nil {
		t.Fatalf("DumpSnapshot() error = %v", err)
	}
	if err := local.LoadSnapshot(ctx, snap); err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	got, err := local.GetPurchaseByID(ctx, "purchase-1")
	if err != nil {
		t.Fatalf("GetPurchaseByID() error = %v", err)
	}
	if got.Status != "refunded" || got.KeyStatus != "key_delivered" || got.APIKeyID != "key-1" {
		t.Fatalf("purchase was downgraded by stale snapshot: %+v", got)
	}
}

func TestSkillMarketLoadSnapshotDoesNotMakeAssignedAPIKeyAvailable(t *testing.T) {
	ctx := context.Background()
	local := newTestStore(t)
	remote := newTestStore(t)
	if _, err := NewAPIKeyPoolService(local, []byte("cluster-secret")); err != nil {
		t.Fatalf("local NewAPIKeyPoolService() error = %v", err)
	}
	if _, err := NewAPIKeyPoolService(remote, []byte("cluster-secret")); err != nil {
		t.Fatalf("remote NewAPIKeyPoolService() error = %v", err)
	}
	now := fmtTime(time.Now().Add(-time.Hour))
	if _, err := local.db.ExecContext(ctx, `INSERT INTO sm_api_keys (id, skill_id, env_name, encrypted_key, status, buyer_email, assigned_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-1", "skill-1", "API_KEY", "encrypted-local", "assigned", "buyer@example.com", now, now); err != nil {
		t.Fatalf("insert local api key: %v", err)
	}
	if _, err := remote.db.ExecContext(ctx, `INSERT INTO sm_api_keys (id, skill_id, env_name, encrypted_key, status, buyer_email, assigned_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-1", "skill-1", "API_KEY", "encrypted-local", "available", "", "", now); err != nil {
		t.Fatalf("insert remote api key: %v", err)
	}
	snap, err := remote.DumpSnapshot(ctx)
	if err != nil {
		t.Fatalf("DumpSnapshot() error = %v", err)
	}
	if err := local.LoadSnapshot(ctx, snap); err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	var status, buyerEmail, assignedAt string
	if err := local.readDB.QueryRowContext(ctx, `SELECT status, buyer_email, assigned_at FROM sm_api_keys WHERE id = ?`, "key-1").Scan(&status, &buyerEmail, &assignedAt); err != nil {
		t.Fatalf("query api key: %v", err)
	}
	if status != "assigned" || buyerEmail != "buyer@example.com" || assignedAt == "" {
		t.Fatalf("api key was downgraded by stale snapshot: status=%q buyer=%q assigned_at=%q", status, buyerEmail, assignedAt)
	}
}

func TestSkillMarketLoadSnapshotReconcilesSameEmailUserIDs(t *testing.T) {
	ctx := context.Background()
	local := newTestStore(t)
	remote := newTestStore(t)
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	localUser := &SkillMarketUser{
		ID:        "local-user",
		Email:     "uploader@example.com",
		Status:    "verified",
		CreatedAt: older,
		UpdatedAt: older,
	}
	if err := local.CreateUser(ctx, localUser); err != nil {
		t.Fatalf("local CreateUser() error = %v", err)
	}
	if err := local.CreateSubmission(ctx, &SkillSubmission{
		ID:        "local-sub",
		Email:     "uploader@example.com",
		UserID:    "local-user",
		Status:    "pending",
		ZipPath:   "/tmp/local.zip",
		CreatedAt: older,
		UpdatedAt: older,
	}); err != nil {
		t.Fatalf("local CreateSubmission() error = %v", err)
	}
	remoteUser := *localUser
	remoteUser.ID = "cluster-user"
	remoteUser.UpdatedAt = newer
	if err := remote.CreateUser(ctx, &remoteUser); err != nil {
		t.Fatalf("remote CreateUser() error = %v", err)
	}
	snap, err := remote.DumpSnapshot(ctx)
	if err != nil {
		t.Fatalf("DumpSnapshot() error = %v", err)
	}
	if err := local.LoadSnapshot(ctx, snap); err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	got, err := local.GetUserByEmail(ctx, "uploader@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}
	if got.ID != "cluster-user" {
		t.Fatalf("user id = %q, want cluster-user", got.ID)
	}
	sub, err := local.GetSubmissionByID(ctx, "local-sub")
	if err != nil {
		t.Fatalf("GetSubmissionByID() error = %v", err)
	}
	if sub.UserID != "cluster-user" {
		t.Fatalf("submission user id = %q, want cluster-user", sub.UserID)
	}
}

func TestSkillMarketLoadSnapshotRewritesIncomingUserIDWhenLocalUserIsNewer(t *testing.T) {
	ctx := context.Background()
	local := newTestStore(t)
	remote := newTestStore(t)
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	if err := local.CreateUser(ctx, &SkillMarketUser{
		ID:        "local-user",
		Email:     "uploader@example.com",
		Status:    "verified",
		CreatedAt: older,
		UpdatedAt: newer,
	}); err != nil {
		t.Fatalf("local CreateUser() error = %v", err)
	}
	if err := remote.CreateUser(ctx, &SkillMarketUser{
		ID:        "remote-user",
		Email:     "uploader@example.com",
		Status:    "verified",
		CreatedAt: older,
		UpdatedAt: older,
	}); err != nil {
		t.Fatalf("remote CreateUser() error = %v", err)
	}
	if err := remote.CreateSubmission(ctx, &SkillSubmission{
		ID:        "remote-sub",
		Email:     "uploader@example.com",
		UserID:    "remote-user",
		Status:    "pending",
		ZipPath:   "/tmp/remote.zip",
		CreatedAt: older,
		UpdatedAt: older,
	}); err != nil {
		t.Fatalf("remote CreateSubmission() error = %v", err)
	}
	snap, err := remote.DumpSnapshot(ctx)
	if err != nil {
		t.Fatalf("DumpSnapshot() error = %v", err)
	}
	if err := local.LoadSnapshot(ctx, snap); err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	got, err := local.GetUserByEmail(ctx, "uploader@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}
	if got.ID != "local-user" {
		t.Fatalf("user id = %q, want local-user", got.ID)
	}
	sub, err := local.GetSubmissionByID(ctx, "remote-sub")
	if err != nil {
		t.Fatalf("GetSubmissionByID() error = %v", err)
	}
	if sub.UserID != "local-user" {
		t.Fatalf("incoming submission user id = %q, want local-user", sub.UserID)
	}
}

func TestUserServiceEnsureAccountWithIDCreatesStableClusterUser(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	svc := NewUserService(store, nil)
	user, err := svc.EnsureAccountWithID(ctx, "user-cluster", "Uploader@Example.com")
	if err != nil {
		t.Fatalf("EnsureAccountWithID() error = %v", err)
	}
	if user.ID != "user-cluster" || user.Email != "uploader@example.com" || user.Status != "verified" {
		t.Fatalf("user = %+v", user)
	}
	if !user.UpdatedAt.Equal(time.Unix(0, 0).UTC()) {
		t.Fatalf("UpdatedAt = %v, want epoch placeholder", user.UpdatedAt)
	}
	got, err := store.GetUserByEmail(ctx, "uploader@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}
	if got.ID != "user-cluster" {
		t.Fatalf("stored user id = %q", got.ID)
	}
}

func TestStoreEmitSyncCoalescesConcurrentSnapshots(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	rec := &blockingSyncRecorder{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	store.SetSyncRecorder(rec)

	store.emitSync(ctx)
	<-rec.entered

	const concurrent = 8
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.emitSync(ctx)
		}()
	}
	wg.Wait()
	close(rec.release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&rec.calls) < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&rec.calls); got != 2 {
		t.Fatalf("sync calls = %d, want 2 coalesced emissions", got)
	}
}

func TestStoreEmitSyncIgnoresRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := newTestStore(t)
	rec := &countingSyncRecorder{}
	store.SetSyncRecorder(rec)

	store.emitSync(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&rec.calls) < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&rec.calls); got < 1 {
		t.Fatalf("sync calls = %d, want at least 1 even with canceled request context", got)
	}
}

func TestStoreEmitSyncReturnsWithoutBlockingOnSnapshotAppend(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	rec := &blockingSyncRecorder{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	store.SetSyncRecorder(rec)

	done := make(chan struct{})
	go func() {
		store.emitSync(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emitSync blocked on snapshot append")
	}
	<-rec.entered
	close(rec.release)
}

func TestStoreSyncRecorderCanBeClearedDuringAsyncEmission(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	rec := &blockingSyncRecorder{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	store.SetSyncRecorder(rec)

	store.emitSync(ctx)
	<-rec.entered
	store.SetSyncRecorder(nil)
	close(rec.release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		store.syncMu.Lock()
		running := store.syncRunning
		store.syncMu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync emission did not finish after recorder was cleared")
}

func TestStoreSyncEmissionRecoversAndResetsAfterRecorderPanic(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	rec := &panicSyncRecorder{}
	store.SetSyncRecorder(rec)

	store.emitSync(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		store.syncMu.Lock()
		running := store.syncRunning
		store.syncMu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync emission did not reset after recorder panic")
}

func TestStoreSyncEmissionRetriesPendingAfterRecorderPanic(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	panicRec := &blockingPanicSyncRecorder{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	store.SetSyncRecorder(panicRec)

	store.emitSync(ctx)
	<-panicRec.entered
	store.emitSync(ctx)
	countRec := &countingSyncRecorder{}
	store.SetSyncRecorder(countRec)
	close(panicRec.release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&countRec.calls) < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&countRec.calls); got < 1 {
		t.Fatalf("pending sync was not retried after recorder panic; calls=%d", got)
	}
}

func TestStoreSyncEmissionResetsAfterSnapshotDumpFailure(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	rec := &countingSyncRecorder{}
	store.SetSyncRecorder(rec)
	if err := store.readDB.Close(); err != nil {
		t.Fatalf("close read db: %v", err)
	}

	store.emitSync(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		store.syncMu.Lock()
		running := store.syncRunning
		store.syncMu.Unlock()
		if !running {
			if got := atomic.LoadInt32(&rec.calls); got != 0 {
				t.Fatalf("sync calls = %d, want 0 after dump failure", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync emission did not reset after snapshot dump failure")
}

type blockingSyncRecorder struct {
	calls   int32
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingSyncRecorder) AppendSkillMarketSnapshot(context.Context, *Snapshot) {
	if atomic.AddInt32(&r.calls, 1) == 1 {
		r.once.Do(func() { close(r.entered) })
		<-r.release
	}
}

type countingSyncRecorder struct {
	calls int32
}

func (r *countingSyncRecorder) AppendSkillMarketSnapshot(context.Context, *Snapshot) {
	atomic.AddInt32(&r.calls, 1)
}

type panicSyncRecorder struct{}

func (r *panicSyncRecorder) AppendSkillMarketSnapshot(context.Context, *Snapshot) {
	panic("boom")
}

type blockingPanicSyncRecorder struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingPanicSyncRecorder) AppendSkillMarketSnapshot(context.Context, *Snapshot) {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	panic("boom")
}
