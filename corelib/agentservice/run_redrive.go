package agentservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// drivenRun is one paused run whose loop died with the process. Resume
// starts this request on the same run id; it does not admit a second run.
type drivenRun struct {
	principal  Principal
	instanceID string
	sessionID  string
	run        Run
	req        ExecuteRequest
}

// prepareDrivenRun loads the run's own message and session so a later start
// can execute it. It does not start anything. A missing executor, message,
// or session is an error: the caller must not report the run as resumed.
func (s *Service) prepareDrivenRun(ctx context.Context, p Principal, instanceID string, run Run) (*drivenRun, error) {
	if s == nil || (s.executor == nil && s.runtime == nil) {
		return nil, fmt.Errorf("paused run has no executor to continue")
	}
	tenant, err := s.store.GetTenant(p.TenantID)
	if err != nil {
		return nil, err
	}
	user, err := s.store.GetUser(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	inst = s.withInstanceReadiness(inst)
	if !inst.Ready {
		return nil, fmt.Errorf("instance is not ready: %s", inst.ReadyReason)
	}
	sess, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, run.SessionID)
	if err != nil {
		return nil, err
	}
	msg, err := s.store.GetMessage(run.SessionID, run.UserMessageID)
	if err != nil {
		return nil, fmt.Errorf("paused run has no message to continue: %w", err)
	}
	cfg, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil && err != ErrUserConfigNotFound {
		return nil, err
	}
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	history, err := s.store.ListMessages(run.SessionID)
	if err != nil {
		return nil, err
	}
	req := ExecuteRequest{
		Principal:  p,
		Tenant:     tenant,
		User:       user,
		Instance:   inst,
		Session:    sess,
		RunID:      run.ID,
		Message:    msg,
		History:    history,
		DataDir:    inst.DataDir,
		Config:     cfg.AppConfig,
		ToolPolicy: toolPolicyFromMetadata(msg.Metadata, sess.Metadata),
	}
	return &drivenRun{principal: p, instanceID: instanceID, sessionID: sess.ID, run: run, req: req}, nil
}

// launchDrivenRun registers the run's cancel and executes it. The cancel is
// registered before this returns, so a resume that follows is a live run.
func (s *Service) launchDrivenRun(ctx context.Context, driven *drivenRun) {
	if driven == nil {
		return
	}
	parent := context.Background()
	if ctx != nil {
		parent = context.WithoutCancel(ctx)
	}
	execCtx, cancel := context.WithCancel(parent)
	s.registerRunCancel(driven.run.ID, cancel)
	go func() {
		defer s.clearRunCancel(driven.run.ID)
		defer cancel()
		if err := s.beginRequest(); err != nil {
			return
		}
		defer s.activeRequests.Done()
		s.finishDrivenRun(execCtx, driven)
	}()
}

func (s *Service) finishDrivenRun(ctx context.Context, driven *drivenRun) {
	run := driven.run
	req := driven.req
	res, execErr := s.executeDriven(ctx, run, req)
	completed := s.now()
	s.reapRunSupplements(run.ID)
	if s.pauses.release(run.ID) {
		s.persistPauses()
	}
	run.CompletedAt = &completed
	if !run.StartedAt.IsZero() {
		run.DurationMs = completed.Sub(run.StartedAt).Milliseconds()
	}
	p := driven.principal
	if execErr != nil {
		if errors.Is(execErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			run.Status = RunStatusCancelled
			run.Error = "run cancelled"
		} else {
			run.Status = RunStatusFailed
			run.Error = execErr.Error()
		}
		event := runTerminalEventFor(run, "run.failed", runFailurePayload(run.Error, execErr))
		if run.Status == RunStatusCancelled {
			event = runTerminalEventFor(run, "run.cancelled", runFailurePayload(run.Error, execErr))
		}
		s.saveRunTerminal(run, event, nil)
		return
	}
	assistant := Message{
		ID: NewID("msg"), SessionID: driven.sessionID, TenantID: p.TenantID, UserID: p.UserID,
		InstanceID: driven.instanceID, Role: MessageRoleAssistant, OutputType: "text/plain",
		Content:  agentruntime.UserFacingText(textutil.SanitizeVisibleChatText(res.Content)),
		Metadata: cloneMap(res.Metadata), CreatedAt: completed,
	}
	run.Status = RunStatusSucceeded
	run.AssistantMessageID = assistant.ID
	run.WaitingForUser = false
	events := []RunEvent{
		runTerminalEventFor(run, "assistant.message", map[string]any{"message_id": assistant.ID}),
		runTerminalEventFor(run, "run.completed", map[string]any{"message_id": assistant.ID}),
	}
	if _, err := s.saveRunCompletionWithEvents(assistant, run, events); err != nil {
		run.Status = RunStatusFailed
		run.Error = err.Error()
		s.saveRunTerminal(run, runTerminalEventFor(run, "run.failed", runFailurePayload(run.Error, err)), nil)
	}
}

func (s *Service) executeDriven(ctx context.Context, run Run, req ExecuteRequest) (*ExecuteResult, error) {
	if runtime := s.Runtime(); runtime != nil {
		runtimeResult, runtimeErr := runtime.Execute(ctx, agentruntime.TurnRequest{
			Scope: agentruntime.Scope{
				TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID,
				SessionID: run.SessionID, RunID: run.ID,
			},
			Input:  req,
			Host:   s.runtimeHostCapabilities(),
			Events: serviceRuntimeEventSink{service: s, run: run},
		})
		if runtimeResult.Output != nil {
			if res, ok := runtimeResult.Output.(*ExecuteResult); ok {
				return res, runtimeErr
			}
			if value, ok := runtimeResult.Output.(ExecuteResult); ok {
				return &value, runtimeErr
			}
		}
		if runtimeErr != nil {
			return nil, runtimeErr
		}
		return nil, fmt.Errorf("agent runtime returned no result")
	}
	if s.executor == nil {
		return nil, fmt.Errorf("paused run has no executor to continue")
	}
	return s.executor.Execute(ctx, req)
}
