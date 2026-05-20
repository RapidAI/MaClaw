package memory

import (
	"context"
	"time"
)

// ExtractOnlineConversationForHost runs the shared online extraction pipeline if
// it is installed on the store. Host adapters pass prepared conversation
// messages and do not need to inspect or hold the OnlineExtractor directly.
func (s *Store) ExtractOnlineConversationForHost(ctx context.Context, messages []ConversationMessage, summary string, referenceTime time.Time, ownerID string) *OnlineExtractionResult {
	if s == nil {
		return &OnlineExtractionResult{}
	}
	oe := s.OnlineExtractor()
	if oe == nil {
		return &OnlineExtractionResult{}
	}
	return oe.ExtractAndIntegrate(ctx, messages, summary, referenceTime, ownerID)
}

// ExtractOnlineConversation is kept as the core store-level primitive. Host
// packages should prefer ExtractOnlineConversationForHost so the boundary is
// visible and policy tests can keep host integrations on corelib facades.
func (s *Store) ExtractOnlineConversation(ctx context.Context, messages []ConversationMessage, summary string, referenceTime time.Time, ownerID string) *OnlineExtractionResult {
	return s.ExtractOnlineConversationForHost(ctx, messages, summary, referenceTime, ownerID)
}
