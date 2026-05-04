package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"
)

// AgentNetTaskEntry is a task published by a AgentNet peer via Hub relay.
type AgentNetTaskEntry struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	Reward      float64   `json:"reward"`
	Creator     string    `json:"creator,omitempty"`
	PeerID      string    `json:"peer_id,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	CreatedAt   string    `json:"created_at,omitempty"`
	ExpiresAt   time.Time `json:"-"`
}

// AgentNetTaskBoard is an in-memory task bulletin board that aggregates
// tasks published by AgentNet peers through the Hub. Tasks expire after
// 24 hours to keep the board fresh.
type AgentNetTaskBoard struct {
	mu    sync.RWMutex
	tasks map[string]*AgentNetTaskEntry // keyed by "peerID:taskID"
}

var globalTaskBoard = &AgentNetTaskBoard{
	tasks: make(map[string]*AgentNetTaskEntry),
}

const taskTTL = 24 * time.Hour

func (b *AgentNetTaskBoard) publish(entry *AgentNetTaskEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := entry.PeerID + ":" + entry.ID
	entry.ExpiresAt = time.Now().Add(taskTTL)
	b.tasks[key] = entry
}

func (b *AgentNetTaskBoard) publishBatch(entries []AgentNetTaskEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for i := range entries {
		e := &entries[i]
		key := e.PeerID + ":" + e.ID
		e.ExpiresAt = now.Add(taskTTL)
		b.tasks[key] = e
	}
}

func (b *AgentNetTaskBoard) browse(limit int) []AgentNetTaskEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	now := time.Now()
	var result []AgentNetTaskEntry
	for _, t := range b.tasks {
		if now.Before(t.ExpiresAt) {
			result = append(result, *t)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt > result[j].CreatedAt
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

func (b *AgentNetTaskBoard) gc() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for k, t := range b.tasks {
		if now.After(t.ExpiresAt) {
			delete(b.tasks, k)
		}
	}
}

// AgentNetTaskPublishHandler accepts task announcements from AgentNet peers.
// POST /api/AgentNet/tasks/publish
// Body: single task or array of tasks
func AgentNetTaskPublishHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		// Try array first, then single object.
		var entries []AgentNetTaskEntry
		if err := json.Unmarshal(raw, &entries); err != nil {
			var single AgentNetTaskEntry
			if err2 := json.Unmarshal(raw, &single); err2 != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task format"})
				return
			}
			entries = []AgentNetTaskEntry{single}
		}
		// Validate
		for i := range entries {
			if entries[i].ID == "" || entries[i].Title == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and title are required"})
				return
			}
			if entries[i].Status == "" {
				entries[i].Status = "open"
			}
		}
		globalTaskBoard.publishBatch(entries)
		// Lazy GC
		globalTaskBoard.gc()
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "count": len(entries)})
	}
}

// AgentNetTaskBrowseHandler returns aggregated tasks from all peers.
// GET /api/AgentNet/tasks/browse?limit=50
func AgentNetTaskBrowseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n := 0; json.Unmarshal([]byte(v), &n) == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		tasks := globalTaskBoard.browse(limit)
		if tasks == nil {
			tasks = []AgentNetTaskEntry{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "tasks": tasks})
	}
}
