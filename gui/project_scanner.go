package main

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// ProjectScanner scans a project directory for .mcp/servers.json and registers
// discovered MCP Servers into the MCPRegistry with source="project".
type ProjectScanner struct {
	registry *MCPRegistry
}

// NewProjectScanner creates a new ProjectScanner bound to the given registry.
func NewProjectScanner(registry *MCPRegistry) *ProjectScanner {
	return &ProjectScanner{registry: registry}
}

// projectServerEntry mirrors the JSON schema of entries in .mcp/servers.json.
type projectServerEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	EndpointURL string `json:"endpoint_url"`
	AuthType    string `json:"auth_type"`
	AuthSecret  string `json:"auth_secret"`
}

// ScanProject reads .mcp/servers.json from projectPath and returns the parsed
// MCP server entries. Each discovered server is also registered in the
// MCPRegistry with source="project". If the config file does not exist, an
// empty slice is returned (not an error).
func (s *ProjectScanner) ScanProject(projectPath string) ([]corelib.MCPServerEntry, error) {
	configPath := filepath.Join(projectPath, ".mcp", "servers.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []corelib.MCPServerEntry{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}

	var raw []projectServerEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath, err)
	}

	entries := make([]corelib.MCPServerEntry, 0, len(raw))
	for _, r := range raw {
		if r.ID == "" || r.Name == "" || r.EndpointURL == "" {
			log.Printf("[ProjectScanner] skipping entry with missing required fields in %s", configPath)
			continue
		}

		entry := corelib.MCPServerEntry{
			ID:          r.ID,
			Name:        r.Name,
			EndpointURL: r.EndpointURL,
			AuthType:    r.AuthType,
			AuthSecret:  r.AuthSecret,
			CreatedAt:   time.Now().Format(time.RFC3339),
			Source:      corelib.MCPSourceProject,
		}
		entries = append(entries, entry)

		if s.registry != nil {
			if err := s.registry.RegisterAutoDiscovered(entry, corelib.MCPSourceProject); err != nil {
				log.Printf("[ProjectScanner] failed to register %s: %v", r.ID, err)
			}
		}
	}

	return entries, nil
}
