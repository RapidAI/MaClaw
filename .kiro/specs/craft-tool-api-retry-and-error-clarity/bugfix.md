# Bugfix Requirements Document

## Introduction

`craft_tool` 的 `generateScript()` 函数在调用智谱编程 API（`https://open.bigmodel.cn/api/anthropic`）时，遇到 HTTP 400 + `code:"1234"`（智谱服务端间歇性"网络错误"）不重试，直接失败。同时 `buildCraftFailureResult()` 返回的错误信息缺少 provider 上下文（provider 名称和 API URL），导致 skill_runner 将原始 JSON 错误透传给 LLM 后，LLM 误判为 default_tool_provider（讯飞星辰）配置问题，触发漂移检测器中止。

两个问题叠加：A）可重试的瞬时故障未重试；B）错误信息缺少 provider 上下文导致 LLM 误判并漂移。

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN 智谱编程 API 返回 HTTP 400 + JSON body 包含 `"code":"1234"` 和 `"网络错误"` THEN `generateScript()` 的重试逻辑不匹配此错误（仅匹配 429/rate limit/too many requests），`doSimpleLLMRequest` 返回的 error 直接透传为最终失败

1.2 WHEN `generateScript()` 因智谱 code:1234 错误失败 THEN `executeCraftToolCore()` 调用 `buildCraftFailureResult()` 构造错误结果，该结果不包含当前 LLM provider 名称（如"智谱编程"）和 API URL（如 `https://open.bigmodel.cn/api/anthropic`），LLM 无法判断是哪个 provider 出错

1.3 WHEN skill_runner 将 craft_tool 的失败结果透传给主 LLM THEN LLM 看到原始 JSON 错误 `{"type":"error","error":{"message":"网络错误，错误id：...，请稍后重试","code":"1234"}}` 后错误推断为 default_tool_provider 配置问题，开始尝试用 manage_config/discover/craft_tool/bash 修改配置，最终触发漂移检测器中止

### Expected Behavior (Correct)

2.1 WHEN 智谱编程 API 返回 HTTP 400 + JSON body 包含 `"code":"1234"` 和 `"网络错误"` THEN `generateScript()` SHALL 将此错误视为可重试的瞬时故障，使用与 HTTP 429 相同的指数退避策略（2s→4s→8s，最多 3 次）进行重试

2.2 WHEN `generateScript()` 所有重试耗尽仍失败 THEN `buildCraftFailureResult()` SHALL 在错误结果中包含当前 LLM provider 的名称和 API URL，格式如 `provider: 智谱编程 (https://open.bigmodel.cn/api/anthropic)`，使 LLM 能准确识别故障来源

2.3 WHEN craft_tool 因 API 瞬时故障（code:1234）最终失败 THEN 错误信息 SHALL 使用人类可读的描述（如"智谱 API 服务端临时故障（code:1234），已重试 N 次仍失败"）替代原始 JSON 透传，防止 LLM 对原始错误 JSON 做错误推断

### Unchanged Behavior (Regression Prevention)

3.1 WHEN `generateScript()` 遇到 HTTP 429 / rate limit / too many requests 错误 THEN the system SHALL CONTINUE TO 使用现有的指数退避重试逻辑（2s→4s→8s，最多 3 次）

3.2 WHEN `generateScript()` 遇到非 429 且非 code:1234 的错误（如 HTTP 401 认证失败、HTTP 403 权限不足、网络超时等） THEN the system SHALL CONTINUE TO 立即返回失败，不进行重试

3.3 WHEN craft_tool 因脚本执行失败（非 API 错误）而失败 THEN `buildCraftFailureResult()` SHALL CONTINUE TO 返回现有的 failure_category 分类和 advice 建议，不受 provider 上下文注入影响

3.4 WHEN craft_tool 执行成功 THEN `buildCraftSuccessResult()` SHALL CONTINUE TO 返回现有的成功结果格式，不受本次修改影响
