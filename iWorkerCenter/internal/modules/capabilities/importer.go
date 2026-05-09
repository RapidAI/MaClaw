package capabilities

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	marketschema "github.com/RapidAI/CodeClaw/corelib/skillmarket"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// CloudSkill represents a skill package from iWorkerCloud skill market.
type CloudSkill = marketschema.Skill

// Importer handles importing capability packages from iWorkerCloud.
type Importer struct {
	write        *sql.DB
	cloudURL     string
	centerID     string
	centerSecret string
	httpClient   *http.Client
}

// NewImporter creates an Importer backed by iWorkerCloud skill market.
func NewImporter(write *sql.DB, cloudURL, centerID, centerSecret string) *Importer {
	return &Importer{
		write:        write,
		cloudURL:     strings.TrimRight(cloudURL, "/"),
		centerID:     strings.TrimSpace(centerID),
		centerSecret: centerSecret,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// SearchCloud searches iWorkerCloud for skills this Center is entitled to use.
func (imp *Importer) SearchCloud(query string) ([]CloudSkill, error) {
	if err := imp.ensureConfigured(); err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/api/centers/%s/skills/search?q=%s", imp.cloudURL, url.PathEscape(imp.centerID), url.QueryEscape(query))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Center-Secret", imp.centerSecret)

	resp, err := imp.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloud skill search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("cloud skill search returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Results []CloudSkill `json:"results"`
	}
	if err := decodeCloudCapabilityJSON(resp.Body, 1<<20, &result); err != nil {
		return nil, fmt.Errorf("decode cloud skill search: %w", err)
	}
	if result.Results == nil {
		result.Results = []CloudSkill{}
	}
	return result.Results, nil
}

// ImportFromCloud downloads a skill from iWorkerCloud and creates a local capability package.
func (imp *Importer) ImportFromCloud(skillID, tenantID string) (*CapabilityPackage, error) {
	if err := imp.ensureConfigured(); err != nil {
		return nil, err
	}
	skillID = strings.TrimSpace(skillID)
	tenantID = strings.TrimSpace(tenantID)
	if skillID == "" {
		return nil, fmt.Errorf("skill_id is required")
	}
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}

	endpoint := fmt.Sprintf("%s/api/centers/%s/skills/%s", imp.cloudURL, url.PathEscape(imp.centerID), url.PathEscape(skillID))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Center-Secret", imp.centerSecret)

	resp, err := imp.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch cloud skill: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("cloud skill fetch returned %d: %s", resp.StatusCode, string(body))
	}

	var skill CloudSkill
	if err := decodeCloudCapabilityJSON(resp.Body, 1<<20, &skill); err != nil {
		return nil, fmt.Errorf("decode cloud skill: %w", err)
	}
	pkg, err := imp.DownloadPackage(skillID)
	if err != nil {
		return nil, fmt.Errorf("download cloud skill package: %w", err)
	}

	source := "iworkercloud:" + skillID
	var existingID string
	err = imp.write.QueryRow(`SELECT id FROM capability_packages WHERE tenant_id=? AND source=?`, tenantID, source).Scan(&existingID)
	if err == nil {
		log.Printf("[importer] cloud skill %q already imported as %s", skill.Name, existingID)
		return imp.lookupCapability(tenantID, existingID)
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("check existing capability: %w", err)
	}

	now := time.Now().Format(time.RFC3339)
	id := idgen.New("cap")
	riskLevel := firstNonEmpty(skill.RiskLevel, "low")
	version := firstNonEmpty(skill.Version, "1.0.0")
	category := firstNonEmpty(skill.Category, "general")

	_, err = imp.write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_sha256, package_size, package_content, local_skill_origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending_review', ?, ?, ?, ?, ?, 'cloud_imported', ?, ?)`,
		id, tenantID, skill.Name, skill.Description, category, version, source, riskLevel, "package_cached", pkg.Format, pkg.SHA256, pkg.Size, pkg.ContentBase64, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert capability: %w", err)
	}

	log.Printf("[importer] imported cloud skill %q as %s (pending_review)", skill.Name, id)
	return &CapabilityPackage{
		ID: id, Name: skill.Name, Description: skill.Description,
		Category: category, Version: version, Source: source,
		RiskLevel: riskLevel, Status: "pending_review", PackageStatus: "package_cached",
		PackageFormat: pkg.Format, PackageSHA256: pkg.SHA256, PackageSize: pkg.Size,
		LocalSkillOrigin: "cloud_imported",
		CreatedAt:        now, UpdatedAt: now,
	}, nil
}

func (imp *Importer) lookupCapability(tenantID, id string) (*CapabilityPackage, error) {
	var cp CapabilityPackage
	err := imp.write.QueryRow(capabilitySelectSQL+" WHERE tenant_id=? AND id=?", tenantID, strings.TrimSpace(id)).Scan(&cp.ID, &cp.Name, &cp.Description, &cp.Category, &cp.Version, &cp.Source, &cp.RiskLevel, &cp.Status, &cp.PackageStatus, &cp.PackageFormat, &cp.PackageSHA256, &cp.PackageSize, &cp.LocalSkillOrigin, &cp.CloudPublishStatus, &cp.CloudSkillID, &cp.CloudPublishedAt, &cp.CloudPublishError, &cp.SafetyStatus, &cp.SafetyReason, &cp.SafetyReviewedAt, &cp.CreatedAt, &cp.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &cp, nil
}

func (imp *Importer) DownloadPackage(skillID string) (*marketschema.PackageDownload, error) {
	if err := imp.ensureConfigured(); err != nil {
		return nil, err
	}
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return nil, fmt.Errorf("skill_id is required")
	}
	endpoint := fmt.Sprintf("%s/api/centers/%s/skills/%s/package", imp.cloudURL, url.PathEscape(imp.centerID), url.PathEscape(skillID))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Center-Secret", imp.centerSecret)
	resp, err := imp.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download cloud skill package: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("cloud skill package returned %d: %s", resp.StatusCode, string(body))
	}
	var pkg marketschema.PackageDownload
	if err := decodeCloudCapabilityJSON(resp.Body, 8<<20, &pkg); err != nil {
		return nil, fmt.Errorf("decode cloud skill package: %w", err)
	}
	if err := validatePackageDownload(&pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

func validatePackageDownload(pkg *marketschema.PackageDownload) error {
	if pkg == nil || strings.TrimSpace(pkg.ContentBase64) == "" {
		return fmt.Errorf("empty skill package")
	}
	decoded, err := base64.StdEncoding.DecodeString(pkg.ContentBase64)
	if err != nil {
		return fmt.Errorf("invalid skill package content: %w", err)
	}
	if pkg.Size > 0 && int64(len(decoded)) != pkg.Size {
		return fmt.Errorf("skill package size mismatch")
	}
	if pkg.SHA256 != "" {
		sum := sha256.Sum256(decoded)
		if fmt.Sprintf("%x", sum[:]) != strings.ToLower(pkg.SHA256) {
			return fmt.Errorf("skill package sha256 mismatch")
		}
	}
	return nil
}

func decodeCloudCapabilityJSON(body io.Reader, limit int64, dst any) error {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("cloud capability JSON exceeds %d bytes", limit)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("cloud capability JSON contains trailing data")
		}
		return err
	}
	return nil
}

// ApproveCapability changes status from pending_review to active for one tenant.
func (imp *Importer) ApproveCapability(id, tenantID string) error {
	now := time.Now().Format(time.RFC3339)
	res, err := imp.write.Exec(`UPDATE capability_packages SET status='active', updated_at=? WHERE id=? AND tenant_id=? AND status='pending_review'`, now, id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("capability %s not found or not in pending_review status", id)
	}
	return nil
}

// RejectCapability changes status from pending_review to rejected for one tenant.
func (imp *Importer) RejectCapability(id, tenantID string) error {
	now := time.Now().Format(time.RFC3339)
	res, err := imp.write.Exec(`UPDATE capability_packages SET status='rejected', updated_at=? WHERE id=? AND tenant_id=? AND status='pending_review'`, now, id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("capability %s not found or not in pending_review status", id)
	}
	return nil
}

func (imp *Importer) ensureConfigured() error {
	if imp.cloudURL == "" {
		return fmt.Errorf("iWorkerCloud URL not configured")
	}
	if imp.centerID == "" || imp.centerSecret == "" {
		return fmt.Errorf("iWorkerCloud center credentials not configured")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
