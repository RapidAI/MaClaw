package capabilities

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corel "github.com/RapidAI/CodeClaw/corelib"
	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/agentruntime"
)

type InstalledCapability struct {
	Capability CapabilityPackage  `json:"capability"`
	Entry      corel.NLSkillEntry `json:"entry"`
}

type RuntimeCapabilityEntry struct {
	CapabilityID string             `json:"capability_id"`
	Name         string             `json:"name"`
	Source       string             `json:"source"`
	Version      string             `json:"version"`
	RiskLevel    string             `json:"risk_level"`
	Entry        corel.NLSkillEntry `json:"entry"`
}

func (h *Handler) installCapabilityPackage(ctx context.Context, tenantID, id string) (*InstalledCapability, error) {
	var cp CapabilityPackage
	var packageContent string
	row := h.read.QueryRowContext(ctx, `SELECT id, name, description, category, version, source, risk_level, status, package_status, package_format, package_sha256, package_size, created_at, updated_at, package_content FROM capability_packages WHERE id=? AND tenant_id=?`, id, tenantID)
	if err := row.Scan(&cp.ID, &cp.Name, &cp.Description, &cp.Category, &cp.Version, &cp.Source, &cp.RiskLevel, &cp.Status, &cp.PackageStatus, &cp.PackageFormat, &cp.PackageSHA256, &cp.PackageSize, &cp.CreatedAt, &cp.UpdatedAt, &packageContent); err != nil {
		return nil, err
	}
	entry, err := buildRuntimeEntryFromPackage(cp, packageContent)
	now := time.Now().Format(time.RFC3339)
	if err != nil {
		_, _ = h.write.ExecContext(ctx, `UPDATE capability_packages SET package_status='install_failed', install_error=?, updated_at=? WHERE id=? AND tenant_id=?`, err.Error(), now, id, tenantID)
		return nil, err
	}
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("marshal runtime entry: %w", err)
	}
	res, err := h.write.ExecContext(ctx, `UPDATE capability_packages SET package_status='installed', installed_entry_json=?, installed_at=?, install_error='', updated_at=? WHERE id=? AND tenant_id=?`, string(entryJSON), now, now, id, tenantID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	cp.PackageStatus = "installed"
	cp.UpdatedAt = now
	return &InstalledCapability{Capability: cp, Entry: *entry}, nil
}

func buildRuntimeEntryFromPackage(cp CapabilityPackage, packageContent string) (*corel.NLSkillEntry, error) {
	if strings.TrimSpace(cp.PackageStatus) == "" || cp.PackageStatus == "metadata_only" || cp.PackageStatus == "package_unavailable" {
		return nil, fmt.Errorf("capability package is not cached")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(packageContent))
	if err != nil {
		return nil, fmt.Errorf("decode capability package: %w", err)
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("capability package is empty")
	}
	if cp.PackageSize > 0 && int64(len(decoded)) != cp.PackageSize {
		return nil, fmt.Errorf("capability package size mismatch")
	}
	if cp.PackageSHA256 != "" {
		sum := sha256.Sum256(decoded)
		if fmt.Sprintf("%x", sum[:]) != strings.ToLower(cp.PackageSHA256) {
			return nil, fmt.Errorf("capability package sha256 mismatch")
		}
	}
	format := strings.ToLower(strings.TrimSpace(cp.PackageFormat))
	if format == "" {
		format = "skill.md"
	}
	switch format {
	case "skill.md", "markdown", "claude-skill.md":
		entry, err := coreskill.ParseMarkdownSkill(string(decoded), coreskill.MarkdownSkillOptions{
			NameFallback:        cp.Name,
			DescriptionFallback: cp.Description,
			Source:              "iworkercenter",
			SourceProject:       cp.Source,
			TrustLevel:          "center-approved",
		})
		if err != nil {
			return nil, err
		}
		entry.HubSkillID = cp.ID
		entry.HubVersion = cp.Version
		entry.Source = "iworkercenter"
		entry.SourceProject = cp.Source
		entry.Status = "active"
		return entry, nil
	default:
		return nil, fmt.Errorf("unsupported capability package format %q", cp.PackageFormat)
	}
}

func (h *Handler) runtimeEntryForCapability(ctx context.Context, tenantID, id string) (*corel.NLSkillEntry, error) {
	var entryJSON string
	err := h.read.QueryRowContext(ctx, `SELECT installed_entry_json FROM capability_packages WHERE id=? AND tenant_id=? AND package_status='installed'`, id, tenantID).Scan(&entryJSON)
	if err != nil {
		return nil, err
	}
	var entry corel.NLSkillEntry
	if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (h *Handler) runtimeEntriesForColleague(ctx context.Context, tenantID, colleagueID string) ([]RuntimeCapabilityEntry, error) {
	colleagueID = strings.TrimSpace(colleagueID)
	if colleagueID == "" {
		return nil, fmt.Errorf("colleague_id is required")
	}
	rows, err := h.read.QueryContext(ctx, `SELECT cp.id, cp.name, cp.source, cp.version, cp.risk_level, cp.installed_entry_json
		FROM capability_packages cp
		JOIN colleague_capability_bindings ccb ON cp.id = ccb.capability_id AND cp.tenant_id = ccb.tenant_id
		WHERE cp.tenant_id=? AND ccb.colleague_id=? AND cp.status IN ('active','approved') AND cp.package_status='installed'
		ORDER BY cp.name`, tenantID, colleagueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RuntimeCapabilityEntry{}
	for rows.Next() {
		var item RuntimeCapabilityEntry
		var entryJSON string
		if err := rows.Scan(&item.CapabilityID, &item.Name, &item.Source, &item.Version, &item.RiskLevel, &entryJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(entryJSON), &item.Entry); err != nil {
			return nil, fmt.Errorf("decode installed runtime entry for %s: %w", item.CapabilityID, err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) RuntimeSkillsForWorker(ctx context.Context, tenantID, workerID string) ([]agentruntime.RuntimeSkill, error) {
	entries, err := h.runtimeEntriesForColleague(ctx, tenantID, workerID)
	if err != nil {
		return nil, err
	}
	items := make([]agentruntime.RuntimeSkill, 0, len(entries))
	for _, entry := range entries {
		items = append(items, agentruntime.RuntimeSkill{
			CapabilityID: entry.CapabilityID,
			Name:         entry.Name,
			Source:       entry.Source,
			Version:      entry.Version,
			RiskLevel:    entry.RiskLevel,
			Entry:        entry.Entry,
		})
	}
	return items, nil
}

func (h *Handler) SelectWorkerForTask(ctx context.Context, tenantID, roleCode, taskText string) (string, bool, error) {
	query := strings.ToLower(strings.TrimSpace(taskText + " " + roleCode))
	if query == "" {
		return "", false, nil
	}
	rows, err := h.read.QueryContext(ctx, `SELECT ccb.colleague_id, cp.id, cp.name, cp.source, cp.version, cp.risk_level, cp.installed_entry_json
		FROM capability_packages cp
		JOIN colleague_capability_bindings ccb ON cp.id = ccb.capability_id AND cp.tenant_id = ccb.tenant_id
		LEFT JOIN colleagues col ON col.id = ccb.colleague_id AND col.tenant_id = ccb.tenant_id
		LEFT JOIN roles r ON r.id = col.role_id AND r.tenant_id = col.tenant_id
		WHERE cp.tenant_id=? AND cp.status IN ('active','approved') AND cp.package_status='installed'
		  AND (? = '' OR r.code = ?)
		ORDER BY cp.name`, tenantID, strings.TrimSpace(roleCode), strings.TrimSpace(roleCode))
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	bestWorker := ""
	bestScore := 0
	for rows.Next() {
		var workerID string
		var item RuntimeCapabilityEntry
		var entryJSON string
		if err := rows.Scan(&workerID, &item.CapabilityID, &item.Name, &item.Source, &item.Version, &item.RiskLevel, &entryJSON); err != nil {
			return "", false, err
		}
		if err := json.Unmarshal([]byte(entryJSON), &item.Entry); err != nil {
			continue
		}
		score := scoreRuntimeSkillMatch(query, item)
		score += h.capabilityUsageScore(ctx, tenantID, item.CapabilityID, workerID)
		if score > bestScore {
			bestScore = score
			bestWorker = workerID
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if bestScore <= 0 || strings.TrimSpace(bestWorker) == "" {
		return "", false, nil
	}
	return bestWorker, true, nil
}

func scoreRuntimeSkillMatch(query string, item RuntimeCapabilityEntry) int {
	score := 0
	for _, token := range skillMatchTokens(item) {
		if token == "" {
			continue
		}
		if strings.Contains(query, token) {
			score += 3
		}
		for _, part := range strings.Fields(strings.ReplaceAll(token, "-", " ")) {
			if len(part) >= 3 && strings.Contains(query, part) {
				score++
			}
		}
	}
	return score
}

func skillMatchTokens(item RuntimeCapabilityEntry) []string {
	tokens := []string{item.Name, item.Entry.Name, item.Entry.Description, item.Source}
	tokens = append(tokens, item.Entry.Triggers...)
	for i := range tokens {
		tokens[i] = strings.ToLower(strings.TrimSpace(tokens[i]))
	}
	return tokens
}
