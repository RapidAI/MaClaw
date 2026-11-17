package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/chatcard"
)

// ChatCardEvidence is a fresh read of the task the person just handed back.
// Text that was already printed on the card is not this value.
type ChatCardEvidence struct {
	Source     string
	Text       string
	ObservedAt time.Time
}

// ChatCardObserver reads the task's current evidence. The card click is not
// that read.
type ChatCardObserver interface {
	ObserveTaskEvidence(ctx context.Context, p Principal, taskID, runID string) (ChatCardEvidence, error)
}

// ChatCardObserverFunc adapts a function to ChatCardObserver.
type ChatCardObserverFunc func(context.Context, Principal, string, string) (ChatCardEvidence, error)

func (f ChatCardObserverFunc) ObserveTaskEvidence(ctx context.Context, p Principal, taskID, runID string) (ChatCardEvidence, error) {
	return f(ctx, p, taskID, runID)
}

// ChatCardContinuation is the suspended task plus the person's decision.
// PriorEffects already happened and must not be applied again.
type ChatCardContinuation struct {
	ResumeID     string
	Mode         string
	TaskID       string
	RunID        string
	SessionID    string
	InstanceID   string
	Decision     string
	PriorEffects []string
	Evidence     ChatCardEvidence
}

// ChatCardContinuationResult is only the post-checkpoint step.
type ChatCardContinuationResult struct {
	Content  string
	Evidence string
	Source   string
	Effects  []string
}

// SetChatCardObserver installs the host that re-reads a task for a handoff.
func (s *Service) SetChatCardObserver(observer ChatCardObserver) {
	if s == nil {
		return
	}
	s.chatCardObserver = observer
}

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
func (s *Service) attachChatCard(p Principal, sess Session, run *Run, msg *Message) {
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
	noteChatCardCheckpoint(run, stored.ID, msg.Metadata["side_effect"])
}

func askOptions(raw string) []string {
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

// NoteChatCardCheckpoint marks the run suspended at this card. The run's
// status and earlier side effects stay as they are.
func (s *Service) NoteChatCardCheckpoint(ctx context.Context, p Principal, instanceID, runID, cardID, priorEffect string) error {
	run, err := s.GetRun(ctx, p, instanceID, runID)
	if err != nil {
		return err
	}
	noteChatCardCheckpoint(run, cardID, priorEffect)
	return s.store.SaveRun(*run)
}

func noteChatCardCheckpoint(run *Run, cardID, priorEffect string) {
	if run == nil {
		return
	}
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	run.Metadata["chat_card_suspended"] = strings.TrimSpace(cardID)
	if effect := strings.TrimSpace(priorEffect); effect != "" {
		run.Metadata["side_effect"] = effect
	}
	if effect := strings.TrimSpace(run.Metadata["side_effect"]); effect != "" {
		run.Metadata["chat_card_prior_effects"] = effect
	}
	run.WaitingForUser = true
}

// DeliverChatCardResume runs the post-checkpoint continuation for a queued
// resume. The checkpoint run stays in its current status. Work recorded in
// chat_card_prior_effects is not executed again.
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
	return s.deliverChatCardResume(ctx, p, run, out.ResumeID, out.Mode, out.TaskID)
}

func (s *Service) deliverChatCardResume(ctx context.Context, p Principal, run *Run, resumeID, mode, taskID string) (*Run, error) {
	ex, err := s.chatCards.BeginExecution(resumeID)
	if err != nil {
		return nil, err
	}
	if ex.Status == "completed" {
		if err := s.ensureContinuationRun(ctx, p, run, ex); err != nil {
			return nil, err
		}
		_ = s.chatCards.MarkDelivered(resumeID)
		return run, nil
	}
	prior := priorEffects(run)
	var evidence ChatCardEvidence
	if mode == "verify" {
		if s.chatCardObserver == nil {
			return nil, fmt.Errorf("chat card verification has no evidence source")
		}
		evidence, err = s.chatCardObserver.ObserveTaskEvidence(ctx, p, taskID, run.ID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(evidence.Text) == "" {
			return nil, fmt.Errorf("chat card verification did not observe current task evidence")
		}
		evidence.ObservedAt = s.now()
	}
	in := ChatCardContinuation{
		ResumeID: resumeID, Mode: mode, TaskID: taskID, RunID: run.ID,
		SessionID: run.SessionID, InstanceID: run.InstanceID,
		PriorEffects: prior, Evidence: evidence,
	}
	result, err := s.invokeChatCardContinuation(ctx, p, run, in)
	if err != nil {
		return nil, err
	}
	for _, effect := range result.Effects {
		if containsEffect(prior, effect) {
			return nil, fmt.Errorf("chat card continuation repeated a completed effect %q", effect)
		}
	}
	if s.chatCardBeforeFinish != nil {
		if err := s.chatCardBeforeFinish(); err != nil {
			return nil, err
		}
	}
	continuationID := NewID("run")
	committed, stored, err := s.chatCards.FinishExecution(chatcard.Execution{
		ResumeID: resumeID, Mode: mode, Evidence: result.Evidence, Source: result.Source,
		Effects: result.Effects, Content: result.Content, ContinuationRunID: continuationID,
		InstanceID: run.InstanceID,
	})
	if err != nil {
		return nil, err
	}
	if !committed {
		if err := s.ensureContinuationRun(ctx, p, run, stored); err != nil {
			return nil, err
		}
		_ = s.chatCards.MarkDelivered(resumeID)
		return run, nil
	}
	if err := s.saveContinuationRun(run, continuationID, mode, resumeID, result); err != nil {
		return nil, err
	}
	if err := s.chatCards.MarkDelivered(resumeID); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Service) invokeChatCardContinuation(ctx context.Context, p Principal, run *Run, in ChatCardContinuation) (ChatCardContinuationResult, error) {
	req := ExecuteRequest{
		Principal:            p,
		Instance:             Instance{ID: run.InstanceID},
		Session:              Session{ID: run.SessionID},
		RunID:                run.ID,
		ChatCardContinuation: &in,
	}
	if continuer, ok := s.executor.(interface {
		ContinueChatCard(context.Context, ExecuteRequest, ChatCardContinuation) (ChatCardContinuationResult, error)
	}); ok {
		return continuer.ContinueChatCard(ctx, req, in)
	}
	return continueChatCard(in)
}

// ContinueChatCard is the production continuation. A payment sentence in the
// decision is not recorded as checkout authorization.
func (e *CoreAgentExecutor) ContinueChatCard(ctx context.Context, req ExecuteRequest, in ChatCardContinuation) (ChatCardContinuationResult, error) {
	_ = ctx
	_ = e
	_ = req
	return continueChatCard(in)
}

// continueChatCard is the production post-checkpoint step. It does not call
// the model and it does not record a payment authorization. Purchase, send,
// and publish are not granted by this slice.
func continueChatCard(in ChatCardContinuation) (ChatCardContinuationResult, error) {
	if in.Mode == "verify" && strings.TrimSpace(in.Evidence.Text) == "" {
		return ChatCardContinuationResult{}, fmt.Errorf("chat card verification did not observe current task evidence")
	}
	content := "已从上次停下的地方继续。购买、发送和发布不在这张卡片的授权里。"
	if in.Mode == "verify" {
		content = "已核对当前任务证据：" + strings.TrimSpace(in.Evidence.Text)
	}
	return ChatCardContinuationResult{
		Content: content, Evidence: strings.TrimSpace(in.Evidence.Text), Source: strings.TrimSpace(in.Evidence.Source),
	}, nil
}

func (s *Service) saveContinuationRun(parent *Run, continuationID, mode, resumeID string, result ChatCardContinuationResult) error {
	now := s.now()
	meta := map[string]string{
		"chat_card_resume_id":    resumeID,
		"chat_card_parent_run":   parent.ID,
		"chat_card_continuation": "1",
		"chat_card_mode":         mode,
	}
	if strings.TrimSpace(result.Evidence) != "" {
		meta["task_outcome_evidence"] = result.Evidence
		meta[TaskOutcomeKey] = TaskOutcomeUnverified
		meta[TaskOutcomeNoticeKey] = "核对的是当前任务证据。点击本身不是操作已经成功。"
	}
	if len(result.Effects) > 0 {
		meta["applied_effects"] = strings.Join(result.Effects, ",")
	}
	cont := Run{
		ID: continuationID, TenantID: parent.TenantID, UserID: parent.UserID, InstanceID: parent.InstanceID,
		SessionID: parent.SessionID, Status: RunStatusSucceeded, StartedAt: now, CompletedAt: &now,
		Metadata: meta,
	}
	msg := Message{
		ID: NewID("msg"), SessionID: parent.SessionID, TenantID: parent.TenantID, UserID: parent.UserID,
		InstanceID: parent.InstanceID, Role: MessageRoleAssistant, OutputType: "text/plain",
		Content: result.Content, Metadata: cloneMap(meta), CreatedAt: now,
	}
	if err := s.store.SaveMessage(msg); err != nil {
		return err
	}
	cont.AssistantMessageID = msg.ID
	if err := s.store.SaveRun(cont); err != nil {
		return err
	}
	if parent.Metadata == nil {
		parent.Metadata = map[string]string{}
	}
	status := parent.Status
	completed := parent.CompletedAt
	sideEffect := parent.Metadata["side_effect"]
	parent.Metadata["chat_card_resume_id"] = resumeID
	parent.Metadata["chat_card_continued"] = continuationID
	parent.Metadata["side_effect"] = sideEffect
	parent.Status = status
	parent.CompletedAt = completed
	parent.WaitingForUser = false
	return s.store.SaveRun(*parent)
}

func (s *Service) ensureContinuationRun(ctx context.Context, p Principal, parent *Run, ex chatcard.Execution) error {
	if strings.TrimSpace(ex.ContinuationRunID) == "" {
		return nil
	}
	if _, err := s.GetRun(ctx, p, parent.InstanceID, ex.ContinuationRunID); err == nil {
		return nil
	}
	return s.saveContinuationRun(parent, ex.ContinuationRunID, ex.Mode, ex.ResumeID, ChatCardContinuationResult{
		Content: ex.Content, Evidence: ex.Evidence, Source: ex.Source, Effects: ex.Effects,
	})
}

// ReconcileChatCardOutbox delivers every queued resume that was committed
// and not finished. A restart calls this without waiting for another click.
func (s *Service) ReconcileChatCardOutbox(ctx context.Context) error {
	if s == nil || s.chatCards == nil {
		return nil
	}
	queued, err := s.chatCards.QueuedResumes()
	if err != nil {
		return err
	}
	var first error
	for _, item := range queued {
		if strings.TrimSpace(item.InstanceID) == "" {
			if first == nil {
				first = fmt.Errorf("chat card resume %s has no instance", item.ID)
			}
			continue
		}
		card, err := s.chatCards.Get(item.CardID)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		p := Principal{TenantID: card.TenantID, UserID: card.UserID}
		run, err := s.GetRun(ctx, p, item.InstanceID, item.RunID)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if _, err := s.deliverChatCardResume(ctx, p, run, item.ID, item.Mode, item.TaskID); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// StartChatCardReconciler delivers queued resumes now and again until the
// service closes or ctx is cancelled.
func (s *Service) StartChatCardReconciler(ctx context.Context) {
	if s == nil || s.chatCards == nil {
		return
	}
	s.chatCardMu.Lock()
	if s.chatCardCancel != nil {
		s.chatCardMu.Unlock()
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.chatCardCancel = cancel
	s.chatCardMu.Unlock()
	go s.chatCardReconcileLoop(runCtx)
}

func (s *Service) stopChatCardReconciler() {
	if s == nil {
		return
	}
	s.chatCardMu.Lock()
	cancel := s.chatCardCancel
	s.chatCardCancel = nil
	s.chatCardMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Service) chatCardReconcileLoop(ctx context.Context) {
	_ = s.ReconcileChatCardOutbox(context.Background())
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.ReconcileChatCardOutbox(context.Background())
		}
	}
}

func priorEffects(run *Run) []string {
	if run == nil || run.Metadata == nil {
		return nil
	}
	return splitEffects(run.Metadata["chat_card_prior_effects"], run.Metadata["side_effect"])
}

func splitEffects(values ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

func containsEffect(effects []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, effect := range effects {
		if effect == want {
			return true
		}
	}
	return false
}

// ParseAskUserOptions is unused by the model. Kept so a host test can build
// the same option list the card uses.
func ParseAskUserOptions(req *agent.AskUserRequest) []string {
	if req == nil {
		return nil
	}
	return append([]string(nil), req.Options...)
}
