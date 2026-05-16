# 需求文档：智能工具路由 v2

## 概述

升级 Maclaw 的工具选择管线，从单一 BM25/向量检索升级为三层递进架构：描述增强 → 经验感知打分 → 按需工具加载。目标是提升工具选择准确率、支持工具规模扩展、利用历史经验优化选择。

## 背景

当前工具选择管线（`corelib/tool/router.go` + `builder.go` + `hybrid.go`）存在三个瓶颈：
1. 检索信号单一：只有 BM25 + cosine 两个信号，缺少历史经验信号
2. 工具描述质量参差：MCP/Skill 工具描述简陋，导致检索召回率低
3. 静态 budget 限制：`MaxToolBudget=28` 硬上限，工具规模扩展受限

参考研究：
- RAG-MCP (2025): 语义检索减少 50%+ prompt tokens，工具选择准确率提升 3x
- ToolScale TDWA (2025): 加权嵌入策略提升工具描述质量
- MCP-Zero (2025): 按需工具发现，突破 budget 限制
- Control Plane as a Tool (2025): meta-tool 模式解耦工具编排

## 需求列表

### Layer 1: Description Enrichment（工具描述增强）

- REQ-1.1: 为每个注册工具维护 synthetic queries（合成查询），作为额外的检索索引文本
- REQ-1.2: 内置工具的 synthetic queries 硬编码，不依赖 LLM
- REQ-1.3: MCP/Skill 工具首次注册时用 LLM 自动生成 synthetic queries，缓存到本地
- REQ-1.4: enrichment 数据持久化到 `~/.maclaw/data/tool_enrichments.json`
- REQ-1.5: Router 和 DynamicToolBuilder 的 BM25 索引文本从 `name+desc+tags` 改为使用 enriched text
- REQ-1.6: 无 enrichment 时回退到原始 `name+desc+tags`，保持向后兼容

### Layer 2: Experience-Aware Scoring（经验感知打分）

- REQ-2.1: 维护工具使用日志（tool name, query tokens, success/fail, timestamp）
- REQ-2.2: 使用日志持久化到 `~/.maclaw/data/tool_usage.json`，ring buffer 上限 2000 条
- REQ-2.3: 打分公式从单信号改为三信号融合：`α*retrieval + β*experience + γ*priority`
- REQ-2.4: experience score 基于 token overlap + recency decay + success weight 计算
- REQ-2.5: 在 agent loop 的 tool_call 执行完成后自动记录使用结果
- REQ-2.6: tracker 为 nil 时回退到原始打分逻辑，保持向后兼容

### Layer 3: Lazy Tool Loading（按需工具加载）

- REQ-3.1: 新增 `discover_tool` 内置工具，允许 LLM 按需搜索未加载的工具
- REQ-3.2: discover_tool 从 Registry 全量工具中检索，返回 top-5 匹配工具的名称和描述
- REQ-3.3: 匹配的工具自动加入当前会话的 session tools，后续轮次永远包含
- REQ-3.4: session tools 在新会话开始时清空
- REQ-3.5: discover_tool 占用 CoreToolNames 的 1 个 slot，不额外增加 budget

## 非功能需求

- NFR-1: 所有新增组件通过接口注入，不修改现有公共 API 签名
- NFR-2: 每个 Layer 独立可用，任一 Layer 缺失时系统回退到原始行为
- NFR-3: enrichment 生成和 usage tracking 不阻塞主请求路径（异步/后台）
- NFR-4: 新增代码总量控制在 ~500 行，对现有文件的改动控制在每文件 30 行以内
