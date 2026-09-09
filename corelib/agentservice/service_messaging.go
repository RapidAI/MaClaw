package agentservice

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
	"strings"
	"sync"
	"time"
)

const maxMessageAttachments = coreim.ThirdPartyMaxAttachments

func (s *Service) SendMessage(ctx context.Context, p Principal, instanceID string, in SendMessageInput) (*Session, *Run, *Message, error) {
	if err := s.beginRequest(); err != nil {
		return nil, nil, nil, err
	}
	defer s.activeRequests.Done()
	sess, err := s.resolveSendSession(ctx, p, instanceID, in)
	if err != nil {
		return nil, nil, nil, err
	}
	clientMessageID := strings.TrimSpace(in.ClientMessageID)
	metadata := cloneMap(in.Metadata)
	if clientMessageID != "" {
		if metadata == nil {
			metadata = map[string]string{}
		}
		metadata["client_message_id"] = clientMessageID
	}
	if preset := strings.TrimSpace(in.MoAPreset); preset != "" {
		if metadata == nil {
			metadata = map[string]string{}
		}
		if strings.TrimSpace(metadata["moa_preset"]) == "" {
			metadata["moa_preset"] = preset
		}
	}
	run, msg, err := s.PostMessage(ctx, p, instanceID, sess.ID, PostMessageInput{Content: in.Content, InputType: in.InputType, Attachments: in.Attachments, Metadata: metadata, ClientCapabilities: in.ClientCapabilities, ContinuationHandle: in.ContinuationHandle, RefineTask: in.RefineTask, OnToken: in.OnToken, DatabaseApproval: cloneDatabaseApproval(in.DatabaseApproval)})
	if err != nil {
		return sess, run, msg, err
	}
	updatedSess, getErr := s.GetSession(ctx, p, instanceID, sess.ID)
	if getErr == nil {
		sess = updatedSess
	}
	return sess, run, msg, nil
}

// SendMessageAsync is the session-resolving counterpart of PostMessageAsync
// used by the convenience /instances/{instanceId}/messages endpoint.
func (s *Service) SendMessageAsync(ctx context.Context, p Principal, instanceID string, in SendMessageInput) (*Session, *Run, error) {
	if err := s.beginRequest(); err != nil {
		return nil, nil, err
	}
	defer s.activeRequests.Done()
	sess, err := s.resolveSendSession(ctx, p, instanceID, in)
	if err != nil {
		return nil, nil, err
	}
	clientMessageID := strings.TrimSpace(in.ClientMessageID)
	metadata := cloneMap(in.Metadata)
	if clientMessageID != "" {
		if metadata == nil {
			metadata = map[string]string{}
		}
		metadata["client_message_id"] = clientMessageID
	}
	if preset := strings.TrimSpace(in.MoAPreset); preset != "" {
		if metadata == nil {
			metadata = map[string]string{}
		}
		if strings.TrimSpace(metadata["moa_preset"]) == "" {
			metadata["moa_preset"] = preset
		}
	}
	run, err := s.PostMessageAsync(ctx, p, instanceID, sess.ID, PostMessageInput{
		Content:            in.Content,
		InputType:          in.InputType,
		Attachments:        in.Attachments,
		Metadata:           metadata,
		ClientCapabilities: in.ClientCapabilities,
		ContinuationHandle: in.ContinuationHandle,
		RefineTask:         in.RefineTask,
		OnToken:            in.OnToken,
		DatabaseApproval:   cloneDatabaseApproval(in.DatabaseApproval),
	})
	if err != nil {
		return sess, run, err
	}
	updatedSess, getErr := s.GetSession(ctx, p, instanceID, sess.ID)
	if getErr == nil {
		sess = updatedSess
	}
	return sess, run, nil
}

func (s *Service) resolveSendSession(ctx context.Context, p Principal, instanceID string, in SendMessageInput) (*Session, error) {
	if strings.TrimSpace(in.SessionID) != "" {
		sess, err := s.GetSession(ctx, p, instanceID, in.SessionID)
		if err != nil {
			return nil, err
		}
		if sess.Archived {
			return nil, ErrSessionArchived
		}
		return sess, nil
	}
	clientSessionKey := strings.TrimSpace(in.ClientSessionKey)
	if clientSessionKey != "" {
		release := s.lockIdempotency(idempotencyKey("session", p.TenantID, p.UserID, instanceID, clientSessionKey))
		defer release()
		if sess, ok := s.findSessionByClientKey(p, instanceID, clientSessionKey); ok {
			if sess.Archived {
				return nil, ErrSessionArchived
			}
			return &sess, nil
		}
	}
	metadata := cloneMap(in.SessionMetadata)
	if clientSessionKey != "" {
		if metadata == nil {
			metadata = map[string]string{}
		}
		metadata["client_session_key"] = clientSessionKey
	}
	sess, err := s.CreateSession(ctx, p, instanceID, CreateSessionInput{AgentID: in.AgentID, Title: in.Title, Metadata: metadata})
	if err != nil && clientSessionKey != "" && errors.Is(err, ErrAlreadyExists) {
		// A repository-level unique constraint may win a race across service
		// processes before this process's lookup sees the committed row. Replay
		// the canonical session instead of surfacing a transient duplicate.
		if existing, ok := s.findSessionByClientKey(p, instanceID, clientSessionKey); ok {
			if existing.Archived {
				return nil, ErrSessionArchived
			}
			return &existing, nil
		}
	}
	return sess, err
}

func (s *Service) findSessionByClientKey(p Principal, instanceID, clientSessionKey string) (Session, bool) {
	items, err := s.store.ListSessions(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return Session{}, false
	}
	for _, sess := range items {
		if sess.Metadata != nil && sess.Metadata["client_session_key"] == clientSessionKey {
			return sess, true
		}
	}
	return Session{}, false
}

func (s *Service) findExistingClientMessage(sessionID string, p Principal, instanceID, clientMessageID string) (Run, *Message, bool) {
	messages, err := s.store.ListMessages(sessionID)
	if err != nil {
		return Run{}, nil, false
	}
	var userMessageID string
	for _, msg := range messages {
		if msg.Role == MessageRoleUser && msg.Metadata != nil && msg.Metadata["client_message_id"] == clientMessageID {
			userMessageID = msg.ID
			break
		}
	}
	if userMessageID == "" {
		return Run{}, nil, false
	}
	run, err := s.store.GetRunByUserMessageID(p.TenantID, p.UserID, instanceID, userMessageID)
	if err != nil {
		return Run{}, nil, false
	}
	var assistant *Message
	if run.AssistantMessageID != "" {
		for _, msg := range messages {
			if msg.ID == run.AssistantMessageID {
				copy := msg
				assistant = &copy
				break
			}
		}
	}
	return run, assistant, true
}

func (s *Service) PostMessage(ctx context.Context, p Principal, instanceID, sessionID string, in PostMessageInput) (*Run, *Message, error) {
	requestStarted := time.Now()
	if err := s.beginRequest(); err != nil {
		return nil, nil, err
	}
	defer s.activeRequests.Done()
	// Metrics are attached to the admitted run rather than the HTTP request:
	// idempotent replays and validation failures must not inflate execution
	// counters. terminalStatus is set on every post-admission return path.
	var admitted bool
	terminalStatus := "failed"
	defer func() {
		if admitted {
			s.RuntimeMetrics().RecordTurnFinished(p.TenantID, terminalStatus)
		}
	}()
	tenant, err := s.store.GetTenant(p.TenantID)
	if err != nil {
		return nil, nil, err
	}
	user, err := s.store.GetUser(p.TenantID, p.UserID)
	if err != nil {
		return nil, nil, err
	}
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, nil, err
	}
	inst = s.withInstanceReadiness(inst)
	if !inst.Ready {
		return nil, nil, fmt.Errorf("instance is not ready: %s", inst.ReadyReason)
	}
	sess, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if sess.Archived {
		return nil, nil, ErrSessionArchived
	}
	// Idempotency-Key/client_message_id is enforced inside the service boundary
	// so HTTP, IM and GUI callers share the same atomic behavior. Synchronous
	// calls retain the keyed lock through execution; asynchronous admission
	// releases it once the durable run exists so retries can replay immediately.
	clientMessageID := ""
	if in.Metadata != nil {
		clientMessageID = strings.TrimSpace(in.Metadata["client_message_id"])
	}
	var releaseIdempotency func()
	if clientMessageID != "" {
		releaseIdempotency = s.lockIdempotency(idempotencyKey("message", p.TenantID, p.UserID, instanceID, sess.ID, clientMessageID))
		// Async admission only needs serialization through durable run creation;
		// keeping this lock while waiting for the model would make retries block
		// behind the entire execution. The nil check lets the deferred cleanup
		// remain safe after the early release below.
		defer func() {
			if releaseIdempotency != nil {
				releaseIdempotency()
			}
		}()
		if run, msg, ok := s.findExistingClientMessage(sess.ID, p, instanceID, clientMessageID); ok {
			if in.onRunCreated != nil {
				in.onRunCreated(run)
				if in.asyncAdmission {
					releaseIdempotency()
					releaseIdempotency = nil
				}
			}
			return &run, msg, nil
		}
	}
	cfg, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil && err != ErrUserConfigNotFound {
		return nil, nil, err
	}
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	content := strings.TrimSpace(in.Content)
	if content == "" && len(in.Attachments) == 0 {
		return nil, nil, fmt.Errorf("content is required")
	}
	attachments := CanonicalizeReviewedHostMessageAttachments(in.Attachments)
	if err := validateMessageAttachments(attachments); err != nil {
		return nil, nil, err
	}
	if err := s.enforceRuntimeRateLimit(ctx, p.TenantID); err != nil {
		return nil, nil, err
	}
	if err := s.enforceQuotaLimit(p.TenantID, p.UserID, quotaMetricMessages); err != nil {
		return nil, nil, err
	}
	if err := s.enforceQuotaLimit(p.TenantID, p.UserID, quotaMetricRuns); err != nil {
		return nil, nil, err
	}
	var taskRelation *TaskRelationDecision
	if handleID := strings.TrimSpace(in.ContinuationHandle); handleID != "" {
		var decision TaskRelationDecision
		if in.RefineTask {
			decision, err = s.refineTaskRelation(handleID, p, sess, content)
			if err != nil {
				return nil, nil, err
			}
		} else {
			decision, err = s.consumeTaskContinuationHandle(handleID, p, sess)
			if err != nil {
				return nil, nil, err
			}
		}
		taskRelation = &decision
	} else if in.RefineTask {
		// An amendment command always refines one explicit, current task. It
		// cannot nominate a root by itself, so require the one-time continuation
		// selector that the user chose alongside it.
		return nil, nil, fmt.Errorf("task refinement requires continuation handle")
	}
	effectiveContent, pendingAskReq := buildEffectiveUserContent(sess, content)
	now := s.now()
	userMsg := Message{ID: NewID("msg"), SessionID: sess.ID, TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, Role: MessageRoleUser, InputType: defaultString(in.InputType, "text/plain"), Content: content, Attachments: attachments, Metadata: cloneMap(in.Metadata), CreatedAt: now}
	run := Run{ID: NewID("run"), TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, SessionID: sess.ID, UserMessageID: userMsg.ID, Status: RunStatusRunning, StartedAt: now, Metadata: correlationMetadata(ctx)}
	// Preserve the inbound parent span for compatibility while assigning a
	// distinct deterministic child span to the durable run. The child id is
	// stable for this run and can therefore link events/audits across process
	// boundaries without requiring an in-process exporter.
	if traceID := agentruntime.TraceID(ctx); traceID != "" {
		parentSpanID := agentruntime.SpanID(ctx)
		if parentSpanID != "" {
			run.Metadata["parent_span_id"] = parentSpanID
		}
		seed := parentSpanID
		if seed == "" {
			seed = agentruntime.CorrelationID(ctx)
		}
		runSpanID := agentruntime.DeriveChildSpanID(seed, run.ID)
		if run.Metadata == nil {
			run.Metadata = make(map[string]string)
		}
		run.Metadata["run_span_id"] = runSpanID
		ctx = agentruntime.WithRunSpanID(ctx, runSpanID)
	}
	admissionEvent := runTerminalEventFor(run, "run.started", map[string]any{"session_id": sess.ID, "message_id": userMsg.ID})
	admissionEventCommitted, err := s.saveRunAdmissionWithEvents(userMsg, run, []RunEvent{admissionEvent})
	if err != nil {
		if clientMessageID != "" && errors.Is(err, ErrAlreadyExists) {
			// A durable unique constraint can win a cross-process retry race
			// after the initial lookup. Return the canonical run/message pair,
			// preserving the same idempotent replay contract as the keyed lock.
			if existingRun, existingMsg, ok := s.findExistingClientMessage(sess.ID, p, instanceID, clientMessageID); ok {
				if in.onRunCreated != nil {
					in.onRunCreated(existingRun)
				}
				return &existingRun, existingMsg, nil
			}
		}
		return nil, nil, err
	}
	admitted = true
	// Queue wait is measured from request ingress to durable admission. For
	// asynchronous callers this captures scheduler/storage back-pressure while
	// remaining meaningful for synchronous GUI and TUI turns.
	s.RuntimeMetrics().RecordTurnAdmitted(p.TenantID, time.Since(requestStarted))
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "message.posted", ResourceType: "message", ResourceID: userMsg.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: mergeCorrelationMetadata(ctx, map[string]string{"instance_id": instanceID, "session_id": sessionID, "role": string(userMsg.Role)})})
	if !admissionEventCommitted {
		s.emitRunEvent(run, admissionEvent.Type, admissionEvent.Payload)
	}
	if in.onRunCreated != nil {
		in.onRunCreated(run)
		if in.asyncAdmission && releaseIdempotency != nil {
			// Release immediately after durable admission so retries can observe
			// this run while its executor continues in the background.
			releaseIdempotency()
			releaseIdempotency = nil
		}
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.started", ResourceType: "run", ResourceID: run.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: mergeCorrelationMetadata(ctx, map[string]string{"instance_id": instanceID, "session_id": sessionID})})
	history, histErr := s.store.ListMessages(sess.ID)
	if histErr != nil {
		// The run may already have been returned to an async caller. Persist a
		// terminal failure instead of leaving an admitted run stuck in running
		// when history preparation fails after admission.
		completed := s.now()
		run.Status = RunStatusFailed
		run.Error = histErr.Error()
		run.CompletedAt = &completed
		run.DurationMs = completed.Sub(run.StartedAt).Milliseconds()
		terminalStatus = "failed"
		failedEvent := runTerminalEventFor(run, "run.failed", map[string]any{"error": run.Error})
		committed, terminalErr := s.saveRunTerminalWithEvents(run, []RunEvent{failedEvent})
		if terminalErr != nil {
			// Transaction failure means the repository could not make the
			// terminal state authoritative. Preserve the legacy best-effort
			// fallback so callers do not observe an indefinitely running run.
			_ = s.store.SaveRun(run)
			committed = false
		}
		if !committed {
			s.emitRunEvent(run, failedEvent.Type, failedEvent.Payload)
		}
		return &run, nil, histErr
	}
	execMsg := userMsg
	execMsg.Content = effectiveContent
	execCtx, cancelExec := context.WithCancel(ctx)
	execReq := ExecuteRequest{
		Principal:          p,
		Tenant:             tenant,
		User:               user,
		Instance:           inst,
		Session:            sess,
		Message:            execMsg,
		History:            history,
		DataDir:            inst.DataDir,
		Config:             cfg.AppConfig,
		ClientCapabilities: in.ClientCapabilities,
		DatabaseApproval:   cloneDatabaseApproval(in.DatabaseApproval),
		TaskRelation:       taskRelation,
		ToolPolicy:         toolPolicyFromMetadata(userMsg.Metadata, sess.Metadata),
		MutationScope: mutationScopeFromMetadata(
			userMsg.Metadata,
			sess.Metadata,
		),
		OpsApprovedCommands: opsApprovedCommandsFromMetadata(
			userMsg.Metadata,
			sess.Metadata,
		),
		MoAPreset: moaPresetFromMetadata(userMsg.Metadata, sess.Metadata),
	}
	var firstTokenMu sync.Mutex
	firstTokenObserved := false
	execReq.OnToken = func(delta string) {
		first := false
		firstTokenMu.Lock()
		if !firstTokenObserved {
			firstTokenObserved = true
			first = true
		}
		firstTokenMu.Unlock()
		s.RuntimeMetrics().RecordToken(p.TenantID, time.Since(run.StartedAt), first)
		if in.OnToken != nil {
			in.OnToken(delta)
		}
	}
	// One cancel registration owns both boundaries. The normal request context
	// stops live execution; a CoreAgentExecutor additionally closes the durable
	// coding task subtree first. Keep the capability optional so non-coding
	// executors retain their current service contract.
	s.registerRunCancel(run.ID, func() {
		if canceller, supported := s.executor.(interface {
			CancelCodingRuntimeTask(ExecuteRequest) (bool, error)
		}); supported {
			_, _ = canceller.CancelCodingRuntimeTask(execReq)
		}
		cancelExec()
	})
	var res *ExecuteResult
	var execErr error
	if runtime := s.Runtime(); runtime != nil {
		runtimeResult, runtimeErr := runtime.Execute(execCtx, agentruntime.TurnRequest{
			Scope:  agentruntime.Scope{TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, SessionID: sess.ID, RunID: run.ID},
			Input:  execReq,
			Host:   s.runtimeHostCapabilities(),
			Events: serviceRuntimeEventSink{service: s, run: run},
		})
		execErr = runtimeErr
		if execErr == nil && runtimeResult.Output != nil {
			var ok bool
			res, ok = runtimeResult.Output.(*ExecuteResult)
			if !ok {
				if value, valueOK := runtimeResult.Output.(ExecuteResult); valueOK {
					res = &value
				} else {
					execErr = fmt.Errorf("agent runtime returned unexpected result type %T", runtimeResult.Output)
				}
			}
		}
	} else {
		res, execErr = s.executor.Execute(execCtx, execReq)
	}
	if execErr == nil && res == nil {
		execErr = fmt.Errorf("agent runtime returned nil result")
	}
	s.clearRunCancel(run.ID)
	cancelExec()
	completed := s.now()
	run.CompletedAt = &completed
	run.DurationMs = completed.Sub(run.StartedAt).Milliseconds()
	if execErr != nil {
		if errors.Is(execCtx.Err(), context.Canceled) || errors.Is(execErr, context.Canceled) {
			run.Status = RunStatusCancelled
			terminalStatus = "cancelled"
			run.Error = "run cancelled"
			cancelledEvent := runTerminalEventFor(run, "run.cancelled", map[string]any{"error": run.Error})
			committed, terminalErr := s.saveRunTerminalWithEvents(run, []RunEvent{cancelledEvent})
			if terminalErr != nil {
				_ = s.store.SaveRun(run)
				committed = false
			}
			if !committed {
				s.emitRunEvent(run, cancelledEvent.Type, cancelledEvent.Payload)
			}
			_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.cancelled", ResourceType: "run", ResourceID: run.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: mergeCorrelationMetadata(ctx, map[string]string{"instance_id": instanceID, "session_id": sessionID})})
			return &run, nil, execErr
		}
		run.Status = RunStatusFailed
		terminalStatus = "failed"
		run.Error = execErr.Error()
		failedEvent := runTerminalEventFor(run, "run.failed", map[string]any{"error": run.Error})
		committed, terminalErr := s.saveRunTerminalWithEvents(run, []RunEvent{failedEvent})
		if terminalErr != nil {
			_ = s.store.SaveRun(run)
			committed = false
		}
		if !committed {
			s.emitRunEvent(run, failedEvent.Type, failedEvent.Payload)
		}
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.failed", ResourceType: "run", ResourceID: run.ID, ActorType: "system", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: mergeCorrelationMetadata(ctx, map[string]string{"instance_id": instanceID, "session_id": sessionID})})
		return &run, nil, execErr
	}
	assistant := Message{ID: NewID("msg"), SessionID: sess.ID, TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, Role: MessageRoleAssistant, OutputType: defaultString(res.OutputType, "text/plain"), Content: agentruntime.UserFacingText(textutil.SanitizeVisibleChatText(res.Content)), Metadata: cloneMap(res.Metadata), CreatedAt: completed}
	if s.AssistantMessageMetadataHook != nil {
		for key, value := range s.AssistantMessageMetadataHook(ctx, p, inst, sess, run, assistant, cfg.AppConfig) {
			if assistant.Metadata == nil {
				assistant.Metadata = map[string]string{}
			}
			assistant.Metadata[key] = value
		}
	}
	run.Status = RunStatusSucceeded
	run.AssistantMessageID = assistant.ID
	run.ResponseSource = strings.TrimSpace(assistant.Metadata[metaResponseSource])
	run.WaitingForUser = normalizeResponseSourceKind(run.ResponseSource).IsWaitingForUser()
	terminalEvents := make([]RunEvent, 0, 3)
	if run.WaitingForUser {
		terminalEvents = append(terminalEvents, runTerminalEventFor(run, "ask_user", map[string]any{"response_source": run.ResponseSource}))
	}
	terminalEvents = append(terminalEvents,
		runTerminalEventFor(run, "assistant.message", map[string]any{"message_id": assistant.ID}),
		runTerminalEventFor(run, "run.completed", map[string]any{"message_id": assistant.ID}),
	)
	terminalEventsCommitted, err := s.saveRunCompletionWithEvents(assistant, run, terminalEvents)
	if err != nil {
		run.Status = RunStatusFailed
		terminalStatus = "failed"
		run.Error = err.Error()
		run.CompletedAt = &completed
		run.DurationMs = completed.Sub(run.StartedAt).Milliseconds()
		failedEvent := runTerminalEventFor(run, "run.failed", map[string]any{"error": run.Error})
		committed, terminalErr := s.saveRunTerminalWithEvents(run, []RunEvent{failedEvent})
		if terminalErr != nil {
			_ = s.store.SaveRun(run)
			committed = false
		}
		if !committed {
			s.emitRunEvent(run, failedEvent.Type, failedEvent.Payload)
		}
		return &run, nil, err
	}
	if !terminalEventsCommitted {
		for _, event := range terminalEvents {
			s.emitRunEvent(run, event.Type, event.Payload)
		}
	}
	terminalStatus = "succeeded"
	if pendingAskReq != nil {
		sess.Metadata = clearPendingAskUserMetadata(sess.Metadata)
	}
	if res != nil && res.Metadata != nil && normalizeResponseSourceKind(res.Metadata[metaResponseSource]).IsWaitingForUser() {
		sess.Metadata = setPendingAskUserMetadata(sess.Metadata, res.Metadata)
	}
	sess.UpdatedAt = completed
	_ = s.store.SaveSession(sess)
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.succeeded", ResourceType: "run", ResourceID: run.ID, ActorType: "system", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: mergeCorrelationMetadata(ctx, map[string]string{"instance_id": instanceID, "session_id": sessionID, "assistant_message_id": assistant.ID})})
	return &run, &assistant, nil
}

// enforceRuntimeRateLimit is intentionally placed after idempotency replay
// detection and request validation. Retrying an already admitted message must
// not consume a new token, and malformed requests must not burn tenant
// capacity. The shared Service boundary means GUI, srv, IM and TUI callers
// all observe the same policy.
func (s *Service) enforceRuntimeRateLimit(ctx context.Context, tenantID string) error {
	if s != nil && s.distributedLimiter != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		allowed, retryAfter, err := s.distributedLimiter.Allow(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("distributed runtime rate limiter: %w", err)
		}
		if !allowed {
			if metrics := s.RuntimeMetrics(); metrics != nil {
				metrics.RecordRateLimited(tenantID)
			}
			return &RateLimitError{RetryAfter: retryAfter}
		}
		return nil
	}
	limiter := s.RuntimeRateLimiter()
	if limiter == nil || !limiter.Enabled() {
		return nil
	}
	allowed, retryAfter, evicted := limiter.AllowWithStatus(tenantID, s.now())
	if evicted {
		if metrics := s.RuntimeMetrics(); metrics != nil {
			metrics.RecordRateLimitBucketEvicted()
		}
	}
	if allowed {
		return nil
	}
	if metrics := s.RuntimeMetrics(); metrics != nil {
		metrics.RecordRateLimited(tenantID)
	}
	return &RateLimitError{RetryAfter: retryAfter}
}

// PostMessageAsync admits a message and returns as soon as its durable Run is
// created. Execution continues in a service-owned goroutine and is coordinated
// by the same active-request/run-cancel lifecycle as synchronous requests.
// The caller context only controls how long admission is awaited; it does not
// cancel the admitted run after this method returns.
func (s *Service) PostMessageAsync(ctx context.Context, p Principal, instanceID, sessionID string, in PostMessageInput) (*Run, error) {
	if s == nil {
		return nil, ErrServiceClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCh := make(chan Run, 1)
	doneCh := make(chan error, 1)
	in.asyncAdmission = true
	in.onRunCreated = func(run Run) {
		select {
		case runCh <- run:
		default:
		}
	}
	correlationID := agentruntime.CorrelationID(ctx)
	traceID := agentruntime.TraceID(ctx)
	spanID := agentruntime.SpanID(ctx)
	go func() {
		execCtx := agentruntime.WithCorrelationID(context.Background(), correlationID)
		execCtx = agentruntime.WithTraceID(execCtx, traceID)
		execCtx = agentruntime.WithSpanID(execCtx, spanID)
		_, _, err := s.PostMessage(execCtx, p, instanceID, sessionID, in)
		doneCh <- err
	}()
	// Prefer an already-admitted run even when the caller context was canceled
	// at the same instant; once a run id exists it is the useful retry/poll
	// contract for the caller.
	select {
	case run := <-runCh:
		return &run, nil
	default:
	}
	select {
	case run := <-runCh:
		return &run, nil
	case err := <-doneCh:
		select {
		case run := <-runCh:
			return &run, nil
		default:
		}
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("message execution completed before run admission")
	case <-ctx.Done():
		select {
		case run := <-runCh:
			return &run, nil
		default:
		}
		return nil, ctx.Err()
	}
}

// consumeTaskContinuationHandle is the sole Service ingress that converts a
// transport handle into a trusted semantic relation. It has no fallback to a
// RootTaskID, user text, session metadata, or historic tool surface. A missing
// semantic coordinator is therefore an explicit unavailability result rather
// than permission to replay a potentially mutating task.
func (s *Service) consumeTaskContinuationHandle(handleID string, principal Principal, session Session) (TaskRelationDecision, error) {
	if s == nil {
		return TaskRelationDecision{}, fmt.Errorf("task continuation unavailable")
	}
	s.dynamicSemanticMu.Lock()
	resources := s.dynamicSemantic
	s.dynamicSemanticMu.Unlock()
	if resources == nil {
		return TaskRelationDecision{}, fmt.Errorf("task continuation unavailable")
	}
	resources.mu.Lock()
	coordinator := resources.coordinator
	resources.mu.Unlock()
	if coordinator == nil {
		return TaskRelationDecision{}, fmt.Errorf("task continuation unavailable")
	}
	trustedSessionID := semanticTaskSessionID(principal, session)
	record, err := coordinator.ConsumeTaskContinuationHandle(handleID, principal.TenantID, memoryOwnerIDForPrincipal(principal), trustedSessionID, s.now().UTC())
	if err != nil {
		return TaskRelationDecision{}, fmt.Errorf("task continuation rejected: %w", err)
	}
	return verifiedTaskRelationDecision(TaskRelationDecision{
		Kind:               TaskRelationContinue,
		RootTaskID:         record.RootTaskID,
		ContinuationHandle: record.ID,
		EvidenceIDs:        []string{"task_continuation_handle:" + record.ID},
	}, principal, trustedSessionID), nil
}

// refineTaskRelation atomically converts the explicit, scope-bound
// continuation selector into a server-owned amendment command. The command
// remains active until PublishSurface consumes it atomically with the winning
// child; if a child CAS loses, only this exact opaque selector can retry it.
func (s *Service) refineTaskRelation(handleID string, principal Principal, session Session, amendmentText string) (TaskRelationDecision, error) {
	if s == nil {
		return TaskRelationDecision{}, fmt.Errorf("task refinement unavailable")
	}
	s.dynamicSemanticMu.Lock()
	resources := s.dynamicSemantic
	s.dynamicSemanticMu.Unlock()
	if resources == nil {
		return TaskRelationDecision{}, fmt.Errorf("task refinement unavailable")
	}
	resources.mu.Lock()
	coordinator := resources.coordinator
	resources.mu.Unlock()
	if coordinator == nil {
		return TaskRelationDecision{}, fmt.Errorf("task refinement unavailable")
	}
	amendmentText = strings.TrimSpace(amendmentText)
	if amendmentText == "" {
		return TaskRelationDecision{}, fmt.Errorf("task refinement requires amendment text")
	}
	trustedSessionID := semanticTaskSessionID(principal, session)
	// The digest is computed only after authenticated Service ingress accepted
	// an explicit refine action. The client sees neither the digest nor command
	// ID and cannot nominate a root/revision; PublishSurface will consume this
	// command only if its expected-parent CAS wins.
	digest := coretool.SchemaDigest([]byte("task-amendment/v1\x00" + amendmentText))
	continuation, command, err := coordinator.PrepareTaskRefinement(handleID, principal.TenantID, memoryOwnerIDForPrincipal(principal), trustedSessionID, digest, s.now().UTC())
	if err != nil {
		return TaskRelationDecision{}, fmt.Errorf("task refinement rejected: %w", err)
	}
	return verifiedTaskRelationDecision(TaskRelationDecision{
		Kind:               TaskRelationRefine,
		RootTaskID:         continuation.RootTaskID,
		ContinuationHandle: continuation.ID,
		AmendmentCommandID: command.ID,
		AmendmentDigest:    command.Digest,
		AmendmentRevision:  command.ParentRevision,
		AmendmentFencing:   command.ParentFencingToken,
		EvidenceIDs:        []string{"task_continuation_handle:" + continuation.ID, "task_amendment_command:" + command.ID},
	}, principal, trustedSessionID), nil
}

func semanticTaskSessionID(principal Principal, session Session) string {
	return fmt.Sprintf("srv:%s:%s", strings.TrimSpace(session.ID), strings.TrimSpace(principal.UserID))
}

func validateMessageAttachments(attachments []agent.MessageAttachment) error {
	if len(attachments) > maxMessageAttachments {
		return fmt.Errorf("attachments exceeds %d items", maxMessageAttachments)
	}
	for i, att := range attachments {
		if strings.TrimSpace(att.Data) == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(att.Data)
		if err != nil {
			return fmt.Errorf("attachments[%d].data must be base64", i)
		}
		maxBytes := messageAttachmentMaxBytes(att)
		if len(decoded) > maxBytes {
			return fmt.Errorf("attachments[%d].data exceeds %d bytes", i, maxBytes)
		}
	}
	return nil
}

func messageAttachmentMaxBytes(att agent.MessageAttachment) int {
	if _, ok := ReviewedHostTrustedAudioMIME(att); ok {
		return ReviewedHostAudioTranscribeMaxBytes
	}
	if _, ok := ReviewedHostTrustedDocumentMIME(att.FileName, att.MimeType); ok {
		return int(agent.MaxOfficeReadFileBytes)
	}
	return coreim.ThirdPartyMaxDirectBytes
}

func toolPolicyFromMetadata(messageMetadata, sessionMetadata map[string]string) v2.ToolFilterPolicy {
	for _, metadata := range []map[string]string{messageMetadata, sessionMetadata} {
		policy := v2.ToolFilterPolicy(strings.TrimSpace(metadata["tool_policy"]))
		switch policy {
		case v2.ToolFilterDocOnly, v2.ToolFilterPlanning, v2.ToolFilterFull, v2.ToolFilterOpsControlled:
			return policy
		}
	}
	return v2.ToolFilterNone
}

func mutationScopeFromMetadata(messageMetadata, sessionMetadata map[string]string) v2.MutationScope {
	for _, metadata := range []map[string]string{messageMetadata, sessionMetadata} {
		scope := v2.MutationScope(strings.TrimSpace(metadata["mutation_scope"]))
		switch scope {
		case v2.MutationScopeNone, v2.MutationScopeWorkflowDoc, v2.MutationScopeArtifact, v2.MutationScopeProject, v2.MutationScopeOps:
			return scope
		}
	}
	return v2.MutationScopeUnknown
}

func opsApprovedCommandsFromMetadata(messageMetadata, sessionMetadata map[string]string) []v2.OpsApprovedCommand {
	policyText := strings.TrimSpace(messageMetadata["ops_approved_commands"])
	approvalSources := []map[string]string{messageMetadata}
	if policyText == "" {
		policyText = strings.TrimSpace(sessionMetadata["ops_approved_commands"])
		approvalSources = []map[string]string{messageMetadata, sessionMetadata}
	}
	if policyText == "" {
		return nil
	}
	decision := v2.ExtractOpsRiskDecision(policyText)
	switch decision {
	case v2.OpsRiskDecisionAutoExecute:
		return v2.ExtractOpsApprovedCommands(policyText)
	case v2.OpsRiskDecisionApprovalRequired:
		if !opsExecutionApprovalSatisfies(approvalSources, v2.ExtractOpsApprovalRequirement(policyText)) {
			return nil
		}
		if !opsApprovalDigestMatches(approvalSources, policyText) {
			return nil
		}
		return v2.ExtractOpsApprovedCommands(policyText)
	default:
		// deny / document_only / propose / unknown — do not propagate executable commands
		return nil
	}
}

func opsExecutionApprovalSatisfies(metadataSources []map[string]string, required v2.OpsApprovalRequirement) bool {
	actual := opsExecutionApprovalLevelFromMetadata(metadataSources)
	if required == v2.OpsApprovalRequirementSingle {
		return actual == v2.OpsApprovalRequirementSingle || actual == v2.OpsApprovalRequirementDouble
	}
	if required == v2.OpsApprovalRequirementDouble {
		return actual == v2.OpsApprovalRequirementDouble
	}
	return false
}

func opsExecutionApprovalLevelFromMetadata(metadataSources []map[string]string) v2.OpsApprovalRequirement {
	for _, metadata := range metadataSources {
		level := strings.ToLower(strings.TrimSpace(metadata["ops_execution_approval_level"]))
		switch level {
		case string(v2.OpsApprovalRequirementSingle):
			return v2.OpsApprovalRequirementSingle
		case string(v2.OpsApprovalRequirementDouble):
			return v2.OpsApprovalRequirementDouble
		}
		value := strings.ToLower(strings.TrimSpace(metadata["ops_execution_approved"]))
		switch value {
		case "true", "yes", "approved", "1":
			return v2.OpsApprovalRequirementSingle
		}
	}
	return v2.OpsApprovalRequirementUnknown
}

func opsApprovalDigestMatches(metadataSources []map[string]string, policyText string) bool {
	for _, metadata := range metadataSources {
		expected := strings.TrimSpace(metadata["ops_approval_digest"])
		if expected == "" {
			continue
		}
		if strings.EqualFold(expected, v2.OpsApprovalDigest(policyText)) {
			return true
		}
	}
	return false
}
