# Bugfix Requirements Document

## Introduction

When a user has an active workflow (e.g., coding, PPT design) and the current phase has no output yet (`hasOutput=false`), any user message — including completely unrelated requests like "check server 2's status" — gets unconditionally hijacked into the workflow agent loop. The `HandleInput` section 4 (default branch) returns `RunAgentLoop=true` with a `PhasePrompt` and `ToolFilterPolicy=doc_only`, causing `handleActiveWorkflow` to set `workflowAgentLoopMarker=true`. The subsequent agent loop runs in workflow mode where `applyWorkflowToolFilter` strips all conditional tools (SSH, browser, web_search, etc.), leaving the LLM unable to execute the user's actual request. The LLM spins endlessly trying to generate a "phase document" while the user sees "执行中..." indefinitely.

The root cause is that the `PendingConfirm` path (which has an LLM intent classifier to detect "other" messages) only activates when `phase.NeedsConfirm=true AND hasOutput=true`. When either condition is false, `HandleInput` falls through to section 4 which unconditionally returns `RunAgentLoop=true` without any mechanism to detect that the user's message is unrelated to the workflow. The `DefaultInput` field exists in `WorkflowResponse` and `handleActiveWorkflow` already checks `!resp.DefaultInput` to skip setting `workflowAgentLoopMarker`, but `HandleInput` section 4 never sets `DefaultInput=true`.

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN a user has an active workflow whose current phase has no output yet (`hasOutput=false`) AND the user sends a message unrelated to the workflow (e.g., "查看驱网服务器资源状态") THEN the system hijacks the message into the workflow agent loop with `doc_only` tool filtering, stripping SSH and other conditional tools, causing the LLM to spin indefinitely without performing any operations

1.2 WHEN `HandleInput` reaches section 4 (default branch) for any input during a no-output phase THEN the system unconditionally returns `RunAgentLoop=true` with `DefaultInput=false`, regardless of whether the user's message is related to the workflow

1.3 WHEN `handleActiveWorkflow` receives a response with `RunAgentLoop=true` and `DefaultInput=false` THEN the system sets `workflowAgentLoopMarker=true` and stashes the `PhasePrompt`, causing the agent loop to run in workflow mode with `doc_only` tool filtering applied by `applyWorkflowToolFilter`

1.4 WHEN the agent loop runs in workflow mode with `doc_only` tool filtering THEN SSH tools, browser tools, web_search, and other conditional tools are removed from the LLM's tool list, preventing the LLM from executing the user's unrelated request

### Expected Behavior (Correct)

2.1 WHEN a user has an active workflow whose current phase has no output yet AND the user sends a message unrelated to the workflow (e.g., SSH server operations, weather queries, file operations) THEN the system SHALL detect that the message is unrelated and fall through to the normal agent loop with the full tool list, allowing the LLM to execute the user's request

2.2 WHEN `HandleInput` reaches section 4 (default branch) THEN the system SHALL set `DefaultInput=true` in the response, signaling to the caller that this is a default fallthrough case where the message may or may not be related to the workflow

2.3 WHEN `handleActiveWorkflow` receives a response with `RunAgentLoop=true` and `DefaultInput=true` THEN the system SHALL NOT set `workflowAgentLoopMarker=true`, allowing the message to fall through to the normal agent loop without workflow-specific tool filtering

2.4 WHEN a user sends a workflow-related message (e.g., "开工", actual requirements text, confirm words) during a no-output phase THEN the system SHALL still correctly trigger the workflow agent loop to generate the phase document (the existing PendingConfirm path and confirm/skip word detection remain unchanged)

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a user sends confirm words (e.g., "确认", "OK") during a phase with `NeedsConfirm=true` and existing output THEN the system SHALL CONTINUE TO advance to the next phase via `advancePhase`

3.2 WHEN a user sends skip words during a skippable phase THEN the system SHALL CONTINUE TO skip the phase via `advancePhase`

3.3 WHEN `handlePendingConfirm` classifies a message as "other" during a NeedsConfirm phase with output THEN the system SHALL CONTINUE TO set `workflowPendingConfirmOther=true` and fall through to the normal agent loop with `SkipNeedsConfirmGate=true`

3.4 WHEN `handlePendingConfirm` classifies a message as "modify" THEN the system SHALL CONTINUE TO set `workflowPendingConfirmOther=true` and fall through to the normal agent loop

3.5 WHEN a user sends a message with image attachments and short text (<50 rune) during an active workflow THEN the system SHALL CONTINUE TO skip workflow interception (attachment bypass)

3.6 WHEN the workflow engine has no active workflow for the user THEN the system SHALL CONTINUE TO return nil from `handleWorkflowInterception`, allowing normal agent loop processing

3.7 WHEN `applyWorkflowToolFilter` is called with `SkipNeedsConfirmGate=true` on the loop context THEN the system SHALL CONTINUE TO skip `doc_only` tool filtering, preserving the full tool list

3.8 WHEN a user provides substantial input to an input-driven workflow waiting for documents (`IsWaitingForInput=true`) THEN the system SHALL CONTINUE TO mark `InputReceived=true` and proceed to phase execution
