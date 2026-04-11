package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SharedMemoryEntry represents a memory item fetched from iWorkerCenter.
type SharedMemoryEntry struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Level   string   `json:"level"`   // enterprise, role, team
	Scope   string   `json:"scope"`   // all, office, data, production, quality
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
// Memories are injected in order: enterprise → role → team.
// Returns empty string if no memories are available.
func buildMemorySystemPrompt(memories []SharedMemoryEntry) string {
	if len(memories) == 0 {
		return ""
	}

	var enterprise, role, team []string
	for _, m := range memories {
		line := fmt.Sprintf("- %s：%s", m.Title, m.Content)
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
		parts = append(parts, "【企业背景】\n"+strings.Join(enterprise, "\n"))
	}
	if len(role) > 0 {
		parts = append(parts, "【角色知识】\n"+strings.Join(role, "\n"))
	}
	if len(team) > 0 {
		parts = append(parts, "【团队信息】\n"+strings.Join(team, "\n"))
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
