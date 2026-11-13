package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// botDiscussMetaKey is the plan and the discuss/planned/executing state of
// one conversation, stored on that session. A process restart reads it back
// from the session. It is not a run.
const botDiscussMetaKey = "bot_discuss"

// BotDiscussState is one conversation's plan. State is discuss, planned, or
// executing. Excerpts are the lines already written into the session.
type BotDiscussState struct {
	State    string        `json:"state"`
	Session  string        `json:"session,omitempty"`
	Plan     BotIntakePlan `json:"plan"`
	Excerpts []string      `json:"excerpts,omitempty"`
	Persona  string        `json:"persona,omitempty"`
}

// BotDiscussInput is one discussion turn. A nil plan leaves the stored plan
// as it is. UserText and AssistantText are appended to the session and do
// not start a run.
type BotDiscussInput struct {
	ClientSessionKey string         `json:"client_session_key"`
	ClientMessageID  string         `json:"client_message_id,omitempty"`
	UserText         string         `json:"user_text,omitempty"`
	AssistantText    string         `json:"assistant_text,omitempty"`
	Plan             *BotIntakePlan `json:"plan,omitempty"`
	Persona          string         `json:"persona,omitempty"`
}

// RecordBotDiscussion writes this turn into the conversation and merges the
// plan. It does not create a run.
func (s *Service) RecordBotDiscussion(ctx context.Context, p Principal, instanceID string, in BotDiscussInput) (BotDiscussState, error) {
	key := strings.TrimSpace(in.ClientSessionKey)
	if key == "" {
		return BotDiscussState{}, ErrBotThreadsBadBot
	}
	sess, err := s.resolveSendSession(ctx, p, instanceID, SendMessageInput{ClientSessionKey: key})
	if err != nil {
		return BotDiscussState{}, err
	}
	state := readDiscussState(sess.Metadata)
	state.Session = key
	if id := strings.TrimSpace(in.ClientMessageID); id != "" {
		if messages, err := s.store.ListMessages(sess.ID); err == nil {
			for _, msg := range messages {
				if msg.Role == MessageRoleUser && msg.Metadata != nil && msg.Metadata["client_message_id"] == id {
					return state, nil
				}
			}
		}
	}
	state.Plan = mergeDiscussPlan(state.Plan, in.Plan)
	if persona := clipPersona(in.Persona); persona != "" {
		state.Persona = persona
	}
	state.Excerpts = appendDiscussLines(state.Excerpts, in.UserText, in.AssistantText)
	if state.State != "executing" {
		state.State = discussStateOf(state.Plan)
	}
	now := s.now()
	userText := strings.TrimSpace(in.UserText)
	if userText != "" {
		meta := map[string]string{"bot_discuss": "1"}
		if id := strings.TrimSpace(in.ClientMessageID); id != "" {
			meta["client_message_id"] = id
		}
		msg := Message{ID: NewID("msg"), SessionID: sess.ID, TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, Role: MessageRoleUser, InputType: "text/plain", Content: userText, Metadata: meta, CreatedAt: now}
		if err := s.store.SaveMessage(msg); err != nil && !errors.Is(err, ErrAlreadyExists) {
			return BotDiscussState{}, err
		}
	}
	if ack := strings.TrimSpace(in.AssistantText); ack != "" {
		msg := Message{ID: NewID("msg"), SessionID: sess.ID, TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, Role: MessageRoleAssistant, Content: ack, Metadata: map[string]string{"bot_discuss": "1"}, CreatedAt: now}
		if err := s.store.SaveMessage(msg); err != nil {
			return BotDiscussState{}, err
		}
	}
	if err := s.writeDiscussState(ctx, p, instanceID, sess, state); err != nil {
		return BotDiscussState{}, err
	}
	return state, nil
}

// ReadBotDiscussion returns the plan stored on this conversation. A missing
// plan is an empty discuss state, not an error.
func (s *Service) ReadBotDiscussion(ctx context.Context, p Principal, instanceID, clientSessionKey string) (BotDiscussState, error) {
	key := strings.TrimSpace(clientSessionKey)
	if key == "" {
		return BotDiscussState{State: "discuss"}, nil
	}
	sess, ok := s.findSessionByClientKey(p, instanceID, key)
	if !ok {
		return BotDiscussState{State: "discuss", Session: key}, nil
	}
	state := readDiscussState(sess.Metadata)
	state.Session = key
	if state.State == "" {
		state.State = "discuss"
	}
	return state, nil
}

// MarkBotDiscussionExecuting records that this conversation's plan is the
// instruction of a run that has been admitted.
func (s *Service) MarkBotDiscussionExecuting(ctx context.Context, p Principal, instanceID, clientSessionKey string) error {
	key := strings.TrimSpace(clientSessionKey)
	if key == "" {
		return nil
	}
	sess, ok := s.findSessionByClientKey(p, instanceID, key)
	if !ok {
		return nil
	}
	state := readDiscussState(sess.Metadata)
	state.Session = key
	state.State = "executing"
	return s.writeDiscussState(ctx, p, instanceID, &sess, state)
}

func (s *Service) writeDiscussState(ctx context.Context, p Principal, instanceID string, sess *Session, state BotDiscussState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	meta := cloneMap(sess.Metadata)
	if meta == nil {
		meta = map[string]string{}
	}
	meta[botDiscussMetaKey] = string(raw)
	_, err = s.UpdateSession(ctx, p, instanceID, sess.ID, UpdateSessionInput{Metadata: meta})
	return err
}

func readDiscussState(meta map[string]string) BotDiscussState {
	var state BotDiscussState
	if meta == nil {
		return state
	}
	raw := strings.TrimSpace(meta[botDiscussMetaKey])
	if raw == "" {
		return state
	}
	_ = json.Unmarshal([]byte(raw), &state)
	return state
}

func mergeDiscussPlan(cur BotIntakePlan, next *BotIntakePlan) BotIntakePlan {
	if next == nil {
		return cur
	}
	if goal := strings.TrimSpace(next.Goal); goal != "" {
		cur.Goal = goal
	}
	if next.Constraints != nil {
		cur.Constraints = next.Constraints
	}
	if next.Success != nil {
		cur.Success = next.Success
	}
	if next.Open != nil {
		cur.Open = next.Open
	}
	return cur
}

func discussStateOf(plan BotIntakePlan) string {
	if strings.TrimSpace(plan.Goal) == "" {
		return "discuss"
	}
	if plan.Open != nil && len(*plan.Open) > 0 {
		return "discuss"
	}
	return "planned"
}

func appendDiscussLines(have []string, userText, assistantText string) []string {
	if line := oneLineRunes(userText, 200); line != "" {
		have = append(have, "人: "+line)
	}
	if line := oneLineRunes(assistantText, 200); line != "" {
		have = append(have, "助手: "+line)
	}
	if len(have) > 12 {
		have = have[len(have)-12:]
	}
	return have
}

// ExecutionText is the instruction a started run receives: the plan and the
// discussion already in this conversation, not the start sentence alone.
func (s BotDiscussState) ExecutionText() string {
	var b strings.Builder
	b.WriteString("按这个计划执行。")
	if goal := strings.TrimSpace(s.Plan.Goal); goal != "" {
		b.WriteString("\n目标：")
		b.WriteString(goal)
	}
	writePlanLines(&b, "约束", s.Plan.Constraints)
	writePlanLines(&b, "完成标准", s.Plan.Success)
	writePlanLines(&b, "还没定的问题", s.Plan.Open)
	if len(s.Excerpts) > 0 {
		b.WriteString("\n讨论摘录：")
		for _, line := range s.Excerpts {
			b.WriteString("\n- ")
			b.WriteString(line)
		}
	}
	return b.String()
}

func writePlanLines(b *strings.Builder, title string, lines *[]string) {
	if lines == nil || len(*lines) == 0 {
		return
	}
	b.WriteString("\n")
	b.WriteString(title)
	b.WriteString("：")
	for _, line := range *lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		b.WriteString("\n- ")
		b.WriteString(line)
	}
}
