package llmservice

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

// AuthSyncClient periodically syncs tenant authorization status from HubCenter.
// Runs as a background goroutine integrated into Hub's heartbeat cycle.
type AuthSyncClient struct {
	client       *MaClawProviderClient
	accessCtrl   *TenantLLMAccessControl
	tenantIDs    func() []string // returns current tenant IDs to sync
	interval     time.Duration

	mu       sync.Mutex
	lastSync time.Time
	cancel   context.CancelFunc
}

// NewAuthSyncClient creates a sync client that periodically refreshes
// tenant authorization status from HubCenter.
func NewAuthSyncClient(
	client *MaClawProviderClient,
	accessCtrl *TenantLLMAccessControl,
	tenantIDs func() []string,
) *AuthSyncClient {
	return &AuthSyncClient{
		client:     client,
		accessCtrl: accessCtrl,
		tenantIDs:  tenantIDs,
		interval:   2 * time.Minute, // sync every 2 minutes
	}
}

// Start begins the background sync loop.
func (s *AuthSyncClient) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.syncLoop(ctx)
}

// Stop terminates the background sync loop.
func (s *AuthSyncClient) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// SyncNow triggers an immediate sync (e.g., called from heartbeat handler).
func (s *AuthSyncClient) SyncNow(ctx context.Context) {
	s.mu.Lock()
	s.lastSync = time.Now()
	s.mu.Unlock()
	s.doSync(ctx)
}

func (s *AuthSyncClient) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.doSync(ctx)
		}
	}
}

func (s *AuthSyncClient) doSync(ctx context.Context) {
	if s.client == nil || s.accessCtrl == nil || s.tenantIDs == nil {
		return
	}
	tenants := s.tenantIDs()
	for _, tid := range tenants {
		// Respect context cancellation between tenants
		if ctx.Err() != nil {
			return
		}
		status, err := s.client.QueryAuthorization(ctx, tid)
		if err != nil {
			continue // skip this tenant, try next
		}
		s.accessCtrl.UpdateFromHeartbeat(tid, status)
	}
}

// ---------------------------------------------------------------------------
// Heartbeat extension: sync authorization data in heartbeat response
// ---------------------------------------------------------------------------

// HeartbeatAuthPayload is included in Hub→HubCenter heartbeat to request
// authorization status for all tenants.
type HeartbeatAuthPayload struct {
	TenantIDs []string `json:"tenant_ids"`
}

// HeartbeatAuthResponse is the authorization portion of the heartbeat response.
type HeartbeatAuthResponse struct {
	Tenants map[string]*TenantAuthorizationStatus `json:"tenants,omitempty"`
}

// FetchAuthorizationBatch queries HubCenter for multiple tenants at once.
// More efficient than per-tenant queries when called during heartbeat.
func FetchAuthorizationBatch(ctx context.Context, hubCenterURL, machineToken, hubID string, tenantIDs []string) (map[string]*TenantAuthorizationStatus, error) {
	if hubCenterURL == "" || len(tenantIDs) == 0 {
		return nil, nil
	}

	endpoint := strings.TrimRight(hubCenterURL, "/") + "/api/llm/v1/authorization/batch"
	payload, _ := json.Marshal(HeartbeatAuthPayload{TenantIDs: tenantIDs})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+machineToken)
	req.Header.Set("X-Hub-ID", hubID)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("batch authorization request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("batch authorization HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result HeartbeatAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse batch authorization: %w", err)
	}
	return result.Tenants, nil
}
