# MaClaw Knowledge V2 Structured Data Design

## 背景

当前知识库已经支持导入 `xlsx`、`xls`、`csv`，但表格会被转换为按行拼接的文本节点，再进入 `FTS + CJK LIKE + embedding + card/fact` 检索链路。这对自然语言召回有帮助，但对 Excel/CSV 这类结构化数据仍有明显损失：

- 表头和单元格之间的字段关系不够稳定。
- 数字、日期、布尔值无法高效做范围查询。
- 大表被切成文本块后，证据容易过长，命中行不够精准。
- 检索参数没有列级过滤能力。

V2 的目标是把 spreadsheet 作为一等知识类型处理，同时保留自然语言问答能力。

## 目标

- Excel/CSV 导入后保留 `sheet -> column -> row -> cell` 结构。
- 支持列名等值、包含、数字范围、日期范围等结构化检索。
- 支持行级 FTS 与行级 embedding，提升自然语言问题的命中精度。
- 将高价值行派生成 `card/fact`，继续服务现有上下文包、实体、事实图等能力。
- 新程序启动时自动检测旧格式并迁移到 V2。

## 非目标

- 第一阶段不删除旧表。旧链路仍可运行，直到 V2 搜索与导入全部切换完成。
- 第一阶段不要求一次性完成全部旧数据的高质量重建。原文件存在时重读重建，原文件缺失时降级恢复。

## Schema

### `kb_meta`

键值元信息表。

- `schema_version=2`
- `migrated_from=v1`
- `migrated_at=<RFC3339>`

### `kb_sources`

来源表，字段基本继承旧 `knowledge_sources`。V2 中所有表格、卡片、事实都从 source 归属。

### `kb_tables`

一张 sheet 一条记录。

- `id`
- `source_id`
- `sheet_name`
- `table_title`
- `header_row_index`
- `row_count`
- `column_count`
- `schema_json`
- `created_at`
- `updated_at`

### `kb_columns`

sheet 的列定义。

- `id`
- `table_id`
- `column_index`
- `column_name`
- `normalized_name`
- `value_type`
- `aliases_json`
- `stats_json`
- `created_at`
- `updated_at`

### `kb_rows`

结构化主召回单元。

- `id`
- `table_id`
- `source_id`
- `row_index`
- `primary_key_text`
- `row_text`
- `row_json`
- `embedding`
- `created_at`
- `updated_at`

配套 FTS：`kb_rows_fts(row_id, primary_key_text, row_text)`。

### `kb_cells`

精确筛选主表。

- `id`
- `row_id`
- `table_id`
- `column_id`
- `column_name`
- `normalized_column_name`
- `raw_value`
- `normalized_value`
- `value_type`
- `number_value`
- `date_value`
- `bool_value`
- `created_at`
- `updated_at`

### `kb_cards`

自然语言召回层。相比旧 card，新增：

- `row_id`
- `origin_type`

用于区分 `document` 与 `table_row`。

### `kb_facts`

事实三元组层。相比旧 fact，新增：

- `row_id`
- `normalized_object`
- `value_type`
- `number_value`
- `date_value`
- `bool_value`
- `created_at`

## 导入流程

Spreadsheet 走独立 V2 importer：

1. 读取 workbook/sheet。
2. 检测表头行。
3. 规范化列名并去重。
4. 推断列类型。
5. 写入 `kb_tables`。
6. 写入 `kb_columns`。
7. 每行写入 `kb_rows` 和 `kb_rows_fts`。
8. 每个非空单元格写入 `kb_cells`。
9. 按行生成 card/fact。
10. 异步回填 row/card embedding。

行文本格式固定为字段值对：

```text
姓名: 张三 | 部门: 法务 | 入职日期: 2024-01-05
```

## 检索流程

保留统一 `Search`，并新增结构化入口 `SearchStructured`。

自然语言搜索融合：

- `kb_rows_fts`
- `kb_cards_fts`
- `kb_facts_fts`
- row embedding
- card embedding

结构化搜索优先：

- `ColumnEquals`
- `ColumnContains`
- `NumberRanges`
- `DateRanges`

性能约束：

- `kb_cells` 需要按真实查询路径建立复合索引：`normalized_column_name + normalized_value/number_value/date_value/bool_value + row_id`。
- 列过滤查询优先从命中的 `kb_cells.row_id` 生成候选行，再回表到 `kb_rows/kb_tables/kb_sources`，避免大表按行全量扫描。
- 新程序启动时即使检测到已经是 V2，也会再次执行 `CREATE INDEX IF NOT EXISTS`，用于给早期 V2 库自动补齐性能索引。

融合排序原则：

- 精确列过滤结果优先。
- FTS 命中其次。
- embedding 用于补召回。
- `ContextPack` 对 `table_row` 结果返回短证据，不返回整张 sheet。

## 服务/API 入口

V2 结构化检索需要从底层库一路暴露到产品调用层：

- Go core：`SQLiteStore.SearchStructured(ctx, StructuredSearchOptions)`。
- 授权包装：`multiKnowledgeStore.SearchStructured` 复用现有租户、用户、公共知识库 scope 合并逻辑。
- HTTP：`POST /api/v1/knowledge/search/structured`，请求体为 `StructuredSearchOptions`，响应与普通搜索一致：`{ "results": [...], "total": n }`。
- HTTP：`POST /api/v1/knowledge/structured/catalog`，请求体为 `StructuredCatalogOptions`，返回可读范围内的表、sheet、列名与行列统计，用于前端筛选建议。
- GUI/Wails：`KnowledgeSearchStructured(opts)`，前端可直接传 `column_equals`、`column_contains`、`number_ranges`、`date_ranges`。

普通 `POST /api/v1/knowledge/search` 和 `KnowledgeSearch` 保持兼容；结构化入口允许没有 `query`，仅通过列条件检索。

## 自动迁移

启动时执行：

1. 检查 `kb_meta.schema_version`。
2. 没有 `kb_meta` 且存在 `knowledge_sources` 时，判定为 V1。
3. 创建 V2 表。
4. 迁移 source/card/fact 元数据。
5. Spreadsheet source 优先从原文件重读，重建 table/row/cell。
6. 原文件缺失时，从旧 `document_nodes` 降级恢复 row。
7. 重建 V2 FTS。
8. 写入 `schema_version=2`。

第一阶段迁移器会先实现 schema 和元数据桥接；后续 importer 完成后补齐 spreadsheet 重建。

## 实施阶段

1. V2 schema 与迁移骨架。
2. Spreadsheet importer V2。
3. Structured search 与 row FTS。
4. Row card/fact。
5. Search/ContextPack 融合。
6. 自动迁移补齐原文件重建与降级恢复。
7. embedding 回填与性能调优。

## 验收

- 新库启动后存在 `kb_meta.schema_version=2`。
- 新库具备 `kb_sources/kb_tables/kb_columns/kb_rows/kb_cells/kb_cards/kb_facts`。
- 旧 V1 库启动后能创建 V2 表并复制 source/card/fact 基础数据。
- Excel/CSV V2 importer 完成后，`姓名: 张三` 这类问题能命中单行证据。
- 列级过滤能支持等值、包含、数字范围、日期范围。
