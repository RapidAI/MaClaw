# Tasks

## Task 1: Add `ProviderName` field to `MaclawLLMConfig` and populate it

- [x] 1.1 Add `ProviderName string` field to `MaclawLLMConfig` struct in `corelib/types.go`
- [x] 1.2 Populate `ProviderName: p.Name` in the `MaclawLLMConfig` return value inside `GetMaclawLLMConfig()` in `gui/app_maclaw_llm.go`

## Task 2: Extend `generateScript()` retry predicate to match 智谱 code:1234 errors

- [x] 2.1 In `generateScript()` in `gui/tool_craft.go`, extend the retry predicate after the existing 429/rate-limit check to also match errors containing `"code":"1234"` (or `"code": "1234"`) AND `"网络错误"`. When matched, set `lastErr = err` and `continue` (same retry path as 429).
- [x] 2.2 Update the retry-exhaustion error message: detect whether `lastErr` was a code:1234 error and return a human-readable message like `"智谱 API 服务端临时故障（code:1234），已重试 N 次仍失败。请稍后再试。"` instead of the 429-specific message.

## Task 3: Thread provider info through `executeCraftToolCore` into `buildCraftFailureResult`

- [x] 3.1 In `executeCraftToolCore()` in `gui/tool_craft.go`, extract `providerName` and `providerURL` from `cfg.ProviderName` and `cfg.URL` after obtaining `cfg`.
- [x] 3.2 Change `buildCraftFailureResult` signature to accept provider name and URL (e.g., add `providerName string, providerURL string` parameters or a small struct).
- [x] 3.3 In `buildCraftFailureResult`, inject a `provider: <name> (<url>)` line after the `verification:` line when provider info is non-empty.
- [x] 3.4 Update all call sites of `buildCraftFailureResult` in `executeCraftToolCore` (runtime-missing failure at ~line 237 and final failure at ~line 303) to pass provider info.

## Task 4: Humanize API error messages in failure results

- [x] 4.1 In `buildCraftFailureResult`, when `attempt.VerificationMessage` contains raw JSON API error patterns (e.g., `"code":"1234"` or `"type":"error"`), replace the raw JSON with a human-readable summary (e.g., "API 服务端临时故障（code:1234），请稍后重试") in the `⚠️` message line.

## Task 5: Write tests for retry predicate extension

- [x] 5.1 [PBT-exploration] Write a property-based test that generates error messages matching the code:1234 + 网络错误 pattern and asserts `generateScript` retries (does not return immediately). Run on UNFIXED code to confirm the bug (expect failure — function returns error without retrying).
- [x] 5.2 [PBT-fix] After applying the fix from Task 2, re-run the exploration test from 5.1 to verify it passes — all code:1234 errors now trigger retry.
- [x] 5.3 [PBT-preservation] Write a property-based test that generates random error messages NOT matching code:1234 AND NOT matching 429/rate-limit patterns, and asserts the fixed `generateScript` produces the same immediate-failure behavior as the original. This ensures no regression for non-retryable errors.

## Task 6: Write tests for provider context in failure results

- [x] 6.1 Write a unit test that calls `buildCraftFailureResult` with provider info (name="智谱编程", url="https://open.bigmodel.cn/api/anthropic") and a script execution failure, and asserts the output contains a `provider:` line with both name and URL.
- [x] 6.2 Write a unit test that calls `buildCraftFailureResult` without provider info (empty strings) and asserts the output does NOT contain a `provider:` line — backward compatibility preserved.
- [x] 6.3 Write a unit test that calls `buildCraftFailureResult` with a code:1234 API error in `VerificationMessage` and asserts the `⚠️` line contains a human-readable message, not raw JSON.

## Task 7: Verify compilation and existing tests

- [x] 7.1 Run `go build ./gui/...` to verify the Go code compiles after all changes.
- [x] 7.2 Run existing `gui/tool_craft_test.go` tests (if any) to verify no regressions.
