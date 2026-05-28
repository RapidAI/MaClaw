package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxConcurrentPeerProbes = 16

type Prober struct {
	svc      *Service
	interval time.Duration
	slots    chan struct{}
	mu       sync.Mutex
	running  map[string]bool
}

func NewProber(svc *Service, interval time.Duration) *Prober {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Prober{svc: svc, interval: interval, slots: make(chan struct{}, maxConcurrentPeerProbes), running: make(map[string]bool)}
}

func (p *Prober) Run(ctx context.Context) {
	if p == nil || p.svc == nil {
		return
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.probeAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probeAll(ctx)
		}
	}
}

func (p *Prober) probeAll(ctx context.Context) {
	for _, peer := range p.svc.listPeerStates() {
		peer := peer
		if !p.acquireProbeSlot() {
			continue
		}
		if !p.beginPeerProbe(peer.NodeID) {
			p.releaseProbeSlot()
			continue
		}
		go func() {
			defer p.releaseProbeSlot()
			p.probeOne(ctx, peer)
		}()
	}
}

func (p *Prober) acquireProbeSlot() bool {
	if p == nil || p.slots == nil {
		return true
	}
	select {
	case p.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (p *Prober) releaseProbeSlot() {
	if p == nil || p.slots == nil {
		return
	}
	select {
	case <-p.slots:
	default:
	}
}

func (p *Prober) beginPeerProbe(nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	if p == nil || nodeID == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running == nil {
		p.running = make(map[string]bool)
	}
	if p.running[nodeID] {
		return false
	}
	p.running[nodeID] = true
	return true
}

func (p *Prober) endPeerProbe(nodeID string) {
	nodeID = strings.TrimSpace(nodeID)
	if p == nil || nodeID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.running, nodeID)
}

func (p *Prober) probeOne(ctx context.Context, peer *PeerRuntimeState) {
	if peer != nil {
		defer p.endPeerProbe(peer.NodeID)
	}
	if peer == nil || strings.TrimSpace(peer.NodeID) == "" || strings.TrimSpace(peer.BaseURL) == "" {
		return
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peer.BaseURL+"/api/client/quality", nil)
	if err != nil {
		p.svc.markPeerError(peer.NodeID, err.Error())
		return
	}
	client := p.svc.client
	if client == nil {
		client = fallbackHAHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		p.svc.markPeerError(peer.NodeID, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		p.svc.markPeerError(peer.NodeID, fmt.Sprintf("unexpected status: %s", resp.Status))
		return
	}
	var quality ClientQualityView
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&quality); err != nil {
		p.svc.markPeerError(peer.NodeID, err.Error())
		return
	}
	p.svc.updatePeerFromQuality(peer.NodeID, time.Since(start).Milliseconds(), &quality)
}
