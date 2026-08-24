# 干净工作集与按需检索（中文摘要）

英文权威稿：clean-working-set-on-demand-retrieval.md（rev 3.12）。本文件只作摘要。

## 决策

仓库正文用工具拉取，不作为首轮系统提示填料。新任务工作集为空（目录 + 拉取提示）。宿主不在 iteration 0 做 BM25 注入。

## 已落地

- Phase 1：applyPlanningBudget 先处理 required 波次，optional 再逐个填入。optional 超预算是 Omitted，不能把 required 打成 planning_budget_exceeded。
- Phase 2：intent.WantsAmbientRetrieval(primary)，只看主标签。
- Phase 3：TUI/Core 停止自动注入；共享 PromptKnowledgeBaseRules 改为工具拉取；Core/Hub 按 helper 做 Append。
- Phase 4：非托管回合用 AppendAmbientRetrievalNeeds（可从空列表起步）得到检索 Need，再映射到 knowledge_search / memory。已删除 name-pin（不再强行加入 coding_knowledge_search）。VE / /btw 不再倾倒仓库正文。Ambient 记忆改为只读 memory.recall.agent；light close 保留 recall，仍丢掉 memory.manage.agent。宿主侧残留的系统提示注入包装（appendKnowledgeAutoRecall / AppendEnterpriseKnowledgeAutoRecall）已删除；企业库 AppendAutoRecall* 已删除。IM 目录路径不再跑 BM25/embedding。VE/btw 未用 profile 也改为 CatalogOnly。已删除 SystemPromptDeps.KnowledgeAutoRecall 钩子；群组联网证据只认工具结果；设置页去掉无效自动召回开关。已删除 MaxInject / ExpandKnowledgeAutoRecallQuery 等无生产调用的注入阈值；标题常量只作禁写标记。已删除 GUI 异步召回 in-flight 状态机（proactiveRecallInFlight / 带预算的 ProactiveContextForPrompt(msg)）；目录路径保持空查询。DeleteMemories 仍会使冻结快照失效。Project Tab 恢复说明不再声称产物已载入模型；未使用的 tab-seed 只保留标题/路径，不写 Content/Preview。LoadProjectContext 的 RecentProgress 只保留标题/路径。CreateProjectTabSession 不再为未使用的返回值做仓库召回。

## 何时 Append

Managed && WantsAmbientRetrieval(primary)。查找类（天气、实时、URL）与闭环效应标签不加知识库/记忆。非能力标签（non_coding 等）走非托管 Need 路径，不走 Append。

## 证据顺序

1. 本轮已有材料
2. 工具列表里有仓库工具时再拉取
3. 查找类问题用网页/URL 工具，不要先搜知识库
