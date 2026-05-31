# Bugfix Requirements Document

## Introduction

The right-side dashboard / document-preview panel in the MaClaw AI assistant panel (the workflow progress board plus the phase document body) becomes inconsistent with the actual workflow engine state during workflow execution. Users observe one or more of the following symptoms while the agent is producing phase documents (for example, the coding workflow: requirements → design → tasks → implementation → review, or any of the 19 workflow templates):

- The panel stays blank or shows stale content while the agent is actively generating a document.
- The split-pane preview never opens even though a document is being produced.
- The wrong phase node is highlighted on the progress board (or no node is highlighted at all).
- The progress percentage is wrong.
- A phase card shows the wrong status indicator ("生成中" vs "执行中").
- The freshly generated document briefly flickers between raw (preamble-included) content and the cleaned content.
- The highlighted board node and the visible document body refer to different phases.

An investigation identified five distinct defects, each with its own triggering condition. The most impactful is the steering-driven coding flow dropping every document/gate update (Finding 1). This document captures each defect as a bug condition, the correct behavior expected for that condition, and the behaviors that must be preserved (regression prevention) — most importantly the intentional decoupling of the board node from the document body, the existing 19 workflow templates, cross-instance document isolation, and the workflow engine's internal phase-consistency repair logic.

## Bug Analysis

### Current Behavior (Defect)

Each clause below describes an observable defect together with the condition that triggers it.

**Finding 1 — Steering-driven coding flow drops all document and gate updates (root cause)**

1.1 WHEN a coding workflow runs through the steering-driven path (the path that activates only when the workflow engine has no active workflow) THEN the dashboard panel never opens its split-pane preview and shows nothing or stale content, even though the agent is actively producing requirement/design/task documents.

1.2 WHEN the steering-driven coding flow produces a document or a quality-gate result THEN the dashboard silently discards every document-update and gate-result signal, so neither the document body nor the gate banner ever appears for that flow.

**Finding 2 — Unmapped phase ID leaves the board with no highlighted node**

2.1 WHEN the workflow's current phase ID does not match any entry in the board's resolved phase-ID list (any of the 17 templates whose phase IDs are not covered by the coding alias mapping) THEN no phase node is highlighted as the current node on the progress board.

2.2 WHEN the current phase ID is unmatched THEN the board displays an incorrect progress percentage for the workflow.

**Finding 3 — Disagreement on whether a phase produces a document**

3.1 WHEN a phase's metadata is absent (for example, a document-update-only steering flow that carries no phases list) THEN a phase card can show the wrong status indicator — displaying "执行中" when it should show "生成中", or vice versa — because the document-expectation signal disagrees between the two independent sources that determine it.

**Finding 4 — Raw-to-clean document flicker after a phase completes**

4.1 WHEN an agent loop completes and the phase output is captured THEN the board first shows the raw document content (including the preamble that should have been stripped) and only afterward replaces it with the cleaned content, producing a visible flicker.

**Finding 5 — Board node and document body refer to different phases after auto-advance**

5.1 WHEN a phase that does not require confirmation auto-advances to the next phase THEN the board's highlighted (active) node jumps to the next phase while the document body still shows the completed phase's content, and this divergence is presented with no indication that it is intentional, so it reads as an inconsistency.

### Expected Behavior (Correct)

Each clause corresponds to the same-numbered defect above.

1.1 WHEN a coding workflow runs through the steering-driven path THEN the dashboard SHALL open its split-pane preview and display the live document content as the agent produces requirement/design/task documents, matching the behavior of engine-driven workflows.

1.2 WHEN the steering-driven coding flow produces a document or a quality-gate result THEN the dashboard SHALL accept and display that document-update and gate-result, showing the document body and the gate banner for that flow.

2.1 WHEN the workflow's current phase ID is supplied for any of the 19 templates THEN the board SHALL highlight the correct current phase node, resolving the phase ID through a single, consistent source of truth so that no template's phase IDs are left unmatched.

2.2 WHEN the current phase is set THEN the board SHALL display the correct progress percentage corresponding to that phase's position in the workflow.

3.1 WHEN determining whether a phase produces a document THEN the system SHALL derive the document-expectation indicator from a single authoritative source so that the phase card consistently shows "生成中" for document-producing phases and "执行中" for execution phases, with no disagreement when phase metadata is sparse or absent.

4.1 WHEN an agent loop completes and the phase output is captured THEN the board SHALL display the cleaned (preamble-stripped) document content without first showing the raw content, eliminating the raw-to-clean flicker.

5.1 WHEN a phase auto-advances to the next phase THEN the relationship between the highlighted board node and the displayed document body SHALL be presented coherently, so the user can tell that the document body belongs to the just-completed phase while the board has moved on, rather than perceiving it as an inconsistency. (The decoupling itself is preserved — see 3.x in Unchanged Behavior.)

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a phase auto-advances and the document body intentionally remains on the just-completed phase while the board node moves forward THEN the system SHALL CONTINUE TO keep the board's active node and the latest-document phase as separate, independently tracked values (the intentional decoupling must not be collapsed).

3.2 WHEN any of the existing 19 workflow templates runs THEN the system SHALL CONTINUE TO drive its phases through the engine's standard phase-transition lifecycle without requiring template-specific dashboard code.

3.3 WHEN two different workflow instances run (across restarts or workflow-type switches) THEN the system SHALL CONTINUE TO isolate each instance's phase documents and gate results, clearing prior-instance content when a new instance starts.

3.4 WHEN the workflow engine maintains its current phase and phase index THEN the system SHALL CONTINUE TO apply its existing internal consistency-repair logic that keeps the phase identifier and the phase index in agreement.

3.5 WHEN a confirmation-required phase produces a document THEN the system SHALL CONTINUE TO open the preview, display the document, and surface the quality-gate result exactly as it does today for engine-driven confirmation phases.

3.6 WHEN the user has manually closed the split-pane preview THEN the system SHALL CONTINUE TO respect that choice and not force the pane open on subsequent updates.

3.7 WHEN a workflow is fully reset or completed THEN the system SHALL CONTINUE TO clear the board state, dismiss the maximize suggestion, and preserve already-produced documents for viewing until the next instance starts, exactly as it does today.
