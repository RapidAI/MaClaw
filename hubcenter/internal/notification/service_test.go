package notification

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryNotificationStore struct {
	created []*Notification
	items   map[string]*Notification
}

type recordingSyncRecorder struct {
	upserts []*Notification
	deletes []string
}

func (r *recordingSyncRecorder) AppendNotification(ctx context.Context, item *Notification) {
	copy := *item
	r.upserts = append(r.upserts, &copy)
}

func (r *recordingSyncRecorder) DeleteNotification(ctx context.Context, id string) {
	r.deletes = append(r.deletes, id)
}

func (s *memoryNotificationStore) Create(ctx context.Context, n *Notification) error {
	if s.items == nil {
		s.items = map[string]*Notification{}
	}
	copy := *n
	s.created = append(s.created, &copy)
	s.items[n.ID] = &copy
	return nil
}

func (s *memoryNotificationStore) Upsert(ctx context.Context, n *Notification) error {
	if s.items == nil {
		s.items = map[string]*Notification{}
	}
	copy := *n
	s.items[n.ID] = &copy
	return nil
}

func (s *memoryNotificationStore) GetByID(ctx context.Context, id string) (*Notification, error) {
	if s.items != nil {
		if item := s.items[id]; item != nil {
			copy := *item
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *memoryNotificationStore) List(ctx context.Context, filter ListFilter) ([]*Notification, int, error) {
	return nil, 0, nil
}

func (s *memoryNotificationStore) UpdateStatus(ctx context.Context, id string, status Status, updatedAt time.Time) error {
	if s.items != nil {
		if item := s.items[id]; item != nil {
			item.Status = status
			item.UpdatedAt = updatedAt
		}
	}
	return nil
}

func (s *memoryNotificationStore) ListDueDrafts(ctx context.Context, now time.Time) ([]*Notification, error) {
	now = now.UTC()
	var out []*Notification
	for _, item := range s.items {
		if item == nil || item.Status != StatusDraft || item.PublishAt == nil || item.PublishAt.After(now) {
			continue
		}
		copy := *item
		out = append(out, &copy)
	}
	return out, nil
}

func (s *memoryNotificationStore) ClaimDraft(ctx context.Context, id string, next Status, updatedAt time.Time) (bool, error) {
	item := s.items[id]
	if item == nil || item.Status != StatusDraft {
		return false, nil
	}
	item.Status = next
	item.UpdatedAt = updatedAt
	return true, nil
}

func (s *memoryNotificationStore) ClaimDispatch(ctx context.Context, id, node string, until, updatedAt time.Time) (bool, error) {
	item := s.items[id]
	if item == nil || item.Status != StatusDraft {
		return false, nil
	}
	holder := strings.TrimSpace(item.DispatchNode)
	if item.DispatchUntil != nil && item.DispatchUntil.After(updatedAt) && holder != "" && holder != node {
		return false, nil
	}
	item.DispatchNode = node
	lease := until.UTC()
	item.DispatchUntil = &lease
	item.UpdatedAt = updatedAt
	return true, nil
}

func (s *memoryNotificationStore) Delete(ctx context.Context, id string) error {
	if s.items != nil {
		delete(s.items, id)
	}
	return nil
}

func (s *memoryNotificationStore) RecordCascadeResult(ctx context.Context, result *CascadeResult) error {
	return nil
}

func (s *memoryNotificationStore) GetCascadeResults(ctx context.Context, notificationID string) ([]*CascadeResult, error) {
	return nil, nil
}

func TestStatusMayApplyRejectsStaleDraft(t *testing.T) {
	if !StatusMayApply(StatusDraft, StatusPublished) || !StatusMayApply(StatusPublished, StatusRevoked) {
		t.Fatal("forward transitions were rejected")
	}
	if StatusMayApply(StatusPublished, StatusDraft) || StatusMayApply(StatusRevoked, StatusPublished) || StatusMayApply(StatusExpired, StatusDraft) {
		t.Fatal("a terminal row accepted a stale replica")
	}
}

func TestCreateNotificationHonorsDraftStatus(t *testing.T) {
	store := &memoryNotificationStore{}
	svc := NewService(store, nil, nil)

	notif, err := svc.CreateNotification(context.Background(), CreateRequest{
		Title:        "Scheduled maintenance",
		Content:      "Service will restart tonight.",
		Category:     CategoryMaintenance,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		Status:       StatusDraft,
	})
	if err != nil {
		t.Fatalf("CreateNotification returned error: %v", err)
	}
	if notif.Status != StatusDraft {
		t.Fatalf("status = %q, want %q", notif.Status, StatusDraft)
	}
	if len(store.created) != 1 || store.created[0].Status != StatusDraft {
		t.Fatalf("persisted status = %#v, want draft", store.created)
	}
}

func TestPublishDueReleasesScheduledDraftOnce(t *testing.T) {
	store := &memoryNotificationStore{}
	recorder := &recordingSyncRecorder{}
	svc := NewService(store, nil, nil)
	svc.SetSyncRecorder(recorder)
	ctx := context.Background()
	future := time.Now().UTC().Add(time.Hour)
	past := time.Now().UTC().Add(-time.Minute)
	expire := time.Now().UTC().Add(-time.Second)

	scheduled, err := svc.CreateNotification(ctx, CreateRequest{
		Title: "Later", Content: "body", Category: CategoryCustom,
		AudienceType: AudienceAll, Status: StatusPublished, PublishAt: &future,
	})
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if scheduled.Status != StatusDraft || scheduled.PublishAt == nil {
		t.Fatalf("future publish stored as %#v", scheduled)
	}
	manual, err := svc.CreateNotification(ctx, CreateRequest{
		Title: "Draft", Content: "body", Category: CategoryCustom,
		AudienceType: AudienceAll, Status: StatusDraft,
	})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	expired, err := svc.CreateNotification(ctx, CreateRequest{
		Title: "Old", Content: "body", Category: CategoryCustom,
		AudienceType: AudienceAll, Status: StatusDraft, PublishAt: &past, ExpireAt: &expire,
	})
	if err != nil {
		t.Fatalf("expired schedule: %v", err)
	}

	if err := svc.PublishDue(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("PublishDue early: %v", err)
	}
	if got, _ := store.GetByID(ctx, scheduled.ID); got.Status != StatusDraft {
		t.Fatalf("early status = %s", got.Status)
	}
	if got, _ := store.GetByID(ctx, manual.ID); got.Status != StatusDraft {
		t.Fatalf("manual draft status = %s", got.Status)
	}
	if got, _ := store.GetByID(ctx, expired.ID); got.Status != StatusExpired {
		t.Fatalf("expired due status = %s", got.Status)
	}

	if err := svc.PublishDue(ctx, future.Add(time.Minute)); err != nil {
		t.Fatalf("PublishDue due: %v", err)
	}
	if got, _ := store.GetByID(ctx, scheduled.ID); got.Status != StatusPublished {
		t.Fatalf("due status = %s", got.Status)
	}
	if got, _ := store.GetByID(ctx, manual.ID); got.Status != StatusDraft {
		t.Fatalf("manual draft was released")
	}
	publishedSync := 0
	for _, item := range recorder.upserts {
		if item.ID == scheduled.ID && item.Status == StatusPublished {
			publishedSync++
		}
	}
	if publishedSync != 1 {
		t.Fatalf("published sync count = %d", publishedSync)
	}
	if err := svc.PublishDue(ctx, future.Add(time.Minute)); err != nil {
		t.Fatalf("PublishDue again: %v", err)
	}
	publishedSync = 0
	for _, item := range recorder.upserts {
		if item.ID == scheduled.ID && item.Status == StatusPublished {
			publishedSync++
		}
	}
	if publishedSync != 1 {
		t.Fatalf("second pass published again: %d", publishedSync)
	}
}

type failGetStore struct {
	*memoryNotificationStore
	failGet bool
}

func (s *failGetStore) GetByID(ctx context.Context, id string) (*Notification, error) {
	if s.failGet {
		return nil, errors.New("db down")
	}
	return s.memoryNotificationStore.GetByID(ctx, id)
}

func TestPublishDueUsesSnapshotWhenReloadFails(t *testing.T) {
	base := &memoryNotificationStore{}
	store := &failGetStore{memoryNotificationStore: base, failGet: true}
	recorder := &recordingSyncRecorder{}
	svc := NewService(store, nil, nil)
	svc.SetSyncRecorder(recorder)
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Minute)
	created, err := svc.CreateNotification(ctx, CreateRequest{
		Title: "Due", Content: "body", Category: CategoryCustom,
		AudienceType: AudienceAll, Status: StatusDraft, PublishAt: &past,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.PublishDue(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("PublishDue: %v", err)
	}
	if got, _ := base.GetByID(ctx, created.ID); got == nil || got.Status != StatusPublished {
		t.Fatalf("status = %#v", got)
	}
	publishedSync := 0
	for _, item := range recorder.upserts {
		if item.ID == created.ID && item.Status == StatusPublished {
			publishedSync++
		}
	}
	if publishedSync != 1 {
		t.Fatalf("published sync count = %d", publishedSync)
	}
}

func TestCreateNotificationRejectsInvalidStatus(t *testing.T) {
	store := &memoryNotificationStore{}
	svc := NewService(store, nil, nil)

	_, err := svc.CreateNotification(context.Background(), CreateRequest{
		Title:        "Bad status",
		Content:      "This should not be accepted.",
		Category:     CategoryCustom,
		AudienceType: AudienceAll,
		Status:       Status("archived"),
	})
	if err != ErrInvalidStatus {
		t.Fatalf("error = %v, want %v", err, ErrInvalidStatus)
	}
}

func TestCreateNotificationPublishedWithoutCascadeDependencyDoesNotPanic(t *testing.T) {
	store := &memoryNotificationStore{}
	svc := NewService(store, nil, nil)

	notif, err := svc.CreateNotification(context.Background(), CreateRequest{
		Title:        "Immediate notice",
		Content:      "Publish now.",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		Status:       StatusPublished,
	})
	if err != nil {
		t.Fatalf("CreateNotification returned error: %v", err)
	}
	if notif.Status != StatusPublished {
		t.Fatalf("status = %q, want %q", notif.Status, StatusPublished)
	}

	time.Sleep(10 * time.Millisecond)
}

func TestNotificationServiceRecordsHASyncLifecycle(t *testing.T) {
	store := &memoryNotificationStore{}
	recorder := &recordingSyncRecorder{}
	svc := NewService(store, nil, nil)
	svc.SetSyncRecorder(recorder)
	ctx := context.Background()

	notif, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "Lifecycle notice",
		Content:      "Create, revoke, then delete.",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		Status:       StatusPublished,
	})
	if err != nil {
		t.Fatalf("CreateNotification returned error: %v", err)
	}
	if len(recorder.upserts) != 1 || recorder.upserts[0].ID != notif.ID || recorder.upserts[0].Status != StatusPublished {
		t.Fatalf("create sync upserts = %#v, want published notification", recorder.upserts)
	}

	if err := svc.RevokeNotification(ctx, notif.ID); err != nil {
		t.Fatalf("RevokeNotification returned error: %v", err)
	}
	if len(recorder.upserts) != 2 || recorder.upserts[1].Status != StatusRevoked {
		t.Fatalf("revoke sync upserts = %#v, want revoked notification", recorder.upserts)
	}

	if err := svc.DeleteNotification(ctx, notif.ID); err != nil {
		t.Fatalf("DeleteNotification returned error: %v", err)
	}
	if len(recorder.deletes) != 1 || recorder.deletes[0] != notif.ID {
		t.Fatalf("delete sync = %#v, want %q", recorder.deletes, notif.ID)
	}
}

func TestDeleteNotificationRequiresInactiveStatus(t *testing.T) {
	store := &memoryNotificationStore{}
	svc := NewService(store, nil, nil)
	ctx := context.Background()

	notif, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "Delete lifecycle",
		Content:      "Published notifications must be revoked first.",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		Status:       StatusPublished,
	})
	if err != nil {
		t.Fatalf("CreateNotification returned error: %v", err)
	}
	if err := svc.DeleteNotification(ctx, notif.ID); err != ErrCannotDelete {
		t.Fatalf("DeleteNotification published error = %v, want %v", err, ErrCannotDelete)
	}

	if err := store.UpdateStatus(ctx, notif.ID, StatusRevoked, time.Now().UTC()); err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}
	if err := svc.DeleteNotification(ctx, notif.ID); err != nil {
		t.Fatalf("DeleteNotification revoked returned error: %v", err)
	}
	if got, err := store.GetByID(ctx, notif.ID); err != nil || got != nil {
		t.Fatalf("GetByID after delete = %#v, %v; want nil, nil", got, err)
	}
}

// ---------------------------------------------------------------------------
// Pinned (置顶) message fields
// ---------------------------------------------------------------------------

func TestCreateNotificationPinnedDefaultsAndValidation(t *testing.T) {
	store := &memoryNotificationStore{}
	svc := NewService(store, nil, nil)
	ctx := context.Background()

	// A pinned message without a button label gets the default, and a
	// permanent pin implies pinned.
	notif, err := svc.CreateNotification(ctx, CreateRequest{
		Title:         "hc banner",
		Content:       "launch",
		Category:      CategorySystemAnnouncement,
		AudienceType:  AudienceAll,
		Status:        StatusPublished,
		PinnedForever: true,
		LinkURL:       "https://example.com/launch",
	})
	if err != nil {
		t.Fatalf("CreateNotification returned error: %v", err)
	}
	if !notif.Pinned || !notif.PinnedForever {
		t.Fatalf("pinned flags = %v/%v, want true/true", notif.Pinned, notif.PinnedForever)
	}
	if notif.ButtonText != DefaultPinnedButtonText {
		t.Fatalf("button text = %q, want %q", notif.ButtonText, DefaultPinnedButtonText)
	}
	if notif.LinkURL != "https://example.com/launch" {
		t.Fatalf("link url = %q", notif.LinkURL)
	}
	persisted := store.created[0]
	if !persisted.Pinned || !persisted.PinnedForever || persisted.ButtonText != DefaultPinnedButtonText {
		t.Fatalf("persisted pinned fields = %#v", persisted)
	}

	// A non-http link is rejected.
	if _, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "bad link",
		Content:      "c",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		Pinned:       true,
		LinkURL:      "javascript:alert(1)",
	}); err != ErrLinkURLInvalid {
		t.Fatalf("javascript link error = %v, want %v", err, ErrLinkURLInvalid)
	}

	// An English source keeps the English default, including the stored locale.
	en, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "Hello",
		Content:      "Body",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		Status:       StatusPublished,
		Pinned:       true,
		SourceLang:   LangEn,
		LinkURL:      "https://example.com/en",
	})
	if err != nil {
		t.Fatalf("english pinned create: %v", err)
	}
	if en.SourceLang != LangEn || en.ButtonText != "View details" || en.Locales[LangEn].ButtonText != "View details" {
		t.Fatalf("english pinned = lang %q button %q locale %#v", en.SourceLang, en.ButtonText, en.Locales)
	}

	// An over-long button label is rejected.
	if _, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "long button",
		Content:      "c",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		Pinned:       true,
		ButtonText:   repeatRunesForTest("长", 21),
	}); err != ErrButtonTextTooLong {
		t.Fatalf("button text error = %v, want %v", err, ErrButtonTextTooLong)
	}
}

func repeatRunesForTest(r string, n int) string {
	out := make([]rune, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, []rune(r)...)
	}
	return string(out)
}

type staticDispatchOwner struct {
	owns  bool
	id    string
	lease time.Duration
}

func (o staticDispatchOwner) OwnsScheduledDispatch() bool { return o.owns }
func (o staticDispatchOwner) DispatchIdentity() string    { return o.id }
func (o staticDispatchOwner) DispatchLease() time.Duration {
	return o.lease
}

// applyingSync copies each replicated notification onto dest, the way an HA
// upsert lands on a peer's own database.
type applyingSync struct {
	dest *memoryNotificationStore
}

func (a applyingSync) AppendNotification(ctx context.Context, item *Notification) {
	if a.dest == nil || item == nil {
		return
	}
	cp := *item
	_ = a.dest.Upsert(ctx, &cp)
}

func (a applyingSync) DeleteNotification(context.Context, string) {}

type countingCascade struct {
	mu  sync.Mutex
	ids []string
}

func (c *countingCascade) DispatchToHubs(_ context.Context, notif *Notification, _ []HubEndpoint) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if notif != nil {
		c.ids = append(c.ids, notif.ID)
	}
	return nil
}

func (c *countingCascade) DispatchRevoke(context.Context, string, []HubEndpoint) error {
	return nil
}

func (c *countingCascade) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.ids)
}

type allHubsResolver struct{}

func (allHubsResolver) AllHubs(context.Context) ([]HubEndpoint, error) {
	return []HubEndpoint{{ID: "hub-1", URL: "https://hub.example"}}, nil
}

func (allHubsResolver) HubsByIDs(context.Context, []string) ([]HubEndpoint, error) {
	return nil, nil
}

func (allHubsResolver) HubsByTenantPairs(context.Context, []string) ([]HubEndpoint, error) {
	return nil, nil
}

func TestPublishDueCascadesFromDispatchOwnerOnly(t *testing.T) {
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Minute)
	draft := func() *Notification {
		return &Notification{
			ID: "n-due", Title: "Later", Content: "body",
			Category: CategoryCustom, AudienceType: AudienceAll,
			Status: StatusDraft, PublishAt: &past,
		}
	}
	storeA := &memoryNotificationStore{}
	storeB := &memoryNotificationStore{}
	if err := storeA.Create(ctx, draft()); err != nil {
		t.Fatal(err)
	}
	if err := storeB.Create(ctx, draft()); err != nil {
		t.Fatal(err)
	}
	cascadeA := &countingCascade{}
	cascadeB := &countingCascade{}
	owner := NewService(storeA, nil, allHubsResolver{})
	peer := NewService(storeB, nil, allHubsResolver{})
	owner.cascade = cascadeA
	peer.cascade = cascadeB
	owner.SetDispatchOwner(staticDispatchOwner{owns: true})
	peer.SetDispatchOwner(staticDispatchOwner{owns: false})
	now := time.Now().UTC()

	if err := owner.PublishDue(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err := peer.PublishDue(ctx, now); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var gotA *Notification
	for {
		gotA, _ = storeA.GetByID(ctx, "n-due")
		if cascadeA.count() >= 1 && gotA != nil && gotA.Status == StatusPublished {
			break
		}
		if time.Now().After(deadline) {
			status := ""
			if gotA != nil {
				status = string(gotA.Status)
			}
			t.Fatalf("owner cascades = %d, status = %s", cascadeA.count(), status)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if cascadeA.count() != 1 {
		t.Fatalf("owner cascades = %d, want 1", cascadeA.count())
	}
	if cascadeB.count() != 0 {
		t.Fatalf("peer cascades = %d, want 0", cascadeB.count())
	}
	gotB, _ := storeB.GetByID(ctx, "n-due")
	if gotB == nil || gotB.Status != StatusDraft {
		t.Fatalf("peer status = %#v, want draft", gotB)
	}

	if err := owner.PublishDue(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if cascadeA.count() != 1 {
		t.Fatalf("owner cascaded again: %d", cascadeA.count())
	}
}

type failHubsResolver struct {
	calls atomic.Int32
}

func (r *failHubsResolver) AllHubs(context.Context) ([]HubEndpoint, error) {
	r.calls.Add(1)
	return nil, errors.New("resolver down")
}

func (failHubsResolver) HubsByIDs(context.Context, []string) ([]HubEndpoint, error) {
	return nil, nil
}

func (failHubsResolver) HubsByTenantPairs(context.Context, []string) ([]HubEndpoint, error) {
	return nil, nil
}

func TestPublishDueLeavesDraftWhenResolveFails(t *testing.T) {
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Minute)
	store := &memoryNotificationStore{}
	if err := store.Create(ctx, &Notification{
		ID: "n-due", Title: "Later", Content: "body",
		Category: CategoryCustom, AudienceType: AudienceAll,
		Status: StatusDraft, PublishAt: &past,
	}); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingSyncRecorder{}
	cascade := &countingCascade{}
	resolver := &failHubsResolver{}
	svc := NewService(store, nil, resolver)
	svc.cascade = cascade
	svc.SetSyncRecorder(recorder)

	if err := svc.PublishDue(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for resolver.calls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("resolve was not attempted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	got, _ := store.GetByID(ctx, "n-due")
	if got == nil || got.Status != StatusDraft {
		t.Fatalf("status = %#v, want draft", got)
	}
	if cascade.count() != 0 {
		t.Fatalf("cascades = %d, want 0", cascade.count())
	}
	for _, item := range recorder.upserts {
		if item != nil && item.Status == StatusPublished {
			t.Fatal("failed resolve replicated a published row")
		}
	}
}

type blockingCascade struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	countingCascade
}

func (c *blockingCascade) DispatchToHubs(ctx context.Context, notif *Notification, hubs []HubEndpoint) error {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return c.countingCascade.DispatchToHubs(ctx, notif, hubs)
}

func TestPublishDueSkipsWhileCascadeInFlight(t *testing.T) {
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Minute)
	store := &memoryNotificationStore{}
	if err := store.Create(ctx, &Notification{
		ID: "n-due", Title: "Later", Content: "body",
		Category: CategoryCustom, AudienceType: AudienceAll,
		Status: StatusDraft, PublishAt: &past,
	}); err != nil {
		t.Fatal(err)
	}
	cascade := &blockingCascade{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := NewService(store, nil, allHubsResolver{})
	svc.cascade = cascade

	if err := svc.PublishDue(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cascade.started:
	case <-time.After(2 * time.Second):
		t.Fatal("cascade did not start")
	}
	if err := svc.PublishDue(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	close(cascade.release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, _ := store.GetByID(ctx, "n-due")
		if cascade.count() == 1 && got != nil && got.Status == StatusPublished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cascades = %d, status = %#v", cascade.count(), got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPublishDueSkipsWhilePeerLeaseIsCurrent(t *testing.T) {
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Minute)
	draft := func() *Notification {
		return &Notification{
			ID: "n-due", Title: "Later", Content: "body",
			Category: CategoryCustom, AudienceType: AudienceAll,
			Status: StatusDraft, PublishAt: &past,
		}
	}
	storeA := &memoryNotificationStore{}
	storeB := &memoryNotificationStore{}
	if err := storeA.Create(ctx, draft()); err != nil {
		t.Fatal(err)
	}
	if err := storeB.Create(ctx, draft()); err != nil {
		t.Fatal(err)
	}
	standbyCascade := &blockingCascade{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	primaryCascade := &countingCascade{}
	standby := NewService(storeB, nil, allHubsResolver{})
	primary := NewService(storeA, nil, allHubsResolver{})
	standby.cascade = standbyCascade
	primary.cascade = primaryCascade
	standby.SetDispatchOwner(staticDispatchOwner{owns: true, id: "hc-2", lease: time.Hour})
	standby.SetSyncRecorder(applyingSync{dest: storeA})
	primary.SetDispatchOwner(staticDispatchOwner{owns: true, id: "hc-1", lease: time.Hour})

	if err := standby.PublishDue(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-standbyCascade.started:
	case <-time.After(2 * time.Second):
		t.Fatal("standby cascade did not start")
	}
	leased, _ := storeA.GetByID(ctx, "n-due")
	if leased == nil || leased.DispatchNode != "hc-2" || leased.Status != StatusDraft {
		t.Fatalf("primary row = %#v, want hc-2 lease on a draft", leased)
	}

	if err := primary.PublishDue(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if primaryCascade.count() != 0 {
		t.Fatalf("recovered owner cascades = %d, want 0", primaryCascade.count())
	}
	still, _ := storeA.GetByID(ctx, "n-due")
	if still == nil || still.Status != StatusDraft || still.DispatchNode != "hc-2" {
		t.Fatalf("primary row after its tick = %#v", still)
	}

	close(standbyCascade.release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, _ := storeB.GetByID(ctx, "n-due")
		if standbyCascade.count() == 1 && got != nil && got.Status == StatusPublished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("standby cascades = %d, status = %#v", standbyCascade.count(), got)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if primaryCascade.count() != 0 {
		t.Fatalf("recovered owner cascaded after the standby finished: %d", primaryCascade.count())
	}
}

func TestPublishDueStealsExpiredDispatchLease(t *testing.T) {
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Minute)
	expired := time.Now().UTC().Add(-time.Second)
	store := &memoryNotificationStore{}
	if err := store.Create(ctx, &Notification{
		ID: "n-due", Title: "Later", Content: "body",
		Category: CategoryCustom, AudienceType: AudienceAll,
		Status: StatusDraft, PublishAt: &past,
		DispatchNode: "hc-2", DispatchUntil: &expired,
	}); err != nil {
		t.Fatal(err)
	}
	cascade := &countingCascade{}
	svc := NewService(store, nil, allHubsResolver{})
	svc.cascade = cascade
	svc.SetDispatchOwner(staticDispatchOwner{owns: true, id: "hc-1", lease: time.Hour})

	if err := svc.PublishDue(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, _ := store.GetByID(ctx, "n-due")
		if cascade.count() == 1 && got != nil && got.Status == StatusPublished && got.DispatchNode == "hc-1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cascades = %d, row = %#v", cascade.count(), got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
