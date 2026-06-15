// Package v2 is the canonical workflow engine for maclaw.
//
// # Architecture
//
// V2 replaces the original 6-7 layer V1 engine with a single-layer design:
//
//   - Router: BM25 template matching + LLM confirm classification.
//   - StateMachine (machine.go): Phase lifecycle management, advances, cancellation.
//   - PhaseExecutor / TaskRunner (task_runner.go): Background phase execution.
//   - TemplateRegistry (templates.go): All workflow templates registered once.
//   - Store (store.go, store_sqlite.go, store_memory.go): State persistence.
//
// # Type Naming Conventions
//
// This package contains BOTH native V2 types and V1-compat types. The naming
// convention distinguishes them:
//
//   - WorkflowState (state.go): The V2 native state machine state. Contains
//     []Phase with per-phase status, used by StateMachine and Store.
//   - V1WorkflowState (types_compat.go): The compat state consumed by the GUI
//     engine adapter layer. 37+ files reference this type by the V1 name.
//     It maps to the old corelib/workflow.WorkflowState that was deleted.
//   - WorkflowTemplate (templates.go): V2 native template definition used by
//     Router and TemplateRegistry. Compact phase definition with PhaseTemplate.
//   - V1WorkflowTemplate (types_compat.go): The compat template with V1-era
//     fields (Prompt, Kind, MutationScope, InputSchema, DisableOrchestrator).
//     Consumed by WorkflowEngine.HandleInput and GUI code that references V1 names.
//
// # Compatibility Layer (types_compat*.go)
//
// Two files provide backward compatibility for the ~40 files that imported
// the now-deleted corelib/workflow/ package:
//
//   - types_compat.go: Type definitions, tool policy functions, quality gate
//     types, ops command types, and utility functions. Organized into sections
//     (see "===" section headers in the file).
//   - types_compat_engine.go: Stub implementations of WorkflowEngine,
//     WorkflowRegistry, IntentUnderstandingManager, and QuickFilter. These
//     satisfy compilation for test code that still creates V1 engines. Runtime
//     behavior is 100% V2 (delegated to StateMachine).
//
// # Adding a New Workflow Template
//
// 1. Define the template function in templates.go (e.g. MyNewTemplate()).
// 2. Add a WorkflowType constant in types_compat.go.
// 3. Register it in RegisterBuiltinTemplates() in templates.go.
// 4. Add phase labels in gui/frontend/.../WorkflowDocPreview.tsx.
// 5. (Optional) Add phase-specific instructions in phase_prompt.go.
//
// No gate code, detector code, or agent loop changes are needed — the engine's
// unified NeedsConfirm mechanism, tool filtering, and doc capture handle all
// templates generically.
//
// # Key Entry Points for Developers
//
//   - templates.go: RegisterBuiltinTemplates — start here to see all templates.
//   - machine.go: StateMachine — the V2 runtime (Create, HandleInput, Advance, Cancel).
//   - state.go: WorkflowState, Phase, ToolPolicy — V2 native data model.
//   - types_compat.go: V1WorkflowState, V1WorkflowTemplate — compat types for GUI.
//   - types_compat_engine.go: WorkflowEngine — stub engine for test backward compat.
//   - phase_prompt.go: BuildPhaseSystemPrompt — phase-specific LLM instructions.
package v2
