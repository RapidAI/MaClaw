package main

import (
	"encoding/json"
	"sync"
	"testing"
)

type spyWorkflowEvents struct {
	mu                   sync.Mutex
	suggestMaximizeCalls []suggestMaximizeCall
	docUpdateCalls       []docUpdateCall
}

type suggestMaximizeCall struct {
	UserID       string
	WorkflowType string
}

type docUpdateCall struct {
	UserID  string
	PhaseID string
	Content string
}

func newSpyWorkflowEvents() *spyWorkflowEvents {
	return &spyWorkflowEvents{}
}

func (s *spyWorkflowEvents) recordSuggestMaximize(userID, workflowType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suggestMaximizeCalls = append(s.suggestMaximizeCalls, suggestMaximizeCall{
		UserID:       userID,
		WorkflowType: workflowType,
	})
}

func (s *spyWorkflowEvents) recordDocUpdate(userID, phaseID, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docUpdateCalls = append(s.docUpdateCalls, docUpdateCall{
		UserID:  userID,
		PhaseID: phaseID,
		Content: content,
	})
}

func (s *spyWorkflowEvents) suggestMaximizeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.suggestMaximizeCalls)
}

func (s *spyWorkflowEvents) docUpdateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.docUpdateCalls)
}

func (s *spyWorkflowEvents) docUpdates() []docUpdateCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]docUpdateCall, len(s.docUpdateCalls))
	copy(out, s.docUpdateCalls)
	return out
}

func TestSteeringWorkflow_InterceptToolCallUsesExplicitPhaseMetadata(t *testing.T) {
	spy := newSpyWorkflowEvents()
	detector := NewSteeringWorkflowDetector("desktop-user")
	detector.detected = true

	emit := func(phaseID, content string) {
		spy.recordDocUpdate("desktop-user", phaseID, content)
	}

	writeArgs, _ := json.Marshal(map[string]string{
		"path":     "notes.md",
		"content":  "requirements body",
		"phase_id": "requirements",
	})
	detector.interceptToolCall("write_file", string(writeArgs), emit)

	pdfArgs, _ := json.Marshal(map[string]string{
		"content":  "design body",
		"doc_type": "design",
	})
	detector.interceptToolCall("generate_pdf", string(pdfArgs), emit)

	officeArgs, _ := json.Marshal(map[string]string{
		"action":   "generate_pdf",
		"content":  "tasks body",
		"doc_type": "task_plan",
	})
	detector.interceptToolCall("office", string(officeArgs), emit)

	got := spy.docUpdates()
	want := []docUpdateCall{
		{UserID: "desktop-user", PhaseID: "requirements", Content: "requirements body"},
		{UserID: "desktop-user", PhaseID: "design", Content: "design body"},
		{UserID: "desktop-user", PhaseID: "tasks", Content: "tasks body"},
	}
	if len(got) != len(want) {
		t.Fatalf("doc updates = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("docUpdates[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestSteeringWorkflow_InterceptToolCallDoesNotInferFromContent(t *testing.T) {
	spy := newSpyWorkflowEvents()
	detector := NewSteeringWorkflowDetector("desktop-user")
	detector.detected = true

	emit := func(phaseID, content string) {
		spy.recordDocUpdate("desktop-user", phaseID, content)
	}

	detector.interceptToolCall("generate_pdf", `{"content":"# Requirements\n\nbody"}`, emit)
	detector.interceptToolCall("office", `{"action":"generate_pdf","content":"# Technical Design\n\nbody"}`, emit)

	if spy.docUpdateCount() != 0 {
		t.Fatalf("docUpdateCount = %d, want 0", spy.docUpdateCount())
	}
}

func TestSteeringWorkflow_WriteFileFallsBackOnlyToStructuredFileTokens(t *testing.T) {
	spy := newSpyWorkflowEvents()
	detector := NewSteeringWorkflowDetector("desktop-user")
	detector.detected = true

	argsJSON, _ := json.Marshal(map[string]string{
		"path":    "requirements_snake.md",
		"content": "requirements body",
	})
	detector.interceptToolCall("write_file", string(argsJSON), func(phaseID, content string) {
		spy.recordDocUpdate("desktop-user", phaseID, content)
	})

	got := spy.docUpdates()
	if len(got) != 1 {
		t.Fatalf("doc updates = %#v, want one requirements update", got)
	}
	if got[0].PhaseID != "requirements" || got[0].Content != "requirements body" {
		t.Fatalf("doc update = %#v, want requirements body", got[0])
	}
}

func TestSteeringWorkflow_FileTokenFallbackRejectsSubstrings(t *testing.T) {
	spy := newSpyWorkflowEvents()
	detector := NewSteeringWorkflowDetector("desktop-user")
	detector.detected = true

	argsJSON, _ := json.Marshal(map[string]string{
		"path":    "redesign-notes.md",
		"content": "body",
	})
	detector.interceptToolCall("write_file", string(argsJSON), func(phaseID, content string) {
		spy.recordDocUpdate("desktop-user", phaseID, content)
	})

	if spy.docUpdateCount() != 0 {
		t.Fatalf("doc updates = %#v, want no substring-derived phase update", spy.docUpdates())
	}
}

func TestSteeringWorkflow_InterceptToolCallEdgeCases(t *testing.T) {
	detector := NewSteeringWorkflowDetector("test-user")
	detector.detected = true
	spy := newSpyWorkflowEvents()
	emit := func(phaseID, content string) {
		spy.recordDocUpdate("test-user", phaseID, content)
	}

	detector.interceptToolCall("write_file", "{invalid json", emit)
	detector.interceptToolCall("write_file", "{}", emit)
	detector.interceptToolCall("bash", `{"command":"ls"}`, emit)
	detector.interceptToolCall("write_file", `{"phase_id":"requirements","content":""}`, emit)
	detector.interceptToolCall("generate_pdf", `{"phase_id":"requirements","content":""}`, emit)
	detector.interceptToolCall("office", `{"action":"read_excel","phase_id":"requirements","content":"body"}`, emit)
	detector.interceptToolCall("write_file", `{"phase_id":"requirements","content":"body"}`, nil)

	if spy.docUpdateCount() != 0 {
		t.Fatalf("docUpdateCount = %d, want 0", spy.docUpdateCount())
	}
}

func TestSteeringWorkflow_TextOutputUsesPhaseOrder(t *testing.T) {
	spy := newSpyWorkflowEvents()
	detector := NewSteeringWorkflowDetector("desktop-user")
	detector.detected = true

	text := "# Any Heading\n\n" + stringOfLen("body ", 60)
	emit := func(phaseID, content string) {
		spy.recordDocUpdate("desktop-user", phaseID, content)
	}

	detector.interceptTextOutput(text, emit)
	detector.interceptTextOutput(text+"design", emit)
	detector.interceptTextOutput(text+"tasks", emit)
	detector.interceptTextOutput(text+"ignored", emit)

	got := spy.docUpdates()
	wantPhases := []string{"requirements", "design", "tasks"}
	if len(got) != len(wantPhases) {
		t.Fatalf("doc updates = %#v, want phases %v", got, wantPhases)
	}
	for i, phase := range wantPhases {
		if got[i].PhaseID != phase {
			t.Fatalf("docUpdates[%d].PhaseID = %q, want %q", i, got[i].PhaseID, phase)
		}
	}
}

func TestSteeringWorkflow_InactiveDetectorDoesNotEmit(t *testing.T) {
	spy := newSpyWorkflowEvents()
	detector := NewSteeringWorkflowDetector("desktop-user")
	detector.detected = false

	detector.interceptToolCall("write_file", `{"phase_id":"requirements","content":"body"}`, func(phaseID, content string) {
		spy.recordDocUpdate("desktop-user", phaseID, content)
	})
	detector.interceptTextOutput("# Heading\n\n"+stringOfLen("body ", 60), func(phaseID, content string) {
		spy.recordDocUpdate("desktop-user", phaseID, content)
	})

	if spy.docUpdateCount() != 0 {
		t.Fatalf("docUpdateCount = %d, want 0", spy.docUpdateCount())
	}
}

func stringOfLen(seed string, repeat int) string {
	out := ""
	for i := 0; i < repeat; i++ {
		out += seed
	}
	return out
}
