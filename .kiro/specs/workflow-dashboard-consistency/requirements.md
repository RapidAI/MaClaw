# Requirements Document

## Introduction

The workflow dashboard (the right-side "Workflow Progress Board" / document preview in the desktop
AI assistant panel, `WorkflowDocPreview.tsx`) renders per-phase metadata: the ordered list of
phases, their display labels, whether each phase produces a preview document, and the current/active
phase plus quality-gate status. Today a large slice of that metadata is duplicated in the frontend
as hand-maintained maps (`phaseLabels`, `workflowPhaseOrders`, `fallbackNonDocumentPhaseIDs`), while
the authoritative copy already lives in the Go workflow templates (`corelib/workflow/templates.go`).
Keeping the two copies synchronized by hand is exactly the drift this codebase's steering rules call
out, and it produced a set of observable defects.

A prior investigation (see `bugfix.md`) identified five distinct defects that motivate this work:

- **Finding 1** — the steering-driven coding flow silently dropped every document-update and
  quality-gate signal, so the preview never opened.
- **Finding 2** — a current phase identifier that did not match the board's resolved phase-ID list
  left no highlighted node and an incorrect progress percentage.
- **Finding 3** — the document-expectation signal disagreed between two independent sources, so a
  phase card could show the wrong status indicator ("生成中" vs "执行中").
- **Finding 4** — a freshly generated document briefly flickered between raw and cleaned content.
- **Finding 5** — after a non-confirmation phase auto-advanced, the highlighted board node and the
  visible document body referred to different phases, reading as an inconsistency.

This feature makes the backend workflow templates the **single source of truth** for dashboard phase
metadata. The dashboard derives phase order, labels, and document-expectation purely from
backend-emitted metadata; the hardcoded maps are retained only as a degraded-mode fallback; and an
anti-drift mechanism (a generated artifact plus contract tests on both sides) guarantees the
retained fallback can never silently diverge from the templates. The scope of this design is the
phase-metadata single-source-of-truth mechanism that addresses Findings 2 and 3 at the root and
preserves the intentional board/document decoupling behind Finding 5. The requirements below also
capture the existing behaviors that must be preserved (regression prevention).

## Glossary

- **Workflow_Template**: A registered backend definition (`corelib/workflow`) describing one
  workflow type and its ordered phases. There are 19 such built-in templates.
- **Phase_Template**: A single phase within a Workflow_Template, carrying its `ID`, `Name`
  (display label), `ToolPolicy`, `NeedsConfirm`, and `CanSkip`.
- **ToolPolicy**: The tool-filter policy of a Phase_Template (a `ToolFilterPolicy` value).
- **Workflow_Registry**: The backend component that holds every registered Workflow_Template and
  enumerates them deterministically.
- **Phase_Metadata_Deriver**: The single backend function (`workflow.PhaseMetadata`) that projects
  a Workflow_Template into ordered, de-duplicated, dashboard-ready phase metadata.
- **Phase_Meta**: One emitted metadata element produced by the Phase_Metadata_Deriver, carrying
  `id`, `name`, `index`, `expects_document`, `can_skip`, and `needs_confirm`.
- **Phase_Update_Emitter**: The GUI engine-callback adapter (`GUIWorkflowAdapter`) that attaches
  emitted Phase_Meta to the `workflow:phase_update` event.
- **TUI_Adapter**: The terminal engine-callback adapter (`TUIWorkflowCallbacks`) that derives the
  same Phase_Meta for parity.
- **Dashboard**: The frontend renderer (`WorkflowDocPreview` / `WorkflowProgressBoard` and
  `useWorkflowState`) that displays phases, labels, document-expectation, the active node, progress,
  and gate status.
- **Fallback_Maps**: The retained hardcoded frontend maps (`phaseLabels`, `workflowPhaseOrders`,
  `fallbackNonDocumentPhaseIDs`) used only in degraded mode.
- **Phase_Metadata_Generator**: The Go code generator (`cmd/genphasemeta`) that writes the
  Generated_Artifact from the Workflow_Registry.
- **Generated_Artifact**: The auto-generated frontend file (`workflowPhaseMeta.generated.ts`) that
  mirrors the Phase_Metadata_Deriver output for every template.
- **Contract_Test**: The Go and TypeScript tests that enforce agreement between the Fallback_Maps,
  the Generated_Artifact, and the live Workflow_Registry.
- **Workflow_Engine**: The backend engine (`corelib/workflow`) that drives phase transitions and
  maintains the current phase and phase index.

## Requirements

### Requirement 1: Backend templates are the single source of truth for phase metadata

**User Story:** As a dashboard maintainer, I want the backend workflow templates to be the single
source of truth for per-phase dashboard metadata, so that adding or changing a phase requires
editing only the templates.

#### Acceptance Criteria

1. WHEN the Phase_Metadata_Deriver projects a registered Workflow_Template, THE Phase_Metadata_Deriver SHALL produce a phase metadata list whose phase-identifier order equals the template's phase order after applying canonical-ID aliasing and removing duplicate canonical identifiers by retaining only the first occurrence, and SHALL assign each emitted phase a 0-based index that is contiguous from 0 to n-1 and strictly increasing in list order.
2. WHEN the Phase_Metadata_Deriver projects a registered Workflow_Template, THE Phase_Metadata_Deriver SHALL assign to every emitted phase a display label that equals the source Phase_Template Name and contains at least one non-whitespace character.
3. WHEN the Phase_Metadata_Deriver computes the document-expectation of a Phase_Template, THE Phase_Metadata_Deriver SHALL derive the value solely from the Phase_Template ToolPolicy, setting document-expectation to false if and only if the ToolPolicy equals ToolFilterFull or ToolFilterOpsControlled, and to true for every other ToolPolicy value.
4. WHEN emitted phase metadata is serialized to JSON and parsed by the Dashboard, THE Dashboard SHALL reproduce, for every emitted phase, a parsed phase carrying an identical phase identifier, the same relative order by ascending index, and identical expects_document, can_skip, and needs_confirm flag values, with no emitted phase dropped, duplicated, or reordered.
5. WHEN the TUI_Adapter derives phase metadata for a Workflow_Template, THE TUI_Adapter SHALL invoke the same Phase_Metadata_Deriver function used by the Phase_Update_Emitter, producing Phase_Meta elements identical to those the Phase_Update_Emitter produces for the same Workflow_Template, rather than maintaining a separately derived phase list.

### Requirement 2: Anti-drift codegen and contract test mechanism

**User Story:** As a developer, I want an automated anti-drift mechanism, so that the retained
hardcoded fallback maps can never silently diverge from the backend templates.

#### Acceptance Criteria

1. WHERE a phase identifier appears in both a Fallback_Map and the Generated_Artifact, THE Contract_Test SHALL compare the fallback label against the generated label as a character-for-character identical match (case-sensitive, with no trimming) when the fallback label is defined, and SHALL treat an absent fallback label as agreeing, and SHALL compare the fallback document-expectation against the generated document-expectation as an identical boolean.
2. IF a Fallback_Map entry diverges from the corresponding Generated_Artifact entry on either the label comparison or the document-expectation comparison defined in 2.1, THEN THE Contract_Test SHALL fail and SHALL identify the workflow type and phase identifier that diverged.
3. WHEN the Generated_Artifact is regenerated in memory from the Workflow_Registry, THE Contract_Test SHALL verify that the regenerated output is byte-for-byte identical to the committed Generated_Artifact file after normalizing line endings, and SHALL fail with an instruction to regenerate the artifact if they differ.
4. THE Phase_Metadata_Generator SHALL produce the Generated_Artifact by deriving, through the Phase_Metadata_Deriver, exactly one phase metadata list for each Workflow_Template enumerated by the Workflow_Registry, ordered deterministically so that regeneration is byte-stable.

### Requirement 3: Dashboard renders from emitted metadata with fallback as degraded mode only

**User Story:** As a user, I want the dashboard to render purely from backend-emitted metadata and
fall back to the hardcoded maps only when metadata is unavailable, so that the board always reflects
the authoritative templates while remaining functional with older backends.

#### Acceptance Criteria

1. WHERE a workflow type has a Fallback_Map phase order, WHILE the emitted Phase_Meta list for that workflow type is present and non-empty, THE Dashboard SHALL derive a de-duplicated phase-identifier set from the emitted Phase_Meta that contains every phase identifier in that Fallback_Map phase order, and SHALL resolve every identifier in that set to a non-empty display label.
2. WHILE the emitted Phase_Meta list for a workflow type is present and non-empty, THE Dashboard SHALL derive the phase order from the emitted Phase_Meta index order, the phase labels from the emitted Phase_Meta name field, and the document-expectation from the emitted Phase_Meta expects_document field, and SHALL NOT read phase order, labels, or document-expectation from the Fallback_Maps.
3. IF the emitted Phase_Meta list for a workflow type is absent (the phases field is omitted) or empty (zero-length), THEN THE Dashboard SHALL derive the phase order, labels, and document-expectation for that workflow type from the Fallback_Maps.
4. WHEN a phase identifier appears in a document update or as the current phase but is absent from the emitted Phase_Meta, THE Dashboard SHALL append that phase identifier to the end of the rendered phase list and SHALL resolve it to a non-empty label by checking, in order, the emitted Phase_Meta label, then the Fallback_Maps label, then a label derived from the phase identifier itself.

### Requirement 4: Correct phase highlighting and progress for every template

**User Story:** As a user, I want the progress board to highlight the correct current phase node and
show the correct progress for any of the 19 workflow templates, so that no template's phase
identifiers are left unmatched.

#### Acceptance Criteria

1. WHEN a current phase identifier is supplied for any registered Workflow_Template, THE Dashboard SHALL mark exactly one phase node as the active node, being the node whose canonical phase identifier equals the supplied current phase identifier after canonical-ID aliasing, resolved within the Dashboard's resolved phase-identifier list.
2. WHEN a current phase identifier is supplied for any registered Workflow_Template, THE Dashboard SHALL set the progress value as a function of the active node's zero-based index within the same resolved phase-identifier list, such that the value increases monotonically with that index and attains its maximum only at the final phase.

### Requirement 5: Consistent document-expectation indicator

**User Story:** As a user, I want each phase card's status indicator to consistently reflect whether
the phase produces a document, so that the generation and execution indicators never disagree.

#### Acceptance Criteria

1. WHEN the Dashboard renders a phase card, THE Dashboard SHALL derive the phase's document-expectation as a single value that is either document-producing or execution, and SHALL drive every status indicator on that card from that single derived value.
2. WHERE a phase's derived value is document-producing, THE Dashboard SHALL display the document-generation status indicator and SHALL NOT display the execution status indicator.
3. WHERE a phase's derived value is execution, THE Dashboard SHALL display the execution status indicator and SHALL NOT display the document-generation status indicator.

### Requirement 6: Preserve established dashboard behaviors (regression prevention)

**User Story:** As a maintainer, I want the established dashboard behaviors preserved, so that
introducing the single-source-of-truth mechanism causes no regressions.

#### Acceptance Criteria

1. WHILE the visible document body remains on the just-completed phase, WHEN a phase auto-advances, THE Dashboard SHALL set the board's active-node identifier to the newly-advanced phase identifier and leave the latest-document phase identifier unchanged, maintaining the active-node identifier and the latest-document phase identifier as two separate values that each update independently of the other.
2. WHEN any of the 19 registered Workflow_Templates runs, THE Workflow_Engine SHALL advance its phases through the standard phase-transition lifecycle (generate document, optional confirmation pause, advance) and SHALL derive all dashboard phase metadata through the shared Phase_Metadata_Deriver, with no template-specific dashboard code path.
3. WHEN a new workflow instance starts, THE Dashboard SHALL replace the phase-document collection and the gate-result collection with empty collections scoped to the new instance, such that no phase document and no gate result from any prior instance remains displayed or retrievable.
4. WHILE a workflow instance is active, THE Workflow_Engine SHALL keep the current phase index equal to the position of the current phase identifier within the template's canonical (alias-applied, de-duplicated) phase order, such that the phase located at the current phase index always has an identifier equal to the current phase identifier.
5. WHERE the user has not manually closed the split-pane preview, WHEN a confirmation-required (NeedsConfirm) phase produces a document, THE Dashboard SHALL open the split-pane preview, render that phase's document content in the preview pane, and surface that phase's quality-gate result (pass or fail together with its checked items).
6. WHILE the user has manually closed the split-pane preview, THE Dashboard SHALL keep the preview pane closed on every subsequent phase-update, document-update, and gate-result event, until either the user re-opens the preview or a new workflow instance starts.
7. WHEN a workflow is reset or reaches completion, THE Dashboard SHALL clear the progress-board phase state, dismiss the maximize suggestion, and retain every already-produced phase document in a viewable state until the next workflow instance starts.

### Requirement 7: Preserve interface and guard invariants

**User Story:** As a maintainer, I want the existing interface and UI guard invariants preserved, so
that the retained fallback and the engine callback contract remain intact and the anti-drift wiring
cannot be deleted silently.

#### Acceptance Criteria

1. THE Dashboard SHALL retain the WorkflowProgressBoard symbol and the workflowPhaseOrders Fallback_Map symbol as present, defined identifiers in its source, such that the UI guard symbol-presence check for both symbols reports success (non-failure exit).
2. THE Phase_Update_Emitter SHALL implement the EngineCallbacks EmitPhaseUpdate method with an unchanged signature that accepts a user-identifier parameter and a workflow-state parameter and returns a single error value, so that the Phase_Update_Emitter continues to satisfy the EngineCallbacks interface.
3. THE Contract_Test SHALL assert, as a single pass/fail check, that the Generated_Artifact file is present at its committed path and that the Contract_Test source imports the Generated_Artifact module, and THE Contract_Test SHALL fail if either condition is not met.
