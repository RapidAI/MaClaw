package main

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	traceEventLimit    = 128
	traceEvidenceLimit = 96
)

// generateID produces a unique ID for trace jobs and runs.
func generateID() string {
	var buf [2]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%d-%04x", time.Now().UnixNano(), int(buf[0])<<8|int(buf[1]))
}

type AITraceService struct {
	mu       sync.RWMutex
	jobs     map[string]*TraceJob
	runs     map[string]*TraceRun
	events   map[string][]TraceEvent
	evidence map[string][]EvidenceRecord
}

func NewAITraceService() *AITraceService {
	return &AITraceService{
		jobs:     map[string]*TraceJob{},
		runs:     map[string]*TraceRun{},
		events:   map[string][]TraceEvent{},
		evidence: map[string][]EvidenceRecord{},
	}
}

func (s *AITraceService) StartJobRun(kind TraceJobKind, title, source, userID, projectPath string) (*TraceJob, *TraceRun) {
	now := traceNowMillis()
	jobID := "job-" + generateID()
	runID := "run-" + generateID()
	job := &TraceJob{
		JobID:       jobID,
		Kind:        kind,
		Title:       truncateTraceText(title, 160),
		Source:      source,
		UserID:      userID,
		ProjectPath: projectPath,
		Status:      TraceRunStatusStarting,
		LatestRunID: runID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	run := &TraceRun{
		RunID:       runID,
		JobID:       jobID,
		Kind:        kind,
		Title:       job.Title,
		Source:      source,
		UserID:      userID,
		ProjectPath: projectPath,
		Status:      TraceRunStatusStarting,
		StartedAt:   now,
		UpdatedAt:   now,
	}

	s.mu.Lock()
	s.jobs[jobID] = job
	s.runs[runID] = run
	s.mu.Unlock()
	return cloneTraceJob(job), cloneTraceRun(run)
}

func (s *AITraceService) SetRunSessionID(runID, sessionID string) {
	if runID == "" || sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if run := s.runs[runID]; run != nil {
		run.SessionID = sessionID
		run.UpdatedAt = traceNowMillis()
	}
}

func (s *AITraceService) SetRunLoopID(runID, loopID string) {
	if runID == "" || loopID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if run := s.runs[runID]; run != nil {
		run.LoopID = loopID
		run.UpdatedAt = traceNowMillis()
	}
}

func (s *AITraceService) LinkRuns(parentRunID, childRunID string) {
	if parentRunID == "" || childRunID == "" || parentRunID == childRunID {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[parentRunID]
	if run == nil {
		return
	}
	for _, existing := range run.LinkedRunIDs {
		if existing == childRunID {
			return
		}
	}
	run.LinkedRunIDs = append(run.LinkedRunIDs, childRunID)
	run.UpdatedAt = traceNowMillis()
}

func (s *AITraceService) UpdateRun(runID string, status TraceRunStatus, summary, errText string) {
	if runID == "" {
		return
	}
	now := traceNowMillis()
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[runID]
	if run == nil {
		return
	}
	run.Status = status
	if summary != "" {
		run.Summary = truncateTraceText(summary, 400)
	}
	if errText != "" {
		run.Error = truncateTraceText(errText, 400)
	}
	run.UpdatedAt = now
	if isTraceTerminalStatus(status) {
		run.EndedAt = now
	}
	if job := s.jobs[run.JobID]; job != nil {
		job.Status = status
		job.LatestRunID = runID
		job.UpdatedAt = now
	}
}

func (s *AITraceService) AppendEvent(runID string, event TraceEvent) TraceEvent {
	now := traceNowMillis()
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[runID]
	if run == nil {
		return TraceEvent{}
	}
	if event.EventID == "" {
		event.EventID = "trace-event-" + generateID()
	}
	event.RunID = runID
	event.JobID = run.JobID
	if event.ProjectPath == "" {
		event.ProjectPath = run.ProjectPath
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = now
	}
	event.Title = truncateTraceText(sanitizeTraceStoredText(event.Title), 160)
	event.Summary = truncateTraceText(sanitizeTraceStoredText(event.Summary), 400)
	event.RelatedFile = truncateTraceText(event.RelatedFile, 260)
	event.Command = truncateTraceText(event.Command, 260)
	for i := range event.ToolOutcomes {
		event.ToolOutcomes[i].ToolName = truncateTraceText(event.ToolOutcomes[i].ToolName, 80)
		event.ToolOutcomes[i].Outcome = truncateTraceText(event.ToolOutcomes[i].Outcome, 40)
	}
	items := append(s.events[runID], event)
	if len(items) > traceEventLimit {
		items = items[len(items)-traceEventLimit:]
	}
	s.events[runID] = items
	run.EventCount = len(items)
	run.UpdatedAt = now
	if event.Summary != "" {
		run.Summary = event.Summary
	}
	if job := s.jobs[run.JobID]; job != nil {
		job.UpdatedAt = now
	}
	return event
}

func (s *AITraceService) AppendEvidence(runID string, record EvidenceRecord) EvidenceRecord {
	now := traceNowMillis()
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[runID]
	if run == nil {
		return EvidenceRecord{}
	}
	if record.EvidenceID == "" {
		record.EvidenceID = "trace-evidence-" + generateID()
	}
	record.RunID = runID
	record.JobID = run.JobID
	if record.ProjectPath == "" {
		record.ProjectPath = run.ProjectPath
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = now
	}
	record.Summary = truncateTraceText(sanitizeTraceStoredText(record.Summary), 200)
	record.ContentSnippet = truncateTraceText(sanitizeTraceStoredText(record.ContentSnippet), 600)
	record.RelatedFile = truncateTraceText(record.RelatedFile, 260)
	record.Command = truncateTraceText(record.Command, 260)
	items := append(s.evidence[runID], record)
	if len(items) > traceEvidenceLimit {
		items = items[len(items)-traceEvidenceLimit:]
	}
	s.evidence[runID] = items
	run.EvidenceCount = len(items)
	run.UpdatedAt = now
	if record.Summary != "" && run.Summary == "" {
		run.Summary = record.Summary
	}
	if job := s.jobs[run.JobID]; job != nil {
		job.UpdatedAt = now
	}
	return record
}

func (s *AITraceService) ReplaceRun(runID string, events []TraceEvent, evidence []EvidenceRecord) {
	if s == nil || runID == "" {
		return
	}
	now := traceNowMillis()
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[runID]
	if run == nil {
		return
	}
	if events != nil {
		for i := range events {
			events[i].Title = truncateTraceText(sanitizeTraceStoredText(events[i].Title), 160)
			events[i].Summary = truncateTraceText(sanitizeTraceStoredText(events[i].Summary), 400)
		}
		s.events[runID] = append([]TraceEvent(nil), events...)
		run.EventCount = len(events)
	}
	if evidence != nil {
		for i := range evidence {
			evidence[i].Summary = truncateTraceText(sanitizeTraceStoredText(evidence[i].Summary), 200)
			evidence[i].ContentSnippet = truncateTraceText(sanitizeTraceStoredText(evidence[i].ContentSnippet), 600)
		}
		s.evidence[runID] = append([]EvidenceRecord(nil), evidence...)
		run.EvidenceCount = len(evidence)
	}
	run.UpdatedAt = now
	if job := s.jobs[run.JobID]; job != nil {
		job.UpdatedAt = now
	}
}

func (s *AITraceService) GetTrace(runID string) (AIAssistantTraceView, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run := s.runs[runID]
	if run == nil {
		return AIAssistantTraceView{}, false
	}
	events := append([]TraceEvent(nil), s.events[runID]...)
	evidence := append([]EvidenceRecord(nil), s.evidence[runID]...)
	view := AIAssistantTraceView{
		JobID:               run.JobID,
		RunID:               run.RunID,
		Kind:                run.Kind,
		Title:               run.Title,
		Source:              run.Source,
		UserID:              run.UserID,
		ProjectPath:         run.ProjectPath,
		SessionID:           run.SessionID,
		LoopID:              run.LoopID,
		LinkedRunIDs:        append([]string(nil), run.LinkedRunIDs...),
		Status:              run.Status,
		Summary:             run.Summary,
		Error:               run.Error,
		StartedAt:           run.StartedAt,
		UpdatedAt:           run.UpdatedAt,
		EndedAt:             run.EndedAt,
		EventCount:          run.EventCount,
		EvidenceCount:       run.EvidenceCount,
		TrialReflectSummary: buildTrialReflectSummary(run, events, evidence),
		Events:              events,
		Evidence:            evidence,
	}
	return view, true
}

func (s *AITraceService) TraceCounts(runID string) (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run := s.runs[runID]
	if run == nil {
		return 0, 0
	}
	return run.EventCount, run.EvidenceCount
}

func (s *AITraceService) TraceSummary(runID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run := s.runs[runID]
	if run == nil {
		return ""
	}
	if run.Summary != "" {
		return run.Summary
	}
	items := s.events[runID]
	if len(items) > 0 {
		last := items[len(items)-1]
		if last.Summary != "" {
			return last.Summary
		}
		return last.Title
	}
	return ""
}

func (s *AITraceService) RecallEvidence(projectPath, query string, limit int) []EvidenceRecord {
	if limit <= 0 {
		limit = 3
	}
	query = strings.TrimSpace(strings.ToLower(query))
	tokens := strings.Fields(query)
	type scoredEvidence struct {
		record EvidenceRecord
		score  int
	}
	var scored []scoredEvidence

	s.mu.RLock()
	for _, items := range s.evidence {
		for _, item := range items {
			score := traceEvidenceScore(item, projectPath, query, tokens)
			if score <= 0 {
				continue
			}
			scored = append(scored, scoredEvidence{record: item, score: score})
		}
	}
	s.mu.RUnlock()

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].record.CreatedAt > scored[j].record.CreatedAt
		}
		return scored[i].score > scored[j].score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]EvidenceRecord, len(scored))
	for i, item := range scored {
		out[i] = item.record
	}
	return out
}

func (s *AITraceService) MustGetRun(runID string) *TraceRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if run := s.runs[runID]; run != nil {
		return cloneTraceRun(run)
	}
	return nil
}

func cloneTraceJob(job *TraceJob) *TraceJob {
	if job == nil {
		return nil
	}
	clone := *job
	return &clone
}

func cloneTraceRun(run *TraceRun) *TraceRun {
	if run == nil {
		return nil
	}
	clone := *run
	clone.LinkedRunIDs = append([]string(nil), run.LinkedRunIDs...)
	return &clone
}

func buildTrialReflectSummary(run *TraceRun, events []TraceEvent, evidence []EvidenceRecord) *TrialReflectSummary {
	if run == nil {
		return nil
	}
	attemptedTools := make([]string, 0, 4)
	seenTools := map[string]struct{}{}
	failureCategories := make([]string, 0, 4)
	seenCategories := map[string]struct{}{}
	attemptCount := 0
	failureCount := 0
	sawSuccess := false
	sawFailure := false
	repeatGuardTriggered := false
	for _, event := range events {
		if normalizeTraceEventKind(event.Kind) != traceEventKindTrialObserved {
			continue
		}
		if len(event.ToolOutcomes) > 0 {
			attemptCount += len(event.ToolOutcomes)
			for _, observed := range event.ToolOutcomes {
				tool := strings.TrimSpace(observed.ToolName)
				if tool != "" {
					if _, ok := seenTools[tool]; !ok {
						seenTools[tool] = struct{}{}
						attemptedTools = append(attemptedTools, tool)
					}
				}
				switch observed.Outcome {
				case toolOutcomeFailed.String():
					failureCount++
					sawFailure = true
				case toolOutcomeSucceeded.String():
					sawSuccess = true
				}
			}
		} else {
			attemptCount++
			for _, tool := range traceToolNamesFromText(event.Command + " " + event.Summary) {
				if _, ok := seenTools[tool]; ok {
					continue
				}
				seenTools[tool] = struct{}{}
				attemptedTools = append(attemptedTools, tool)
			}
		}
	}
	for _, item := range evidence {
		if normalizeTraceEvidenceCategory(item.Category) == traceEvidenceCategoryRepeatGuard {
			repeatGuardTriggered = true
		}
		if normalizeTraceSourceKind(item.SourceKind) == traceSourceKindAdaptiveRetry && item.Category != "" {
			if _, ok := seenCategories[item.Category]; !ok {
				seenCategories[item.Category] = struct{}{}
				failureCategories = append(failureCategories, item.Category)
			}
		}
		for _, tool := range traceToolNamesFromText(item.Command + " " + item.ContentSnippet) {
			if _, ok := seenTools[tool]; ok {
				continue
			}
			seenTools[tool] = struct{}{}
			attemptedTools = append(attemptedTools, tool)
		}
	}
	if attemptCount == 0 && len(failureCategories) == 0 && !repeatGuardTriggered {
		return nil
	}
	recovered := sawFailure && sawSuccess
	finalOutcome := classifyTrialReflectFinalOutcome(run.Status, sawFailure, sawSuccess)
	strategyParts := make([]string, 0, 4)
	if len(attemptedTools) > 0 {
		strategyParts = append(strategyParts, "tools="+strings.Join(attemptedTools, ", "))
	}
	if failureCount > 0 {
		strategyParts = append(strategyParts, "failures="+fmt.Sprintf("%d", failureCount))
	}
	if len(failureCategories) > 0 {
		strategyParts = append(strategyParts, "categories="+strings.Join(failureCategories, ", "))
	}
	if repeatGuardTriggered {
		strategyParts = append(strategyParts, "repeat guard avoided duplicate failed actions")
	}
	if recovered {
		strategyParts = append(strategyParts, "recovered after failure")
	}
	if len(strategyParts) == 0 {
		strategyParts = append(strategyParts, "trial-reflect summary available")
	}
	return &TrialReflectSummary{
		AttemptCount:      attemptCount,
		AttemptedTools:    attemptedTools,
		FailureCount:      failureCount,
		FailureCategories: failureCategories,
		RepeatGuard:       repeatGuardTriggered,
		Recovered:         recovered,
		FinalOutcome:      finalOutcome.String(),
		StrategyNote:      truncateTraceText(strings.Join(strategyParts, "; "), 220),
	}
}

func traceToolNamesFromText(text string) []string {
	lower := strings.ToLower(text)
	tools := make([]string, 0, 3)
	seen := map[string]struct{}{}
	for _, name := range []string{"bash", "read_file", "write_file", "edit_file", "search_code", "grep", "glob"} {
		if !strings.Contains(lower, name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		tools = append(tools, name)
	}
	return tools
}

func traceEvidenceScore(item EvidenceRecord, projectPath, query string, tokens []string) int {
	text := strings.ToLower(strings.Join([]string{item.Summary, item.ContentSnippet, item.RelatedFile, item.Command, item.Category}, " "))
	score := 0
	if projectPath != "" && item.ProjectPath == projectPath {
		score += 50
	}
	switch normalizeTraceEvidenceCategory(item.Category) {
	case traceEvidenceCategoryError:
		score += 20
	case traceEvidenceCategoryResult:
		score += 16
	case traceEvidenceCategoryFile, traceEvidenceCategoryDecision:
		score += 12
	default:
		score += 4
	}
	if query != "" && strings.Contains(text, query) {
		score += 25
	}
	for _, token := range tokens {
		if len(token) < 2 {
			continue
		}
		if strings.Contains(text, token) {
			score += 5
		}
	}
	if item.CreatedAt > 0 {
		score += 1
	}
	return score
}

func truncateTraceText(text string, limit int) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}

func sanitizeTraceStoredText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if !strings.Contains(text, "Browser") && !strings.Contains(text, "Tool") {
		return text
	}
	loc := rolePrefixReasoningRe.FindStringIndex(text)
	if loc == nil {
		return text
	}
	return strings.TrimSpace(text[:loc[0]])
}
