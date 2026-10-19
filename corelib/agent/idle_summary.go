package agent

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// The idle summary runs on the server after a large turn. The window is
// configuration clamped to five minutes through one hour. An unconfigured
// provider uses the floor. This file does not record a vendor's published
// cache lifetime, and it does not touch a machine.
const (
	PromptSummaryMinTokens = 100000
	PromptSummaryIdle      = 2 * time.Minute
	PromptSummaryMargin    = 45 * time.Second
	PromptCacheTTLFloor    = 5 * time.Minute
	PromptCacheTTLCeil     = time.Hour

	// PrefixPrewarmTail is the fixed tail of an after-summary warm.
	// It is not stored and it is not a system prompt.
	PrefixPrewarmTail      = "预热"
	PrefixPrewarmMaxTokens = 1
)

const (
	idleDue         = "due"
	idleSummarizing = "summarizing"
	idleCommitted   = "committed"
	idleAborted     = "aborted"
	idleSkipped     = "skipped"
)

// IdleTurnNote is one finished turn. The scheduler does not sleep on it.
type IdleTurnNote struct {
	ConversationID  string
	BotID           string
	Provider        string
	Model           string
	PromptTokens    int
	TokensEstimated bool
	TurnEnded       bool
	PendingAsk      bool
	InFlight        bool
	At              time.Time
	// CacheTTL, Idle, and Margin override the configured window for this
	// note. Zero uses the provider configuration and the defaults above.
	CacheTTL time.Duration
	Idle     time.Duration
	Margin   time.Duration
}

// IdleSummaryRequest is the summary call. Its four prefix segments are the
// pinned bytes. Instruction is only the last line.
type IdleSummaryRequest struct {
	ConversationID string
	BotID          string
	Tools          string
	System         string
	MemorySnapshot string
	FirstMessage   string
	History        string
	Instruction    string
	ToolsHash      string
	SystemHash     string
	MemoryHash     string
	FirstHash      string
	HistoryHash    string
	PrefixHash     string
}

// PrefixPrewarm is the warm after a committed summary.
type PrefixPrewarm struct {
	ConversationID string
	BotID          string
	Reason         string
	ToolsHash      string
	SystemHash     string
	MemoryHash     string
	FirstHash      string
	PrefixHash     string
	MaxTokens      int
	Tail           string
	SendToUser     bool
}

// IdleSummaryStatus is the scheduler's decision. The machine flags stay false.
type IdleSummaryStatus struct {
	State       string
	Reason      string
	Built       bool
	Request     *IdleSummaryRequest
	Woke        bool
	Ensured     bool
	LastUsed    bool
	MachineHeld bool
	Prewarms    []PrefixPrewarm
}

// IdleSummaryCommit is applied only after the history hash still matches.
type IdleSummaryCommit struct {
	Summary     string
	NextTools   string
	NextMemory  string
	KeptHistory string
}

type idleTask struct {
	mu sync.Mutex

	botID          string
	conversationID string
	provider       string
	model          string
	state          string
	reason         string
	requestedAt    time.Time
	runAt          time.Time
	deadline       time.Time
	hold           bool
	returned       bool
	built          bool
	gen            uint64

	tools, system, memory, first, history string
	instruction                           string
	toolsHash, systemHash, memoryHash     string
	firstHash, historyHash, prefixHash    string

	prewarms []PrefixPrewarm
}

var (
	idleTasks      sync.Map // bot + NUL + conversation -> *idleTask
	promptCacheTTL sync.Map // provider -> time.Duration
	idleRefresh    sync.Map // bot + NUL + conversation -> func() string
)

// ClampPromptCacheTTL clamps a configured window to [5m, 1h].
// A zero or shorter value becomes 5 minutes.
func ClampPromptCacheTTL(d time.Duration) time.Duration {
	if d < PromptCacheTTLFloor {
		return PromptCacheTTLFloor
	}
	if d > PromptCacheTTLCeil {
		return PromptCacheTTLCeil
	}
	return d
}

// PromptCacheTTL is the clamped window for a provider.
// An unconfigured provider uses 5 minutes.
func PromptCacheTTL(provider string) time.Duration {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if v, ok := promptCacheTTL.Load(provider); ok {
		if d, ok := v.(time.Duration); ok {
			return ClampPromptCacheTTL(d)
		}
	}
	return ClampPromptCacheTTL(0)
}

// SetPromptCacheTTL sets one provider's window. The returned function
// restores the previous value. The stored value is clamped on read.
func SetPromptCacheTTL(provider string, d time.Duration) (restore func()) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	prev, had := promptCacheTTL.Load(provider)
	promptCacheTTL.Store(provider, d)
	return func() {
		if had {
			promptCacheTTL.Store(provider, prev)
			return
		}
		promptCacheTTL.Delete(provider)
	}
}

// SetIdleSummaryMemoryRefresh installs the excerpt refresh for one
// conversation. Commit calls it only after the history hash matches.
func SetIdleSummaryMemoryRefresh(botID, conversationID string, fn func() string) {
	key := idleKey(botID, conversationID)
	if fn == nil {
		idleRefresh.Delete(key)
		return
	}
	idleRefresh.Store(key, fn)
}

// ResetIdleSummaries clears tasks, windows, and refresh callbacks.
func ResetIdleSummaries() {
	idleTasks.Range(func(key, value any) bool {
		if t, ok := value.(*idleTask); ok {
			t.mu.Lock()
			t.gen++
			t.state = ""
			t.mu.Unlock()
		}
		idleTasks.Delete(key)
		return true
	})
	promptCacheTTL.Range(func(key, _ any) bool {
		promptCacheTTL.Delete(key)
		return true
	})
	idleRefresh.Range(func(key, _ any) bool {
		idleRefresh.Delete(key)
		return true
	})
}

// NoteUserReturned cancels a summary that has not been sent.
// A request already sent stays up; commit drops it when the history hash moved.
func NoteUserReturned(botID, conversationID string, at time.Time) {
	_ = at
	t := existingIdleTask(botID, conversationID)
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch t.state {
	case idleDue:
		t.returned = true
		t.state = idleAborted
		t.reason = "user_returned"
		t.built = false
		t.gen++
		log.Printf("prompt_summary_aborted reason=user_returned conversation_id=%s bot_id=%s", t.conversationID, t.botID)
	case idleSummarizing:
		t.returned = true
	}
}

// NoteIdleTurn records a finished turn. Over 100000 tokens, with a pin,
// it marks the conversation due. It does not send and it does not sleep.
func NoteIdleTurn(note IdleTurnNote) {
	note.ConversationID = strings.TrimSpace(note.ConversationID)
	note.BotID = strings.TrimSpace(note.BotID)
	if note.ConversationID == "" || note.BotID == "" {
		return
	}
	if !note.TurnEnded || note.InFlight || note.PendingAsk {
		if t := existingIdleTask(note.BotID, note.ConversationID); t != nil {
			t.mu.Lock()
			if t.state == idleDue {
				t.hold = true
			}
			t.mu.Unlock()
		}
		return
	}
	if note.PromptTokens <= PromptSummaryMinTokens {
		return
	}
	segs, ok := PinnedPrefix(note.BotID, note.ConversationID)
	if !ok {
		return
	}
	at := note.At
	if at.IsZero() {
		at = time.Now()
	}
	ttl := PromptCacheTTL(note.Provider)
	if note.CacheTTL > 0 {
		ttl = ClampPromptCacheTTL(note.CacheTTL)
	}
	idle := note.Idle
	if idle <= 0 {
		idle = PromptSummaryIdle
	}
	margin := note.Margin
	if margin <= 0 {
		margin = PromptSummaryMargin
	}
	deadline := at.Add(ttl - margin)
	notBefore := at.Add(idle)
	runAt := notBefore
	if notBefore.After(deadline) {
		runAt = deadline
	}

	t := loadIdleTask(note.BotID, note.ConversationID)
	t.mu.Lock()
	if t.state == idleSummarizing {
		same := t.historyHash != "" && PromptSegmentHash(segs.History) == t.historyHash && !t.returned
		if same {
			t.mu.Unlock()
			return
		}
		t.state = idleAborted
		t.reason = "user_returned"
		t.built = false
		t.gen++
	}
	t.provider = note.Provider
	t.model = note.Model
	t.state = idleDue
	t.reason = ""
	t.requestedAt = at
	t.runAt = runAt
	t.deadline = deadline
	t.hold = false
	t.returned = false
	t.built = false
	t.tools, t.system, t.memory, t.first, t.history = "", "", "", "", ""
	t.instruction = ""
	t.toolsHash, t.systemHash, t.memoryHash = "", "", ""
	t.firstHash, t.historyHash, t.prefixHash = "", "", ""
	t.gen++
	gen := t.gen
	t.mu.Unlock()
	if note.TokensEstimated {
		log.Printf("prompt_summary_due tokens=estimated conversation_id=%s bot_id=%s", note.ConversationID, note.BotID)
	}
	t.arm(gen, runAt)
}

// TickIdleSummary decides whether this moment may send.
// A cold window skips. A person who already returned cancels.
// The built request copies the pinned prefix.
func TickIdleSummary(botID, conversationID string, now time.Time) IdleSummaryStatus {
	t := existingIdleTask(botID, conversationID)
	if t == nil {
		return IdleSummaryStatus{Reason: "not_due"}
	}
	t.mu.Lock()
	if t.state != idleDue {
		st := t.snapshot()
		t.mu.Unlock()
		return st
	}
	if t.hold {
		t.reason = "pending_ask"
		st := t.snapshot()
		t.mu.Unlock()
		return st
	}
	if now.IsZero() {
		now = time.Now()
	}
	if now.Before(t.runAt) {
		t.reason = "waiting"
		st := t.snapshot()
		t.mu.Unlock()
		return st
	}
	if now.After(t.deadline) || t.returned {
		if t.returned {
			t.state = idleAborted
			t.reason = "user_returned"
			log.Printf("prompt_summary_aborted reason=user_returned conversation_id=%s bot_id=%s", t.conversationID, t.botID)
		} else {
			t.state = idleSkipped
			t.reason = "cache_expired"
			log.Printf("prompt_summary_skipped reason=cache_expired conversation_id=%s bot_id=%s", t.conversationID, t.botID)
		}
		t.built = false
		t.gen++
		st := t.snapshot()
		t.mu.Unlock()
		return st
	}
	bot, conv := t.botID, t.conversationID
	t.mu.Unlock()

	segs, pinned := PinnedPrefix(bot, conv)

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state != idleDue {
		return t.snapshot()
	}
	if t.returned || !pinned {
		t.state = idleAborted
		t.reason = "user_returned"
		t.built = false
		t.gen++
		log.Printf("prompt_summary_aborted reason=user_returned conversation_id=%s bot_id=%s", t.conversationID, t.botID)
		return t.snapshot()
	}
	req := idleRequest(bot, conv, segs, now)
	t.tools = req.Tools
	t.system = req.System
	t.memory = req.MemorySnapshot
	t.first = req.FirstMessage
	t.history = req.History
	t.instruction = req.Instruction
	t.toolsHash = req.ToolsHash
	t.systemHash = req.SystemHash
	t.memoryHash = req.MemoryHash
	t.firstHash = req.FirstHash
	t.historyHash = req.HistoryHash
	t.prefixHash = req.PrefixHash
	t.built = true
	t.state = idleSummarizing
	t.reason = ""
	return t.snapshot()
}

// CommitIdleSummary installs the summary only when the history and the
// four prefix hashes still match the request. Otherwise it drops the result.
func CommitIdleSummary(botID, conversationID string, in IdleSummaryCommit) IdleSummaryStatus {
	t := existingIdleTask(botID, conversationID)
	if t == nil {
		return IdleSummaryStatus{Reason: "not_due"}
	}
	t.mu.Lock()
	if t.state != idleSummarizing || !t.built {
		st := t.snapshot()
		t.mu.Unlock()
		return st
	}
	wantHist := t.historyHash
	wantPrefix := t.prefixHash
	provider, model := t.provider, t.model
	bot, conv := t.botID, t.conversationID
	t.mu.Unlock()

	segs, ok := PinnedPrefix(bot, conv)
	if !ok || PromptSegmentHash(segs.History) != wantHist || PromptPrefixHash(segs.Tools, segs.System, segs.MemorySnapshot, segs.FirstMessage) != wantPrefix {
		t.mu.Lock()
		t.state = idleAborted
		t.reason = "history_mismatch"
		t.gen++
		st := t.snapshot()
		t.mu.Unlock()
		log.Printf("prompt_summary_aborted reason=history_mismatch conversation_id=%s bot_id=%s", conv, bot)
		return st
	}

	mem := in.NextMemory
	if fn := idleRefreshFn(bot, conv); fn != nil {
		if got := fn(); got != "" {
			mem = got
		}
	}
	decision := PrepareConversationPrefix(PromptPrefixMeta{
		ConversationID:  conv,
		BotID:           bot,
		Provider:        provider,
		Model:           model,
		Summarize:       true,
		SummarizeReason: "summarize_idle",
	}, PromptPrefixInput{
		Segments: PromptPrefixSegments{
			Tools:          in.NextTools,
			MemorySnapshot: mem,
			FirstMessage:   in.Summary,
			History:        in.KeptHistory,
		},
		ProvideTools:   in.NextTools != "",
		ProvideMemory:  mem != "",
		ProvideFirst:   true,
		ProvideHistory: true,
	})
	warm := PrefixPrewarm{
		ConversationID: conv,
		BotID:          bot,
		Reason:         "after_summary",
		ToolsHash:      decision.ToolsHash,
		SystemHash:     decision.SystemHash,
		MemoryHash:     decision.MemoryHash,
		FirstHash:      decision.FirstHash,
		PrefixHash:     decision.PrefixHash,
		MaxTokens:      PrefixPrewarmMaxTokens,
		Tail:           PrefixPrewarmTail,
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state != idleSummarizing {
		return t.snapshot()
	}
	t.state = idleCommitted
	t.reason = ""
	t.prewarms = appendLimitedPrewarm(t.prewarms, warm)
	return t.snapshot()
}

// EstimatePromptTokens estimates a turn from its history when the provider
// did not report a prompt size.
func EstimatePromptTokens(entries []ConversationEntry, currentUser string) int {
	msgs := make([]interface{}, 0, len(entries)+1)
	for _, entry := range entries {
		msgs = append(msgs, map[string]interface{}{
			"role":    entry.Role,
			"content": promptContentString(entry.Content),
		})
	}
	if text := strings.TrimSpace(currentUser); text != "" {
		msgs = append(msgs, map[string]interface{}{
			"role":    "user",
			"content": text,
		})
	}
	return EstimateConversationTokens(msgs)
}

func idleRequest(bot, conv string, segs PromptPrefixSegments, now time.Time) IdleSummaryRequest {
	return IdleSummaryRequest{
		ConversationID: conv,
		BotID:          bot,
		Tools:          segs.Tools,
		System:         segs.System,
		MemorySnapshot: segs.MemorySnapshot,
		FirstMessage:   segs.FirstMessage,
		History:        segs.History,
		Instruction:    idleSummaryInstruction(now),
		ToolsHash:      PromptSegmentHash(segs.Tools),
		SystemHash:     PromptSegmentHash(segs.System),
		MemoryHash:     PromptSegmentHash(segs.MemorySnapshot),
		FirstHash:      PromptSegmentHash(segs.FirstMessage),
		HistoryHash:    PromptSegmentHash(segs.History),
		PrefixHash:     PromptPrefixHash(segs.Tools, segs.System, segs.MemorySnapshot, segs.FirstMessage),
	}
}

func idleSummaryInstruction(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return fmt.Sprintf("时间: %s\n变化: 无\n任务: 把到目前为止的对话写成摘要。保留决定、路径、未做完的事、还在等这个人的问题。不调用工具。",
		now.UTC().Format("2006-01-02 15:04"))
}

func appendLimitedPrewarm(have []PrefixPrewarm, warm PrefixPrewarm) []PrefixPrewarm {
	if n := len(have); n > 0 {
		last := have[n-1]
		if last.Reason == warm.Reason && last.PrefixHash == warm.PrefixHash && last.ConversationID == warm.ConversationID {
			return have
		}
	}
	return append(have, warm)
}

func (t *idleTask) arm(gen uint64, runAt time.Time) {
	if !idleSummaryArmed() {
		return
	}
	delay := time.Until(runAt)
	time.AfterFunc(delay, func() {
		t.mu.Lock()
		if t.gen != gen || t.state != idleDue {
			t.mu.Unlock()
			return
		}
		bot, conv := t.botID, t.conversationID
		t.mu.Unlock()
		TickIdleSummary(bot, conv, time.Now())
	})
}

func idleSummaryArmed() bool {
	return flag.Lookup("test.v") == nil
}

func (t *idleTask) snapshot() IdleSummaryStatus {
	st := IdleSummaryStatus{
		State:    t.state,
		Reason:   t.reason,
		Built:    t.built,
		Prewarms: append([]PrefixPrewarm(nil), t.prewarms...),
	}
	if !t.built {
		return st
	}
	req := IdleSummaryRequest{
		ConversationID: t.conversationID,
		BotID:          t.botID,
		Tools:          t.tools,
		System:         t.system,
		MemorySnapshot: t.memory,
		FirstMessage:   t.first,
		History:        t.history,
		Instruction:    t.instruction,
		ToolsHash:      t.toolsHash,
		SystemHash:     t.systemHash,
		MemoryHash:     t.memoryHash,
		FirstHash:      t.firstHash,
		HistoryHash:    t.historyHash,
		PrefixHash:     t.prefixHash,
	}
	st.Request = &req
	return st
}

func idleKey(botID, conversationID string) string {
	return strings.TrimSpace(botID) + "\x00" + strings.TrimSpace(conversationID)
}

func existingIdleTask(botID, conversationID string) *idleTask {
	v, ok := idleTasks.Load(idleKey(botID, conversationID))
	if !ok {
		return nil
	}
	t, _ := v.(*idleTask)
	return t
}

func loadIdleTask(botID, conversationID string) *idleTask {
	key := idleKey(botID, conversationID)
	if v, ok := idleTasks.Load(key); ok {
		if t, ok := v.(*idleTask); ok {
			return t
		}
	}
	t := &idleTask{botID: strings.TrimSpace(botID), conversationID: strings.TrimSpace(conversationID)}
	actual, _ := idleTasks.LoadOrStore(key, t)
	return actual.(*idleTask)
}

func idleRefreshFn(botID, conversationID string) func() string {
	v, ok := idleRefresh.Load(idleKey(botID, conversationID))
	if !ok {
		return nil
	}
	fn, _ := v.(func() string)
	return fn
}
