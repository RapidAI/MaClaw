package llmservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
)

func setOfficialOwnerReleaseVars(t *testing.T, threshold int, window, cooldown, excludeTTL time.Duration) {
	t.Helper()
	oldThreshold := officialOwnerReleaseThreshold
	oldWindow := officialOwnerReleaseWindow
	oldCooldown := officialOwnerReleaseCooldown
	oldExcludeTTL := officialNodeExcludeTTL
	officialOwnerReleaseThreshold = threshold
	officialOwnerReleaseWindow = window
	officialOwnerReleaseCooldown = cooldown
	officialNodeExcludeTTL = excludeTTL
	t.Cleanup(func() {
		officialOwnerReleaseThreshold = oldThreshold
		officialOwnerReleaseWindow = oldWindow
		officialOwnerReleaseCooldown = oldCooldown
		officialNodeExcludeTTL = oldExcludeTTL
	})
}

type ownerReleaseFixture struct {
	mu           sync.Mutex
	released     bool
	releaseCalls int
	releaseBody  string
}

func (f *ownerReleaseFixture) handler(t *testing.T, deadURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/llm/v1/binding/release":
			if r.Method != http.MethodPost {
				t.Errorf("release method = %s, want POST", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-Hub-ID") != "hub1" {
				t.Errorf("release headers = %#v", r.Header)
			}
			body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
			f.mu.Lock()
			f.releaseCalls++
			f.releaseBody = string(body)
			f.released = true
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"released":2}`))
			return
		case "/api/llm/v1/chat/completions":
			f.mu.Lock()
			released := f.released
			f.mu.Unlock()
			if !released {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-dead","redirect_url":"` + deadURL + `"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
			return
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
}

func TestMaClawProviderClientReleasesDeadOwnerBindingAfterThreshold(t *testing.T) {
	setOfficialOwnerReleaseVars(t, 3, time.Minute, time.Second, 5*time.Minute)

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // connection refused from now on

	fixture := &ownerReleaseFixture{}
	n1 := httptest.NewServer(fixture.handler(t, deadURL))
	defer n1.Close()
	n2 := httptest.NewServer(fixture.handler(t, deadURL))
	defer n2.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: n1.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{n1.URL, n2.URL, deadURL})

	// Requests 1..N-1: owner dead, release endpoint untouched.
	for i := 1; i <= 2; i++ {
		_, _, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
		if err == nil || !errors.Is(err, corellm.ErrOfficialOwnerUnreachable) {
			t.Fatalf("request %d err = %v, want owner unreachable", i, err)
		}
		fixture.mu.Lock()
		calls := fixture.releaseCalls
		fixture.mu.Unlock()
		if calls != 0 {
			t.Fatalf("release calls after request %d = %d, want 0", i, calls)
		}
	}

	// The dead owner must stay out of the candidate pool.
	for _, target := range client.orderedTargets("") {
		if sameHubCenterURL(target, deadURL) {
			t.Fatalf("dead owner %s still in orderedTargets %v", deadURL, client.orderedTargets(""))
		}
	}

	// Request N: threshold reached, binding released, live sibling completes it.
	body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
	if err != nil || status != http.StatusOK {
		t.Fatalf("threshold request status=%d err=%v body=%s", status, err, body)
	}
	fixture.mu.Lock()
	calls, releaseBody := fixture.releaseCalls, fixture.releaseBody
	fixture.mu.Unlock()
	if calls != 1 {
		t.Fatalf("release calls = %d, want 1", calls)
	}
	if !strings.Contains(releaseBody, `"node_id":"hc-dead"`) {
		t.Fatalf("release body = %s, want node_id hc-dead", releaseBody)
	}
}

func TestMaClawProviderClientJSON5xxDoesNotTriggerOwnerRelease(t *testing.T) {
	setOfficialOwnerReleaseVars(t, 2, time.Minute, time.Second, 5*time.Minute)

	fixture := &ownerReleaseFixture{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/llm/v1/binding/release":
			fixture.mu.Lock()
			fixture.releaseCalls++
			fixture.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"released":1}`))
			return
		case "/api/llm/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"provider unavailable"}}`))
			return
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer upstream.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: upstream.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{upstream.URL})

	for i := 0; i < 4; i++ {
		body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
		if err != nil || status != http.StatusBadGateway {
			t.Fatalf("forward %d status=%d err=%v body=%s, want bare JSON 502", i, status, err, body)
		}
		if !strings.Contains(string(body), "provider unavailable") {
			t.Fatalf("forward %d body = %s, want original provider error", i, body)
		}
	}
	fixture.mu.Lock()
	calls := fixture.releaseCalls
	fixture.mu.Unlock()
	if calls != 0 {
		t.Fatalf("release calls = %d, want 0 for JSON 5xx responses", calls)
	}
}

func TestMaClawProviderClientReleaseUnsupportedOldHubCenter(t *testing.T) {
	setOfficialOwnerReleaseVars(t, 2, time.Minute, time.Hour, 5*time.Minute)

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/llm/v1/binding/release":
			http.NotFound(w, r)
			return
		case "/api/llm/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-dead","redirect_url":"` + deadURL + `"}`))
			return
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer old.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: old.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{old.URL})

	for i := 0; i < 3; i++ {
		_, _, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
		if err == nil || !errors.Is(err, corellm.ErrOfficialOwnerUnreachable) {
			t.Fatalf("request %d err = %v, want owner unreachable", i, err)
		}
	}

	// 404 marks lastReleaseAt: the release cooldown must block further attempts
	// even though the failure count keeps growing.
	client.mu.Lock()
	st := client.ownerRelease[normalizeHubCenterURLOne(deadURL)]
	client.mu.Unlock()
	if st == nil || st.lastReleaseAt.IsZero() {
		t.Fatalf("404 release attempt must record lastReleaseAt, state = %+v", st)
	}
}

func TestNoteOwnerFailureThresholdWindowAndCooldown(t *testing.T) {
	setOfficialOwnerReleaseVars(t, 3, time.Minute, time.Hour, 5*time.Minute)

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: "https://hc.example.com", HubID: "hub1", MachineToken: "secret"})
	owner := "https://owner.example.com"

	if client.noteOwnerFailure(owner) {
		t.Fatal("first failure must not trigger release")
	}
	if client.noteOwnerFailure(owner) {
		t.Fatal("second failure must not trigger release")
	}
	if !client.noteOwnerFailure(owner) {
		t.Fatal("third failure inside window must trigger release")
	}

	// A finished release attempt stamps lastReleaseAt; the release cooldown
	// must then suppress further attempts.
	client.mu.Lock()
	client.ownerRelease[normalizeHubCenterURLOne(owner)].lastReleaseAt = time.Now()
	client.mu.Unlock()
	if client.noteOwnerFailure(owner) {
		t.Fatal("release cooldown must suppress repeated attempts")
	}

	// A concurrent release in flight must not trigger a second one.
	client.mu.Lock()
	st := client.ownerRelease[normalizeHubCenterURLOne(owner)]
	st.failTimes = []time.Time{time.Now(), time.Now(), time.Now()}
	st.lastReleaseAt = time.Time{}
	st.releasing = true
	client.mu.Unlock()
	if client.noteOwnerFailure(owner) {
		t.Fatal("concurrent release in flight must not trigger a second one")
	}

	// Failures older than the window fall off.
	client.mu.Lock()
	st.releasing = false
	st.failTimes = []time.Time{time.Now().Add(-2 * time.Minute), time.Now().Add(-90 * time.Second)}
	client.mu.Unlock()
	if client.noteOwnerFailure(owner) {
		t.Fatal("stale failures outside the window must not count toward the threshold")
	}
}

func TestReleaseOwnerBindingDecodesReleasedCount(t *testing.T) {
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/binding/release" {
			t.Errorf("path = %s", r.URL.Path)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		var payload struct {
			NodeID string `json:"node_id"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.NodeID != "hc-dead" {
			t.Errorf("release body = %s, want node_id hc-dead", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"released":2}`))
	}))
	defer live.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: live.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{live.URL})
	if !client.releaseOwnerBinding(context.Background(), "hc-dead", "https://dead.example.com") {
		t.Fatal("releaseOwnerBinding() = false, want true")
	}
}

func setRecoverProbeInterval(t *testing.T, interval time.Duration) {
	t.Helper()
	old := officialNodeRecoverProbeInterval
	officialNodeRecoverProbeInterval = interval
	t.Cleanup(func() { officialNodeRecoverProbeInterval = old })
}

func TestExcludedNodeReAdmittedAfterRecoveryProbe(t *testing.T) {
	setRecoverProbeInterval(t, 10*time.Millisecond)

	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer live.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: live.URL, HubID: "hub1", MachineToken: "secret"})
	t.Cleanup(func() { _ = client.Close() })

	// Seed release failure history first, then exclude the node: this ordering
	// keeps the probe loop from observing the cooldown before the seeding.
	client.noteOwnerFailure(live.URL)
	client.noteOwnerFailure(live.URL)
	client.markOwnerUnreachable(live.URL)
	client.mu.RLock()
	_, cooling := client.ownerExcluded[live.URL]
	client.mu.RUnlock()
	if !cooling {
		t.Fatal("precondition: node must be excluded")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		client.mu.RLock()
		_, cooling = client.ownerExcluded[live.URL]
		st := client.ownerRelease[live.URL]
		failTimes := 0
		if st != nil {
			failTimes = len(st.failTimes)
		}
		client.mu.RUnlock()
		if !cooling {
			if failTimes != 0 {
				t.Fatalf("failTimes = %d, want cleared after re-admission", failTimes)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("excluded but reachable node was not re-admitted")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExcludedDeadNodeStaysExcludedAfterFailedRecoveryProbe(t *testing.T) {
	setRecoverProbeInterval(t, 10*time.Millisecond)

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // connection refused from now on

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: deadURL, HubID: "hub1", MachineToken: "secret"})
	t.Cleanup(func() { _ = client.Close() })
	client.markOwnerUnreachable(deadURL)

	time.Sleep(150 * time.Millisecond) // let several probe rounds run
	client.mu.RLock()
	_, cooling := client.ownerExcluded[deadURL]
	client.mu.RUnlock()
	if !cooling {
		t.Fatal("dead node must stay excluded while probes keep failing")
	}
}

func TestCloseStopsRecoverProbeLoop(t *testing.T) {
	setRecoverProbeInterval(t, 10*time.Millisecond)

	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer live.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: live.URL, HubID: "hub1", MachineToken: "secret"})
	client.noteOwnerFailure(live.URL)
	client.markOwnerUnreachable(live.URL)
	if err := client.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	client.mu.RLock()
	_, cooling := client.ownerExcluded[live.URL]
	client.mu.RUnlock()
	if !cooling {
		t.Fatal("precondition: node must still be excluded right after Close")
	}
	time.Sleep(100 * time.Millisecond)
	client.mu.RLock()
	_, cooling = client.ownerExcluded[live.URL]
	client.mu.RUnlock()
	if !cooling {
		t.Fatal("probe loop kept running after Close")
	}
}

func TestReleaseOwnerBindingFallsThrough404ToNextCandidate(t *testing.T) {
	oldCalls, newCalls := 0, 0
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/binding/release" {
			t.Errorf("path = %s", r.URL.Path)
			return
		}
		oldCalls++
		http.NotFound(w, r)
	}))
	defer old.Close()
	modern := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/binding/release" {
			t.Errorf("path = %s", r.URL.Path)
			return
		}
		newCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"released":1}`))
	}))
	defer modern.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: old.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{old.URL, modern.URL})
	if !client.releaseOwnerBinding(context.Background(), "hc-dead", "https://dead.example.com") {
		t.Fatal("releaseOwnerBinding() = false, want true via modern candidate")
	}
	if oldCalls != 1 || newCalls != 1 {
		t.Fatalf("release calls old=%d new=%d, want 1/1 (404 must fall through)", oldCalls, newCalls)
	}
}

func TestReleaseOwnerBindingConcurrentCallsDeduplicated(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/binding/release" {
			t.Errorf("path = %s", r.URL.Path)
			return
		}
		mu.Lock()
		calls++
		mu.Unlock()
		time.Sleep(100 * time.Millisecond) // widen the overlap window
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"released":1}`))
	}))
	defer live.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: live.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{live.URL})

	const goroutines = 8
	var wg sync.WaitGroup
	results := make([]bool, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = client.releaseOwnerBinding(context.Background(), "hc-dead", "https://dead.example.com")
		}(i)
	}
	wg.Wait()

	if calls != 1 {
		t.Fatalf("release endpoint calls = %d, want 1 (releasing flag must dedupe)", calls)
	}
	successes := 0
	for _, ok := range results {
		if ok {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful releases = %d, want 1", successes)
	}
}

func TestProbeLoopDoesNotReAdmitPoolUnhealthyNode(t *testing.T) {
	setRecoverProbeInterval(t, 10*time.Millisecond)

	oldCooldown := officialOwnerCooldown
	officialOwnerCooldown = 30 * time.Second
	t.Cleanup(func() { officialOwnerCooldown = oldCooldown })

	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer live.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: live.URL, HubID: "hub1", MachineToken: "secret"})
	t.Cleanup(func() { _ = client.Close() })
	client.SetHubCenterCandidates([]string{live.URL})

	// Pool-wide failure: short soft skip, the node process is still healthy.
	client.markOwnerPoolUnhealthy(live.URL)
	time.Sleep(200 * time.Millisecond) // several probe rounds

	now := time.Now()
	client.mu.Lock()
	cooling := client.coolingDownLocked(live.URL, now)
	_, excluded := client.ownerExcluded[live.URL]
	client.mu.Unlock()
	if !cooling {
		t.Fatal("pool-unhealthy node must stay soft-skipped for the pool cooldown")
	}
	if excluded {
		t.Fatal("pool-unhealthy node must not be marked as unreachable")
	}
}

func TestOwnerReleaseSucceedsWithNoRemainingCandidates(t *testing.T) {
	setOfficialOwnerReleaseVars(t, 1, time.Minute, time.Second, 5*time.Minute)

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	releaseCalls := 0
	single := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/llm/v1/binding/release":
			releaseCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"released":1}`))
			return
		case "/api/llm/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-dead","redirect_url":"` + deadURL + `"}`))
			return
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer single.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: single.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{single.URL})

	_, _, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
	if err == nil || !errors.Is(err, corellm.ErrOfficialOwnerUnreachable) {
		t.Fatalf("err = %v, want owner unreachable when no candidate remains", err)
	}
	if releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", releaseCalls)
	}
	// The released dead owner stays excluded even though the request failed.
	now := time.Now()
	client.mu.Lock()
	excluded := client.excludedLocked(deadURL, now)
	client.mu.Unlock()
	if !excluded {
		t.Fatal("dead owner must remain excluded after release with no remaining candidates")
	}
}

func TestProbeLoopPurgesExpiredExclusionEntries(t *testing.T) {
	setRecoverProbeInterval(t, 10*time.Millisecond)
	// Tiny exclusion TTL: the entry expires almost immediately; the probe loop
	// must actively purge it (expired entries are otherwise only deleted when
	// some request happens to query that exact URL).
	setOfficialOwnerReleaseVars(t, 3, time.Minute, time.Minute, 50*time.Millisecond)

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: "https://gone.example.com", HubID: "hub1", MachineToken: "secret"})
	t.Cleanup(func() { _ = client.Close() })

	gone := "https://removed-node.example.com"
	client.SetHubCenterCandidates([]string{"https://gone.example.com"})
	client.markOwnerUnreachable(gone)
	client.mu.RLock()
	_, excluded := client.ownerExcluded[gone]
	client.mu.RUnlock()
	if !excluded {
		t.Fatal("precondition: node must be excluded")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		client.mu.RLock()
		_, excluded = client.ownerExcluded[gone]
		_, cooling := client.ownerCooldown[gone]
		client.mu.RUnlock()
		if !excluded && !cooling {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expired exclusion entry was not purged by the probe loop")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReleaseOwnerBindingSurvivesCallerCancel(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/binding/release" {
			t.Errorf("path = %s", r.URL.Path)
			return
		}
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"released":1}`))
	}))
	defer live.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: live.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{live.URL})

	// A user request canceled while the release is in flight must not lose the
	// release side effect: the POST still lands on HubCenter. The short-circuit
	// that skips releasing for canceled requests lives one layer up, in
	// failRequiredOwnerUnlessCanceled; once releaseOwnerBinding runs, the
	// caller's cancellation must not abort it.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !client.releaseOwnerBinding(ctx, "hc-dead", "https://dead.example.com") {
		t.Fatal("releaseOwnerBinding() = false, want true despite canceled caller ctx")
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("release endpoint calls = %d, want 1", calls)
	}
}

func TestExcludedNodeWinsOverTenantPin(t *testing.T) {
	ownerHits, siblingHits := 0, 0
	owner := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		ownerHits++
	}))
	defer owner.Close()
	sibling := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siblingHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"sibling"}}]}`))
	}))
	defer sibling.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: owner.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{owner.URL, sibling.URL})

	// Pin the tenant to the owner via a successful request, then force the pin
	// to outlive exclusion by excluding without going through
	// markOwnerUnreachable (which would delete the pin).
	if _, _, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a"); err != nil {
		t.Fatalf("pinning forward: %v", err)
	}
	if got := client.TenantHubCenterURL("tenant_a"); !sameHubCenterURL(got, owner.URL) {
		t.Fatalf("precondition: pin = %q, want %q", got, owner.URL)
	}
	now := time.Now()
	client.mu.Lock()
	if client.ownerExcluded == nil {
		client.ownerExcluded = map[string]time.Time{}
	}
	client.ownerExcluded[normalizeHubCenterURLOne(owner.URL)] = now.Add(5 * time.Minute)
	client.mu.Unlock()

	body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
	if err != nil || status != http.StatusOK {
		t.Fatalf("Forward() status=%d err=%v body=%s", status, err, body)
	}
	if ownerHits != 1 {
		t.Fatalf("owner hits = %d, want 1 (excluded node must not receive pinned traffic)", ownerHits)
	}
	if siblingHits != 1 {
		t.Fatalf("sibling hits = %d, want 1", siblingHits)
	}
}
