package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// LLMChatCaller abstracts the LLM chat call needed by the compressor for
// multi-message (system+user) prompts. This is richer than LLMSummarizer
// because the compressor needs system prompts.
type LLMChatCaller interface {
	// ChatCall sends messages to the LLM and returns the assistant reply text.
	ChatCall(messages []map[string]string) (string, error)
	// IsConfigured reports whether the LLM backend is ready.
	IsConfigured() bool
}

// LLMConfigRefresher optionally refreshes the LLM config. Implementations
// that wrap a mutable config source can implement this.
type LLMConfigRefresher interface {
	RefreshConfig()
}

// Compressor compresses long memory entries via LLM and manages backups.
type Compressor struct {
	store         *Store
	llm           LLMChatCaller
	emitter       corelib.EventEmitter
	minContentLen int
	maxBackups    int

	mu         sync.Mutex
	running    bool
	cancelFn   context.CancelFunc
	lastRun    time.Time
	lastResult *CompressResult
	lastError  string
}

// NewCompressor creates a Compressor.
func NewCompressor(store *Store, llm LLMChatCaller, emitter corelib.EventEmitter) *Compressor {
	return &Compressor{
		store:         store,
		llm:           llm,
		emitter:       emitter,
		minContentLen: 200,
	}
}

// Compress performs dedup then LLM compression on long entries.
func (mc *Compressor) Compress(ctx context.Context) (*CompressResult, error) {
	if mc.store == nil {
		return nil, fmt.Errorf("memory store is nil")
	}

	backupName, err := mc.createBackup()
	if err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}

	result := &CompressResult{
		BackupName:   backupName,
		TotalEntries: mc.entryCount(),
	}

	result.DedupCount = mc.dedup()

	if mc.llm != nil && mc.llm.IsConfigured() {
		merged, mergeErr := mc.mergeSemanticDuplicates(ctx)
		if mergeErr == nil {
			result.MergedCount = merged
		}
	}

	if mc.llm != nil && mc.llm.IsConfigured() {
		mc.store.mu.RLock()
		var candidates []Entry
		for _, e := range mc.store.entries {
			if e.Category.IsProtected() {
				continue
			}
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

	result.TotalEntries = mc.entryCount()
	return result, nil
}

// ---------------------------------------------------------------------------
// Dedup logic
// ---------------------------------------------------------------------------

const minSubstringLen = 20

func (mc *Compressor) dedup() int {
	mc.store.mu.Lock()
	defer mc.store.mu.Unlock()

	n := len(mc.store.entries)
	if n < 2 {
		return 0
	}

	lower := make([]string, n)
	for i, e := range mc.store.entries {
		lower[i] = strings.TrimSpace(strings.ToLower(e.Content))
	}

	remove := make(map[int]bool)

	for i := 0; i < n; i++ {
		if remove[i] {
			continue
		}
		for j := i + 1; j < n; j++ {
			if remove[j] {
				continue
			}
			if !isDuplicateLower(mc.store.entries[i], mc.store.entries[j], lower[i], lower[j]) {
				continue
			}
			loser := pickLoser(mc.store.entries, i, j)
			remove[loser] = true
		}
	}

	if len(remove) == 0 {
		return 0
	}

	kept := make([]Entry, 0, n-len(remove))
	for i, e := range mc.store.entries {
		if !remove[i] {
			kept = append(kept, e)
		}
	}
	mc.store.entries = kept
	mc.store.dirty = true
	mc.store.signalSave()
	mc.store.bm25.rebuild(kept)
	return len(remove)
}

func isDuplicateLower(a, b Entry, ca, cb string) bool {
	if ca == cb {
		return true
	}
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

func pickLoser(entries []Entry, i, j int) int {
	ei, ej := entries[i], entries[j]
	li := len([]rune(ei.Content))
	lj := len([]rune(ej.Content))

	if li != lj {
		if li > lj {
			return j
		}
		return i
	}
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

const mergeBatchSize = 25

func (mc *Compressor) mergeSemanticDuplicates(ctx context.Context) (int, error) {
	totalMerged := 0

	mc.store.mu.RLock()
	catSet := make(map[Category]bool)
	for _, e := range mc.store.entries {
		catSet[e.Category] = true
	}
	mc.store.mu.RUnlock()

	for cat := range catSet {
		// Never merge protected categories (e.g. self_identity).
		if cat.IsProtected() {
			continue
		}
		mc.store.mu.RLock()
		var entries []Entry
		for _, e := range mc.store.entries {
			if e.Category == cat {
				entries = append(entries, e)
			}
		}
		mc.store.mu.RUnlock()

		if len(entries) < 2 {
			continue
		}
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
				continue
			}
			totalMerged += merged
		}
	}
	return totalMerged, nil
}

type mergeInstruction struct {
	Keep   int    `json:"keep"`
	Remove []int  `json:"remove"`
	Merged string `json:"merged"`
}

func (mc *Compressor) mergeBatch(ctx context.Context, batch []Entry) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

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

	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": sb.String()},
	}

	resp, err := mc.llm.ChatCall(messages)
	if err != nil {
		return 0, err
	}

	body := strings.TrimSpace(resp)
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

	claimed := make(map[int]bool)
	var validInstructions []mergeInstruction
	for _, inst := range instructions {
		if inst.Keep < 0 || inst.Keep >= len(batch) || inst.Merged == "" {
			continue
		}
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
		claimed[inst.Keep] = true
		for _, r := range validRemove {
			claimed[r] = true
		}
		validInstructions = append(validInstructions, inst)
	}

	removeIDs := make(map[string]bool)
	for _, inst := range validInstructions {
		groupIndices := append([]int{inst.Keep}, inst.Remove...)

		bestIdx := inst.Keep
		bestAccess := batch[inst.Keep].AccessCount
		for _, idx := range inst.Remove {
			if batch[idx].AccessCount > bestAccess {
				bestAccess = batch[idx].AccessCount
				bestIdx = idx
			}
		}

		allTags := make([]string, 0)
		for _, idx := range groupIndices {
			allTags = append(allTags, batch[idx].Tags...)
		}

		survivor := batch[bestIdx]
		_ = mc.store.Update(survivor.ID, inst.Merged, survivor.Category, mergeTags(nil, allTags))

		for _, idx := range groupIndices {
			if idx != bestIdx {
				removeIDs[batch[idx].ID] = true
			}
		}
	}

	removed := 0
	if len(removeIDs) > 0 {
		mc.store.mu.Lock()
		kept := make([]Entry, 0, len(mc.store.entries))
		for _, e := range mc.store.entries {
			if removeIDs[e.ID] {
				removed++
			} else {
				kept = append(kept, e)
			}
		}
		mc.store.entries = kept
		mc.store.dirty = true
		mc.store.mu.Unlock()
		mc.store.signalSave()
		mc.store.bm25.rebuild(kept)
	}

	return removed, nil
}

// ---------------------------------------------------------------------------
// Background service
// ---------------------------------------------------------------------------

// Start begins the background auto-compression service.
func (mc *Compressor) Start() {
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
func (mc *Compressor) Stop() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if !mc.running {
		return
	}
	mc.cancelFn()
	mc.running = false
}

// IsRunning returns whether the background service is active.
func (mc *Compressor) IsRunning() bool {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.running
}

// Status returns the current service status.
func (mc *Compressor) Status() CompressorStatus {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	s := CompressorStatus{Running: mc.running}
	if !mc.lastRun.IsZero() {
		s.LastRun = mc.lastRun.Format(time.RFC3339)
	}
	s.LastResult = mc.lastResult
	s.LastError = mc.lastError
	return s
}

func (mc *Compressor) loop(ctx context.Context) {
	mc.runOnce(ctx)

	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if refresher, ok := mc.llm.(LLMConfigRefresher); ok {
				refresher.RefreshConfig()
			}
			mc.runOnce(ctx)
		}
	}
}

func (mc *Compressor) runOnce(ctx context.Context) {
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

	if mc.emitter != nil {
		mc.emitter.Emit("memory:compressed", result)
	}
}

// ---------------------------------------------------------------------------
// LLM compression helpers
// ---------------------------------------------------------------------------

func (mc *Compressor) compressEntry(ctx context.Context, entry Entry) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	systemPrompt := `You are a memory compression assistant. Your task is to compress the given memory content into a much shorter version while preserving ALL key facts, decisions, and actionable information. Rules:
- Keep the compressed version under 50% of the original length
- Preserve names, numbers, paths, commands, and technical terms exactly
- Remove filler words, redundant explanations, and verbose descriptions
- Use concise bullet points or short sentences
- Do NOT add any commentary — return ONLY the compressed content`

	userPrompt := fmt.Sprintf("Category: %s\nTags: %s\n\nOriginal content to compress:\n%s",
		entry.Category, strings.Join(entry.Tags, ", "), entry.Content)

	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	}

	result, err := mc.llm.ChatCall(messages)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result), nil
}

func (mc *Compressor) entryCount() int {
	mc.store.mu.RLock()
	defer mc.store.mu.RUnlock()
	return len(mc.store.entries)
}

// ---------------------------------------------------------------------------
// Backup management
// ---------------------------------------------------------------------------

func (mc *Compressor) backupDir() string {
	return filepath.Join(filepath.Dir(mc.store.path), "memory_backups")
}

func (mc *Compressor) createBackup() (string, error) {
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

const defaultMaxBackups = 20

// ListBackups returns available backup snapshots.
func (mc *Compressor) ListBackups() ([]BackupInfo, error) {
	dir := mc.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var backups []BackupInfo
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		count := mc.countEntriesInFile(filepath.Join(dir, de.Name()))
		backups = append(backups, BackupInfo{
			Name:       de.Name(),
			CreatedAt:  info.ModTime().Format(time.RFC3339),
			SizeBytes:  info.Size(),
			EntryCount: count,
		})
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt > backups[j].CreatedAt
	})

	limit := mc.getMaxBackups()
	if len(backups) > limit {
		for _, old := range backups[limit:] {
			_ = os.Remove(filepath.Join(dir, old.Name))
		}
		backups = backups[:limit]
	}

	return backups, nil
}

func (mc *Compressor) getMaxBackups() int {
	if mc.maxBackups > 0 {
		return mc.maxBackups
	}
	return defaultMaxBackups
}

// SetMaxBackups updates the backup retention limit.
func (mc *Compressor) SetMaxBackups(n int) {
	if n < 8 {
		n = 8
	}
	mc.maxBackups = n
}

// RestoreBackup restores a backup by name.
func (mc *Compressor) RestoreBackup(backupName string) error {
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
	var restored []Entry
	if err := json.Unmarshal(data, &restored); err != nil {
		return fmt.Errorf("parse backup: %w", err)
	}
	if err := os.WriteFile(mc.store.path, data, 0o644); err != nil {
		return fmt.Errorf("write memory file: %w", err)
	}
	mc.store.mu.Lock()
	mc.store.entries = restored
	mc.store.dirty = false
	mc.store.mu.Unlock()
	mc.store.bm25.rebuild(restored)
	return nil
}

// DeleteBackup removes a backup by name.
func (mc *Compressor) DeleteBackup(backupName string) error {
	if strings.ContainsAny(backupName, `/\`) || backupName != filepath.Base(backupName) {
		return fmt.Errorf("invalid backup name: %s", backupName)
	}
	p := filepath.Join(mc.backupDir(), backupName)
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("backup not found: %s", backupName)
	}
	return os.Remove(p)
}

func (mc *Compressor) countEntriesInFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	var entries []Entry
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
