package knowledge

import (
	"log"
	"strings"
)

// logKnowledgeImageEvent is the deliberately small diagnostic surface for
// knowledge-image ingestion. Image parsing errors and source names can carry
// arbitrary local paths or document-derived text, so neither is safe for
// routine logs/telemetry. Keep only a fixed event, a validated source kind,
// and a count useful for operational diagnosis.
func logKnowledgeImageEvent(event, kind string, count int) {
	event = strings.TrimSpace(event)
	if event == "" {
		return
	}
	if kind != "" {
		log.Printf("[knowledge-image] event=%s kind=%s count=%d", event, kind, count)
		return
	}
	log.Printf("[knowledge-image] event=%s count=%d", event, count)
}
