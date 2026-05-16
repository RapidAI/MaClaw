# 混合意图分类器 — 任务列表

## 已完成

- [x] Task 1: 基准测试 — Gemma embedding 意图分类准确率评估
  - `corelib/embedding/intent_bench_test.go`: 51 个测试用例
  - 结论：embedding 单独 78.4%，需要混合方案

- [x] Task 2: 设计文档
  - `.kiro/specs/hybrid-intent-classifier/design.md`

- [x] Task 3: IntentClassifier 核心实现
  - `corelib/tool/intent_classifier.go`: Layer 1 规则 + Layer 2 embedding + Layer 3 LLM
  - Layer 1: 问句模式检测（中英文正则）+ 短指令检测（≤2 runes）
  - Layer 2: 锚点 embedding cosine similarity，高置信阈值 0.78 + gap 0.10
  - Layer 3: LLMClassifyFunc 回调，精简 prompt，容错解析

- [x] Task 4: IntentClassifier 测试
  - `corelib/tool/intent_classifier_test.go`: 50 个测试用例 + Layer 3 专项测试
  - Layer 1+2 准确率 98.0%（49/50）
  - Layer 3 mock 测试 + 解析测试 + embedding+LLM 联合测试全部通过

- [x] Task 5: 集成到 `checkSessionTaskGuard`
  - `gui/im_tools_session.go`: ambiguous/unknown 分支调用 IntentClassifier
  - IntentCoding → 放行 session 创建
  - IntentQuery → 拦截，提示直接回答
  - IntentSSH/IntentContent → 映射到现有 intentSSH/intentNonCoding 处理

- [x] Task 6: 集成到 `matchConditionalKeepRules`（Router.Route）
  - `corelib/tool/router.go`: Route() 中关键词匹配后，用 IntentClassifier 补充
  - IntentSSH → 激活 ssh 工具
  - IntentBrowser → 激活 browser_* 工具
  - IntentQuery → 不激活任何条件工具

- [x] Task 7: Router 增加 IntentClassifier 字段
  - `corelib/tool/router.go`: 新增 SetIntentClassifier / IntentClassifier 方法
  - `gui/tool_router.go`: 新增 SetIntentClassifier / IntentClassifier 代理方法

- [x] Task 8: GUI 初始化接线
  - `gui/app_embedding.go`: activateEmbedderAsync 中创建 IntentClassifier
  - `gui/app_embedding.go`: buildIntentLLMFunc 封装 Layer 3 LLM 回调
  - 使用 GetMaclawLLMConfig + doSimpleLLMRequest，timeout=5s

- [x] Task 9: TUI 初始化接线
  - `tui/app.go`: router 创建后，用 memStore.Embedder() 创建 IntentClassifier

## 待完成

- [x] Task 10: 集成到 coding_tool_gate
  - `gui/coding_tool_gate.go`: 新增 `newCodingToolGateConfigWithClassifier`，接受 IntentClassifier
  - 当关键词分类为 intentCoding 但 IntentClassifier 判定为 IntentQuery 时，覆盖为 non-coding，gate 不激活
  - `gui/im_message_handler.go`: 调用点改为传入 IntentClassifier
  - 原有 `newCodingToolGateConfig` 保留为 nil-classifier 包装，15 个现有测试全部通过
