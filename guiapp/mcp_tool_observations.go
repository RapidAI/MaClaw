package guiapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

// mcpToolObservationRecord is the durable, non-secret copy of one server's
// last successful tools/list. The in-memory health/toolsCache pair dies with
// the process; without a durable copy an installed MCP silently disappeared
// from the semantic surface after every restart until the next background
// probe happened to run — which made "installed" mean "usable only for one
// process lifetime".
//
// The record is evidence, not authority: it is only honored while the
// configured endpoint identity still matches (endpoint digest), while it is
// fresh (mcpToolObservationMaxAge), and while no newer in-memory observation
// contradicts it. A finished negative observation this process always wins
// over a stale durable success.
type mcpToolObservationRecord struct {
	ServerID       string        `json:"server_id"`
	Transport      string        `json:"transport"` // "remote" | "local"
	EndpointDigest string        `json:"endpoint_digest"`
	ObservedAt     string        `json:"observed_at"`
	Tools          []MCPToolView `json:"tools"`
}

// mcpToolObservationMaxAge bounds how long a durable tools/list counts as a
// finished successful observation. Past it the record still exists on disk
// but admits nothing; the background refresh must re-observe.
const mcpToolObservationMaxAge = 24 * time.Hour

// mcpObservationRefreshMinInterval bounds how often a turn may kick off a
// background revalidation probe for the same server. The refresh exists so a
// durable observation stays a cache, never a substitute for re-checking.
const mcpObservationRefreshMinInterval = time.Minute

var mcpToolObservationMu sync.Mutex

// mcpObservationRefreshInFlight dedupes background revalidation probes keyed
// by "remote:<id>" / "local:<id>" -> last kick time.
var mcpObservationRefreshInFlight sync.Map

func mcpToolObservationPath() string {
	return filepath.Join(corelib.MaclawBaseDir(), "skill_evolution", "mcp_tool_observations.json")
}

func mcpToolObservationKey(kind, serverID string) string {
	return strings.TrimSpace(kind) + ":" + strings.TrimSpace(serverID)
}

func readMCPToolObservations() (map[string]mcpToolObservationRecord, error) {
	path := mcpToolObservationPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]mcpToolObservationRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	var records map[string]mcpToolObservationRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("decode MCP tool observations: %w", err)
	}
	if records == nil {
		records = map[string]mcpToolObservationRecord{}
	}
	for key, record := range records {
		if strings.TrimSpace(record.ServerID) == "" || (record.Transport != "remote" && record.Transport != "local") {
			return nil, fmt.Errorf("invalid MCP tool observation identity %q", key)
		}
		if key != mcpToolObservationKey(record.Transport, record.ServerID) {
			return nil, fmt.Errorf("MCP tool observation key mismatch for %q", key)
		}
		if strings.TrimSpace(record.ObservedAt) != "" {
			if _, err := time.Parse(time.RFC3339, record.ObservedAt); err != nil {
				return nil, fmt.Errorf("invalid MCP tool observation time for %q", key)
			}
		}
	}
	return records, nil
}

func writeMCPToolObservations(records map[string]mcpToolObservationRecord) error {
	path := mcpToolObservationPath()
	if len(records) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return configfile.AtomicWriteJSON(path, records)
}

// recordMCPToolObservation merges one finished tools/list observation into
// the durable store. A write failure is logged and simply leaves the record
// absent — which keeps the next restart fail-closed rather than asserting an
// observation that may not be durable.
func recordMCPToolObservation(kind, serverID, endpointDigest string, tools []MCPToolView) {
	serverID = strings.TrimSpace(serverID)
	kind = strings.TrimSpace(kind)
	if serverID == "" || strings.TrimSpace(endpointDigest) == "" {
		return
	}
	mcpToolObservationMu.Lock()
	defer mcpToolObservationMu.Unlock()
	records, err := readMCPToolObservations()
	if err != nil {
		log.Printf("[MCP] read tool observations for write: %v", err)
		return
	}
	record := records[mcpToolObservationKey(kind, serverID)]
	record.ServerID = serverID
	record.Transport = kind
	record.EndpointDigest = strings.TrimSpace(endpointDigest)
	record.ObservedAt = time.Now().UTC().Format(time.RFC3339)
	record.Tools = append([]MCPToolView(nil), tools...)
	records[mcpToolObservationKey(kind, serverID)] = record
	if err := writeMCPToolObservations(records); err != nil {
		log.Printf("[MCP] persist tool observation for %s: %v", serverID, err)
	}
}

// clearMCPToolObservation drops the durable record for a removed or
// re-configured server. A cache miss is not an error.
func clearMCPToolObservation(kind, serverID string) {
	mcpToolObservationMu.Lock()
	defer mcpToolObservationMu.Unlock()
	records, err := readMCPToolObservations()
	if err != nil {
		log.Printf("[MCP] read tool observations for clear: %v", err)
		return
	}
	key := mcpToolObservationKey(kind, serverID)
	if _, ok := records[key]; !ok {
		return
	}
	delete(records, key)
	if err := writeMCPToolObservations(records); err != nil {
		log.Printf("[MCP] clear tool observation for %s: %v", serverID, err)
	}
}

// mcpToolObservationEndpointDigest derives the configured-endpoint identity a
// durable observation is bound to. Any change to the entry — endpoint,
// command, args, env, headers, auth — produces a different digest, so a
// reconfigured server never inherits the previous configuration's tools.
func mcpToolObservationEndpointDigest(entry interface{}) string {
	data, err := json.Marshal(entry)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// mcpToolObservationFresh reports whether record still counts as a finished
// successful observation for the CURRENT configured endpoint.
func mcpToolObservationFresh(record mcpToolObservationRecord, endpointDigest string) bool {
	if strings.TrimSpace(record.EndpointDigest) == "" || record.EndpointDigest != strings.TrimSpace(endpointDigest) {
		return false
	}
	observed, err := time.Parse(time.RFC3339, strings.TrimSpace(record.ObservedAt))
	if err != nil {
		return false
	}
	return time.Since(observed) < mcpToolObservationMaxAge
}

// loadMCPToolObservation reads one durable record without judging it; the
// caller decides freshness against the current endpoint digest.
func loadMCPToolObservation(kind, serverID string) (mcpToolObservationRecord, bool) {
	mcpToolObservationMu.Lock()
	defer mcpToolObservationMu.Unlock()
	records, err := readMCPToolObservations()
	if err != nil {
		return mcpToolObservationRecord{}, false
	}
	record, ok := records[mcpToolObservationKey(kind, serverID)]
	return record, ok
}

// durableRemoteMCPTools returns the durably-observed tools/list for a remote
// server when it is still fresh for the current configured endpoint. The
// caller uses this only when the process has no finished observation of its
// own — a finished in-memory failure always stays authoritative.
func durableRemoteMCPTools(registry *MCPRegistry, serverID string) ([]MCPToolView, bool) {
	if registry == nil {
		return nil, false
	}
	entry, err := registry.findServer(serverID)
	if err != nil || entry == nil {
		return nil, false
	}
	record, ok := loadMCPToolObservation("remote", serverID)
	if !ok || !mcpToolObservationFresh(record, mcpToolObservationEndpointDigest(*entry)) {
		return nil, false
	}
	return append([]MCPToolView(nil), record.Tools...), true
}

// durableLocalMCPTools is the local-stdio counterpart: the entry's command,
// args and env are the endpoint contract.
func durableLocalMCPTools(registry *MCPRegistry, entry corelib.LocalMCPServerEntry) ([]MCPToolView, bool) {
	if registry == nil || strings.TrimSpace(entry.ID) == "" {
		return nil, false
	}
	record, ok := loadMCPToolObservation("local", entry.ID)
	if !ok || !mcpToolObservationFresh(record, mcpToolObservationEndpointDigest(entry)) {
		return nil, false
	}
	return append([]MCPToolView(nil), record.Tools...), true
}

// recordRemoteToolObservation persists a finished remote tools/list. Called
// from the single publication point so every successful observation — health
// probe, lazy fetch, or startup sync — lands here exactly once.
func (r *MCPRegistry) recordRemoteToolObservation(serverID string, tools []MCPToolView) {
	if r == nil {
		return
	}
	entry, err := r.findServer(serverID)
	if err != nil || entry == nil {
		return
	}
	recordMCPToolObservation("remote", serverID, mcpToolObservationEndpointDigest(*entry), tools)
}

// recordLocalToolObservation persists a finished local tools/list.
func (m *LocalMCPManager) recordLocalToolObservation(serverID string, tools []MCPToolView) {
	if m == nil {
		return
	}
	entry, ok := m.localServerEntry(serverID)
	if !ok {
		return
	}
	recordMCPToolObservation("local", serverID, mcpToolObservationEndpointDigest(entry), tools)
}

// scheduleRemoteMCPObservationRefresh kicks an asynchronous health probe so a
// durable observation admitted this turn gets revalidated instead of trusted
// indefinitely. Deduped per server; failures just leave the durable record to
// expire on its TTL.
func scheduleRemoteMCPObservationRefresh(registry *MCPRegistry, serverID string) {
	if registry == nil || strings.TrimSpace(serverID) == "" {
		return
	}
	key := "remote:" + strings.TrimSpace(serverID)
	now := time.Now()
	raw, loaded := mcpObservationRefreshInFlight.LoadOrStore(key, now)
	if loaded {
		last, ok := raw.(time.Time)
		if ok && now.Sub(last) < mcpObservationRefreshMinInterval {
			return
		}
		mcpObservationRefreshInFlight.Store(key, now)
	}
	go func() {
		if err := registry.HealthCheck(serverID); err != nil {
			log.Printf("[MCP] background observation refresh for %s: %v", serverID, err)
		}
	}()
}
