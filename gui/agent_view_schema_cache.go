package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/configfile"
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
	loadedPath string
	items      map[string]agentViewSchemaRecord
}{items: map[string]agentViewSchemaRecord{}}

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
	agentViewSchemaPersistentStore.Lock()
	defer agentViewSchemaPersistentStore.Unlock()
	if err := ensureAgentViewSchemaStoreLoadedLocked(path); err != nil {
		return err
	}
	now := time.Now()
	record := agentViewSchemaPersistentStore.items[version]
	if record.Version == "" {
		record = agentViewSchemaRecord{
			Version:   version,
			Source:    source,
			ID:        id,
			FirstSeen: now,
		}
	}
	record.Source = source
	record.ID = id
	record.ViewID = strings.TrimSpace(fmt.Sprint(view["id"]))
	record.ViewType = strings.TrimSpace(fmt.Sprint(view["type"]))
	record.Title = strings.TrimSpace(fmt.Sprint(view["title"]))
	record.LastSeen = now
	record.UseCount++
	agentViewSchemaPersistentStore.items[version] = record
	return saveAgentViewSchemaStoreLocked(path)
}

func ensureAgentViewSchemaStoreLoadedLocked(path string) error {
	if agentViewSchemaPersistentStore.loadedPath == path {
		return nil
	}
	agentViewSchemaPersistentStore.loadedPath = path
	agentViewSchemaPersistentStore.items = map[string]agentViewSchemaRecord{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var snapshot agentViewSchemaSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	for _, record := range snapshot.Records {
		if strings.TrimSpace(record.Version) != "" {
			agentViewSchemaPersistentStore.items[record.Version] = record
		}
	}
	return nil
}

func saveAgentViewSchemaStoreLocked(path string) error {
	snapshot := agentViewSchemaSnapshot{UpdatedAt: time.Now()}
	for _, record := range agentViewSchemaPersistentStore.items {
		snapshot.Records = append(snapshot.Records, record)
	}
	sortAgentViewSchemaRecords(snapshot.Records)
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return configfile.AtomicWrite(path, append(data, '\n'))
}

func sortAgentViewSchemaRecords(records []agentViewSchemaRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].LastSeen.Equal(records[j].LastSeen) {
			return records[i].Version < records[j].Version
		}
		return records[i].LastSeen.After(records[j].LastSeen)
	})
}

func appendAgentViewHiddenField(view map[string]interface{}, name string, value interface{}) {
	rawFields, ok := view["fields"].([]map[string]interface{})
	if !ok {
		return
	}
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
}
