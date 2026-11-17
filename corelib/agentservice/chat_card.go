package agentservice

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/chatcard"
)

// SetChatCards attaches the durable interaction store. A nil store leaves
// ask_user on the older text path.
func (s *Service) SetChatCards(store *chatcard.Store) {
	if s == nil {
		return
	}
	s.chatCards = store
}

// ChatCards returns the interaction store, or nil when the host did not set one.
func (s *Service) ChatCards() *chatcard.Store {
	if s == nil {
		return nil
	}
	return s.chatCards
}

// attachChatCard publishes a server-owned card for a non-secret ask, and a
// handoff card for a secret. The next typed message is not the decision.
func (s *Service) attachChatCard(p Principal, sess Session, run Run, msg *Message) {
	if s == nil || s.chatCards == nil || msg == nil || msg.Metadata == nil {
		return
	}
	if strings.TrimSpace(msg.Metadata[metaAskUserQuestion]) == "" && strings.TrimSpace(msg.Metadata[metaAskUserSecretName]) == "" {
		return
	}
	botID := strings.TrimSpace(msg.Metadata["bot_id"])
	if botID == "" {
		botID = strings.TrimSpace(run.Metadata["bot_id"])
	}
	if botID == "" {
		botID = run.InstanceID
	}
	taskID := strings.TrimSpace(run.SessionID)
	if taskID == "" {
		taskID = sess.ID
	}
	card, _, err := chatcard.FromAsk(chatcard.AskInput{
		Question:    msg.Metadata[metaAskUserQuestion],
		InputType:   msg.Metadata[metaAskUserInputType],
		Options:     askOptions(msg.Metadata[metaAskUserOptionsJSON]),
		Context:     msg.Metadata["ask_user_context"],
		SecretName:  msg.Metadata[metaAskUserSecretName],
		BotID:       botID,
		TaskID:      taskID,
		RunID:       run.ID,
		TenantID:    p.TenantID,
		UserID:      p.UserID,
		LeaseID:     msg.Metadata["chat_card_lease_id"],
		DesktopMode: msg.Metadata["chat_card_desktop_mode"],
	}, s.now())
	if err != nil {
		return
	}
	stored, err := s.chatCards.Put(chatcard.OpenInput{
		TenantID: card.TenantID, UserID: card.UserID, BotID: card.BotID, TaskID: card.TaskID, RunID: card.RunID,
		Kind: card.Kind, Question: card.Question, Choices: card.Choices, Fields: card.Fields,
		Operation: card.Operation, Evidence: card.Evidence, Lines: card.Lines, Target: card.Target,
		LeaseID: card.LeaseID, DesktopMode: card.DesktopMode, InputPaused: card.InputPaused,
	})
	if err != nil {
		return
	}
	view := chatcard.Public(stored)
	raw, err := json.Marshal(view)
	if err != nil {
		return
	}
	msg.Metadata["interaction_json"] = string(raw)
	msg.Metadata[sessionMetaPendingChatCard] = stored.ID
}

func askOptions(raw string) []string {
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

// DeliverChatCardResume stores the continuation checkpoint for a queued
// resume. It does not reset the run and does not execute the task again.
func (s *Service) DeliverChatCardResume(ctx context.Context, p Principal, instanceID string, out chatcard.Outcome) (*Run, error) {
	if s == nil || s.chatCards == nil {
		return nil, ErrRunNotFound
	}
	run, err := s.GetRun(ctx, p, instanceID, out.RunID)
	if err != nil {
		return nil, err
	}
	if run.InstanceID != instanceID || run.UserID != p.UserID || run.TenantID != p.TenantID || run.ID != out.RunID {
		return nil, ErrRunNotFound
	}
	if strings.TrimSpace(out.ResumeID) == "" {
		return run, nil
	}
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	if existing := strings.TrimSpace(run.Metadata["chat_card_resume_id"]); existing != "" && existing != out.ResumeID {
		return nil, ErrForbidden
	}
	status := run.Status
	completed := run.CompletedAt
	sideEffect := run.Metadata["side_effect"]
	if existing := strings.TrimSpace(run.Metadata["chat_card_resume_id"]); existing == out.ResumeID {
		_ = s.chatCards.MarkDelivered(out.ResumeID)
		return run, nil
	}
	run.Metadata["chat_card_resume_id"] = out.ResumeID
	run.Metadata["chat_card_checkpoint"] = out.Mode + ":" + out.TaskID
	run.Metadata["side_effect"] = sideEffect
	run.Status = status
	run.CompletedAt = completed
	if err := s.store.SaveRun(*run); err != nil {
		return nil, err
	}
	if err := s.chatCards.MarkDelivered(out.ResumeID); err != nil {
		return nil, err
	}
	return run, nil
}

// ParseAskUserOptions is unused by the model. Kept so a host test can build
// the same option list the card uses.
func ParseAskUserOptions(req *agent.AskUserRequest) []string {
	if req == nil {
		return nil
	}
	return append([]string(nil), req.Options...)
}
