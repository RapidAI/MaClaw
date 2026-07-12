# Design Document: Skill Source Reclassification

## Overview

This design introduces new `Source` field values (`"auto_hub"`, `"auto_github"`, `"auto_clawhub"`) to distinguish skills that Maclaw autonomously installed via capability gap detection from skills the user explicitly installed. A centralized `IsLearnedSource` predicate replaces scattered inline source checks, and the frontend tab filtering is updated to classify auto-installed skills into the Learned tab.

## Architecture

### Approach: New Source Values + Centralized Predicate

Rather than adding a separate boolean field (e.g., `AutoInstalled bool`) to `NLSkillEntry`, we introduce new `Source` values with an `"auto_"` prefix. This approach:

- Preserves the existing single-field classification model
- Requires no schema migration for persisted skill JSON files
- Makes the install origin self-documenting in the source value
- Allows a simple string-based predicate for classification

### Component Changes

```
┌─────────────────────────────────────────────────────────┐
│                    corelib/types.go                       │
│  NLSkillEntry.Source: add "auto_hub"|"auto_github"|      │
│                       "auto_clawhub" to valid values     │
│  + IsLearnedSource(source string) bool                   │
└──────────────────────┬──────────────────────────────────┘
                       │ used by
        ┌──────────────┼──────────────────────┐
        ▼              ▼                      ▼
┌──────────────┐ ┌──────────────┐  ┌─────────────────────┐
│ skill_backup │ │app_nl_skills │  │ capability_gap_      │
│   .go        │ │   .go        │  │ detector.go          │
│ Export uses  │ │ Cleanup uses │  │ Sets auto_ prefix    │
│ IsLearned    │ │ IsLearned    │  │ on Source            │
└──────────────┘ └──────────────┘  └─────────────────────┘
                                            │
                       ┌────────────────────┤
                       ▼                    ▼
              ┌──────────────┐    ┌──────────────────────┐
              │im_message_   │    │ToolSearchAndInstall  │
              │handler.go    │    │ (tool call path)     │
              │registerAnd   │    │ Sets auto_ prefix    │
              │ExecuteSkill  │    │ on Source             │
              └──────────────┘    └──────────────────────┘

┌─────────────────────────────────────────────────────────┐
│           Frontend: SkillsManagementPanel.tsx             │
│  + isLearnedSource(source: string): boolean              │
│  installedSkills = skills.filter(!isLearnedSource)       │
│  learnedSkills = skills.filter(isLearnedSource)          │
│  Source icon: for auto_*, for learned, crafted │
└─────────────────────────────────────────────────────────┘
```

## Detailed Design

### 1. Go Helper: `IsLearnedSource` (corelib/types.go)

Add a package-level function next to the `NLSkillEntry` type:

```go
// learnedSources is the set of Source values that classify a skill as
// "learned" (autonomously acquired by Maclaw).
var learnedSources = map[string]bool{
    "learned":      true,
    "crafted":      true,
    "auto_hub":     true,
    "auto_github":  true,
    "auto_clawhub": true,
}

// IsLearnedSource returns true if the given source value indicates a skill
// that was autonomously acquired by Maclaw (learned, crafted, or auto-installed).
func IsLearnedSource(source string) bool {
    return learnedSources[source]
}
```

Update the `Source` field comment:
```go
Source string `json:"source"` // "manual" | "learned" | "hub" | "crafted" | "file" | "zip_import" | "github" | "clawhub" | "auto_hub" | "auto_github" | "auto_clawhub"
```

### 2. CapabilityGapDetector Source Changes (gui/capability_gap_detector.go)

In `Resolve()`, after the skill is downloaded/imported but before `Register()`:

- **Hub path** (Step 6): Set `skill.Source = "auto_hub"` before calling `d.skillExecutor.Register(*skill)`.
- **GitHub fallback path**: Set `imported.Source = "auto_github"` before calling `d.skillExecutor.Register(*imported)`.

The ClawHub path in `Resolve` is not directly present (ClawHub is handled via `toolSearchAndInstallSkill`), but if added, it would use `"auto_clawhub"`.

### 3. toolSearchAndInstallSkill Source Changes (gui/im_message_handler.go)

In `installAndExecuteSkill()`, the `registerAndExecuteSkill` calls pass a `source` string. Change these calls to pass `"auto_"` prefixed values:

- GitHub path: change `"github"` → `"auto_github"`
- ClawHub path: change `"clawhub"` → `"auto_clawhub"`
- SkillMarket path: change `best.Status` → `"auto_" + best.Status` (when `best.Status` is `"hub"`)

The `registerAndExecuteSkill` function itself does not set `Source` on the skill — it receives a pre-built `*NLSkillEntry` whose `Source` is already set by the download/import function. However, the `source` parameter is used for audit logging. We need to also set `skill.Source` to the auto-prefixed value before `Register()`.

### 4. Manual Install Paths (No Changes)

These paths remain unchanged:
- `SkillHubClient.Install()` → `Source: "hub"` (user clicked install in Hub UI)
- TUI `skillhub install` command → `Source: "hub"` (user ran CLI command)
- `ImportNLSkillZip()` → `Source: "zip_import"`
- `ImportNLSkillFile()` → `Source: "file"`

### 5. Backend Filter Updates

#### ExportLearnedSkillsZip (gui/skill_backup.go)

Replace:
```go
if (s.Source == "learned" || s.Source == "crafted") && wanted[s.Name] {
```
With:
```go
if IsLearnedSource(s.Source) && wanted[s.Name] {
```

#### CleanupStaleSkills (gui/app_nl_skills.go)

Replace:
```go
if s.Source != "learned" && s.Source != "crafted" {
    continue
}
```
With:
```go
if !IsLearnedSource(s.Source) {
    continue
}
```

### 6. Frontend Changes (SkillsManagementPanel.tsx)

#### Helper Function

Add near the top of the file or in a shared utils module:

```typescript
const LEARNED_SOURCES = new Set(["learned", "crafted", "auto_hub", "auto_github", "auto_clawhub"]);

function isLearnedSource(source: string): boolean {
    return LEARNED_SOURCES.has(source);
}
```

#### Tab Filtering

Replace:
```typescript
const installedSkills = useMemo(
    () => skills.filter((s) => s.source !== "learned" && s.source !== "crafted"),
    [skills]
);
const learnedSkills = useMemo(
    () => skills.filter((s) => s.source === "learned" || s.source === "crafted"),
    [skills]
);
```
With:
```typescript
const installedSkills = useMemo(
    () => skills.filter((s) => !isLearnedSource(s.source)),
    [skills]
);
const learnedSkills = useMemo(
    () => skills.filter((s) => isLearnedSource(s.source)),
    [skills]
);
```

#### Source Icon Display

Update the source icon in the Learned tab table:

```typescript
const learnedSourceIcon = (source: string) => {
    if (source === "learned") return "";
    if (source === "crafted") return "";
    if (source.startsWith("auto_")) return "";
    return ""; // fallback
};

const learnedSourceTooltip = (source: string) => {
    if (source === "learned") return localizeText("Experience learned", "经验学习", "經驗學習");
    if (source === "crafted") return localizeText("Tool crafted", "工具制作", "工具製作");
    if (source === "auto_hub") return localizeText("Auto-installed from SkillHub", "自动安装自 SkillHub", "自動安裝自 SkillHub");
    if (source === "auto_github") return localizeText("Auto-installed from GitHub", "自动安装自 GitHub", "自動安裝自 GitHub");
    if (source === "auto_clawhub") return localizeText("Auto-installed from ClawHub", "自动安装自 ClawHub", "自動安裝自 ClawHub");
    return source;
};
```

#### Learned Selection Cleanup

Update the `learnedNames` filter on data load:
```typescript
const learnedNames = new Set(
    list.filter((s: NLSkillDefinition) => isLearnedSource(s.source)).map((s: NLSkillDefinition) => s.name)
);
```

### 7. Files Changed

| File | Change |
|------|--------|
| `corelib/types.go` | Add `IsLearnedSource()`, update Source comment |
| `gui/capability_gap_detector.go` | Set `auto_hub`/`auto_github` source in `Resolve()` |
| `gui/im_message_handler.go` | Set `auto_` prefix in `installAndExecuteSkill()` |
| `gui/skill_backup.go` | Use `IsLearnedSource()` in `ExportLearnedSkillsZip` |
| `gui/app_nl_skills.go` | Use `IsLearnedSource()` in `CleanupStaleSkills` |
| `gui/frontend/src/components/remote/SkillsManagementPanel.tsx` | Add `isLearnedSource()`, update filters, icons, tooltips |

## Correctness Properties

### Property 1: IsLearnedSource Partitions All Known Sources

For all known source values (`"manual"`, `"learned"`, `"hub"`, `"crafted"`, `"file"`, `"zip_import"`, `"github"`, `"clawhub"`, `"auto_hub"`, `"auto_github"`, `"auto_clawhub"`), `IsLearnedSource` returns `true` for exactly the learned set and `false` for the installed set. The two sets are disjoint and their union covers all known values.

### Property 2: Tab Classification Consistency

For any skill, `isLearnedSource(skill.source) == true` implies the skill appears in the Learned tab, and `isLearnedSource(skill.source) == false` implies the skill appears in the Installed tab. No skill appears in both tabs. Every skill appears in exactly one tab.

### Property 3: Go and TypeScript Predicates Agree

For all known source strings, the Go `IsLearnedSource(s)` and TypeScript `isLearnedSource(s)` return the same boolean value.

### Property 4: Auto-Prefix Preserves Original Source Information

For all auto-installed sources, stripping the `"auto_"` prefix yields a valid original source value (`"hub"`, `"github"`, `"clawhub"`). This ensures the original provenance is recoverable.

### Property 5: Backward Compatibility — Existing Sources Unchanged

For all source values that existed before this change (`"manual"`, `"learned"`, `"hub"`, `"crafted"`, `"file"`, `"zip_import"`, `"github"`, `"clawhub"`), the tab classification matches the previous behavior: `"learned"` and `"crafted"` go to Learned tab, all others go to Installed tab.

### Property 6: Export/Cleanup Filter Consistency

For any skill, `IsLearnedSource(skill.Source) == true` implies the skill is eligible for `ExportLearnedSkillsZip` and `CleanupStaleSkills`. The eligibility set is identical to the Learned tab set.

### Property 7: Unrecognized Source Defaults to Installed

For any source string not in the known set (e.g., empty string, arbitrary text), `IsLearnedSource` returns `false`, causing the skill to appear in the Installed tab.
