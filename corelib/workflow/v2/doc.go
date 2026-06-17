// Package v2 is the canonical workflow engine for maclaw.
//
// # Architecture
//
// Single-layer design:
//
//   - Router (router.go): BM25 template matching + LLM confirm classification.
//   - StateMachine (machine.go): Phase lifecycle management, advances, cancellation.
//   - TaskRunner (task_runner.go): Background phase execution.
//   - TemplateRegistry (templates.go): All workflow templates registered once.
//   - Store (store.go, store_sqlite.go, store_memory.go): State persistence.
//
// # Type Organization
//
//   - WorkflowState (state.go): The native state machine state. Contains
//     []Phase with per-phase status, used by StateMachine and Store.
//   - EngineState (types.go): GUI runtime state used by the WorkflowEngine
//     adapter layer. Contains PhaseOutputs map and template-derived fields.
//   - WorkflowTemplate (templates.go): Native template definition used by
//     Router and TemplateRegistry. Compact phase definition with PhaseTemplate.
//   - TemplateSpec (types.go): Extended template definition with detailed
//     phase info (Prompt, Kind, MutationScope, InputSchema). Used by
//     WorkflowEngine.HandleInput and GUI adapter code.
//
// # Adding a New Workflow Template
//
// 1. Define the template function in templates.go (e.g. MyNewTemplate()).
// 2. Add a WorkflowType constant in types.go.
// 3. Register it in RegisterBuiltinTemplates() in templates.go.
// 4. Add phase labels in gui/frontend/.../WorkflowDocPreview.tsx.
// 5. (Optional) Add phase-specific instructions in phase_prompt.go.
//
// No gate code, detector code, or agent loop changes are needed — the engine's
// unified NeedsConfirm mechanism, tool filtering, and doc capture handle all
// templates generically.
//
// # Key Entry Points
//
//   - templates.go: RegisterBuiltinTemplates — start here to see all templates.
//   - machine.go: StateMachine — the runtime (Create, HandleInput, Advance, Cancel).
//   - state.go: WorkflowState, Phase, ToolPolicy — native data model.
//   - types.go: WorkflowType, EngineState, TemplateSpec — domain types.
//   - types_compat_engine.go: WorkflowEngine — adapter (DEPRECATED, being removed).
//   - phase_prompt.go: BuildPhaseSystemPrompt — phase-specific LLM instructions.
package v2
