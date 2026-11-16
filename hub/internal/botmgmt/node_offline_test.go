package botmgmt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
)

func TestNodeClientDoesNotDialThePublicHost(t *testing.T) {
	hits := 0
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer public.Close()
	svc := NewService(nil)
	svc.HTTP = public.Client()
	svc.NodeHTTP = func(string) *http.Client { return nil }

	err := svc.call(context.Background(), record{BaseURL: public.URL}, "", "", http.MethodGet, "/health", nil, nil)
	if err != nil || hits != 1 {
		t.Fatalf("public call err=%v hits=%d", err, hits)
	}
	err = svc.call(context.Background(), record{DialNode: "node-1", BaseURL: "http://srv.node-1.node.invalid"}, "", "", http.MethodGet, "/health", nil, nil)
	if err == nil || hits != 1 {
		t.Fatalf("node call err=%v hits=%d", err, hits)
	}
	if nodeLinkOffline(err) {
		t.Fatal("a missing node link was queued as a brief drop")
	}
}

type countingTrip struct{ hits *int }

func (c countingTrip) RoundTrip(*http.Request) (*http.Response, error) {
	*c.hits++
	return nil, errors.New("edge node link is unavailable")
}

func TestMissingNodeLinkIsNotRetried(t *testing.T) {
	hits := 0
	svc := NewService(nil)
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: countingTrip{hits: &hits}}
	}
	err := svc.call(context.Background(), record{DialNode: "dn_lab", BaseURL: "http://srv.dn_lab.node.invalid"}, "", "", http.MethodGet, "/health", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "edge node link is unavailable") || hits != 1 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
	if nodeLinkOffline(err) {
		t.Fatal("a missing node link was queued as a brief drop")
	}
}

func TestNodeOfflineMessageWaitsThenStops(t *testing.T) {
	svc := NewService(&memSettings{})
	svc.queueDrainOff = true
	svc.Now = func() time.Time { return time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC) }
	if !nodeLinkOffline(fmt.Errorf("maclawsrv request failed: %s", edgenode.ErrOffline)) {
		t.Fatal("a wrapped tunnel drop was treated as a normal failure")
	}
	if nodeLinkOffline(fmt.Errorf("maclawsrv request failed: connection refused")) {
		t.Fatal("a public srv failure was treated as a node drop")
	}
	if !nodeLinkOffline(errors.New("desktop service: node is offline")) {
		t.Fatal("a desktop tunnel miss was treated as a normal failure")
	}
	pos, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot-1", desktopQueueItem{
		ClientMessageID: "m1",
		Content:         "继续",
		WaitKind:        desktopQueueWaitNode,
		WaitSince:       svc.now(),
	})
	if err != nil || pos != 1 {
		t.Fatalf("queue = %d %v", pos, err)
	}
	svc.expireNodeQueue("tenant-a", "alice", "bot-1")
	key := desktopQueueKey("tenant-a", "alice", "bot-1")
	if svc.desktopQueues[key][0].State == "gave_up" {
		t.Fatal("a fresh tunnel drop was given up immediately")
	}
	svc.Now = func() time.Time { return time.Date(2026, 11, 15, 0, 3, 0, 0, time.UTC) }
	svc.expireNodeQueue("tenant-a", "alice", "bot-1")
	item := svc.desktopQueues[key][0]
	if item.State != "gave_up" || item.Outcome != nodeOfflineOutcome {
		t.Fatalf("expired item = %+v", item)
	}
}

func TestWakeSaysTheNodeIsOffline(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.Desktop = &desktopCounter{openFn: func(context.Context) (string, error) {
		return "", errors.New("desktop service: node is offline")
	}}
	_, _, err := svc.WakeDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
	if !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("wake %v", err)
	}
}

type offlineNodeTrip struct{}

func (offlineNodeTrip) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("%w: broken pipe", edgenode.ErrOffline)
}

func TestOfflineNodeParksTheMessageWithAClearAck(t *testing.T) {
	hits := 0
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: "http://srv.dn_lab.node.invalid", AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: offlineNodeTrip{}}
	}
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-off")
	if err != nil || !adm.Queued || adm.Ack != nodeOfflineAck {
		t.Fatalf("admission = %+v %v", adm, err)
	}
	if hits != 0 {
		t.Fatalf("public srv hits=%d", hits)
	}
}

func TestSyncSendNamesAnOfflineNode(t *testing.T) {
	hits := 0
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: "http://srv.dn_lab.node.invalid", AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: offlineNodeTrip{}}
	}
	_, err = svc.PostMessagePhase(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-sync")
	if !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("sync send %v", err)
	}
	if view := svc.DesktopQueueStatus("tenant-a", "alice", "bot_alice", "turn-sync"); view.State == "queued" || view.State == "running" || view.State == "gave_up" {
		t.Fatalf("sync send was queued %+v", view)
	}
	if hits != 0 {
		t.Fatalf("public srv hits=%d", hits)
	}
}

func TestNodeReturnSendsTheParkedMessage(t *testing.T) {
	srvSvc := &turnKeySrv{}
	srv := httptest.NewServer(http.HandlerFunc(srvSvc.handler))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.queueDrainOff = true
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.Bots = append(rec.Bots, Bot{ID: "bot_other", Name: "other", InstanceID: "inst_other", OwnerUserID: "alice", NodeID: "dn_other"})
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: srv.URL, AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	svc.NodeHTTP = func(id string) *http.Client {
		if id != "dn_lab" {
			return &http.Client{Transport: offlineNodeTrip{}}
		}
		return srv.Client()
	}
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_alice", desktopQueueItem{
		ClientMessageID: "turn-back", Content: "打开报表", Kind: "new", WaitKind: desktopQueueWaitNode,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_other", desktopQueueItem{
		ClientMessageID: "turn-other", Content: "别动", Kind: "new", WaitKind: desktopQueueWaitNode,
	}); err != nil {
		t.Fatal(err)
	}
	svc.ReleaseNodeQueue("tenant-a", "dn_lab")
	srvSvc.mu.Lock()
	messages := append([]string(nil), srvSvc.messages...)
	srvSvc.mu.Unlock()
	if len(messages) != 1 || !strings.Contains(messages[0], "打开报表") || strings.Contains(messages[0], "别动") {
		t.Fatalf("messages = %v", messages)
	}
	if view := svc.DesktopQueueStatus("tenant-a", "alice", "bot_other", "turn-other"); view.State == "running" || view.State == "done" {
		t.Fatalf("other node started %+v", view)
	}
}

func TestNodeWaitStaysQueuedWhenTheSrvRecordIsGone(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_alice", desktopQueueItem{
		ClientMessageID: "turn-gone", Content: "打开报表", Kind: "new", WaitKind: desktopQueueWaitNode,
	}); err != nil {
		t.Fatal(err)
	}
	svc.drainDesktopQueue("tenant-a", "alice", "bot_alice")
	view := svc.DesktopQueueStatus("tenant-a", "alice", "bot_alice", "turn-gone")
	if view.State != "queued" {
		t.Fatalf("queue = %+v", view)
	}
}

// tokenThenOffline accepts the owner token and then drops the tunnel, which
// is the read of the bot instance that happens after the token succeeds.
type tokenThenOffline struct {
	instanceReads int
}

func (t *tokenThenOffline) RoundTrip(r *http.Request) (*http.Response, error) {
	if r != nil && r.URL != nil && r.URL.Path == "/api/v1/auth/token" {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`)),
			Request:    r,
		}, nil
	}
	if r != nil && r.URL != nil && strings.HasSuffix(r.URL.Path, "/bot-threads/route") {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Request:    r,
		}, nil
	}
	if r != nil && r.URL != nil && strings.HasSuffix(r.URL.Path, "/bot-intake") {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"kind":"new","phase":"execute"}`)),
			Request:    r,
		}, nil
	}
	if r != nil && r.URL != nil && r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/instances/") {
		t.instanceReads++
	}
	return nil, fmt.Errorf("%w: broken pipe", edgenode.ErrOffline)
}

func TestOfflineDuringInstanceReadStillParksTheMessage(t *testing.T) {
	hits := 0
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: "http://srv.dn_lab.node.invalid", AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	trip := &tokenThenOffline{}
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: trip}
	}
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-read")
	if err != nil || !adm.Queued || adm.Ack != nodeOfflineAck {
		t.Fatalf("admission = %+v %v", adm, err)
	}
	if hits != 0 {
		t.Fatalf("public srv hits=%d", hits)
	}
	if trip.instanceReads != 1 {
		t.Fatalf("instance reads=%d", trip.instanceReads)
	}
}

func TestOfflineDuringRouteSkipsTheInstanceRead(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: "http://srv.dn_lab.node.invalid", AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	trip := &tokenThenOffline{}
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: &routeDrop{reads: trip}}
	}
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-route")
	if err != nil || !adm.Queued || adm.Ack != nodeOfflineAck {
		t.Fatalf("admission = %+v %v", adm, err)
	}
	if trip.instanceReads != 0 {
		t.Fatalf("instance reads=%d", trip.instanceReads)
	}
}

func TestOfflineDuringIntakeSkipsTheInstanceRead(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: "http://srv.dn_lab.node.invalid", AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	trip := &tokenThenOffline{}
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: &intakeDrop{reads: trip}}
	}
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-intake")
	if err != nil || !adm.Queued || adm.Ack != nodeOfflineAck {
		t.Fatalf("admission = %+v %v", adm, err)
	}
	if trip.instanceReads != 0 {
		t.Fatalf("instance reads=%d", trip.instanceReads)
	}
}

type offlineMemoryDesktop struct{}

func (offlineMemoryDesktop) Open(context.Context, string, string) (string, error) {
	return "", nil
}
func (offlineMemoryDesktop) Stop(context.Context, string, string) error { return nil }
func (offlineMemoryDesktop) ContainerMemory(context.Context, string, string) (int64, int64, error) {
	return 0, 0, errors.New("desktop service: node is offline")
}

func TestOfflineMemoryReadSkipsTheInstanceRead(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	svc.Desktop = offlineMemoryDesktop{}
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: "http://srv.dn_lab.node.invalid", AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	trip := &tokenThenOffline{}
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: trip}
	}
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-mem-off")
	if err != nil || !adm.Queued || adm.Ack != nodeOfflineAck {
		t.Fatalf("admission = %+v %v", adm, err)
	}
	if trip.instanceReads != 0 {
		t.Fatalf("instance reads=%d", trip.instanceReads)
	}
}

type streamMemoryDesktop struct{}

func (streamMemoryDesktop) Open(context.Context, string, string) (string, error) { return "", nil }
func (streamMemoryDesktop) Stop(context.Context, string, string) error           { return nil }
func (streamMemoryDesktop) ContainerMemory(context.Context, string, string) (int64, int64, error) {
	return 0, 0, fmt.Errorf("%w: broken pipe", edgenode.ErrOffline)
}

// intakeDrop accepts the token and the route, then drops the tunnel on intake.
type intakeDrop struct {
	reads *tokenThenOffline
}

func (d *intakeDrop) RoundTrip(r *http.Request) (*http.Response, error) {
	if r != nil && r.URL != nil && (r.URL.Path == "/api/v1/auth/token" || strings.HasSuffix(r.URL.Path, "/bot-threads/route")) {
		return d.reads.RoundTrip(r)
	}
	if r != nil && r.URL != nil && r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/instances/") {
		d.reads.instanceReads++
	}
	return nil, fmt.Errorf("%w: broken pipe", edgenode.ErrOffline)
}

// routeDrop accepts the owner token and drops the tunnel on the route call.
type routeDrop struct {
	reads *tokenThenOffline
}

func (d *routeDrop) RoundTrip(r *http.Request) (*http.Response, error) {
	if r != nil && r.URL != nil && r.URL.Path == "/api/v1/auth/token" {
		return d.reads.RoundTrip(r)
	}
	if r != nil && r.URL != nil && r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/instances/") {
		d.reads.instanceReads++
	}
	return nil, fmt.Errorf("%w: broken pipe", edgenode.ErrOffline)
}

func TestMemoryStreamFailureStillReadsTheInstance(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	svc.Desktop = streamMemoryDesktop{}
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: "http://srv.dn_lab.node.invalid", AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	trip := &tokenThenOffline{}
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: trip}
	}
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-mem-stream")
	if err != nil || !adm.Queued || adm.Ack != nodeOfflineAck {
		t.Fatalf("admission = %+v %v", adm, err)
	}
	if trip.instanceReads != 1 {
		t.Fatalf("instance reads=%d", trip.instanceReads)
	}
}

func TestMemoryStreamFailureStillSendsTheParkedMessage(t *testing.T) {
	srvSvc := &turnKeySrv{}
	srv := httptest.NewServer(http.HandlerFunc(srvSvc.handler))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.queueDrainOff = true
	svc.Desktop = streamMemoryDesktop{}
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: srv.URL, AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	svc.NodeHTTP = func(string) *http.Client { return srv.Client() }
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_alice", desktopQueueItem{
		ClientMessageID: "turn-stream", Content: "打开报表", Kind: "new", WaitKind: desktopQueueWaitNode,
	}); err != nil {
		t.Fatal(err)
	}
	svc.drainDesktopQueue("tenant-a", "alice", "bot_alice")
	srvSvc.mu.Lock()
	messages := append([]string(nil), srvSvc.messages...)
	srvSvc.mu.Unlock()
	if len(messages) != 1 || !strings.Contains(messages[0], "打开报表") {
		t.Fatalf("messages = %v", messages)
	}
}

func TestDelegateToAnOfflineNodeWaitsForTheTunnel(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.Bots = append(rec.Bots, Bot{
		ID: "bot_main", Name: "main", InstanceID: "inst_main", OwnerUserID: "alice",
	})
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: "http://srv.dn_lab.node.invalid", AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	rec.CrossNotes = []BotCrossNote{{
		ID: "note_1", Owner: "alice", From: "bot_main", Target: "bot_alice",
		Kind: "delegate", Text: "打开报表",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: offlineNodeTrip{}}
	}
	job, ok := svc.pickDispatchJob("tenant-a", "alice", nil)
	if !ok || job.botID != "bot_alice" || !strings.HasPrefix(job.key, "xdel-") {
		t.Fatalf("job = %+v ok=%v", job, ok)
	}
	started, busy := svc.runDispatchJob("tenant-a", "alice", job)
	if !started || busy {
		t.Fatalf("started=%v busy=%v", started, busy)
	}
	if view := svc.DesktopQueueStatus("tenant-a", "alice", "bot_alice", job.key); view.State != "queued" {
		t.Fatalf("queue = %+v", view)
	}
	stored, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, note := range stored.CrossNotes {
		if note.ID == "note_1" && note.RunID != "" {
			t.Fatalf("delegate run was stamped before the node returned: %s", note.RunID)
		}
	}
}

func TestNodeWaitGaveUpTellsTheMainBotAndReleasesTheWake(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	start := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return start }
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.Bots = append(rec.Bots, Bot{
		ID: "bot_main", Name: "main", InstanceID: "inst_main", OwnerUserID: "alice",
		CreatedAt: "2026-01-01T00:00:00Z",
	})
	rec.PrimaryBot = map[string]string{"alice": "bot_main"}
	rec.CrossNotes = []BotCrossNote{
		{
			ID: "note_1", Owner: "alice", From: "bot_main", Target: "bot_alice",
			Kind: "delegate", Text: "打开报表", StartedBy: "hub",
			StartedAt: start.Format(time.RFC3339), CreatedAt: start.Format(time.RFC3339),
		},
		{
			ID: "note_w", Owner: "alice", From: "bot_alice", Target: "bot_main",
			Kind: "report", Text: "做完了", WokenAt: start.Format(time.RFC3339),
			CreatedAt: start.Format(time.RFC3339),
		},
		{
			ID: "note_w2", Owner: "alice", From: "bot_alice", Target: "bot_main",
			Kind: "report", Text: "还有一件", WokenAt: start.Format(time.RFC3339),
			CreatedAt: start.Format(time.RFC3339),
		},
	}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_alice", desktopQueueItem{
		ClientMessageID: "xdel-note_1", Content: "打开报表", Kind: "new",
		WaitKind: desktopQueueWaitNode, WaitSince: start,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_main", desktopQueueItem{
		ClientMessageID: "xwake-note_w", Content: "有一条新消息", Kind: "new",
		WaitKind: desktopQueueWaitNode, WaitSince: start,
		NoteIDs: []string{"note_w", "note_w2"},
	}); err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return start.Add(3 * time.Minute) }
	svc.expireNodeQueue("tenant-a", "alice", "bot_alice")
	svc.expireNodeQueue("tenant-a", "alice", "bot_main")
	stored, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	var delegate, wake, wake2 BotCrossNote
	told := false
	for _, note := range stored.CrossNotes {
		switch note.ID {
		case "note_1":
			delegate = note
		case "note_w":
			wake = note
		case "note_w2":
			wake2 = note
		}
		if note.ReplyTo == "note_1" && strings.Contains(note.Text, "还没连回来") {
			told = true
		}
	}
	if delegate.RunID != "xdel-note_1" || !told {
		t.Fatalf("delegate=%+v told=%v", delegate, told)
	}
	if wake.WokenAt != "" || wake2.WokenAt != "" {
		t.Fatalf("wake claims stayed %q %q", wake.WokenAt, wake2.WokenAt)
	}
}

func TestOfflineWakeParksEveryNote(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer public.Close()
	svc := newTurnKeyService(t, public)
	svc.queueDrainOff = true
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	rec.Bots[0].NodeID = "dn_lab"
	rec.NodeLinks = []nodeLink{{
		NodeID: "dn_lab", BaseURL: "http://srv.dn_lab.node.invalid", AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
	}}
	if err := svc.save(context.Background(), "tenant-a", rec); err != nil {
		t.Fatal(err)
	}
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: offlineNodeTrip{}}
	}
	ok, at := svc.takeWake("tenant-a", "alice", "bot_alice")
	if !ok {
		t.Fatal("wake slot")
	}
	started, busy := svc.runDispatchJob("tenant-a", "alice", dispatchJob{
		kind: "wake", botID: "bot_alice", noteIDs: []string{"note_a", "note_b"},
		content: "有一条新消息", key: "xwake-note_a", wakeAt: at,
	})
	if !started || busy {
		t.Fatalf("started=%v busy=%v", started, busy)
	}
	key := desktopQueueKey("tenant-a", "alice", "bot_alice")
	svc.mu.Lock()
	items := svc.desktopQueues[key]
	svc.mu.Unlock()
	if len(items) != 1 || len(items[0].NoteIDs) != 2 || items[0].NoteIDs[0] != "note_a" || items[0].NoteIDs[1] != "note_b" {
		t.Fatalf("parked = %+v", items)
	}
	got := 0
	for {
		ok, _ := svc.takeWake("tenant-a", "alice", "bot_alice")
		if !ok {
			break
		}
		got++
	}
	if got != wakePerHour {
		t.Fatalf("queued wake spent a slot, %d left", got)
	}
}

func TestNodeWakeSpendsItsSlotWhenItActuallyStarts(t *testing.T) {
	srvSvc := &turnKeySrv{}
	srv := httptest.NewServer(http.HandlerFunc(srvSvc.handler))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.queueDrainOff = true
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_alice", desktopQueueItem{
		ClientMessageID: "xwake-note_busy", Content: "有一条新消息", Kind: "new",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_alice", desktopQueueItem{
		ClientMessageID: "xwake-note_node", Content: "有一条新消息", Kind: "new",
		WaitKind: desktopQueueWaitNode,
	}); err != nil {
		t.Fatal(err)
	}
	svc.drainDesktopQueue("tenant-a", "alice", "bot_alice")
	got := 0
	for {
		ok, _ := svc.takeWake("tenant-a", "alice", "bot_alice")
		if !ok {
			break
		}
		got++
	}
	if got != wakePerHour-1 {
		t.Fatalf("wake slots left = %d, want %d", got, wakePerHour-1)
	}
}
