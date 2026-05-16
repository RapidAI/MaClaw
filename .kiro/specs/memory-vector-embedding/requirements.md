# Maclaw 记忆系统全面升级 — 需求文档

## 背景

当前记忆系统已完成 TUI/GUI 统一（`corelib/memory`），具备 BM25 检索、Memory Stream 三维评分、
LLM 压缩/合并、对话归档等基础能力。但与最新 agent memory 研究（Generative Agents、MemGPT）
相比，在语义检索、知识提炼、记忆组织、生命周期管理等方面存在明显差距。

## 升级目标

7 个增强方向，按依赖关系分阶段实施：

### F1: 向量检索（Embedding）
- 集成 Gemma 300M embedding（CGO 链接 RapidSpeech 静态库）
- BM25 + cosine similarity 双路融合
- 向量持久化，启动时增量补算
- build tag `noembedding` 优雅降级

### F2: 记忆反思（Reflection）
- 记忆条目积累到阈值时，LLM 从低层记忆归纳高层洞察
- 产出存为 `instruction` 或 `preference` 类型
- 与 compressor 互补：compressor 缩短文本，reflection 提炼知识
- 后台定期执行（与 auto-compress 同周期）

### F3: 记忆关联图（Memory Graph）
- Entry 新增 `RelatedIDs []string` 字段
- Save 时用 BM25/embedding 找到相关条目，建立双向链接
- Recall 时沿关联边做 1-hop 扩展
- 关联强度阈值可配置

### F4: 分层记忆（Working / Episodic / Semantic）
- 显式分层：Working（对话上下文）、Episodic（事件记录）、Semantic（抽象知识）
- 现有 category 映射到层级：conversation_summary/session_checkpoint → Episodic，user_fact/preference/instruction → Semantic
- 自动 episodic → semantic 提升：同一 fact 在多次 episodic 中反复出现时提升
- 不同层级有不同的 recall 策略和 token 预算

### F5: 主动遗忘（Ebbinghaus Forgetting Curve）
- Entry 新增 `Strength float64` 字段
- 每次 recall 命中时强度增加（间隔重复效应）
- 随时间指数衰减（遗忘曲线）
- 强度低于阈值标记为 `dormant`，不参与常规 recall 但不删除
- 替代当前粗暴的 LRU 淘汰

### F6: 记忆冲突检测（Conflict Detection）
- Save 新记忆时，用 embedding 相似度找到潜在冲突条目
- LLM 轻量判断是否矛盾
- 冲突时标记旧记忆为 `superseded`（新增状态字段）
- 被 supersede 的记忆不参与 recall 但保留历史

### F7: 跨项目记忆共享（Cross-Project Scope）
- Entry 新增 `Scope` 字段：`global` / `project`
- `global` 记忆在所有项目中参与 recall
- `project` 记忆仅在匹配项目中参与
- Save 时根据 category 自动推断 scope（user_fact/preference/instruction → global，project_knowledge/session_checkpoint → project）
- 支持手动覆盖

## 非功能需求

- 所有增强对现有 API 向后兼容（旧 JSON 文件可正常加载）
- 无 embedding 模型时所有功能正常工作（F1 降级，F3/F6 回退到 BM25）
- 记忆文件大小增长可控（500 条 + 256 维向量 + 关联图 < 5MB）
- LLM 依赖的功能（F2/F6）在 LLM 未配置时静默跳过
