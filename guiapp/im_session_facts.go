package guiapp

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func (h *IMMessageHandler) loadSessionFacts(userID string) *agent.SessionFactOverlay {
	if h == nil {
		return nil
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	raw, ok := h.sessionFacts.Load(userID)
	if !ok {
		return nil
	}
	overlay, _ := raw.(*agent.SessionFactOverlay)
	return agent.CloneSessionFactOverlay(overlay)
}

func (h *IMMessageHandler) storeSessionFacts(userID string, overlay *agent.SessionFactOverlay) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	if overlay != nil && overlay.Len() > 0 {
		if needles := agent.MemoryRetractionNeedles(h.LoadMemoryRetractions()); len(needles) > 0 {
			agent.DropSessionFactsMatching(overlay, needles)
		}
	}
	if overlay == nil || overlay.Len() == 0 {
		h.sessionFacts.Delete(userID)
		return
	}
	h.sessionFacts.Store(userID, agent.CloneSessionFactOverlay(overlay))
}

func (h *IMMessageHandler) clearSessionFacts(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.sessionFacts.Delete(userID)
}

func (h *IMMessageHandler) sessionFactsFor(userID string, loopCtx *LoopContext) *agent.SessionFactOverlay {
	if loopCtx != nil && loopCtx.SessionFacts != nil && loopCtx.SessionFacts.Len() > 0 {
		return loopCtx.SessionFacts
	}
	return h.loadSessionFacts(userID)
}

func (h *IMMessageHandler) admitSessionFactFromMemorySave(userID, content string) {
	loopCtx := h.runtimeLoopContextForOwner(userID)
	facts := agent.ClaimsFromText(content, "memory save")
	if len(facts) == 0 {
		if fact, ok := agent.ExtractSessionFactFromMemoryContent(content); ok {
			facts = []agent.SessionFact{fact}
		}
	}
	for _, fact := range facts {
		h.admitSessionFact(userID, loopCtx, fact)
		h.syncVerifiedFactKnowledge(userID, fact)
	}
}

func (h *IMMessageHandler) syncVerifiedFactToStores(userID string, fact agent.SessionFact) {
	h.syncVerifiedFactMemory(userID, fact)
	h.syncVerifiedFactKnowledge(userID, fact)
}

func (h *IMMessageHandler) syncVerifiedFactMemory(userID string, fact agent.SessionFact) {
	if h == nil || h.memoryStore == nil || strings.TrimSpace(fact.Claim) == "" {
		return
	}
	ownerID := strings.TrimSpace(userID)
	got := h.memoryStore.ApplyVerifiedFact(memory.VerifiedFact{
		Entity:      fact.Entity,
		Predicate:   fact.Predicate,
		Claim:       fact.Claim,
		Evidence:    fact.Evidence,
		Aliases:     append([]string(nil), fact.Aliases...),
		StrictOwner: isIsolatedAssistantSessionUserID(ownerID),
	}, ownerID)
	if got.Superseded > 0 || got.Saved {
		h.RefreshMemorySnapshot(userID)
	}
}

func (a *App) syncMemoryFromVerifiedKnowledgeText(text string) {
	if a == nil {
		return
	}
	facts := agent.ClaimsFromText(text, "knowledge save")
	if len(facts) == 0 {
		if fact, ok := agent.ExtractSessionFactFromMemoryContent(text); ok {
			facts = []agent.SessionFact{fact}
		}
	}
	if len(facts) == 0 {
		return
	}
	ownerID := projectSessionOwnerID(a.GetCurrentProjectPath())
	for _, fact := range facts {
		if a.imHandler != nil {
			a.imHandler.syncVerifiedFactMemory(ownerID, fact)
			continue
		}
		if a.memoryStore != nil {
			_ = a.memoryStore.ApplyVerifiedFact(memory.VerifiedFact{
				Entity:      fact.Entity,
				Predicate:   fact.Predicate,
				Claim:       fact.Claim,
				Evidence:    fact.Evidence,
				Aliases:     append([]string(nil), fact.Aliases...),
				StrictOwner: isIsolatedAssistantSessionUserID(ownerID),
			}, ownerID)
		}
	}
}

func (h *IMMessageHandler) syncVerifiedFactKnowledge(userID string, fact agent.SessionFact) {
	if h == nil || h.app == nil || strings.TrimSpace(fact.Claim) == "" {
		return
	}
	store := getAutoRecallStoreForApp(h.app, false)
	if store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if _, err := store.ApplyVerifiedFact(ctx, knowledge.VerifiedFactRequest{
		Entity:      fact.Entity,
		Predicate:   fact.Predicate,
		Claim:       fact.Claim,
		Evidence:    fact.Evidence,
		Aliases:     append([]string(nil), fact.Aliases...),
		OwnerID:     strings.TrimSpace(userID),
		StrictOwner: isIsolatedAssistantSessionUserID(userID),
	}); err != nil {
		log.Printf("[session-facts] knowledge verified-fact sync failed: %v", err)
	}
}

func (h *IMMessageHandler) admitSessionFact(userID string, loopCtx *LoopContext, fact agent.SessionFact) {
	if h == nil {
		return
	}
	overlay := agent.EnsureSessionFactOverlay(h.sessionFactsFor(userID, loopCtx))
	if !agent.AdmitSessionFact(overlay, fact) {
		return
	}
	if loopCtx != nil {
		loopCtx.SessionFacts = overlay
	}
	h.storeSessionFacts(userID, overlay)
}

type memoryWarehouseChange struct {
	Needles []string
	Items   []agent.MemoryRetractionItem
}

func (h *IMMessageHandler) applyMemoryWarehouseChange(change memoryWarehouseChange) {
	if h == nil {
		return
	}
	h.admitMemoryRetractions(change.Items)
	h.dropSessionFactsMatching(change.Needles)
	h.redactLiveConversations(change.Needles)
}

func (h *IMMessageHandler) admitMemoryRetractions(items []agent.MemoryRetractionItem) {
	if h == nil || len(items) == 0 {
		return
	}
	h.memoryRetractionMu.Lock()
	defer h.memoryRetractionMu.Unlock()
	for _, item := range items {
		h.memoryRetractions = agent.AdmitMemoryRetraction(h.memoryRetractions, item)
	}
	h.memoryRetractions = agent.CompactMemoryRetraction(h.memoryRetractions)
}

func (h *IMMessageHandler) LoadMemoryRetractions() *agent.MemoryRetraction {
	if h == nil {
		return nil
	}
	h.memoryRetractionMu.Lock()
	defer h.memoryRetractionMu.Unlock()
	h.memoryRetractions = agent.CompactMemoryRetraction(h.memoryRetractions)
	return agent.CloneMemoryRetraction(h.memoryRetractions)
}

func (h *IMMessageHandler) dropSessionFactsMatching(needles []string) {
	if h == nil || len(needles) == 0 {
		return
	}
	h.sessionFacts.Range(func(key, value any) bool {
		overlay, _ := value.(*agent.SessionFactOverlay)
		agent.DropSessionFactsMatching(overlay, needles)
		if overlay == nil || overlay.Len() == 0 {
			h.sessionFacts.Delete(key)
		}
		return true
	})
	h.sessionLoops.Range(func(_, value any) bool {
		state, _ := value.(*sessionLoopState)
		if state == nil {
			return true
		}
		state.stateMu.Lock()
		if state.loopCtx != nil {
			agent.DropSessionFactsMatching(state.loopCtx.SessionFacts, needles)
		}
		state.stateMu.Unlock()
		return true
	})
}

func (h *IMMessageHandler) redactLiveConversations(needles []string) {
	if h == nil || len(needles) == 0 {
		return
	}
	if h.memory != nil {
		h.memory.RewriteAllSessions(func(_ string, entries []agent.ConversationEntry) []agent.ConversationEntry {
			next, changed := agent.RedactConversationEntries(entries, needles)
			if !changed {
				return nil
			}
			return next
		})
	}
	redactLoop := func(ctx *LoopContext) {
		if ctx == nil {
			return
		}
		if next, changed := agent.RedactConversationEntries(ctx.History, needles); changed {
			ctx.History = next
		}
		ctx.Conversation = redactConversationMaps(ctx.Conversation, needles)
	}
	h.sessionLoops.Range(func(_, value any) bool {
		state, _ := value.(*sessionLoopState)
		if state == nil {
			return true
		}
		state.stateMu.Lock()
		redactLoop(state.loopCtx)
		state.stateMu.Unlock()
		return true
	})
	h.inFlightTurns.Range(func(_, value any) bool {
		turn, _ := value.(*inFlightTurn)
		if turn != nil {
			redactLoop(turn.ctx)
		}
		return true
	})
}

func redactConversationMaps(conversation []interface{}, needles []string) []interface{} {
	return agent.RedactConversationMessages(conversation, needles)
}

func (h *IMMessageHandler) appendMemoryRetractionSection(b *strings.Builder) {
	if h == nil || b == nil {
		return
	}
	section := agent.RenderMemoryRetraction(h.LoadMemoryRetractions())
	if section == "" {
		return
	}
	b.WriteByte('\n')
	b.WriteString(section)
	if !strings.HasSuffix(section, "\n") {
		b.WriteByte('\n')
	}
}

func (h *IMMessageHandler) appendSessionFactsSection(b *strings.Builder, userID string, loopCtx *LoopContext) {
	if h == nil || b == nil {
		return
	}
	section := agent.RenderSessionFacts(h.sessionFactsFor(userID, loopCtx))
	if section == "" {
		return
	}
	b.WriteByte('\n')
	b.WriteString(section)
	if !strings.HasSuffix(section, "\n") {
		b.WriteByte('\n')
	}
}
