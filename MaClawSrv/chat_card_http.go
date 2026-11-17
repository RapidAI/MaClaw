package main

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/chatcard"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// desktopChatCards is the store request_takeover can publish into.
// The HTTP server sets it. Tests that construct a store set it too.
var desktopChatCards *chatcard.Store

type interactionOpenBody struct {
	Kind        string              `json:"kind"`
	Question    string              `json:"question"`
	Choices     []chatcard.Choice   `json:"choices"`
	Fields      []chatcard.Field    `json:"fields"`
	Operation   chatcard.Operation  `json:"operation"`
	Evidence    []chatcard.Evidence `json:"evidence"`
	Lines       []chatcard.Line     `json:"lines"`
	Target      *chatcard.Target    `json:"target"`
	LeaseID     string              `json:"lease_id"`
	DesktopMode string              `json:"desktop_mode"`
	ShotBase64  string              `json:"shot_base64"`
}

type interactionDecisionBody struct {
	TaskID           string            `json:"task_id"`
	BotID            string            `json:"bot_id"`
	ChoiceIDs        []string          `json:"choice_ids"`
	Fields           map[string]string `json:"fields"`
	OperationVersion int               `json:"operation_version"`
	PayloadHash      string            `json:"payload_hash"`
	ConsentHash      string            `json:"consent_hash"`
	Decline          bool              `json:"decline"`
	TargetOrigin     string            `json:"target_origin"`
	TargetURL        string            `json:"target_url"`
	TargetFrame      string            `json:"target_frame"`
	ShotID           string            `json:"shot_id"`
	CapturedAt       time.Time         `json:"captured_at"`
	LeaseID          string            `json:"lease_id"`
}

func (s *HTTPServer) handleOpenInteraction(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s == nil || s.chatCards == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "interaction store unavailable"})
		return
	}
	var body interactionOpenBody
	if !decodeJSON(w, r, &body) {
		return
	}
	run, err := s.svc.GetRun(r.Context(), p, r.PathValue("instanceId"), r.PathValue("runId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	botID := strings.TrimSpace(run.Metadata["bot_id"])
	if botID == "" {
		botID = run.InstanceID
	}
	taskID := run.SessionID
	var shot []byte
	if strings.TrimSpace(body.ShotBase64) != "" {
		shot, err = chatcard.DecodeShot(body.ShotBase64)
		if err != nil {
			writeChatCardError(w, err)
			return
		}
	}
	card, err := s.chatCards.Put(chatcard.OpenInput{
		TenantID: p.TenantID, UserID: p.UserID, BotID: botID, TaskID: taskID, RunID: run.ID,
		Kind: body.Kind, Question: body.Question, Choices: body.Choices, Fields: body.Fields,
		Operation: body.Operation, Evidence: body.Evidence, Lines: body.Lines, Target: body.Target,
		LeaseID: body.LeaseID, DesktopMode: body.DesktopMode, ShotBytes: shot,
	})
	if err != nil {
		writeChatCardError(w, err)
		return
	}
	if sess, err := s.svc.GetSession(r.Context(), p, run.InstanceID, run.SessionID); err == nil && sess != nil {
		if sess.Metadata == nil {
			sess.Metadata = map[string]string{}
		}
		delete(sess.Metadata, "pending_ask_user")
		delete(sess.Metadata, "pending_ask_user_question")
		delete(sess.Metadata, "pending_ask_user_input_type")
		delete(sess.Metadata, "pending_ask_user_options_json")
		sess.Metadata["pending_chat_card"] = card.ID
		_, _ = s.svc.UpdateSession(r.Context(), p, run.InstanceID, run.SessionID, agentservice.UpdateSessionInput{Metadata: sess.Metadata})
	}
	writeJSON(w, http.StatusOK, chatcard.Public(card))
}

func (s *HTTPServer) handleGetInteraction(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	card, ok := s.ownedCard(w, r, p)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, chatcard.Public(card))
}

func (s *HTTPServer) handleInteractionShot(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s == nil || s.chatCards == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "interaction store unavailable"})
		return
	}
	raw, card, err := s.chatCards.ShotBytes(chatActor(p), r.PathValue("requestId"))
	if err != nil {
		writeChatCardError(w, err)
		return
	}
	if card.RunID != r.PathValue("runId") || card.Shot == nil {
		writeChatCardError(w, chatcard.ErrForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"mime": card.Shot.MIME,
		"data": base64.StdEncoding.EncodeToString(raw),
	})
}

func (s *HTTPServer) handleInteractionDecision(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s == nil || s.chatCards == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "interaction store unavailable"})
		return
	}
	var body interactionDecisionBody
	if !decodeJSON(w, r, &body) {
		return
	}
	run, err := s.svc.GetRun(r.Context(), p, r.PathValue("instanceId"), r.PathValue("runId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	out, err := s.chatCards.Submit(chatActor(p), chatcard.Submission{
		RequestID: r.PathValue("requestId"), TaskID: body.TaskID, BotID: body.BotID, RunID: run.ID,
		ChoiceIDs: body.ChoiceIDs, Fields: body.Fields, OperationVersion: body.OperationVersion,
		PayloadHash: body.PayloadHash, ConsentHash: body.ConsentHash, Decline: body.Decline, TargetOrigin: body.TargetOrigin,
		TargetURL: body.TargetURL, TargetFrame: body.TargetFrame, ShotID: body.ShotID,
		CapturedAt: body.CapturedAt, LeaseID: body.LeaseID,
	})
	if err != nil {
		writeChatCardError(w, err)
		return
	}
	delivered := run
	if out.Resume {
		delivered, err = s.svc.DeliverChatCardResume(r.Context(), p, run.InstanceID, out)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": out.Status, "resume": out.Resume, "already": out.Already, "mode": out.Mode,
		"task_id": out.TaskID, "run_id": delivered.ID, "request_id": out.RequestID,
		"resume_id": out.ResumeID, "bot_id": out.BotID, "done": false,
	})
}

func (s *HTTPServer) handleInteractionDesktop(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s == nil || s.chatCards == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "interaction store unavailable"})
		return
	}
	var body struct {
		BotID      string    `json:"bot_id"`
		LeaseID    string    `json:"lease_id"`
		CapturedAt time.Time `json:"captured_at"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	bind, err := s.chatCards.Desktop(chatActor(p), r.PathValue("requestId"), body.BotID, body.LeaseID, body.CapturedAt)
	if err != nil {
		writeChatCardError(w, err)
		return
	}
	if bind.RunID != r.PathValue("runId") || bind.Wake {
		writeChatCardError(w, chatcard.ErrForbidden)
		return
	}
	writeJSON(w, http.StatusOK, bind)
}

func (s *HTTPServer) ownedCard(w http.ResponseWriter, r *http.Request, p agentservice.Principal) (chatcard.Card, bool) {
	if s == nil || s.chatCards == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "interaction store unavailable"})
		return chatcard.Card{}, false
	}
	card, err := s.chatCards.Get(r.PathValue("requestId"))
	if err != nil {
		writeChatCardError(w, err)
		return chatcard.Card{}, false
	}
	if card.TenantID != p.TenantID || card.UserID != p.UserID || card.RunID != r.PathValue("runId") {
		writeChatCardError(w, chatcard.ErrForbidden)
		return chatcard.Card{}, false
	}
	return card, true
}

func chatActor(p agentservice.Principal) chatcard.Actor {
	return chatcard.Actor{Kind: chatcard.ActorUser, TenantID: p.TenantID, UserID: p.UserID, Authenticated: true}
}

func writeChatCardError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, chatcard.ErrUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, chatcard.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, chatcard.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, chatcard.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, chatcard.ErrExpired), errors.Is(err, chatcard.ErrStale):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// offerTakeoverCard records a handoff for this run when a screenshot was
// already captured. It does not take a new picture.
func offerTakeoverCard(ctx context.Context, scope agentruntime.Scope, reason string) {
	store := desktopChatCards
	if store == nil {
		return
	}
	owner, bot := desktop.SplitDesktopUser(scope.UserID)
	if owner == "" {
		owner = scope.UserID
	}
	mode := "shared"
	lease := owner
	if bot != "" {
		mode = "isolated"
		lease = scope.UserID
	}
	if strings.TrimSpace(bot) == "" {
		bot = scope.InstanceID
	}
	var shot []byte
	if slot := desktopShotSlotFrom(ctx); slot != nil {
		slot.mu.Lock()
		data := strings.TrimSpace(slot.shot.Data)
		ready := slot.ready
		slot.mu.Unlock()
		if ready && data != "" {
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err == nil {
				shot = decoded
			}
		}
	}
	taskID := scope.SessionID
	if taskID == "" {
		taskID = scope.RunID
	}
	_, _ = store.Put(chatcard.OpenInput{
		TenantID: scope.TenantID, UserID: owner, BotID: bot, TaskID: taskID, RunID: scope.RunID,
		Kind: chatcard.KindNativeHandoff, Question: reason,
		LeaseID: lease, DesktopMode: mode, ShotBytes: shot, InputPaused: true,
	})
}
