package httpapi

import (
	"context"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

const mobileKnowledgeMaxRunes = 50000

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
	title := strings.TrimSpace(draft.Title)
	if title == "" {
		title = "mobile-document-" + draft.ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err = mobileKnowledgeStore.SaveText(ctx, knowledge.TextSaveRequest{
		Text:      text,
		Title:     title,
		Kind:      "mobile_document",
		OwnerID:   ownerID,
		TenantID:  tenantID,
		TopicHint: "mobile_document:" + draft.ID,
		Labels:    []string{"mobile", "document", draft.Template},
	})
	if err != nil {
		log.Printf("[mobile-knowledge] ingest draft=%s failed: %v", draft.ID, err)
		return
	}
	log.Printf("[mobile-knowledge] ingested draft=%s owner=%s runes=%d", draft.ID, ownerID, utf8.RuneCountInString(text))
}
