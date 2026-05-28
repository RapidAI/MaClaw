package ha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestProberSkipsPeerAlreadyRunning(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		switch n {
		case 1:
			close(firstStarted)
			<-releaseFirst
		case 2:
			close(secondStarted)
		}
		_ = json.NewEncoder(w).Encode(&ClientQualityView{OK: true, ServiceStatus: "healthy", QualityScore: 100})
	}))
	defer server.Close()

	svc := NewService("hc-1", "hc-1", "", "secret", []StaticPeer{{NodeID: "hc-2", NodeName: "hc-2", BaseURL: server.URL}})
	prober := NewProber(svc, time.Second)

	prober.probeAll(context.Background())
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first probe did not start")
	}
	prober.probeAll(context.Background())
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	gotDuringFirst := requests
	mu.Unlock()
	if gotDuringFirst != 1 {
		t.Fatalf("requests while first probe is running = %d, want 1", gotDuringFirst)
	}

	close(releaseFirst)
	deadline := time.After(time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		prober.probeAll(context.Background())
		select {
		case <-secondStarted:
			return
		case <-ticker.C:
			continue
		case <-deadline:
			t.Fatal("second probe did not start after first completed")
		}
	}
}

func TestProberCapsConcurrentPeerProbes(t *testing.T) {
	var mu sync.Mutex
	active := 0
	maxActive := 0
	requests := 0
	started := make(chan struct{}, 5)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		requests++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		started <- struct{}{}
		<-release
		mu.Lock()
		active--
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(&ClientQualityView{OK: true, ServiceStatus: "healthy", QualityScore: 100})
	}))
	defer server.Close()

	peers := []StaticPeer{
		{NodeID: "hc-2", NodeName: "hc-2", BaseURL: server.URL},
		{NodeID: "hc-3", NodeName: "hc-3", BaseURL: server.URL},
		{NodeID: "hc-4", NodeName: "hc-4", BaseURL: server.URL},
		{NodeID: "hc-5", NodeName: "hc-5", BaseURL: server.URL},
		{NodeID: "hc-6", NodeName: "hc-6", BaseURL: server.URL},
	}
	svc := NewService("hc-1", "hc-1", "", "secret", peers)
	prober := NewProber(svc, time.Second)
	prober.slots = make(chan struct{}, 2)

	prober.probeAll(context.Background())
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("probe did not reach concurrency cap")
		}
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	gotRequests := requests
	gotMaxActive := maxActive
	mu.Unlock()
	if gotRequests != 2 {
		t.Fatalf("requests before releasing slots = %d, want 2", gotRequests)
	}
	if gotMaxActive > 2 {
		t.Fatalf("max active probes = %d, want <= 2", gotMaxActive)
	}
	close(release)
}

func TestProberRejectsOversizedQualityResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"service_status":"healthy","quality_score":100,"padding":"`))
		for i := 0; i < (1<<20)+1; i++ {
			_, _ = w.Write([]byte("x"))
		}
		_, _ = w.Write([]byte(`"}`))
	}))
	defer server.Close()

	svc := NewService("hc-1", "hc-1", "", "secret", []StaticPeer{{NodeID: "hc-2", NodeName: "hc-2", BaseURL: server.URL}})
	prober := NewProber(svc, time.Second)
	prober.probeOne(context.Background(), svc.listPeerStates()[0])

	peer := svc.listPeerStates()[0]
	if peer.Reachable || peer.LastError == "" {
		t.Fatalf("peer after oversized response = %+v, want unreachable with error", peer)
	}
}
