package agentservice

import (
	"context"
	"strings"
)

func (s *Service) registerRunCancel(runID string, cancel context.CancelFunc) {
	if strings.TrimSpace(runID) == "" || cancel == nil {
		return
	}
	s.runMu.Lock()
	if s.closed {
		s.runMu.Unlock()
		// Close may have taken its cancellation snapshot just before this run
		// was registered. Cancel immediately so a late admission cannot keep
		// shutdown waiting on an uncancellable executor.
		cancel()
		return
	}
	if s.runningRuns == nil {
		s.runningRuns = map[string]context.CancelFunc{}
	}
	s.runningRuns[runID] = cancel
	s.runMu.Unlock()
}

func (s *Service) takeRunCancel(runID string) (context.CancelFunc, bool) {
	if strings.TrimSpace(runID) == "" {
		return nil, false
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	cancel, ok := s.runningRuns[runID]
	if ok {
		delete(s.runningRuns, runID)
	}
	return cancel, ok
}

func (s *Service) clearRunCancel(runID string) {
	if strings.TrimSpace(runID) == "" {
		return
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	delete(s.runningRuns, runID)
}

func (s *Service) GetRun(ctx context.Context, p Principal, instanceID, runID string) (*Run, error) {
	if err := s.beginRequest(); err != nil {
		return nil, err
	}
	defer s.activeRequests.Done()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	v, err := s.store.GetRun(p.TenantID, p.UserID, instanceID, runID)
	if err != nil {
		return nil, err
	}
	enriched, err := s.enrichRun(v)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) CancelRun(ctx context.Context, p Principal, instanceID, runID string) (*Run, error) {
	if err := s.beginRequest(); err != nil {
		return nil, err
	}
	defer s.activeRequests.Done()
	_ = ctx
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	run, err := s.store.GetRun(p.TenantID, p.UserID, instanceID, runID)
	if err != nil {
		return nil, err
	}
	cancel, ok := s.takeRunCancel(run.ID)
	if !ok || run.Status != RunStatusRunning {
		if run.Status == RunStatusCancelled {
			enriched, enrichErr := s.enrichRun(run)
			if enrichErr != nil {
				return nil, enrichErr
			}
			return &enriched, nil
		}
		return nil, ErrRunNotRunning
	}
	cancel()
	completed := s.now()
	run.Status = RunStatusCancelled
	run.Error = "run cancelled"
	run.CompletedAt = &completed
	run.DurationMs = completed.Sub(run.StartedAt).Milliseconds()
	cancelledEvent := runTerminalEventFor(run, "run.cancelled", map[string]any{"error": run.Error})
	committed, terminalErr := s.saveRunTerminalWithEvents(run, []RunEvent{cancelledEvent})
	if terminalErr != nil {
		// Keep cancellation authoritative even when an optional event
		// transaction is unavailable; the executor will replay the same
		// deterministic event id when it observes context cancellation.
		if saveErr := s.store.SaveRun(run); saveErr != nil {
			return nil, saveErr
		}
		committed = false
		terminalErr = nil
	}
	if !committed {
		s.emitRunEvent(run, cancelledEvent.Type, cancelledEvent.Payload)
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.cancel_requested", ResourceType: "run", ResourceID: run.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": run.SessionID}})
	enriched, err := s.enrichRun(run)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) ListRuns(ctx context.Context, p Principal, instanceID string, in ListRunsInput) ([]Run, error) {
	if err := s.beginRequest(); err != nil {
		return nil, err
	}
	defer s.activeRequests.Done()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	items, err := s.store.ListRuns(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	status := in.Status
	sessionID := strings.TrimSpace(in.SessionID)
	responseSource := strings.TrimSpace(in.ResponseSource)
	filtered := make([]Run, 0, len(items))
	for _, item := range items {
		if status != "" && item.Status != status {
			continue
		}
		if sessionID != "" && item.SessionID != sessionID {
			continue
		}
		if responseSource != "" && strings.TrimSpace(item.ResponseSource) != responseSource {
			continue
		}
		if in.WaitingForUser != nil && item.WaitingForUser != *in.WaitingForUser {
			continue
		}
		filtered = append(filtered, item)
	}
	return s.enrichRuns(filtered)
}

func (s *Service) hasRunningRuns(tenantID, userID, instanceID, sessionID string) (bool, error) {
	runs, err := s.store.ListRuns(tenantID, userID, instanceID)
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		if sessionID != "" && run.SessionID != sessionID {
			continue
		}
		if run.Status == RunStatusRunning {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) collectRunningRunBlockers(tenantID, userID string) ([]DeleteBlocker, error) {
	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return nil, err
	}
	blockers := make([]DeleteBlocker, 0)
	for _, inst := range instances {
		runs, err := s.store.ListRuns(tenantID, userID, inst.ID)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if run.Status != RunStatusRunning {
				continue
			}
			blockers = append(blockers, DeleteBlocker{
				Kind:       "running_run",
				TenantID:   tenantID,
				UserID:     userID,
				InstanceID: inst.ID,
				SessionID:  run.SessionID,
				RunID:      run.ID,
				Reason:     "instance has a running run",
			})
		}
	}
	return blockers, nil
}

func (s *Service) enrichSessions(items []Session) ([]Session, error) {
	out := make([]Session, 0, len(items))
	for _, item := range items {
		enriched, err := s.enrichSession(item)
		if err != nil {
			return nil, err
		}
		out = append(out, enriched)
	}
	return out, nil
}

func (s *Service) enrichSession(sess Session) (Session, error) {
	sess.WaitingForUser = sess.Metadata != nil && sess.Metadata[sessionMetaPendingAskUser] == "true"
	if sess.WaitingForUser {
		sess.PendingAsk = &SessionPendingAsk{
			Question:  strings.TrimSpace(sess.Metadata[sessionMetaPendingAskUserQuestion]),
			InputType: strings.TrimSpace(sess.Metadata[sessionMetaPendingAskUserInputType]),
			Options:   parsePendingAskUserOptions(sess.Metadata[sessionMetaPendingAskUserOptions]),
		}
	}
	messages, err := s.store.ListMessages(sess.ID)
	if err != nil {
		return Session{}, err
	}
	if len(messages) > 0 {
		last := messages[len(messages)-1].CreatedAt
		sess.LastMessageAt = &last
	}
	return sess, nil
}

func (s *Service) enrichRuns(items []Run) ([]Run, error) {
	out := make([]Run, 0, len(items))
	for _, item := range items {
		enriched, err := s.enrichRun(item)
		if err != nil {
			return nil, err
		}
		out = append(out, enriched)
	}
	return out, nil
}

func (s *Service) enrichRun(run Run) (Run, error) {
	if run.CompletedAt != nil {
		run.DurationMs = run.CompletedAt.Sub(run.StartedAt).Milliseconds()
	}
	if strings.TrimSpace(run.AssistantMessageID) == "" {
		return run, nil
	}
	messages, err := s.store.ListMessages(run.SessionID)
	if err != nil {
		return Run{}, err
	}
	for _, msg := range messages {
		if msg.ID != run.AssistantMessageID {
			continue
		}
		if msg.Metadata != nil {
			run.ResponseSource = strings.TrimSpace(msg.Metadata[metaResponseSource])
			run.WaitingForUser = normalizeResponseSourceKind(run.ResponseSource).IsWaitingForUser()
		}
		break
	}
	return run, nil
}
