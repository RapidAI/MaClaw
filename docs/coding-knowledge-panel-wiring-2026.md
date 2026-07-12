# Coding knowledge panel wiring (2026-07)

**Track:** coding-subagent knowledge accumulation — Phase 4 GUI management panel  
**Status:** **frozen** — see [coding-knowledge-panel-track-freeze-2026.md](./coding-knowledge-panel-track-freeze-2026.md)

## Problem

Backend APIs (`gui/app_coding_knowledge.go`, store, extract/save, evolution) were already
implemented, but **Programming Tools → 编程知识库** still used frontend stubs:

- list always `[]`
- delete / confirm no-ops
- search fell through to general `KnowledgeSearch`
- stats reused general knowledge stats

The management UI looked complete but never touched `coding_knowledge.db`.

## Change

| Layer | Work |
|-------|------|
| Frontend | `ProgrammingToolsSettingsPanel.tsx` imports real `CodingKnowledge*` Wails bindings |
| Wails JS | `gui/frontend/wailsjs/go/main/App.js` + `App.d.ts` export the coding-knowledge methods |
| Filter JSON | `CodingListFilter` gains snake_case `json` tags so `{ scope, language, limit }` from UI unmarshals reliably |
| Tests | Go CRUD on App bindings; vitest for list / search / confirm / delete / reset |

### Increment 2 — detail/edit + pack IO + graduate (2026-07)

| Layer | Work |
|-------|------|
| Store | `UpdateExperience` keeps stable ID via `TextSaveRequest.ForceID` (was delete+new id) |
| Frontend | Edit modal (Get → form → Update); Export/Import pack buttons; Graduate for `verified` |
| Wails | Bind `ExportToFile` / `ImportFromFile` / `GraduateToSteering` + path pickers |
| Tests | Store update-preserves-id; vitest editor save / export / import / graduate |

### Increment 3 — capacity limits + eviction UI (2026-07)

| Layer | Work |
|-------|------|
| Backend | `CodingKnowledgeCapacity` snapshot; `CodingKnowledgeEvict` enforces **per-project then global** |
| Config | Panel edits `coding_knowledge_max_total` / `coding_knowledge_max_per_project` via `PatchConfigFields` |
| Frontend | Capacity `N/max` badge, limit inputs, over-limit hint, project overflow list, **Run eviction** |
| Tests | Go capacity+evict; vitest limit patch + eviction |

## Operator check

```powershell
go test ./corelib/knowledge/ -count=1 -timeout 60s -run "TestCodingKnowledgeStore_UpdatePreservesID"
go test ./gui/ -count=1 -timeout 120s -run "TestCodingKnowledgeWailsBindingsCRUD|TestCodingKnowledgeResetFile|TestCodingKnowledgeCapacityAndEvict"

cd gui/frontend
node ./node_modules/vitest/vitest.mjs run src/components/settings/__tests__/ProgrammingToolsSettingsPanel.test.tsx
```

## Still out of scope (later named goals)

- Multi-select bulk delete / bulk graduate
- Rich failed_attempts / contraindications list editors
- Scheduled/background capacity doctor alert when over limit

## Related design

- [coding-subagent-knowledge-accumulation-design.md](./coding-subagent-knowledge-accumulation-design.md) Phase 4 / Phase 5
