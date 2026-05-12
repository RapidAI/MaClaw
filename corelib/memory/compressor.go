package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
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

// maxConsecutiveCompressFailures is the circuit breaker threshold.
// After this many consecutive failures, the background loop stops retrying
// until the next scheduled cycle (inspired by Claude Code's autocompact).
const maxConsecutiveCompressFailures = 3

// Compressor compresses long memory entries via LLM and manages backups.
type Compressor struct {
	store         *Store
	llm           LLMChatCaller
	emitter       corelib.EventEmitter
	minContentLen int
	maxBackups    int
	gcThreshold   int

	mu                    sync.Mutex
	running               bool
	cancelFn              context.CancelFunc
	lastRun               time.Time
	lastResult            *CompressResult
	lastError             string
	experienceProtection  string
	consecutiveFailures   int  // circuit breaker: consecutive compress failures
	circuitBreakerTripped bool // true when failures >= maxConsecutiveCompressFailures
}

// NewCompressor creates a Compressor.
func NewCompressor(store *Store, llm LLMChatCaller, emitter corelib.EventEmitter) *Compressor {
	return &Compressor{
		store:         store,
		llm:           llm,
		emitter:       emitter,
		minContentLen: 200,
		gcThreshold:   450,
	}
}

// SetLLM rewires the LLM used by compression and semantic merge passes.
func (mc *Compressor) SetLLM(llm LLMChatCaller) {
	mc.mu.Lock()
	mc.llm = llm
	mc.mu.Unlock()
}

// SetExperienceProtectionSamples installs a bounded, read-only prompt prelude
// from the memory experience distiller. It does not change compression
// eligibility; it only tells LLM-backed maintenance which concrete evidence is
// unsafe to flatten.
func (mc *Compressor) SetExperienceProtectionSamples(samples []ProtectedExperienceCandidate) {
	mc.mu.Lock()
	mc.experienceProtection = FormatExperienceProtectionPrompt(samples)
	mc.mu.Unlock()
}

func (mc *Compressor) experienceProtectionPrompt() string {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.experienceProtection
}

// SetGCThreshold sets the active entry count threshold that triggers GC.
func (mc *Compressor) SetGCThreshold(n int) {
	mc.gcThreshold = n
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
			if e.Pinned {
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

	// Backfill CompactForm for entries that don't have one yet.
	if mc.llm != nil && mc.llm.IsConfigured() {
		mc.backfillCompactForms(ctx)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// CompactForm backfill
// ---------------------------------------------------------------------------

// backfillCompactForms generates CompactForm for entries missing it.
// Processes up to 30 entries per cycle to limit LLM calls.
func (mc *Compressor) backfillCompactForms(ctx context.Context) {
	mc.store.mu.RLock()
	type pending struct {
		id      string
		content string
		cat     Category
	}
	var todo []pending
	for _, e := range mc.store.entries {
		if e.CompactForm == "" && len([]rune(e.Content)) > 20 && !e.Category.IsProtected() {
			todo = append(todo, pending{id: e.ID, content: e.Content, cat: e.Category})
		}
	}
	mc.store.mu.RUnlock()

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
		// Only use compact form if it's actually shorter.
		if len([]rune(compact)) >= len([]rune(p.content)) {
			continue
		}

		mc.store.mu.Lock()
		for i := range mc.store.entries {
			if mc.store.entries[i].ID == p.id && mc.store.entries[i].CompactForm == "" {
				mc.store.entries[i].CompactForm = compact
				// Refresh BM25 index since entryToDoc now includes CompactForm.
				mc.store.bm25.updateEntry(mc.store.entries[i])
				updated++
				break
			}
		}
		mc.store.mu.Unlock()
	}
done:

	if updated > 0 {
		mc.store.mu.Lock()
		mc.store.dirty = true
		mc.store.mu.Unlock()
		mc.store.signalSave()
		log.Printf("[memory_compact] backfilled %d/%d compact forms", updated, len(todo))
	}
}

// compactOneEntry asks the LLM to produce a minimal representation of a memory
// entry for context injection. The result should be ≤50% of the original length.
func (mc *Compressor) compactOneEntry(ctx context.Context, content string, cat Category) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	systemPrompt := `You are a memory compactor. Compress the memory entry into the shortest possible representation that preserves ALL key facts and remains searchable.

Rules:
- Use concise natural language sentences (NOT telegraphic arrows or symbols)
- Keep entity names adjacent to their attributes (e.g. "api.rapidai.tech: OmniRoute Docker, port 18099, GLM-5.1")
- Keep names, numbers, paths, commands, hostnames EXACTLY as-is
- Use semicolons to separate independent facts within the same topic
- Target ≤40% of original length
- Return ONLY the compact text, no commentary

Good: "api.rapidai.tech 服务器部署 OmniRoute Docker 容器, 使用 GLM-5.1 模型, 端口 18099"
Bad: "服务器→api.rapidai.tech→OmniRoute→Docker; 模型→GLM-5.1; 端口→18099"`

	userPrompt := fmt.Sprintf("[%s] %s", cat, content)

	resp, err := mc.llm.ChatCall([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp), nil
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
		if remove[i] || mc.store.entries[i].Pinned {
			continue
		}
		for j := i + 1; j < n; j++ {
			if remove[j] || mc.store.entries[j].Pinned {
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
	mc.store.rebuildDerivedIndexesLocked(true)
	mc.store.dirty = true
	mc.store.signalSave()
	return len(remove)
}

func isDuplicateLower(a, b Entry, ca, cb string) bool {
	// Different non-empty owners are isolated; shared memories can dedup with any owner.
	if a.OwnerID != "" && b.OwnerID != "" && a.OwnerID != b.OwnerID {
		return false
	}

	if ca == cb {
		return true
	}
	// Use canonical category mapping so Claude-style categories dedup against
	// their legacy equivalents (e.g. "project" -> "project_knowledge",
	// "feedback" -> "instruction", "user" -> "user_fact").
	if MapToCanonical(a.Category) == MapToCanonical(b.Category) {
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

// catOwnerKey is a composite key for grouping entries by (Category, OwnerID).
// This ensures multi-tenant isolation: entries from different users are never merged.
type catOwnerKey struct {
	Category Category
	OwnerID  string
}

func (mc *Compressor) mergeSemanticDuplicates(ctx context.Context) (int, error) {
	totalMerged := 0

	// 多租户隔离：按 (CanonicalCategory, OwnerID) 二元组分组
	// 使用 MapToCanonical 确保 Claude-style 类别与 legacy 类别合并到同一组
	// （如 "project" 和 "project_knowledge" 在同一组中做语义去重）
	mc.store.mu.RLock()
	groupSet := make(map[catOwnerKey]bool)
	for _, e := range mc.store.entries {
		groupSet[catOwnerKey{Category: MapToCanonical(e.Category), OwnerID: e.OwnerID}] = true
	}
	mc.store.mu.RUnlock()

	for key := range groupSet {
		// Never merge protected categories (e.g. self_identity).
		if key.Category.IsProtected() {
			continue
		}
		mc.store.mu.RLock()
		var entries []Entry
		for _, e := range mc.store.entries {
			if MapToCanonical(e.Category) == key.Category && e.OwnerID == key.OwnerID && !e.Pinned {
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
		sb.WriteString(formatExperiencePromptEntry(i, e, 500))
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
	if protection := mc.experienceProtectionPrompt(); protection != "" {
		userPrompt = protection + "\n\nEntries:\n" + userPrompt
	}

	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	}

	resp, err := mc.llm.ChatCall(messages)
	if err != nil {
		return 0, err
	}

	var instructions []mergeInstruction
	if err := extractJSONFromLLMResponse(resp, &instructions); err != nil {
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
		mc.store.rebuildDerivedIndexesLocked(true)
		mc.store.dirty = true
		mc.store.mu.Unlock()
		mc.store.signalSave()
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
	// Delay initial compaction to avoid competing with the user's first
	// message for API bandwidth. The backfill and GC are background
	// maintenance — they can wait until the system is idle.
	select {
	case <-ctx.Done():
		return
	case <-time.After(60 * time.Second):
	}

	mc.maybeRunGC(ctx)
	mc.runOnce(ctx)
	mc.runDreamCycle()

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
			// Reset circuit breaker on each scheduled cycle.
			mc.mu.Lock()
			mc.circuitBreakerTripped = false
			mc.consecutiveFailures = 0
			mc.mu.Unlock()

			mc.maybeRunGC(ctx)
			mc.runOnce(ctx)
			mc.runDreamCycle()
		}
	}
}

// maybeRunGC checks if active entry count >= gcThreshold and runs GC if so.
func (mc *Compressor) maybeRunGC(ctx context.Context) {
	if mc.store.ActiveCount() >= mc.gcThreshold {
		_, _ = mc.RunGC(ctx)
	}
}

// runDreamCycle executes the background self-healing pass.
func (mc *Compressor) runDreamCycle() {
	result := mc.store.DreamCycle()
	if mc.emitter != nil && result != nil {
		mc.emitter.Emit("memory:dream", result)
	}
}

func (mc *Compressor) runOnce(ctx context.Context) {
	// Circuit breaker: skip if tripped.
	mc.mu.Lock()
	if mc.circuitBreakerTripped {
		mc.mu.Unlock()
		log.Printf("[memory_compress] circuit breaker active (%d consecutive failures) — skipping", mc.consecutiveFailures)
		return
	}
	mc.mu.Unlock()

	result, err := mc.Compress(ctx)
	mc.mu.Lock()
	mc.lastRun = time.Now()
	mc.lastResult = result
	if err != nil {
		mc.lastError = err.Error()
		mc.consecutiveFailures++
		if mc.consecutiveFailures >= maxConsecutiveCompressFailures {
			mc.circuitBreakerTripped = true
			log.Printf("[memory_compress] circuit breaker tripped after %d consecutive failures", mc.consecutiveFailures)
		}
	} else {
		mc.lastError = ""
		mc.consecutiveFailures = 0
		mc.circuitBreakerTripped = false
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
	if protection := mc.experienceProtectionPrompt(); protection != "" {
		userPrompt = protection + "\n\n" + userPrompt
	}
	if hint := CompressionProtectionHint(entry); hint != "" {
		userPrompt = fmt.Sprintf("Protection hint: %s\n\n%s", hint, userPrompt)
	}

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

// RunGC performs an intelligent garbage collection cycle:
// 1. Skip pinned and protected entries
// 2. Sort remaining by LRU (lowest AccessCount, oldest UpdatedAt)
// 3. Archive low-priority entries to bring count below threshold
// 4. Scan archive for entries relevant to top-20 recently accessed active entries
// 5. Revive matching archived entries (limit 10 per cycle)
// 6. Emit memory:gc event with GCResult
// ownerID is used for multi-tenant isolation (empty string for single-user mode).
func (mc *Compressor) RunGC(ctx context.Context, ownerID ...string) (*GCResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Extract ownerID from variadic parameter (for backward compatibility).
	filterOwner := ""
	if len(ownerID) > 0 {
		filterOwner = ownerID[0]
	}

	mc.store.mu.Lock()

	result := &GCResult{
		ActiveBefore: len(mc.store.entries),
	}

	// Separate protected/pinned from evictable.
	var protected []Entry
	var evictable []Entry
	for _, e := range mc.store.entries {
		// 多租户隔离：只处理属于该用户的记忆（或共享记忆）
		if filterOwner != "" && e.OwnerID != "" && e.OwnerID != filterOwner {
			protected = append(protected, e) // 其他用户的记忆视为受保护
			continue
		}
		if e.Pinned || e.Category.IsProtected() {
			protected = append(protected, e)
			result.SkippedPinned++
		} else {
			evictable = append(evictable, e)
		}
	}

	// Sort evictable by LRU: lowest AccessCount first, then oldest UpdatedAt.
	sort.SliceStable(evictable, func(i, j int) bool {
		if evictable[i].AccessCount != evictable[j].AccessCount {
			return evictable[i].AccessCount < evictable[j].AccessCount
		}
		return evictable[i].UpdatedAt.Before(evictable[j].UpdatedAt)
	})

	// Determine how many to archive to bring count below threshold.
	target := mc.gcThreshold - len(protected)
	if target < 0 {
		target = 0
	}

	var toArchive []Entry
	var kept []Entry
	if len(evictable) > target {
		excess := len(evictable) - target
		toArchive = evictable[:excess]
		kept = evictable[excess:]
	} else {
		kept = evictable
	}

	// Rebuild active entries.
	newEntries := make([]Entry, 0, len(protected)+len(kept))
	newEntries = append(newEntries, protected...)
	newEntries = append(newEntries, kept...)
	mc.store.entries = newEntries
	mc.store.rebuildDerivedIndexesLocked(false)
	mc.store.dirty = true

	mc.store.mu.Unlock()
	mc.store.signalSave()

	// Archive evicted entries.
	if mc.store.archive != nil && len(toArchive) > 0 {
		_ = mc.store.archive.Add(toArchive...)
	}
	result.ArchivedCount = len(toArchive)

	// Revive relevant archived entries (limit 10).
	if mc.store.archive != nil {
		// Collect tags and categories from top-20 most recently accessed active entries.
		mc.store.mu.RLock()
		active := make([]Entry, len(mc.store.entries))
		copy(active, mc.store.entries)
		mc.store.mu.RUnlock()

		// Sort by AccessCount desc, then UpdatedAt desc to find top-20.
		sort.SliceStable(active, func(i, j int) bool {
			if active[i].AccessCount != active[j].AccessCount {
				return active[i].AccessCount > active[j].AccessCount
			}
			return active[i].UpdatedAt.After(active[j].UpdatedAt)
		})
		topN := 20
		if len(active) < topN {
			topN = len(active)
		}
		top := active[:topN]

		tagSet := make(map[string]bool)
		catSet := make(map[Category]bool)
		for _, e := range top {
			for _, tag := range e.Tags {
				tagSet[tag] = true
			}
			catSet[e.Category] = true
		}

		tags := make([]string, 0, len(tagSet))
		for t := range tagSet {
			tags = append(tags, t)
		}
		cats := make([]Category, 0, len(catSet))
		for c := range catSet {
			cats = append(cats, c)
		}

		relevant := mc.store.archive.FindRelevant(tags, cats, 10, filterOwner)
		var revived []Entry
		for _, re := range relevant {
			if len(revived) >= 10 {
				break
			}
			// Re-check owner isolation before reviving archived entries.
			if filterOwner != "" && re.OwnerID != "" && re.OwnerID != filterOwner {
				continue
			}
			removed, err := mc.store.archive.Remove(re.ID)
			if err != nil {
				continue
			}
			removed.UpdatedAt = time.Now()
			removed.AccessCount = 1
			revived = append(revived, *removed)
		}

		if len(revived) > 0 {
			mc.store.mu.Lock()
			for _, r := range revived {
				mc.store.entries = append(mc.store.entries, r)
			}
			mc.store.rebuildDerivedIndexesLocked(true)
			mc.store.dirty = true
			mc.store.mu.Unlock()
			mc.store.signalSave()
		}
		result.RevivedCount = len(revived)
	}

	mc.store.mu.RLock()
	result.ActiveAfter = len(mc.store.entries)
	mc.store.mu.RUnlock()

	// Emit memory:gc event.
	if mc.emitter != nil {
		mc.emitter.Emit("memory:gc", result)
	}

	log.Printf("[memory_gc] archived=%d revived=%d active: %d->%d skipped_pinned=%d",
		result.ArchivedCount, result.RevivedCount, result.ActiveBefore, result.ActiveAfter, result.SkippedPinned)

	return result, nil
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
	mc.store.mu.RLock()
	data, err := json.MarshalIndent(mc.store.entries, "", "  ")
	mc.store.mu.RUnlock()
	if err != nil {
		return "", fmt.Errorf("marshal memory snapshot: %w", err)
	}
	name := fmt.Sprintf("memories_backup_%s.json", time.Now().Format("20060102_150405"))
	dst := filepath.Join(dir, name)
	if err := fileutil.AtomicWriteFile(dst, data, 0o644); err != nil {
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
func (mc *Compressor) pruneOldBackups() ([]backupFileInfo, error) {
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

const defaultMaxBackups = 20

// ListBackups returns available backup snapshots.
func (mc *Compressor) ListBackups() ([]BackupInfo, error) {
	// pruneOldBackups scans the directory, prunes excess files, and returns
	// the surviving files sorted newest-first. Reuse that list directly.
	files, err := mc.pruneOldBackups()
	if err != nil {
		return nil, err
	}

	dir := mc.backupDir()
	backups := make([]BackupInfo, 0, len(files))
	for _, f := range files {
		count := mc.countEntriesInFile(filepath.Join(dir, f.name))
		backups = append(backups, BackupInfo{
			Name:       f.name,
			CreatedAt:  f.modTime.Format(time.RFC3339),
			SizeBytes:  f.size,
			EntryCount: count,
		})
	}
	return backups, nil
}

func (mc *Compressor) getMaxBackups() int {
	mc.mu.Lock()
	n := mc.maxBackups
	mc.mu.Unlock()
	if n > 0 {
		return n
	}
	return defaultMaxBackups
}

// SetMaxBackups updates the backup retention limit and immediately prunes
// any excess backup files.
func (mc *Compressor) SetMaxBackups(n int) {
	if n < 8 {
		n = 8
	}
	mc.mu.Lock()
	mc.maxBackups = n
	mc.mu.Unlock()
	mc.pruneOldBackups() //nolint:errcheck
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
	if err := fileutil.AtomicWriteFile(mc.store.path, data, 0o644); err != nil {
		return fmt.Errorf("write memory file: %w", err)
	}
	mc.store.mu.Lock()
	mc.store.entries = restored
	mc.store.rebuildDerivedIndexesLocked(false)
	mc.store.dirty = false
	mc.store.mu.Unlock()
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
