package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestPromoteGroupDiscussionResultToMemoryWritesProjectKnowledge(t *testing.T) {
	store := newTestMemoryStore(t)
	app := &App{memoryStore: store}
	detail := groupDiscussionMemoryTestDetail("disc-1", "release safety", "Which rollout path should we use?", "Use staged rollout.")
	detail.Messages = append(detail.Messages, a2a.Message{ID: "disc-1-m2", SessionID: "disc-1", FromID: "expert", Kind: a2a.MessageEvidence, Content: "Rollback immediately if gate fails."})
	detail.Decision = &a2a.Decision{ProposalID: "prop-1", Summary: "Use staged rollout", Rationale: "Accepted because rollback gates are explicit.", RollbackOn: []string{"gate fails"}}
	detail.ReviewSummaries = map[string]a2a.ReviewSummary{"prop-1": {Approvals: 1, ReviewedBy: []string{"expert"}}}
	detail.Session = &a2a.Session{ID: "disc-1", Escalation: &a2a.Escalation{RaisedBy: "expert", Reason: "requires owner approval", Target: "iworkercenter"}}
	result := GroupDiscussionSummarizeResult{
		ConsultationID: "disc-1",
		Summary:        "Use staged rollout",
		Rationale:      "Expert evidence favored smaller blast radius.",
		Risks:          []string{"Migration risk remains"},
		AnswerCount:    1,
		Confidence:     0.82,
	}

	app.promoteGroupDiscussionResultToMemory(detail, result)

	entries := store.List(memory.CategoryProjectKnowledge, "")
	var found *memory.Entry
	for i := range entries {
		if hasTag(entries[i].Tags, groupDiscussionResultTag) {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected A2A result memory, got %#v", entries)
	}
	if found.SourceType != groupDiscussionMemorySourceType || !strings.Contains(found.Content, "Use staged rollout") || !hasTag(found.Tags, "discussion:disc-1") {
		t.Fatalf("unexpected A2A memory entry: %#v", *found)
	}
	for _, want := range []string{"Decision rationale: Accepted because rollback gates are explicit.", "Rollback on:\n- gate fails", "Review summaries:\n- prop-1: approvals=1", "Escalation:\n- Reason: requires owner approval", "- Target: iworkercenter", "- Raised by: expert"} {
		if !strings.Contains(found.Content, want) {
			t.Fatalf("memory content missing %q:\n%s", want, found.Content)
		}
	}
	if !strings.Contains(found.Content, "Matched rollback triggers:\n- gate fails") {
		t.Fatalf("memory content missing matched rollback trigger section:\n%s", found.Content)
	}
	for _, want := range []string{"has_rollback", "has_review_summary", "has_escalation"} {
		if !hasTag(found.Tags, want) {
			t.Fatalf("memory tags missing %q: %#v", want, found.Tags)
		}
	}
	if !hasTag(found.Tags, groupDiscussionRollbackTriggered) {
		t.Fatalf("memory tags missing rollback triggered marker: %#v", found.Tags)
	}
	if !hasTagPrefix(found.Tags, "escalation_target:") {
		t.Fatalf("memory tags missing escalation target hash: %#v", found.Tags)
	}
	if !hasTagPrefix(found.Tags, groupDiscussionRollbackTagPrefix) {
		t.Fatalf("memory tags missing rollback condition hash: %#v", found.Tags)
	}
	if !hasTagPrefix(found.Tags, groupDiscussionRollbackMatchPref) {
		t.Fatalf("memory tags missing rollback matched hash: %#v", found.Tags)
	}
}

func TestPromoteGroupDiscussionResultToMemoryCreatesConflictReviewCandidate(t *testing.T) {
	store := newTestMemoryStore(t)
	app := &App{memoryStore: store}
	first := groupDiscussionMemoryTestDetail("disc-1", "release safety", "Which rollout path should we use?", "Use staged rollout.")
	second := groupDiscussionMemoryTestDetail("disc-2", "release safety", "Which rollout path should we use?", "Avoid staged rollout.")

	app.promoteGroupDiscussionResultToMemory(first, GroupDiscussionSummarizeResult{ConsultationID: "disc-1", Summary: "Use staged rollout", AnswerCount: 1})
	app.promoteGroupDiscussionResultToMemory(second, GroupDiscussionSummarizeResult{ConsultationID: "disc-2", Summary: "Avoid staged rollout", AnswerCount: 1})

	entries := store.List(memory.CategoryProjectKnowledge, "")
	if !hasMemoryEntryWithTag(entries, groupDiscussionConflictTag) {
		t.Fatalf("expected A2A conflict review memory, got %#v", entries)
	}
	for _, entry := range entries {
		if hasTag(entry.Tags, groupDiscussionConflictTag) && (!strings.Contains(entry.Content, "Review before") || entry.SourceType != groupDiscussionMemorySourceType) {
			t.Fatalf("unexpected conflict review entry: %#v", entry)
		}
	}
}

func TestGroupDiscussionOpposingDecisionSignals(t *testing.T) {
	if !groupDiscussionOpposingDecisionSignals("Summary: Use staged rollout", "Summary: Avoid staged rollout") {
		t.Fatal("expected opposing use/avoid signals")
	}
	if groupDiscussionOpposingDecisionSignals("Summary: Use staged rollout", "Summary: Prefer staged rollout") {
		t.Fatal("same-direction signals should not conflict")
	}
}

func TestSummarizeGroupDiscussionDetailUsesEscalation(t *testing.T) {
	detail := groupDiscussionMemoryTestDetail("disc-1", "release safety", "Which rollout path should we use?", "")
	detail.Messages = nil
	detail.Session = &a2a.Session{ID: "disc-1", Escalation: &a2a.Escalation{RaisedBy: "expert", Reason: "requires owner approval", Target: "iworkercenter"}}

	got := summarizeGroupDiscussionDetail(detail)

	if got.Summary != "Escalated: requires owner approval" || !strings.Contains(got.Rationale, "iworkercenter") || !strings.Contains(got.Rationale, "expert") {
		t.Fatalf("summary = %+v", got)
	}
}

func groupDiscussionMemoryTestDetail(id, topic, question, answer string) a2a.HubDiscussionDetail {
	return a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: id, Topic: topic, Question: question, Status: string(a2a.SessionOpen), ParticipantIDs: []string{"initiator", "expert"}},
		Messages:   []a2a.Message{{ID: id + "-m1", SessionID: id, FromID: "expert", Kind: a2a.MessageAnswer, Content: answer}},
	}
}

func hasTagPrefix(tags []string, prefix string) bool {
	for _, tag := range tags {
		if strings.HasPrefix(tag, prefix) {
			return true
		}
	}
	return false
}
