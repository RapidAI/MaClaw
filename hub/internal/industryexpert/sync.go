package industryexpert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SyncService pulls full tenant catalogues from HubCenter.  It is intentionally
// pull-based: HubCenter only needs to expose a durable authenticated snapshot,
// while Hubs can keep their last verified catalogue during outages.
type SyncService struct {
	Store       *Store
	BaseURL     func(context.Context) (string, error)
	Credentials func(context.Context) (hubID, secret string, err error)
	Tenants     func(context.Context) ([]string, error)
	Client      *http.Client
}

func (s *SyncService) SyncAll(ctx context.Context) {
	if s == nil || s.Store == nil || s.Tenants == nil {
		return
	}
	tenants, err := s.Tenants(ctx)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, tenantID := range tenants {
		tenantID = normalizeTenantID(tenantID)
		if seen[tenantID] {
			continue
		}
		seen[tenantID] = true
		if err := s.SyncTenant(ctx, tenantID); err != nil {
			s.Store.MarkFailure(ctx, tenantID, err)
		}
	}
}

func (s *SyncService) SyncTenant(ctx context.Context, tenantID string) error {
	if s == nil || s.Store == nil || s.BaseURL == nil || s.Credentials == nil {
		return fmt.Errorf("managed industry expert sync unavailable")
	}
	baseURL, err := s.BaseURL(ctx)
	if err != nil {
		return err
	}
	hubID, secret, err := s.Credentials(ctx)
	if err != nil {
		return err
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	hubID = strings.TrimSpace(hubID)
	secret = strings.TrimSpace(secret)
	if baseURL == "" || hubID == "" || secret == "" {
		return fmt.Errorf("HubCenter registration unavailable")
	}
	path := "/api/hubs/" + url.PathEscape(hubID) + "/tenants/" + url.PathEscape(normalizeTenantID(tenantID)) + "/industry-expert-catalog"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Accept", "application/json")
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HubCenter managed industry catalogue: %s", resp.Status)
	}
	var catalog Catalog
	if err := json.Unmarshal(bytes.TrimSpace(body), &catalog); err != nil {
		return fmt.Errorf("parse managed industry catalogue: %w", err)
	}
	return s.Store.Replace(ctx, tenantID, catalog)
}
