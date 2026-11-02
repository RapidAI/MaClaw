# Effective Code Statistics Report

## Scope

- Scope: tracked source files from `git ls-files`
- Included: first-party application, service, frontend, mobile and firmware source; tests are reported separately
- Excluded: docs, assets, test fixtures, build outputs, caches, generated code (e.g. `wailsjs`, `*.generated.*`, `*_gen.go`), bundled third-party code (`third_party`, `corelib/amrnb`, `corelib/opus`, firmware components) and build/CI/deploy script directories (`scripts`, `build`, `deploy`, `.github`)
- Effective LOC: non-empty, non-comment lines only
- Generated on: `2026-11-02`

## Overview

- Source files counted: `10,239`
- Physical lines: `3,964,391`
- Blank lines: `265,784`
- Comment-only lines: `237,750`
- Effective LOC: `3,460,857`
- Production: `2,157,455`
- Test: `1,303,402`

## By Code Kind

| Code Kind | Files | Effective LOC |
|---|---|---|
| Production | 6,086 | 2,157,455 |
| Test | 4,153 | 1,303,402 |

## By Business Category

| Category | Files | Effective LOC |
|---|---|---|
| Backend/Core | 7,835 | 2,627,412 |
| Frontend | 1,468 | 619,263 |
| Embedded/Firmware | 676 | 99,122 |
| Mobile | 208 | 78,438 |
| Native/Engine | 52 | 36,622 |

## Business Category x Code Kind

| Category | Code Kind | Files | Effective LOC |
|---|---|---|---|
| Backend/Core | Production | 4,376 | 1,539,511 |
| Backend/Core | Test | 3,459 | 1,087,901 |
| Embedded/Firmware | Production | 577 | 89,031 |
| Embedded/Firmware | Test | 99 | 10,091 |
| Frontend | Production | 959 | 449,028 |
| Frontend | Test | 509 | 170,235 |
| Mobile | Production | 122 | 43,263 |
| Mobile | Test | 86 | 35,175 |
| Native/Engine | Production | 52 | 36,622 |

## By Language

| Language | Files | Effective LOC |
|---|---|---|
| Go | 7,689 | 2,592,262 |
| TypeScript/TSX | 607 | 319,318 |
| JavaScript | 174 | 119,522 |
| TypeScript | 606 | 113,803 |
| C | 279 | 70,797 |
| CSS | 54 | 60,205 |
| Dart | 144 | 52,101 |
| Python | 95 | 38,208 |
| Assembly | 44 | 35,443 |
| HTML | 69 | 24,791 |
| PowerShell | 100 | 12,977 |
| C/C Header | 289 | 12,244 |
| Shell | 13 | 2,790 |
| Batch | 22 | 2,768 |
| Objective-C | 4 | 1,105 |
| C++ | 1 | 1,065 |
| YAML | 27 | 980 |
| XML | 14 | 391 |
| SQL | 3 | 51 |
| Swift | 4 | 33 |
| Kotlin | 1 | 3 |

## By Top-Level Directory

| Directory | Files | Effective LOC |
|---|---|---|
| guiapp | 3,532 | 1,376,581 |
| corelib | 3,475 | 949,750 |
| hub | 956 | 413,056 |
| hubcenter | 423 | 183,166 |
| MaClawSrv | 308 | 105,234 |
| iot-agentos | 633 | 94,510 |
| mobile | 208 | 78,438 |
| datasrv | 114 | 62,867 |
| tui | 161 | 54,873 |
| web | 67 | 51,187 |
| desktopd | 81 | 24,571 |
| ClawMateMaker | 60 | 12,865 |
| TigerProxy | 32 | 10,808 |
| buddy | 62 | 8,357 |
| maclaw-cli | 4 | 7,198 |
| vscode-ext | 28 | 6,670 |
| cmd | 17 | 6,595 |
| (root) | 22 | 4,179 |
| Ins-maclaw | 23 | 2,968 |
| ThirdAPIDemo | 6 | 2,046 |
| internal | 12 | 1,899 |
| srvdemo | 3 | 1,586 |
| maclaw-miniprogram | 9 | 925 |
| esp32-nv3023-lcd-test | 2 | 489 |
| ecapa-raw | 1 | 39 |

## Largest Files Top 20

| File | Language | Effective LOC |
|---|---|---|
| `corelib/tts/g2p_zh_pinyin_full.go` | Go | 20,995 |
| `guiapp/frontend/src/components/pages/AppsPage.tsx` | TypeScript/TSX | 18,994 |
| `guiapp/coding_subagent.go` | Go | 16,724 |
| `corelib/embedding/tensor/avx512_kernels_amd64.s` | Assembly | 14,348 |
| `guiapp/frontend/src/components/pages/__tests__/AppsPage.test.tsx` | TypeScript/TSX | 13,523 |
| `guiapp/app.go` | Go | 13,452 |
| `guiapp/coding_subagent_test.go` | Go | 12,360 |
| `MaClawSrv/http_test.go` | Go | 10,206 |
| `corelib/agent/loop_test.go` | Go | 9,354 |
| `guiapp/frontend/src/components/ai/__tests__/AIAssistantPanel.test.tsx` | TypeScript/TSX | 9,306 |
| `datasrv/structureddata/webui.go` | Go | 9,104 |
| `datasrv/structureddata/http_test.go` | Go | 8,680 |
| `hub/internal/httpapi/mobile_handlers.go` | Go | 8,143 |
| `guiapp/frontend/src/components/filecompanion/FileCompanionWindow.test.tsx` | TypeScript/TSX | 8,095 |
| `guiapp/frontend/src/components/ai/__tests__/useAIAssistant.test.ts` | TypeScript | 7,648 |
| `corelib/agent/tools_local_search_test.go` | Go | 7,620 |
| `guiapp/app_project_search.go` | Go | 7,584 |
| `guiapp/im_agent_loop_shared.go` | Go | 7,516 |
| `guiapp/frontend/src/components/pages/AppsPage.css` | CSS | 7,373 |
| `guiapp/coding_subagent_localization.go` | Go | 7,265 |

## Excluded (for reference, not in totals)

| Group | Files | Effective LOC |
|---|---|---|
| Bundled third-party | 1,428 | 131,068 |
| Build/CI/deploy scripts | 122 | 20,829 |
