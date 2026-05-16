# Requirements Document

## Introduction

iWorkerCenter 作为所有 DiWorker LLM 请求的统一转发中心，当前的 `rankProviders()` 仅依赖简单关键词匹配和 Priority 分数来选择 Provider。本特性在代理层增加"工作类型理解"能力：在转发请求之前，先基于规则分析请求内容判断工作类型（Work Type），再根据工作类型的复杂度和质量要求映射到不同成本等级（Cost Tier），最终选择对应的模型服务。分类结果记录审计日志，管理员可配置路由规则。

## Glossary

- **Task_Classifier**: iWorkerCenter 中负责分析请求内容并判定工作类型的组件，运行在 `handleChatCompletions` 转发路径中
- **Work_Type**: 对 LLM 请求内容的分类标签，例如 `document_writing`、`data_analysis`、`quality_report`、`simple_qa`、`table_formatting`、`production_report`
- **Cost_Tier**: 按模型成本和能力划分的等级，包含 `high`（复杂分析、正式文档）、`medium`（结构化输出、摘要）、`low`（简单问答、格式化）
- **Routing_Rule**: 管理员可配置的映射规则，定义 Work_Type 到 Cost_Tier 以及 Cost_Tier 到 Provider 的对应关系
- **Classification_Log**: 记录每次分类决策的审计日志条目，包含请求摘要、判定的 Work_Type、Cost_Tier、选中的 Provider 和耗时
- **Provider**: iWorkerCenter 中已注册的 LLM 服务端点，具有 ID、Protocol、Model、Priority、Features、Cost_Tier 等属性
- **DiWorker**: 面向终端用户的数字化同事客户端，通过 iWorkerCenter 的 `/v1/chat/completions` 端点提交 LLM 请求
- **Role**: 数字员工的角色定义（office、data、production、quality、general），每个角色有默认能力和适用任务类型

## Requirements

### Requirement 1: Rule-Based Work Type Classification

**User Story:** As iWorkerCenter, I want to classify incoming LLM requests into work types using lightweight rule-based analysis, so that I can route them to cost-appropriate model services without adding significant latency.

#### Acceptance Criteria

1. WHEN a chat completion request is received, THE Task_Classifier SHALL extract the `task_type` field and message content from the request body and determine a Work_Type within 5 milliseconds
2. WHEN the request contains an explicit `task_type` value matching a configured Work_Type keyword mapping, THE Task_Classifier SHALL use the keyword-matched Work_Type as the classification result
3. WHEN the request `task_type` is "自由输入" or absent, THE Task_Classifier SHALL analyze the message content using keyword matching rules to determine the Work_Type
4. THE Task_Classifier SHALL support the following built-in Work_Type values: `document_writing`, `data_analysis`, `quality_report`, `production_report`, `table_formatting`, `long_text_summary`, `simple_qa`
5. WHEN no keyword rule matches the request content, THE Task_Classifier SHALL assign the default Work_Type `simple_qa`
6. IF the Task_Classifier encounters a malformed request body, THEN THE Task_Classifier SHALL skip classification and proceed with the existing `rankProviders` fallback logic

### Requirement 2: Cost Tier Mapping

**User Story:** As iWorkerCenter, I want to map each classified Work_Type to a Cost_Tier, so that complex tasks use high-capability models and simple tasks use cost-efficient models.

#### Acceptance Criteria

1. THE Routing_Rule SHALL define a mapping from each Work_Type to exactly one Cost_Tier
2. THE Routing_Rule SHALL provide the following default mappings: `document_writing` → `high`, `data_analysis` → `high`, `quality_report` → `high`, `production_report` → `medium`, `table_formatting` → `medium`, `long_text_summary` → `medium`, `simple_qa` → `low`
3. WHEN a Work_Type has no explicit Cost_Tier mapping configured, THE Routing_Rule SHALL map the Work_Type to the `medium` Cost_Tier as a fallback
4. THE Provider SHALL include a `cost_tier` attribute with a value of `high`, `medium`, or `low`
5. WHEN a Cost_Tier is determined, THE Task_Classifier SHALL filter available Providers to those matching the Cost_Tier, then rank them by Priority score

### Requirement 3: Provider Selection with Cost Tier Awareness

**User Story:** As iWorkerCenter, I want the provider selection to prefer providers matching the determined Cost_Tier while preserving the existing fallback mechanism, so that cost optimization does not compromise availability.

#### Acceptance Criteria

1. WHEN providers matching the determined Cost_Tier are available, THE Task_Classifier SHALL rank those providers by Priority and select the highest-ranked one
2. WHEN no provider matching the determined Cost_Tier is available, THE Task_Classifier SHALL fall back to the existing `rankProviders` logic using all enabled providers
3. WHEN the selected provider fails, THE Task_Classifier SHALL try the next provider in the ranked list, including providers from other Cost_Tiers if the same-tier providers are exhausted
4. WHEN the request explicitly specifies a provider ID in the `model` field, THE Task_Classifier SHALL bypass Cost_Tier filtering and use the specified provider directly

### Requirement 4: Role-Based Routing Enhancement

**User Story:** As iWorkerCenter, I want to incorporate the colleague's role into routing decisions, so that office colleagues get document-optimized models and data colleagues get analysis-optimized models.

#### Acceptance Criteria

1. WHEN the request message content contains a colleague name that maps to a known Role, THE Task_Classifier SHALL use the Role as a secondary routing signal
2. WHILE a Role-based provider preference is configured (e.g., office → document-capable model, quality → causal-analysis model), THE Task_Classifier SHALL boost the Priority score of providers matching the Role preference by a configurable weight
3. WHEN both Work_Type Cost_Tier and Role preference point to different providers, THE Task_Classifier SHALL prioritize the Cost_Tier selection and use Role preference only as a tiebreaker within the same Cost_Tier

### Requirement 5: Classification Audit Logging

**User Story:** As an administrator, I want every classification decision to be logged, so that I can audit routing behavior and track cost distribution across work types.

#### Acceptance Criteria

1. WHEN a classification decision is made, THE Task_Classifier SHALL write a Classification_Log entry containing: timestamp, request_id, detected Work_Type, determined Cost_Tier, selected Provider ID, classification latency in milliseconds, and a truncated request summary (first 200 characters of user message)
2. THE Classification_Log SHALL be written to the standard Go logger with a `[TaskRoute]` prefix for structured log parsing
3. IF writing the Classification_Log fails, THEN THE Task_Classifier SHALL proceed with the request without blocking the forwarding path

### Requirement 6: Admin-Configurable Routing Rules

**User Story:** As an administrator, I want to configure which work types map to which cost tiers and which providers belong to which cost tiers, so that I can adjust routing behavior without code changes.

#### Acceptance Criteria

1. THE Routing_Rule configuration SHALL be stored in the `~/.iworkercenter/settings.json` file alongside existing provider configuration
2. THE Routing_Rule configuration SHALL include: a `work_type_keywords` map (Work_Type → list of keyword strings), a `work_type_tier` map (Work_Type → Cost_Tier), and a `role_provider_boost` map (Role code → list of preferred Provider IDs)
3. WHEN the `settings.json` file does not contain routing rule configuration, THE Task_Classifier SHALL use built-in default rules
4. WHEN the `settings.json` file is updated, THE Task_Classifier SHALL reload the routing rules on the next request without requiring a server restart
5. THE Provider configuration in `settings.json` SHALL be extended with an optional `cost_tier` field; WHEN the field is absent, THE Task_Classifier SHALL assign the provider a default Cost_Tier of `medium`

### Requirement 7: Latency Budget Compliance

**User Story:** As iWorkerCenter, I want the classification step to complete within a strict latency budget, so that the proxy path does not add noticeable delay to end users.

#### Acceptance Criteria

1. THE Task_Classifier SHALL complete the entire classification process (Work_Type detection + Cost_Tier mapping + provider filtering) within 10 milliseconds for any single request
2. THE Task_Classifier SHALL use only in-memory data structures for keyword matching and rule lookup, with no external service calls during classification
3. WHEN the classification process exceeds the 10-millisecond budget, THE Task_Classifier SHALL abort classification and fall back to the existing `rankProviders` logic
