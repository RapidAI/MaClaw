# Agent 数据库连接工具设计与实现方案

状态：reviewed proposal / implementation baseline  
日期：2026-09-01  
范围：MaClaw Agent 工具、连接管理、数据库/文件数据源适配、权限审批、审计和动态 UI

本次审查重点：凭据不能经过模型、Excel 写入能力必须与现有实现一致、dry-run 不能承诺“绝对无副作用”、以及不同数据源的事务/分页能力必须显式声明。

## 1. 背景与目标

Agent 当前可以通过 `office` 读取/写入 Excel，但无法用统一、受控的方式连接业务数据库。用户需要在对话中完成“查询订单”“统计 PostgreSQL 数据”“更新 SQL Server 状态”“读取 Access 文件”“把结果写回 Excel”等任务，而不应被迫手写临时脚本或把数据库密码暴露给模型。

本方案新增一个统一的 `database` 工具，在同一套参数、权限、结果和审计模型下支持：

| 数据源 | 首期能力 | 备注 |
| --- | --- | --- |
| MySQL/MariaDB | 连接、元数据、参数化查询、写入、事务批处理 | TCP/TLS；默认单语句 |
| PostgreSQL | 连接、元数据、参数化查询、写入、事务批处理 | 支持 schema、数组/JSON 类型摘要 |
| SQL Server | 连接、元数据、参数化查询、写入、事务批处理 | 支持 SQL Server TLS 和实例名 |
| Excel（`.xlsx`/`.xls`/`.csv`） | 工作表发现、读取、筛选、`.xlsx` 范围写入、导出 | 现有 writer 只保证 `.xlsx`；`.xls/.csv` 首期只读，导出时转换为新 `.xlsx` |
| Access（`.mdb`/`.accdb`） | 表/列发现、参数化查询；写入需显式开启 | Windows 优先；启动时报告 ODBC 驱动缺失；不承诺跨平台写入 |

目标：

1. Agent 能先发现连接和 schema，再生成可解释的查询计划。
2. 所有 SQL 使用参数绑定；结果有行数、字节数和超时上限，避免把大表灌入上下文。
3. 默认只读；写入、DDL、删除和批量覆盖必须经过策略检查和用户确认。
4. 密码只保存在系统密钥环或外部 secret provider，模型、日志和审计中永不出现明文。
5. 与现有 `ToolRegistry`、`CoreToolRegistry`、AgentView、语义能力和审计链路复用，不建立第二套 Agent loop。
6. 在桌面单用户和 MaClawSrv 多租户部署中保持相同的工具语义；profile、secret、连接和审计都按 `tenantID → ownerID → sessionID` 隔离。

非目标：

- 首期不做完整 BI/报表设计器，不替代专业数据库客户端。
- 不允许 Agent 通过 `bash`、动态 Go/Python 或拼接 ODBC 命令绕过 `database` 工具的策略。
- 不在服务器端永久保存用户明文凭据，不自动执行迁移、备份恢复或任意存储过程。
- 不通过 Agent 侧 SQL 重写“猜测”租户过滤条件；跨租户隔离必须由独立数据库账号、RLS 或预先授权的视图保证。

## 2. 用户体验与典型流程

```text
用户：统计本月每个客户的订单金额，并导出 Excel
          |
          v
Agent 调 database(action=list_connections)
          |
          v
Agent 调 database(action=inspect/query, profile_id=crm, connection_id=..., params=...)
          |
          +--> 结果过大：返回 handle + next_cursor，Agent 分页读取
          |
          v
Agent 调 database(action=export_excel, result_handle=..., file_path=...)
          |
          v
动态 UI 展示查询摘要、数据来源、行数和导出路径
```

当用户只说“查一下订单”而没有指定连接时，工具返回连接候选和缺失字段，触发现有右侧参数表单；不得猜测生产连接或默认使用上一次连接。

## 3. 总体架构

```text
Agent Loop (GUI / TUI / AgentService)
        |
        | database tool call
        v
DatabaseToolHandler
  - 参数/schema 校验
  - owner/session 绑定
  - SecurityGuard + PolicyEngine
  - 结果裁剪、脱敏、receipt
        v
ConnectionManager
  - profile registry
  - credential resolver (OS keyring)
  - pool/idle TTL/cancel
        v
DataSourceAdapter interface
  |-- SQLAdapter: mysql / postgres / sqlserver / access-odbc
  |-- ExcelAdapter: xlsx/xls/csv reader + xlsx writer
  `-- QueryFacade: schema、分页、类型归一化、错误分类
        v
Audit / Metrics / AgentView result_browser
```

核心原则：

- `database` 是唯一对 Agent 暴露的入口；驱动只存在于后端 adapter 层。
- 工具定义必须保持单一真源：优先在 `corelib/agent/tool_register_core.go` 注册 schema/handler contract，再由 GUI `RegisteredTool` 做 host adapter；不能在 GUI、TUI 各维护一套字段列表。GUI 的 adapter 必须使用 `HandlerCtx`，把宿主 request context（审批上下文和 `RequestScope`）传给 `corelib/database`，不得退回无 context 的包装。
- 连接对象属于 `ownerID + sessionID`，不能被另一个用户或任务猜 ID 复用。两者优先从宿主注入的 `database.RequestScope` 读取；模型参数中的 `_owner_id`/`_session_id` 不能覆盖可信 scope。
- SQL 数据源使用 `database/sql`；连接池只在内存中保存，空闲超时后关闭。
- Excel 查询在隔离的临时 SQLite 数据集上执行只读 SQL，写入仍通过表格 writer 回写，避免引入任意脚本执行。
- Access 通过 ODBC DSN/连接字符串适配；能力检测失败时返回可操作的安装提示，而不是静默降级。

## 4. 工具契约

工具名：`database`  
分类：`data`  
风险：查询为 `safe/read_only`，写入为 `elevated`，DDL/删除/覆盖为 `dangerous`。

### 4.1 统一参数

```json
{
  "action": "list_connections|connect|disconnect|inspect|query|execute|batch_execute|read_table|write_table|export_excel|job_status|explain|list_favorites|save_favorite|delete_favorite",
  "profile_id": "crm-prod",
  "connection_id": "session-scoped-id",
  "sql": "select customer_id, sum(amount) total from orders where created_at >= :start_date group by customer_id",
  "params": {"start_date": "2026-09-01"},
  "table": "orders",
  "file_path": "reports/orders.xlsx",
  "sheet": "Sheet1",
  "range": "A1:C100",
  "rows": [["customer_id", "total"]],
  "limit": 200,
  "max_affected_rows": 100,
  "expected_affected_rows": 12,
  "cursor": "...",
  "timeout_seconds": 30,
  "dry_run": true
}
```

规则：

- `action` 必填；每个 action 通过 JSON Schema 声明自己的 required/dependent fields。
- Agent 只能传 `profile_id`；密码、完整 DSN、TLS 私钥和一次性 secret 不进入 tool schema。连接表单在 UI 中安全提交，后端返回短期 `connection_id`。
- `query` 只接受单条 `SELECT`/`WITH`/元数据查询；禁止多语句、注释拼接和未绑定的用户值。
- `WITH` 必须递归检查 CTE body，拒绝 PostgreSQL/SQL Server 的 data-modifying CTE（如 `WITH x AS (UPDATE ... RETURNING ...)`）；`SELECT INTO`、`COPY ... TO`、`OPENROWSET`、`EXEC/CALL`、外部表和 pass-through query 均按写入/外带数据处理。
- 跨数据库的规范占位符为 `:name`，`params` 使用 JSON object；adapter 将其转换为目标驱动参数。为避免与 PostgreSQL JSON `?` 运算符、时间字面量和方言语法冲突，规范 SQL 不使用裸 `?` 占位符；仅在明确声明 `dialect` 的兼容模式下接受 positional 参数。
- `execute`/`batch_execute` 必须先通过策略预检和 dry-run，再经过显式审批；生产 profile 默认不能执行 DDL。审批通过后由 GUI、srv 或 TUI 宿主把一次性审批上下文注入 request context，模型参数不得携带 `approval_token`。
- DML 必须包含可审计的 `WHERE`（除非 profile 明确授予全表操作）；调用方可设置 `max_affected_rows`，实际影响行数超过上限时自动回滚。`expected_affected_rows` 不作为安全边界，只用于审批时的偏差告警。
- `file_path` 必须经过现有 owner 工作目录边界检查；禁止 `..`、UNC 越界和任意绝对路径（管理员策略可放宽）。
- `owner_id/session_id` 是宿主认证的 request scope，不属于模型参数。若请求未注入可信 scope 却携带下划线身份字段，handler 必须拒绝；已注入 scope 时始终覆盖同名模型字段。
- `limit` 默认 100，最大 5,000；响应默认 1.5 MB，行数截断时返回受控内存 `next_cursor`，字节截断时明确返回 `pagination_unavailable`。

### 4.2 Action 语义

| Action | 说明 | 默认风险 |
| --- | --- | --- |
| `list_connections` | 列出当前 owner 可见的 profile（只返回名称、类型、状态） | safe |
| `connect` | 按已配置 profile 建立 session 连接并执行 ping；凭据只从后端 secret_ref 解析 | safe（新主机需策略批准） |
| `disconnect` | 关闭当前 session 的连接 | safe |
| `inspect` | 列 schema、表、列、索引和估算行数；支持 `table` 过滤 | safe |
| `query` | 参数化只读 SQL，支持分页、超时和列脱敏 | safe |
| `execute` | 单条 INSERT/UPDATE/DELETE/DDL；先 dry-run，再确认提交；受 `max_affected_rows` 约束 | dangerous |
| `batch_execute` | 在一个事务中执行有序参数组，失败自动回滚 | dangerous |
| `read_table` | Excel/Access 便捷读取，避免 Agent 书写方言 SQL | safe |
| `write_table` | 仅保证 `.xlsx` 范围/表格写入；`.xls/.csv` 返回 read-only 能力提示；覆盖前必须显示 diff/影响行数 | elevated |
| `export_excel` | 将查询结果写入新文件，不覆盖源文件，除非用户确认；可提交脱敏 `rows` 或加密 `result_handle` | elevated |
| `job_status` | 轮询异步 `query` 作业；按 owner/session 隔离 | safe |
| `explain` | 对只读 SQL 做方言 EXPLAIN 预览，或仅用 `prompt` 返回 schema 约束（不执行用户 SQL） | safe |
| `list_favorites` | 列出当前 owner 的只读查询收藏 | safe |
| `save_favorite` | 保存一条参数化只读 SQL 收藏 | safe |
| `delete_favorite` | 删除当前 owner 的一条收藏 | safe |

Action 输入约束（实现时直接生成 JSON Schema）：

| Action | 必填字段 | 互斥/附加约束 |
| --- | --- | --- |
| `list_connections` | 无 | 只按当前 owner 返回摘要 |
| `connect` | `profile_id` | 不接受 password/DSN；profile 必须 active |
| `disconnect` | `connection_id` | 只能关闭当前 session 的连接 |
| `inspect` | `connection_id` 或 `profile_id` | 二者都无则返回缺失字段；未连接时可短暂 connect + inspect |
| `query` | `connection_id`（或 `profile_id`）, `sql` 或 `favorite_id` | `params` 的 key 必须与 `:name` 占位符完全匹配；只读语句；`async=true` 立即返回 `job_id` |
| `execute` | `connection_id`, `sql`, `dry_run` | `dry_run=false` 仅能在宿主注入的有效一次性审批上下文下提交；模型参数中的 `approval_token` 直接拒绝 |
| `batch_execute` | `connection_id`, `statements`, `dry_run` | `statements` 非空且受最大条数/字节数限制 |
| `read_table` | `connection_id`, `table` 或 `file_path` | Excel 只能读取允许的 sheet/range |
| `write_table` | `file_path`, `sheet`, `rows`, `range`（更新已有文件时必填） | 仅 `.xlsx`；范围写入保留范围外内容；可附 `connection_id` 绑定 Excel profile 并执行其 operation allowlist；覆盖需宿主注入的审批上下文 |
| `export_excel` | `file_path`，以及 `rows` 或 `result_handle` | 句柄由后端按 owner/session/connection 解析，模型提交的 `rows` 不能覆盖句柄内容；可附 `connection_id` 进行来源 profile 策略校验；confidential/restricted 导出禁止随后自动上传 IM/公网 |
| `job_status` | `job_id` | 只能读取同一 owner/session 的作业 |
| `explain` | `connection_id`（或 `profile_id`），以及 `sql`/`favorite_id` 或 `prompt` | 拒绝 `EXPLAIN ANALYZE`；Access 无 EXPLAIN |
| `list_favorites` | 无 | 只返回当前 owner 的收藏 |
| `save_favorite` | `favorite_name`, `sql` | 只接受只读参数化 SQL；可选 `profile_id` |
| `delete_favorite` | `favorite_id` | 只能删除当前 owner 的收藏 |

### 4.3 结果格式

```json
{
  "ok": true,
  "profile_id": "crm-prod",
  "connection_id": "session-scoped-id",
  "columns": [{"name": "customer_id", "type": "varchar"}, {"name": "total", "type": "decimal"}],
  "rows": [["C001", 1280.50]],
  "row_count": 1,
  "truncated": false,
  "next_cursor": null,
  "result_handle": null,
  "elapsed_ms": 42,
  "warnings": [],
  "mutation": null
}
```

所有成功结果都带 `contract_version`（当前值为 `1`）。写入提交必须由可信宿主在 request context 中注入服务端签发的短期、一次性审批上下文（包含不敏感的 `approval_id` 和仅供后端校验的 opaque token）；模型参数、工具 schema、结果、日志和审计事件均不得携带 token。客户端不得自行生成、持久化或长期缓存该上下文。审批上下文缺失、过期或与预览不匹配时必须 fail-closed。

写操作额外返回 `matched_rows`、`affected_rows`、`dry_run`、`dry_run_guarantee`、`commit_id` 和可回滚提示。`dry_run_guarantee` 取 `policy_only`、`rolled_back_transaction` 或 `unsupported`；不得把事务回滚表述成跨触发器/外部调用的绝对无副作用。截断结果同时返回 `next_cursor` 与 `result_handle`（同一 token），用于再次调用 `database(action=query, cursor=...)` 分页，或 `database(action=export_excel, result_handle=...)` 导出；它绑定 owner/session/profile/connection。分页消费后失效；导出是只读快照，不推进游标。字节上限导致的截断会返回 `pagination_unavailable`，不会伪造可恢复游标。数据库工具的超大输出经 toolresult 落盘时强制加密（AES-256-GCM store key），可由 `read_tool_result` 分页读回。`result_handle` 另以 AES-256-GCM 写入 `database_results/{id}.enc`，进程重启后在 TTL 内仍可分页或导出。错误统一分类为 `connection`, `authentication`, `timeout`, `permission`, `syntax`, `constraint`, `driver_missing`, `path_denied`, `result_too_large`, `quota_exceeded`, `unsupported_capability`, `cancelled`，供 Agent 决定重试或向用户提问。

## 5. 连接 Profile 与凭据

Profile 建议存储在 AppConfig 的 `database_profiles` 节点；该节点已标记为 user-web 的复杂字段，必须由安全的结构化编辑器处理；密码字段只保存 `secret_ref`：

```json
{
  "id": "crm-prod",
  "name": "CRM 生产库",
  "schema_version": 1,
  "type": "postgres",
  "host": "db.example.com",
  "port": 5432,
  "database": "crm",
  "username": "agent_readonly",
  "secret_ref": "keyring://maclaw/database/crm-prod",
  "tls": {"mode": "verify-full", "ca_file": "..."},
  "default_schema": "public",
  "read_only": true,
  "write_enabled": false,
  "allow_ddl": false,
  "allowed_operations": ["inspect", "query"],
  "allowed_schemas": ["public"],
  "allowed_tables": ["orders", "customers"],
  "denied_columns": ["password_hash", "access_token"],
  "masked_columns": ["phone", "email"],
  "max_affected_rows": 0
}
```

`schema_version` 用于审批前后检测配置变更；`allowed_schemas/tables` 是 deny-by-default 的资源边界，不能只依赖 prompt 约束。`max_affected_rows=0` 表示只读 profile。Excel/Access profile 使用 `file_path`、`sheet`、`dsn` 等字段；路径仍需在 owner 工作区内。加密的 Access/Excel 文件需要在 secure form 输入文件密钥并绑定到 profile，解析失败返回 `authentication`/`unsupported_capability`，不得尝试破解或把密码写入参数。profile 管理 UI 要支持新增、测试连接、复制（不复制 secret）、停用、删除和轮换凭据。连接测试只执行 ping/元数据探测，不执行用户 SQL。禁止通过 Agent 文本参数创建包含密码的临时 profile；一次性连接必须走 UI secure form，凭据只在内存中存活到 session 结束。

Profile CRUD 成功后，宿主必须调用 `Manager.UpdateProfiles` 原子刷新运行时配置；被删除、停用、轮换或字段变更的 profile 会关闭旧连接并使其分页游标失效。配置读取失败不得以空列表覆盖当前 profile，避免瞬时 I/O 故障造成误拒绝或状态抖动。

桌面端 profile 可加密保存在本地 AppConfig，secret 放 OS keyring；MaClawSrv profile 放租户数据库，secret 通过部署环境的 KMS/secret provider 引用。导出、备份和日志均剥离 secret；轮换 secret 时旧连接立即标记 `credential_stale` 并在下次请求前重建。

## 6. 驱动与适配器实现

建议依赖与隔离：

| Adapter | Go driver/组件 | 平台与注意事项 |
| --- | --- | --- |
| MySQL | `github.com/go-sql-driver/mysql` | DSN 必须开启参数解析；TLS 配置白名单化 |
| PostgreSQL | `github.com/jackc/pgx/v5/stdlib` | 使用 `database/sql` 兼容层；禁用多语句扩展 |
| SQL Server | `github.com/microsoft/go-mssqldb` | 支持 `server\instance`、加密和证书校验 |
| Access | `github.com/alexbrainman/odbc`（仅 Windows build tag） | Windows 首期；检查 Access Database Engine/ODBC DSN；非 Windows/CGO-free 构建不链接 ODBC，能力返回 `driver_missing`；32/64 位必须与进程一致 |
| Excel | 现有 `OfficeRead`、`GoExcel`、`xlsReader` | 复用 `corelib/agent` Office 解析和 owner 路径解析 |

统一接口（建议放在 `corelib/database`）：

```go
type Adapter interface {
    Ping(ctx context.Context) error
    Inspect(ctx context.Context, req InspectRequest) (SchemaInfo, error)
    Query(ctx context.Context, req QueryRequest) (QueryResult, error)
    Execute(ctx context.Context, req ExecuteRequest) (MutationResult, error)
    Close() error
}

type Capabilities struct {
    Read, Write, Transactions, Cursors, Explain bool
    MaxPageSize int
}
```

`ConnectionManager` 负责按 profile 创建 adapter、注入 context deadline、限制并发（每 profile 默认 4 个 in-flight）、处理 idle TTL（默认 10 分钟）和优雅关闭。连接池 checkout 时必须重置 `search_path`/`SET`/临时表等 session 状态，禁止把一个 owner 的 session 状态带给另一个 owner；工具不暴露 `SET`, `USE`, `ATTACH` 等 session 改写语句。驱动错误必须转换为上述稳定 error class，原始错误只进入受控 debug 日志并脱敏。每个 adapter 在 `connect`/`inspect` 时返回 `Capabilities`，工具据此拒绝不支持的写入、事务或 cursor，而不是运行到一半才失败。

能力基线（当前实现）：MySQL/PostgreSQL/SQL Server/Access 的 `query` 结果由 Manager 物化到受控内存并提供短期 cursor；Excel 的 SQL facade 也支持同样的 cursor，但 `read_table` 便捷 action 仍只返回有界表格结果。Excel 不支持数据库事务，`batch_execute` 对不支持事务的数据源直接返回 `unsupported_capability`。达到行数上限时返回可继续消费的 `next_cursor`；达到 1.5 MB 字节上限或 100,000 行扫描上限时必须带 warning，前者返回 `pagination_unavailable`。加密持久化 `result_handle` 已启用：物化结果写入 `database_results/{id}.enc`，重启后在 TTL 内可继续分页。

资源配额按 profile 可配置且有全局上限：并发 4、单查询 30 秒、扫描行数 100,000、返回 5,000 行/1.5 MB、临时磁盘 256 MB、单批 100 条语句。超限统一返回 `quota_exceeded`，并在 cancel/timeout 路径关闭 rows、连接和临时文件；不得依赖 GC 回收数据库资源。

驱动依赖采用可选构建/初始化路径：普通安装不因 Access ODBC 缺失而启动失败；Windows 安装器提供 ODBC 检查项，Linux/macOS 仅在检测到 unixODBC + Access 兼容驱动时注册 Access 能力。Access 文件默认以只读共享模式打开，写入前检查文件锁、备份副本和剩余磁盘空间；不支持在 Agent 工具中执行数据库压缩/修复。依赖版本、许可证和 CVE 扫描纳入 release checklist。

### SQL 占位符与方言

工具契约统一采用命名占位符 `:name` + JSON object 参数；adapter 在执行前转换为目标驱动格式（PostgreSQL 的 `$1..$n`、SQL Server 的 `@p1..@pn`、MySQL/ODBC 的驱动参数），并通过驱动原生参数绑定执行。转换器必须跳过字符串、标识符、注释和 PostgreSQL `::type` cast；参数缺失、重复定义或转换失败均拒绝执行。模型不得直接生成带用户值的字符串拼接 SQL。`inspect` 返回 `dialect` 字段，便于 Agent 在需要方言特性（如 PostgreSQL `ILIKE`、SQL Server `TOP`）时先确认目标。

为兼容现有调用，MVP 可在 MySQL/SQLite facade 接受 positional `?`，但必须显式传 `parameter_mode=positional` 并在结果 warning 中标记；PostgreSQL JSON `?` 运算符和 Access 方言不走该兼容路径。

SQL 安全检查采用“轻量 tokenizer + 驱动只读事务/连接属性”双重门禁：tokenizer 负责拒绝多语句、注释、危险关键字和无界 DML；它不是完整 SQL 解析器，遇到无法识别的语法必须 fail-closed，并提示用户改用受支持语法。

`sqlguard.go` 输出结构化 `StatementClass`（read、dml、ddl、session、external、unknown）和 `ResourceRefs`（schema/table/column），供 PolicyEngine 判断；它不直接改写 SQL。实现必须带 dialect/version fixture，覆盖引号标识符、嵌套括号、CTE、注释、Unicode 空白、SQL Server 方括号和 Access 方言，防止仅靠正则造成绕过。

`dry_run` 语义：先执行 tokenizer、profile allowlist、权限和参数校验；对支持事务的数据源可在隔离事务中执行并回滚，结果标记 `rolled_back_transaction`；对不支持事务或检测到触发器/外部副作用的语句只做 `policy_only` 预检，不模拟执行。真正提交必须重新校验 schema/version 和宿主注入的审批上下文，防止预览后数据已变化；opaque token 只在后端 adapter 调用链中短暂存在，审计只记录 `approval_id`。

DDL 不使用“执行后回滚”作为预览：MySQL 等数据库可能对 DDL 隐式提交，故 DDL 的 `dry_run` 固定为 `policy_only`，首期默认拒绝；只有后续引入经过验证的 `EXPLAIN`/迁移计划器后，才允许单独的 schema-change workflow。`batch_execute` 禁止混入 DDL、事务控制语句或 session 设置语句。

审批暂停期间，后端保存脱敏后的 pending mutation（SQL fingerprint、参数摘要、profile/schema 版本、过期时间），不把完整 secret 或任意可执行 SQL 放进前端状态。用户批准后由同一 owner/session 恢复执行；拒绝、取消、超时或配置变更都会销毁 pending mutation。提交时除 schema 版本外，还应在支持的数据库上使用 `SERIALIZABLE`/锁定策略或重新验证目标行版本；若实际影响行数超出上限或预览偏差超过策略阈值，自动回滚并要求重新确认。`batch_execute` 要么整体提交，要么整体回滚，禁止“部分成功”返回为成功。

### 类型、NULL 与二进制数据

结果层统一编码为 JSON-safe 值：SQL `NULL` 映射为 `null`，时间保留 RFC3339（同时返回原始类型），decimal 默认以字符串返回以避免浮点精度损失，超长文本和 binary/blob 只返回长度、摘要及受控下载 handle。Excel 的空单元格、日期序列值和公式结果必须在 `warnings` 中声明推断规则；Agent 不得把二进制列直接写入 prompt。

### Excel 查询策略

1. `inspect` 读取工作表、列名和推断类型。
2. 对 OOXML/BIFF 容器先复用现有 Office reader 的大小、压缩比、加密和格式预检；禁用宏、外部链接和公式重算。`query` 再将选定工作表流式导入临时 SQLite 表，列名进行安全引用，执行只读 SQL；SQLite 仅是查询引擎，不代表 Excel 具备事务能力。
3. 结果按统一格式返回；临时库在请求结束后删除。
4. `write_table` 只能写入 `.xlsx` 的明确 sheet/range；`.xls/.csv` 必须导出为新 `.xlsx`，先生成变更摘要，用户确认后调用现有 writer。写入前保存源文件哈希，提交时若哈希变化则拒绝并要求重新预览。对已存在的工作簿，`range` 为强制字段且必须是闭合的 A1 矩形；输入 `rows` 的行列数不得超出矩形，写入只覆盖矩形内单元格并保留其它 sheet/单元格。省略 `range` 仅允许创建新工作簿，避免历史上的整本覆盖导致数据丢失。
5. 导出到 Excel 前对以 `=`, `+`, `-`, `@` 开头的外部值执行公式注入防护（按策略转义为文本），并在结果 warning 中标记被转义单元格数量。

导入 SQLite 时对重复/空表头生成稳定的 `col_001` 等安全列名，并返回原始表头映射；禁止把工作表名或单元格内容直接拼入 SQL。单个工作簿的 sheet 数、行数、列数和解压后大小均受配额约束。

这样既支持“筛选/聚合 Excel”，又不会把 Excel 当成可执行脚本或任意 SQL 文件。

### 分页、游标与一致性

- 当结果被行数上限截断时，首页返回短期 `next_cursor`（TTL 5 分钟）；token 绑定 `ownerID/sessionID/profileID`，不可跨会话复用。未截断的结果不生成句柄。
- 当前实现统一将有界结果物化到 Manager 内存；后续可按同一契约替换为驱动 cursor 或加密临时存储，禁止自动拼接无界 `OFFSET`。
- 未包含 `ORDER BY` 的分页查询返回 warning `unstable_order`；需要跨页一致性时，Agent 应显式提供稳定排序键。
- 连接断开、schema 版本变化或 token 过期时，后续分页必须失败并要求重新查询，不能悄悄从第一页重跑。
- 取消请求要同时取消 driver context、关闭 result rows，并释放临时文件/SQLite 数据库。

## 7. 权限、安全与审计

### 7.1 策略门禁

- profile 级 allowlist：主机、数据库、schema、表和操作类型。
- 网络出口也受 profile allowlist 约束；解析 DNS 后再次校验 IP，避免通过 DNS rebinding/重定向绕过主机策略。默认拒绝未知公网地址，内网地址需管理员显式允许。
- 生产数据访问必须使用最小权限数据库账号；租户/部门隔离优先依赖数据库 RLS、授权视图或独立 schema。工具只做资源 allowlist，不对模型生成 SQL 做不可靠的租户条件注入。
- 默认 `read_only=true`；`execute`、`batch_execute`、`write_table`、`export_excel` 走现有 SecurityGuard/PolicyEngine。若文件操作携带 `connection_id`，handler 还必须按该 Excel profile 的 `allowed_operations`、`write_enabled` 和 `read_only` 再校验；不携带连接的导出只能使用宿主工作区/策略边界，不能借此绕过已授权 profile 的拒绝规则。
- 识别 `DROP`、`TRUNCATE`、无 WHERE 的 UPDATE/DELETE、批量覆盖和存储过程调用为 dangerous，强制二次确认。
- 查询超时默认 30 秒；单次返回行/字节上限；检测到笛卡尔积或全表扫描时只告警，不替 Agent 擅自改写语义。
- 禁止通过 SQL 访问系统表以外的秘密表；列级脱敏规则支持 password/token/身份证/手机号/邮箱等模式。
- 允许配置列级 denylist（直接拒绝读取）和 mask 规则（仅返回部分值）；脱敏发生在 adapter 扫描后、结果持久化前，原值不得进入 result handle。

### 7.2 凭据与日志

- secret 通过 `go-keyring` 或部署环境 secret provider 解析；绝不放入 tool definition、prompt、result、任务文档或 crash dump。
- 审计记录 `owner_id/session_id/profile_id/action/sql_fingerprint/affected_rows/risk/approval_id/result_class/timestamp`；只存 SQL 指纹和参数摘要，不存完整敏感值或 approval token。成功提交（`dry_run=false`）的 `execute`、`batch_execute`、`write_table`、`export_excel` 事件必须带非敏感 `approval_id`；dry-run 预览可不带 approval_id，拒绝/失败事件不得回显 token。
- 每次写入产生不可变 mutation receipt，关联用户确认和 commit_id；失败事务也记录回滚原因。
- 审批上下文中的 token 必须一次性、短 TTL，并由 PolicyEngine 绑定 SQL 指纹、参数摘要、profile、schema 版本和预览影响范围；任何字段变化都必须重新 dry-run。批处理指纹按规范化后的 SQL 顺序和 `NUL` 分隔计算，避免审批校验与审计使用不同 canonical form。token 只能由可信宿主通过 context 注入，不能从模型 JSON、HTTP query 或普通表单字段读取。
- 数据库结果落盘已实现加密：toolresult spill 对 `database` 工具输出强制 AES-256-GCM（store key 0600 存于 store root，`.enc` 后缀，session 隔离，`read_tool_result` 透明解密分页，prune/stats 覆盖）；`export_excel` 接受显式脱敏 `rows` 或加密 `result_handle`（后端按 owner/session/connection 解析，模型提交的 `rows` 不能覆盖句柄内容）。跨进程 `result_handle` 使用 owner 目录权限、5 分钟 TTL 和 AES-256-GCM 存储，过期或断开后删除，审计只保留句柄摘要。`export_excel` 视为跨边界数据导出，必须检查数据分类、目标路径和行数配额，confidential/restricted 导出禁止随后自动上传到 IM/公网。

## 8. Agent 与动态 UI 集成

### Agent 行为约定

系统提示补充以下规则：

1. 先 `list_connections`/`inspect`，再写 SQL；连接不明确时询问用户。
2. 查询优先使用 `query` 的参数绑定和小 `limit`，不得选择 `select *` 读取大表。
3. 任何写入先 `dry_run=true`，向用户说明影响行数、筛选条件和目标 profile，再提交。
4. 结果被行数截断时使用 `cursor` 继续读取；若 warning 为 `pagination_unavailable`，应缩小查询或重新设计筛选条件，不要重复执行无界查询。持久化 `read_tool_result` 句柄启用后再补充对应流程。

### AgentView

复用现有 `form`、`approval`、`progress`、`result_browser`：

- `connect`：连接配置表单 + 测试连接按钮。
- `query`：SQL/参数类型预览（敏感值掩码）、数据源徽标、执行进度、取消按钮；UI 不回显 secret 或完整身份证/令牌。
- `execute`：dry-run 影响摘要、风险标签、批准/拒绝。
- 结果：分页表格、列脱敏提示、导出 Excel 操作和审计编号。

UI 只渲染 schema 白名单组件，不执行模型生成的 HTML/JS。

## 9. 代码落点与实现步骤

### Phase 0：契约与依赖（1 个迭代）

- 新建 `corelib/database`：`types.go`、`adapter.go`、`manager.go`、`policy.go`、`errors.go`、`sqlguard.go`、`excel_facade.go`。
- 固化工具 JSON Schema、结果 envelope、error class 和 profile 迁移版本。
- 为 AppConfig/MaClawSrv 增加 profile migration v1（无 profile → 空列表；旧明文密码仅在成功写入 secret provider 并完成校验后清零），迁移失败时 fail-closed，不启动数据库工具且保留可审计的迁移错误。
- 增加四类 SQL driver 与 ODBC 的按需初始化，避免未使用驱动影响启动时间；驱动导入使用 build tags/独立 adapter 文件，普通 GUI/TUI 构建不因 Access ODBC 缺失失败。

### Phase 1：只读 MVP（1～2 个迭代）

- 实现 MySQL/PostgreSQL/SQL Server `Ping/Inspect/Query`。
- 实现 Access ODBC 能力探测和 `Inspect/Query`。
- 实现 Excel `inspect/read_table` 和临时 SQLite 查询 facade。
- 在 `corelib/agent/tool_register_core.go` 增加统一 `database` ToolEntry/Schema；GUI 的 `gui/tool_registry_builtin.go` 仅绑定 host handler（`gui/tool_database.go`），TUI/AgentService 通过 `CoreToolDeps.DatabaseHandler` 或 `ExtraHandlers["database"]` 接线，禁止复制 schema。
- 同步更新 `expert_tool_meta.go`、coding/light tool allowlist、semantic capability catalog 和 workflow filter，明确 `query` 与 `execute` 是不同风险投影。
- 接入结果裁剪、短期 `next_cursor`、owner 工作目录和连接 session 生命周期；持久化 `read_tool_result` 句柄另列后续阶段。

### Phase 2：写入与审批（1 个迭代）

- 实现 `execute/batch_execute/write_table/export_excel`、dry-run 和事务回滚。
- 给工具补充 `CapabilityProvision`、`EffectClass`、风险等级和 workflow 过滤规则。
- 接入 mutation receipt、审计事件和高风险审批 AgentView。

### Phase 3：配置与可观测性（1 个迭代）

- AppConfig/profile CRUD、密钥环绑定、导入导出（导出不含 secret）；`corelib/doctor` 提供 profile 结构、文件可读性及可选 ODBC 编译能力检查。
- GUI 数据源管理面板、连接测试、指标（连接成功率、查询耗时、截断率、拒绝率）。
- 文档、升级迁移、doctor 检查（驱动缺失、ODBC 配置、TLS 证书）。

### Phase 4：灰度与运营（持续）

- 增加 `database_tool_enabled` 总开关、按租户/profile 的 allowlist 和紧急 kill switch；关闭后已有连接立即拒绝新请求并在 TTL 内回收。
- 先在测试 profile 和只读用户灰度，达到成功率/延迟/拒绝率阈值后再开放写能力；每个阶段保留回滚开关，不依赖重新发布客户端。
- 监控 p95 连接耗时、p95 查询耗时、超时率、截断率、审批拒绝率和驱动错误率；日志只记录 fingerprint，不采集 SQL/参数原文。

### 配置面与运行面分离

profile CRUD、secret 轮换和驱动诊断属于配置面；`query/execute` 属于运行面。MaClawSrv 建议提供以下受鉴权的配置 API（桌面端用本地 Wails binding 实现同等语义）：

| API | 说明 |
| --- | --- |
| `GET /api/v1/database/profiles` | 只返回当前 tenant/owner 可见的非敏感摘要 |
| `POST /api/v1/database/profiles` | 创建/更新 profile，secret 使用单独 secret 引用或一次性上传 |
| `POST /api/v1/database/profiles/{id}/test` | ping + inspect 能力探测，不执行用户 SQL |
| `POST /api/v1/database/profiles/{id}/rotate-secret` | 轮换 secret 并使旧连接失效 |
| `DELETE /api/v1/database/profiles/{id}` | 停用并回收连接；保留审计引用 |

运行面不接受任意 DSN，只接受已授权 `profile_id` 和 session-scoped `connection_id`。API 与 Agent tool 共用同一 `PolicyEngine`、审计字段和错误 envelope，避免 HTTP 路径成为绕过工具门禁的后门。

MaClawSrv 的 profile 写 API 需要管理员/owner 权限、CSRF/重放保护和幂等键；运行面按 tenant、owner、profile 设置速率限制与并发配额。错误响应不得回显驱动连接串、服务器 banner、文件绝对路径或 SQL 原文；详细诊断仅写脱敏服务日志。

## 10. 测试与验收

### 单元/契约测试

- schema required/dependent 字段、SQL 多语句和危险语句识别。
- 命名占位符转换（字符串/注释/`::type` 中的 `:`、参数 key 不匹配、PostgreSQL/SQL Server 方言）及 tokenizer fail-closed；兼容 positional 模式单独测试。
- 参数绑定、limit/timeout/字节上限、分页 cursor 和结果脱敏。
- profile owner/session 隔离、idle TTL、取消 context 后连接释放。
- 资源配额（并发、超时、扫描行数、结果大小、临时磁盘、批量条数）和 cancel 后无句柄泄漏。
- 连接池 session state 重置、RLS/授权视图资源拒绝、schema/行版本变化导致审批上下文失效。
- `sqlguard` 方言 fixture（MySQL backtick、PostgreSQL `::`/JSON operator、SQL Server `[]`、Access `#date#`）和 unknown 语句 fail-closed。
- adapter 错误到稳定 error class 的映射；驱动缺失给出可操作提示。
- Excel 类型推断、合并单元格、公式值读取、压缩炸弹/宏/外链拒绝、`.xlsx` 范围写入（矩形越界和合并区域 fail-closed，范围外内容保留）、`.xls/.csv` 只读拒绝和源文件哈希冲突。

### 集成测试矩阵

使用 Docker 临时实例覆盖 MySQL、PostgreSQL、SQL Server；Access 使用 Windows CI 上的 `.accdb` fixture + ODBC；Excel 覆盖 `.xlsx/.xls/.csv` fixture。所有写测试都验证事务回滚和审计 receipt。

当前自动化状态（2026-09-02 起）：

| 数据源 | 自动化方式 | 覆盖范围 | 运行位置 |
| --- | --- | --- | --- |
| MySQL/PostgreSQL/SQL Server | `corelib/database/integration_test.go`（`//go:build dbintegration`），DSN 取自 `MACLAW_DBTEST_MYSQL_DSN`/`MACLAW_DBTEST_PG_DSN`/`MACLAW_DBTEST_MSSQL_DSN`，未设置即 Skip | Connect/Ping、Inspect、`:name` 参数化查询、limit 截断 + cursor 翻页、dry-run `rolled_back_transaction` 且数据不变、`IssueApproval` + 一次性审批上下文提交、`max_affected_rows` 超限回滚、SELECT INTO/多语句/无 WHERE UPDATE 拒绝、metadata-only 审计断言（无 SQL 原文） | CI-only：`.github/workflows/main.yml` 的 `database-integration` job（postgres:16 / mysql:8 / mssql 2022 service 容器） |
| Access | `corelib/database/integration_access_windows_test.go`（`//go:build windows && cgo`，默认 `go test` 即运行），fixture 为 `testdata/integration_fixture.mdb`（ADOX + ACE 生成，People 表 4 行） | Connect、参数化查询验证行内容、只读 profile 拒绝 execute；Inspect 依赖 MSysObjects 授权，无工作组文件的机器上按设计降级为 `unsupported_capability`（测试记录并放行），有授权的机器上断言表结构 | 本机/Windows CI（release workflow 的 `build-windows` job 已加 `go test ./corelib/database/`）；缺 ACE 驱动时 Skip |
| Excel | 现有 `excel_test.go`（临时 SQLite facade + mock） | 见单元/契约测试 | 默认 `go test` |

注意：`.accdb` 格式已移除用户级安全（ULS），无法授予 ODBC Admin 对 `MSysObjects` 的读权限；`.mdb` 的 GRANT 又需要 Jet 工作组信息文件（仅完整 Access 提供）。因此 fixture 采用 `.mdb`，且 Inspect 表枚举在无工作组文件的机器上验证的是设计好的降级路径，而不是表结构断言。

MaClawSrv 另测 tenant/owner 越权、profile API 幂等/重放、速率限制和错误脱敏；GUI/TUI 运行同一套 core tool schema snapshot，防止工具面漂移。

### 验收标准

1. Agent 可在无脚本情况下完成“发现连接 → 查询 → 分页 → 导出 Excel”。
2. 未授权写入在 handler 前被拒绝；批准后只影响预览中的行数。
3. 任意 tool/result/log/任务记录中搜索不到密码和完整敏感参数。
4. 大结果不会阻塞 Agent loop，能通过 handle/cursor 继续读取。
5. 任一驱动不可用不会影响其它数据源和普通 Agent 工具启动；Access 缺少 ODBC 时有明确 doctor 提示。
6. 预览后 profile、schema 版本或 SQL 指纹变化会使审批上下文失效，不能提交旧预览。

## 11. 风险与后续演进

| 风险 | 缓解措施 |
| --- | --- |
| SQL 注入或模型生成危险 SQL | 只允许参数绑定；语句解析 + profile allowlist + 审批 |
| 大查询耗尽内存/上下文 | 行/字节/超时上限，流式扫描，受控内存 cursor；持久化 result handle 后续实现 |
| Access 驱动在目标机器缺失 | 启动 doctor 检查；能力降级为只读提示，不伪造成功 |
| Excel 类型不稳定 | 返回推断类型和警告；写入前显示列映射 |
| 查询结果写入 Excel 形成公式注入 | 对 `= + - @` 前缀值转义为文本并计入 warning |
| 生产误操作 | read-only profile、无 WHERE 检查、二次确认、mutation receipt |
| 驱动升级破坏行为 | adapter 契约测试 + 锁定版本 + 容器矩阵 |

SSH 隧道、异步大查询、只读副本路由、查询收藏、explain/NL 预览与 DDL 计划器已交付（见 §14）；后续演进仍须复用本工具的权限和审计边界。

## 12. 本次审查结论

| 原设计问题 | 改进决定 |
| --- | --- |
| Agent 参数可能携带密码/完整 DSN | 工具只接受 `profile_id`；一次性连接改走 secure form，secret 仅后端内存可见 |
| Excel 宣称支持 `.xls/.csv` 写入，与现有 writer 不一致 | 明确 `.xlsx` 才是首期写入格式，`.xls/.csv` 只读并可导出为新 `.xlsx` |
| dry-run 容易被误解为绝对无副作用 | 增加 `dry_run_guarantee`，区分 policy-only、事务回滚和 unsupported |
| 所有 adapter 被假设具备事务/cursor | 新增 `Capabilities`，按数据源报告能力并在不支持时提前拒绝 |
| 分页 token、排序和取消语义不明确 | 增加 owner 绑定、TTL、稳定排序 warning、过期失败和资源释放规则 |
| 仅依赖 tokenizer 可能误判复杂 SQL | 明确 tokenizer 不是完整解析器，无法识别时 fail-closed，并叠加驱动只读属性 |
| `?` 同时是 PostgreSQL JSON 运算符和占位符，跨方言转换有歧义 | 改为命名占位符 `:name` + object 参数；旧 positional 模式仅限显式兼容路径 |
| 缺少统一资源上限，连接/临时文件可能泄漏 | 增加并发、超时、扫描行数、结果大小、临时磁盘和批量条数配额，并要求显式释放 |
| 导出数据可能触发 Excel 公式注入 | 对危险前缀单元格转义，并返回 warning 计数 |
| 加密的 Access/Excel 文件凭据处理未定义 | 仅允许 secure form + secret_ref，失败分类为 authentication/unsupported |
| 连接池可能泄漏上一个会话的 `SET/search_path` 状态 | checkout 时重置 session state，并禁止 Agent 执行 session 改写语句 |
| Excel/查询结果可能造成压缩炸弹、宏执行或数据外带 | 复用 Office 预检、禁用宏/外链；当前仅内存分页，持久化句柄启用后必须加密短期存储，导出需数据分类审批 |
| dry-run 与提交之间存在数据竞争 | 绑定 schema 版本并使用行版本/隔离级别复核，超限自动回滚 |
| 新增驱动可能导致安装/构建失败 | Access ODBC 采用可选能力和构建检查，纳入许可证/CVE/release checklist |
| 缺少运营回滚手段 | 增加总开关、租户/profile 灰度、kill switch 和关键指标 |
| GUI/TUI 可能复制工具 schema，长期产生漂移 | 以 `corelib/agent/tool_register_core.go` 为单一真源，host 仅注入 handler |
| SQL 安全若靠正则会被方言/嵌套语法绕过 | 独立 `sqlguard` 输出语句分类和资源引用，unknown 必须 fail-closed，并建立 dialect fixture |
| Access 位数/文件锁问题未定义 | 安装器检查 ODBC 位数，默认只读共享，写入前检查锁、备份和磁盘 |
| Excel `range` 仅停留在契约文字，旧 writer 会整本覆盖 | 新增 `WriteFileRange`：校验 A1 矩形边界，保留其它 sheet/单元格；已有文件省略 range 直接拒绝，并对合并区域 fail-closed |
| 宿主未注入身份 scope 时仍可伪造 `_owner_id/_session_id` | handler 拒绝未绑定可信 `RequestScope` 的下划线身份字段，绑定后始终以 context 值覆盖模型字段 |
| Excel 文件写入未统一纳入 profile operation allowlist | `operationAllowed` 覆盖 `write_table/export_excel`；携带 `connection_id` 的文件操作按 Excel profile 再校验，且 operation allowlist 不能覆盖 `write_enabled/read_only` 双重开关 |
| `read_table` 便捷路径没有统一结果大小边界 | 复用 1.5 MB JSON 预算；超限截断时返回 `result_byte_limit` 与 `pagination_unavailable`，首行超限直接返回 `result_too_large`，不伪造游标 |

## 13. 交付物清单

- `corelib/database` 接口、驱动 adapter、连接池和策略单元测试。
- `gui/tool_database.go` 与 `gui/tool_registry_builtin.go` 注册改动；TUI/AgentService handler 接线。
- AppConfig schema、密钥环存储迁移和 doctor 检查。
- 数据源管理与查询结果 AgentView 面板。
- Docker/ODBC/Office fixture 集成测试及本设计文档对应的用户手册章节。

## 14. 实现一致性审查（2026-09-02）

本节是设计与当前代码的验收边界，防止“文档已定义”被误解为“功能已交付”。每个阶段合入前必须更新状态，并由契约测试锁定行为。

| 能力 | 当前实现 | 交付门槛/剩余工作 |
| --- | --- | --- |
| 统一工具 schema | `corelib/database.ToolProperties()` 作为唯一属性来源，Core/GUI 注册均引用它；GUI 注册使用 `HandlerCtx` 传递宿主 request context；GUI、TUI、AgentService 启动时记录 `ToolSchemaHash()`（doctor 报告同一值），用于跨宿主漂移诊断 | 跨版本部署的"hash 不一致则禁用"策略仍需运维侧落地；禁止 host 手写字段，变更需同步版本号 |
| 命名参数 | 扫描器跳过字符串、注释、标识符和 `::` cast；按 MySQL/ODBC、PostgreSQL、SQL Server 生成占位符 | 增加方言 fixture 和 unknown 语法 fail-closed 测试；不得回退字符串拼接 |
| 查询门禁 | `sqlguard` 拒绝空 SQL、多语句、注释、session/external 语句和 data-modifying CTE；`SELECT INTO`/`INTO OUTFILE` 归类为 external 并在 query/execute 两条路径拒绝；profile schema/table allowlist 与列 deny/mask 已在 adapter 层执行；方言 fixture 已覆盖 MySQL backtick、PostgreSQL `::`/JSON 运算符、SQL Server `[]`、Access `#date#` 与 Unicode 空白绕过；SQL/Excel facade 的 `Capabilities.Cursors=true` | MySQL/PostgreSQL/SQL Server 的真实驱动集成测试已在 CI `database-integration` job 自动化（见 §10 矩阵）；开放生产 profile 前仍需在灰度环境复核 |
| 写入审批 | profile `read_only + write_enabled` 双重 opt-in；非 dry-run 提交只接受宿主注入的一次性审批上下文，模型参数中的 token 直接拒绝；GUI task-panel 已通过 `HandlerCtx` 注入内存 opaque token；SQL handler 会校验 SQL/参数 fingerprint、profile/schema 版本、原子消费 approval ID，并在影响行数超限时回滚；Excel 文件操作若携带 `connection_id` 同样执行 profile operation allowlist，提交结果返回 `receipt_id` 并写入 metadata-only 审计；`Manager.IssueApproval` + `PendingStore`（`database_pending.json`，0600，原子重写，损坏文件隔离）提供统一签发与持久化 pending mutation：token 只存 SHA-256 摘要、绑定 SQL/参数 fingerprint 与 profile schema 版本、过期即销毁、配置变更由 `UpdateProfiles` 清除、重启后仍只能消费一次；配置 PendingStore 后进入严格模式，非签发 token 一律 fail-closed；GUI task-panel 已迁移到 `IssueApproval` 统一签发（严格模式失败时回退内存 token 兼容无 store 宿主）；TUI 通过 `ExtraHandlersCtx` + PendingStore + 交互确认签发；MaClawSrv `POST /api/v1/admin/database/approvals` 签发并把 token 留在 session envelope；`SetIssueGate` 接入 PolicyEngine deny | 无剩余代码缺口 |
| dry-run | 纯 DML 批在支持事务的 SQL adapter 上于事务中执行并回滚，返回 `rolled_back_transaction` 和预演影响行数；含 DDL 的批与 Excel 保持 `policy_only`（DDL 可能隐式提交，不做虚假回滚承诺），并返回 `plan`（操作类型、目标表、当前列、destructive 标记）；dry-run 中的语法/约束/行数超限错误在提交前暴露 | 触发器/序列等外部副作用仍可能存在，guarantee 字段不得被解读为绝对无副作用 |
| 只读副本 | profile `replica_host`/`replica_port`（可选独立 `replica_ssh_session_id`）将 query/inspect/explain 路由到只读副本；写入始终走主库；副本连接强制 `read_only`；副本不可达时回退主库并带 `replica_unavailable`/`replica_fallback` warning；模型参数不得携带 replica 字段 | 副本与主库必须共享同一套 secret_ref/权限边界 |
| 查询收藏 | `save_favorite`/`list_favorites`/`delete_favorite` 按 owner 隔离，只接受只读参数化 SQL，持久化 `database_favorites.json`（0600）；`query`/`explain` 可用 `favorite_id` 代替 sql | 收藏不得保存凭据或写语句 |
| explain / NL 预览 | `explain`+sql 对 SELECT 做方言 EXPLAIN（PostgreSQL `EXPLAIN (FORMAT TEXT)`、MySQL `EXPLAIN`、SQL Server `SHOWPLAN_TEXT`、Excel `EXPLAIN QUERY PLAN`），拒绝 `EXPLAIN ANALYZE`；仅 `prompt` 时返回 schema + 生成约束，不执行用户 SQL | Access 无 EXPLAIN，fail-closed |
| 分页/句柄 | Manager 提供 owner/session/connection 绑定、5 分钟 TTL、消费后删除的内存 `next_cursor`；截断结果同时返回 `result_handle`（与 `next_cursor` 同一 token）；断开、空闲过期、关闭或 profile schema 版本变化会使 cursor 失效并删除磁盘副本；首页已返回行数/字节截断 warning；已取消的请求在并发闸门处确定性拒绝（`cancelled`），不会随机占用槽位；超大结果经 toolresult spill 落盘时强制加密（AES-256-GCM store key，`.enc` 后缀，`read_tool_result` 透明解密分页，session 隔离与 prune/stats 均覆盖 `.enc`），明文不再落盘；`Manager.SetResultStoreDir` 将物化结果以 AES-256-GCM 写入 `database_results/{id}.enc`，进程重启后仍可在 TTL 内按 owner/session/connection 分页，无需活连接 | 无剩余代码缺口 |
| Profile 热刷新 | GUI/TUI 在 database 调用前刷新 profile；`UpdateProfiles` 对变更/删除配置关闭旧连接、保留未变更 profile 的并发槽并清理游标；已有跨请求并发更新 `-race` 压力测试；MaClawSrv 管理配置 API（`/api/v1/admin/database/profiles*`，含 test/rotate-secret/delete 与幂等键）经 `CoreAgentExecutor.RefreshDatabaseProfiles` 接入同一刷新入口，写配置成功后对该 tenant/user 的全部缓存 manager 调 `UpdateProfiles`；桌面端已交付 GUI 数据源管理面板（设置 → 数据源）：Wails binding `DatabaseProfilesList/Save/Delete/SetSecret/Test`（`guiapp/app_database_profiles.go`）复用同一 `UpdateProfiles` 刷新入口，凭据经 `DatabaseProfileSetSecret` 仅写入 OS keyring（service `maclaw`、item `database/<profile-id>`），profile 只持久化 `secret_ref` 并在写入后 bump SchemaVersion | 无剩余代码缺口 |
| 连接生命周期 | session 连接支持显式 disconnect、owner/session 绑定、关闭、默认 10 分钟 idle TTL、每 profile 4 路并发配额，并在 checkout 前执行方言 session reset；`Manager.UpdateProfiles` 会关闭配置变更/删除 profile 的旧连接并清理游标；测试已覆盖并发配额 quota_exceeded、reset 失败 fail-closed（用户 SQL 不执行）、disconnect/TTL 后 cursor 失效且无泄漏、取消请求不泄漏并发槽 | 真实驱动连接/查询/事务路径已由 CI `database-integration` job 覆盖；高并发压力仍靠单元级 `-race` 测试 |
| Excel/Access | Excel 查询复用临时 SQLite，core/TUI/AgentService 具备 workspace 路径边界；加密工作簿走 `secret_ref`：无密码 fail-closed 为 `authentication`，有密码但本进程无法解密为 `unsupported_capability`（默认不破解）；`.xlsx` 写入支持 source SHA-256 并发修改检测，显式 A1 `range` 通过 zip XML 补丁更新矩形内单元格并保留 `styles.xml` 与范围外 `s=`；合并单元格重叠 fail-closed 且有回归测试；已有文件省略 `range` 会 fail-closed；非 `.xlsx` 范围写入仍可能丢失样式，结果带 `range_styles_not_preserved` warning；Access 依赖 ODBC；adapter 仅在 `windows && cgo` 构建链接，CGO_ENABLED=0 构建可编译并 fail-closed 返回 `driver_missing`；`DetectAccessODBC` 提供 cgo-free 运行期检测（注册表探测 ACE 驱动安装与 32/64 位匹配），doctor 与 connect 错误均给出可操作安装提示；只读 profile 的 DSN 带 `ReadOnly=1`；写入前检查 `.ldb`/`.laccdb` 锁、可写句柄、剩余磁盘空间，并复制 `.maclaw-prewrite.bak`；不支持 COMPACT/REPAIR；`Transactions=false`，dry-run 为 `policy_only`，单条 DML 在审批后直接提交 | Access 真实 ODBC 写入由 `TestIntegrationAccessWrite` 覆盖，无 ACE 驱动时 Skip |
| SSH 隧道 | profile `ssh_session_id` 绑定已批准的活动 SSH 会话；`Manager.SetTunnelDialer` 由 GUI/TUI/Core 注入，经 `ssh.Client.DialContext` 直连远端 Host:Port，不做本地端口转发；模型参数中的 `ssh_session_id` 直接拒绝；Access/Excel 不可隧道；无 dialer 或会话不存在 fail-closed | 必须复用现有 SSH 会话与审批，数据库工具不得自行打开 SSH |
| 异步大查询 | `query` 带 `async=true` 立即返回 `job_id`；`job_status` 按 owner/session 隔离轮询；完成后写入加密 `result_handle`（与 cursor 同一存储）；每 manager 最多 4 个在途作业，TTL 10 分钟；断开/关闭取消在途作业 | 大查询不得阻塞 Agent loop |
| 审计/回执 | `Manager.SetAuditSink` 输出 metadata-only 事件（fingerprint、参数数量、影响行数、approval ID、receipt ID），不携带 token/SQL 原文；`FileAuditStore` 提供 append-only JSONL 持久化（0600，失败不阻断工具调用），并对带 receipt ID 的成功写入追加不可变 mutation receipt 记录；GUI、TUI 和 AgentService 已接线到各自数据目录的 `database_audit.jsonl`；MaClawSrv 的 `DatabaseAuditSink` 已接入中央租户审计链（`Service.RecordAuditEvent`，统一脱敏）；`GET /api/v1/admin/database/receipts/{receiptId}` 先查租户审计链，再回退 DataRoot JSONL，按 tenant/user 隔离且不返回 token/SQL | 无剩余代码缺口 |

### 14.1 兼容性与版本策略

工具契约增加 `contract_version`（当前为 `1`，由服务端结果 envelope 返回）。新增 action 或字段只能向后兼容；改变必填字段、错误类别或分页语义必须提升主版本。GUI、TUI 和 MaClawSrv 在启动时记录 schema hash，发现不一致则禁用 database 工具并提示升级，而不是让不同 host 以不同字段执行同一调用。

### 14.2 发布阻断条件

以下任一项未满足时，功能只能在测试 profile 灰度，不能标记为生产可用：

- 未通过 MySQL/PostgreSQL/SQL Server/Access/Excel 的契约和安全 fixture；
- 写操作没有一次性审批上下文、schema 版本复核和不可变 mutation receipt；
- 结果句柄未绑定 owner/session、未设置 TTL，或取消路径无法证明 rows/临时文件已释放；
- profile/table/path allowlist 仍由 prompt 约束，未在 handler/adapter 层强制执行；
- 任一日志、错误响应、导出元数据或任务记录可能包含 secret、完整参数或未脱敏敏感列。
