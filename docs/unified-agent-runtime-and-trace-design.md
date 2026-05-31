# Unified Agent Runtime and Trace Design

## 1. Background

MaClaw has multiple entry points:

- Desktop AI assistant
- IM channels such as Weixin
- Third-party protocol gateways
- Digital employees
- Local AI invited into a discussion
- Background system jobs

These entry points must not each own special workflow logic. After dispatch, every request should enter the same runtime path, use the same state ownership rules, the same policy resolver, and the same trace mechanism.

Recent logs showed tool policy leakage: an IM request was affected by a desktop coding workflow phase (`doc_only`). This happened because workflow state and tool policy were resolved from shared runtime state instead of request-owned context.

Root fix is not channel-specific allowlists. Root fix is explicit ownership for every request, then deriving workflow, policy, lock, and trace from that ownership.

## 2. Goals

1. Use one dispatch pipeline for desktop, IM, third-party protocols, digital employees, local AI, and system jobs.
2. Keep adapters thin: receive external messages, normalize them, and send responses.
3. Remove global active workflow and global current tool policy from agent execution.
4. Make all state ownership explicit and traceable.
5. Make tool authorization explainable: every deny must show which policy layer denied it.
6. Allow collaboration between actors without sharing runtime state.

## 3. Non-Goals

1. No separate business logic for Weixin, desktop, and digital employees.
2. No hardcoded exceptions like "allow tts on Weixin".
3. No reuse of the main assistant's agent loop state by invited local AI.
4. No user workflow policy inheritance for background jobs.

## 4. Core Principle

Separate four concepts:

```text
Ingress channel: where request comes from
Actor: who is executing
Session: conversation/runtime ownership boundary
Task/workflow: optional structured process attached to this request
```

Unified pipeline:

```text
Ingress Adapter
  -> RequestEnvelope normalization
  -> Intent classification
  -> Session and task resolution
  -> AgentRuntime.Dispatch
  -> Workflow attachment decision
  -> ToolPolicy resolution
  -> Tool execution
  -> ResponseEnvelope
  -> Egress Adapter
```

Adapters only translate formats. They must not decide workflow state, tool permission, or agent execution mode.

## 5. Standard Request Envelope

All entry points produce a `RequestEnvelope`.

```go
type RequestEnvelope struct {
    RequestID    string
    Source       SourceRef
    Actor        ActorRef
    Conversation ConversationRef
    Parent       ParentRef
    Payload      MessagePayload
    Debug        DebugOptions
}

type SourceRef struct {
    Channel  string // desktop, im, third_party, discussion, system
    Provider string // local, weixin, lark, telegram, api, scheduler
}

type ActorRef struct {
    ActorID   string // main-ai, local-ai, employee-sales, system
    ActorType string // main_ai, local_ai, digital_employee, tool_agent, system
}

type ConversationRef struct {
    ConversationID string
    SessionKey     string
}

type ParentRef struct {
    ParentRequestID string
    ParentTaskID    string
    HandoffToken    string
}
```

Example session keys:

```text
desktop:local:desktop-user:main
im:weixin:o9cq802UzUN9ln7xyVX8S3V93w5g@im.wechat:direct
im:weixin:room_xxx:group
third_party:api:tenant_123:conversation_456
discussion:gd_123:actor:local-ai
system:scheduler:background:capability-market-heartbeat
```

## 6. State Ownership

Runtime state is owned by `SessionKey + ActorID`.

```text
workflow:{SessionKey}:{ActorID}
pending:{SessionKey}:{ActorID}
policy:{RequestID}
lock:{SessionKey}:{ActorID}
trace:{TraceID}
```

Shared state:

- Long-term memory
- User preferences
- Project facts
- Explicit task brief
- Discussion transcript visible to participants

Isolated state:

- Active workflow phase
- Pending confirmation
- In-flight task
- Tool policy
- Capability token
- Execution lock
- Hidden conversation state

## 7. Workflow Attachment

Workflow is optional. A request does not attach workflow merely because some workflow exists elsewhere.

Unified rule:

```go
func AttachWorkflow(ctx RuntimeContext, intent Intent) *WorkflowState {
    active := workflowStore.Get(ctx.SessionKey, ctx.ActorID)

    if intent == IntentWorkflowContinue && active != nil {
        return active
    }

    if intent == IntentCodingNewProject {
        return workflowStore.Create(ctx.SessionKey, ctx.ActorID)
    }

    if ctx.Parent.HandoffToken != "" {
        return workflowStore.AttachByHandoffToken(ctx.Parent.HandoffToken, ctx)
    }

    return nil
}
```

Behavior:

- Desktop active workflow does not affect Weixin ordinary chat.
- Weixin message `continue` only continues Weixin's own session workflow.
- Cross-channel continuation requires explicit task id or handoff token.
- Invited local AI receives child task context, not parent workflow state.

## 8. Unified Agent Runtime

Every request enters the same dispatch method.

```go
type AgentRuntime interface {
    Dispatch(ctx RuntimeContext, req RequestEnvelope) (ResponseEnvelope, error)
}
```

`RuntimeContext` is derived once from the envelope:

```go
type RuntimeContext struct {
    TraceID        string
    RequestID      string
    SessionKey     string
    ActorID         string
    ActorType       string
    Channel         string
    Provider        string
    ConversationID  string
    ParentRequestID string
    ParentTaskID    string
}
```

No downstream component should infer identity from globals such as `desktop-user` or `current workflow`.

## 9. Tool Policy Resolver

Tool policy is resolved per request and emitted as a short-lived capability token.

Policy layers:

```text
SecurityPolicy
+ SourceProfile
+ ActorProfile
+ TaskPolicy(optional)
= CapabilityToken
```

Security policy always wins. Source, actor, and task policies cannot override critical security denial.

```go
type CapabilityToken struct {
    TokenID      string
    RequestID    string
    SessionKey   string
    ActorID      string
    AllowedTools map[string]ToolGrant
    DeniedTools  map[string]DenyReason
    ExpiresAt    time.Time
}

type DenyReason struct {
    Layer  string // security, source, actor, task
    Code   string
    Detail string
}
```

Tool executor must authorize only with the current token:

```go
AuthorizeTool(ctx, token, toolName, args)
```

It must not read global workflow policy.

## 10. Policy Profiles

Source profiles are configuration, not channel-specific branches.

```yaml
sources:
  desktop.local:
    capabilities: [text, file, screenshot, voice]
  im.weixin:
    capabilities: [text, voice, file]
  third_party.api:
    capabilities: [text, file]
  discussion.internal:
    capabilities: [text, file]
  system.scheduler:
    capabilities: [system_background]
```

Actor profiles are configuration:

```yaml
actors:
  main_ai:
    default_tools: [read_file, list_directory, web_fetch, memory]
  local_ai:
    default_tools: [web_fetch, memory]
  digital_employee:
    default_tools: [knowledge_search, memory, web_fetch]
  system:
    default_tools: [health_check, registry_refresh, memory_sync]
```

Task policy narrows permissions for the current task only.

Example `doc_only` policy:

```yaml
task_policies:
  doc_only:
    allow:
      - read_file
      - list_directory
      - web_fetch
      - memory
      - tts
      - send_file
      - generate_pdf
      - manage_skill.run_safe_installed
    deny:
      - write_file
      - edit_file
      - bash.mutating
      - coding_subagent.execute
      - skill.auto_install_untrusted
```

`doc_only` means "do not enter implementation stage". It does not mean "block all tools".

## 11. Actor Invitation Model

Inviting local AI or a digital employee is dispatch, not state sharing.

Parent creates child envelope:

```go
func InviteActor(parent RuntimeContext, actor ActorRef, brief TaskBrief) RequestEnvelope
```

Child receives:

- Task summary
- Allowed context excerpts
- Allowed files or file summaries
- Expected output schema
- ParentTaskID for traceability
- Its own `SessionKey + ActorID`

Child does not receive:

- Parent workflow phase
- Parent pending confirmation
- Parent capability token
- Parent lock
- Parent hidden reasoning or full conversation state

Discussion session keys:

```text
discussion:{discussion_id}:actor:{actor_id}
```

Main assistant synthesizes child results. Child actors cannot directly advance parent workflow gate unless explicitly granted by task capability.

## 12. Locks and Concurrency

Locks are not global.

Default lock key:

```text
lock:{SessionKey}:{ActorID}
```

Expected behavior:

- Same Weixin direct chat remains serial.
- Desktop assistant does not wait for Weixin long task.
- Invited local AI does not block main assistant unless parent waits for result.

## 13. Background Jobs

Background jobs run as system actor.

```text
Channel = system
Provider = scheduler
ActorID = system
ActorType = system
SessionKey = system:scheduler:background:{job_name}
```

Policy:

```text
SecurityPolicy + SystemBackgroundPolicy
```

They must not inherit user task policy such as `doc_only`.

Examples:

- Capability market heartbeat
- Skill registry refresh
- Memory sync
- LLM ping
- Health check

## 14. Trace Mechanism

Every request has trace id. Every stage logs structured events.

```go
type TraceEvent struct {
    TraceID       string
    RequestID     string
    ParentTraceID string
    SessionKey    string
    ActorID       string
    Channel       string
    Provider      string
    Stage         string
    Intent        string
    WorkflowID    string
    PolicyID      string
    ToolName      string
    Decision      string
    ReasonLayer   string
    ReasonCode    string
    Message       string
    Timestamp     time.Time
}
```

Required stages:

```text
trace.start
ingress.normalized
intent.classified
session.resolved
workflow.attached
workflow.skipped
policy.resolved
tool.authorized
tool.denied
agent.response
egress.sent
trace.end
```

For policy denials, logs must answer:

```text
Which request?
Which actor?
Which session?
Which tool?
Which policy layer denied it?
Was workflow attached? If yes, why?
```

## 15. Trace Viewer

Add debug view searchable by:

- TraceID
- RequestID
- SessionKey
- ActorID
- Tool name
- Deny reason code

Trace timeline:

```text
1. Ingress adapter normalized request
2. Intent classified
3. Workflow attached/skipped with reason
4. Capability token generated
5. Tool calls authorized or denied
6. Response sent
```

Warnings to highlight:

```text
workflow session_key != request session_key
tool executor used expired token
policy resolved without request_id
background job used user policy
```

## 16. Migration Plan

### Phase 1: Add envelope and trace without behavior change

- Add `RequestEnvelope`, `RuntimeContext`, and `TraceContext`.
- Update desktop, Weixin, third-party, discussion, and system entry points to emit envelopes.
- Log `request_id`, `session_key`, `actor_id`, `channel`, and `provider` at agent loop start.

Acceptance:

- All agent loop logs include ownership fields.
- Existing behavior still works.

### Phase 2: Move workflow state to session ownership

- Replace active workflow lookup by user/global with `SessionKey + ActorID`.
- Add handoff token support for cross-channel continuation.

Acceptance:

- Desktop active workflow does not attach to Weixin request.
- Weixin `continue` does not advance desktop workflow.

### Phase 3: Introduce policy resolver and capability token

- Implement `ResolvePolicy(ctx, intent, workflow)`.
- Make tool executor require current request token.
- Log policy layer for all denials.

Acceptance:

- Tool denial explains `security/source/actor/task` layer.
- No tool executor reads global current policy.

### Phase 4: Normalize doc_only and task policies

- Redefine `doc_only` as implementation-stage guard.
- Allow safe read, document, voice, and installed-safe-skill tools.
- Keep dangerous or mutating tools denied.

Acceptance:

- Desktop `doc_only` blocks code implementation.
- Ordinary IM weather and TTS still work.

### Phase 5: Actor invitation isolation

- Change local AI and digital employee invitation to child envelope dispatch.
- Child actor receives brief and capability token, not parent runtime state.

Acceptance:

- Invited local AI does not inherit main assistant workflow phase.
- Child actor results return to parent for synthesis.

### Phase 6: Per-owner locks and system background policy

- Change locks to `SessionKey + ActorID`.
- Move background jobs to system context.

Acceptance:

- Desktop and Weixin requests run concurrently.
- Background heartbeat is not blocked by user workflow.

### Phase 7: Trace viewer and regression tests

- Add trace viewer or CLI trace inspection command.
- Add regression tests listed below.

## 17. Regression Tests

### Test 1: Desktop doc_only does not affect Weixin weather

Setup:

```text
Desktop starts coding workflow and stops at doc_only.
Weixin asks: 查询北京天气
```

Expected:

```text
Weixin request has its own session_key.
No desktop workflow is attached.
weather-query or web_fetch is allowed.
tts is allowed if response mode requests voice.
No doc_only denial appears in Weixin trace.
```

### Test 2: Weixin continue does not advance desktop workflow

Setup:

```text
Desktop waits for requirement confirmation.
Weixin sends: 继续
```

Expected:

```text
No active workflow in Weixin session.
Runtime asks which task to continue or treats it as ordinary chat.
Desktop workflow remains unchanged.
```

### Test 3: Invited local AI isolated from parent

Setup:

```text
Main assistant in coding doc_only.
Digital employee discussion invites local AI to research a topic.
```

Expected:

```text
Local AI receives discussion session_key and actor_id.
No parent doc_only policy attached.
Local AI tool grants come from discussion source + local_ai actor + task policy.
```

### Test 4: Background heartbeat independent

Setup:

```text
User workflow in restrictive phase.
Capability market heartbeat runs.
```

Expected:

```text
Heartbeat context is system actor.
No user workflow policy is loaded.
No misleading tool policy denial appears.
```

### Test 5: Lock isolation

Setup:

```text
Weixin starts long-running task.
Desktop sends a message during the task.
```

Expected:

```text
Desktop request starts immediately.
Same Weixin conversation remains serial.
Trace shows distinct lock keys.
```

## 18. Implementation Checklist

- [x] Define `RequestEnvelope` and `ResponseEnvelope`.
- [x] Define `RuntimeContext` and initial trace projection.
- [x] Add adapter normalization for desktop.
- [x] Add adapter normalization for Weixin.
- [ ] Add adapter normalization for third-party protocol gateway.
- [ ] Add adapter normalization for digital employee discussion.
- [ ] Add system background envelope.
- [ ] Replace workflow lookup with `SessionKey + ActorID`.
- [ ] Add handoff token for explicit cross-session continuation.
- [x] Implement first explicit policy owner resolver for agent-loop tool execution.
- [x] Carry explicit policy owner through agent-view tool forms and approvals.
- [x] Carry explicit policy owner through skill runner launches, including agent-loop `manage_skill(action=run)`.
- [x] Carry explicit runtime owner through task-orchestrator `send_and_observe` enrichment.
- [x] Carry explicit runtime owner through capability-gap skill search/install/run paths.
- [x] Use runtime owner for prompt context sections: steering, memory snapshots, proactive recall, situation report, and workflow experience.
- [x] Use runtime owner for project-tab working directory and coding/session intent gates.
- [x] Use runtime owner for prior-conversation coding-context checks before steering workflow activation.
- [x] Use runtime owner for skill install confirmations, memory tool ownership, and workflow form fallback resolution.
- [x] Preflight auto install-and-run with both install and run policy before skill registration side effects.
- [x] Remove implicit single-active workflow inheritance from generic tool policy owner resolution.
- [x] Remove implicit `lastUserID` workflow inheritance from direct/no-owner tool execution.
- [x] Remove implicit `lastUserID` and active-current-runtime workflow inheritance from AgentView tool submissions without hidden owner.
- [x] Treat non-empty runtime policy owner as authoritative even before full request-id trace plumbing is present.
- [x] Preserve provided runtime policy owner when loop preparation generates missing request/session metadata.
- [x] Treat explicit request envelope with empty runtime owner as isolated: no fallback to `lastUserID` project/workflow state.
- [x] Fail closed for owner-aware skill/subagent launch tools when an explicit runtime envelope has no owner.
- [x] Use runtime owner for `compress_context` pending compression state.
- [x] Use runtime owner for direct `run_skill` and search/install SkillHub execution paths.
- [x] Carry runtime platform through owner-aware SkillHub install confirmation so IM prompts do not read the desktop/global loop context.
- [x] Carry runtime platform and owner through search-and-install SkillHub path before network search/install side effects.
- [x] Carry runtime platform through screenshot and TTS tools so IM media delivery does not read desktop/global loop context.
- [x] Scope `agent_status` main-agent state by runtime owner so remote `/btw` cannot see another channel's current loop.
- [x] Carry hidden runtime owner through `agent_status` so status reads are scoped like other owner-aware tools.
- [x] Carry hidden runtime owner through `async_wait` so wait cancellation follows the owning loop, not the global current loop.
- [x] Carry hidden runtime owner through `set_max_iterations` so loop-limit changes target the owning runtime and empty-owner runtimes fail closed.
- [x] Route desktop AI cancel through desktop/project session identity so an IM loop in legacy global state is not cancelled by the main UI.
- [x] Require Hub `im.cancel_session` to include target user/session identity instead of cancelling the legacy global loop.
- [x] Use explicit manual/desktop owner for skill AgentView submissions.
- [x] Preserve explicit runtime owner through MCP AgentView correction forms.
- [x] Inject hidden runtime owner into synchronous tool args so handler internals do not read the global loop owner.
- [x] Restrict hidden runtime owner injection to owner-aware internal tools so generic/MCP tools never receive private runtime fields.
- [x] Centralize hidden runtime owner consumption so internal tools strip private owner fields before downstream execution.
- [x] Use explicit runtime owner for memory tool context-hint conversation lookup.
- [x] Reject memory tool/context-hint desktop fallback when an explicit runtime envelope has no owner.
- [x] Prevent prompt runtime/situation-report memory sections from falling back to desktop when an explicit runtime envelope has no owner.
- [x] Use hidden runtime owner for local `bash` default project working-directory resolution without leaking it to external tools.
- [x] Use runtime owner for agent-loop tool exposure/execution, bonus-round tools, loop-command tool callbacks, skill auto-run fallbacks, send_and_observe task enrichment, truncation fallback catalogs, NeedsConfirm gates, coding-gate bypasses, steering doc/suggest events, task-orchestrator routing, SubAgent routing, and post-loop workflow capture.
- [x] Make manual remote/App API policy checks use explicit desktop owner instead of arbitrary single-active workflow fallback.
- [x] Move scheduled-task execution path to explicit system background owner.
- [x] Split remote launch policy owners by source: desktop, mobile, handoff, and AI.
- [x] Resolve remote session control owner from the session launch source.
- [ ] Implement capability token.
- [ ] Require token in tool executor.
- [ ] Split policy layers and log deny layer.
- [ ] Redefine `doc_only` policy.
- [ ] Change actor invitation to child envelope dispatch.
- [ ] Change locks to `SessionKey + ActorID`.
- [x] Move capability-market background sync to system context.
- [ ] Add trace viewer or trace CLI.
- [x] Add first regression tests for owner isolation and trace policy denial.

## 19. Summary

Fix is unified ownership, not channel-specific logic.

All entry points normalize into one envelope. All actors use one runtime. Workflow attaches only when current request owns or explicitly references it. Tool policy is resolved per request into a capability token. Invited agents receive task briefs, not parent state. Trace logs explain every dispatch and every tool decision.

This makes desktop assistant, IM channels, third-party protocols, digital employees, local AI, and background jobs consistent, debuggable, and isolated without special-case logic per channel.
