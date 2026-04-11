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

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// HubCenterSkill represents a skill from hubcenter's API.
type HubCenterSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`
	RiskLevel   string   `json:"risk_level"`
}

// Importer handles importing capability packages from hubcenter.
type Importer struct {
	write         *sql.DB
	hubCenterURL  string
	httpClient    *http.Client
}

// NewImporter creates an Importer.
func NewImporter(write *sql.DB, hubCenterURL string) *Importer {
	return &Importer{
		write:        write,
		hubCenterURL: strings.TrimRight(hubCenterURL, "/"),
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// SearchHubCenter searches hubcenter for available skills.
func (imp *Importer) SearchHubCenter(query string) ([]HubCenterSkill, error) {
	if imp.hubCenterURL == "" {
		return nil, fmt.Errorf("hubcenter URL not configured")
	}
	url := fmt.Sprintf("%s/api/skills/search?q=%s", imp.hubCenterURL, url.QueryEscape(query))
	resp, err := imp.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("hubcenter request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("hubcenter returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Skills []HubCenterSkill `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result.Skills, nil
}

// ImportFromHubCenter downloads a skill from hubcenter and creates a local capability package.
// The download happens outside the transaction; only the DB insert is transactional.
func (imp *Importer) ImportFromHubCenter(skillID string) (*CapabilityPackage, error) {
	if imp.hubCenterURL == "" {
		return nil, fmt.Errorf("hubcenter URL not configured")
	}

	// Step 1: Fetch skill metadata (outside transaction)
	url := fmt.Sprintf("%s/api/skills/%s", imp.hubCenterURL, skillID)
	resp, err := imp.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch skill: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hubcenter returned %d", resp.StatusCode)
	}

	var skill HubCenterSkill
	if err := json.NewDecoder(resp.Body).Decode(&skill); err != nil {
		return nil, fmt.Errorf("decode skill: %w", err)
	}

	// Step 2: Check for existing import (idempotent by source)
	var existingID string
	err = imp.write.QueryRow(`SELECT id FROM capability_packages WHERE source=?`,
		"hubcenter:"+skillID).Scan(&existingID)
	if err == nil {
		// Already imported, return existing
		log.Printf("[importer] skill %q already imported as %s", skill.Name, existingID)
		return &CapabilityPackage{ID: existingID, Name: skill.Name, Status: "active"}, nil
	}

	// Step 3: Insert into DB
	now := time.Now().Format(time.RFC3339)
	id := idgen.New("cap")
	riskLevel := skill.RiskLevel
	if riskLevel == "" {
		riskLevel = "low"
	}
	version := skill.Version
	if version == "" {
		version = "1.0.0"
	}

	_, err = imp.write.Exec(`INSERT INTO capability_packages (id, name, description, category, version, source, risk_level, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending_review', ?, ?)`,
		id, skill.Name, skill.Description, skill.Category, version,
		"hubcenter:"+skillID, riskLevel, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert capability: %w", err)
	}

	log.Printf("[importer] imported skill %q from hubcenter as %s (pending_review)", skill.Name, id)
	return &CapabilityPackage{
		ID: id, Name: skill.Name, Description: skill.Description,
		Category: skill.Category, Version: version, Source: "hubcenter:" + skillID,
		RiskLevel: riskLevel, Status: "pending_review", CreatedAt: now, UpdatedAt: now,
	}, nil
}

// ApproveCapability changes status from pending_review to active.
func (imp *Importer) ApproveCapability(id string) error {
	now := time.Now().Format(time.RFC3339)
	res, err := imp.write.Exec(`UPDATE capability_packages SET status='active', updated_at=? WHERE id=? AND status='pending_review'`, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("capability %s not found or not in pending_review status", id)
	}
	return nil
}

// RejectCapability changes status from pending_review to rejected.
func (imp *Importer) RejectCapability(id string) error {
	now := time.Now().Format(time.RFC3339)
	res, err := imp.write.Exec(`UPDATE capability_packages SET status='rejected', updated_at=? WHERE id=? AND status='pending_review'`, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("capability %s not found or not in pending_review status", id)
	}
	return nil
}
