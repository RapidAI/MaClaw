// Package memory owns the shared long-term memory subsystem used by GUI, TUI,
// and server-side agents.
//
// Write-path guidance:
//
//   - Store.Save and Store.SaveWithContext are the low-level evidence writes.
//     They perform safety scanning, secret redaction, deterministic dedup, index
//     updates, and persistence. New host integrations should avoid calling them
//     directly unless they are implementing a new corelib memory helper.
//
//   - Store.SaveGovernedWithContext is the tool/online-extraction write path
//     for uncertain LLM candidates. It can accept, quarantine, or reject a
//     candidate before the low-level write happens.
//
//   - Store.UpsertEntryByID and Store.UpsertEntryByTags are the generated-memory
//     primitives for stable records that should update rather than duplicate.
//     Prefer the narrower helpers below when a category convention exists.
//
//   - Store.UpsertProjectKnowledge, Store.UpsertTaskArtifact,
//     Store.UpsertConversationSummary, Store.UpsertSessionCheckpoint, and
//     Store.UpsertGeneratedInsight are the generated write paths shared by GUI,
//     TUI, and server-side integrations.
//
//   - Store.SaveManualMemory and Store.UpdateManualMemory are the user-authored
//     GUI/TUI/server management paths. Manual creates intentionally remain
//     distinct user actions while still going through corelib safety/indexing.
//
// Maintenance-path guidance:
//
//   - StoreFactory helpers and Maintenance are the host-facing entry points for
//     opening stores, compression, and backup/restore. GUI, TUI, and MaClawSrv
//     should not construct independent maintenance implementations.
//
//   - Pending semantic dedup handles precise async duplicate checks for recent
//     writes.
//
//   - Candidate consolidation handles quarantined low-confidence memory
//     candidates; it is not a generic dedup replacement.
//
//   - Compressor, Synthesizer, TMT Consolidator, ProfileConsolidator, and theme
//     rebuild are delayed maintenance views. Schema/profile formation must pass
//     the consolidation gate, and derived outputs must preserve evidence and
//     boundary metadata instead of overwriting raw episodic evidence.
package memory
