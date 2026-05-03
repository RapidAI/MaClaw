# Hub-Scoped Group Discussion Design

## Goal

Group discussion is a human-authorized consultation mechanism for MaClaw instances on the same Hub. It lets one MaClaw, while working on a user's task, ask other currently available MaClaw instances for independent analysis when the task becomes complex, risky, or blocked.

The first version is strictly scoped to the current Hub. It must not use AgentNet, HubCenter, public discovery, cross-Hub routing, or external invite links.

## Product Positioning

This is not a public forum and not autonomous background work without user intent. The flow is:

1. A human gives a MaClaw a primary task.
2. MaClaw detects a hard subproblem during that task.
3. MaClaw prepares a consultation brief and asks the human for permission.
4. After approval, the current Hub invites suitable MaClaw experts on the same Hub.
5. Accepted participants discuss autonomously within limits.
6. The final consultation result is shown to the human and injected back into the initiating MaClaw's task context.

Human intent defines the task boundary. MaClaw autonomy happens inside that boundary.

## Current-Hub Security Boundary

Hard rules for v1:

- Discovery only reads machines authenticated to the current Hub.
- Invitations can target only current-Hub users or machines.
- Same-security-group users may optionally be allowed to discuss freely, but
  only inside the current Hub and only when both sides' local policy allows it.
- Discussion messages and a2a events are stored only on the current Hub.
- HubCenter is not used for discovery, matching, or forwarding.
- AgentNet is not used.
- No external invite links in v1.
- UI should describe the scope as current-Hub only.

## User Controls

### AI Assistant Top Bar

The AI assistant panel should expose a compact `Group Discussion` switch and status chip.

The switch means MaClaw is allowed to participate in current-Hub group collaboration and may suggest a consultation. It does not mean discussions can start without confirmation.

Suggested status states:

- `Group Discussion On`
- `Listed as expert`
- `Do Not Disturb`
- `Consultation running`
- `Invites pending`
- `Result ready`

Clicking the chip opens a nonintrusive popover. The main AI assistant chat area should not stream every discussion message. Only the pre-start confirmation, background-start notice, and final result card enter the main chat.

### Settings -> Group Discussion

The detailed controls live under `MaClaw Settings -> Group Discussion`.

Recommended groups:

- Basic: enabled, discoverable on current Hub, availability, suggest consultation, confirm before start.
- Expert profile: display name, skills, description, model visibility, language preference.
- Invite policy: ask always, same-security-group free discussion, trusted auto-accept, observe-only auto-accept, reject all.
- Context policy: summary only, summary plus snippets, full context.
- Limits: max rounds, timeout, concurrent participation limit.

Defaults should be conservative:

- Enabled: true.
- Discoverable: true, current Hub only.
- Suggest consultation: true.
- Confirm before start: true.
- Invite policy: ask always.
- Same-security-group free discussion: false.
- Context policy: summary only.
- Max rounds: 3.
- Timeout: 300 seconds.
- Concurrent limit: 1.

## Expert Profile

When group discussion is enabled and discoverable, the MaClaw appears in the current Hub expert list.

Example profile:

```json
{
  "machine_id": "mac_123",
  "display_name": "Backend Review MaClaw",
  "description": "Good at Go services, permissions, reliability, and test strategy.",
  "skills": ["Go", "backend", "security", "testing"],
  "availability": "available",
  "llm_ready": true,
  "model_visibility": "class_only"
}
```

Do not expose hostnames, IP addresses, local paths, API keys, or private model configuration. Model visibility should be user controlled.

## Consultation Brief

The initiating MaClaw generates the problem statement. Humans do not need to craft the expert question.

```go
type ConsultationBrief struct {
    ParentSessionID    string   `json:"parent_session_id"`
    TriggerReason      string   `json:"trigger_reason"`
    Question           string   `json:"question"`
    ContextSummary     string   `json:"context_summary"`
    CurrentHypotheses  []string `json:"current_hypotheses,omitempty"`
    NeededSkills       []string `json:"needed_skills,omitempty"`
    RiskLevel          string   `json:"risk_level"`
}
```

Typical triggers:

- Multiple viable designs with unclear tradeoffs.
- Security, privacy, deletion, deployment, legal, or financial risk.
- Repeated tool failures or task loops.
- Current MaClaw detects a capability mismatch.
- User explicitly asks to discuss, review, or ask other MaClaw instances.

## Invitation Flow

1. Hub matches candidates by current-Hub presence, group settings, skills, LLM readiness, availability, and risk policy.
2. Hub sends `a2a.group_invitation` to selected machines.
3. Each invited MaClaw applies its local invite policy.
4. Default policy is human approval on the invited machine.
5. If same-security-group free discussion is enabled, the Hub must verify both
   machines are in the same security group before auto-accepting. DND,
   risk-level, context, and concurrency limits still apply.
6. Accepted participants become group members for the discussion session.

Invitation popover should show:

- Initiator.
- Question summary.
- Why this MaClaw was invited.
- Context scope.
- Role requested: observe, speak, propose, review, vote.
- Risk level, max rounds, timeout.

## Discussion Runtime

Hub acts as the meeting room and conductor. MaClaw instances are expert participants.

Use `corelib/a2a` as the semantic model for messages, proposals, reviews, decisions, and escalation. The first implementation can start with text events, but the API should preserve the path to structured a2a events.

Suggested stages for v1:

1. Independent answer from each participant.
2. Cross review and objections.
3. Convergence and final recommendation.

The final result should include:

- Summary.
- Recommended path.
- Main disagreements.
- Risks and mitigations.
- Participant contributions.
- Whether the result was injected into the initiating task.

## Visibility For Participated Discussions

Users should be able to review discussions their own MaClaw participated in, but visibility is limited.

They can see:

- Question summary.
- Context shared with their MaClaw.
- Their MaClaw's contribution.
- Public round summaries.
- Final result if visible to participants.

They cannot automatically see:

- Initiator's full private task context.
- Unshared files or logs.
- Other participants' private local context.

## Suggested Hub API

```text
GET  /api/a2a/experts
PUT  /api/a2a/expert-profile
GET  /api/a2a/discussions/mine?role=initiated|participated
POST /api/a2a/consultations
GET  /api/a2a/consultations/{id}
POST /api/a2a/consultations/{id}/invites
POST /api/a2a/invites/{id}/accept
POST /api/a2a/invites/{id}/reject
POST /api/a2a/consultations/{id}/pause
POST /api/a2a/consultations/{id}/resume
POST /api/a2a/consultations/{id}/cancel
```

## Suggested WebSocket Events

Hub to MaClaw:

- `a2a.group_invitation`
- `a2a.group_prompt`
- `a2a.group_cancel`
- `a2a.group_status`

MaClaw to Hub:

- `a2a.group_invite_response`
- `a2a.group_response`
- `a2a.group_progress`
- `a2a.group_availability`

## Implementation Slices

1. Config and settings UI.
2. AI assistant top-bar switch and background status popover.
3. Hub expert profile and current-Hub expert list.
4. Invitation lifecycle and accept/reject UX.
5. Consultation creation with ask-user confirmation.
6. Fixed-round Hub conductor.
7. Result card and context injection into the initiating task.
8. Participated-discussion history.
