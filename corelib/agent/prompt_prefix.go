package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Segment order is frozen: tools, system, memory snapshot, first message.
// History is append-only and is not part of the prefix. StableCacheKey stays
// the short debug hash of the stable system segment; it is not this pin.
const (
	PromptSegmentTools          = "tools"
	PromptSegmentSystem         = "system"
	PromptSegmentMemorySnapshot = "memory_snapshot"
	PromptSegmentFirstMessage   = "first_message"
	PromptSegmentHistory        = "history"

	// DesktopAgentsMDPath is read once when a conversation starts. A missing
	// file is an empty segment, not an error. Later edits do not move the pin.
	DesktopAgentsMDPath = "/home/desktop/AGENTS.md"
)

// PromptPrefixSegments are the UTF-8 bytes of one turn, before the pin
// decides which of them are actually sent.
type PromptPrefixSegments struct {
	Tools          string
	System         string
	MemorySnapshot string
	FirstMessage   string
	History        string
}

// PromptPrefixMeta identifies the conversation the pin belongs to.
// Summarize is the one expected rewrite. The first write is not a defect.
type PromptPrefixMeta struct {
	ConversationID  string
	BotID           string
	Provider        string
	Model           string
	Summarize       bool
	SummarizeReason string
}

// PromptPrefixInput says which segments this call is supplying. A call that
// builds the system prompt does not yet have the tool list, and a later call
// that only has tools must not wipe the system pin.
type PromptPrefixInput struct {
	Segments       PromptPrefixSegments
	ProvideTools   bool
	ProvideSystem  bool
	ProvideMemory  bool
	ProvideFirst   bool
	ProvideHistory bool
}

// PromptPrefixBug is one unexpected change. Kind is prompt_prefix_bug or
// prompt_history_rewrite. The request still sends the pinned prefix bytes.
type PromptPrefixBug struct {
	Kind     string
	Segment  string
	OldHash  string
	NewHash  string
	PromptID string
	BotID    string
}

// PromptPrefixDecision is what this turn sends, plus the hashes of those bytes.
type PromptPrefixDecision struct {
	Send        PromptPrefixSegments
	ToolsHash   string
	SystemHash  string
	MemoryHash  string
	FirstHash   string
	HistoryHash string
	PrefixHash  string
	Bugs        []PromptPrefixBug
	Fresh       bool
	Refreshed   bool
}

// PromptPrefixPin is the conversation record for one prefix. It lives with
// the conversation, not on the machine snapshot. The worker holds it in
// memory; a fresh process treats the next turn as a first write.
type PromptPrefixPin struct {
	mu sync.Mutex

	ConversationID    string
	BotID             string
	ToolSchemaVersion string
	PrefixHash        string
	Order             []string

	Tools          string
	System         string
	MemorySnapshot string
	FirstMessage   string
	History        string

	ToolsPinned  bool
	SystemPinned bool
	MemoryPinned bool
	FirstPinned  bool
	HistorySeen  bool
}

const (
	promptPrefixPinTTL           = 24 * time.Hour
	promptPrefixPinSweepInterval = 5 * time.Minute
)

type promptPrefixPinEntry struct {
	pin        *PromptPrefixPin
	lastAccess time.Time
}

type promptPrefixPinStore struct {
	mu        sync.Mutex
	pins      map[string]*promptPrefixPinEntry
	lastSweep time.Time
}

func (s *promptPrefixPinStore) getOrCreate(key string, meta PromptPrefixMeta, now time.Time) *PromptPrefixPin {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	if entry, ok := s.pins[key]; ok && now.Before(entry.lastAccess.Add(promptPrefixPinTTL)) {
		entry.lastAccess = now
		return entry.pin
	}
	pin := &PromptPrefixPin{
		ConversationID: meta.ConversationID,
		BotID:          meta.BotID,
		Order:          promptPrefixOrder(),
	}
	if s.pins == nil {
		s.pins = make(map[string]*promptPrefixPinEntry)
	}
	s.pins[key] = &promptPrefixPinEntry{pin: pin, lastAccess: now}
	return pin
}

func (s *promptPrefixPinStore) lookup(key string, now time.Time) (*PromptPrefixPin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	entry, ok := s.pins[key]
	if !ok || !now.Before(entry.lastAccess.Add(promptPrefixPinTTL)) {
		delete(s.pins, key)
		return nil, false
	}
	entry.lastAccess = now
	return entry.pin, true
}

func (s *promptPrefixPinStore) drop(key string) {
	s.mu.Lock()
	delete(s.pins, key)
	s.mu.Unlock()
}

func (s *promptPrefixPinStore) reset() {
	s.mu.Lock()
	s.pins = make(map[string]*promptPrefixPinEntry)
	s.lastSweep = time.Time{}
	s.mu.Unlock()
}

func (s *promptPrefixPinStore) sweepLocked(now time.Time) {
	if !s.lastSweep.IsZero() && now.Sub(s.lastSweep) < promptPrefixPinSweepInterval {
		return
	}
	s.lastSweep = now
	for key, entry := range s.pins {
		if !now.Before(entry.lastAccess.Add(promptPrefixPinTTL)) {
			delete(s.pins, key)
		}
	}
}

var (
	promptPrefixPins       promptPrefixPinStore
	promptPrefixUnexpected sync.Map // segment + NUL + provider + NUL + model -> *atomic.Int64
	agentsMDPathValue      atomic.Value
)

func init() {
	agentsMDPathValue.Store("")
}

// PromptSegmentHash is the sha256 of the exact UTF-8 bytes that are sent.
func PromptSegmentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PromptPrefixHash hashes the four prefix segments in frozen order.
func PromptPrefixHash(tools, system, memorySnapshot, firstMessage string) string {
	var b strings.Builder
	b.Grow(len(tools) + len(system) + len(memorySnapshot) + len(firstMessage))
	b.WriteString(tools)
	b.WriteString(system)
	b.WriteString(memorySnapshot)
	b.WriteString(firstMessage)
	return PromptSegmentHash(b.String())
}

// SetAgentsMDPath overrides the AGENTS.md path. The returned function puts
// the previous path back. Tests use it; production passes an empty path to
// ReadAgentsMD and gets DesktopAgentsMDPath.
func SetAgentsMDPath(path string) (restore func()) {
	prev, _ := agentsMDPathValue.Load().(string)
	agentsMDPathValue.Store(path)
	return func() { agentsMDPathValue.Store(prev) }
}

// ReadAgentsMD reads the conversation-start instructions. A missing or
// unreadable file is an empty segment.
func ReadAgentsMD(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path, _ = agentsMDPathValue.Load().(string)
		path = strings.TrimSpace(path)
	}
	if path == "" {
		path = DesktopAgentsMDPath
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(body)
}

// SystemWithAgentsMD appends the pinned instructions to the system segment.
// An empty file leaves the system text unchanged.
func SystemWithAgentsMD(system, agents string) string {
	if agents == "" {
		return system
	}
	if system == "" {
		return agents
	}
	return system + "\n" + agents
}

// CanonicalToolJSON is the tool segment: one JSON array, keys sorted by
// encoding/json, tools ordered by name. The same list always yields the
// same bytes.
func CanonicalToolJSON(tools []map[string]interface{}) string {
	if len(tools) == 0 {
		return "[]"
	}
	sorted := append([]map[string]interface{}(nil), tools...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return canonicalToolName(sorted[i]) < canonicalToolName(sorted[j])
	})
	raw, err := json.Marshal(sorted)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func canonicalToolName(tool map[string]interface{}) string {
	if tool == nil {
		return ""
	}
	if fn, ok := tool["function"].(map[string]interface{}); ok {
		if name, ok := fn["name"].(string); ok {
			return name
		}
	}
	if name, ok := tool["name"].(string); ok {
		return name
	}
	return ""
}

// ToolsFromCanonicalJSON restores the pinned tool list. Callers send this
// instead of a list rebuilt after the conversation started.
func ToolsFromCanonicalJSON(text string) ([]map[string]interface{}, error) {
	text = strings.TrimSpace(text)
	if text == "" || text == "[]" {
		return nil, nil
	}
	var tools []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

// PromptHistoryText splits a conversation into the pinned first line and the
// append-only history after it. The current user line is the newest history
// line, or the first line when the conversation is just starting.
func PromptHistoryText(entries []ConversationEntry, currentUser string) (first, history string) {
	lines := make([]string, 0, len(entries)+1)
	for _, entry := range entries {
		role := strings.TrimSpace(entry.Role)
		if role == "" {
			continue
		}
		lines = append(lines, role+":"+promptContentString(entry.Content))
	}
	if text := strings.TrimSpace(currentUser); text != "" {
		lines = append(lines, "user:"+text)
	}
	if len(lines) == 0 {
		return "", ""
	}
	first = lines[0]
	if len(lines) == 1 {
		return first, ""
	}
	return first, strings.Join(lines[1:], "\n")
}

func promptContentString(content interface{}) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

// PrepareConversationPrefix returns the bytes to send for this conversation.
// The first write and a summary refresh are not defects. Any other change to
// a pinned segment is logged, counted, and replaced with the pinned bytes.
func PrepareConversationPrefix(meta PromptPrefixMeta, in PromptPrefixInput) PromptPrefixDecision {
	meta.ConversationID = strings.TrimSpace(meta.ConversationID)
	meta.BotID = strings.TrimSpace(meta.BotID)
	if meta.ConversationID == "" {
		pin := &PromptPrefixPin{}
		return decidePromptPrefix(pin, meta, in)
	}
	key := meta.BotID + "\x00" + meta.ConversationID
	pin := promptPrefixPins.getOrCreate(key, meta, time.Now())
	return decidePromptPrefix(pin, meta, in)
}

// DropConversationPrefix forgets one conversation's pin. A new conversation
// (/new, a new session) calls this so the next turn may pin again.
func DropConversationPrefix(botID, conversationID string) {
	promptPrefixPins.drop(strings.TrimSpace(botID) + "\x00" + strings.TrimSpace(conversationID))
}

// ResetPromptPrefixPins clears every pin and counter. Tests use it so cases
// do not share a conversation.
func ResetPromptPrefixPins() {
	promptPrefixPins.reset()
	promptPrefixUnexpected.Range(func(key, _ any) bool {
		promptPrefixUnexpected.Delete(key)
		return true
	})
}

// PromptPrefixUnexpectedTotal is maclaw_prompt_prefix_unexpected_change_total
// for one segment, provider, and model.
func PromptPrefixUnexpectedTotal(segment, provider, model string) int64 {
	v, ok := promptPrefixUnexpected.Load(promptPrefixCounterKey(segment, provider, model))
	if !ok {
		return 0
	}
	return v.(*atomic.Int64).Load()
}

func promptPrefixOrder() []string {
	return []string{
		PromptSegmentTools,
		PromptSegmentSystem,
		PromptSegmentMemorySnapshot,
		PromptSegmentFirstMessage,
	}
}

func decidePromptPrefix(pin *PromptPrefixPin, meta PromptPrefixMeta, in PromptPrefixInput) PromptPrefixDecision {
	pin.mu.Lock()
	defer pin.mu.Unlock()
	if pin.ConversationID == "" {
		pin.ConversationID = meta.ConversationID
	}
	if pin.BotID == "" {
		pin.BotID = meta.BotID
	}
	if len(pin.Order) == 0 {
		pin.Order = promptPrefixOrder()
	}

	var decision PromptPrefixDecision
	if meta.Summarize && prefixHasPin(pin) {
		oldPrefix := pin.PrefixHash
		applySummaryRefresh(pin, in)
		decision.Refreshed = true
		decision.Send = pinnedSegments(pin)
		fillPrefixHashes(&decision)
		reason := "summarize"
		if r := strings.TrimSpace(meta.SummarizeReason); r != "" {
			reason = r
		}
		log.Printf("prompt_prefix_refresh reason=%s conversation_id=%s bot_id=%s old_prefix=%s prefix=%s",
			reason, pin.ConversationID, pin.BotID, oldPrefix, decision.PrefixHash)
		logPromptSegment(decision)
		return decision
	}

	if !prefixHasPin(pin) {
		applyFirstWrite(pin, in)
		decision.Fresh = true
		decision.Send = pinnedSegments(pin)
		fillPrefixHashes(&decision)
		logPromptSegment(decision)
		return decision
	}

	decision.Send = pinnedSegments(pin)
	if in.ProvideTools {
		decision.Bugs = append(decision.Bugs, compareSegment(pin, meta, PromptSegmentTools, &pin.Tools, &pin.ToolsPinned, in.Segments.Tools)...)
	}
	if in.ProvideSystem {
		decision.Bugs = append(decision.Bugs, compareSegment(pin, meta, PromptSegmentSystem, &pin.System, &pin.SystemPinned, in.Segments.System)...)
	}
	if in.ProvideMemory {
		decision.Bugs = append(decision.Bugs, compareSegment(pin, meta, PromptSegmentMemorySnapshot, &pin.MemorySnapshot, &pin.MemoryPinned, in.Segments.MemorySnapshot)...)
	}
	if in.ProvideFirst {
		decision.Bugs = append(decision.Bugs, compareSegment(pin, meta, PromptSegmentFirstMessage, &pin.FirstMessage, &pin.FirstPinned, in.Segments.FirstMessage)...)
	}
	if in.ProvideHistory {
		if bug, ok := compareHistory(pin, meta, in.Segments.History); ok {
			decision.Bugs = append(decision.Bugs, bug)
		}
	}
	decision.Send = pinnedSegments(pin)
	if in.ProvideTools {
		pin.ToolSchemaVersion = PromptSegmentHash(pin.Tools)
	}
	fillPrefixHashes(&decision)
	logPromptSegment(decision)
	return decision
}

func prefixHasPin(pin *PromptPrefixPin) bool {
	return pin.ToolsPinned || pin.SystemPinned || pin.MemoryPinned || pin.FirstPinned || pin.HistorySeen
}

func applyFirstWrite(pin *PromptPrefixPin, in PromptPrefixInput) {
	if in.ProvideTools {
		pin.Tools = in.Segments.Tools
		pin.ToolsPinned = true
		pin.ToolSchemaVersion = PromptSegmentHash(pin.Tools)
	}
	if in.ProvideSystem {
		pin.System = in.Segments.System
		pin.SystemPinned = true
	}
	if in.ProvideMemory {
		pin.MemorySnapshot = in.Segments.MemorySnapshot
		pin.MemoryPinned = true
	}
	if in.ProvideFirst && in.Segments.FirstMessage != "" {
		pin.FirstMessage = in.Segments.FirstMessage
		pin.FirstPinned = true
	}
	if in.ProvideHistory {
		pin.History = in.Segments.History
		pin.HistorySeen = true
	}
	pin.PrefixHash = PromptPrefixHash(pin.Tools, pin.System, pin.MemorySnapshot, pin.FirstMessage)
}

func applySummaryRefresh(pin *PromptPrefixPin, in PromptPrefixInput) {
	// A summary commit is allowed to replace the prefix. The next turn pins
	// these bytes. This is not prompt_prefix_bug.
	if in.ProvideTools {
		pin.Tools = in.Segments.Tools
		pin.ToolsPinned = true
		pin.ToolSchemaVersion = PromptSegmentHash(pin.Tools)
	}
	if in.ProvideSystem {
		pin.System = in.Segments.System
		pin.SystemPinned = true
	}
	if in.ProvideMemory {
		pin.MemorySnapshot = in.Segments.MemorySnapshot
		pin.MemoryPinned = true
	}
	if in.ProvideFirst {
		pin.FirstMessage = in.Segments.FirstMessage
		pin.FirstPinned = in.Segments.FirstMessage != ""
	}
	if in.ProvideHistory {
		pin.History = in.Segments.History
		pin.HistorySeen = true
	}
	pin.PrefixHash = PromptPrefixHash(pin.Tools, pin.System, pin.MemorySnapshot, pin.FirstMessage)
}

func compareSegment(pin *PromptPrefixPin, meta PromptPrefixMeta, segment string, pinned *string, flagged *bool, rebuilt string) []PromptPrefixBug {
	if !*flagged {
		// The first time this segment appears (tools after the system prompt,
		// or the person's first sentence after a three-segment prewarm) is a
		// first write, not a defect.
		if segment == PromptSegmentFirstMessage && rebuilt == "" {
			return nil
		}
		*pinned = rebuilt
		*flagged = true
		return nil
	}
	if rebuilt == *pinned {
		return nil
	}
	bug := PromptPrefixBug{
		Kind:     "prompt_prefix_bug",
		Segment:  segment,
		OldHash:  PromptSegmentHash(*pinned),
		NewHash:  PromptSegmentHash(rebuilt),
		PromptID: pin.ConversationID,
		BotID:    pin.BotID,
	}
	notePromptPrefixUnexpected(segment, meta.Provider, meta.Model)
	log.Printf("prompt_prefix_bug segment=%s old=%s new=%s conversation_id=%s bot_id=%s",
		bug.Segment, bug.OldHash, bug.NewHash, bug.PromptID, bug.BotID)
	return []PromptPrefixBug{bug}
}

func compareHistory(pin *PromptPrefixPin, meta PromptPrefixMeta, rebuilt string) (PromptPrefixBug, bool) {
	if !pin.HistorySeen {
		pin.History = rebuilt
		pin.HistorySeen = true
		return PromptPrefixBug{}, false
	}
	if strings.HasPrefix(rebuilt, pin.History) {
		pin.History = rebuilt
		return PromptPrefixBug{}, false
	}
	bug := PromptPrefixBug{
		Kind:     "prompt_history_rewrite",
		Segment:  PromptSegmentHistory,
		OldHash:  PromptSegmentHash(pin.History),
		NewHash:  PromptSegmentHash(rebuilt),
		PromptID: pin.ConversationID,
		BotID:    pin.BotID,
	}
	notePromptPrefixUnexpected(PromptSegmentHistory, meta.Provider, meta.Model)
	log.Printf("prompt_history_rewrite conversation_id=%s bot_id=%s old=%s new=%s",
		bug.PromptID, bug.BotID, bug.OldHash, bug.NewHash)
	// The rewritten history is what this turn is sending. Later turns append
	// to that, so the baseline moves after the one alert.
	pin.History = rebuilt
	return bug, true
}

func pinnedSegments(pin *PromptPrefixPin) PromptPrefixSegments {
	return PromptPrefixSegments{
		Tools:          pin.Tools,
		System:         pin.System,
		MemorySnapshot: pin.MemorySnapshot,
		FirstMessage:   pin.FirstMessage,
		History:        pin.History,
	}
}

func fillPrefixHashes(d *PromptPrefixDecision) {
	d.ToolsHash = PromptSegmentHash(d.Send.Tools)
	d.SystemHash = PromptSegmentHash(d.Send.System)
	d.MemoryHash = PromptSegmentHash(d.Send.MemorySnapshot)
	d.FirstHash = PromptSegmentHash(d.Send.FirstMessage)
	d.HistoryHash = PromptSegmentHash(d.Send.History)
	d.PrefixHash = PromptPrefixHash(d.Send.Tools, d.Send.System, d.Send.MemorySnapshot, d.Send.FirstMessage)
}

func logPromptSegment(d PromptPrefixDecision) {
	log.Printf("prompt_segment tools=%s system=%s memory_snapshot=%s first_message=%s prefix=%s",
		d.ToolsHash, d.SystemHash, d.MemoryHash, d.FirstHash, d.PrefixHash)
}

func notePromptPrefixUnexpected(segment, provider, model string) {
	key := promptPrefixCounterKey(segment, provider, model)
	actual, _ := promptPrefixUnexpected.LoadOrStore(key, &atomic.Int64{})
	actual.(*atomic.Int64).Add(1)
}

func promptPrefixCounterKey(segment, provider, model string) string {
	return segment + "\x00" + provider + "\x00" + model
}

// PinnedPrefix is the bytes this conversation will send. The idle summary
// replays them. A missing pin returns false.
func PinnedPrefix(botID, conversationID string) (PromptPrefixSegments, bool) {
	key := strings.TrimSpace(botID) + "\x00" + strings.TrimSpace(conversationID)
	pin, ok := promptPrefixPins.lookup(key, time.Now())
	if !ok {
		return PromptPrefixSegments{}, false
	}
	pin.mu.Lock()
	defer pin.mu.Unlock()
	if !prefixHasPin(pin) {
		return PromptPrefixSegments{}, false
	}
	return pinnedSegments(pin), true
}
