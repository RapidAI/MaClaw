package capabilities

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	marketschema "github.com/RapidAI/CodeClaw/corelib/skillmarket"
)

const cloudPublishRuleKey = "capability_cloud_publish_rule"

const cloudPublishSkillJSONBodyLimit = 2 << 20

type CloudPublishRule struct {
	Enabled              bool    `json:"enabled"`
	AllowCloudImported   bool    `json:"allow_cloud_imported"`
	MinUsageCount        int     `json:"min_usage_count"`
	MinSuccessRate       float64 `json:"min_success_rate"`
	MinAverageQuality    float64 `json:"min_average_quality"`
	DefaultPricing       string  `json:"default_pricing"`
	DefaultPrice         int64   `json:"default_price"`
	RequirePackageCached bool    `json:"require_package_cached"`
}

type CloudPublishRequest struct {
	Pricing string `json:"pricing"`
	Price   int64  `json:"price"`
	Force   bool   `json:"force"`
}

func defaultCloudPublishRule() CloudPublishRule {
	return CloudPublishRule{Enabled: false, MinUsageCount: 5, MinSuccessRate: 0.8, MinAverageQuality: 80, DefaultPricing: "free", RequirePackageCached: true}
}

func (h *Handler) GetCloudPublishRule(ctx context.Context, tenantID string) (CloudPublishRule, error) {
	rule := defaultCloudPublishRule()
	var raw string
	err := h.read.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key=?`, tenantCloudPublishRuleKey(tenantID)).Scan(&raw)
	if err == sql.ErrNoRows {
		return rule, nil
	}
	if err != nil {
		return rule, err
	}
	if strings.TrimSpace(raw) == "" {
		return rule, nil
	}
	if err := json.Unmarshal([]byte(raw), &rule); err != nil {
		return defaultCloudPublishRule(), nil
	}
	return normalizeCloudPublishRule(rule), nil
}

func (h *Handler) SetCloudPublishRule(ctx context.Context, tenantID string, rule CloudPublishRule) (CloudPublishRule, error) {
	rule = normalizeCloudPublishRule(rule)
	data, _ := json.Marshal(rule)
	key := tenantCloudPublishRuleKey(tenantID)
	res, err := h.write.ExecContext(ctx, `UPDATE system_settings SET value_json=?, updated_at=datetime('now') WHERE key=?`, string(data), key)
	if err != nil {
		return rule, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return rule, nil
	}
	_, err = h.write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now'))`, key, string(data))
	return rule, err
}

func tenantCloudPublishRuleKey(tenantID string) string {
	return cloudPublishRuleKey + ":" + strings.TrimSpace(tenantID)
}

func normalizeCloudPublishRule(rule CloudPublishRule) CloudPublishRule {
	if rule.MinUsageCount < 0 {
		rule.MinUsageCount = 0
	}
	if rule.MinSuccessRate < 0 {
		rule.MinSuccessRate = 0
	}
	if rule.MinSuccessRate > 1 {
		rule.MinSuccessRate = 1
	}
	if rule.MinAverageQuality < 0 {
		rule.MinAverageQuality = 0
	}
	if rule.MinAverageQuality > 100 {
		rule.MinAverageQuality = 100
	}
	rule.DefaultPricing = normalizePricing(rule.DefaultPricing)
	if rule.DefaultPricing == "free" {
		rule.DefaultPrice = 0
	}
	if rule.DefaultPrice < 0 {
		rule.DefaultPrice = 0
	}
	return rule
}

func normalizePricing(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "paid", "charge", "priced":
		return "paid"
	default:
		return "free"
	}
}

type cloudPublishCapability struct {
	CapabilityPackage
	PackageContent string
}

func (h *Handler) PublishCapabilityToCloud(ctx context.Context, tenantID, capabilityID string, req CloudPublishRequest) (*marketschema.Skill, error) {
	rule, err := h.GetCloudPublishRule(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !rule.Enabled && !req.Force {
		return nil, fmt.Errorf("cloud publish rule is disabled")
	}
	capability, err := h.cloudPublishCapability(ctx, tenantID, capabilityID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(capability.SafetyStatus) == "quarantined" {
		return nil, fmt.Errorf("quarantined skills cannot be published to cloud")
	}
	if !req.Force {
		if err := h.ensureCapabilityMature(ctx, tenantID, capability, rule); err != nil {
			return nil, err
		}
	}
	centerID, centerSecret, err := h.cloudPublishIdentity(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	pricing := normalizePricing(firstNonEmpty(req.Pricing, rule.DefaultPricing))
	price := req.Price
	if price <= 0 {
		price = rule.DefaultPrice
	}
	if pricing == "free" {
		price = 0
	}
	cloudSkillID := cloudSkillID(centerID, capability.ID)
	input := marketschema.SkillInput{
		ID:                   cloudSkillID,
		Name:                 capability.Name,
		Description:          capability.Description,
		Category:             capability.Category,
		Version:              capability.Version,
		RiskLevel:            capability.RiskLevel,
		Status:               "active",
		Price:                price,
		Author:               cloudSkillAuthor(centerID),
		AuthorEmail:          cloudSkillAuthorEmail(centerID),
		SourceCenterID:       centerID,
		PackageFormat:        capability.PackageFormat,
		PackageContentBase64: capability.PackageContent,
	}
	skill, err := h.publishSkillInputToCloud(ctx, centerID, centerSecret, input)
	now := time.Now().Format(time.RFC3339)
	if err != nil {
		_, _ = h.write.ExecContext(ctx, `UPDATE capability_packages SET cloud_publish_status='failed', cloud_publish_error=?, updated_at=? WHERE tenant_id=? AND id=?`, err.Error(), now, tenantID, capability.ID)
		return nil, err
	}
	_, _ = h.write.ExecContext(ctx, `UPDATE capability_packages SET cloud_publish_status='published', cloud_skill_id=?, cloud_published_at=?, cloud_publish_error='', updated_at=? WHERE tenant_id=? AND id=?`, skill.ID, now, now, tenantID, capability.ID)
	return skill, nil
}

func (h *Handler) ensureCapabilityMature(ctx context.Context, tenantID string, cap cloudPublishCapability, rule CloudPublishRule) error {
	if strings.TrimSpace(cap.SafetyStatus) == "quarantined" {
		return fmt.Errorf("quarantined skills cannot be published to cloud")
	}
	if rule.RequirePackageCached && strings.TrimSpace(cap.PackageContent) == "" {
		return fmt.Errorf("capability package is required before cloud publish")
	}
	if strings.HasPrefix(strings.ToLower(cap.Source), "iworkercloud:") && !rule.AllowCloudImported {
		return fmt.Errorf("cloud-imported skills are not allowed to be re-uploaded")
	}
	summary, err := h.capabilityUsageSummary(ctx, tenantID, cap.ID)
	if err != nil {
		return err
	}
	if summary.Total < rule.MinUsageCount {
		return fmt.Errorf("capability is not mature: usage %d < %d", summary.Total, rule.MinUsageCount)
	}
	if summary.SuccessRate < rule.MinSuccessRate {
		return fmt.Errorf("capability is not mature: success rate %.2f < %.2f", summary.SuccessRate, rule.MinSuccessRate)
	}
	if summary.AverageQuality < rule.MinAverageQuality {
		return fmt.Errorf("capability is not mature: average quality %.1f < %.1f", summary.AverageQuality, rule.MinAverageQuality)
	}
	return nil
}

func (h *Handler) cloudPublishCapability(ctx context.Context, tenantID, capabilityID string) (cloudPublishCapability, error) {
	var cap cloudPublishCapability
	err := h.read.QueryRowContext(ctx, `SELECT id, name, description, category, version, source, risk_level, status, package_status, package_format, package_sha256, package_size, local_skill_origin, cloud_publish_status, cloud_skill_id, cloud_published_at, cloud_publish_error, safety_status, safety_reason, safety_reviewed_at, created_at, updated_at, package_content FROM capability_packages WHERE tenant_id=? AND id=?`, tenantID, strings.TrimSpace(capabilityID)).Scan(&cap.ID, &cap.Name, &cap.Description, &cap.Category, &cap.Version, &cap.Source, &cap.RiskLevel, &cap.Status, &cap.PackageStatus, &cap.PackageFormat, &cap.PackageSHA256, &cap.PackageSize, &cap.LocalSkillOrigin, &cap.CloudPublishStatus, &cap.CloudSkillID, &cap.CloudPublishedAt, &cap.CloudPublishError, &cap.SafetyStatus, &cap.SafetyReason, &cap.SafetyReviewedAt, &cap.CreatedAt, &cap.UpdatedAt, &cap.PackageContent)
	return cap, err
}

func (h *Handler) cloudPublishIdentity(ctx context.Context, tenantID string) (centerID, centerSecret string, err error) {
	err = h.read.QueryRowContext(ctx, `SELECT cloud_center_id, cloud_secret FROM tenants WHERE id=?`, tenantID).Scan(&centerID, &centerSecret)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(centerID) == "" || strings.TrimSpace(centerSecret) == "" {
		return "", "", fmt.Errorf("cloud center credentials are missing")
	}
	if h.cloudURL == "" {
		return "", "", fmt.Errorf("iWorkerCloud URL is not configured")
	}
	return strings.TrimSpace(centerID), strings.TrimSpace(centerSecret), nil
}

func cloudSkillAuthor(centerID string) string {
	centerID = strings.TrimSpace(centerID)
	if centerID == "" {
		return "iWorkerCenter"
	}
	return "iWorkerCenter " + centerID
}

func cloudSkillAuthorEmail(centerID string) string {
	centerID = sanitizeSkillID(centerID)
	if centerID == "" || centerID == "skill" {
		centerID = "center"
	}
	return centerID + "@iworkercenter.local.invalid"
}

func (h *Handler) publishSkillInputToCloud(ctx context.Context, centerID, centerSecret string, input marketschema.SkillInput) (*marketschema.Skill, error) {
	data, _ := json.Marshal(input)
	if len(data) > cloudPublishSkillJSONBodyLimit {
		return nil, fmt.Errorf("cloud publish payload exceeds %d bytes", cloudPublishSkillJSONBodyLimit)
	}
	endpoint := fmt.Sprintf("%s/api/centers/%s/skills", h.cloudURL, url.PathEscape(centerID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Center-Secret", centerSecret)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("cloud publish returned %d: %s", resp.StatusCode, string(body))
	}
	var skill marketschema.Skill
	if err := decodeCloudCapabilityJSON(resp.Body, 1<<20, &skill); err != nil {
		return nil, err
	}
	return &skill, nil
}

func cloudSkillID(centerID, capabilityID string) string {
	return "center-" + sanitizeSkillID(centerID) + "-" + sanitizeSkillID(capabilityID)
}

func sanitizeSkillID(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	var b strings.Builder
	lastDash := false
	for _, r := range v {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "skill"
	}
	return out
}
