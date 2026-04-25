package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

type SkillMarketClient struct {
	app    *App
	client *http.Client
}

func NewSkillMarketClient(app *App) *SkillMarketClient {
	return &SkillMarketClient{
		app:    app,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *SkillMarketClient) baseURL() string {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	urls := cfg.HubCenterBaseURLs(defaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}
func (c *SkillMarketClient) selectBaseURL(ctx context.Context) (string, []string, error) {
	base, discovered, err := c.app.resolveHubCenterBaseURLCached(ctx, c.client)
	if err != nil {
		return "", nil, err
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return base, discovered, nil
}

func (c *SkillMarketClient) getSkillPurchaseMode() string {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return "auto"
	}
	mode := strings.TrimSpace(cfg.SkillPurchaseMode)
	if mode == "" {
		mode = "auto"
	}
	return mode
}

func (c *SkillMarketClient) SubmitSkill(ctx context.Context, zipPath, email string) (string, error) {
	base, discovered, err := c.selectBaseURL(ctx)
	if err != nil {
		return "", err
	}
	f, err := os.Open(zipPath)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer f.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("zip", filepath.Base(zipPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", err
	}
	_ = w.WriteField("email", email)
	w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/skills/submit", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("submit skill: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("submit failed (%d): %s", resp.StatusCode, string(body))
	}
	var result struct {
		SubmissionID string `json:"submission_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return result.SubmissionID, nil
}

func (c *SkillMarketClient) GetSubmissionStatus(ctx context.Context, submissionID string) (string, string, error) {
	var result struct {
		Status   string `json:"status"`
		ErrorMsg string `json:"error_msg"`
	}
	if _, _, err := c.app.getHubCenterJSON(ctx, c.client, "/api/v1/skill-submissions/"+submissionID, 0, &result); err != nil {
		return "", "", err
	}
	return result.Status, result.ErrorMsg, nil
}

func (c *SkillMarketClient) DownloadEncrypted(ctx context.Context, skillID, email string) ([]byte, error) {
	mode := c.getSkillPurchaseMode()
	if mode == "free_only" {
		price, err := c.getSkillPrice(ctx, skillID)
		if err == nil && price > 0 {
			return nil, fmt.Errorf("skill %s requires %d credits, skipped (free_only mode)", skillID, price)
		}
	}
	path := fmt.Sprintf("/api/v1/skillmarket/%s/download?email=%s", skillID, email)
	_, _, data, err := c.app.getHubCenterBytes(ctx, c.client, path, 0)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	return data, nil
}

type AccountInfo struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Status            string `json:"status"`
	Credits           int64  `json:"credits"`
	SettledCredits    int64  `json:"settled_credits"`
	PendingSettlement int64  `json:"pending_settlement"`
	VoucherCount      int    `json:"voucher_count"`
}

func (c *SkillMarketClient) EnsureAccount(ctx context.Context, email string) (*AccountInfo, error) {
	base, discovered, err := c.selectBaseURL(ctx)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"email": email})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/account/ensure", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info AccountInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return &info, nil
}

func (c *SkillMarketClient) GetAccountInfo(ctx context.Context, email string) (*AccountInfo, error) {
	var info AccountInfo
	if _, _, err := c.app.getHubCenterJSON(ctx, c.client, "/api/v1/account/"+email, 0, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *SkillMarketClient) SubmitRating(ctx context.Context, skillID, email string, score int) error {
	base, discovered, err := c.selectBaseURL(ctx)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]interface{}{"email": email, "score": score})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/skillmarket/"+skillID+"/rate", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("rate failed (%d): %s", resp.StatusCode, string(body))
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return nil
}

func (c *SkillMarketClient) GetPublicKey(ctx context.Context) ([]byte, error) {
	home, _ := os.UserHomeDir()
	cachePath := filepath.Join(home, ".maclaw", "skillmarket_pubkey.pem")
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		return data, nil
	}
	_, _, data, err := c.app.getHubCenterBytes(ctx, c.client, "/api/v1/crypto/pubkey", 0)
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
	_ = os.WriteFile(cachePath, data, 0o644)
	return data, nil
}

func (c *SkillMarketClient) getSkillPrice(ctx context.Context, skillID string) (int, error) {
	var result struct {
		Results []struct {
			Price int `json:"price"`
		} `json:"results"`
	}
	path := "/api/v1/skillmarket/search?q=&top_n=1&skill_id=" + skillID
	if _, _, err := c.app.getHubCenterJSON(ctx, c.client, path, 0, &result); err != nil {
		return 0, err
	}
	if len(result.Results) > 0 {
		return result.Results[0].Price, nil
	}
	return 0, nil
}

func (c *SkillMarketClient) getJSON(ctx context.Context, path string, dest interface{}) error {
	_, _, err := c.app.getHubCenterJSON(ctx, c.client, path, 0, dest)
	return err
}
