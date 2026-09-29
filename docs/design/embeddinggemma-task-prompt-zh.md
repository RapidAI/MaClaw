# EmbeddingGemma 官方 Task Prompt 接入

| 字段 | 值 |
|---|---|
| 状态 | Implemented |
| 日期 | 2026-09-29 |
| 包 | `corelib/embedding`、`corelib/knowledge`、`MaClawSrv` |
| 模型制品 | `embeddinggemma-300M-Q8_0.gguf`（`embedding.DefaultModelFilename`） |
| 新增文件 | `corelib/embedding/prompt.go`、`corelib/embedding/gemma_prompt.go`、`corelib/embedding/prompt_test.go`、`corelib/knowledge/prompt_roles_test.go` |
| 修改文件 | `corelib/embedding/gemma.go`（`GemmaEmbedder` 增加 `modelName` 字段并从 GGUF 头部填充）、`corelib/knowledge/{vector_search,structured_search,image_search,import_parallel,import_prepare}.go`、`MaClawSrv/ai_models.go` + `MaClawSrv/ai_models_test.go`（服务端适配器补齐角色与标识，见「装配点审计」） |
| 相关实现 | `docs/design/gemma3-embedding-inference-optimization-plan-zh.md` |

> 本文记录一次**能力接入**：把 EmbeddingGemma 模型卡要求的 task prompt 接进 `corelib/embedding`，
> 并说明哪些消费方已经切换、哪些**刻意没有切换**以及依据的实测数据。

---

## Overview

EmbeddingGemma 是**带指令的嵌入模型**：同一个文本，加不同的 task 前缀会被投影到不同的向量空间。
此前 `corelib/embedding` 对原文直接编码，等于始终使用「无指令」这一个空间。

本次改动：

1. 在 `corelib/embedding/prompt.go` 引入 `Role` 枚举与模型卡原文模板，提供 `ApplyPrompt`；
2. `GemmaEmbedder` 实现 `RoleEmbedder`（`EmbedWithRole` / `EmbedBatchWithRole`）；
3. 通过 `EmbedAs` / `EmbedBatchAs` 让**不支持 prompt 的 Embedder（测试替身、远程服务、Noop）无感回退**；
4. `GemmaEmbedder.ModelID()` 随 prompt 档位变化，使知识库**自动**重嵌入旧向量而不是混用两个空间；
5. **知识库检索链路**（query 侧 / document 侧）切换到对应 prompt。

**没有**改动的消费方（意图分类、话题切换检测、中断相关度、子代理打分）都是**按绝对余弦标定阈值**的，
详见「落地范围」一节。

---

## 官方模板

来自模型卡 "Prompt Instructions" 章节，逐字照抄：

| 用途 | 模板 |
|---|---|
| 检索 query | `task: search result | query: {content}` |
| 检索 document | `title: {title\|none} | text: {content}` |
| 句间相似度 | `task: sentence similarity | query: {content}` |
| 分类 | `task: classification | query: {content}` |
| 聚类 | `task: clustering | query: {content}` |
| 代码检索 | `task: code retrieval | query: {content}` |
| 问答 | `task: question answering | query: {content}` |
| 事实核查 | `task: fact checking | query: {content}` |

文档侧统一走 `ApplyPrompt(text, RoleDocument)`，title 槽保持字面量 `none` —— 理由见「实测数据」第 4 节。

---

## 实测数据

在本机用同一份 `embeddinggemma-300M-Q8_0.gguf`、同一批文本，对比 raw / prompt 两种档位。
**所有消费方都按绝对余弦判定，所以必须看绝对值的位移，而不只是排序。**

### 1. 非对称检索（24 query × 48 文档，query 与目标文档语言刻意不同）

| 档位 | 正样本 cosine | 负样本 cosine | 间隔 |
|---|---|---|---|
| raw | mean **0.8035**，p5 0.7299 | mean 0.6269，p95 0.7119 | 0.177 |
| prompt | mean **0.7041**，p5 0.6178 | mean 0.4264，p95 0.5224 | **0.278** |

- **排序变好**：混合知识库 Recall@1 `0.833 → 0.958`，Tatoeba 中→英 Recall@1 `0.930 → 0.941`。
- **绝对余弦整体下移**（正样本 0.80→0.70，负样本 0.63→0.43），但正样本 p5=0.618 仍远高于
  知识库现有的 `sim < 0.25` / `sim < 0.3` 下限，**阈值不会误杀**。
- 正负间隔从 0.177 拉大到 0.278（+57%），区分度显著提升。

### 2. 对称相似度（自建 70 对中英混合/跨语言句对）

| 档位 | 同义对(≥4) | 无关对(≤1) | 间隔 |
|---|---|---|---|
| raw | mean 0.8715 | mean 0.5930 | 0.278 |
| prompt | mean 0.9164 | mean 0.7143 | **0.202** |

**余弦整体上移约 +0.12**，且无关对抬升更多（0.59→0.71），间隔反而变窄。
若沿用 `CosineSameThreshold=0.45` / `CosineNewThreshold=0.25`，
大量原本落在「new」区间的句子会被抬进「same/unsure」区间 —— 这正是相似度链路本次不切的原因。

### 3. 质量不退化

中文 STS-B（C-MTEB STSB，1361 对）：raw ρ=`0.7301` → prompt ρ=`0.7594`。
英文 STS-B：raw ρ=`0.8236` → prompt ρ=`0.8387`。prompt 在公开 STS 上是**正向**的。

### 4. 文档侧 title 槽：放进槽里 vs 折进正文（`scripts/title_probe.py`）

模型卡的文档模板是 `title: {title|none} | text: {content}`。但 aicoder 的文本构造器
（`cardEmbeddingText` / `nodeEmbeddingText`）**已经把标题拼进正文**，所以实际喂给模型的是
`title: none | text: <标题> <正文>` —— title 槽永远是 `none`。

用同一份 48 文档 / 24 查询集合（标题取每篇的首个小句，32/48 篇可切出）实测三种写法：

| 配方 | R@1 | R@3 | MRR | pos_p5 | neg_p95 |
|---|---|---|---|---|---|
| `title: none` + 标题折进正文（**现采用**） | **0.958** | 1.000 | 0.979 | **0.6170** | **0.6409** |
| `title: <标题>` + 正文（把标题放进槽） | 0.958 | 1.000 | 0.979 | 0.6037 | 0.6580 |
| `title: none` + 丢掉标题 | 0.917 | 0.958 | 0.948 | 0.5105 | 0.6340 |
| 无 prompt | 0.875 | 1.000 | 0.924 | 0.6104 | 0.6613 |

结论：

- **把标题放进 title 槽不产生任何收益** —— R@1/R@3/MRR 完全相同，且正样本 p5 更差、负样本 p95 更差。
- **标题不能丢** —— 丢掉后 R@1 从 0.958 掉到 0.917。标题必须留在正文里。
- 因此文档侧继续用 `ApplyPrompt(text, RoleDocument)`，**不引入**带 title 参数的辅助函数
  （曾经的 `DocumentPrompt` 属无用代码，已删除）。

---

## 设计

```
Role (RoleNone/Query/Document/Similarity/Classification/Clustering/CodeRetrieval/QA/FactChecking)
  └─ PromptPrefix(role) -> 模型卡模板
  └─ ApplyPrompt(text, role)      // 受全局开关约束

RoleEmbedder { EmbedWithRole; EmbedBatchWithRole }   // GemmaEmbedder 实现
  └─ EmbedAs(emb, text, role)      // 不支持则回落 emb.Embed
  └─ EmbedBatchAs(emb, texts, role)
```

**为什么用 `EmbedAs` 而不是直接给 `Embedder` 接口加方法**：`Embedder` 在仓库里有大量测试替身
（`appEmbeddingTestEmbedder`、`cuGateStubEmbedder`、`staticEmbedder` …）。给接口加方法会一次性
打断所有实现；用可选接口 + 包级辅助函数，旧实现零改动继续工作。

**空输入不受影响（已实测，未加特判）**。一度怀疑「空查询会被加上裸模板、变成非退化常量向量、
从而越过相似度下限」，实测否掉了：`Tokenizer.Encode` 会先给文本前补一个空格
（`text = " " + text`），所以 `Encode("")` 得到 `▁` 这 **1 个 token**，
`GemmaEmbedder.Embed("")` 从来就是成功的（`gemma_infer.go` 里的 `len(tokens) == 0` 分支
对空串实际不可达），返回单位向量。也就是说**改动前后空查询都会返回结果**，
不存在「失败关闭 → 返回任意结果」的回归，因此 `ApplyPrompt` 不需要为空白加特判。

**`ModelID()` 是迁移的关键**。知识库把模型标识随向量一起落库
（`knowledge_embedding_metadata.model_id`），检索时用 `WHERE em.model_id = ?` 过滤，
回填时只选 `model_id` 不匹配的行。因此：

- 标识不变 → 不产生任何重算；
- 标识改变 → 下次 `SetEmbedder` 时后台自动重嵌入（`generation` 保护 + 10 分钟超时）。

`ModelID()` 取值（三段式：**模型标识 : 输出维度 : 配方**）：

| 档位 | ModelID |
|---|---|
| prompt 开 | `Embeddinggemma 300m Qat Q8_0 Unquantized:768:prompt-v1` |
| prompt 关 | `Embeddinggemma 300m Qat Q8_0 Unquantized:768:raw` |

第一段**不是硬编码常量**，而是在 `NewGemmaEmbedder` 里从 GGUF 头部读出来的
（`general.name`，回退 `general.basename`，再回退 `general.architecture`）。
这一点很重要：知识库原来的回退标识是 `fmt.Sprintf("%T:%d", emb, emb.Dim())`，
也就是 `*embedding.GemmaEmbedder:768` —— **换一个同样是 768 维的模型不会让索引失效**，
检索会继续拿两个不同空间的向量算余弦，安静地返回看似合理的垃圾分数。
现在换文件就会换标识，`SetEmbedder` 会自动重嵌入。

模板形状变化时必须 bump `promptRecipeVersion`，否则新旧两种配方会在同一个索引里混用。

---

## 落地范围

| 消费方 | 位置 | 任务形态 | 绝对阈值 | 本次处理 |
|---|---|---|---|---|
| 知识库检索 | `corelib/knowledge/vector_search.go`、`structured_search.go`、`image_search.go`、`import_parallel.go`、`import_prepare.go` | 非对称检索 | `sim < 0.25` / `< 0.3` 下限 | ✅ **已切**（query→`RoleQuery`，document→`RoleDocument`） || 意图分类 | `corelib/intent/classifier.go`、`layer2.go` | 分类 | 绝对分数 + gap（源码注释中出现 `0.833`/`0.72` 这类标定值） | ⛔ 未切，见下 |
| 话题切换检测 | `corelib/agent/topic_detector.go` | 对称相似度 | `CosineSameThreshold=0.45` / `CosineNewThreshold=0.25` | ⛔ 未切 |
| 中断相关度 | `guiapp/im_interrupt_handler.go` | 对称相似度 | `progress.CosineSimilarity` 比较 | ⛔ 未切 |
| 子代理打分 | `guiapp/coding_subagent_scoring.go` | 检索 | `codingSubAgentEmbeddingBaseline = 0.2` + `threshold` | ⛔ 未切 |
| **分类头（已训练权重）** | `corelib/llmpool/head*.go`、`hub/internal/httpapi/llm_class_head.go` | 分类（线性头） | `HeadDim = 256`（MRL 截断）+ 训练好的权重 | ⛔ 未切，**理由更强**，见下 |

### 注意：**「索引路径」不等于「只用 RoleDocument」**

审计角色时容易踩这个坑：`SaveText` 在写完向量之后还会跑主题关联
（`text.go:81` → `import_post.go:276` 的 `refreshSourceTopicLinksFast`），
它把**新文档的标题 / TopicHint** 当作**查询**去检索已入库的语料，因此会走 `RoleQuery`。

这是**正确**的用法，不是串档：标题在这里就是"信息需求"，被匹配的语料是以 `RoleDocument` 入库的，
`RoleQuery` 探测 + `RoleDocument` 语料恰好是**成对**的非对称检索。
所以断言「索引期间不得出现 `RoleQuery`」是**错的**（`TestKnowledgeIndexingUsesDocumentRole`
的注释里写明了这一点，防止后人"顺手修掉"）。

### 为什么其余链路本次不切

它们的判定都建立在**绝对余弦**上，而这些常数是按「无 prompt」分布标定的：

- 相似度链路：实测余弦整体 **+0.12**，会直接跨过 0.45 的 `same` 边界；
- 意图分类：分数整体抬高会让更多标签越过绝对阈值，`gap` 逻辑与「本地可验证复合对」的判定随之失真；
- 子代理打分：`baseline = 0.2` 是直接从 raw 余弦里减掉的，换档后 `embScore` 系统性偏大。

**分类头是最不能切的一个**，理由与其他链路**性质不同**：它不是"阈值需要重标定"，而是
**权重已经训练在 raw 空间上**。`hub/internal/httpapi/llm_class_head.go` 的 `embedClassHeadPreview`
走的是裸 `emb.Embed(preview)`，`corelib/llmpool/head_train.go` 用这些向量拟合一个
`HeadDim = 256` 的线性层。切档会让**所有已训练头直接失效**——必须**重训**，调阈值救不回来。

> 顺带澄清一个易踩的名字：`embedding.SharedGemma256()` 的 "256" 是**历史遗留**，
> 它命名的是消费方（那个 256 维的分类头），**不是输出宽度**。实际返回 `DefaultEmbeddingDim`（768）维；
> `head.go` 取前 256 维是**合法的 MRL 截断**。所以此处**没有**维度 bug，但**不要**照名字去假设维度，
> 要用 `Dim()`。已在该函数注释里写明。

**正确做法是先切档、再重新标定阈值**（分类头则是重训），而不是直接把 prompt 打开。
每个链路都预留了 `embedding.EmbedAs(emb, text, embedding.RoleXxx)` 这一行改动量。

---

## 装配点审计

prompt 只在**真正调用 `EmbedAs` 的那条链路上生效**。因此除了改调用点，还必须确认每个
知识库实例拿到的 embedder 都实现了 `RoleEmbedder` 与 `ModelID()` —— 否则会**静默**退回无 prompt 空间。

全仓库构造 `knowledge.NewSQLiteStore` 的位置共 9 处，其中只有 3 处会 `SetEmbedder`：

| 装配点 | embedder | 角色/标识 | 结论 |
|---|---|---|---|
| `guiapp/app_knowledge.go:416` | `*embedding.GemmaEmbedder`（768） | ✅ 原生实现 | 正常 |
| `hub/internal/httpapi/mobile_knowledge.go:106` | `embedding.NewDefaultEmbedder()` → `*GemmaEmbedder`（768） | ✅ 原生实现 | 正常 |
| `MaClawSrv/knowledge_store.go:122` | `srvAIModelEmbedderAdapter`（768，绑定 `DefaultEmbeddingDim`） | ⚠️ **原先两者都没有，且宽度是 256** | **本次修复** |

其余 6 处（`corelib/enterpriseknowledge/{client.go ×2, sync.go}`、
`corelib/knowledge/coding_store.go`、`guiapp/app_user_data_migration.go`、
`hub/internal/digitalasset/knowledge_host.go`）
**从不设置 embedder**，走 FTS-only，与 prompt 无关。

> `guiapp/im_knowledge_auto_recall.go` 的 `knowledgeAutoRecallStore` **不是**第 4 个装配点：
> 它通过 `app.openKnowledgeStore()` + `app.attachKnowledgeEmbedder(store)` 复用上面第一行那两个函数
> （即 `app_knowledge.go:366` 与 `:416`），拿到的同样是原生 `*GemmaEmbedder`。

### 查询侧 ↔ 存储侧配对（正确性的核心主张）

角色不是"加上就行"——它必须**成对**。查询向量与它要比对的存储向量必须落在同一个空间，
否则余弦照样返回、只是排序变差，从症状上看不出来。逐条核对结果（8 个调用点全覆盖）：

| 查询侧（`RoleQuery`） | 存储侧（`RoleDocument`） | 配对 |
|---|---|---|
| `vector_search.go:51` `searchByEmbedding` → 卡片 | `vector_search.go:465` `backfillCardEmbeddingsForGeneration` | ✅ |
| `vector_search.go:51` `searchByEmbedding` → 节点 | `vector_search.go:877` `BackfillNodeEmbeddingsForSources` | ✅ |
| `structured_search.go:20` `searchTableRowsByEmbedding` | `vector_search.go:551` `backfillTableRowEmbeddingsForGeneration` | ✅ |
| `image_search.go:48` `SearchImages`（检索 `NodeTypeImage` 节点） | 同上，图片就是节点，走节点回填 | ✅ |
| （查询侧无对应） | `import_parallel.go:270`、`import_prepare.go:260` 导入期卡片嵌入 | — |

两个易错点已在上面「索引路径 ≠ 只用 RoleDocument」一节说明；
`import_*` 两个是纯存储侧（导入期写卡片），没有查询侧对应项，属正常。

### 服务端适配器缺口（本次修复）

`MaClawSrv/ai_models.go` 的 `srvAIModelEmbedderAdapter` 原先只实现
`Embed` / `EmbedBatch` / `Dim` / `Close`。它是**服务端知识库唯一见过的 embedder**，所以：

- 缺 `RoleEmbedder` → `EmbedAs` 走回落分支，**服务端继续用无 prompt 空间**，
  而桌面端已经用 prompt 空间 —— 同一份功能「一半生效」，且没有任何报错。
- 缺 `ModelID` → 退回 `fmt.Sprintf("%T:%d", emb, emb.Dim())`，
  **prompt 档位不进标识**，开启 prompt 后服务端旧向量不会失效，一个索引里混两个空间。

修复内容：

1. 补 `EmbedWithRole` / `EmbedBatchWithRole`（含「prompt 关或无模板 → 直接走 `EmbedBatch`」快路径）；
2. 补 `ModelID()`；
3. 抽出 `srvAIEmbeddingDim` 常量并**绑定到 `embedding.DefaultEmbeddingDim`**（即 768，见下节），
   同时用于 `NewGemmaEmbedder(modelPath, srvAIEmbeddingDim)` 与 `Dim()` ——
   该维度是向量空间标识的一部分，两处必须一致；
4. `ModelID()` 第一段取 `filepath.Base(manager.modelPath(...))` 而非常量，
   这样 `MACLAW_EMBEDDING_MODEL_PATH` 指向别的权重文件时标识会跟着变（`modelPath` 是纯查表，
   不需要权重加载完成，因此不会在加载完成时二次变化、触发第二轮重嵌入）。

`MaClawSrv/ai_models_test.go` 新增 7 条用例（含编译期 `var _ embedding.RoleEmbedder = srvAIModelEmbedderAdapter{}`），
利用既有的 `countingSrvEmbedder`（第二个分量 = `len([]rune(text))+1`）直接断言**前缀真的进了模型**；
其中 `TestSrvAIEmbeddingDimMatchesProductionWidth` 钉住「服务端宽度 == 生产宽度」与「`Dim()` == 常量」两层一致，
防止未来再漂移。

### 维度已统一到 768（本次修复）

**原先的差异**：`MaClawSrv` 用 **256 维**，而 `embedding.DefaultEmbeddingDim = 768`。
`init.go` 的注释记录过 256 截断会让 CJK 区分度塌陷（天气查询下 `git_status` 压过 `web_search`）。
这不是 bug（维度是 `ModelID` 的一段，`…:256:prompt-v1` 与 `…:768:prompt-v1` 天然隔离，不会跨空间混用），
但意味着服务端的中文检索质量一直低于桌面端，而且两个面各维护一个空间。

**本次已统一**：`srvAIEmbeddingDim = embedding.DefaultEmbeddingDim`（768），不再写字面量。
这样服务端与桌面端共用一个空间，也不可能出现「一边改了宽度另一边没跟」。

**代价与迁移**：服务端标识从 `…:256:prompt-v1` 变为 `…:768:prompt-v1`，因此服务端知识库会**全量重嵌入一次**
（回填只选 `model_id` 不匹配的行，天然可续；重嵌入完成前旧行不参与检索，检索是逐步恢复的）。
由于维度也进了 `ModelID`，这次重嵌入与 prompt 切换的重嵌入**合并成同一次**，不会各跑一遍。
存储占用约放大 3 倍（768 vs 256 个 float32），换取中文区分度。

**统一后的全仓收敛性审计**（`grep -rn "NewGemmaEmbedder(\|NewDefaultEmbedder(\|SharedGemma256()" --include=*.go`，
排除测试）：**所有生产构造点现在都产 768**，不存在第二个宽度——

| 构造点 | 宽度来源 |
|---|---|
| `guiapp/app.go:1104`、`app_embedding.go:107/225/539` | `NewDefaultEmbedder` / `DefaultEmbeddingDim` |
| `hub/internal/httpapi/mobile_knowledge.go:139` | `NewDefaultEmbedder` |
| `hub/internal/httpapi/llm_class_head.go:431`、`hubcenter/.../class_head.go:348` | `SharedGemma256()`（返回 768，名字是历史遗留） |
| `MaClawSrv/ai_models.go:703` | `srvAIEmbeddingDim` = `DefaultEmbeddingDim` |
| `corelib/tts/cmd/{synthesize,tts_asr_test}/main.go` | `DefaultEmbeddingDim`（原先写字面量 768，本次改为绑定常量） |

两个附带收益：① 服务端与桌面端的 `ModelID` 现在**相同**（同一模型文件 + 同宽 + 同配方），
所以一份知识库在两端之间同步/共用时是合法的同一个空间——此前会被维度段隔成两个空间；
② `corelib/embedding/gemma_test.go` 里大量 `NewGemmaEmbedder(path, 256)` 是**刻意**的——
它们测的就是 MRL 截断本身，不属于「该统一」的构造点。

---

## 开关与迁移

| 环境变量 | 行为 |
|---|---|
| （未设置） | prompt **开启**（默认） |
| `MACLAW_EMBED_PROMPTS=0` / `off` / `false` / `no` | 关闭，回到历史行为 |

`embedding.SetPromptsEnabled(bool)` 提供进程内覆盖，供测试与运维工具使用。

**升级后的行为**：`ModelID` 变化 → 首次 `SetEmbedder`（即应用启动）触发后台全量重嵌入
（cards → nodes → table rows），带 `generation` 校验，10 分钟超时。

> **注意：即使 `MACLAW_EMBED_PROMPTS=0`，升级后也躲不掉这一次重嵌入。**
> 因为标识的**第一段**同时改了：从旧的回退值 `%T:%d`（即 `*embedding.GemmaEmbedder:768`）
> 换成了从 GGUF 头部读出的真名（`Embeddinggemma 300m Qat Q8_0 Unquantized`）。
> 这是**故意**的（见「设计」一节：否则换一个同维度的模型不会让索引失效），
> 代价就是所有人都要重嵌一次。所以 `=0` 并不是"跳过迁移"的开关，
> 它的用途是**用旧空间先跑一遍回填、确认链路正常**，再择期开 prompt。
>
> **服务端还叠加了一层维度变化**：`srvAIEmbeddingDim` 从 256 统一到 768，
> 所以服务端标识从 `…:256:prompt-v1` 变成 `…:768:prompt-v1`。这与上面的 prompt 切换
> **合并为同一次**重嵌入（都在同一次 `ModelID` 变化里），不会各跑一遍。

**需要知道的代价**：重嵌入完成前，`model_id` 仍是旧值的行**不会**参与检索
（检索按 `model_id` 过滤）。也就是说升级后存在一个「检索逐步恢复」的窗口。
知识库很大时应评估该窗口，必要时：
1. 先用 `MACLAW_EMBED_PROMPTS=0` 启动、确认回填逻辑正常，再择期开启；
2. 或分批重启以续跑未完成的重嵌入（回填只选 `model_id` 不匹配的行，天然可续）。

---

## 验证

```bash
# 模板、开关、回落语义 + prompt 真的进入了模型
go test ./corelib/embedding/ -run "Prompt|ApplyPrompt|EmbedAs|Gemma" -v

# 知识库角色链路（全量套件本机跑不完：692 个测试 × ~90s 文件删除开销，见文末环境说明）
go test ./corelib/knowledge/ -run "TestAllQuerySitesUseQueryRole|TestKnowledgeRetrievalUsesRolePrompts|TestKnowledgeIndexingUsesDocumentRole" -count=1 -v -timeout 1200s

# 服务端适配器：角色、快路径、标识、宽度一致
go test ./MaClawSrv/ -run "TestSrvAIModelEmbedderAdapter|TestSrvAIEmbeddingDimMatchesProductionWidth" -count=1 -v
```

关键断言：

- `TestPromptPrefixMatchesModelCard` —— 模板与模型卡逐字一致；
- `TestApplyPromptDisabledIsIdentity` / `TestApplyPromptRoleNoneIsIdentity` —— 关闭开关与 `RoleNone` 都是恒等变换；
- `TestNeedsPromptMatchesApplyPrompt` —— 对**全部 9 个 role × 开关两档**做属性断言：
  `NeedsPrompt(role)` 必须等价于「`ApplyPrompt` 会改写文本」。批量快路径只在二者等价时才安全，
  一旦漂移，信任 `NeedsPrompt` 的调用方会跳过前缀、静默落回无 prompt 空间；
- `TestEmbedAsFallsBackForPlainEmbedders` —— 旧 Embedder 走原文，不被改写；
- `TestGemmaRoleProducesDifferentVectors` —— query 与 document prompt 产出的向量余弦 `< 0.999`，
  证明 prompt 确实进入了前向，而不是被 tokenizer 吞掉；
- `TestGemmaModelIDTracksPromptRegime` —— 两个档位的 `ModelID` 必须不同；
- `TestGemmaModelIDCarriesGGUFIdentity` —— `ModelID` 必须以 GGUF 里 `general.name` 的值为前缀，
  这样换模型文件（哪怕维度相同）就会换标识；
- `TestGemmaBatchRoleShortCircuitsWhenDisabled` / `TestGemmaBatchRoleDiffersWhenEnabled` ——
  关档时 `EmbedBatchWithRole` 与普通 `EmbedBatch` 必须**逐位一致**（快路径不能改变行为），
  开档时必须不同。

知识库链路（`corelib/knowledge/prompt_roles_test.go`，用记录 `Role` 的 `recordingRoleEmbedder`）：

- `TestAllQuerySitesUseQueryRole` —— **3 个查询侧调用点逐个覆盖**（`vector_search.searchByEmbedding`、
  `structured_search.searchTableRowsByEmbedding`、`image_search.SearchImages`），每个都必须用 `RoleQuery`
  且不得用 `RoleDocument`。刻意不"用一个代表点推断全部"：它们分处三个文件，任何一个被改回裸 `Embed`
  都会静默跨空间比较（余弦照样返回，看不出坏），所以必须逐点钉住；
- `TestKnowledgeRetrievalUsesRolePrompts` —— 端到端：入库后能被检索到，且 query 走 `RoleQuery`；
- `TestKnowledgeIndexingUsesDocumentRole` —— `SaveText` + `WaitBackground` 后入库文本必须是 `RoleDocument`。
  （**只断言 `RoleDocument` 存在，不断言 `RoleQuery` 不存在**——索引期间的主题关联会合法地走 `RoleQuery`，见上一节。）
- `TestEmbedderSwitchBackfillUsesDocumentRole` —— **迁移路径**：换一个 `ModelID` 不同的 embedder 触发全量回填，
  回填必须用 `RoleDocument`。这条最不能少：回填是**唯一会把整个索引重写一遍**的路径，
  角色漏传会把全库写进 query 空间，而 `ModelID` 过滤器还会"认证"结果是自洽的（query 比 query，永远自洽），
  从症状上完全看不出来。

> `SearchImages` 那条用**英文**查询：`bm25.ShortEntityMention` 对拉丁字母返回 false，
> 因此词法锚点短路不会跳过嵌入分支，`RoleQuery` 一定会被记录到。

四条测试都额外调用 `assertNoRolelessEmbedding`：**链路上不允许出现任何"无角色"的嵌入调用**。
这是真正能抓到缺陷的断言——`EmbedAs` 在调用点忘了传 role 时会**静默回落**到裸 `Embed`
（余弦照样返回，只表现为排序变差），而这种回落在记录器里正是 `RoleNone`。
反过来，只断言"某处出现过 `RoleDocument`"是不够的：任意一个正确的调用点就能满足它，
另一个漏传 role 的调用点会被完全掩盖。全部 8 个调用点均已显式传 role，生产代码中不存在 `RoleNone`。

这几条是**防回退**用的：任何调用点被改回裸 `emb.Embed`，它们都会失败。
注意 `embeddingMetadataTestEmbedder` 这类旧测试替身只实现基础 `Embedder`、不含 `RoleEmbedder`，
走的是 `EmbedAs` 的回落路径，因此现有知识库测试行为不变。

> **不要被这个包的测试耗时误导。** 本机（Windows）上每次**文件删除**固定卡 ~30s：
> 实测删除一个只含一个纯文本文件的临时目录就要 **30.3s**，`%TEMP%` 与 `F:` 两个盘一致。
> 一个测试 ≈ 关库（SQLite 驱动要删 `-wal`/`-shm`）1 次 + `t.TempDir()` 清理 2 次 ≈ 90.5s。
> 这正是每个测试都稳定在 ~90.5s 的原因，**与 `corelib/knowledge` 的代码无关**
> （独立探针实测：`NewSQLiteStore` 100ms、`SetEmbedder` 0ms、`SaveText` 3.7ms）。
> 排查测试慢时不要往这条链路上找。
> 另：`go test` 被中途 kill 会留下 `go.exe` 占着构建缓存锁，之后任何 `go test` 都会**静默挂死**
> （连 `-run '^$'` 都跑不完且无输出）。症状是 `go.exe` 存活但 CPU 时间只有几秒、且没有
> `compile`/`link`/`*.test.exe` 子进程——此时 `taskkill //F //IM go.exe` 后再串行重跑。
> 注意区分：`go vet` 本机也能跑完，只是**慢**（3.5 分钟起，同样是 30s 删除拖的），
> 所以上面的命令块刻意不含 `vet`——`go test` 本身会编译测试文件，类型错误一样会暴露。

服务端适配器（`MaClawSrv/ai_models_test.go`）：

- 编译期 `var _ embedding.RoleEmbedder = srvAIModelEmbedderAdapter{}` —— 适配器一旦丢掉角色方法就编译失败；
- `TestSrvAIModelEmbedderAdapterAppliesRolePrompts` —— 前缀真的加上了（用向量第二分量差值等于前缀 rune 数验证），
  且 `RoleNone` 与裸 `Embed` 完全一致；
- `TestSrvAIModelEmbedderAdapterBatchRoleAppliesPrompts` —— 批量路径每个元素都带前缀，且仍只跑一次批量推理；
- `TestSrvAIModelEmbedderAdapterBatchRoleShortCircuitsWhenDisabled` —— 关档时复用 `EmbedBatch`，结果逐位不变；
- `TestSrvAIModelEmbedderAdapterModelIDTracksPromptRegime` —— 两档 `ModelID` 必须不同、以模型文件名开头、以配方结尾；
- `TestSrvAIModelEmbedderAdapterModelIDFollowsModelPathOverride` ——
  `MACLAW_EMBEDDING_MODEL_PATH` 换文件时 `ModelID` 必须跟着换，否则换了权重还继续用旧索引。
