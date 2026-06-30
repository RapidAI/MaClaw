package maclawappcontract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DownloadGUIInstallHubPackage fetches a published Enterprise Hub MaClaw App
// package through the same HTTP shape used by the GUI install entrypoint.
// Callers verify cryptographic package signatures before applying the broader
// GUI install governance contract.
func DownloadGUIInstallHubPackage(ctx context.Context, client *http.Client, baseURL, token, capabilityID string) (map[string]any, error) {
	capabilityID = strings.TrimSpace(capabilityID)
	if capabilityID == "" {
		return nil, fmt.Errorf("capability_id is required")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("enterprise Hub marketplace URL or auth token is not configured")
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/capabilities/maclaw-apps/"+url.PathEscape(capabilityID)+"/package", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download maclaw app package from enterprise Hub failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var pkg map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}
