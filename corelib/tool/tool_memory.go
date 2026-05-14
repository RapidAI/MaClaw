package tool

// ToolMemory: per-tool persistent rule store.
// Inspired by OpenHuman's memory/tool_memory/ module — each tool has its own
// namespace of learned rules that are automatically injected before execution.
//
// Rules are learned from tool execution patterns:
// - SSH: "api.rapidai.tech needs source /etc/profile after connect"
// - bash: "project X needs cd /path/to/project first"
// - web_fetch: "site Y requires User-Agent header"
//
// Rules have confidence scores that increase with successful use and decay
// with failures. Rules below a confidence threshold are auto-pruned.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ToolRule represents a learned rule for a specific tool + context.
type ToolRule struct {
	Key        string    `json:"key"`        // unique identifier (e.g. "ssh:api.rapidai.tech:profile_source")
	ToolName   string    `json:"tool_name"`  // which tool this rule applies to
	Context    string    `json:"context"`    // context key for matching (e.g. host, project path)
	Content    string    `json:"content"`    // the rule text to inject
	Confidence float64   `json:"confidence"` // 0.0-1.0, increases with confirmation
	LearnedAt  time.Time `json:"learned_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	UsedCount  int       `json:"used_count"`
	FailCount  int       `json:"fail_count"` // times the rule was applied but tool still failed
}

// ToolMemoryStore manages per-tool persistent rules.
type ToolMemoryStore struct {
	mu      sync.RWMutex
	rules   []ToolRule
	byTool  map[string][]int // toolName → indices into rules slice
	path    string           // persistence file path
	dirty   bool
}

const (
	// minConfidence is the threshold below which rules are pruned.
	minConfidence = 0.2
	// maxRulesPerTool limits rules per tool to prevent unbounded growth.
	maxRulesPerTool = 20
	// maxTotalRules limits total rules across all tools.
	maxTotalRules = 200
	// initialConfidence for newly learned rules.
	initialConfidence = 0.5
	// confirmBoost is added to confidence when a rule is confirmed effective.
	confirmBoost = 0.15
	// failPenalty is subtracted from confidence when a rule doesn't help.
	failPenalty = 0.1
	// maxInjectRules limits how many rules are injected per tool call.
	maxInjectRules = 3
)

// NewToolMemoryStore creates a store that persists to the given path.
// If path is empty, operates in memory-only mode.
func NewToolMemoryStore(path string) *ToolMemoryStore {
	s := &ToolMemoryStore{path: path, byTool: make(map[string][]int)}
	if path != "" {
		s.load()
	}
	s.rebuildIndex()
	return s
}

// InjectRules returns relevant rules for a tool call, formatted for injection
// into the conversation as a system hint before tool execution.
// contextKeys are values extracted from tool arguments that help match rules
// (e.g. SSH host, file path, project directory).
func (s *ToolMemoryStore) InjectRules(toolName string, contextKeys []string) string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	indices := s.byTool[toolName]
	if len(indices) == 0 {
		return ""
	}

	var matched []ToolRule
	for _, idx := range indices {
		if idx >= len(s.rules) {
			continue
		}
		r := s.rules[idx]
		if r.Confidence < minConfidence {
			continue
		}
		// Match by context: rule context must be a substring of any context key
		if r.Context != "" && len(contextKeys) > 0 {
			found := false
			for _, ck := range contextKeys {
				if strings.Contains(ck, r.Context) || strings.Contains(r.Context, ck) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		matched = append(matched, r)
	}

	if len(matched) == 0 {
		return ""
	}

	// Sort by confidence (highest first), then by recency.
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Confidence != matched[j].Confidence {
			return matched[i].Confidence > matched[j].Confidence
		}
		return matched[i].LastUsedAt.After(matched[j].LastUsedAt)
	})

	// Limit injection count
	n := maxInjectRules
	if len(matched) < n {
		n = len(matched)
	}

	var sb strings.Builder
	sb.WriteString("[工具记忆] ")
	sb.WriteString(toolName)
	sb.WriteString(" 相关经验：\n")
	for i := 0; i < n; i++ {
		r := matched[i]
		sb.WriteString(fmt.Sprintf("- %s (置信度 %.0f%%)\n", r.Content, r.Confidence*100))
	}
	return sb.String()
}

// LearnRule adds or updates a rule. If a rule with the same key exists,
// its confidence is boosted. Otherwise a new rule is created.
func (s *ToolMemoryStore) LearnRule(toolName, key, context, content string) {
	if s == nil || toolName == "" || key == "" || content == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Check if rule already exists
	for i := range s.rules {
		if s.rules[i].Key == key {
			s.rules[i].Confidence = clampConfidence(s.rules[i].Confidence + confirmBoost)
			s.rules[i].LastUsedAt = now
			s.rules[i].UsedCount++
			s.rules[i].Content = content // update content in case it changed
			s.dirty = true
			return
		}
	}

	// Create new rule
	newIdx := len(s.rules)
	s.rules = append(s.rules, ToolRule{
		Key:        key,
		ToolName:   toolName,
		Context:    context,
		Content:    content,
		Confidence: initialConfidence,
		LearnedAt:  now,
		LastUsedAt: now,
		UsedCount:  1,
	})
	s.byTool[toolName] = append(s.byTool[toolName], newIdx)
	s.dirty = true

	// Enforce limits (may rebuild index)
	s.enforceLimits(toolName)
}

// ConfirmRule boosts confidence of a rule (tool succeeded after rule was applied).
func (s *ToolMemoryStore) ConfirmRule(key string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.rules {
		if s.rules[i].Key == key {
			s.rules[i].Confidence = clampConfidence(s.rules[i].Confidence + confirmBoost)
			s.rules[i].LastUsedAt = time.Now()
			s.rules[i].UsedCount++
			s.dirty = true
			return
		}
	}
}

// RecordFailure decreases confidence of a rule (tool failed despite rule being applied).
func (s *ToolMemoryStore) RecordFailure(key string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.rules {
		if s.rules[i].Key == key {
			s.rules[i].Confidence = clampConfidence(s.rules[i].Confidence - failPenalty)
			s.rules[i].FailCount++
			s.dirty = true
			return
		}
	}
}

// GetRules returns all rules for a tool (for debugging/display).
func (s *ToolMemoryStore) GetRules(toolName string) []ToolRule {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []ToolRule
	for _, r := range s.rules {
		if r.ToolName == toolName {
			result = append(result, r)
		}
	}
	return result
}

// AllRules returns all rules (for debugging/display).
func (s *ToolMemoryStore) AllRules() []ToolRule {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ToolRule, len(s.rules))
	copy(out, s.rules)
	return out
}

// Flush persists rules to disk if dirty.
func (s *ToolMemoryStore) Flush() {
	if s == nil || s.path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	s.saveLocked()
}

// Prune removes rules below minConfidence threshold.
func (s *ToolMemoryStore) Prune() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	before := len(s.rules)
	var kept []ToolRule
	for _, r := range s.rules {
		if r.Confidence >= minConfidence {
			kept = append(kept, r)
		}
	}
	s.rules = kept
	pruned := before - len(kept)
	if pruned > 0 {
		s.dirty = true
		s.rebuildIndex()
		log.Printf("[tool-memory] pruned %d low-confidence rules", pruned)
	}
	return pruned
}

// --- Internal ---

func (s *ToolMemoryStore) rebuildIndex() {
	s.byTool = make(map[string][]int)
	for i, r := range s.rules {
		s.byTool[r.ToolName] = append(s.byTool[r.ToolName], i)
	}
}

func (s *ToolMemoryStore) enforceLimits(toolName string) {
	// Per-tool limit
	toolCount := 0
	for _, r := range s.rules {
		if r.ToolName == toolName {
			toolCount++
		}
	}
	if toolCount > maxRulesPerTool {
		s.evictLowestConfidence(toolName, toolCount-maxRulesPerTool)
	}

	// Total limit
	if len(s.rules) > maxTotalRules {
		s.evictOldest(len(s.rules) - maxTotalRules)
	}
}

func (s *ToolMemoryStore) evictLowestConfidence(toolName string, count int) {
	type indexed struct {
		idx  int
		conf float64
	}
	var candidates []indexed
	for i, r := range s.rules {
		if r.ToolName == toolName {
			candidates = append(candidates, indexed{i, r.Confidence})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].conf < candidates[j].conf
	})
	toRemove := make(map[int]bool)
	for i := 0; i < count && i < len(candidates); i++ {
		toRemove[candidates[i].idx] = true
	}
	var kept []ToolRule
	for i, r := range s.rules {
		if !toRemove[i] {
			kept = append(kept, r)
		}
	}
	s.rules = kept
	s.rebuildIndex()
}

func (s *ToolMemoryStore) evictOldest(count int) {
	sort.Slice(s.rules, func(i, j int) bool {
		return s.rules[i].LastUsedAt.Before(s.rules[j].LastUsedAt)
	})
	if count >= len(s.rules) {
		s.rules = nil
		s.rebuildIndex()
		return
	}
	s.rules = s.rules[count:]
	s.rebuildIndex()
}

func (s *ToolMemoryStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return // file doesn't exist yet, start empty
	}
	var rules []ToolRule
	if err := json.Unmarshal(data, &rules); err != nil {
		log.Printf("[tool-memory] failed to parse %s: %v", s.path, err)
		return
	}
	s.rules = rules
}

func (s *ToolMemoryStore) saveLocked() {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[tool-memory] failed to create dir %s: %v", dir, err)
		return
	}
	data, err := json.MarshalIndent(s.rules, "", "  ")
	if err != nil {
		log.Printf("[tool-memory] failed to marshal rules: %v", err)
		return
	}
	if err := os.WriteFile(s.path, data, 0644); err != nil {
		log.Printf("[tool-memory] failed to write %s: %v", s.path, err)
		return
	}
	s.dirty = false
}

func clampConfidence(c float64) float64 {
	if c > 1.0 {
		return 1.0
	}
	if c < 0.0 {
		return 0.0
	}
	return c
}

// ExtractContextKeys extracts context keys from tool arguments for rule matching.
// Different tools have different relevant context:
// - ssh: host, session_id
// - bash: working directory, command prefix
// - web_fetch: domain
// - write_file: directory path
func ExtractContextKeys(toolName string, args map[string]interface{}) []string {
	var keys []string
	switch toolName {
	case "ssh":
		if host, ok := args["host"].(string); ok && host != "" {
			keys = append(keys, host)
		}
		if sid, ok := args["session_id"].(string); ok && sid != "" {
			keys = append(keys, sid)
		}
	case "bash":
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			// Extract first word as context
			parts := strings.Fields(cmd)
			if len(parts) > 0 {
				keys = append(keys, parts[0])
			}
		}
	case "web_fetch":
		if u, ok := args["url"].(string); ok && u != "" {
			// Extract domain
			if idx := strings.Index(u, "://"); idx >= 0 {
				rest := u[idx+3:]
				if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
					keys = append(keys, rest[:slashIdx])
				} else {
					keys = append(keys, rest)
				}
			}
		}
	case "write_file", "read_file", "edit_file":
		if path, ok := args["path"].(string); ok && path != "" {
			dir := filepath.Dir(path)
			keys = append(keys, dir)
		}
	}
	return keys
}
