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
)

// SkillMarketClient 与 HubCenter SkillMarket API 交互。
type SkillMarketClient struct {
	app    *App
	client *http.Client
}

// NewSkillMarketClient 创建 SkillMarketClient。
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
	url := strings.TrimSpace(cfg.RemoteHubCenterURL)
	if url == "" {
		url = defaultRemoteHubCenterURL
	}
	return strings.TrimRight(url, "/")
}

// getSkillPurchaseMode 返回 Skill获取策略配置，默认 "auto"。
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

// ── Submit / Upload ─────────────────────────────────────────────────────

// SubmitSkill 上传 Skill zip 包到 SkillMarket。
func (c *SkillMarketClient) SubmitSkill(ctx context.Context, zipPath, email string) (string, error) {
	base := c.baseURL()
	if base == "" {
		return "", fmt.Errorf("hubcenter URL not configured")
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

	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/v1/skills/submit", &buf)
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
	return result.SubmissionID, nil
}

// GetSubmissionStatus 查询提交状态。
func (c *SkillMarketClient) GetSubmissionStatus(ctx context.Context, submissionID string) (string, string, error) {
	base := c.baseURL()
	if base == "" {
		return "", "", fmt.Errorf("hubcenter URL not configured")
	}
	var result struct {
		Status   string `json:"status"`
		ErrorMsg string `json:"error_msg"`
	}
	if err := c.getJSON(ctx, base+"/api/v1/skill-submissions/"+submissionID, &result); err != nil {
		return "", "", err
	}
	return result.Status, result.ErrorMsg, nil
}

// ── Download (Encrypted) ────────────────────────────────────────────────

// DownloadEncrypted 下载加密的 Skill 包。
// free_only 模式下，付费 Skill 会被跳过。
func (c *SkillMarketClient) DownloadEncrypted(ctx context.Context, skillID, email string) ([]byte, error) {
	base := c.baseURL()
	if base == "" {
		return nil, fmt.Errorf("hubcenter URL not configured")
	}

	// free_only 模式检查：先查询价格，付费则跳过
	mode := c.getSkillPurchaseMode()
	if mode == "free_only" {
		price, err := c.getSkillPrice(ctx, skillID)
		if err == nil && price > 0 {
			return nil, fmt.Errorf("skill %s requires %d credits, skipped (free_only mode)", skillID, price)
		}
	}

	url := fmt.Sprintf("%s/api/v1/skillmarket/%s/download?email=%s", base, skillID, email)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download failed (%d): %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

// ── Account ─────────────────────────────────────────────────────────────

// AccountInfo 账户信息。
type AccountInfo struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Status            string `json:"status"`
	Credits           int64  `json:"credits"`
	SettledCredits    int64  `json:"settled_credits"`
	PendingSettlement int64  `json:"pending_settlement"`
	VoucherCount      int    `json:"voucher_count"`
}

// EnsureAccount 确保账户存在。
func (c *SkillMarketClient) EnsureAccount(ctx context.Context, email string) (*AccountInfo, error) {
	base := c.baseURL()
	if base == "" {
		return nil, fmt.Errorf("hubcenter URL not configured")
	}
	payload, _ := json.Marshal(map[string]string{"email": email})
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/v1/account/ensure", bytes.NewReader(payload))
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
	return &info, nil
}

// GetAccountInfo 获取账户信息。
func (c *SkillMarketClient) GetAccountInfo(ctx context.Context, email string) (*AccountInfo, error) {
	base := c.baseURL()
	if base == "" {
		return nil, fmt.Errorf("hubcenter URL not configured")
	}
	var info AccountInfo
	if err := c.getJSON(ctx, base+"/api/v1/account/"+email, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ── Rating ──────────────────────────────────────────────────────────────

// SubmitRating 提交评分。
func (c *SkillMarketClient) SubmitRating(ctx context.Context, skillID, email string, score int) error {
	base := c.baseURL()
	if base == "" {
		return fmt.Errorf("hubcenter URL not configured")
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"email": email,
		"score": score,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/v1/skillmarket/"+skillID+"/rate", bytes.NewReader(payload))
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
	return nil
}

// ── Public Key ──────────────────────────────────────────────────────────

// GetPublicKey 获取 HubCenter RSA 公钥并缓存到本地。
func (c *SkillMarketClient) GetPublicKey(ctx context.Context) ([]byte, error) {
	// 先检查本地缓存
	home, _ := os.UserHomeDir()
	cachePath := filepath.Join(home, ".maclaw", "skillmarket_pubkey.pem")
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		return data, nil
	}

	base := c.baseURL()
	if base == "" {
		return nil, fmt.Errorf("hubcenter URL not configured")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/v1/crypto/pubkey", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 缓存到本地
	_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
	_ = os.WriteFile(cachePath, data, 0o644)
	return data, nil
}

// ── helpers ─────────────────────────────────────────────────────────────

// getSkillPrice 查询 Skill 价格（用于 free_only 模式检查）。
func (c *SkillMarketClient) getSkillPrice(ctx context.Context, skillID string) (int, error) {
	base := c.baseURL()
	if base == "" {
		return 0, fmt.Errorf("hubcenter URL not configured")
	}
	var result struct {
		Results []struct {
			Price int `json:"price"`
		} `json:"results"`
	}
	if err := c.getJSON(ctx, base+"/api/v1/skillmarket/search?q=&top_n=1&skill_id="+skillID, &result); err != nil {
		return 0, err
	}
	if len(result.Results) > 0 {
		return result.Results[0].Price, nil
	}
	return 0, nil
}

func (c *SkillMarketClient) getJSON(ctx context.Context, url string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed (%d): %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}
