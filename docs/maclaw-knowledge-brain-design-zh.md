# MaClaw 中外脑知识存储设计文档

状态：待 review  
日期：2026-05-05  
范围：MaClaw 长期知识存储、文档/表格/网页摄取、目录批量导入、当前话题关联召回

## 1. 背景

当前 MaClaw 已经具备长期记忆、会话记忆、项目索引、语义图、实体索引、话题聚类、工具路由、网页抓取、文档处理等基础能力。现有记忆系统更适合保存用户偏好、项目事实、会话摘要、工作流产物和高价值经验，但不适合直接承载大量外部资料，例如用户上传的大型 PDF、Word、Excel，以及公共网站链接。

本设计目标是给 MaClaw 增加一套“中外脑”能力：

- **中脑**：识别当前话题、任务、项目、实体和用户意图，决定什么时候存、存到哪里、召回什么。
- **外脑**：存储大量外部知识源，包括文档、表格、公共网页、目录批量导入资料，以及从会话/工作流中沉淀出的知识。
- **内脑**：继续使用现有 `corelib/memory`，保存用户长期偏好、项目记忆、任务检查点和高价值结论。

本方案避免“原始 RAG”模式，即不把文档简单切块后做向量召回并塞入上下文，而是把来源资料转化为可追踪、可关联、可分层召回的知识对象。

补充原则（2026-05-05 实现口径）：

- **存储时可用 LLM 提升质量**：文档/网页写入外脑时，先用规则抽取生成基础知识卡片；当 MaClaw LLM 已配置且文本较长或用户提供话题提示时，再调用 LLM 辅助生成更高质量的 claim、summary、topics、tags。LLM 失败不阻塞导入，自动回退到规则卡片。
- **查询时默认不依赖 LLM**：搜索优先走本地 SQLite FTS5，先召回结构化知识卡片，再补充原始 DocumentNode 命中；LLM rerank 只作为后续可选增强，不作为查询链路的硬依赖。
- **结构化优先于原文片段**：查询结果面向知识卡片和来源证据，只有需要补充上下文时才回填原文节点，避免把外脑退化成大段 chunk 注入。
- **事实层本地派生**：写入知识卡片时同步生成 entities 与轻量 Fact（三元组），并为 Fact 建立本地 FTS；用于后续图谱、实体索引和非 LLM 查询增强。事实层可以由规则生成，也可以由 LLM 结果补全。
- **目录导入后台化**：设置页目录导入优先走后台 job，前端轮询任务状态，同时从 SQLite 展示最近导入批次，避免大目录导入阻塞 UI。

## 2. 目标

1. 支持用户输入文档和公共网站 URL，并方便地保存为长期知识。
2. 在设置中新增“知识库”Tab，允许用户从目录批量录入文档知识，并管理已保存来源。
3. 知识必须与当前话题、当前项目、用户身份、会话实体有关联。
4. 支持大规模资料存储，不把全部知识放入现有 memory JSON。
5. 复用现有 MaClaw 基础设施，避免重做 embedding、BM25、语义图、实体索引、项目索引、提取器和工具链。
6. 支持异步摄取：用户上传、保存 URL 或批量导入目录后快速可见，后台继续深度蒸馏。
7. 支持知识来源追溯：回答时能回到原始 URL、页码、sheet、章节、段落或本地相对路径。
8. 支持安全边界：公共网页访问、防 SSRF、prompt injection 扫描、敏感信息脱敏、多租户隔离、本地目录导入权限边界。

## 3. 非目标

1. 第一阶段不引入独立向量数据库服务，优先使用 SQLite + FTS5 + 现有 embedding 缓存。
2. 第一阶段不做登录态网页、付费墙、私有站点爬取。
3. 第一阶段不执行 Office 宏，不运行文档中的脚本。
4. 第一阶段不做任意深度网站镜像，只支持单页保存和受限的同域链接扩展。
5. 第一阶段目录导入不做实时文件系统监听，只做用户触发的扫描和增量导入。
6. 外脑不替代现有 `memory.Store`，而是与其协同。

## 4. 现有能力复用

| 现有模块 | 复用方式 |
|---|---|
| `corelib/memory.Store` | 保存热知识、用户偏好、项目关键结论、会话摘要 |
| `corelib/memory.SemanticGraph` | 复用实体-关系图，扩展为知识卡片与来源节点的关系图 |
| `corelib/memory.EntityIndex` | 实体中心召回，支持“当前话题里的公司/产品/条款/指标”命中 |
| `corelib/memory.TopicClusterer` | 当前话题聚类和知识域激活 |
| `corelib/memory.TemporalTree` | 分层摘要，支持会话/天/周/Profile 级召回 |
| `corelib/memory.ProjectIndex` | 项目资料、任务产物、项目决策入口 |
| `corelib/memory.KnowledgeExtractor` | 从对话、文档摘要、网页正文中抽取知识点 |
| `corelib/memory.OnlineExtractor` | 每轮会话增量抽取当前实体、偏好、项目事实 |
| `corelib/memory.Promoter` / `Reflector` | 将重复出现的临时信息晋升为稳定知识或高层洞察 |
| `corelib/embedding` | 本地 embedding 生成和查询缓存 |
| `corelib/workflow.SQLiteStore` 模式 | 复用 `modernc.org/sqlite`、WAL、busy_timeout、schema 初始化风格 |
| 现有 web 工具 | URL 抓取、正文提取、必要时浏览器渲染 |
| 文档/Excel/PDF 能力 | 作为外脑 parser adapter 的基础 |
| GUI 设置页 | 新增“知识库”Tab，提供来源管理和目录批量导入入口 |

## 5. 总体架构

```text
用户输入：对话 / 文件 / URL / 设置页目录批量导入 / 工作流产物 / 工具结果
        |
        v
Trigger Detector 触发器识别
        |
        v
Topic Cortex 中脑
  - 当前话题
  - 项目路径
  - 用户/租户
  - 当前任务阶段
  - 会话实体
  - 保存意图
  - 来源作用域
        |
        v
Knowledge Intake 摄取入口
  - source 记录
  - batch import 记录
  - parser adapter
  - document node 结构化
  - 快速摘要
  - 异步任务入队
        |
        v
Knowledge Distiller 深度蒸馏
  - 知识卡片
  - 实体/关系
  - 章节摘要
  - 表格理解
  - 来源证据
        |
        v
KnowledgeStore 外脑
  - SQLite/FTS5
  - embedding
  - source/document/card/fact/batch/schema
        |
        +--------------------+
        |                    |
        v                    v
Memory Store 内脑       SemanticGraph/EntityIndex/TopicClusterer
  热知识与高价值摘要      关系扩展和话题激活
        |                    |
        +---------+----------+
                  v
           Recall Brain 召回
                  |
                  v
           LLM 上下文注入
```

## 6. 核心概念

### 6.1 Source

Source 是用户保存的原始资料入口，例如 Word、PDF、Excel、网页 URL 或目录导入中的单个文件。

```go
type KnowledgeSource struct {
    ID           string
    Kind         string // docx, pdf, xlsx, url, html, markdown, text, conversation, workflow_artifact
    URI          string // file path, uploaded file id, or URL
    CanonicalURI string
    Title        string
    Author       string
    SiteName     string
    PublishedAt  *time.Time
    FetchedAt    time.Time
    ContentHash  string
    OwnerID      string
    TenantID     string
    ProjectPath  string
    TopicHint    string
    SourceTrust  float64
    BatchID      string
    RelativePath string
    Status       string // pending, parsed, distilled, failed, stale, disabled
    ErrorMessage string
}
```

### 6.2 DocumentNode

DocumentNode 是从 Source 解析出的结构节点。它不是普通 chunk，而是保留文档结构和证据位置的节点。

```go
type DocumentNode struct {
    ID         string
    SourceID   string
    ParentID   string
    Type       string // heading, paragraph, table, sheet, page, image, link
    Title      string
    Text       string
    Level      int
    Page       int
    SheetName  string
    RowRange   string
    ColRange   string
    XPath      string
    Offset     int
    Metadata   map[string]string
    TokenCount int
}
```

### 6.3 KnowledgeCard

KnowledgeCard 是外脑召回的主要对象。它比原文片段更短、更稳定、可引用。

```go
type KnowledgeCard struct {
    ID          string
    SourceID    string
    NodeID      string
    Title       string
    Claim       string
    Summary     string
    Entities    []string
    Topics      []string
    Tags        []string
    ProjectPath string
    OwnerID      string
    TenantID     string
    ValidAt     *time.Time
    InvalidAt   *time.Time
    Confidence  float64
    Importance  float64
    SourceTrust float64
    Embedding   []float32
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### 6.4 KnowledgeFact

KnowledgeFact 是实体关系事实，用于图扩展和多跳召回。

```go
type KnowledgeFact struct {
    ID         string
    CardID     string
    SourceID   string
    Subject    string
    Predicate  string
    Object     string
    Negated    bool
    ValidAt    *time.Time
    InvalidAt  *time.Time
    Confidence float64
}
```

### 6.5 ImportBatch

ImportBatch 表示一次目录批量导入任务。

```go
type ImportBatch struct {
    ID           string
    RootPath     string
    OwnerID      string
    TenantID     string
    ProjectPath  string
    TopicHint    string
    Recursive    bool
    IncludeExts  []string
    ExcludeGlobs []string
    MaxFileBytes int64
    Status       string // scanning, queued, running, completed, failed, cancelled
    TotalFiles   int
    QueuedFiles  int
    Imported     int
    Skipped      int
    Failed       int
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

## 7. 用户触发方式

### 7.1 显式保存

用户明确表达保存意图时直接触发长期知识存储。

示例：

```text
保存这个链接
把这篇文章加入外脑
记住这个 PDF
以后回答这个项目时参考这份 Excel
把刚才总结的结论存成项目知识
这个规则以后都要记住
```

处理策略：

- 文件或 URL：进入 `KnowledgeIntake`。
- 对话结论：进入 `memory.Store`，必要时同步写入 `KnowledgeStore`。
- 用户规则/偏好：高风险长期影响，默认请求确认或使用显式触发。

### 7.2 文件/URL 上传触发

用户上传文件或发送 URL 时，系统先创建候选 Source，并基于当前话题给出默认处理。

```text
用户上传/粘贴 URL
  -> 快速识别类型、标题、摘要、来源
  -> 绑定当前 topic/project/user
  -> 默认作为“当前会话资料”可用
  -> 用户明确保存或配置允许时进入长期外脑
```

推荐默认行为：

- 当前对话中上传的资料，先作为临时上下文资料。
- 用户说“保存”“学习”“以后参考”时，长期保存。
- 工作流场景下上传的输入文件，可默认保存到当前项目外脑，并给出轻量提示。

### 7.3 自动沉淀

系统可自动保存高价值、低风险、强相关内容。

自动保存候选：

- 工作流阶段产物：需求、设计、任务拆解、合同审查结论。
- 用户纠正：例如“不是这样，我们统一用 X 规则”。
- 项目决策：例如“这个项目确定使用 SQLite”。
- 工具成功经验：部署命令、API 对接方式、排障路径。
- 长对话结束后的稳定摘要。

风险分级：

| 内容类型 | 默认行为 |
|---|---|
| 会话摘要、任务检查点 | 自动存入内脑 |
| 工作流产物 | 自动存入项目知识 |
| 用户偏好、长期规则 | 显式保存或确认后保存 |
| 外部网页/文档 | 上传候选，显式保存或项目配置允许后保存 |
| 敏感信息 | 默认不保存或脱敏后保存 |

### 7.4 设置页知识库 Tab 触发

除了聊天中“随手保存”，需要在设置中提供一个稳定的知识库管理入口，方便用户批量建设外脑。

入口位置：

```text
设置 -> 知识库
```

核心能力：

- 添加公共 URL。
- 上传单个文档。
- 选择本地目录，批量导入目录下的 Word / PDF / Excel / Markdown / 文本文档。
- 查看导入队列、解析状态、失败原因和重试入口。
- 搜索已保存来源和知识卡片。
- 删除、停用或刷新来源。
- 配置默认保存范围：当前项目、个人外脑、仅本机。

目录批量导入流程：

```text
用户选择目录
  -> 选择导入范围：当前项目 / 个人外脑
  -> 选择文件类型：doc/docx/pdf/xls/xlsx/md/txt
  -> 可选：递归子目录、跳过隐藏文件、最大文件大小
  -> 预扫描：文件数、总大小、预计任务数、重复文件数
  -> 用户确认
  -> 后台批量 intake
  -> 设置页显示进度、失败、跳过、完成
```

批量导入必须具备：

- **hash 去重**：相同内容不重复导入。
- **增量扫描**：目录再次导入时只处理新增或变化文件。
- **失败隔离**：单个文件失败不影响整批任务。
- **后台运行**：用户可离开设置页，任务继续执行。
- **可取消**：用户可取消排队中任务；已完成的 Source 不回滚，除非用户选择删除。
- **作用域绑定**：每个 Source 记录 project/user/tenant，避免跨项目污染。
- **目录来源记录**：保存 batch id、原始目录、相对路径，方便后续刷新。

目录导入默认策略：

| 场景 | 默认行为 |
|---|---|
| 用户在项目中打开设置页 | 默认导入到当前项目外脑 |
| 用户在全局设置页导入 | 默认导入到个人外脑 |
| 文件超过大小限制 | 跳过并提示 |
| 文件 hash 已存在 | 标记为 skipped_duplicate |
| 文件已变化 | 创建新版本并重新蒸馏 |

## 8. URL 公共网页支持

### 8.1 范围

URL 入口用于保存公共网站信息，便于用户把网上资料沉淀到外脑。

支持：

- 公共网页文章、博客、文档页、政策页、产品页。
- 静态 HTML 正文提取。
- 必要时通过浏览器渲染 JS 页面。
- 标题、作者、发布时间、站点名、canonical URL、正文、表格、图片 alt/link 提取。
- 后台版本更新检测。

暂不支持：

- 需要登录的页面。
- 付费墙内容。
- 内网地址、localhost、云元数据地址。
- 大规模整站镜像。

### 8.2 URL 保存流程

```text
用户：保存这个链接 https://example.com/article
  |
  v
URLNormalizer
  - 补全 scheme
  - 跟踪跳转
  - 计算 canonical URL
  - 检查公网地址
  |
  v
WebFetcher
  - 先普通 fetch
  - 失败或正文过少时浏览器渲染
  |
  v
WebParser
  - 抽正文、标题、发布时间、作者、表格、链接
  |
  v
KnowledgeIntake
  - 创建 Source
  - 写 DocumentNode
  - 快速摘要
  - 异步蒸馏卡片/实体/关系
```

### 8.3 URL 安全

URL 抓取必须经过安全检查：

- 只允许 `http` 和 `https`。
- 禁止 `localhost`、`127.0.0.0/8`、`::1`、内网网段、link-local、云元数据地址。
- 跳转后的最终地址也必须重新检查。
- 限制单页大小、下载时间、重定向次数。
- 抓取结果进入知识提取前先运行 prompt injection scanner。
- 抓取内容进入长期存储前执行敏感信息脱敏。

## 9. 文件类型支持

### 9.1 Word / DOCX

处理重点：

- 提取标题层级、段落、表格、批注、页眉页脚。
- 保留章节路径，例如“合同审查 > 付款条款 > 违约责任”。
- 每张表格作为独立 DocumentNode。
- 知识卡片绑定章节和段落位置。
- 不执行宏。

### 9.2 PDF

处理重点：

- 识别文本型 PDF 和扫描型 PDF。
- 文本型 PDF 提取 page/block/table。
- 扫描型 PDF 后续接 OCR，第一阶段可标记为待 OCR。
- 每条知识带页码和 source span。
- 表格单独建节点，避免混入普通段落。

### 9.3 Excel / XLSX

处理重点：

- 不把每个单元格作为知识卡片。
- 识别 sheet、表头、数据区域、公式、关键指标、异常值、汇总行。
- 生成表结构摘要、字段说明、统计摘要、关键行、异常行、公式依赖。
- 召回时按“表 / 字段 / 指标 / 时间范围 / 项目实体”查找。

### 9.4 Markdown / TXT

处理重点：

- Markdown 保留标题层级、代码块、表格和链接。
- TXT 按空行、标题样式、长度窗口切为 DocumentNode，但不作为原始 RAG chunk 直接注入。
- 目录导入时 Markdown/TXT 可作为轻量优先支持类型。

## 10. 存储设计

新增包建议：

```text
corelib/knowledge/
  intake.go
  store.go
  schema.go
  parser_docx.go
  parser_pdf.go
  parser_excel.go
  parser_markdown.go
  parser_text.go
  parser_web.go
  batch_import.go
  distiller.go
  recall.go
  source.go
  trigger.go
  security.go
```

第一阶段使用 SQLite，复用项目已有 `modernc.org/sqlite`。

建议数据库位置：

```text
~/.maclaw/data/knowledge.db
```

maclawsrv 多租户模式可按 tenant/user 隔离：

```text
~/.maclaw/server/<tenant>/<user>/knowledge.db
```

### 10.1 表结构草案

```sql
CREATE TABLE knowledge_sources (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    uri TEXT NOT NULL,
    canonical_uri TEXT,
    title TEXT,
    author TEXT,
    site_name TEXT,
    published_at TEXT,
    fetched_at TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    owner_id TEXT,
    tenant_id TEXT,
    project_path TEXT,
    topic_hint TEXT,
    source_trust REAL DEFAULT 0.5,
    batch_id TEXT,
    relative_path TEXT,
    status TEXT NOT NULL,
    error_message TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE document_nodes (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    parent_id TEXT,
    type TEXT NOT NULL,
    title TEXT,
    text TEXT,
    level INTEGER DEFAULT 0,
    page INTEGER DEFAULT 0,
    sheet_name TEXT,
    row_range TEXT,
    col_range TEXT,
    xpath TEXT,
    offset INTEGER DEFAULT 0,
    metadata_json TEXT,
    token_count INTEGER DEFAULT 0,
    FOREIGN KEY(source_id) REFERENCES knowledge_sources(id)
);

CREATE VIRTUAL TABLE document_nodes_fts USING fts5(
    title,
    text,
    content='document_nodes',
    content_rowid='rowid'
);

CREATE TABLE knowledge_cards (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    node_id TEXT,
    title TEXT,
    claim TEXT NOT NULL,
    summary TEXT,
    entities_json TEXT,
    topics_json TEXT,
    tags_json TEXT,
    project_path TEXT,
    owner_id TEXT,
    tenant_id TEXT,
    valid_at TEXT,
    invalid_at TEXT,
    confidence REAL DEFAULT 0.5,
    importance REAL DEFAULT 1.0,
    source_trust REAL DEFAULT 0.5,
    embedding BLOB,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(source_id) REFERENCES knowledge_sources(id)
);

CREATE VIRTUAL TABLE knowledge_cards_fts USING fts5(
    title,
    claim,
    summary,
    content='knowledge_cards',
    content_rowid='rowid'
);

CREATE TABLE knowledge_facts (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    predicate TEXT NOT NULL,
    object TEXT NOT NULL,
    negated INTEGER DEFAULT 0,
    valid_at TEXT,
    invalid_at TEXT,
    confidence REAL DEFAULT 0.5,
    FOREIGN KEY(card_id) REFERENCES knowledge_cards(id),
    FOREIGN KEY(source_id) REFERENCES knowledge_sources(id)
);

CREATE TABLE knowledge_import_batches (
    id TEXT PRIMARY KEY,
    root_path TEXT NOT NULL,
    owner_id TEXT,
    tenant_id TEXT,
    project_path TEXT,
    topic_hint TEXT,
    recursive INTEGER DEFAULT 1,
    include_exts_json TEXT,
    exclude_globs_json TEXT,
    max_file_bytes INTEGER DEFAULT 0,
    status TEXT NOT NULL,
    total_files INTEGER DEFAULT 0,
    queued_files INTEGER DEFAULT 0,
    imported_files INTEGER DEFAULT 0,
    skipped_files INTEGER DEFAULT 0,
    failed_files INTEGER DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE knowledge_import_items (
    id TEXT PRIMARY KEY,
    batch_id TEXT NOT NULL,
    source_id TEXT,
    file_path TEXT NOT NULL,
    relative_path TEXT,
    file_hash TEXT,
    file_size INTEGER DEFAULT 0,
    status TEXT NOT NULL,
    error_message TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(batch_id) REFERENCES knowledge_import_batches(id),
    FOREIGN KEY(source_id) REFERENCES knowledge_sources(id)
);

CREATE INDEX idx_sources_scope ON knowledge_sources(tenant_id, owner_id, project_path, kind, status);
CREATE INDEX idx_sources_batch ON knowledge_sources(batch_id, relative_path);
CREATE INDEX idx_cards_scope ON knowledge_cards(tenant_id, owner_id, project_path, updated_at);
CREATE INDEX idx_facts_subject ON knowledge_facts(subject);
CREATE INDEX idx_facts_object ON knowledge_facts(object);
CREATE INDEX idx_import_items_batch ON knowledge_import_items(batch_id, status);
CREATE INDEX idx_import_items_hash ON knowledge_import_items(file_hash);
```

说明：

- `embedding BLOB` 第一阶段可直接存 float32 bytes；后续可拆为独立向量索引。
- FTS5 表是否需要手动维护，实施时按 SQLite driver 支持情况决定。
- 如果 FTS5 在目标平台有兼容问题，降级为普通 LIKE/BM25 内存索引。
- 批量导入表只记录导入任务和文件状态，真正可召回内容仍落在 Source / Node / Card / Fact。

## 11. 中脑 Topic Cortex

Topic Cortex 是“当前话题关联性”的核心，建议作为轻量状态挂在 agent loop 或 `LoopContext` 周边。

```go
type TopicContext struct {
    UserMessage    string
    ConversationID string
    OwnerID        string
    TenantID       string
    ProjectPath    string
    WorkflowType   string
    WorkflowPhase  string
    Intent         string
    TopicLabels    []string
    Entities       []string
    RecentSources  []string
    RecentCards    []string
    SaveIntent     bool
    SourceHints    []string
}
```

TopicContext 来源：

- 当前用户消息。
- 最近 N 轮会话。
- 当前项目路径。
- 当前工作流状态。
- 已上传文件或 URL。
- 设置页导入时选择的作用域和 topic hint。
- `OnlineExtractor` 识别出的实体和事实。
- 用户显式触发词。

TopicContext 用途：

- 决定输入资料是临时使用、项目外脑、个人外脑还是需要确认。
- 为 Source/Card 写入 `topic_hint`、`project_path`、`entities`。
- 召回时做 scope gating 和 topic activation。
- 工具路由时激活 web/document/knowledge 工具。

## 12. 摄取流程

### 12.1 快速摄取

快速摄取目标是让用户几秒内能开始问问题。

```text
1. 保存 Source 元信息
2. 解析主要文本结构
3. 建立 DocumentNode FTS
4. 生成短摘要
5. 将 Source 标记为 parsed
6. 异步提交 deep_distill job
```

快速摄取产物：

- Source 元信息。
- DocumentNode 基础结构。
- 1 个 source-level 摘要卡片。
- 当前会话可用的临时上下文摘要。

### 12.2 深度蒸馏

深度蒸馏目标是让资料进入长期外脑，并可跨项目/跨会话被高质量召回。

```text
1. 分章节/表格/页面蒸馏知识卡片
2. 抽取实体和关系事实
3. 生成层级摘要
4. 计算 embedding
5. 写入 KnowledgeStore
6. 同步高价值摘要到 memory.Store
7. 更新 SemanticGraph / EntityIndex / TopicClusterer
```

深度蒸馏需要限流：

- 单个大文件按批次处理。
- 每批限制 token 和 LLM 调用次数。
- 失败可重试。
- 支持暂停和恢复。

### 12.3 目录批量导入

目录导入是多个 Source 的批处理，不改变单个 Source 的摄取模型。

```text
1. 创建 ImportBatch
2. 扫描目录，生成 ImportItem
3. 计算文件 hash，判断重复、过大、未知类型
4. 用户确认后将可导入文件入队
5. 每个文件独立调用 KnowledgeIntake
6. 更新 batch 进度
7. 完成后显示 imported/skipped/failed 明细
```

实现约束：

- 默认递归子目录，但可关闭。
- 默认跳过隐藏文件、临时文件、系统文件和明显的二进制文件。
- 默认支持 `.docx`、`.pdf`、`.xlsx`、`.md`、`.txt`；`.doc`、`.xls` 可作为兼容项，取决于现有 parser 能力。
- 默认单文件大小上限 100MB，可在设置中调整。
- 目录导入不能阻塞 GUI 主线程。

## 13. 召回设计

召回入口建议：

```go
type RecallBrain interface {
    RecallKnowledge(ctx context.Context, topic TopicContext, opts RecallOptions) ([]RecallItem, error)
}
```

召回不是单路向量搜索，而是多路候选融合：

```text
1. Scope Gate
   - tenant/user/project/global
   - 当前资料/近期资料/项目资料

2. Topic Activation
   - 当前话题标签
   - 当前实体
   - 当前工作流阶段
   - 最近上传 source
   - 最近导入 batch

3. Candidate Retrieval
   - KnowledgeCard FTS
   - DocumentNode FTS
   - embedding 相似度
   - EntityIndex 命中
   - SemanticGraph 1-2 跳扩展
   - ProjectIndex 项目产物
   - memory.Store 热记忆

4. Score Fusion
   - 话题相关性
   - 项目/用户作用域
   - 实体路径强度
   - BM25/FTS 分数
   - embedding 相似度
   - 来源可信度
   - 新鲜度/有效期
   - 重要性
   - 去冗余惩罚

5. LLM Rerank
   - 从候选中选真正有助于当前回答的少量卡片

6. Context Injection
   - 注入知识卡片摘要
   - 必要时回填原文证据
   - 保留来源 URL/页码/sheet/章节/相对路径
```

初始打分建议：

```text
Score =
  0.22 * topic_affinity +
  0.18 * entity_affinity +
  0.18 * lexical_score +
  0.18 * vector_score +
  0.10 * graph_score +
  0.08 * source_trust +
  0.04 * freshness +
  0.02 * importance -
  redundancy_penalty - stale_penalty
```

权重后续可通过使用反馈调优。

## 14. 与内脑协同

外脑和内脑分工：

| 类型 | 存储位置 |
|---|---|
| 用户偏好、长期规则、自我身份 | `memory.Store` |
| 项目关键决策、高价值结论 | `memory.Store` + `KnowledgeStore` |
| 大文档原文、网页正文、表格结构 | `KnowledgeStore` |
| 目录批量导入的文件来源和解析结构 | `KnowledgeStore` |
| 知识卡片、实体关系、来源证据 | `KnowledgeStore` + derived graph |
| 会话短期上下文 | `ConversationMemory` |
| 会话过期摘要 | `memory.Store` |

同步规则：

- 深度蒸馏后，只把高价值、短摘要、强项目相关知识同步到 `memory.Store`。
- 大量来源节点和普通知识卡片只留在 `KnowledgeStore`。
- `memory.Store` 的 recall 可调用 `RecallBrain` 补充外脑结果。
- 外脑召回命中的知识卡片可以增加热度，并在多次命中后晋升为内脑项目记忆。

## 15. 工具和 API 草案

### 15.1 Agent 工具

建议增加或扩展以下工具：

```text
knowledge_save_source
  保存文件、URL、当前对话片段或工作流产物。

knowledge_list_sources
  查看当前项目/用户保存过的资料。

knowledge_search
  在外脑中搜索知识卡片和来源。

knowledge_get_source
  查看来源详情、抓取状态、摘要和证据。

knowledge_forget_source
  删除或停用某个来源。

knowledge_refresh_url
  重新抓取公共网页并增量更新。

knowledge_import_directory
  从用户选择的目录批量导入文档，支持预扫描、增量导入、进度查询和失败重试。

knowledge_import_status
  查询批量导入任务状态、已完成数量、失败文件和跳过原因。
```

### 15.2 Go 接口

```go
type IntakeRequest struct {
    SourceType     string
    URI            string
    FileBytes      []byte
    FileName       string
    OwnerID        string
    TenantID       string
    ProjectPath    string
    TopicHint      string
    TriggerMode    string // explicit, upload, inferred, workflow_auto, settings_batch
    SaveScope      string // session, project, personal, local_only
    RequireConfirm bool
}

type IntakeResult struct {
    SourceID      string
    Status        string
    Title         string
    Summary       string
    ImmediateUse  bool
    BackgroundJob string
    Warnings      []string
}
```

目录批量导入接口：

```go
type DirectoryImportRequest struct {
    RootPath      string
    OwnerID       string
    TenantID      string
    ProjectPath   string
    TopicHint     string
    SaveScope     string // project, personal, local_only
    Recursive     bool
    IncludeExts   []string
    ExcludeGlobs  []string
    MaxFileBytes  int64
    DryRun        bool // true = 只预扫描，不入库
}

type DirectoryImportResult struct {
    BatchID        string
    Status         string
    TotalFiles     int
    QueuedFiles    int
    DuplicateFiles int
    SkippedFiles   int
    EstimatedBytes int64
    Warnings       []string
}
```

### 15.3 REST API

MaClawSrv 可增加：

```text
POST   /api/knowledge/sources
GET    /api/knowledge/sources
GET    /api/knowledge/sources/{id}
DELETE /api/knowledge/sources/{id}
POST   /api/knowledge/sources/{id}/refresh
POST   /api/knowledge/search
GET    /api/knowledge/jobs/{id}
POST   /api/knowledge/imports/directories/scan
POST   /api/knowledge/imports/directories
GET    /api/knowledge/imports/{batchId}
POST   /api/knowledge/imports/{batchId}/cancel
```

## 16. 用户体验

### 16.1 保存 URL

```text
用户：保存这个链接 https://example.com/article

MaClaw：
已保存到当前项目外脑：文章标题
我会先用快速摘要支持本轮对话，后台继续整理知识卡片和来源证据。
```

### 16.2 上传文件

```text
用户上传：合同.pdf

MaClaw：
已读取合同.pdf，识别到 18 页、6 个主要章节。
你可以直接问这份合同；如果要长期保存，我会把它作为当前项目资料进入外脑。
```

如果当前工作流要求资料沉淀：

```text
MaClaw：
已作为“合同审查 / 当前项目”资料学习，后台正在整理条款、风险点和证据页码。
```

### 16.3 自动沉淀提示

```text
MaClaw：
我识别到一条项目决策：“本项目使用 SQLite 作为本地知识存储”。是否保存为项目知识？
```

### 16.4 设置页知识库 Tab

设置页新增“知识库”Tab，定位是外脑的管理控制台。

页面布局建议：

```text
设置 / 知识库

[添加 URL] [上传文档] [导入目录]

作用域：当前项目 / 个人外脑 / 仅本机
状态筛选：全部 / 处理中 / 已完成 / 失败 / 已停用
搜索框：搜索来源、标题、实体、知识卡片

来源列表：
  标题 | 类型 | 作用域 | 状态 | 更新时间 | 卡片数 | 操作

导入任务：
  目录 | 总数 | 完成 | 跳过 | 失败 | 进度 | 操作
```

导入目录交互：

```text
1. 用户点击“导入目录”
2. 选择本地目录
3. 选择是否递归子目录
4. 选择文件类型和最大文件大小
5. 点击“预扫描”
6. 展示将导入、重复、跳过、过大、未知类型文件数量
7. 用户确认后后台导入
8. 进度列表实时显示每个文件状态
```

设置页不展示大段文档正文，只展示来源、摘要、知识卡片数量、最近错误和操作入口，避免把管理页变成文档阅读器。

## 17. 安全与合规

1. 文件解析不执行宏、不执行嵌入脚本。
2. URL 仅支持公共网页，禁止访问内网和本机地址。
3. 抓取内容和文档内容进入长期存储前执行 prompt injection 扫描。
4. 敏感信息写入前脱敏：API key、password、token、私钥等。
5. 多租户模式下所有 Source/Card/Fact 必须携带 tenant/user scope。
6. 回答时区分“来源事实”和“模型推断”，必要时展示 URL、页码、sheet、章节、相对路径。
7. 支持用户删除来源和相关卡片。
8. 网页内容记录抓取时间和版本 hash，避免把旧网页当最新事实。
9. 目录导入只读取用户显式选择的目录，不扫描系统目录、隐藏目录或符号链接指向的外部路径，除非用户明确开启。
10. 批量导入需要限制并发、文件大小和总任务数量，避免本机资源被耗尽。

## 18. 观测与诊断

建议增加诊断信息：

- Source 数量、类型分布、解析失败数。
- ImportBatch 数量、导入进度、失败原因、跳过原因。
- 待处理 job 数量、失败原因、平均处理时长。
- KnowledgeCard 数量、平均 token、embedding 缺失数。
- 召回命中来源、各路召回贡献、rerank 前后变化。
- URL 抓取失败原因、安全拦截原因。
- 外脑命中后是否帮助工具调用或最终回答。

可增加命令：

```text
knowledge doctor
knowledge stats
knowledge explain <query>
knowledge imports
```

## 19. 分阶段实施计划

### Phase 1：最小可用外脑

目标：支持 URL / 文件 / 目录批量导入保存为 Source，建立 SQLite 存储和基础召回。

任务：

- 新增 `corelib/knowledge` 包。
- 建立 SQLite schema。
- 实现 `KnowledgeStore` CRUD。
- 实现 URL 公共网页保存：安全检查、fetch、正文解析、Source/Node 写入。
- 实现文本型文档基础 parser adapter。
- 实现 `knowledge_save_source` 和 `knowledge_search`。
- 在设置中新增“知识库”Tab。
- 实现目录预扫描和批量导入任务。
- 召回复用 FTS + 当前 topic/project scope。

验收：

- 用户能保存一个公共 URL 并在后续对话中召回。
- 用户能上传一个文本型文档并保存到当前项目外脑。
- 用户能在设置页从目录批量导入文档，并看到进度、失败和跳过原因。
- Source 可列出、可删除、可查看状态。
- URL 内网地址被拦截。
- 重复文件不会重复入库。

### Phase 2：知识卡片和关系图

目标：摆脱原文片段召回，启用知识卡片、实体、关系。

任务：

- 实现 `KnowledgeDistiller`。
- 从 Source/Node 生成 KnowledgeCard。
- 抽取 KnowledgeFact 并同步到 `SemanticGraph`。
- 复用 `EntityIndex` 和 `TopicClusterer`。
- 召回融合 FTS、embedding、实体、图扩展。
- 将高价值知识同步到 `memory.Store`。

验收：

- 查询同一实体可召回多个来源的相关知识。
- 回答能附带来源 URL/页码/sheet/相对路径。
- 不是简单 chunk 注入，而是优先注入知识卡片。

### Phase 3：中脑和自动沉淀

目标：让当前话题驱动保存和召回。

任务：

- 实现 `TopicContext` 生成。
- 接入 agent loop、workflow state、conversation memory。
- 实现显式/上传/自动三类触发器。
- 工作流产物自动入项目外脑。
- 用户长期规则确认后保存。

验收：

- 同一个 URL 在不同项目下保存后，召回结果按项目隔离。
- 当前会话实体会影响知识卡片排序。
- 工作流产物能自动沉淀，并可在新会话召回。

### Phase 4：高级能力

目标：提升大规模、高质量、可维护性。

任务：

- PDF OCR。
- Excel 结构理解和公式依赖。
- URL 版本刷新和 diff。
- 目录导入配置模板和定期刷新。
- 外部向量索引可插拔。
- 知识质量评估和重复合并。
- `knowledge explain` 可解释召回。

## 20. 测试计划

单元测试：

- URL 安全检查：内网、localhost、跳转、超时、非法 scheme。
- Source hash 去重。
- 目录扫描：递归、隐藏文件、扩展名过滤、大小限制、重复文件。
- SQLite schema 初始化和迁移。
- DocumentNode FTS 查询。
- KnowledgeCard 序列化和 embedding BLOB。
- scope 隔离：tenant/user/project。

集成测试：

- 保存公共 URL -> 解析 -> 搜索 -> 召回。
- 上传 DOCX/PDF/XLSX -> 快速摘要 -> 后台蒸馏。
- 设置页选择目录 -> 预扫描 -> 导入 -> 进度更新 -> 搜索召回。
- 当前话题影响召回排序。
- 删除 Source 后相关 Node/Card/Fact 不再召回。

回归测试：

- 不影响现有 `memory.Store` 保存和召回。
- 不影响工具路由。
- 不影响 MaClawSrv 多租户数据隔离。

## 21. 待确认问题

1. 用户上传文件时，默认是“仅当前会话使用”还是“保存到当前项目外脑”？
2. GUI/TUI/IM 三种入口是否使用相同的保存确认策略？
3. URL 保存是否需要支持同域一跳链接扩展，还是第一版只保存单页？
4. PDF OCR 是否进入第一版，还是作为 Phase 4？
5. Excel 第一版是否只做表结构摘要，不做复杂公式依赖？
6. 外脑知识是否需要用户可见的管理页面，包括搜索、删除、刷新、查看来源？
7. 是否需要为企业版增加来源白名单/黑名单配置？
8. 目录批量导入是否默认递归子目录？
9. 目录导入的文件大小上限默认值是多少，例如 50MB、100MB 还是 200MB？
10. 是否需要支持目录定期刷新，还是第一版只支持手动重新扫描？

## 22. 推荐决策

建议第一版按以下决策开工：

1. 新增 `corelib/knowledge`，不要把大量外部知识塞进 `corelib/memory.Store`。
2. SQLite 使用 `modernc.org/sqlite`，沿用已有 WAL 和 busy_timeout 风格。
3. URL 第一版只支持公共单页保存，不支持登录态和整站爬取。
4. 用户显式说“保存/学习/以后参考”时直接长期保存。
5. 上传文件默认临时可问；在工作流场景或用户显式保存时进入项目外脑。
6. 深度蒸馏异步执行，快速摘要先返回。
7. 召回优先返回 KnowledgeCard，必要时再回填原文证据。
8. 高价值结论同步到 `memory.Store`，普通来源节点只留在 `KnowledgeStore`。
9. 设置页第一版就提供“知识库”Tab，支持 URL、上传文件、目录批量导入和来源管理。
10. 目录导入默认递归子目录、跳过隐藏文件、按 hash 去重，单文件大小上限先设为 100MB，可在设置中调整。


## 23. 当前实现进度（2026-05-05）

本轮已经完成第一批可运行闭环：

1. `corelib/knowledge` 提供 SQLite/FTS5 外脑存储，覆盖 `Source`、`DocumentNode`、`KnowledgeCard`、`KnowledgeFact`、`ImportBatch`、`ImportItem`。
2. 写入链路支持规则优先的卡片和事实生成；MaClaw LLM 配置可用且输入值得增强时，写入期可调用 LLM 辅助结构化，失败则回退规则结果。
3. 查询链路默认不依赖 LLM，按 `knowledge_cards_fts -> knowledge_facts_fts -> document_nodes_fts` 检索，并支持范围、结果类型、来源类型过滤。
4. 公共 URL 保存复用现有网页抓取能力，并保留公共 HTTP(S)、私网/本机地址和重定向安全边界。
5. 设置页新增“知识库”Tab，支持保存公共 URL、选择目录、预扫描、异步批量导入、最近批次、批次文件明细、来源列表、刷新和删除。
6. 智能体工具已接入 `knowledge_search`、`knowledge_save_url`、`knowledge_import_directory`、`knowledge_import_status`。其中查询工具只走本地库；保存 URL 和目录导入只应在用户明确要求保存/录入时触发。
7. 当前仍保留为第一阶段实现：`.doc`/`.xls` 识别但解析器待完善；实时 per-file 进度、实体图深度融合、敏感信息扫描和 LLM fact distiller 属于后续增强。
8. 来源详情已补齐只读链路：后端提供按 `source_id` 查看 KnowledgeCard/KnowledgeFact 的 API，设置页来源列表可展开查看该来源沉淀出的卡片和事实，便于用户检查录入质量。
9. 设置页已支持直接选择一个或多个文档录入，复用目录导入的 hash 去重、解析、规则优先蒸馏、可选 LLM 写入期增强和批次记录链路，不会误扫同目录未选中文件。
10. 已新增本地召回解释能力：`KnowledgeExplain` 和 `knowledge_explain` 只走本地 SQLite/FTS，返回卡片/事实/节点命中及 URL、相对路径、页码、sheet、行列范围等 citation 信息；设置页搜索区提供“解释召回”入口。
11. 已补齐显式文件录入闭环：`KnowledgeScanFiles` 支持对用户选中的文件做预扫描，设置页增加“预扫描已选”，智能体工具新增 `knowledge_import_files`，仅在用户明确提供/批准文件路径后扫描或导入。
12. 已增强异步目录导入进度：`SQLiteStore` 导入循环支持进度回调，GUI job 轮询可看到 `processed_files` 和 `current_file`，设置页导入摘要会展示已处理数量和当前文件。
13. 已补齐智能体侧知识库管理工具：`knowledge_stats` 可查看外脑计数，`knowledge_list_sources` 可列出本地来源，`knowledge_refresh_source` 可在用户明确要求时刷新已有 URL/HTML 来源，`knowledge_delete_source` 仅在用户明确要求删除指定来源时移除来源及派生卡片/事实。
14. 已继续补齐智能体侧诊断入口：`knowledge_list_import_batches` 可查看最近导入批次，`knowledge_list_import_items` 可查看批次内每个文件的导入/跳过/失败状态，`knowledge_source_detail` 可按 `source_id` 返回来源元信息及该来源沉淀出的 KnowledgeCard/KnowledgeFact，便于在对话中检查录入质量。
15. 已增强来源管理筛选能力：`ListSourcesOptions` 支持 `query`、`kind/source_kinds`、`status`、项目/用户/租户过滤；设置页“已保存来源”增加关键词、类型和状态筛选；`knowledge_list_sources` 工具同步透出这些参数，便于大量 URL/文档录入后的本地维护。
16. 已增强外脑健康统计：`KnowledgeStats` 除总量外返回来源类型分布、来源状态分布、导入批次状态分布和导入文件状态分布；设置页顶部展示这些分布，`knowledge_stats` 工具也可在对话中直接用于诊断大量录入后的失败、跳过和堆积情况。
17. 已新增外脑 Doctor 诊断闭环：`KnowledgeDoctor` 基于本地统计生成 `status/score/findings`，可发现失败来源、pending/stale 来源、legacy doc/xls、失败导入项、unsupported/too large/symlink/duplicate 等维护信号；设置页顶部展示前三条诊断，智能体工具新增 `knowledge_doctor`，全程不依赖 LLM。
18. 已补充来源结构节点诊断：后端新增 `ListNodesBySource` / `KnowledgeListNodesBySource`，设置页来源详情可同时查看解析出的 `DocumentNode`、知识卡片和事实；`knowledge_source_detail` 工具返回 `nodes/node_count`，用于排查文档、表格和网页到底被结构化成了哪些可检索节点。
19. 已补充来源可逆禁用能力：后端新增 `DisableSource` / `EnableSource`，默认本地检索会排除 `disabled` 来源，设置页来源列表提供启用/禁用按钮，智能体工具新增 `knowledge_disable_source` / `knowledge_enable_source`。删除仍作为破坏性操作保留，禁用用于临时排除过期或低质量资料。
20. 已增强外脑覆盖度诊断：`Stats` 新增 `sources_without_nodes/cards/facts`，`KnowledgeDoctor` 可发现 active 来源缺少解析节点、知识卡片或结构化事实的问题，帮助用户在批量录入后快速定位“存了但召回质量弱”的来源。
21. 已增强来源摘要：`ListSources` / `GetSource` 返回 `node_count/card_count/fact_count`，智能体通过 `knowledge_list_sources` 可直接看到每个来源的结构化覆盖度，不必逐个展开来源详情。
22. 已补充覆盖度定位筛选：`ListSourcesOptions` 和 `knowledge_list_sources` 支持 `coverage_filter`，可筛出 `missing_nodes`、`missing_cards`、`missing_facts`、`pdf_ocr_needed`、`complete`、`has_nodes/cards/facts` 来源；设置页来源列表增加覆盖度筛选下拉，用于从 Doctor 的覆盖缺口直接定位到具体来源。
23. 已补充本地文档来源刷新：`KnowledgeRefreshSource` / `knowledge_refresh_source` 现在可对已导入的 Markdown、TXT、DOCX、PDF、XLSX 以及 legacy DOC/XLS 来源重新读取原文件，重建 `DocumentNode`、知识卡片和事实索引；设置页对可刷新来源统一显示“刷新”，用于文件更新后的手动重建。
24. 已补充目录二次导入的路径更新语义：批量导入时如果同一路径、同作用域的来源已存在且文件内容发生变化，会复用原 `source_id`，清理旧节点/卡片/事实后重建索引，避免同一文件多次修改后在来源列表中产生重复记录。
25. 已增强本地召回与当前话题的关联性：`SearchOptions` 新增 `topic_hint/context_terms`，`knowledge_search` / `knowledge_explain` 可传入当前话题词；查询仍只走 SQLite/FTS，不调用 LLM，但会扩大候选集后按 BM25、结果类型、source trust、project scope 和话题命中做本地重排，让外脑优先返回与当前上下文更相关的卡片、事实和来源节点。
26. 已增强本地文件健康诊断：`KnowledgeDoctor` 会检查已导入本地文档是否在磁盘上缺失、不可访问或内容 hash 已变化，分别输出 `missing_local_files`、`inaccessible_local_files`、`changed_local_files`，提示用户刷新、重新导入、禁用或删除对应来源。
27. 已补齐可定位的诊断和批量刷新闭环：`DoctorFinding` 现在可返回 `source_ids/examples`，本地文件缺失、变更、不可访问诊断会带出受影响来源；新增 `RefreshSources` / `KnowledgeRefreshSources` / `knowledge_refresh_sources`，设置页 Doctor 摘要对 `changed_local_files` 提供“刷新受影响来源”按钮，可批量重建变更文件的节点、卡片和事实索引。
28. 已增强文档结构化解析质量：Markdown 不再默认整篇落成单个节点，而是按 ATX 标题拆成带 `level/parent_id/offset/line_start` 的 section 节点；纯文本和 DOCX 会按段落聚合为可控大小的节点，避免超长文档只有一个粗粒度召回单元，同时仍保持查询阶段不依赖 LLM。
29. 已增强 Excel 表格结构化粒度：XLSX 不再默认一张 sheet 一个节点，而是按行内容大小切成多个 `sheet` 节点，每个节点保留 `sheet_name`、`row_range`、`row_start/row_end` 和 offset，召回引用可直接定位到行范围，适合更大的表格资料录入。
30. 已补齐 CSV 表格输入：知识库新增 `csv` source kind，默认目录扫描包含 `.csv`，显式文件录入和设置页扩展选择支持 CSV；解析复用现有 `corelib/excel` CSV reader 和表格行分块逻辑，召回结果保留 `sheet_name/row_range`，后续刷新和 Doctor 漂移检查同样适用。
31. 已增强公共 URL 保存的幂等语义：同一作用域下再次保存相同 `uri/canonical_uri` 的网页时会复用原 `source_id`，清理旧的节点、卡片和事实后重建派生索引，避免用户反复收藏同一页面导致来源列表堆积重复记录；不同项目/租户/用户作用域仍保持隔离。
32. 已补齐显式文本/当前话题保存入口：核心层新增 `SaveText` 和 `TextSaveRequest`，支持 `conversation`、`workflow_artifact`、`text` 三类来源，按内容 hash 和作用域幂等更新；GUI 设置页新增“保存文本知识”区域，智能体工具新增 `knowledge_save_text`，用于用户明确要求“记住/保存/加入知识库”时把结论、笔记或工作流产物写入外脑，后续查询仍走本地 SQLite/FTS。
33. 已增强知识质量诊断：核心层新增 `ListDuplicateCards`，按作用域和规范化 claim 本地识别重复知识卡片；`KnowledgeDoctor` 会输出 `duplicate_card_claims` 诊断并附带样例和来源 ID；智能体工具新增 `knowledge_list_duplicate_cards`，设置页新增“知识质量/查重复卡片”入口。当前只做非破坏式诊断，不自动删除或合并，避免误伤多来源互证。
34. 已补齐来源治理元数据编辑：核心层新增 `UpdateSourceMetadata`，GUI/Wails 暴露 `KnowledgeUpdateSourceMetadata`，智能体工具新增 `knowledge_update_source_metadata`，设置页来源列表提供“编辑”入口，可调整来源标题、`topic_hint` 和 `source_trust`。这些字段用于本地召回重排和来源管理，不修改原始文档/网页内容，查询阶段仍不依赖 LLM。
35. 已补齐本地上下文包能力：核心层新增 `ContextPack`，基于本地 FTS 召回和 topic/context/source trust 重排，把 KnowledgeCard、KnowledgeFact、DocumentNode 命中压缩为带 `K1/K2...` 标签和 citation 的预算化上下文包；GUI/Wails 暴露 `KnowledgeContextPack`，智能体工具新增 `knowledge_context_pack`，设置页搜索区新增“上下文包”按钮用于人工检查。该能力用于回答前组织结构化证据，不调用 LLM，也不是原始 chunk RAG。
36. 已补齐重复知识的非破坏式治理：核心层新增 `knowledge_card_suppressions` 表和 `SuppressDuplicateCards` / `SuppressCards` / `RestoreSuppressedCards` / `ListSuppressedCards`，被抑制卡片会退出 cards/facts 本地召回和上下文包，但来源、节点、原始卡片仍保留，可随时恢复；智能体工具新增 `knowledge_suppress_duplicate_cards`、`knowledge_list_suppressed_cards`、`knowledge_restore_suppressed_cards`，设置页“知识质量”区可对重复组执行“抑制重复项”和恢复召回。
37. 已补齐批量公共 URL 录入：核心层新增 `SaveURLs` 和 `URLBatchSaveResult`，复用单 URL 的公共网络安全校验、抓取、结构化和幂等保存能力，逐条返回 saved/failed/skipped 结果，单条失败不阻断整批；GUI/Wails 暴露 `KnowledgeSaveURLs`，智能体工具新增 `knowledge_save_urls`，设置页“保存公共网页”区支持粘贴多行 URL 并查看每条结果。
38. 已补齐按筛选批量刷新来源：核心层新增 `RefreshSourcesByFilter`，基于现有 `ListSourcesOptions` 的关键词、类型、状态、覆盖度、项目/租户/用户范围筛出目标来源后批量重建索引；GUI/Wails 暴露 `KnowledgeRefreshSourcesByFilter`，智能体工具新增 `knowledge_refresh_sources_by_filter`，设置页“已保存来源”可直接刷新当前筛选结果，适合批量更新网页或本地文档后一次性同步外脑。
39. 已补齐按筛选批量启停来源：核心层新增 `DisableSourcesByFilter` / `EnableSourcesByFilter` 和批量状态更新结果，按现有来源筛选条件非破坏式禁用或恢复一批来源；GUI/Wails 暴露对应接口，智能体工具新增 `knowledge_disable_sources_by_filter` / `knowledge_enable_sources_by_filter`，设置页“已保存来源”可对当前筛选结果执行批量禁用/启用，适合大量录入后的低质量来源临时排除和恢复。
40. 已补齐导入失败/跳过项重试闭环：核心层新增 `RetryImportBatch`，可从旧批次中选择 failed 或指定 skipped/item_ids 文件重新扫描并生成新的重试批次，旧批次明细保留用于审计；GUI/Wails 暴露 `KnowledgeRetryImportBatch`，智能体工具新增 `knowledge_retry_import_batch`，设置页“最近导入”对有失败/跳过项的批次提供“重试失败/跳过”，成功重试后同一路径来源会幂等更新，不堆积重复来源。
41. 已补齐知识库能力清单：核心层新增 `Capabilities`，统一返回默认导入扩展、各来源格式的 parser/search unit/status/refreshable/default_import，以及“查询不依赖 LLM、写入期可选 LLM”的能力声明；GUI/Wails 暴露 `KnowledgeCapabilities`，智能体工具新增 `knowledge_capabilities`，设置页顶部展示已支持格式和待增强格式，避免批量导入前后用户无法判断 `.doc/.xls`、OCR 等能力边界。
42. 已补齐写入期结构化模式控制：核心层新增 `distill_mode`，支持 `auto`、`rules_only`、`llm_if_available` 三种模式；URL 保存、批量 URL、文本保存、目录/文件导入和失败批次重试都可传入该模式。`rules_only` 明确跳过 LLM，`llm_if_available` 在配置可用时主动增强，默认 `auto` 继续规则优先并按本地启发式选择是否调用 LLM；设置页新增“结构化模式”选择，智能体工具同步透出参数，查询阶段仍不依赖 LLM。
43. 已补齐本地敏感信息诊断：核心层新增 `ScanSensitiveContent`，用本地规则扫描已索引的节点和卡片，识别疑似私钥、AWS/GitHub/OpenAI/Slack token、password/token/api_key 字段和 bearer token，诊断结果只返回脱敏片段；`KnowledgeDoctor` 新增 `possible_sensitive_content` 发现项，GUI/Wails 暴露 `KnowledgeScanSensitiveContent`，智能体工具新增 `knowledge_scan_sensitive`，设置页“知识质量”区提供“敏感信息扫描”入口，方便大量导入后快速发现不该进入外脑的资料并执行禁用或删除。
44. 已补齐本地数据库维护能力：核心层新增 `Maintain`，执行 SQLite `integrity_check`、三个 FTS5 表的 `optimize`、WAL checkpoint，并可选执行 `VACUUM` 压缩；GUI/Wails 暴露 `KnowledgeMaintain`，智能体工具新增 `knowledge_maintain`，设置页“知识质量”区提供“维护索引”和“压缩数据库”，用于大量 URL/文档导入后的本地索引维护和空间回收，全程不依赖 LLM。
45. 已补齐敏感来源隔离闭环：核心层新增 `DisableSensitiveSources`，复用本地脱敏扫描命中的 `source_id` 批量禁用来源，不删除原始数据；GUI/Wails 暴露 `KnowledgeDisableSensitiveSources`，智能体工具新增 `knowledge_disable_sensitive_sources`，设置页“知识质量”区提供“隔离敏感来源”入口。命中内容仍只返回脱敏片段，隔离后默认本地搜索/上下文包不再召回这些来源，用户可通过已有启用入口恢复。
46. 已补齐本地快照导出能力：核心层新增 `ExportSnapshot`，以 JSONL 流式导出 Source/DocumentNode/KnowledgeCard/KnowledgeFact，并默认对疑似 token/password/private key 等文本字段做本地规则脱敏；GUI/Wails 暴露 `KnowledgeExportSnapshot`，智能体工具新增 `knowledge_export_snapshot`，设置页“知识质量”区提供“导出快照”入口。该能力用于备份、审计和后续迁移，导出阶段不依赖 LLM，也不会把大库一次性加载进内存。
47. 已补齐快照恢复/迁移导入能力：核心层新增 `ImportSnapshot`，可读取 `knowledge_export_snapshot` 生成的 JSONL，按 Source -> DocumentNode -> KnowledgeCard -> KnowledgeFact 重放入库并同步 FTS；默认 `dry_run=true`、`overwrite=false`，先做预检查和计数，不覆盖现有同 ID 记录。GUI/Wails 暴露 `KnowledgeImportSnapshot`，智能体工具新增 `knowledge_import_snapshot`，设置页“知识质量”区新增快照路径、预检查恢复和恢复快照入口，用于跨机器迁移、备份恢复和审计演练。
48. 已增强快照恢复预检查的可审计性：`ImportSnapshot` 结果新增 `would_import`、`conflicts`、`unknown_records` 和最多 20 条 `conflict_items` 样例，dry-run 时能明确区分“会导入的记录”“因同 ID 已存在而跳过的记录”和“未知类型记录”。设置页恢复结果同步展示冲突数和未知记录数，便于用户在真正恢复前判断是否需要启用覆盖恢复或先清理目标库。
49. 已补齐快照恢复覆盖控制：核心层 `ImportSnapshot` 已支持 `overwrite=true` 覆盖同 ID 记录并重建 FTS，测试覆盖了普通 dry-run、冲突 dry-run、覆盖 dry-run 和覆盖恢复；设置页快照恢复区新增“覆盖已有”开关，恢复结果展示覆盖状态。默认仍为关闭，避免误覆盖当前知识库；需要迁移更新同一批来源时可先预检查再打开覆盖恢复。
50. 已补齐按范围导出快照：核心层 `ExportSnapshot` 新增 `source_ids` 白名单，导出时 Source、DocumentNode、KnowledgeCard、KnowledgeFact 都会按选中来源裁剪；导出 manifest/result 标记 `scoped/source_ids`。智能体工具 `knowledge_export_snapshot` 支持传入 `source_ids`，也可用 query/kind/status/coverage/project/owner/tenant 等现有来源筛选先选出来源再导出；设置页“导出快照”默认导出当前来源列表筛选结果，便于只迁移某个项目或某批来源，而不是每次搬全库。
51. 已补齐快照恢复引用完整性预检查：`ImportSnapshot` 在 dry-run 和真实恢复时会跟踪本次快照已出现的 source/node/card，并结合目标库已有记录，提前发现 DocumentNode 缺失 Source、KnowledgeCard 缺失 Source/Node、KnowledgeFact 缺失 Source/Card 等坏快照问题；结果新增 `missing_references`，失败样例进入 `failures`，设置页恢复结果同步展示缺失引用数量，避免坏 JSONL 到真实恢复阶段才撞库失败。
52. 已补齐真实恢复前自动安全备份：`ImportSnapshot` 在非 dry-run 恢复前默认先调用 `ExportSnapshot` 导出当前本地知识库，返回 `safety_backup_path/safety_backup`，再进入事务重放快照；智能体工具 `knowledge_import_snapshot` 支持 `skip_safety_backup`、`safety_backup_path`、`safety_backup_redact`，设置页新增“恢复前备份”开关并展示备份路径。这样覆盖恢复也有可追溯的本地回退点，dry-run 仍只做检查不写库。
53. 已补齐快照恢复结果样例展示：设置页在恢复预检查/恢复结果中展示最多 5 条 `conflict_items` 和 `failures`，包括记录类型、ID、行号和错误信息。用户不需要打开 JSONL 或开发者控制台，就能判断冲突来自哪些 Source/Node/Card/Fact，坏快照的问题也能直接定位。
54. 已补齐扫描型 PDF 的可诊断提示：Doctor 新增 `pdf_ocr_needed` 检查，对 active PDF 来源若没有任何有意义的文本节点，会明确提示“可能需要 OCR”，并返回 Source 引用样例。这样图片型/扫描件 PDF 不会被误判成普通缺卡片问题，用户能知道应先做 OCR 或导入可选中文本的 PDF。
55. 已把 OCR 诊断接入来源筛选：`ListSourcesOptions.coverage_filter`、`knowledge_list_sources`、批量刷新/启停工具和设置页来源列表均支持 `pdf_ocr_needed`，Doctor 发现扫描型 PDF 后可以直接用同一个过滤条件定位、禁用、刷新或导出受影响来源。
56. 已补齐公共 URL 的本地域名治理：核心层新增 `knowledge_url_domain_policies` 表和 `URLDomainPolicy` 能力，支持 allow/block 域名规则；block 规则优先，存在 allow 规则时未命中的域名默认拒绝。`SaveURL/RefreshURL` 会在保存前检查策略，`knowledge_url_domain_policies` 工具支持 list/replace/check，设置页“保存公共网页”区新增域名策略编辑，Doctor 会提示 `url_domain_policies_active`，便于大批量保存公共网站信息时控制来源范围和质量。
57. 已把 URL 域名策略纳入快照迁移：全量 `ExportSnapshot` 会导出 `url_domain_policy` 记录并统计 `url_policies`；`ImportSnapshot` 支持 dry-run、冲突预览和 overwrite 恢复这些策略；按来源范围的 scoped export 不携带全局策略，避免只迁移某个项目来源时意外覆盖目标机器的全局 URL 治理规则。设置页快照导出/恢复结果同步展示 URL policies 数量。
58. 已补齐 legacy Office 的本地转换解析路径：`.doc/.xls` 不再只是“识别待转换”，解析层会探测 `MACLAW_KNOWLEDGE_SOFFICE`、PATH 中的 `soffice/libreoffice` 以及常见安装路径；找到 LibreOffice/soffice 时，`.doc` 临时转 `.docx` 后走现有 DOCX 段落解析，`.xls` 临时转 `.xlsx` 后走现有表格 row-block 解析。没有转换器时仍返回 unsupported parser，导入项保持 pending/诊断清晰；Capabilities 和 Doctor 会根据本机转换器状态动态展示 `supported_with_local_converter` 或 `recognized_pending_converter`。
59. 已补齐 URL 来源按域名治理视图：`ListSourcesOptions` 新增 `domain`，`knowledge_list_sources`、按筛选导出快照、批量刷新、批量启停和设置页来源列表都支持按域名筛选；筛选 `example.com` 会包含其子域名，筛选具体子域则只定位该子域。`Stats` 新增 `sources_by_domain`，设置页顶部展示 URL 域名分布，便于大批量保存公共网页后按站点治理、导出或刷新。
60. 已把 URL 域名治理延伸到本地召回链路：`SearchOptions`、`knowledge_search`、`knowledge_explain`、`knowledge_context_pack` 和设置页搜索区均支持 `domain` 参数；查询 `example.com` 会在 SQLite/FTS 召回阶段限定到该域名及子域名，查询具体子域名则只返回该站点来源，整个过程仍不调用 LLM。这样用户可以按公共网站录入、治理、导出、刷新，也可以按同一域名做解释召回和上下文包组装。
61. 设置页搜索区继续补齐来源类型过滤：同一个搜索入口现在可同时按搜索范围、结果类型、来源类型和 URL 域名限定召回；例如只搜 `url` + `example.com`，或只搜 `pdf` / `xlsx` 来源。底层仍复用 `SearchOptions.source_kinds` 和本地 SQLite/FTS，不新增查询期 LLM 依赖。
62. 写入期可选 LLM 结构化继续增强：`LLMCardDistiller` 的输出 schema 从“只产出知识卡片”扩展为“卡片 + grounded facts”，每张卡片可携带 `facts[{subject,predicate,object,confidence}]`；落库时会统一校正 `source_id/card_id`、清洗去重并写入 `knowledge_facts` / `knowledge_facts_fts`。这让必要时使用 LLM 提升数据质量，但查询、解释和上下文包仍完全依赖本地结构化索引。
63. 新增本地事实关系图能力：核心层提供 `FactGraph`，GUI/Wails 暴露 `KnowledgeFactGraph`，智能体工具新增 `knowledge_fact_graph`；它从 `knowledge_facts` / `knowledge_facts_fts` 读取实体、谓词和对象，返回 nodes/edges/citations，可按项目范围、来源类型、URL 域名和 disabled 状态过滤。该能力用于关系、依赖、决策、实体网络类问题，仍完全不调用 LLM，也不是原始 chunk RAG。
64. 设置页搜索区接入事实图谱视图：在 `Search / Explain / Context pack` 之外新增 `Fact graph` 操作，复用同一组范围、来源类型和 URL 域名过滤条件，直接展示本地事实边、来源标题、citation 和置信度。这样用户可以在 Knowledge tab 中检查“结构化事实到底长什么样”，不用只依赖搜索结果列表。
65. 事实图谱已补齐本地钻取能力：`SearchOptions`、`knowledge_fact_graph` 和设置页 `Fact graph` 均支持 `entity/predicate` 过滤，可按主体/客体实体和关系谓词收窄事实边；返回结果新增 `top_entities/top_predicates` 摘要，帮助用户在大量录入后快速发现高频实体、关系和下一步可钻取方向。该链路仍只查询 SQLite/FTS 与结构化事实表，不依赖 LLM。
66. 新增本地事实索引目录：核心层提供 `FactIndex`，GUI/Wails 暴露 `KnowledgeFactIndex`，智能体工具新增 `knowledge_fact_index`，设置页搜索区新增 `Fact index` 操作。它从 `knowledge_facts` 聚合高频 `entity/predicate/subject/object`，返回命中次数、来源数、卡片数、关系样例和关联对象样例；用户可以先浏览知识库里有哪些实体/关系，再一键钻入 `Fact graph`。该能力复用现有来源范围、来源类型、URL 域名和 disabled 过滤，查询阶段不调用 LLM，也不新增额外存储。
67. 新增本地实体画像：核心层提供 `EntityProfile`，GUI/Wails 暴露 `KnowledgeEntityProfile`，智能体工具新增 `knowledge_entity_profile`，设置页搜索区新增 `Entity profile` 操作。它复用事实图谱的本地召回结果，聚合某个实体的事实边、关联实体、关系分布和 citations；用户可以从 `Fact index` 点击实体进入画像，再从画像跳回 `Fact graph` 做关系过滤。该能力面向“大量知识录入后快速理解一个实体在库里的上下文”，仍然只依赖 SQLite/FTS 和结构化事实表，不调用 LLM。
68. 新增本地搜索分面能力：核心层提供 `SearchFacets`，GUI/Wails 暴露 `KnowledgeSearchFacets`，智能体工具新增 `knowledge_search_facets`，设置页搜索区新增 `Facets` 操作。它复用现有 SQLite/FTS 召回和 scope/source/domain 过滤，把一次查询聚合为结果类型、来源类型、域名、来源、实体和关系分面；实体/关系分面额外走 fact-only 本地召回补足结构化信号，便于大规模知识库中先缩小范围再进入 `EntityProfile` 或 `FactGraph`，查询阶段仍完全不依赖 LLM。
69. 新增按精确来源收窄本地召回：`SearchOptions` 增加 `source_ids`，所有本地搜索链路（Search/Explain/ContextPack/SearchFacets/FactGraph/FactIndex/EntityProfile）都能限定到一个或多个具体 Source；`knowledge_*` 查询类工具同步支持该参数，设置页搜索区新增 Source ID 输入框，搜索分面里的 Sources 项可一键填入 source_id 并立即重新检索。这样在大批量目录或 URL 录入后，用户可以从分面定位到单个文档/网页来源，再做局部搜索、解释、上下文包或关系钻取，仍然只依赖 SQLite/FTS 与结构化事实表。
70. 设置页来源管理与局部召回打通：已保存来源列表新增“在此搜索”动作，点击后会把该来源的 `source_id`、来源类型以及 URL 域名（如适用）同步到搜索区；若当前已有查询词，则立即在该单一来源内重新执行本地搜索。这样用户从 Doctor、覆盖度筛选、批量导入结果或来源列表定位到某个文档/网页后，可以直接进入局部召回与解释链路，不再需要手动复制 source_id。
71. 搜索区补充可见筛选状态：设置页搜索控件下方新增当前筛选条，展示结果类型、来源类型、URL 域名和 source_id 等有效限制，并提供“清除筛选”动作；清除后如果已有查询词，会立即按全局条件重新执行本地搜索。这样从来源分面或“在此搜索”进入局部召回后，用户不会被隐藏过滤条件困住，也能快速回到全库召回。
72. 搜索分面支持空查询浏览模式：`SearchFacets` 在没有 query 时不再直接返回空结果，而是按当前 scope/source/domain/source_id 过滤条件聚合本地来源、结果类型、域名、高频实体和关系；`knowledge_search_facets` 的 query 参数变为可选，设置页 `Facets` 按钮也可在空搜索框下直接使用。用户导入一批文档或 URL 后，可以先不提问题，直接浏览知识库里有哪些来源类型、站点、实体和关系，再选择局部搜索或进入实体画像/事实图谱。
73. 空查询分面浏览的统计精度增强：`SearchFacets` browse mode 中 `count`、source kind 和 domain 分布不再只基于最近返回的 Sources 列表，而是按当前过滤范围做全量来源计数；source kind 使用 SQL 聚合，domain 使用本地 URL/站点名归一化统计，Sources 列表仍保留有限条最近来源用于点击钻取。这样大规模知识库中点击空查询 `Facets` 时，顶部来源类型和域名分布更接近真实全库结构。
74. 设置页搜索筛选条已支持单项移除：结果类型、来源类型、URL 域名和 `source_id` 会以可点击 chip 展示，点击单个 chip 即可移除对应条件并在已有查询词下自动重搜；仍保留“清除筛选”用于一键回到全库召回。
75. 已补齐来源刷新前变更预览：核心层新增 `PreviewSourceRefresh` / `SourceChangePreview`，可对 URL/HTML 和本地文档来源做只读重抓取或重解析，比较内容 hash、解析节点数量和新增/移除节点样例，不写库、不调用 LLM；GUI/Wails 暴露 `KnowledgePreviewSourceRefresh`，智能体工具新增 `knowledge_preview_source_refresh`，设置页来源列表新增“预览刷新”，用户可以先判断是否需要重建节点、卡片和事实索引。
76. 已把刷新预览扩展到批量治理：核心层新增 `PreviewSourcesRefresh` 和 `PreviewSourcesRefreshByFilter`，可按 source_ids 或现有来源筛选条件只读预览一批 URL/文档来源的 changed/unchanged/failed 分布；GUI/Wails 暴露批量预览接口，智能体工具新增 `knowledge_preview_sources_refresh` / `knowledge_preview_sources_refresh_by_filter`，设置页“已保存来源”增加“预览筛选来源”，大规模刷新前可以先估算真实变更面。
77. 已补齐“只刷新变化项”的高效维护路径：核心层新增 `RefreshChangedSources` / `RefreshChangedSourcesByFilter`，先执行本地只读预览，再仅对 `changed=true` 且无错误的来源重建节点、卡片和事实；GUI/Wails 暴露 `KnowledgeRefreshChangedSources*`，智能体工具新增 `knowledge_refresh_changed_sources` / `knowledge_refresh_changed_sources_by_filter`，设置页提供“刷新预览变化项”和“仅刷新变化项”，避免大规模来源维护时重复刷新未变化文档或网页。
78. 已新增本地知识建议能力：核心层新增 `Suggest` / `KnowledgeSuggestResult`，从结构化 facts、来源、URL 域名和来源类型中聚合 entity/predicate/source/domain/source_kind 建议；GUI/Wails 暴露 `KnowledgeSuggest`，智能体工具新增 `knowledge_suggest`，设置页搜索区新增“本地建议”。该能力用于大库中先发现可钻取对象再搜索、画像或建图，全程只读 SQLite/FTS，不依赖 LLM。
79. 已补齐来源版本审计：新增 `knowledge_source_versions` 表和 `SourceVersion`，每次文本保存、公共 URL 保存、目录/文件导入、来源刷新都会在同一事务内记录来源 hash、状态、节点数、卡片数、事实数和写入原因。GUI/Wails 暴露 `KnowledgeListSourceVersions`，智能体工具新增 `knowledge_list_source_versions`，`knowledge_source_detail` 和设置页来源详情同步展示最近版本历史，便于追踪网页/文档在多次保存和刷新后的结构化质量变化。
80. 已补齐公共网页批量发现入口：核心层新增 `DiscoverURLs` / `URLDiscoveryResult`，可从粘贴文本、HTML、sitemap XML 或混合笔记中本地解析 URL，支持 base_url 解析相对链接、same-domain 过滤、公共 URL 校验和域名策略拒绝；发现阶段不抓网页、不写库。GUI/Wails 暴露 `KnowledgeDiscoverURLs`，智能体工具新增 `knowledge_discover_urls`，设置页“保存公共网页”区新增“发现链接”，发现出的候选会回填到批量保存列表，再复用现有 `SaveURLs` 安全录入链路。
81. 已补齐本地派生知识重建能力：核心层新增 `RebuildSourceDerived` / `RebuildSourcesDerived*`，可在不重新抓网页、不重新读取文件、不改变 `DocumentNode` 的情况下，从已有解析节点重建 KnowledgeCard 和 KnowledgeFact，并记录 `rebuild_derived` 来源版本。GUI/Wails 暴露单来源和筛选批量重建接口，智能体工具新增 `knowledge_rebuild_source_derived`、`knowledge_rebuild_sources_derived`、`knowledge_rebuild_sources_derived_by_filter`，设置页来源列表新增“重建卡片/事实”。这用于修复 Doctor 报告的 `missing_cards/missing_facts`，也用于升级结构化规则或启用 LLM 提质后批量补强旧知识。
82. 已补齐来源标签/集合的发现与治理闭环：来源可挂多个标签，来源列表、批量刷新/启停、搜索、解释、上下文包、事实图谱、事实索引、实体画像和本地建议均可按 `labels` 限定范围；`Stats` 新增 `sources_by_label`，搜索分面新增 `labels`，设置页可筛选、展示、编辑并从分面点击进入标签范围。标签随快照以 `source_label` 记录导出/恢复，查询阶段仍只依赖 SQLite/FTS，不调用 LLM。
83. 已将来源标签纳入本地建议链路：`KnowledgeSuggest` / `knowledge_suggest` 新增 `label` 建议类型，可从当前 scope/source/domain/label 过滤后的来源集合中聚合高频标签并返回样例来源；设置页“本地建议”点击标签会直接进入对应标签限定的召回范围。该能力用于大库浏览和标签治理，全程仍只读本地 SQLite/FTS 与结构化表，不调用 LLM。
84. 已把标签治理前移到写入入口：目录导入、显式文件导入、单 URL、批量 URL 和显式文本保存均支持传入 `labels`，并可启用规则型 `auto_labels` 自动追加 `kind:*`、`scope:*`、`domain:*`、`folder:*` 等标签。重复保存同一内容时会追加新标签而不是清空旧标签，保证“先入库、后整理”和“边保存、边归档”两种工作方式都成立。
85. 已补齐设置页标签目录：知识库 Tab 的已保存来源区会展示当前筛选范围内的标签集合、数量和样例来源，支持一键按标签过滤来源、一键进入该标签范围的本地召回、以及对标签做重命名/删除治理。Doctor 同步新增 `unlabeled_sources` 诊断，提示活跃但未归档的来源，帮助大规模录入后快速发现需要整理的知识集合。
86. 已优化智能体侧保存默认值：`knowledge_save_url`、`knowledge_save_urls`、`knowledge_save_text`、`knowledge_import_directory`、`knowledge_import_files` 在未显式传入 `auto_labels` 时默认启用规则自动标签，确保对话中触发的 URL 保存、文本记忆和文件导入天然带有可治理的类型、域名、scope 和目录标签；如用户需要纯手工归档，仍可显式传入 `auto_labels:false`。
87. 已把 Doctor 诊断升级为可执行筛选：`DoctorFinding` 新增 `filter` 字段，可直接携带 `status`、`coverage_filter`、`source_kinds` 或 `source_ids` 等本地来源过滤条件；`ListSourcesOptions` 新增 `source_ids`，覆盖度筛选新增 `missing_labels`。设置页 Doctor 面板每条可定位 finding 都提供“查看受影响来源”，无需用户复制 source_id 或手工猜筛选条件；智能体侧也可直接复用 `filter` 进入 `knowledge_list_sources`、批量刷新、禁用或标签治理。
88. 已补齐历史来源的自动标签回填能力：核心层新增 `BackfillSourceAutoLabels`，可按 `source_ids` 或来源筛选条件批量追加缺失的 `kind:*`、`scope:*`、`domain:*`、`folder:*` 规则标签，保留已有手工标签且支持 dry-run 预览；GUI/Wails 暴露 `KnowledgeBackfillSourceAutoLabels`，智能体工具新增 `knowledge_backfill_source_auto_labels`，设置页已保存来源区新增“回填自动标签”按钮。这样旧数据也能进入标签目录、Doctor `missing_labels` 修复和后续本地召回治理闭环。
89. 已把 Doctor 的未打标诊断接入一键修复：设置页 Doctor 面板在 `unlabeled_sources` finding 上直接显示“回填标签”，点击后复用 finding 自带的 `filter` 调用 `KnowledgeBackfillSourceAutoLabels`，自动追加规则标签并刷新来源列表、标签目录和健康诊断。用户无需先切到来源区、再手动选择筛选条件，诊断、定位、修复形成一个 UI 闭环。
90. 已把 Doctor 的派生知识缺口接入一键修复：设置页 Doctor 面板在 `sources_without_cards` 和 `sources_without_facts` finding 上直接显示“重建派生知识”，点击后复用 finding 自带的 `filter` 调用 `KnowledgeRebuildSourcesDerivedByFilter`，从已解析 `DocumentNode` 重建 KnowledgeCard/KnowledgeFact，不重新抓网页、不重新读取文件。这样“有节点但缺卡片/事实”的旧数据或失败蒸馏结果，可以从诊断区直接修复并刷新本地召回质量。
91. 已把 Doctor 的敏感内容诊断接入一键隔离：设置页 Doctor 面板在 `possible_sensitive_content` finding 上直接显示“隔离受影响来源”，点击后复用现有 `KnowledgeDisableSensitiveSources` 链路批量禁用命中的敏感来源，并同步刷新敏感扫描结果、隔离结果、来源列表、Doctor 状态和当前搜索结果。这样 API key、token、私钥等高风险内容被本地规则发现后，可以从诊断区直接进入可逆隔离状态，查询阶段默认排除这些来源，避免把敏感材料继续带入本地召回。
92. 已把 Doctor 的重复知识诊断接入治理入口：设置页 Doctor 面板在 `duplicate_card_claims` finding 上直接显示“查看重复组”，点击后调用 `KnowledgeListDuplicateCards` 加载重复 KnowledgeCard claim 分组，并复用现有“抑制重复项/恢复抑制”区域完成可逆治理。这样重复结论不需要先切到维护区手动查重，诊断发现后可以直接进入重复组检查，再决定是否保留最佳卡片并从本地召回中隐藏其余重复卡片。
93. 已优化 Doctor 面板的可见诊断范围：设置页顶部不再只展示前三条 finding，而是最多展示八条并提示剩余数量，确保覆盖度缺口、敏感内容、重复知识、标签缺失、文件变化等多个诊断同时出现时，关键的一键治理按钮仍然能直接暴露给用户。这样大规模知识库进入维护状态后，不会因为诊断排序把后续可执行问题藏起来。
94. 已补充重复知识的批量治理动作：设置页重复卡片区域在加载重复组后提供“抑制已加载重复组”，会逐组调用现有 `KnowledgeSuppressDuplicateCards`，保留每组评分最高的卡片并把其余重复卡片加入 `knowledge_card_suppressions`，随后刷新重复组、已抑制列表和当前搜索结果。该操作不删除来源、不删除节点、不删除原始卡片，后续仍可通过“恢复抑制”把卡片重新纳入本地召回。
95. 已补齐智能体侧重复知识批量治理工具：新增 `knowledge_suppress_duplicate_groups`，可按 `limit/project_path/owner_id/tenant_id` 批量处理当前检测出的重复 KnowledgeCard claim 分组，逐组保留最佳卡片并抑制其余重复卡片，返回 processed/skipped/suppressed/errors 明细。这样当 `knowledge_doctor` 报告 `duplicate_card_claims` 时，智能体可以先用 `knowledge_list_duplicate_cards` 展示重复组，再在用户明确要求治理时一次性执行可逆抑制。
96. 已补齐重复抑制的批量恢复闭环：设置页“已抑制卡片”区域新增“恢复已加载抑制项”，可一次性恢复当前加载的 suppressed cards；智能体工具新增 `knowledge_restore_suppressed_cards_bulk`，支持按 `limit` 和 `reason_contains` 批量恢复被抑制的卡片。这样批量去重不是单向操作，用户或智能体都可以把误抑制的卡片重新纳入本地召回。
97. 已新增本地来源质量画像：核心层提供 `SourceQualityReport`，按来源聚合解析节点、知识卡片、结构化事实、标签、状态、可信度、敏感命中和重复 claim 信号，输出 0-100 分、grade、signals 和建议动作，全程只读 SQLite/FTS 与本地规则，不调用 LLM。GUI/Wails 暴露 `KnowledgeSourceQualityReport`，设置页“知识质量”区新增“来源质量”按钮，智能体工具新增 `knowledge_source_quality`，用于在大规模录入后快速判断哪些来源应刷新、重建、补标签、抑制重复、隔离或继续保留。
98. 已把来源质量画像接入 Doctor：`KnowledgeDoctor` 会基于本地 `SourceQualityReport` 发现低分来源并输出 `low_quality_sources` finding，携带 source_ids、示例分数和可直接用于来源列表定位的 filter；设置页 Doctor 面板对该 finding 新增“查看质量”动作，可直接加载对应来源质量项。这样覆盖缺口、敏感命中、重复 claim、低可信度和未打标等多个信号可以被聚合成一条可执行诊断，不需要用户逐项手动排查。
99. 已增强来源质量画像的巡检效率：`SourceQualityReport` 结果默认按低分优先排序，同分时优先展示敏感命中和重复 claim 更多的来源；`ListSourcesOptions` 新增 `quality_grade/quality_grades/min_quality_score/max_quality_score`，`knowledge_source_quality` 工具和设置页“来源质量”入口均可按质量等级或最高分切片。这样用户或智能体可以直接拉取 `poor` 或 55 分以下来源，优先处理最影响召回质量和安全性的知识输入。
100. 已把来源质量画像从“来源列表”升级为“治理摘要”：`SourceQualityReport` 新增 `signals` 和 `actions` 聚合，统计当前质量切片中最常见的问题信号和建议动作，例如 `missing_nodes`、`missing_cards`、`possible_sensitive_content`、`duplicate_card_claims` 以及对应的刷新、重建、隔离、抑制、补标签动作。设置页质量报告顶部展示 Top signals / Top actions，智能体工具返回同样结构，便于先判断整批知识库最主要的治理方向，再进入具体来源处理。
101. 已把来源质量画像接入一键派生知识修复：设置页质量报告会统计当前切片里带有 `missing_cards` 或 `missing_facts` 的来源，并提供“重建质量缺口”动作，调用 `KnowledgeRebuildSourcesDerived` 对这些 source_id 从已有 `DocumentNode` 重建 KnowledgeCard/KnowledgeFact。该动作不重新抓网页、不重新读取文件，适合在质量巡检后直接修复“已解析但召回弱”的来源，并刷新质量报告、来源列表和当前搜索结果。
102. 已补齐智能体侧质量缺口自动修复工具：新增 `knowledge_rebuild_quality_gaps`，会先用本地 `SourceQualityReport` 按来源筛选、质量等级和分数切片识别带有 `missing_cards` 或 `missing_facts` 信号的来源，再调用 `KnowledgeRebuildSourcesDerived` 从已有 `DocumentNode` 重建派生 KnowledgeCard/KnowledgeFact。该工具的选择阶段只读 SQLite/FTS 与本地规则，不依赖 LLM；只有在显式传入 `distill_mode=llm_if_available` 时，写入阶段才会在可用时借 MaClaw LLM 提升结构化质量。
103. 已把来源质量画像接入标签治理闭环：新增智能体工具 `knowledge_backfill_quality_labels`，可先按本地质量报告筛出带 `missing_labels` 信号的来源，再批量回填 `kind:*`、`scope:*`、`domain:*`、`folder:*` 等规则标签；设置页质量报告同步新增“回填质量标签”动作。这样质量巡检不仅能发现未归档来源，也能直接把旧数据纳入标签目录、来源筛选和后续本地召回治理，全程不调用 LLM。
104. 已继续把来源质量画像接入安全与去重治理：新增 `knowledge_disable_quality_sensitive_sources`，可从质量切片中隔离带 `possible_sensitive_content` 信号的来源；新增 `knowledge_suppress_quality_duplicate_groups`，可从质量切片中定位带 `duplicate_card_claims` 的来源并可逆抑制相关重复卡片组。设置页质量报告同步提供“隔离敏感来源”和“抑制重复组”动作，仍然保留来源、节点和原始卡片，只改变默认本地召回参与状态或重复卡片抑制状态。
105. 已新增本地质量治理计划：核心层提供 `SourceQualityMaintenancePlan`，在不写库、不调用 LLM 的情况下，把 `SourceQualityReport` 里的信号排序成可执行清单，覆盖敏感来源隔离、缺节点刷新/重导入、派生卡片/事实重建、重复卡片组抑制和自动标签回填，并附带推荐智能体工具与参数。GUI/Wails 暴露 `KnowledgeSourceQualityMaintenancePlan`，设置页“知识质量”区新增“治理计划”，智能体工具新增 `knowledge_quality_maintenance_plan`，用于先审阅整批维护顺序，再按风险执行具体治理动作。
106. 已把质量治理计划推进到可执行闭环：核心层新增 `ExecuteSourceQualityMaintenancePlan`，可按计划执行敏感来源隔离、派生知识重建、自动标签回填和重复卡片组抑制，并对缺节点刷新/重导入保持跳过提示；智能体工具新增 `knowledge_execute_quality_maintenance_plan`，默认 `dry_run=true` 先预演，只有显式关闭 dry-run 才写库。设置页“知识质量”区新增“预演执行”和“执行计划”，执行后会刷新质量报告、来源列表和当前召回结果。
107. 已给质量治理执行器补上策略护栏：`SourceQualityMaintenanceExecuteRequest` 新增 `max_sources_per_action`、`allow_sensitive_disable` 和 `allow_duplicate_suppression`。dry-run 仍可完整预演；真实写库时若单个动作影响来源数超过阈值，或未显式允许敏感来源隔离/重复卡片抑制，会跳过该动作并返回明确原因。设置页执行计划默认限制单动作 100 个来源，并在用户点击执行时显式传入可逆治理许可；智能体工具仍默认 dry-run，适合先审阅再执行。
108. 已新增知识质量治理策略预设：核心层提供 `SourceQualityMaintenancePolicies`，内置 `conservative`、`balanced`、`enriched`、`strict` 四档策略，明确每档动作集合、单动作来源上限、是否允许敏感来源隔离/重复抑制、以及是否可能在写入结构化阶段使用 LLM。查询链路仍标记为 `query_requires_llm=false`；只有 `enriched` 在重建派生知识时默认使用 `llm_if_available` 提升存储质量。GUI/Wails 暴露 `KnowledgeQualityMaintenancePolicies`，设置页“知识质量”区可选择策略并显示说明，智能体工具新增 `knowledge_quality_maintenance_policies` 且 `knowledge_execute_quality_maintenance_plan` 支持 `policy` 参数。
109. 已把设置页目录批量录入默认扩展名补齐为完整文档入口：默认包含 `.docx/.doc/.pdf/.xlsx/.xls/.csv/.md/.txt`。其中 `.doc/.xls` 会复用本地 LibreOffice/soffice 转换器探测能力，转换器可用时进入正常解析、卡片和事实生成链路；不可用时仍会被识别、记录并由 Capabilities/Doctor 给出明确诊断，而不是静默丢失。智能体导入工具说明也同步改为“老 Office 格式依赖本地转换器”，避免把用户提供的 doc/excel 文件误判为不可支持。
110. 已把“与当前话题有关联性”从搜索重排扩展成可见的本地主题关联报告：核心层新增 `TopicRelevance`，按 `topic_hint/context_terms/query` 拆出主题词，并在来源元数据、标签、KnowledgeCard、KnowledgeFact 和 DocumentNode 中本地计分，返回相关来源、命中主题词、标签命中、卡片/事实/节点命中数和分数。GUI/Wails 暴露 `KnowledgeTopicRelevance`，设置页检索区新增“话题关联”按钮，智能体工具新增 `knowledge_topic_relevance`。该报告只读 SQLite/结构化表，不调用 LLM，可在保存新知识前判断当前话题已有多少关联来源，也可在回答前快速定位与当前上下文最相关的知识源。
111. 已把临时话题关联推进为可持久的外脑网络：SQLite 新增 `knowledge_source_links`，用于保存 `topic_related` 来源关系、关联分数、命中主题词和证据摘要。`SaveText`、`SaveURL` 和文件导入在提交成功后会自动刷新当前来源的本地话题关联边；核心层提供 `RefreshSourceTopicLinks`、`RefreshSourceTopicLinksByFilter` 和 `ListSourceLinks`，GUI/Wails 与智能体工具新增 `knowledge_list_source_links`、`knowledge_refresh_topic_links`，来源详情区也展示“话题关联来源”。这样知识库不只是孤立卡片和事实，而会逐步形成可解释、可维护、无需查询时 LLM 的来源关系图。
112. 已把来源关系图纳入快照迁移闭环：`ExportSnapshot` 现在会导出 `source_link` 记录，`ImportSnapshot` 会校验两端 `source_id` 是否存在、处理复合键冲突、支持 dry-run/overwrite，并在恢复后重建 `knowledge_source_links`。范围导出时只保留两端来源都在选中集合内的关系，避免半条边引用缺失来源；脱敏导出也会同步处理关联命中词和证据摘要。这样备份/恢复不只迁移原始来源、节点、卡片和事实，也能保留外脑已经沉淀出的主题关联网络。
113. 已把来源关系图覆盖率纳入健康统计：`Stats` 新增 `source_links` 和 `sources_without_links`，`ListSources` / 智能体工具 / 设置页覆盖率筛选新增 `missing_links` 与 `has_links`，Doctor 在多来源知识库中会提示 `sources_without_links`，并携带可直接定位的 filter。这样大规模批量录入后可以区分“内容已解析但尚未接入主题网络”的来源，后续可通过刷新 topic links 把孤立资料接入外脑图谱。
114. 已把来源关系图接入质量治理计划：`SourceQualityReport` 会把多来源知识库中尚无 `topic_related` 边的活跃来源标记为 `missing_links`，`SourceQualityMaintenancePlan` 新增 `refresh_topic_links` 动作，`ExecuteSourceQualityMaintenancePlan` 可在 dry-run 后批量调用本地 `RefreshSourceTopicLinks` 重建来源关联图。四档治理策略均包含该动作，且全程只使用本地话题相关性，不依赖查询时 LLM；这样“导入、诊断、计划、执行、再诊断”的维护闭环覆盖到外脑网络本身。
115. 已新增本地来源图谱读取能力：核心层提供 `SourceGraph`，按来源筛选条件返回 `nodes/edges/isolates`，边来自已持久化的 `knowledge_source_links` 并按无向关系折叠去重，节点携带类型、状态、标签、覆盖率和 degree。GUI/Wails 暴露 `KnowledgeSourceGraph`，智能体工具新增 `knowledge_source_graph`，用于在不调用 LLM 的情况下查看当前项目、个人外脑或指定筛选切片的来源网络结构，快速发现高连接来源和孤立资料。
116. 已把本地来源图谱接入设置页检索区：新增“来源图谱”按钮，复用当前关键词、来源类型、URL 域名、来源 ID 和标签筛选，直接调用 `KnowledgeSourceGraph` 渲染高连接来源、已持久化关联边和孤立来源。该视图只读取 SQLite 中的 `knowledge_source_links` 与来源元数据，不在查询阶段调用 LLM；用户可以从图谱节点一键进入“在此来源内搜索”，把诊断到的孤立资料或高连接资料继续带入本地召回闭环。
117. 已把 `search_scope` 下沉到来源列表筛选层：`ListSourcesOptions` 现在支持 `all/project/personal` 作用域，GUI/Wails 会在 `project` 作用域下自动补当前项目路径，在 `personal/local` 作用域下只读取空 `project_path` 的个人外脑来源。来源图谱、质量治理、标签汇总和智能体侧来源工具因此可以复用同一套本地作用域语义，避免“个人资料混入项目图谱”或“项目资料污染个人外脑诊断”。
118. 已给设置页来源图谱增加本地 SVG 预览：在详细列表上方按连接度展示 Top 来源节点和已持久化边，节点大小随 degree 变化，点击节点可直接进入该来源内搜索。该预览不引入前端新依赖，不访问网络，不调用 LLM，只把 SQLite 中已经沉淀的来源关系以更直观的方式暴露给用户，便于快速发现知识枢纽和孤立资料。
119. 已把来源图谱升级为本地图分析报告：`SourceGraph` 现在会计算连通组件、最大组件规模、整体密度、组件内边数、平均度、Top 节点和 Top 关联词，并给每个节点标注 `component_id`。设置页同步展示组件摘要，智能体工具 `knowledge_source_graph` 也会返回这些指标。这样大规模外脑不只知道“有哪些边”，还能本地判断知识库是连成一张可用网络，还是分散成多个孤岛。
120. 已把来源图谱健康接入 Doctor 诊断闭环：当知识库已有持久化来源边但整体图谱被切成多个连通组件时，`KnowledgeDoctor` 会报告 `source_graph_fragmented`；当存在无任何边的孤立来源时，会报告 `source_graph_isolates`，并携带受影响 `source_ids/examples/filter`。设置页 Doctor 面板会为这些 finding 直接暴露“刷新关联”动作，复用 `KnowledgeRefreshSourceTopicLinksByFilter`，让大规模导入后的“外脑没有连成网”问题可以从诊断区直接进入本地修复闭环。
121. 已新增聚焦来源邻域能力：核心层提供 `SourceNeighborhood(source_id, depth, limit, edge_limit)`，从一个来源出发沿持久化 `knowledge_source_links` 做本地 BFS，最多支持三跳，并复用 `SourceGraph` 返回邻域内节点、边、组件、孤岛、密度和焦点来源信息。GUI/Wails 暴露 `KnowledgeSourceNeighborhood`，智能体工具新增 `knowledge_source_neighborhood`；设置页来源详情会自动展示该来源的二跳邻域子图。这样用户或智能体不必浏览全库图谱，就能围绕某份文档/网页快速查看它连接到哪些附近知识，仍然全程不调用 LLM。
122. 已把来源邻域并入智能体来源详情闭环：`knowledge_source_detail` 默认会随来源元信息、解析节点、卡片、事实和版本历史一起返回聚焦邻域图，并支持 `include_neighborhood`、`neighborhood_depth`、`neighborhood_limit`、`edge_limit` 控制范围。这样智能体检查某个来源质量时，不需要再额外调用全库图谱，也能看到该来源附近的关联来源、组件和孤立情况；该查询仍只读取 SQLite/FTS 与持久化来源边，不依赖 LLM。
123. 已新增两个来源之间的本地路径解释能力：核心层提供 `SourcePath(from_source_id, to_source_id, max_depth, edge_limit)`，沿 `knowledge_source_links` 做最短路径 BFS，返回有序路径节点、每一跳关系、分数、命中词和证据摘要。GUI/Wails 暴露 `KnowledgeSourcePath`，智能体工具新增 `knowledge_source_path`，用于回答“这两份资料为什么会连上/中间经过哪些知识源”。该能力只读取已持久化的来源关系图，不在查询阶段调用 LLM。
124. 已把来源路径解释接入设置页来源详情：在某个来源的“话题关联来源”列表中，可直接对任一关联来源点击“路径”，前端调用 `KnowledgeSourcePath` 展示从当前来源到目标来源的有序节点链、每一跳关系、分数、命中词和证据摘要，并可继续跳转到目标来源内搜索。这样用户在 GUI 中也能检查外脑图谱的连接理由，而不必只依赖智能体工具输出。
125. 已增强来源路径的可诊断性：`SourcePath` / `knowledge_source_path` 现在返回 `visited_count`、`searched_edge_count` 和 `truncated`，设置页路径面板同步展示已访问节点数、已搜索边数和是否被预算截断。这样查不到路径时可以区分“确实没有连接”和“深度/边数预算不足”，便于后续对大规模知识库调参和补边。
126. 已新增来源关联候选预览能力：核心层提供 `PreviewSourceTopicLinks(source_id, limit)`，复用本地 `TopicRelevance` 计算某个来源最可能连接到的候选来源，但不写入 `knowledge_source_links`；GUI/Wails 暴露 `KnowledgePreviewSourceTopicLinks`，智能体工具新增 `knowledge_preview_topic_links`，设置页来源行新增“预览关联”按钮并在来源详情展示候选关系、分数、命中词和证据摘要。这样孤立来源可以先被诊断和审阅，再决定是否执行真正的“关联”写入。
127. 已新增人工确认的单条来源关联写入：核心层提供 `LinkSources`，按 `source_id/related_source_id/relation/score/terms/evidence` 双向写入 `knowledge_source_links`，并校验两端来源存在且不能自关联；GUI/Wails 暴露 `KnowledgeLinkSources`，智能体工具新增 `knowledge_link_sources`，设置页“关联预览”候选项新增“写入关联”按钮。这样外脑图谱既可以批量自动补边，也可以由用户或智能体在明确意图下把某条高价值候选关系固化下来。
128. 已补齐单条来源关联的可逆治理：核心层新增 `UnlinkSources`，按 `source_id/related_source_id/relation` 双向删除 `knowledge_source_links` 并返回删除数量；GUI/Wails 暴露 `KnowledgeUnlinkSources`，智能体工具新增 `knowledge_unlink_sources`，设置页已保存的“话题关联来源”列表新增“移除关联”按钮。这样人工写入或自动补边产生的错误关系可以被精确撤销，不需要删除来源或重建整张图谱。
129. 已补齐来源关联治理审计：SQLite 新增 `knowledge_source_link_events`，人工 `LinkSources` 和 `UnlinkSources` 会记录 `link/unlink` 事件、关系、分数、命中词、证据摘要和时间；核心层提供 `ListSourceLinkEvents`，GUI/Wails 暴露 `KnowledgeListSourceLinkEvents`，智能体工具新增 `knowledge_list_source_link_events`，设置页来源详情新增“关联事件”列表。这样外脑图谱的人工修正不再是黑盒，后续可追踪是谁/何时/依据什么把资料连上或断开。
130. 已把来源关联审计并入智能体常用的来源详情入口：`knowledge_source_detail` 新增 `link_events_limit`，返回 `link_events` 与 `link_event_count`，与来源节点、卡片、事实、版本、已保存关联和邻域图同包输出。这样 agent 在回答“这个来源存了什么、怎么连到别的资料、最近是否被人工修正过”时，不需要额外调用审计工具也能拿到完整本地证据链。
131. 已让来源关联审计进入快照迁移链路：`ExportSnapshot` 会导出 `source_link_event` 记录并计入 `source_link_events`，敏感内容按既有规则脱敏；`ImportSnapshot` 支持 dry-run、overwrite、引用校验和冲突检测，恢复后 `ListSourceLinkEvents` 可直接看到迁移过来的人工关联历史。这样知识库备份/迁移不只保留资料和图谱边，也保留图谱治理过程。
132. 已把来源关联审计纳入知识库统计：`Stats` 新增 `source_link_events` 和 `link_events_by_action`，本地统计可直接看到人工 link/unlink 治理动作数量和分布，测试覆盖手工关联、移除关联后的统计结果。这样 Doctor、设置页和智能体工具后续都能复用同一份统计口径，不需要重复扫描审计表。
133. 已把图谱关联统计露出到设置页顶部：知识库 Tab 的总览现在显示 `Links` 与 `Link events`，统计拆分区新增 link/unlink 事件分布。用户批量导入、自动补边、人工修正后，可以直接在设置页判断“资料是否连起来”和“图谱是否有治理痕迹”。
134. 已新增来源时间线能力：核心层提供 `SourceTimeline`，本地合并 source 创建/更新、`knowledge_source_versions` 保存/刷新/重建记录、`knowledge_source_link_events` 人工 link/unlink 审计事件，并按时间倒序返回 `local_source_timeline_no_llm`；GUI/Wails 暴露 `KnowledgeSourceTimeline`，智能体工具新增 `knowledge_source_timeline`，`knowledge_source_detail` 也会内嵌 `timeline`，设置页来源详情新增“来源时间线”。这样排查单条知识的生命周期时，不再需要分别查看版本、关联事件和状态字段。
135. 已同步设置页的审计可见性细节：编辑来源元数据后会刷新 nodes/versions/links/link events/timeline/neighborhood/cards/facts，快照导出和恢复摘要也显示 `source_versions/source_links/source_link_events`。这样用户在设置页做元数据治理或迁移演练时，可以直接看到版本链、图谱边和关联审计是否被包含。
136. 已新增来源摘要能力：核心层提供 `SourceDigest`，把单个来源的元数据、标签、主题、实体、代表节点、卡片、事实、关联来源和时间线压缩为一份本地摘要，并明确返回 `local_source_digest_no_llm` 与 `query_does_not_require_llm`；GUI/Wails 暴露 `KnowledgeSourceDigest`，智能体工具新增 `knowledge_source_digest`，`knowledge_source_detail` 也会内嵌 `digest`；设置页来源详情新增“来源摘要”，用于快速判断某个文档、表格或网页到底沉淀出了什么知识，而不依赖查询阶段 LLM。
137. 已把知识存储触发语义写入 IM 系统提示：只有当用户明确表达“保存到知识库/记住/加入外脑/归档/以后可查/批量录入”等长期保存意图时，智能体才调用 `knowledge_save_url(s)`、`knowledge_import_files/directory` 或 `knowledge_save_text`；用户只是要求查看、总结或搜索时不会自动写入。提示中同时规定：公共 URL 可先用 `knowledge_discover_urls` 提取候选，查询/解释/上下文包/来源摘要默认走本地只读工具，写入阶段可在配置可用时用 LLM 辅助结构化但必须能回退到规则链路。
