# Craft Tool API Retry and Error Clarity Bugfix Design

## Overview

Two bugs in `craft_tool`'s LLM interaction layer cause cascading failures when using the 智谱编程 API (`open.bigmodel.cn`). Bug A: `generateScript()` only retries on HTTP 429 / rate-limit errors but not on 智谱's proprietary `code:"1234"` transient "网络错误" (HTTP 400), causing immediate failure on a retryable condition. Bug B: `buildCraftFailureResult()` returns raw error JSON without provider context (name + URL), so when `skill_runner` passes the failure to the main LLM, it misattributes the error to the wrong provider and drifts into configuration-fixing loops.

The fix extends `generateScript()`'s retry predicate to also match `code:"1234"` + `"网络错误"` patterns, and threads provider identity (name + URL) from `MaclawLLMConfig` through `executeCraftToolCore` into `buildCraftFailureResult` so error messages are unambiguous.

## Glossary

- **Bug_Condition (C)**: The condition that triggers the bug — (A) 智谱 API returns HTTP 400 with JSON body containing `"code":"1234"` and `"网络错误"`, (B) `buildCraftFailureResult` is called without provider context after an API failure
- **Property (P)**: The desired behavior — (A) `generateScript` retries with exponential backoff identical to 429 handling, (B) failure result includes `provider: <name> (<url>)` line
- **Preservation**: Existing retry behavior for HTTP 429, immediate failure for non-retryable errors, script execution failure formatting, and success result formatting must remain unchanged
- **generateScript**: Function in `gui/tool_craft.go` (~line 395) that calls `doSimpleLLMRequest` to generate a script, with retry logic for 429 errors
- **executeCraftToolCore**: Function in `gui/tool_craft.go` (~line 204) that orchestrates script generation, execution, and verification; calls `buildCraftFailureResult` on failure
- **buildCraftFailureResult**: Function in `gui/tool_craft.go` (~line 557) that formats the failure output string returned to `skill_runner`
- **MaclawLLMConfig**: Struct in `corelib/types.go` containing URL, Key, Model, Protocol — currently lacks provider name
- **doSimpleLLMRequest**: Function in `gui/llm_request_helper.go` that dispatches to OpenAI or Anthropic protocol; returns errors as `fmt.Errorf("HTTP %d: %s", ...)` strings

## Bug Details

### Bug Condition

The bug manifests when `generateScript()` calls `doSimpleLLMRequest` and the 智谱 API returns an HTTP 400 response with a JSON body like `{"type":"error","error":{"message":"网络错误，错误id：...，请稍后重试","code":"1234"}}`. The existing retry predicate only checks for `"429"`, `"rate limit"`, and `"too many requests"` substrings, so this error falls through to the `return "", err` immediate-failure path. Simultaneously, `buildCraftFailureResult()` receives no provider context, so the raw JSON error is embedded in the failure result without attribution.

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type LLMErrorResponse
  OUTPUT: boolean

  RETURN (input.errorMessage CONTAINS '"code":"1234"'
         OR input.errorMessage CONTAINS '"code": "1234"')
         AND input.errorMessage CONTAINS '网络错误'
         AND NOT (input.errorMessage CONTAINS '429'
                  OR input.errorMessage CONTAINS 'rate limit'
                  OR input.errorMessage CONTAINS 'too many requests')
END FUNCTION
```

### Examples

- **Example 1**: 智谱 API returns `HTTP 400: {"type":"error","error":{"message":"网络错误，错误id：20250715...，请稍后重试","code":"1234"}}` → `generateScript` fails immediately instead of retrying → `buildCraftFailureResult` returns raw JSON without provider name → LLM misattributes to 讯飞星辰
- **Example 2**: 智谱 API returns `HTTP 400: {"error":{"code": "1234", "message": "网络错误"}}` (variant spacing) → same immediate failure
- **Example 3**: 智谱 API returns `HTTP 429: Too Many Requests` → existing retry logic handles correctly (not a bug, preserved)
- **Edge Case**: Error contains both `"code":"1234"` and `"429"` → existing 429 check matches first, retry triggered (correct behavior, no change needed)

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- HTTP 429 / "rate limit" / "too many requests" errors continue to trigger exponential backoff retry (2s→4s→8s, max 3 retries)
- Non-retryable errors (HTTP 401, 403, network timeout, TLS errors, etc.) continue to fail immediately without retry
- Script execution failures (non-API errors) in `buildCraftFailureResult` continue to use existing `failure_category` classification and advice
- `buildCraftSuccessResult` output format is completely unchanged
- `shouldRetryCraftAttempt` logic for script-level retries is unchanged
- `doSimpleLLMRequest` / `doSimpleAnthropicRequest` error handling is unchanged

**Scope:**
All inputs that do NOT involve 智谱 `code:"1234"` errors should be completely unaffected by this fix. This includes:
- All HTTP 429 rate limit errors (existing retry path)
- All non-429, non-code:1234 errors (immediate failure path)
- All successful LLM responses
- All script execution, verification, and registration flows

## Hypothesized Root Cause

Based on the bug description, the most likely issues are:

1. **Incomplete Retry Predicate**: `generateScript()`'s retry check at ~line 410 only matches `"429"`, `"rate limit"`, and `"too many requests"` substrings. 智谱's `code:"1234"` transient error uses HTTP 400 (not 429) and a Chinese error message that doesn't contain any of these English keywords. The error is retryable (智谱's own message says "请稍后重试") but the predicate doesn't recognize it.

2. **No Provider Identity in MaclawLLMConfig**: `MaclawLLMConfig` struct has `URL` and `Model` but no `Name` field. The provider name (e.g., "智谱编程") is only available in `GetMaclawLLMConfig()` via `data.Current` but is not passed through to `generateScript` or `buildCraftFailureResult`. Without the name, error messages can only include the URL.

3. **Raw Error Passthrough**: `buildCraftFailureResult` embeds `attempt.VerificationMessage` (which contains the raw `doSimpleLLMRequest` error string) directly into the output. For API errors, this is the raw HTTP response body (JSON), which the main LLM then misinterprets.

4. **Missing Provider Context in executeCraftToolCore**: `executeCraftToolCore` has access to `cfg` (which has `cfg.URL`) but doesn't pass provider info to `buildCraftFailureResult`. The function signature of `buildCraftFailureResult(request, attempt)` has no way to receive provider context.

## Correctness Properties

Property 1: Bug Condition - 智谱 code:1234 Errors Trigger Retry

_For any_ error from `doSimpleLLMRequest` where the error message contains `"code":"1234"` (or `"code": "1234"`) AND `"网络错误"`, the fixed `generateScript` function SHALL retry with the same exponential backoff strategy as HTTP 429 errors (2s→4s→8s, max 3 retries), rather than failing immediately.

**Validates: Requirements 2.1**

Property 2: Preservation - Non-code:1234 Error Behavior Unchanged

_For any_ error from `doSimpleLLMRequest` where the error message does NOT match the code:1234 pattern AND does NOT match the existing 429/rate-limit pattern, the fixed `generateScript` function SHALL produce the same immediate-failure behavior as the original function, preserving the fail-fast path for non-retryable errors.

**Validates: Requirements 3.1, 3.2**

Property 3: Bug Condition - Failure Result Includes Provider Context

_For any_ call to `buildCraftFailureResult` where provider info (name and/or URL) is available, the output string SHALL contain a `provider:` line with the provider name and URL, enabling the consuming LLM to correctly attribute the error source.

**Validates: Requirements 2.2**

Property 4: Preservation - Script Failure Results Unchanged

_For any_ call to `buildCraftFailureResult` where the failure is a script execution error (not an API error), the output string SHALL be identical to the original function's output, preserving existing `failure_category`, advice, and formatting.

**Validates: Requirements 3.3, 3.4**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/tool_craft.go`

**Function**: `generateScript` (~line 395)

**Specific Changes**:
1. **Extend retry predicate**: After the existing 429/rate-limit check, add a second condition that matches `"code":"1234"` (or `"code": "1234"`) AND `"网络错误"` in the error message. When matched, set `lastErr = err` and `continue` (same as 429 path).
2. **Update exhaustion message**: When all retries are exhausted, check whether `lastErr` was a code:1234 error. If so, return a human-readable message like `"智谱 API 服务端临时故障（code:1234），已重试 N 次仍失败。请稍后再试。"` instead of the 429-specific message.

**Function**: `executeCraftToolCore` (~line 204)

**Specific Changes**:
3. **Extract provider info**: After `cfg := app.GetMaclawLLMConfig()`, also extract the provider name. Since `MaclawLLMConfig` lacks a `Name` field, either: (a) add a `ProviderName string` field to `MaclawLLMConfig` and populate it in `GetMaclawLLMConfig()`, or (b) call `app.GetMaclawLLMProviders()` to get `data.Current` as the provider name. Option (a) is cleaner.
4. **Pass provider info to buildCraftFailureResult**: Pass the provider name and URL to `buildCraftFailureResult` so it can include them in the output.

**File**: `corelib/types.go`

**Specific Changes**:
5. **Add ProviderName to MaclawLLMConfig**: Add `ProviderName string` field to the struct.

**File**: `gui/app_maclaw_llm.go`

**Specific Changes**:
6. **Populate ProviderName in GetMaclawLLMConfig**: Set `ProviderName: p.Name` (which is `data.Current`) in the returned `MaclawLLMConfig`.

**Function**: `buildCraftFailureResult` (~line 557)

**Specific Changes**:
7. **Accept provider info parameter**: Change signature to accept provider name and URL (either as separate params or a small struct).
8. **Inject provider line**: After the `verification:` line, add `provider: <name> (<url>)` when provider info is available.
9. **Humanize API error messages**: When `attempt.VerificationMessage` contains raw JSON API error patterns (e.g., `"code":"1234"`), replace the raw JSON with a human-readable summary.

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write unit tests for `generateScript` that mock `doSimpleLLMRequest` to return errors matching the code:1234 pattern. Run these tests on the UNFIXED code to observe immediate failure (no retry).

**Test Cases**:
1. **Code:1234 No Retry Test**: Mock `doSimpleLLMRequest` to return `fmt.Errorf("HTTP 400: {\"error\":{\"code\":\"1234\",\"message\":\"网络错误\"}}")` → on unfixed code, `generateScript` returns error immediately without retrying (will fail assertion that retry occurred)
2. **Code:1234 Variant Spacing Test**: Mock error with `"code": "1234"` (space after colon) → same immediate failure on unfixed code
3. **Failure Result No Provider Test**: Call `buildCraftFailureResult` with an API error attempt → output lacks provider info (will fail assertion that provider line exists)

**Expected Counterexamples**:
- `generateScript` returns error on first code:1234 occurrence without sleeping/retrying
- `buildCraftFailureResult` output contains raw JSON error but no `provider:` line
- Possible causes: retry predicate only matches 429 patterns; `buildCraftFailureResult` has no provider info parameter

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  retryCount, result := generateScript_fixed(cfg, request, runtimes, previous, client)
  ASSERT retryCount > 0  // at least one retry attempted
  IF all retries exhausted THEN
    ASSERT result.error CONTAINS "code:1234" OR "临时故障"
    ASSERT result.error NOT CONTAINS raw JSON body
  END IF
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT generateScript_original(input) = generateScript_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many error message strings automatically across the input domain
- It catches edge cases like error messages that partially match patterns
- It provides strong guarantees that non-code:1234 errors are unaffected

**Test Plan**: Observe behavior on UNFIXED code first for 429 errors, auth errors, and successful responses, then write property-based tests capturing that behavior.

**Test Cases**:
1. **429 Retry Preservation**: Verify HTTP 429 errors still trigger retry with same backoff timing
2. **Auth Error Preservation**: Verify HTTP 401/403 errors still fail immediately
3. **Success Path Preservation**: Verify successful LLM responses still return script content
4. **Script Failure Preservation**: Verify `buildCraftFailureResult` for script execution errors (non-API) produces identical output with and without provider info

### Unit Tests

- Test `generateScript` retry predicate with code:1234 error patterns (various JSON formats)
- Test `generateScript` retry predicate with 429 errors (preservation)
- Test `generateScript` with non-retryable errors (immediate failure preservation)
- Test `buildCraftFailureResult` with provider info present (includes provider line)
- Test `buildCraftFailureResult` without provider info (backward compatible, no provider line)
- Test `buildCraftFailureResult` with script execution errors (unchanged output)
- Test exhaustion message format for code:1234 vs 429 errors

### Property-Based Tests

- Generate random error message strings and verify: code:1234 + 网络错误 → retry; 429/rate-limit → retry; all others → immediate failure
- Generate random `craftAttemptResult` with varying `VerificationMessage` content and verify `buildCraftFailureResult` output includes provider line when provider info is provided, and is unchanged for script errors
- Generate random `MaclawLLMConfig` with varying ProviderName/URL and verify provider line formatting

### Integration Tests

- Test full `executeCraftToolCore` flow with mocked LLM returning code:1234 then success → verify retry happened and script was generated
- Test full `executeCraftToolCore` flow with mocked LLM returning code:1234 exhausting all retries → verify failure result includes provider info and human-readable message
- Test `skill_runner` craft_tool case receives failure result with provider context
