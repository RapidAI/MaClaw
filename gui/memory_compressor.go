package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// MemoryBackupInfo describes a single memory backup snapshot.
type MemoryBackupInfo struct {
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	SizeBytes  int64  `json:"size_bytes"`
	EntryCount int    `json:"entry_count"`
}

// MemoryCompressorStatus is returned by the status query binding.
type MemoryCompressorStatus struct {
	Running    bool                   `json:"running"`
	LastRun    string                 `json:"last_run,omitempty"`
	LastResult *memory.CompressResult `json:"last_result,omitempty"`
	LastError  string                 `json:"last_error,omitempty"`
}

// MemoryCompressor compresses long memory entries via LLM and manages backups.
type MemoryCompressor struct {
	store     *memory.Store
	llmConfig corelib.MaclawLLMConfig
	client    *http.Client
	// minContentLen is the minimum content length (in runes) to consider for compression.
	minContentLen int
	// maxBackups is the configurable limit on backup files to keep. 0 means use defaultMaxBackups.
	maxBackups int

	// Background service fields
	app        *App
	mu         sync.Mutex
	running    bool
	cancelFn   context.CancelFunc
	lastRun    time.Time
	lastResult *memory.CompressResult
	lastError  string

	// compressCount tracks the number of in-flight Compress() calls.
	// Protected by mu. Incremented at Compress() entry, decremented at exit.
	// IsCompressing() returns compressCount > 0.
	// An atomic counter (not a bool) because manual and auto compress can overlap.
	compressCount int
}

// NewMemoryCompressor creates a MemoryCompressor.
func NewMemoryCompressor(store *memory.Store, cfg corelib.MaclawLLMConfig, app *App) *MemoryCompressor {
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
func (mc *MemoryCompressor) Compress(ctx context.Context) (*memory.CompressResult, error) {
	if mc.store == nil {
		return nil, fmt.Errorf("memory store is nil")
	}

	mc.mu.Lock()
	mc.compressCount++
	mc.mu.Unlock()
	defer func() {
		mc.mu.Lock()
		mc.compressCount--
		mc.mu.Unlock()
	}()

	// 1. Create a backup before any modification.
	backupName, err := mc.createBackup()
	if err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}

	result := &memory.CompressResult{
		BackupName:   backupName,
		TotalEntries: mc.entryCount(),
	}

	// 2. Dedup — always runs, no LLM needed.
	result.DedupCount = mc.dedup()

	// 3. LLM semantic merge — group by category, ask LLM to merge duplicates.
	if mc.isConfigured() {
		merged, mergeErr := mc.mergeSemanticDuplicates(ctx)
		if mergeErr == nil {
			result.MergedCount = merged
		}
	}

	// 4. LLM compression — only if configured.
	if mc.isConfigured() {
		mc.store.RLock()
		var candidates []memory.Entry
		for _, e := range mc.store.Entries() {
			if e.Pinned {
				continue
			}
			if len([]rune(e.Content)) >= mc.minContentLen {
				candidates = append(candidates, e)
			}
		}
		mc.store.RUnlock()

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

	// 5. Backfill CompactForm for entries missing it.
	if mc.isConfigured() {
		mc.backfillCompactForms(ctx)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// CompactForm backfill
// ---------------------------------------------------------------------------

// backfillCompactForms generates CompactForm for entries missing it.
// Processes up to 30 entries per cycle to limit LLM calls.
func (mc *MemoryCompressor) backfillCompactForms(ctx context.Context) {
	mc.store.RLock()
	type pending struct {
		id      string
		content string
		cat     memory.Category
	}
	var todo []pending
	for _, e := range mc.store.Entries() {
		if e.CompactForm == "" && len([]rune(e.Content)) > 20 && !e.Category.IsProtected() {
			todo = append(todo, pending{id: e.ID, content: e.Content, cat: e.Category})
		}
	}
	mc.store.RUnlock()

	if len(todo) == 0 {
		return
	}
	if len(todo) > 30 {
		todo = todo[:30]
	}

	updated := 0
	for _, p := range todo {
		select {
		case <-ctx.Done():
			goto done
		default:
		}

		compact, err := mc.compactOneEntry(ctx, p.content, p.cat)
		if err != nil || compact == "" {
			continue
		}
		if len([]rune(compact)) >= len([]rune(p.content)) {
			continue
		}

		mc.store.Lock()
		for i, e := range mc.store.Entries() {
			if e.ID == p.id && e.CompactForm == "" {
				mc.store.Entries()[i].CompactForm = compact
				updated++
				break
			}
		}
		mc.store.Unlock()
	}
done:

	if updated > 0 {
		mc.store.Lock()
		mc.store.MarkDirty()
		mc.store.Unlock()
		mc.store.SignalSave()
		log.Printf("[memory_compact] backfilled %d/%d compact forms", updated, len(todo))
	}
}

// compactOneEntry asks the LLM to produce a minimal representation of a memory
// entry for context injection.
func (mc *MemoryCompressor) compactOneEntry(ctx context.Context, content string, cat memory.Category) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	mc.mu.Lock()
	llmCfg := mc.llmConfig
	mc.mu.Unlock()

	systemPrompt := `You are a memory compactor. Convert the memory entry into the shortest possible representation that preserves ALL key facts. Rules:
- Use telegraphic style: drop articles, filler words, "the user said", etc.
- Use → to show relationships (e.g. "用户→偏好→Go语言")
- Use ; to separate independent facts
- Keep names, numbers, paths, commands EXACTLY as-is
- Target ≤40% of original length
- Return ONLY the compact text, no commentary`

	userPrompt := fmt.Sprintf("[%s] %s", cat, content)

	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": userPrompt},
	}

	result, err := doSimpleLLMRequest(ctx, llmCfg, messages, mc.client, 30*time.Second)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Content), nil
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
	mc.store.Lock()
	defer mc.store.Unlock()

	entries := mc.store.Entries()
	n := len(entries)
	if n < 2 {
		return 0
	}

	// Pre-compute lowercased content to avoid repeated allocations.
	lower := make([]string, n)
	for i, e := range entries {
		lower[i] = strings.TrimSpace(strings.ToLower(e.Content))
	}

	// Mark indices to remove.
	remove := make(map[int]bool)

	for i := 0; i < n; i++ {
		if remove[i] || entries[i].Pinned {
			continue
		}
		for j := i + 1; j < n; j++ {
			if remove[j] || entries[j].Pinned {
				continue
			}
			if !mc.isDuplicateLower(entries[i], entries[j], lower[i], lower[j]) {
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

	kept := make([]memory.Entry, 0, n-len(remove))
	for i, e := range entries {
		if !remove[i] {
			kept = append(kept, e)
		}
	}
	mc.store.SetEntries(kept)
	mc.store.MarkDirty()
	mc.store.SignalSave()
	return len(remove)
}

// minSubstringLen is the minimum rune length for substring-based dedup.
// Entries shorter than this are only deduped by exact match to avoid
// false positives (e.g. "go" matching "go build -o app").
const minSubstringLen = 20

// isDuplicate checks if two entries are duplicates.
func (mc *MemoryCompressor) isDuplicate(a, b memory.Entry) bool {
	ca := strings.TrimSpace(strings.ToLower(a.Content))
	cb := strings.TrimSpace(strings.ToLower(b.Content))
	return mc.isDuplicateLower(a, b, ca, cb)
}

// isDuplicateLower is the inner dedup check using pre-computed lowercase content.
func (mc *MemoryCompressor) isDuplicateLower(a, b memory.Entry, ca, cb string) bool {
	// Multi-tenant isolation: different users' entries are never duplicates.
	// Empty OwnerID (shared) can match with any user.
	if a.OwnerID != "" && b.OwnerID != "" && a.OwnerID != b.OwnerID {
		return false
	}

	// Exact match.
	if ca == cb {
		return true
	}

	// Substring match within the same category — only when both sides are
	// long enough to avoid aggressive false positives.
	// Use canonical category mapping so Claude-style categories dedup against
	// their legacy equivalents (e.g. "project" ↔ "project_knowledge").
	if memory.MapToCanonical(a.Category) == memory.MapToCanonical(b.Category) {
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
	entries := mc.store.Entries()
	ei := entries[i]
	ej := entries[j]

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
// LLM semantic merge
// ---------------------------------------------------------------------------

// mergeBatchSize is the max number of entries sent to LLM in one merge call.
const mergeBatchSize = 25

// mergeSemanticDuplicates groups entries by category, sends each batch to the
// LLM to identify semantically duplicate items, and merges them. Returns the
// total number of entries removed by merging.
func (mc *MemoryCompressor) mergeSemanticDuplicates(ctx context.Context) (int, error) {
	totalMerged := 0

	// Multi-tenant isolation: group by (Category, OwnerID) to ensure
	// different users' entries are never merged.
	type catOwnerKey struct {
		Category memory.Category
		OwnerID  string
	}

	mc.store.RLock()
	groupSet := make(map[catOwnerKey]bool)
	for _, e := range mc.store.Entries() {
		groupSet[catOwnerKey{Category: memory.MapToCanonical(e.Category), OwnerID: e.OwnerID}] = true
	}
	mc.store.RUnlock()

	for key := range groupSet {
		// Never merge protected categories (e.g. self_identity).
		if key.Category.IsProtected() {
			continue
		}
		// Re-snapshot entries for this category+owner each iteration so we see
		// the latest state after previous batches may have mutated the store.
		mc.store.RLock()
		var entries []memory.Entry
		for _, e := range mc.store.Entries() {
			if memory.MapToCanonical(e.Category) == key.Category && e.OwnerID == key.OwnerID && !e.Pinned {
				entries = append(entries, e)
			}
		}
		mc.store.RUnlock()

		if len(entries) < 2 {
			continue
		}
		// Process in batches.
		for start := 0; start < len(entries); start += mergeBatchSize {
			select {
			case <-ctx.Done():
				return totalMerged, ctx.Err()
			default:
			}
			end := start + mergeBatchSize
			if end > len(entries) {
				end = len(entries)
			}
			batch := entries[start:end]
			if len(batch) < 2 {
				continue
			}
			merged, err := mc.mergeBatch(ctx, batch)
			if err != nil {
				continue // skip this batch on error, don't abort
			}
			totalMerged += merged
		}
	}
	return totalMerged, nil
}

// mergeInstruction is the LLM merge response format.
type mergeInstruction struct {
	Keep   int    `json:"keep"`
	Remove []int  `json:"remove"`
	Merged string `json:"merged"`
}

// mergeBatch sends a batch of entries to the LLM and asks it to identify
// groups of semantically equivalent entries. For each group the LLM returns
// a merged (shortest) version; we keep the entry with the highest AccessCount
// and delete the rest.
func (mc *MemoryCompressor) mergeBatch(ctx context.Context, batch []memory.Entry) (int, error) {
	// Check context before making the LLM call.
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	mc.mu.Lock()
	llmCfg := mc.llmConfig
	mc.mu.Unlock()

	// Build numbered list for the prompt.
	var sb strings.Builder
	for i, e := range batch {
		fmt.Fprintf(&sb, "[%d] %s\n", i, truncStr(e.Content, 500))
	}

	systemPrompt := `You are a memory compression assistant. You will receive a numbered list of memory entries from the same category.
Your job is to reduce the total number of entries by merging. There are two merge strategies:

1. **Semantic dedup**: entries that express the same meaning or fact → merge into the shortest version.
2. **Fact consolidation**: multiple short, scattered entries about the same topic/entity → combine into ONE comprehensive entry.

Reply with a JSON array. Each element is an object:
  {"keep": <index of the entry to keep>, "remove": [<indices to remove>], "merged": "<merged text>"}

Rules:
- "merged" must be the shortest text that preserves ALL key facts, decisions, names, numbers, paths, and commands from every entry in the group.
- Use concise bullet points when combining multiple distinct facts into one entry.
- Do NOT group unrelated entries just because they are short.
- If an entry has nothing to merge with, do NOT include it.
- Return ONLY the JSON array, no markdown, no commentary.
- Indices are 0-based, matching the [N] labels.
- If nothing can be merged, return an empty array: []`

	userPrompt := sb.String()

	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": userPrompt},
	}

	resp, err := doSimpleLLMRequest(ctx, llmCfg, messages, mc.client, 60*time.Second)
	if err != nil {
		return 0, err
	}

	// Parse the JSON response.
	body := strings.TrimSpace(resp.Content)
	// Strip markdown code fences if present.
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	var instructions []mergeInstruction
	if err := json.Unmarshal([]byte(body), &instructions); err != nil {
		return 0, fmt.Errorf("parse merge response: %w", err)
	}

	if len(instructions) == 0 {
		return 0, nil
	}

	// Apply merge instructions.
	// First pass: collect all indices claimed by any instruction to detect conflicts.
	claimed := make(map[int]bool)
	var validInstructions []mergeInstruction
	for _, inst := range instructions {
		if inst.Keep < 0 || inst.Keep >= len(batch) || inst.Merged == "" {
			continue
		}
		// Validate remove indices and skip already-claimed ones.
		validRemove := make([]int, 0, len(inst.Remove))
		for _, r := range inst.Remove {
			if r >= 0 && r < len(batch) && r != inst.Keep && !claimed[r] {
				validRemove = append(validRemove, r)
			}
		}
		if len(validRemove) == 0 || claimed[inst.Keep] {
			continue
		}
		inst.Remove = validRemove
		// Mark all indices in this group as claimed.
		claimed[inst.Keep] = true
		for _, r := range validRemove {
			claimed[r] = true
		}
		validInstructions = append(validInstructions, inst)
	}

	removeIDs := make(map[string]bool)
	for _, inst := range validInstructions {
		// Gather all indices in this merge group.
		groupIndices := append([]int{inst.Keep}, inst.Remove...)

		// Pick the entry with highest AccessCount as survivor.
		bestIdx := inst.Keep
		bestAccess := batch[inst.Keep].AccessCount
		for _, idx := range inst.Remove {
			if batch[idx].AccessCount > bestAccess {
				bestAccess = batch[idx].AccessCount
				bestIdx = idx
			}
		}

		// Collect tags from all entries in the group.
		allTags := make([]string, 0)
		for _, idx := range groupIndices {
			allTags = append(allTags, batch[idx].Tags...)
		}

		survivor := batch[bestIdx]
		_ = mc.store.Update(survivor.ID, inst.Merged, survivor.Category, memory.MergeTags(nil, allTags))

		// Mark non-survivors for removal.
		for _, idx := range groupIndices {
			if idx != bestIdx {
				removeIDs[batch[idx].ID] = true
			}
		}
	}

	// Remove merged-away entries.
	removed := 0
	if len(removeIDs) > 0 {
		mc.store.Lock()
		kept := make([]memory.Entry, 0, len(mc.store.Entries()))
		for _, e := range mc.store.Entries() {
			if removeIDs[e.ID] {
				removed++
			} else {
				kept = append(kept, e)
			}
		}
		mc.store.SetEntries(kept)
		mc.store.MarkDirty()
		mc.store.Unlock()
		mc.store.SignalSave()
	}

	return removed, nil
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

// IsCompressing returns whether a compression operation is currently in progress.
func (mc *MemoryCompressor) IsCompressing() bool {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.compressCount > 0
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

func (mc *MemoryCompressor) compressEntry(ctx context.Context, entry memory.Entry) (string, error) {
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

	result, err := doSimpleLLMRequest(ctx, llmCfg, messages, mc.client, 30*time.Second)
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
	mc.store.RLock()
	defer mc.store.RUnlock()
	return len(mc.store.Entries())
}

// ---------------------------------------------------------------------------
// Backup management
// ---------------------------------------------------------------------------

func (mc *MemoryCompressor) backupDir() string {
	return filepath.Join(filepath.Dir(mc.store.Path()), "memory_backups")
}

func (mc *MemoryCompressor) createBackup() (string, error) {
	dir := mc.backupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	if err := mc.store.Flush(); err != nil {
		return "", fmt.Errorf("flush before backup: %w", err)
	}
	mc.store.RLock()
	data, err := json.MarshalIndent(mc.store.Entries(), "", "  ")
	mc.store.RUnlock()
	if err != nil {
		return "", fmt.Errorf("marshal memory snapshot: %w", err)
	}
	name := fmt.Sprintf("memories_backup_%s.json", time.Now().Format("20060102_150405"))
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}

	// Prune old backups beyond the retention limit immediately after creation,
	// so the backup directory never grows beyond limit+1 files regardless of
	// whether the user opens the backup list UI.
	// Error is best-effort — backup was already created successfully.
	mc.pruneOldBackups() //nolint:errcheck

	return name, nil
}

// pruneOldBackups removes the oldest backup files that exceed the retention
// limit and returns the surviving files sorted newest-first by modTime.
// Returns (nil, nil) when the directory does not exist (no backups yet).
// Callers that need the file list (e.g. ListBackups) can consume the return
// value instead of doing a second ReadDir.
func (mc *MemoryCompressor) pruneOldBackups() ([]backupFileInfo, error) {
	dir := mc.backupDir()
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []backupFileInfo
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		files = append(files, backupFileInfo{name: de.Name(), modTime: info.ModTime(), size: info.Size()})
	}
	// Sort newest first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})
	limit := mc.getMaxBackups()
	if len(files) > limit {
		for _, old := range files[limit:] {
			_ = os.Remove(filepath.Join(dir, old.name))
		}
		files = files[:limit]
	}
	return files, nil
}

// backupFileInfo holds the metadata from a single ReadDir + Info call,
// shared between pruneOldBackups and ListBackups to avoid double scanning.
type backupFileInfo struct {
	name    string
	modTime time.Time
	size    int64
}

// defaultMaxBackups is the default number of backup files to keep.
const defaultMaxBackups = 20

func (mc *MemoryCompressor) ListBackups() ([]MemoryBackupInfo, error) {
	// pruneOldBackups scans the directory, prunes excess files, and returns
	// the surviving files sorted newest-first. We reuse that list directly
	// instead of doing a second ReadDir.
	files, err := mc.pruneOldBackups()
	if err != nil {
		return nil, err
	}

	dir := mc.backupDir()
	backups := make([]MemoryBackupInfo, 0, len(files))
	for _, f := range files {
		count := mc.countEntriesInFile(filepath.Join(dir, f.name))
		backups = append(backups, MemoryBackupInfo{
			Name:       f.name,
			CreatedAt:  f.modTime.Format(time.RFC3339),
			SizeBytes:  f.size,
			EntryCount: count,
		})
	}
	return backups, nil
}

// getMaxBackups returns the effective max backups limit.
func (mc *MemoryCompressor) getMaxBackups() int {
	mc.mu.Lock()
	n := mc.maxBackups
	mc.mu.Unlock()
	if n > 0 {
		return n
	}
	return defaultMaxBackups
}

const minBackups = 8

// SetMaxBackups updates the backup retention limit and immediately prunes
// any excess backup files.
func (mc *MemoryCompressor) SetMaxBackups(n int) {
	if n < minBackups {
		n = minBackups
	}
	mc.mu.Lock()
	mc.maxBackups = n
	mc.mu.Unlock()
	mc.pruneOldBackups() //nolint:errcheck
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
	var entries []memory.Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse backup: %w", err)
	}
	if err := os.WriteFile(mc.store.Path(), data, 0o644); err != nil {
		return fmt.Errorf("write memory file: %w", err)
	}
	mc.store.Lock()
	mc.store.SetEntries(entries)
	// File already written to disk — no need to flush again.
	mc.store.Unlock()
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
	var entries []memory.Entry
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
