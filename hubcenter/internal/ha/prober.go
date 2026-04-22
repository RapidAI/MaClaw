package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Prober struct {
	svc      *Service
	interval time.Duration
}

func NewProber(svc *Service, interval time.Duration) *Prober {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Prober{svc: svc, interval: interval}
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
		go p.probeOne(ctx, peer)
	}
}

func (p *Prober) probeOne(ctx context.Context, peer *PeerRuntimeState) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peer.BaseURL+"/api/client/quality", nil)
	if err != nil {
		p.svc.markPeerError(peer.NodeID, err.Error())
		return
	}
	resp, err := p.svc.client.Do(req)
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
	if err := json.NewDecoder(resp.Body).Decode(&quality); err != nil {
		p.svc.markPeerError(peer.NodeID, err.Error())
		return
	}
	p.svc.updatePeerFromQuality(peer.NodeID, time.Since(start).Milliseconds(), &quality)
}
