package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewProjectScanner(t *testing.T) {
	scanner := NewProjectScanner(nil)
	if scanner == nil {
		t.Fatal("expected non-nil scanner")
	}
}

func TestScanProject_NoConfigFile(t *testing.T) {
	dir := t.TempDir()
	scanner := NewProjectScanner(nil)

	entries, err := scanner.ScanProject(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestScanProject_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, ".mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}

	servers := []projectServerEntry{
		{
			ID:          "server-1",
			Name:        "Test Server",
			EndpointURL: "http://localhost:8080",
			AuthType:    "bearer",
			AuthSecret:  "secret-token",
		},
		{
			ID:          "server-2",
			Name:        "Another Server",
			EndpointURL: "http://localhost:9090",
			AuthType:    "none",
		},
	}
	data, _ := json.Marshal(servers)
	if err := os.WriteFile(filepath.Join(mcpDir, "servers.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	scanner := NewProjectScanner(nil)
	entries, err := scanner.ScanProject(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].ID != "server-1" {
		t.Fatalf("entry 0 ID: got %q, want %q", entries[0].ID, "server-1")
	}
	if entries[0].Name != "Test Server" {
		t.Fatalf("entry 0 Name: got %q, want %q", entries[0].Name, "Test Server")
	}
	if entries[0].EndpointURL != "http://localhost:8080" {
		t.Fatalf("entry 0 EndpointURL: got %q", entries[0].EndpointURL)
	}
	if entries[0].AuthType != "bearer" {
		t.Fatalf("entry 0 AuthType: got %q", entries[0].AuthType)
	}
	if entries[0].AuthSecret != "secret-token" {
		t.Fatalf("entry 0 AuthSecret: got %q", entries[0].AuthSecret)
	}
	if entries[0].Source != MCPSourceProject {
		t.Fatalf("entry 0 Source: got %q, want %q", entries[0].Source, MCPSourceProject)
	}

	if entries[1].ID != "server-2" {
		t.Fatalf("entry 1 ID: got %q, want %q", entries[1].ID, "server-2")
	}
}

func TestScanProject_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, ".mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mcpDir, "servers.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	scanner := NewProjectScanner(nil)
	_, err := scanner.ScanProject(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestScanProject_SkipsMissingFields(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, ".mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// One valid entry, one missing ID, one missing name, one missing endpoint.
	raw := `[
		{"id":"ok","name":"OK","endpoint_url":"http://localhost:1234"},
		{"name":"NoID","endpoint_url":"http://localhost:1234"},
		{"id":"no-name","endpoint_url":"http://localhost:1234"},
		{"id":"no-url","name":"NoURL"}
	]`
	if err := os.WriteFile(filepath.Join(mcpDir, "servers.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	scanner := NewProjectScanner(nil)
	entries, err := scanner.ScanProject(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 valid entry, got %d", len(entries))
	}
	if entries[0].ID != "ok" {
		t.Fatalf("expected entry ID %q, got %q", "ok", entries[0].ID)
	}
}

func TestScanProject_EmptyArray(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, ".mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mcpDir, "servers.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	scanner := NewProjectScanner(nil)
	entries, err := scanner.ScanProject(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}
