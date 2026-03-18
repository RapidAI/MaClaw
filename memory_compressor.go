package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryBackupInfo describes a single memory backup snapshot.
type MemoryBackupInfo struct {
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	SizeBytes  int64  `json:"size_bytes"`
	EntryCount int    `json:"entry_count"`
}

// CompressResult holds the outcome of a compression run.
type CompressResult struct {
	BackupName      string `json:"backup_name"`
	TotalEntries    int    `json:"total_entries"`
	DedupCount      int    `json:"dedup_count"`
	CompressedCount int    `json:"compressed_count"`
	SkippedCount    int    `json:"skipped_count"`
	ErrorCount      int    `json:"error_count"`
	SavedChars      int    `json:"saved_chars"`
}

// MemoryCompressorStatus is returned by the status query binding.
type MemoryCompressorStatus struct {
	Running    bool            `json:"running"`
	LastRun    string          `json:"last_run,omitempty"`
	LastResult *CompressResult `json:"last_result,omitempty"`
	LastError  string          `json:"last_error,omitempty"`
}

// MemoryCompressor compresses long memory entries via LLM and manages backups.
type MemoryCompressor struct {
	store     *MemoryStore
	llmConfig MaclawLLMConfig
	client    *http.Client
	// minContentLen is the minimum content length (in runes) to consider for compression.
	minContentLen int

	// Background service fields
	app        *App
	mu         sync.Mutex
	running    bool
	cancelFn   context.CancelFunc
	lastRun    time.Time
	lastResult *CompressResult
	lastError  string
}

// NewMemoryCompressor creates a MemoryCompressor.
func NewMemoryCompressor(store *MemoryStore, cfg MaclawLLMConfig, app *App) *MemoryCompressor {
	return &MemoryCompressor{
		store:         store,
		llmConfig:     cfg,
		client:        &http.Client{Timeout: 60 * time.Second},
		minContentLen: 200,
		app:           app,
	}
}

// ---------------------------------------------------------------------------
// One-shot compress (dedup + LLM compress)
// ---------------------------------------------------------------------------

// Compress performs dedup then LLM compression on long entries.
// A backup is created before any modification.
func (mc *MemoryCompressor) Compress(ctx context.Context) (*CompressResult, error) {
	if mc.store == nil {
		return nil, fmt.Errorf("memory store is nil")
	}

	// 1. Create a backup before any modification.
	backupName, err := mc.createBackup()
	if err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}

	result := &CompressResult{
		BackupName:   backupName,
		TotalEntries: mc.entryCount(),
	}

	// 2. Dedup — always runs, no LLM needed.
	result.DedupCount = mc.dedup()

	// 3. LLM compression — only if configured.
	if mc.isConfigured() {
		mc.store.mu.RLock()
		var candidates []MemoryEntry
		for _, e := range mc.store.entries {
			if len([]rune(e.Content)) >= mc.minContentLen {
				candidates = append(candidates, e)
			}
		}
		mc.store.mu.RUnlock()

		for _, entry := range candidates {
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			default:
			}

			compressed, err := mc.compressEntry(ctx, entry)
			if err != nil {
				result.ErrorCount++
				continue
			}
			if compressed == "" || len([]rune(compressed)) >= len([]rune(entry.Content)) {
				result.SkippedCount++
				continue
			}

			saved := len([]rune(entry.Content)) - len([]rune(compressed))
			if err := mc.store.Update(entry.ID, compressed, entry.Category, entry.Tags); err != nil {
				result.ErrorCount++
				continue
			}
			result.CompressedCount++
			result.SavedChars += saved
		}
	}

	result.TotalEntries = mc.entryCount() // refresh after dedup
	return result, nil
}

// ---------------------------------------------------------------------------
// Dedup logic
// ---------------------------------------------------------------------------

// dedup removes duplicate and near-duplicate memory entries.
// Two entries are considered duplicates if:
//   - Their content is identical (exact match), OR
//   - One content is a substring of the other within the same category
//
// When duplicates are found, the entry with the higher AccessCount (or newer
// UpdatedAt as tiebreaker) is kept; the others are removed.
// Returns the number of entries removed.
func (mc *MemoryCompressor) dedup() int {
	mc.store.mu.Lock()
	defer mc.store.mu.Unlock()

	n := len(mc.store.entries)
	if n < 2 {
		return 0
	}

	// Pre-compute lowercased content to avoid repeated allocations.
	lower := make([]string, n)
	for i, e := range mc.store.entries {
		lower[i] = strings.TrimSpace(strings.ToLower(e.Content))
	}

	// Mark indices to remove.
	remove := make(map[int]bool)

	for i := 0; i < n; i++ {
		if remove[i] {
			continue
		}
		for j := i + 1; j < n; j++ {
			if remove[j] {
				continue
			}
			if !mc.isDuplicateLower(mc.store.entries[i], mc.store.entries[j], lower[i], lower[j]) {
				continue
			}
			// Keep the "better" entry.
			loser := mc.pickLoser(i, j)
			remove[loser] = true
		}
	}

	if len(remove) == 0 {
		return 0
	}

	kept := make([]MemoryEntry, 0, n-len(remove))
	for i, e := range mc.store.entries {
		if !remove[i] {
			kept = append(kept, e)
		}
	}
	mc.store.entries = kept
	mc.store.dirty = true
	mc.store.signalSave()
	return len(remove)
}

// minSubstringLen is the minimum rune length for substring-based dedup.
// Entries shorter than this are only deduped by exact match to avoid
// false positives (e.g. "go" matching "go build -o app").
const minSubstringLen = 20

// isDuplicate checks if two entries are duplicates.
func (mc *MemoryCompressor) isDuplicate(a, b MemoryEntry) bool {
	ca := strings.TrimSpace(strings.ToLower(a.Content))
	cb := strings.TrimSpace(strings.ToLower(b.Content))
	return mc.isDuplicateLower(a, b, ca, cb)
}

// isDuplicateLower is the inner dedup check using pre-computed lowercase content.
func (mc *MemoryCompressor) isDuplicateLower(a, b MemoryEntry, ca, cb string) bool {
	// Exact match.
	if ca == cb {
		return true
	}

	// Substring match within the same category — only when both sides are
	// long enough to avoid aggressive false positives.
	if a.Category == b.Category {
		runeA, runeB := len([]rune(ca)), len([]rune(cb))
		shorter := runeA
		if runeB < shorter {
			shorter = runeB
		}
		if shorter >= minSubstringLen {
			if strings.Contains(ca, cb) || strings.Contains(cb, ca) {
				return true
			}
		}
	}

	return false
}

// pickLoser returns the index of the entry that should be removed.
// When one entry is a substring of the other, the shorter one is always the
// loser (the longer entry contains more information). Otherwise we prefer
// keeping higher AccessCount; ties broken by newer UpdatedAt.
func (mc *MemoryCompressor) pickLoser(i, j int) int {
	ei := mc.store.entries[i]
	ej := mc.store.entries[j]

	li := len([]rune(ei.Content))
	lj := len([]rune(ej.Content))

	// If lengths differ significantly (substring case), keep the longer one.
	if li != lj {
		if li > lj {
			return j // j is shorter → loser
		}
		return i // i is shorter → loser
	}

	// Same length (exact match case): prefer higher AccessCount.
	if ei.AccessCount != ej.AccessCount {
		if ei.AccessCount > ej.AccessCount {
			return j
		}
		return i
	}
	if ei.UpdatedAt.After(ej.UpdatedAt) {
		return j
	}
	return i
}

// ---------------------------------------------------------------------------
// Background service
// ---------------------------------------------------------------------------

// Start begins the background auto-compression service. It runs an initial
// compress immediately, then repeats every 6 hours. Calling Start when
// already running is a no-op.
func (mc *MemoryCompressor) Start() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if mc.running {
		return
	}
	mc.running = true
	ctx, cancel := context.WithCancel(context.Background())
	mc.cancelFn = cancel
	go mc.loop(ctx)
}

// Stop halts the background service.
func (mc *MemoryCompressor) Stop() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if !mc.running {
		return
	}
	mc.cancelFn()
	mc.running = false
}

// IsRunning returns whether the background service is active.
func (mc *MemoryCompressor) IsRunning() bool {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.running
}

// Status returns the current service status.
func (mc *MemoryCompressor) Status() MemoryCompressorStatus {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	s := MemoryCompressorStatus{Running: mc.running}
	if !mc.lastRun.IsZero() {
		s.LastRun = mc.lastRun.Format(time.RFC3339)
	}
	s.LastResult = mc.lastResult
	s.LastError = mc.lastError
	return s
}

func (mc *MemoryCompressor) loop(ctx context.Context) {
	// Run immediately on start.
	mc.runOnce(ctx)

	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Refresh LLM config each cycle in case user changed it.
			if mc.app != nil {
				cfg := mc.app.GetMaclawLLMConfig()
				mc.mu.Lock()
				mc.llmConfig = cfg
				mc.mu.Unlock()
			}
			mc.runOnce(ctx)
		}
	}
}

func (mc *MemoryCompressor) runOnce(ctx context.Context) {
	result, err := mc.Compress(ctx)
	mc.mu.Lock()
	mc.lastRun = time.Now()
	mc.lastResult = result
	if err != nil {
		mc.lastError = err.Error()
	} else {
		mc.lastError = ""
	}
	mc.mu.Unlock()

	// Emit event so the frontend can refresh.
	if mc.app != nil {
		mc.app.emitEvent("memory:compressed", result)
	}
}

// ---------------------------------------------------------------------------
// LLM compression helpers
// ---------------------------------------------------------------------------

func (mc *MemoryCompressor) compressEntry(ctx context.Context, entry MemoryEntry) (string, error) {
	// Snapshot LLM config under lock to avoid data race with loop().
	mc.mu.Lock()
	llmCfg := mc.llmConfig
	mc.mu.Unlock()

	systemPrompt := `You are a memory compression assistant. Your task is to compress the given memory content into a much shorter version while preserving ALL key facts, decisions, and actionable information. Rules:
- Keep the compressed version under 50% of the original length
- Preserve names, numbers, paths, commands, and technical terms exactly
- Remove filler words, redundant explanations, and verbose descriptions
- Use concise bullet points or short sentences
- Do NOT add any commentary — return ONLY the compressed content`

	userPrompt := fmt.Sprintf("Category: %s\nTags: %s\n\nOriginal content to compress:\n%s",
		entry.Category, strings.Join(entry.Tags, ", "), entry.Content)

	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": userPrompt},
	}

	result, err := doSimpleLLMRequest(llmCfg, messages, mc.client, 30*time.Second)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Content), nil
}

func (mc *MemoryCompressor) isConfigured() bool {
	mc.mu.Lock()
	cfg := mc.llmConfig
	mc.mu.Unlock()
	return strings.TrimSpace(cfg.URL) != "" &&
		strings.TrimSpace(cfg.Model) != ""
}

func (mc *MemoryCompressor) entryCount() int {
	mc.store.mu.RLock()
	defer mc.store.mu.RUnlock()
	return len(mc.store.entries)
}

// ---------------------------------------------------------------------------
// Backup management
// ---------------------------------------------------------------------------

func (mc *MemoryCompressor) backupDir() string {
	return filepath.Join(filepath.Dir(mc.store.path), "memory_backups")
}

func (mc *MemoryCompressor) createBackup() (string, error) {
	dir := mc.backupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	if err := mc.store.flush(); err != nil {
		return "", fmt.Errorf("flush before backup: %w", err)
	}
	data, err := os.ReadFile(mc.store.path)
	if err != nil {
		return "", fmt.Errorf("read memory file: %w", err)
	}
	name := fmt.Sprintf("memories_backup_%s.json", time.Now().Format("20060102_150405"))
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	return name, nil
}

// maxBackups is the maximum number of backup files to keep. Oldest are pruned.
const maxBackups = 30

func (mc *MemoryCompressor) ListBackups() ([]MemoryBackupInfo, error) {
	dir := mc.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var backups []MemoryBackupInfo
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		count := mc.countEntriesInFile(filepath.Join(dir, de.Name()))
		backups = append(backups, MemoryBackupInfo{
			Name:       de.Name(),
			CreatedAt:  info.ModTime().Format(time.RFC3339),
			SizeBytes:  info.Size(),
			EntryCount: count,
		})
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt > backups[j].CreatedAt
	})

	// Auto-prune oldest backups beyond the limit.
	if len(backups) > maxBackups {
		for _, old := range backups[maxBackups:] {
			_ = os.Remove(filepath.Join(dir, old.Name))
		}
		backups = backups[:maxBackups]
	}

	return backups, nil
}

func (mc *MemoryCompressor) RestoreBackup(backupName string) error {
	// Sanitize: reject path separators to prevent directory traversal.
	if strings.ContainsAny(backupName, `/\`) || backupName != filepath.Base(backupName) {
		return fmt.Errorf("invalid backup name: %s", backupName)
	}
	dir := mc.backupDir()
	src := filepath.Join(dir, backupName)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("backup not found: %s", backupName)
	}
	_, _ = mc.createBackup()
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	var entries []MemoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse backup: %w", err)
	}
	if err := os.WriteFile(mc.store.path, data, 0o644); err != nil {
		return fmt.Errorf("write memory file: %w", err)
	}
	mc.store.mu.Lock()
	mc.store.entries = entries
	mc.store.dirty = false
	mc.store.mu.Unlock()
	return nil
}

func (mc *MemoryCompressor) DeleteBackup(backupName string) error {
	// Sanitize: reject path separators to prevent directory traversal.
	if strings.ContainsAny(backupName, `/\`) || backupName != filepath.Base(backupName) {
		return fmt.Errorf("invalid backup name: %s", backupName)
	}
	p := filepath.Join(mc.backupDir(), backupName)
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("backup not found: %s", backupName)
	}
	return os.Remove(p)
}

func (mc *MemoryCompressor) countEntriesInFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	var entries []MemoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return -1
	}
	return len(entries)
}

func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
