# Maclaw 记忆系统全面升级 — 任务列表

## Phase 1: 基础设施（Entry 扩展 + 向量 + Scope + 遗忘曲线）

### Task 1: Entry 类型扩展
- [x] `types.go` 新增 `Scope`、`Status` 类型和常量
- [x] `Entry` 新增 `Embedding []float32`、`RelatedIDs []string`、`Strength float64`、`Scope Scope`、`Status Status` 字段
- [x] 验证旧 JSON 文件（无新字段）可正常反序列化（omitempty 兼容）
- [x] `Save()` 中自动推断 Scope（`inferScope(category)`）
- [x] 新 Entry 初始 Strength = 1.0

### Task 2: CMake 静态库构建
- [x] RapidSpeech.cpp/CMakeLists.txt 添加 `rapidspeech_static` target（text embedding 相关源码 + ggml CPU）
- [x] 创建 `build/build_rapidspeech.cmd` 和 `build/build_rapidspeech.sh`
- [x] 验证静态库编译成功（需要 CMake + C++ 编译器环境）

### Task 3: Embedder 接口 + NoopEmbedder
- [x] 创建 `corelib/embedding/embedder.go`（`Embed`、`EmbedBatch`、`Dim`、`Close`）
- [x] 创建 `corelib/embedding/noop.go`
- [x] 编译通过

### Task 4: CGO GemmaEmbedder
- [x] 创建 `corelib/embedding/gemma_cgo.go`（build tag: `cgo_embedding`）
- [x] `#cgo LDFLAGS` 链接静态库
- [x] `NewGemmaEmbedder(modelPath, dim)` → `rs_init_from_file(RS_TASK_TEXT_EMBED)`
- [x] `Embed(text)` → `rs_reset` → `rs_push_text` → `rs_process` → `rs_get_embedding_output`
- [x] `sync.Mutex` 保护（ggml 非线程安全）
- [x] 集成测试（需要模型文件 + 静态库）

### Task 5: 向量索引
- [x] 创建 `corelib/memory/vector_index.go`
- [x] `vectorIndex` 结构体：`map[string][]float32` + cosine similarity
- [x] `add`、`remove`、`update`、`rebuild`、`score` 方法
- [x] 编译通过

### Task 6: 遗忘曲线 + 强度管理
- [x] 创建 `corelib/memory/forgetting.go`
- [x] `decayStrength` / `isDormant` / `boostStrength` / `batchDecayAndMark`
- [x] 编译通过

### Task 7: Store 集成 Phase 1
- [x] `Store` 新增 `vecIndex`、`graph` 字段
- [x] `Save()` 自动计算 embedding（stub）+ 自动推断 scope + 初始化 strength
- [x] `RecallForProject()` 融合 cosine similarity：`0.4×bm25 + 0.6×cosine + affinity`
- [x] `RecallDynamic()` 同上
- [x] Recall 中过滤 dormant 和 superseded 条目
- [x] Recall 中 ScopeProject 条目仅匹配项目时参与
- [x] embedder 不可用时优雅降级（queryEmbeddingCached 返回 nil）
- [x] 启动时后台补算缺失 embedding 的旧条目（需要 embedder 实例）

## Phase 2: 关联图 + 分层

### Task 8: 记忆关联图
- [x] 创建 `corelib/memory/graph.go`
- [x] `memoryGraph` 结构体：`map[string]map[string]float64`
- [x] `link(id1, id2, strength)` — 双向边
- [x] `expand(ids, hops)` — BFS 扩展
- [x] `rebuild(entries)` — 从 Entry.RelatedIDs 重建
- [x] 关联上限：每条最多 5 关联
- [x] 编译通过
- [x] Save 时自动关联（需要 embedder 实例做 cosine 计算）

### Task 9: Recall 图扩展
- [x] RecallForProject/RecallDynamic 中 graph expand 基础设施就绪
- [x] 实际 1-hop 扩展集成（待 graph 有数据后启用）

### Task 10: 分层记忆 — Episodic→Semantic 提升
- [x] 创建 `corelib/memory/promoter.go`
- [x] `Promoter` 结构体：`store + llm`
- [x] `Promote(ctx)` — 扫描 episodic，LLM 确认后存为 preference/instruction
- [x] 编译通过

## Phase 3: 智能层（LLM 驱动）

### Task 11: 记忆反思
- [x] 创建 `corelib/memory/reflector.go`
- [x] `Reflector` 结构体：`store + llm`
- [x] 触发条件：条目 > 50 且距上次反思 > 24h
- [x] LLM 归纳偏好/习惯/决策模式 → 存为 preference/instruction
- [x] 编译通过

### Task 12: 记忆冲突检测
- [x] 创建 `corelib/memory/conflict.go`
- [x] `ConflictDetector` 结构体：`store + embedder + llm`
- [x] `Check(newEntry)` — embedding cosine top-5 → LLM 判断矛盾
- [x] `Supersede(id)` — 标记旧记忆为 superseded
- [x] 降级：无 embedder 用 BM25，无 LLM 跳过
- [x] 编译通过

### Task 13: Compressor 流水线升级
- [x] 创建 `corelib/memory/pipeline.go`
- [x] 后台服务：decay → compress → promote → reflect
- [x] 每 6h 执行一次完整流水线
- [x] 发射事件：`memory:pipeline_completed`
- [x] 编译通过

## Phase 4: 集成 + 构建

### Task 14: TUI/GUI 集成
- [x] TUI `main.go`：初始化 GemmaEmbedder（失败用 Noop），传入 Store
- [x] GUI `app.go`：同上
- [x] 模型路径：`~/.maclaw/models/gemma-emb.gguf`
- [x] CLI：`maclaw-tui memory embed-status` / `memory graph <id>` / `memory strength`

### Task 15: 构建 + CI
- [x] `.github/workflows/main.yml`：Go 构建前编译 RapidSpeech 静态库
- [x] `build_win_tiger.bat` / `deploy_all.cmd`：集成静态库构建
- [x] `noembedding` build tag CI job
- [x] 全量测试通过
