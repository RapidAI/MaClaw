package guiapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const agentViewSchemaVersionField = "_agent_view_schema_version"

var agentViewSchemaVersionCache = struct {
	sync.Mutex
	items map[string]string
}{items: map[string]string{}}

type agentViewSchemaRecord struct {
	Version   string    `json:"version"`
	Source    string    `json:"source"`
	ID        string    `json:"id"`
	ViewID    string    `json:"view_id,omitempty"`
	ViewType  string    `json:"view_type,omitempty"`
	Title     string    `json:"title,omitempty"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	UseCount  int       `json:"use_count"`
}

type agentViewSchemaSnapshot struct {
	UpdatedAt time.Time               `json:"updated_at"`
	Records   []agentViewSchemaRecord `json:"records"`
}

var agentViewSchemaPersistentStore = struct {
	sync.Mutex
	database *agentViewSchemaDatabase
}{}

func agentViewSchemaVersion(source, id string, contract interface{}) string {
	source = strings.TrimSpace(source)
	id = strings.TrimSpace(id)
	payload, _ := json.Marshal(contract)
	cacheKey := source + "\x00" + id + "\x00" + string(payload)
	agentViewSchemaVersionCache.Lock()
	defer agentViewSchemaVersionCache.Unlock()
	if version := agentViewSchemaVersionCache.items[cacheKey]; version != "" {
		return version
	}
	// Evict all entries when cache exceeds max size to prevent unbounded memory growth.
	// The cache is a pure computation cache (SHA256 of inputs), so eviction only costs
	// re-computation on next access, not data loss.
	if len(agentViewSchemaVersionCache.items) >= 512 {
		agentViewSchemaVersionCache.items = make(map[string]string, 256)
	}
	sum := sha256.Sum256([]byte(cacheKey))
	version := hex.EncodeToString(sum[:8])
	agentViewSchemaVersionCache.items[cacheKey] = version
	return version
}

func attachAgentViewSchemaVersion(view map[string]interface{}, source, id string, contract interface{}) map[string]interface{} {
	if view == nil {
		return nil
	}
	version := agentViewSchemaVersion(source, id, contract)
	meta, _ := view["meta"].(map[string]interface{})
	if meta == nil {
		meta = map[string]interface{}{}
		view["meta"] = meta
	}
	meta["schemaVersion"] = version
	meta["schemaSource"] = strings.TrimSpace(source)
	meta["schemaID"] = strings.TrimSpace(id)
	appendAgentViewHiddenField(view, agentViewSchemaVersionField, version)
	return view
}

func agentViewSchemaStorePath(a *App) string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "agent_view_schemas.json")
}

func (a *App) recordAgentViewSchema(view map[string]interface{}) {
	_ = recordAgentViewSchema(agentViewSchemaStorePath(a), view)
}

func recordAgentViewSchema(path string, view map[string]interface{}) error {
	path = strings.TrimSpace(path)
	if path == "" || view == nil {
		return nil
	}
	meta, _ := view["meta"].(map[string]interface{})
	version := strings.TrimSpace(fmt.Sprint(meta["schemaVersion"]))
	source := strings.TrimSpace(fmt.Sprint(meta["schemaSource"]))
	id := strings.TrimSpace(fmt.Sprint(meta["schemaID"]))
	if version == "" || source == "" || id == "" {
		return nil
	}
	now := time.Now()
	record := agentViewSchemaRecord{
		Version:   version,
		Source:    source,
		ID:        id,
		ViewID:    strings.TrimSpace(fmt.Sprint(view["id"])),
		ViewType:  strings.TrimSpace(fmt.Sprint(view["type"])),
		Title:     strings.TrimSpace(fmt.Sprint(view["title"])),
		FirstSeen: now,
		LastSeen:  now,
		UseCount:  1,
	}
	agentViewSchemaPersistentStore.Lock()
	defer agentViewSchemaPersistentStore.Unlock()
	return upsertAgentViewSchemaRecordLocked(path, record)
}

func appendAgentViewHiddenField(view map[string]interface{}, name string, value interface{}) {
	if view == nil || strings.TrimSpace(name) == "" {
		return
	}
	if rawFields, ok := view["fields"].([]map[string]interface{}); ok {
		for _, field := range rawFields {
			if strings.TrimSpace(fmt.Sprint(field["name"])) == name {
				field["value"] = value
				return
			}
		}
		view["fields"] = append(rawFields, map[string]interface{}{
			"name":  name,
			"label": name,
			"type":  "hidden",
			"value": value,
		})
		return
	}
	// Builders sometimes store fields as []interface{} of maps.
	if list, ok := view["fields"].([]interface{}); ok {
		for _, item := range list {
			field, _ := item.(map[string]interface{})
			if field == nil {
				continue
			}
			if strings.TrimSpace(fmt.Sprint(field["name"])) == name {
				field["value"] = value
				return
			}
		}
		view["fields"] = append(list, map[string]interface{}{
			"name":  name,
			"label": name,
			"type":  "hidden",
			"value": value,
		})
		return
	}
	// No fields slice yet — create a typed map slice for later form rendering.
	view["fields"] = []map[string]interface{}{{
		"name":  name,
		"label": name,
		"type":  "hidden",
		"value": value,
	}}
}
