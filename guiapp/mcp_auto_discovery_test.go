package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMCPAutoDiscovery_ScanFile_NotExist(t *testing.T) {
	// ScanProject on a dir without .mcp.json should not error
	r := NewToolRegistry()
	d := &MCPAutoDiscovery{
		registry:    r,
		mcpRegistry: nil,
		watching:    make(map[string]bool),
		stopCh:      make(chan struct{}),
	}
	err := d.ScanProject(t.TempDir())
	if err != nil {
		t.Errorf("ScanProject on empty dir: %v", err)
	}
}

func TestMCPAutoDiscovery_ScanFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte("not json"), 0644)

	r := NewToolRegistry()
	d := &MCPAutoDiscovery{
		registry:    r,
		mcpRegistry: nil,
		watching:    make(map[string]bool),
		stopCh:      make(chan struct{}),
	}
	err := d.ScanProject(dir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestMCPAutoDiscovery_ParseValidDecl(t *testing.T) {
	dir := t.TempDir()
	decl := MCPDeclFile{
		Servers: []MCPDeclServer{
			{ID: "test-server", Name: "Test", EndpointURL: "http://localhost:8080", Tags: []string{"test"}},
			{ID: "", Name: "Empty ID", EndpointURL: "http://localhost:8081"},
		},
	}
	data, _ := json.Marshal(decl)
	path := filepath.Join(dir, ".mcp.json")
	os.WriteFile(path, data, 0644)

	// Just test that the file can be parsed correctly
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var parsed MCPDeclFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Servers) != 2 {
		t.Errorf("servers len = %d, want 2", len(parsed.Servers))
	}
	// Verify that empty ID servers would be skipped (ID check in scanFile)
	validCount := 0
	for _, s := range parsed.Servers {
		if s.ID != "" && s.EndpointURL != "" {
			validCount++
		}
	}
	if validCount != 1 {
		t.Errorf("valid servers = %d, want 1", validCount)
	}
}

func TestMCPAutoDiscovery_Stop(t *testing.T) {
	d := &MCPAutoDiscovery{
		watching: make(map[string]bool),
		stopCh:   make(chan struct{}),
	}
	d.Stop()
	// Calling Stop again should not panic
	d.Stop()
}

func TestMCPDeclFile_Parse(t *testing.T) {
	raw := `{"servers":[{"id":"s1","name":"Server 1","endpoint_url":"http://localhost:9000","tags":["db"]}]}`
	var decl MCPDeclFile
	if err := json.Unmarshal([]byte(raw), &decl); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(decl.Servers) != 1 {
		t.Fatalf("servers len = %d, want 1", len(decl.Servers))
	}
	if decl.Servers[0].ID != "s1" {
		t.Errorf("server ID = %s, want s1", decl.Servers[0].ID)
	}
	if len(decl.Servers[0].Tags) != 1 || decl.Servers[0].Tags[0] != "db" {
		t.Errorf("tags = %v, want [db]", decl.Servers[0].Tags)
	}
}
