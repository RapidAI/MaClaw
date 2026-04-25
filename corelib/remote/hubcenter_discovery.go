package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HubCenterNode describes one reachable HubCenter address advertised by a peer node.
type HubCenterNode struct {
	FQDN     string `json:"fqdn,omitempty"`
	NodeID   string `json:"node_id,omitempty"`
	NodeName string `json:"node_name,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	Current  bool   `json:"current,omitempty"`
}

// HubCenterDiscoveryView mirrors GET /api/client/hubcenters from hubcenter.
type HubCenterDiscoveryView struct {
	OK         bool            `json:"ok"`
	URLs       []string        `json:"urls"`
	Nodes      []HubCenterNode `json:"nodes"`
	Count      int             `json:"count"`
	TTLSeconds int             `json:"ttl_seconds"`
	ServerTime time.Time       `json:"server_time"`
}

// NormalizeHubCenterURL normalizes a candidate hubcenter URL for comparison and requests.
func NormalizeHubCenterURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	return strings.TrimRight(value, "/")
}

// NormalizeHubCenterURLs deduplicates hubcenter URLs while preserving first-seen order.
func NormalizeHubCenterURLs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := NormalizeHubCenterURL(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

// FetchHubCenterDiscovery loads the current effective HubCenter address list from one node.
func FetchHubCenterDiscovery(ctx context.Context, client *http.Client, baseURL string) (*HubCenterDiscoveryView, error) {
	baseURL = NormalizeHubCenterURL(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("hubcenter base url is empty")
	}
	if client == nil {
		client = http.DefaultClient
	}
	requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, baseURL+"/api/client/hubcenters", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hubcenter discovery failed with status %d", resp.StatusCode)
	}
	var view HubCenterDiscoveryView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		return nil, err
	}
	view.URLs = NormalizeHubCenterURLs(view.URLs)
	if len(view.URLs) == 0 && len(view.Nodes) > 0 {
		fallback := make([]string, 0, len(view.Nodes))
		for _, node := range view.Nodes {
			fallback = append(fallback, node.BaseURL)
		}
		view.URLs = NormalizeHubCenterURLs(fallback)
	}
	view.Count = len(view.URLs)
	return &view, nil
}

// DiscoverHubCenterURLs merges local seed URLs with a live list fetched from any reachable HubCenter node.
// The merged result is re-ranked by health so clients can refresh their local fallback list.
func DiscoverHubCenterURLs(ctx context.Context, client *http.Client, seedURLs []string, preferred string) []string {
	seeds := NormalizeHubCenterURLs(seedURLs)
	if len(seeds) == 0 {
		return nil
	}
	if client == nil {
		client = http.DefaultClient
	}
	orderedSeeds := SelectBestCenter(ctx, client, seeds, preferred)
	if len(orderedSeeds) == 0 {
		orderedSeeds = seeds
	}
	merged := append([]string(nil), orderedSeeds...)
	for _, seed := range orderedSeeds {
		view, err := FetchHubCenterDiscovery(ctx, client, seed)
		if err != nil || view == nil {
			continue
		}
		merged = append(merged, view.URLs...)
		for _, node := range view.Nodes {
			merged = append(merged, node.BaseURL)
		}
		break
	}
	merged = NormalizeHubCenterURLs(merged)
	if len(merged) <= 1 {
		return merged
	}
	// Skip the second probe if discovery didn't find any new URLs beyond the seeds.
	// This avoids a redundant concurrent probe round that can add 3-10s latency.
	if !hasNewURLs(orderedSeeds, merged) {
		return orderedSeeds
	}
	return SelectBestCenter(ctx, client, merged, preferred)
}

// hasNewURLs returns true if merged contains URLs not present in base.
func hasNewURLs(base, merged []string) bool {
	set := make(map[string]struct{}, len(base))
	for _, u := range base {
		set[NormalizeHubCenterURL(u)] = struct{}{}
	}
	for _, u := range merged {
		if _, ok := set[NormalizeHubCenterURL(u)]; !ok {
			return true
		}
	}
	return false
}
