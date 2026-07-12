# Coding knowledge panel track freeze (2026-07)

**Status: frozen for observation**  
Default “继续” should **not** stack more programming-knowledge panel micro-features without a **named new goal**.

Progress log: [coding-knowledge-panel-wiring-2026.md](./coding-knowledge-panel-wiring-2026.md)  
Design: [coding-subagent-knowledge-accumulation-design.md](./coding-subagent-knowledge-accumulation-design.md)

## Shipped (design → product)

| Item | Summary |
|------|---------|
| Wails bindings wired | Settings panel no longer stubs; talks to `coding_knowledge.db` |
| List / search / confirm / delete / reset | Full CRUD surface for experiences |
| Detail / edit modal | Get → edit title/category/scope/status/content/snippet → Update (stable ID) |
| Export / import pack | JSON pack file dialogs |
| Graduate to steering | Verified experiences → `~/.maclaw/steering/` |
| Capacity + eviction | Max total / max per project config; capacity snapshot; Run eviction (project then global) |
| SubAgent paths | Extract/save + recall tools (pre-existing; panel now manages the store) |

### Desktop APIs (management)

| Method | Role |
|--------|------|
| `CodingKnowledgeStats` / `List` / `Search` / `Get` / `Update` | Inspect & edit |
| `CodingKnowledgeConfirm` / `Delete` / `ResetFile` | Lifecycle |
| `CodingKnowledgeExportToFile` / `ImportFromFile` | Pack IO |
| `CodingKnowledgeGraduateToSteering` | Evolution |
| `CodingKnowledgeCapacity` / `Evict` | Capacity |

## Operator regression

```powershell
go test ./corelib/knowledge/ -count=1 -timeout 60s -run "TestCodingKnowledgeStore_UpdatePreservesID"
go test ./gui/ -count=1 -timeout 120s -run "TestCodingKnowledgeWailsBindingsCRUD|TestCodingKnowledgeResetFile|TestCodingKnowledgeCapacityAndEvict"

cd gui/frontend
node ./node_modules/vitest/vitest.mjs run src/components/settings/__tests__/ProgrammingToolsSettingsPanel.test.tsx
```

## Out of freeze (explicit goals only)

1. Multi-select bulk delete / bulk graduate  
2. Rich `failed_attempts` / `contraindications` list editors  
3. System Doctor / background alert when over capacity  
4. Scheduled auto-evict policy UI beyond manual “Run eviction”

## Freeze rule

Do not stack more coding-knowledge panel work on generic “继续”.  
Re-open only with an explicit goal name + acceptance criteria.  
Default “继续” should switch product line (latency, HubCenter, cost-route freeze index, etc.).
