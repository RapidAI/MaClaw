package httpapi

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

const mobileKnowledgeMaxRunes = 50000

var errMobileOwnerPurged = errors.New("mobile owner has been purged")

// mobileSaveKnowledgeForOwner holds the shared read lock for the complete
// database write. purgeMobileUserKnowledgeData takes the write lock before
// deleting sources, so no in-flight request can recreate user data after its
// purge has completed.
func mobileSaveKnowledgeForOwner(ctx context.Context, tenantID, ownerID string, req knowledge.TextSaveRequest) (knowledge.Source, error) {
	mobileKnowledgePurgeState.RLock()
	defer mobileKnowledgePurgeState.RUnlock()
	if mobileKnowledgeOwnerIsPurgedLocked(tenantID, ownerID) {
		return knowledge.Source{}, errMobileOwnerPurged
	}
	if mobileKnowledgeStore == nil {
		return knowledge.Source{}, errors.New("mobile knowledge store is not initialized")
	}
	return mobileKnowledgeStore.SaveText(ctx, req)
}

func mobileDocumentDraftStillOwned(draft mobileDocumentDraftRecord, tenantID, ownerID string) bool {
	mobileDocuments.Lock()
	current, found := mobileDocuments.drafts[draft.ID]
	mobileDocuments.Unlock()
	return found && current.OwnerID == ownerID &&
		mobileMeetingRecordingTenantMatches(tenantID, current.TenantID)
}

// mobileIngestDocumentDraft indexes a mobile emergency draft into the shared
// knowledge store so the full agent can auto-recall it later.
// Best-effort: failures are logged and never fail the document API.
func mobileIngestDocumentDraft(principal *auth.ViewerPrincipal, draft mobileDocumentDraftRecord) {
	if principal == nil {
		return
	}
	text := strings.TrimSpace(draft.Markdown)
	if text == "" {
		return
	}
	// Ensure knowledge store is initialized with the core agent.
	_, _, err := mobileEnsureCoreAgent()
	if err != nil || mobileKnowledgeStore == nil {
		return
	}
	if utf8.RuneCountInString(text) > mobileKnowledgeMaxRunes {
		runes := []rune(text)
		text = string(runes[:mobileKnowledgeMaxRunes]) + "\n\n…(truncated for knowledge ingest)"
	}
	tenantID := strings.TrimSpace(principal.TenantID)
	if tenantID == "" {
		tenantID = "default"
	}
	ownerID := strings.TrimSpace(principal.UserID)
	if ownerID == "" {
		ownerID = strings.TrimSpace(principal.Email)
	}
	// The document process worker is asynchronous. Revalidate the draft just
	// before indexing so an unbind that removed it cannot leave a knowledge
	// source behind. The write helper then closes the remaining purge/save race.
	if !mobileDocumentDraftStillOwned(draft, tenantID, ownerID) {
		return
	}
	title := strings.TrimSpace(draft.Title)
	if title == "" {
		title = "mobile-document-" + draft.ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err = mobileSaveKnowledgeForOwner(ctx, tenantID, ownerID, knowledge.TextSaveRequest{
		Text:      text,
		Title:     title,
		Kind:      "mobile_document",
		OwnerID:   ownerID,
		TenantID:  tenantID,
		TopicHint: "mobile_document:" + draft.ID,
		Labels:    []string{"mobile", "document", draft.Template},
	})
	if err != nil {
		if errors.Is(err, errMobileOwnerPurged) {
			return
		}
		log.Printf("[mobile-knowledge] ingest draft=%s failed: %v", draft.ID, err)
		return
	}
	log.Printf("[mobile-knowledge] ingested draft=%s owner=%s runes=%d", draft.ID, ownerID, utf8.RuneCountInString(text))
}
