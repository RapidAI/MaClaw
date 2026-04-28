package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SharedMemoryEntry represents a memory item fetched from iWorkerCenter.
type SharedMemoryEntry struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Level   string   `json:"level"` // enterprise, role, team
	Scope   string   `json:"scope"` // all, office, data, production, quality
	Tags    []string `json:"tags"`
}

// fetchSharedMemories retrieves shared memories from iWorkerCenter.
// It fetches enterprise-level memories plus role-specific memories matching roleCode.
// Returns empty slice on any error (non-blocking).
func fetchSharedMemories(centerBaseURL string, roleCode string, timeoutSec int) []SharedMemoryEntry {
	if centerBaseURL == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}

	url := strings.TrimRight(centerBaseURL, "/") + "/client/memories"
	if roleCode != "" {
		url += "?role_code=" + roleCode
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil
	}

	var result struct {
		Memories []SharedMemoryEntry `json:"memories"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	return result.Memories
}

// buildMemorySystemPrompt assembles shared memories into a system prompt section.
// Memories are injected in order: enterprise -> role -> team.
// Returns empty string if no memories are available.
func buildMemorySystemPrompt(memories []SharedMemoryEntry) string {
	if len(memories) == 0 {
		return ""
	}

	var enterprise, role, team []string
	for _, m := range memories {
		line := fmt.Sprintf("- %s: %s", m.Title, m.Content)
		switch m.Level {
		case "enterprise":
			enterprise = append(enterprise, line)
		case "role":
			role = append(role, line)
		case "team":
			team = append(team, line)
		default:
			enterprise = append(enterprise, line)
		}
	}

	var parts []string
	if len(enterprise) > 0 {
		parts = append(parts, "[Enterprise Context]\n"+strings.Join(enterprise, "\n"))
	}
	if len(role) > 0 {
		parts = append(parts, "[Role Knowledge]\n"+strings.Join(role, "\n"))
	}
	if len(team) > 0 {
		parts = append(parts, "[Team Context]\n"+strings.Join(team, "\n"))
	}

	return strings.Join(parts, "\n\n")
}

// colleagueRoleCode maps a colleague name to a role code for memory filtering.
// First tries to fetch from iWorkerCenter, falls back to hardcoded mapping.
func colleagueRoleCode(colleagueName string) string {
	// Try fetching from center
	settings, _ := readDiWorkerSettings()
	if settings.Center.Enabled {
		baseURL := strings.TrimRight(strings.TrimSpace(settings.Center.BaseURL), "/")
		if baseURL == "" {
			baseURL = buildCenterBaseURL(settings.Center.Host, settings.Center.Port)
		}
		if colleagues := fetchCenterColleagues(baseURL, 3); len(colleagues) > 0 {
			for _, c := range colleagues {
				if c.Name == colleagueName && c.RoleCode != "" {
					return c.RoleCode
				}
			}
		}
	}

	// Fallback to hardcoded mapping
	switch colleagueName {
	case "小迪":
		return "office"
	case "阿宁":
		return "data"
	case "老陈":
		return "production"
	case "小周":
		return "quality"
	default:
		return ""
	}
}

// WorkerMemoryEntry represents an iWorker memory stored canonically in iWorkerCenter.
type WorkerMemoryEntry struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"tenant_id"`
	DepartmentID string   `json:"department_id,omitempty"`
	WorkerID     string   `json:"worker_id,omitempty"`
	Scope        string   `json:"scope"`
	Content      string   `json:"content"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags"`
	SourceType   string   `json:"source_type,omitempty"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

type WorkerMemoryStats struct {
	TenantID      string         `json:"tenant_id"`
	DepartmentID  string         `json:"department_id,omitempty"`
	WorkerID      string         `json:"worker_id,omitempty"`
	Total         int            `json:"total"`
	ByScope       map[string]int `json:"by_scope"`
	ByCategory    map[string]int `json:"by_category"`
	VisibleScopes []string       `json:"visible_scopes"`
}
type SaveWorkerMemoryRequest struct {
	Scope      string   `json:"scope"`
	Content    string   `json:"content"`
	Category   string   `json:"category"`
	Tags       []string `json:"tags"`
	SourceType string   `json:"source_type"`
}

func fetchWorkerMemoryStats(centerBaseURL, tenantID, departmentID, workerID string, timeoutSec int) (WorkerMemoryStats, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	tenantID = firstNonEmptyString(strings.TrimSpace(tenantID), "default")
	departmentID = strings.TrimSpace(departmentID)
	workerID = strings.TrimSpace(workerID)
	if centerBaseURL == "" {
		return WorkerMemoryStats{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if workerID == "" {
		return WorkerMemoryStats{}, fmt.Errorf("worker_id is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	values := url.Values{}
	values.Set("tenant_id", tenantID)
	if departmentID != "" {
		values.Set("department_id", departmentID)
	}
	values.Set("worker_id", workerID)
	endpoint := centerBaseURL + "/client/iworker/memory-stats?" + values.Encode()
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return WorkerMemoryStats{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return WorkerMemoryStats{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return WorkerMemoryStats{}, fmt.Errorf("iWorkerCenter memory stats failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var stats WorkerMemoryStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return WorkerMemoryStats{}, err
	}
	if stats.ByScope == nil {
		stats.ByScope = map[string]int{}
	}
	if stats.ByCategory == nil {
		stats.ByCategory = map[string]int{}
	}
	return stats, nil
}
func fetchWorkerMemories(centerBaseURL, tenantID, departmentID, workerID, query string, limit int, timeoutSec int) []WorkerMemoryEntry {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	tenantID = firstNonEmptyString(strings.TrimSpace(tenantID), "default")
	departmentID = strings.TrimSpace(departmentID)
	workerID = strings.TrimSpace(workerID)
	if centerBaseURL == "" || workerID == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	if limit <= 0 {
		limit = 10
	}

	values := url.Values{}
	values.Set("tenant_id", tenantID)
	if departmentID != "" {
		values.Set("department_id", departmentID)
	}
	values.Set("worker_id", workerID)
	values.Set("limit", fmt.Sprintf("%d", limit))
	if strings.TrimSpace(query) != "" {
		values.Set("query", strings.TrimSpace(query))
	}
	endpoint := centerBaseURL + "/client/iworker/memories?" + values.Encode()
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return readWorkerMemoryCache(tenantID, departmentID, workerID)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return readWorkerMemoryCache(tenantID, departmentID, workerID)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return readWorkerMemoryCache(tenantID, departmentID, workerID)
	}
	var result struct {
		Memories []WorkerMemoryEntry `json:"memories"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return readWorkerMemoryCache(tenantID, departmentID, workerID)
	}
	_ = writeWorkerMemoryCache(tenantID, departmentID, workerID, result.Memories)
	return result.Memories
}

func saveWorkerMemory(centerBaseURL, tenantID, departmentID, workerID string, req SaveWorkerMemoryRequest, timeoutSec int) (WorkerMemoryEntry, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	tenantID = firstNonEmptyString(strings.TrimSpace(tenantID), "default")
	departmentID = strings.TrimSpace(departmentID)
	workerID = strings.TrimSpace(workerID)
	if centerBaseURL == "" {
		return WorkerMemoryEntry{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if workerID == "" {
		return WorkerMemoryEntry{}, fmt.Errorf("worker_id is required")
	}
	if strings.TrimSpace(req.Content) == "" {
		return WorkerMemoryEntry{}, fmt.Errorf("memory content is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	payload := map[string]any{
		"tenant_id":     tenantID,
		"department_id": departmentID,
		"worker_id":     workerID,
		"scope":         firstNonEmptyString(strings.TrimSpace(req.Scope), "personal"),
		"content":       strings.TrimSpace(req.Content),
		"category":      strings.TrimSpace(req.Category),
		"tags":          req.Tags,
		"source_type":   firstNonEmptyString(strings.TrimSpace(req.SourceType), "iworker"),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return WorkerMemoryEntry{}, err
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Post(centerBaseURL+"/client/iworker/memories", "application/json", bytes.NewReader(body))
	if err != nil {
		return WorkerMemoryEntry{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return WorkerMemoryEntry{}, err
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return WorkerMemoryEntry{}, fmt.Errorf("iWorkerCenter memory save failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var saved WorkerMemoryEntry
	if err := json.Unmarshal(respBody, &saved); err != nil {
		return WorkerMemoryEntry{}, err
	}
	cached := readWorkerMemoryCache(tenantID, departmentID, workerID)
	cached = upsertWorkerMemoryCache(cached, saved)
	_ = writeWorkerMemoryCache(tenantID, departmentID, workerID, cached)
	return saved, nil
}

func deleteWorkerMemory(centerBaseURL, tenantID, departmentID, workerID, memoryID string, timeoutSec int) error {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	tenantID = firstNonEmptyString(strings.TrimSpace(tenantID), "default")
	departmentID = strings.TrimSpace(departmentID)
	workerID = strings.TrimSpace(workerID)
	memoryID = strings.TrimSpace(memoryID)
	if centerBaseURL == "" {
		return fmt.Errorf("iWorkerCenter base URL is required")
	}
	if workerID == "" {
		return fmt.Errorf("worker_id is required")
	}
	if memoryID == "" {
		return fmt.Errorf("memory id is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	values := url.Values{}
	values.Set("tenant_id", tenantID)
	if departmentID != "" {
		values.Set("department_id", departmentID)
	}
	values.Set("worker_id", workerID)
	endpoint := centerBaseURL + "/client/iworker/memories/" + url.PathEscape(memoryID) + "?" + values.Encode()
	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("iWorkerCenter memory delete failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	cached := removeWorkerMemoryCache(readWorkerMemoryCache(tenantID, departmentID, workerID), memoryID)
	_ = writeWorkerMemoryCache(tenantID, departmentID, workerID, cached)
	return nil
}
func buildWorkerMemorySystemPrompt(memories []WorkerMemoryEntry) string {
	if len(memories) == 0 {
		return ""
	}
	var company, department, personal []string
	for _, m := range memories {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		line := "- " + content
		switch m.Scope {
		case "company":
			company = append(company, line)
		case "department":
			department = append(department, line)
		default:
			personal = append(personal, line)
		}
	}
	var parts []string
	if len(company) > 0 {
		parts = append(parts, "[Company Memory]\n"+strings.Join(company, "\n"))
	}
	if len(department) > 0 {
		parts = append(parts, "[Department Memory]\n"+strings.Join(department, "\n"))
	}
	if len(personal) > 0 {
		parts = append(parts, "[Personal Memory]\n"+strings.Join(personal, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

func workerMemoryCachePath(tenantID, departmentID, workerID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	key := workerMemoryCacheKey(tenantID, departmentID, workerID)
	return filepath.Join(home, ".iworker", "cache", "memories", key+".json"), nil
}

func workerMemoryCacheKey(tenantID, departmentID, workerID string) string {
	tenantID = sanitizeCacheName(firstNonEmptyString(tenantID, "default"))
	departmentID = sanitizeCacheName(firstNonEmptyString(departmentID, "default"))
	workerID = sanitizeCacheName(firstNonEmptyString(workerID, "default"))
	return strings.Join([]string{tenantID, departmentID, workerID}, "__")
}

func readWorkerMemoryCache(tenantID, departmentID, workerID string) []WorkerMemoryEntry {
	path, err := workerMemoryCachePath(tenantID, departmentID, workerID)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var memories []WorkerMemoryEntry
	if err := json.Unmarshal(data, &memories); err != nil {
		return nil
	}
	return memories
}

func writeWorkerMemoryCache(tenantID, departmentID, workerID string, memories []WorkerMemoryEntry) error {
	path, err := workerMemoryCachePath(tenantID, departmentID, workerID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(memories, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func upsertWorkerMemoryCache(items []WorkerMemoryEntry, saved WorkerMemoryEntry) []WorkerMemoryEntry {
	for i := range items {
		if items[i].ID != "" && items[i].ID == saved.ID {
			items[i] = saved
			return items
		}
	}
	return append([]WorkerMemoryEntry{saved}, items...)
}

func removeWorkerMemoryCache(items []WorkerMemoryEntry, memoryID string) []WorkerMemoryEntry {
	memoryID = strings.TrimSpace(memoryID)
	if memoryID == "" || len(items) == 0 {
		return items
	}
	filtered := items[:0]
	for _, item := range items {
		if item.ID != memoryID {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
func sanitizeCacheName(v string) string {
	v = strings.TrimSpace(v)
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
