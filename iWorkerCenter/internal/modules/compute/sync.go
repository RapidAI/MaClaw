package compute

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrWaitingForCredentials indicates sync is configured but Center registration credentials are not ready yet.
var ErrWaitingForCredentials = errors.New("center credentials are not configured")

// ComputeProvider mirrors the iWorkerCloud compute provider configuration.
type ComputeProvider struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	BaseURL              string  `json:"base_url"`
	APIKey               string  `json:"api_key,omitempty"`
	HasAPIKey            bool    `json:"has_api_key,omitempty"`
	Protocol             string  `json:"protocol"`
	UserAgent            string  `json:"user_agent"`
	ComputeType          string  `json:"compute_type"`
	Model                string  `json:"model"`
	Enabled              bool    `json:"enabled"`
	Priority             int     `json:"priority"`
	Description          string  `json:"description"`
	InputPricePerMToken  float64 `json:"input_price_per_mtoken"`
	OutputPricePerMToken float64 `json:"output_price_per_mtoken"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

// ComputeSyncStatus tracks the state of provider configuration sync.
type ComputeSyncStatus struct {
	LastSyncAt    string `json:"last_sync_at"`
	Status        string `json:"status"` // success | failure | pending | waiting_for_credentials
	Error         string `json:"error,omitempty"`
	ProviderCount int    `json:"provider_count"`
}

// CredentialResolver resolves the current Cloud center credentials before each sync.
type CredentialResolver func() (centerID, centerSecret string)

// syncResponse is the JSON shape returned by the Cloud compute-providers API.
type syncResponse struct {
	Providers         []ComputeProvider `json:"providers"`
	ComputePermission bool              `json:"compute_permission"`
	ForceSync         bool              `json:"force_sync"`
}

// SyncManager periodically pulls LLM provider configurations from iWorkerCloud.
// It is safe for concurrent use.
type SyncManager struct {
	cloudURL     string // iWorkerCloud base URL
	centerID     string // this center's ID
	centerSecret string // this center's secret

	resolveCredentials CredentialResolver

	mu                sync.RWMutex
	providers         []ComputeProvider
	syncStatus        ComputeSyncStatus
	computePermission bool
	forceSync         bool

	stopCh   chan struct{}
	client   *http.Client
	interval time.Duration // polling interval (default 5 min)
}

// NewSyncManager creates a SyncManager that polls the given iWorkerCloud URL.
func NewSyncManager(cloudURL, centerID, centerSecret string) *SyncManager {
	return &SyncManager{
		cloudURL:     strings.TrimRight(cloudURL, "/"),
		centerID:     centerID,
		centerSecret: centerSecret,
		syncStatus:   ComputeSyncStatus{Status: "pending"},
		stopCh:       make(chan struct{}),
		client:       &http.Client{Timeout: 15 * time.Second},
		interval:     5 * time.Minute,
	}
}

// NewSyncManagerWithResolver creates a SyncManager that resolves credentials dynamically.
func NewSyncManagerWithResolver(cloudURL string, resolve CredentialResolver) *SyncManager {
	sm := NewSyncManager(cloudURL, "", "")
	sm.resolveCredentials = resolve
	return sm
}

// IsConfigured returns true if the SyncManager has a cloud URL and center ID.
func (sm *SyncManager) IsConfigured() bool {
	if sm.cloudURL == "" {
		return false
	}
	if sm.centerID != "" && sm.centerSecret != "" {
		return true
	}
	return sm.resolveCredentials != nil
}

// Start launches a background goroutine that syncs every 5 minutes.
// It performs an immediate sync on startup.
func (sm *SyncManager) Start() {
	go sm.loop()
}

// Stop signals the background goroutine to exit.
func (sm *SyncManager) Stop() {
	select {
	case <-sm.stopCh:
		// already stopped
	default:
		close(sm.stopCh)
	}
}

// SyncNow triggers an immediate sync and returns any error.
func (sm *SyncManager) SyncNow() error {
	return sm.doSync()
}

// GetProviders returns the current provider list (thread-safe copy).
func (sm *SyncManager) GetProviders() []ComputeProvider {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	out := make([]ComputeProvider, len(sm.providers))
	copy(out, sm.providers)
	return out
}

// GetSyncStatus returns the current sync status.
func (sm *SyncManager) GetSyncStatus() ComputeSyncStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.syncStatus
}

// GetComputePermission returns whether this center has compute self-management permission.
func (sm *SyncManager) GetComputePermission() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.computePermission
}

// HasForceSync returns true if the last sync response included force_sync.
// Reading this flag also clears it.
func (sm *SyncManager) HasForceSync() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	v := sm.forceSync
	sm.forceSync = false
	return v
}

// loop runs the periodic sync until Stop is called.
func (sm *SyncManager) loop() {
	// Immediate first sync.
	if err := sm.doSync(); err != nil {
		if !errors.Is(err, ErrWaitingForCredentials) {
			log.Printf("[compute/sync] initial sync failed: %v", err)
		}
	}

	ticker := time.NewTicker(sm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopCh:
			return
		case <-ticker.C:
			if err := sm.doSync(); err != nil {
				if !errors.Is(err, ErrWaitingForCredentials) {
					log.Printf("[compute/sync] sync failed: %v", err)
				}
			}
		}
	}
}

func (sm *SyncManager) currentCredentials() (string, string) {
	if sm.resolveCredentials != nil {
		centerID, centerSecret := sm.resolveCredentials()
		if centerID != "" || centerSecret != "" {
			return strings.TrimSpace(centerID), centerSecret
		}
	}
	return strings.TrimSpace(sm.centerID), sm.centerSecret
}

// doSync performs a single sync request to iWorkerCloud.
func (sm *SyncManager) doSync() error {
	centerID, centerSecret := sm.currentCredentials()
	if sm.cloudURL == "" {
		err := fmt.Errorf("cloud URL is not configured")
		sm.recordFailure(err)
		return err
	}
	if centerID == "" || centerSecret == "" {
		err := ErrWaitingForCredentials
		sm.recordWaitingForCredentials(err)
		return err
	}
	url := fmt.Sprintf("%s/api/centers/%s/compute-providers", sm.cloudURL, centerID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		sm.recordFailure(err)
		return err
	}
	req.Header.Set("X-Center-Secret", centerSecret)

	resp, err := sm.client.Do(req)
	if err != nil {
		sm.recordFailure(err)
		return fmt.Errorf("sync request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		syncErr := fmt.Errorf("sync: status %d, body: %s", resp.StatusCode, string(body))
		sm.recordFailure(syncErr)
		return syncErr
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
	if err != nil {
		sm.recordFailure(err)
		return fmt.Errorf("read sync response: %w", err)
	}

	var sr syncResponse
	if err := json.Unmarshal(data, &sr); err != nil {
		sm.recordFailure(err)
		return fmt.Errorf("decode sync response: %w", err)
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.providers = sr.Providers
	sm.computePermission = sr.ComputePermission
	sm.forceSync = sr.ForceSync
	sm.syncStatus = ComputeSyncStatus{
		LastSyncAt:    time.Now().UTC().Format(time.RFC3339),
		Status:        "success",
		ProviderCount: len(sr.Providers),
	}

	log.Printf("[compute/sync] synced %d providers from cloud", len(sr.Providers))
	return nil
}

func (sm *SyncManager) recordWaitingForCredentials(err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.syncStatus = ComputeSyncStatus{
		LastSyncAt:    time.Now().UTC().Format(time.RFC3339),
		Status:        "waiting_for_credentials",
		Error:         err.Error(),
		ProviderCount: len(sm.providers),
	}
}

// recordFailure updates syncStatus with an error while preserving existing providers.
func (sm *SyncManager) recordFailure(err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.syncStatus = ComputeSyncStatus{
		LastSyncAt:    time.Now().UTC().Format(time.RFC3339),
		Status:        "failure",
		Error:         err.Error(),
		ProviderCount: len(sm.providers), // keep previous count
	}
}
