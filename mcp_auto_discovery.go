package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// mcpDeclFile is the project-level MCP declaration filename.
const mcpDeclFile = ".mcp.json"

// globalMCPFile is the global MCP server registry filename under ~/.maclaw/.
const globalMCPFile = "mcp-servers.json"

// MCPDeclServer is a single server entry in a .mcp.json declaration file.
type MCPDeclServer struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	EndpointURL string   `json:"endpoint_url"`
	AuthType    string   `json:"auth_type"`
	AuthSecret  string   `json:"auth_secret"`
	Tags        []string `json:"tags"`
}

// MCPDeclFile is the top-level structure of a .mcp.json file.
type MCPDeclFile struct {
	Servers []MCPDeclServer `json:"servers"`
}

// MCPAutoDiscovery scans project and global MCP declaration files,
// registers discovered servers into MCPRegistry, and syncs their tools
// into the ToolRegistry for dynamic availability.
type MCPAutoDiscovery struct {
	app         *App
	registry    *ToolRegistry
	mcpRegistry *MCPRegistry
	watcher     *fsnotify.Watcher
	mu          sync.Mutex
	watching    map[string]bool // project paths being watched
	stopCh      chan struct{}
}

// NewMCPAutoDiscovery creates a new auto-discovery instance.
func NewMCPAutoDiscovery(app *App, registry *ToolRegistry, mcpRegistry *MCPRegistry) *MCPAutoDiscovery {
	return &MCPAutoDiscovery{
		app:         app,
		registry:    registry,
		mcpRegistry: mcpRegistry,
		watching:    make(map[string]bool),
		stopCh:      make(chan struct{}),
	}
}

// ScanProject reads {projectPath}/.mcp.json and registers discovered servers.
func (d *MCPAutoDiscovery) ScanProject(projectPath string) error {
	declPath := filepath.Join(projectPath, mcpDeclFile)
	return d.scanFile(declPath, MCPSourceProject)
}

// ScanGlobal reads ~/.maclaw/mcp-servers.json and registers global servers.
func (d *MCPAutoDiscovery) ScanGlobal() error {
	homeDir := d.app.GetUserHomeDir()
	globalPath := filepath.Join(homeDir, ".maclaw", globalMCPFile)
	return d.scanFile(globalPath, MCPSourceManual)
}

// scanFile parses a declaration file and registers each server.
func (d *MCPAutoDiscovery) scanFile(path string, source MCPServerSource) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no declaration file — not an error
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	var decl MCPDeclFile
	if err := json.Unmarshal(data, &decl); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	for _, srv := range decl.Servers {
		if srv.ID == "" || srv.EndpointURL == "" {
			continue
		}
		d.registerServer(srv, source)
	}
	return nil
}

// registerServer registers a declared MCP server into MCPRegistry and
// syncs its tools into the ToolRegistry.
func (d *MCPAutoDiscovery) registerServer(srv MCPDeclServer, source MCPServerSource) {
	entry := MCPServerEntry{
		ID:          srv.ID,
		Name:        srv.Name,
		EndpointURL: srv.EndpointURL,
		AuthType:    srv.AuthType,
		AuthSecret:  srv.AuthSecret,
		Source:      source,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	// Register into MCPRegistry (ignore duplicate errors).
	if err := d.mcpRegistry.Register(entry); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			log.Printf("[MCPAutoDiscovery] register %s: %v", srv.ID, err)
		}
	}

	// Sync tools into ToolRegistry.
	d.syncServerTools(srv)
}

// syncServerTools fetches tools from an MCP server and registers them
// into the ToolRegistry with category=mcp.
func (d *MCPAutoDiscovery) syncServerTools(srv MCPDeclServer) {
	if d.registry == nil || d.mcpRegistry == nil {
		return
	}

	// Use MCPRegistry's GetServerTools to get the server's tools.
	tools := d.mcpRegistry.GetServerTools(srv.ID)
	for _, t := range tools {
		toolName := fmt.Sprintf("mcp_%s_%s", srv.ID, t.Name)
		tags := append([]string{"mcp", srv.ID}, srv.Tags...)

		// Build input schema from MCPToolView.
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]interface{}{}
		}

		serverID := srv.ID
		mcpToolName := t.Name
		d.registry.Register(RegisteredTool{
			Name:        toolName,
			Description: fmt.Sprintf("[MCP:%s] %s", srv.Name, t.Description),
			Category:    ToolCategoryMCP,
			Tags:        tags,
			Priority:    0,
			Status:      RegToolAvailable,
			InputSchema: schema,
			Source:      "mcp:" + serverID,
			Handler: func(args map[string]interface{}) string {
				result, err := d.mcpRegistry.CallTool(serverID, mcpToolName, args)
				if err != nil {
					return fmt.Sprintf("MCP 工具调用失败: %v", err)
				}
				return result
			},
		})
	}
}

// WatchProject starts watching a project's .mcp.json for changes.
// On change, it re-scans and updates registrations.
func (d *MCPAutoDiscovery) WatchProject(projectPath string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.watching[projectPath] {
		return nil // already watching
	}

	if d.watcher == nil {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			return fmt.Errorf("create watcher: %w", err)
		}
		d.watcher = w
		go d.watchLoop()
	}

	if err := d.watcher.Add(projectPath); err != nil {
		return fmt.Errorf("watch %s: %w", projectPath, err)
	}
	d.watching[projectPath] = true
	return nil
}

// watchLoop processes fsnotify events.
func (d *MCPAutoDiscovery) watchLoop() {
	for {
		select {
		case event, ok := <-d.watcher.Events:
			if !ok {
				return
			}
			base := filepath.Base(event.Name)
			if base != mcpDeclFile {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				projectPath := filepath.Dir(event.Name)
				log.Printf("[MCPAutoDiscovery] detected change in %s, re-scanning", event.Name)
				if err := d.ScanProject(projectPath); err != nil {
					log.Printf("[MCPAutoDiscovery] re-scan error: %v", err)
				}
			}
		case err, ok := <-d.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[MCPAutoDiscovery] watcher error: %v", err)
		case <-d.stopCh:
			return
		}
	}
}

// Stop stops all file watchers and cleans up.
func (d *MCPAutoDiscovery) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	select {
	case <-d.stopCh:
		// already stopped
	default:
		close(d.stopCh)
	}

	if d.watcher != nil {
		d.watcher.Close()
		d.watcher = nil
	}
	d.watching = make(map[string]bool)
}
