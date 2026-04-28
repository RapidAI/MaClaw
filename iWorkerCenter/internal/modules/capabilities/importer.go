package capabilities

import (
	"database/sql"
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&skill); err != nil {
		return nil, fmt.Errorf("decode cloud skill: %w", err)
	}

	source := "iworkercloud:" + skillID
	var existingID string
	err = imp.write.QueryRow(`SELECT id FROM capability_packages WHERE tenant_id=? AND source=?`, tenantID, source).Scan(&existingID)
	if err == nil {
		log.Printf("[importer] cloud skill %q already imported as %s", skill.Name, existingID)
		return &CapabilityPackage{ID: existingID, Name: skill.Name, Source: source, Status: "active"}, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("check existing capability: %w", err)
	}

	now := time.Now().Format(time.RFC3339)
	id := idgen.New("cap")
	riskLevel := firstNonEmpty(skill.RiskLevel, "low")
	version := firstNonEmpty(skill.Version, "1.0.0")
	category := firstNonEmpty(skill.Category, "general")

	_, err = imp.write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending_review', ?, ?)`,
		id, tenantID, skill.Name, skill.Description, category, version, source, riskLevel, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert capability: %w", err)
	}

	log.Printf("[importer] imported cloud skill %q as %s (pending_review)", skill.Name, id)
	return &CapabilityPackage{
		ID: id, Name: skill.Name, Description: skill.Description,
		Category: category, Version: version, Source: source,
		RiskLevel: riskLevel, Status: "pending_review", CreatedAt: now, UpdatedAt: now,
	}, nil
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
