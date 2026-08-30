# 云端工作区多机协同同步设计

## 1. 文档信息

- 状态：提案
- 适用组件：MaClaw GUI、MaClaw Hub、`hub/internal/cloudworkspace`
- 目标版本：Cloud Workspace Sync v2
- 更新时间：2026-08-29

## 2. 背景与问题

当前云端工作区采用 workspace 级排他租约、整棵 manifest 替换和 `if-match` revision 校验。该模型适合“单机读写、其他机器等待”，但不支持多台机器同时编辑：

1. 第二台机器会因活动 lease 收到 `CLOUD_WORKSPACE_IN_USE`。
2. 两台机器修改不同文件时，整棵 manifest 仍可能产生 revision 冲突。
3. 发生冲突后，客户端没有保存 base manifest，无法可靠执行三方合并。
4. Hub 当前对象以 AES-GCM 封装原始文件，缺少压缩层，重复内容虽可寻址去重，但单对象占用仍偏大。

本设计将同步模型改为“多机可写、按文件增量、事件日志驱动”，并将对象存储改为“压缩后加密”。

## 3. 设计目标

### 3.1 必须满足

- 多台不同机器可以同时打开同一个 workspace 并读写。
- 不同文件的并发修改自动合并。
- 同一文本文件的非重叠修改自动合并。
- 冲突不能静默覆盖或丢失；无法合并时保留双方版本。
- 客户端离线可编辑，恢复网络后自动补同步。
- 云端对象默认使用 Zstandard 压缩，再使用 AES-256-GCM 加密。
- 相同明文内容跨机器去重。
- 重复请求幂等，可安全重试。
- 兼容现有 v1 客户端并支持渐进迁移。

### 3.2 非目标

- 不保证二进制文件的语义级自动合并。
- 不把 Hub 变成实时协同编辑器的渲染服务。
- 不在 v2 中改变现有用户、租户和机器认证体系。

## 4. 总体架构

```text
┌──────────────┐       operations/events       ┌──────────────┐
│  MacLaw GUI A │ ───────────────────────────▶ │              │
└──────────────┘                               │              │
                                               │  MaClaw Hub  │
┌──────────────┐       operations/events       │              │
│  MacLaw GUI B │ ◀─────────────────────────── │              │
└──────────────┘                               └──────┬───────┘
                                                       │
                                         compressed + encrypted objects
                                                       │
                                               ┌───────▼───────┐
                                               │ Object Store   │
                                               └───────────────┘
```

同步事实来源从“最后一次 manifest”扩展为：

- 当前文件表：每个路径的最新版本和 tombstone。
- 不可变 operation 事件表：记录所有文件变更。
- 定期生成的压缩 snapshot：用于快速打开和事件回收。
- 内容寻址对象表：记录明文 hash、压缩信息和物理存储位置。

## 5. 并发模型

### 5.1 从排他 lease 改为协同 session

workspace 不再对普通编辑要求唯一 lease。每个 GUI 打开时注册一个 session：

```text
workspace_id
tenant_id
user_id
machine_id
client_instance_id
session_id
last_seen_at
capabilities
```

session 用于在线状态、审计、断线清理和通知，不阻止其他机器编辑。

现有 lease 保留为维护锁，适用于：

- 删除、恢复 workspace；
- 迁移 v1 数据；
- 压缩/重建 snapshot；
- 管理员执行强制修复。

### 5.2 版本层次

- `workspace_seq`：workspace 的单调递增事件序号。
- `file_revision`：单个路径的版本号。
- `base_file_revision`：客户端生成 operation 时看到的文件版本。
- `client_cursor`：客户端已经应用到的 workspace 事件序号。

服务端以数据库事务为线性化点，按 `workspace_id + path` 更新当前文件版本。

### 5.3 冲突判定

提交 operation 时：

1. `base_file_revision` 等于当前 `file_revision`：直接应用。
2. base 过期，但远端和本地修改的是不同路径：直接应用并产生新事件。
3. base 过期且同一路径被修改：进入合并器。
4. operation 的 `op_id` 已存在：返回此前结果，不重复应用。

## 6. 文件冲突策略

### 6.1 文本文件

服务端或客户端执行三方合并：

```text
base   = base_file_revision 对应内容
local  = 本次 operation 内容
remote = 当前云端内容
```

非重叠行自动合并。重叠区域生成标准冲突标记，或保存为独立冲突副本，交由 GUI 解决。

文件类型策略：

- `.go`、`.ts`、`.js`、`.py`、`.md`、`.txt`：行级三方合并。
- JSON/YAML：解析后按字段合并，字段类型冲突时回退文本冲突。
- 未知文本：尝试 UTF-8 三方合并，失败则按二进制处理。

### 6.2 二进制文件

不尝试覆盖任意一方，保留两个对象并创建冲突路径：

```text
image.png
image.conflict-machineA-<opid>.png
```

事件中记录 `conflict_of` 和双方 revision，GUI 显示“保留本地/保留云端/另存双方”。

### 6.3 删除与修改并发

删除使用 tombstone，而不是立即物理删除。删除与修改并发时保留修改版本并生成待解决冲突，只有确认后才清理 tombstone 和对象引用。

## 7. 客户端状态与流程

每个 GUI 在 `.maclaw-cloud/state.json` 保存：

```json
{
  "protocol": 2,
  "workspace_id": "cws_xxx",
  "client_instance_id": "gui-uuid",
  "last_event_cursor": 1842,
  "snapshot_seq": 1800,
  "pending_op_ids": ["op-1", "op-2"],
  "conflict_count": 0
}
```

### 7.1 打开 workspace

1. 注册 session。
2. 获取最近 snapshot 和 `snapshot_seq`。
3. 使用 cursor 拉取增量 events。
4. 校验对象 hash，解密、解压并写入本地缓存。
5. 启动 watcher 和事件订阅。

### 7.2 本地编辑

1. watcher 发现文件变化。
2. 计算明文 SHA-256，生成 operation。
3. 将 operation 写入本地 pending 队列。
4. 后台上传对象，再提交 operation。
5. 收到确认后推进 cursor 并删除 pending 项。

### 7.3 接收远端事件

1. 按顺序拉取 `workspace_seq`。
2. 对未修改的本地文件直接应用。
3. 对本地有 pending 修改的文件执行三方合并。
4. 冲突文件进入冲突目录/冲突列表，不阻塞其他路径同步。

### 7.4 进程内与跨线程串行化

- 每个 workspace 一个 Push/Apply mutex 或 singleflight。
- watcher、手动保存、关闭流程不得并发提交同一路径 operation。
- 失败 operation 采用指数退避，达到上限后保留在 pending 队列并提示用户。

## 8. API 设计

### 8.1 Session

```http
POST /api/v1/cloud-workspaces/{id}/sessions
POST /api/v1/cloud-workspaces/{id}/sessions/{session_id}/heartbeat
DELETE /api/v1/cloud-workspaces/{id}/sessions/{session_id}
```

### 8.2 Snapshot 与事件

```http
GET /api/v1/cloud-workspaces/{id}/snapshot
GET /api/v1/cloud-workspaces/{id}/events?after_seq=1842&limit=500
```

### 8.3 Operation

```http
POST /api/v1/cloud-workspaces/{id}/operations
```

请求示例：

```json
{
  "op_id": "op-machineA-uuid",
  "path": "src/main.go",
  "kind": "put",
  "base_file_revision": "fr-42",
  "object_sha256": "64-char-lowercase-sha256",
  "plain_size": 12000,
  "client_instance_id": "gui-uuid"
}
```

响应应包含：

```json
{
  "accepted": true,
  "workspace_seq": 1843,
  "file_revision": "fr-43",
  "merge": "none|auto|conflict",
  "current": { "path": "src/main.go", "object_sha256": "..." }
}
```

### 8.4 对象 API

保留现有 PUT/分块/complete API，增加压缩元数据协商：

```http
X-Object-Plain-SHA256: ...
X-Object-Plain-Size: 12000
X-Object-Compression: zstd
```

服务端不得信任客户端声明的 hash、大小或压缩类型，必须解压后重新校验明文 hash。

## 9. 压缩与加密存储

### 9.1 写入管线

```text
明文文件
  → 计算 SHA-256（对象身份）
  → 评估压缩收益
  → Zstandard 压缩（默认 level 3）
  → AES-256-GCM 加密
  → 分块写入对象存储
```

压缩收益小于 5% 时保存原文，`compression=none`。以下类型默认跳过压缩：`jpg/png/gif/webp/mp4/zip/gz/7z/pdf`（可配置）。

### 9.2 读取管线

```text
读取密文 → AES-GCM 解密 → Zstd 解压（如适用） → 校验明文 SHA-256 → 返回
```

### 9.3 元数据

对象表增加：

```sql
ALTER TABLE cloud_workspace_objects ADD COLUMN plain_size_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cloud_workspace_objects ADD COLUMN stored_size_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cloud_workspace_objects ADD COLUMN compression TEXT NOT NULL DEFAULT 'none';
ALTER TABLE cloud_workspace_objects ADD COLUMN compression_level INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cloud_workspace_objects ADD COLUMN encryption_version TEXT NOT NULL DEFAULT 'aes-gcm-v1';
```

Manifest/snapshot 文件项增加：

```json
{
  "path": "src/main.go",
  "object_sha256": "...",
  "plain_size": 12000,
  "stored_size": 4200,
  "compression": "zstd",
  "file_revision": "fr-43",
  "tombstone": false
}
```

配额按 `plain_size` 统计，磁盘告警按 `stored_size` 统计；重复对象只计一次物理空间。

## 10. 数据库模型

```sql
CREATE TABLE cloud_workspace_files (
  workspace_id TEXT NOT NULL,
  path TEXT NOT NULL,
  file_revision TEXT NOT NULL,
  object_sha256 TEXT,
  plain_size_bytes INTEGER NOT NULL DEFAULT 0,
  tombstone INTEGER NOT NULL DEFAULT 0,
  updated_seq INTEGER NOT NULL,
  PRIMARY KEY (workspace_id, path)
);

CREATE TABLE cloud_workspace_events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL,
  op_id TEXT NOT NULL UNIQUE,
  path TEXT NOT NULL,
  kind TEXT NOT NULL,
  base_file_revision TEXT,
  new_file_revision TEXT NOT NULL,
  object_sha256 TEXT,
  client_instance_id TEXT NOT NULL,
  conflict_of_seq INTEGER,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_cws_events_workspace_seq
  ON cloud_workspace_events(workspace_id, seq);
```

原 `cloud_workspace_manifest_entries` 保留为 v1 兼容视图和 snapshot 缓存，不再作为并发写入的唯一事实来源。

## 11. 安全设计

- 继续使用 tenant/user/workspace 级访问校验。
- session token 与 machine token 绑定，禁止伪造 `client_instance_id`。
- operation 的 `op_id` 必须不可预测，建议 UUIDv4。
- 路径继续执行现有安全规范化，拒绝绝对路径、`..`、反斜杠和 NUL。
- 压缩前执行大小上限检查，解压后再次执行明文大小上限检查，防止压缩炸弹。
- AES-GCM AAD 绑定 tenant、user、workspace、对象 hash 和 encryption version。
- 日志中不记录明文内容和密钥，仅记录 hash、大小、seq 和错误类型。

## 12. 兼容与迁移

### Phase 1：压缩对象（兼容 v1）

- 新对象使用 zstd；旧对象标记 `compression=none`。
- 读取端同时支持压缩和未压缩对象。
- 保持现有 lease + manifest API。
- 后台低优先级重压缩旧对象，失败可重试。

### Phase 2：增量协议

- 新增 session、snapshot、events、operations API。
- GUI 通过能力协商选择 v2；旧 GUI 继续使用 v1。
- v2 operation 成功后同步更新 v1 manifest 视图，保证旧客户端可读。

### Phase 3：多机协同

- 普通编辑不再要求 workspace 排他 lease。
- lease 限定为维护锁；维护期间新 operation 返回可重试错误。
- 启用文本三方合并、二进制冲突副本和离线 pending 队列。

### 回滚

- 任何 workspace 可切回 v1 只读或单写模式。
- v2 事件日志和 snapshot 保留，不删除对象。
- 回滚前必须等待 pending operation 清空或显式导出。

## 13. 可观测性

新增指标：

- `cloud_workspace_active_sessions`
- `cloud_workspace_operations_total{kind,result}`
- `cloud_workspace_merge_total{result}`
- `cloud_workspace_pending_operations`
- `cloud_workspace_event_lag`
- `cloud_workspace_compression_ratio`
- `cloud_workspace_stored_bytes` 与 `plain_bytes`
- `cloud_workspace_conflicts_total`

审计事件至少包含 workspace、machine、client instance、op_id、seq、结果和冲突类型。

## 14. 测试与验收标准

### 并发正确性

- 两台机器同时修改不同文件：最终两处修改均存在。
- 两台机器修改同一文本的不同区域：自动合并成功。
- 两台机器修改同一文本的同一区域：生成冲突，不丢失任一版本。
- 两台机器同时删除/修改同一文件：生成 tombstone 冲突。
- 相同 `op_id` 重试：只产生一个事件。
- 网络断开后恢复：pending operation 按顺序补发。

### 存储正确性

- zstd 对象读取后明文 hash 与原文件一致。
- 压缩失败或收益不足时可回退到 `none`。
- 解密、解压、hash 校验任一步失败均拒绝对象。
- 相同明文 hash 不创建重复对象。
- 物理空间统计使用压缩后大小，逻辑配额使用明文大小。

### 兼容性

- v1 GUI 可读取 v2 生成的兼容 manifest。
- v2 GUI 可读取 v1 未压缩对象。
- 迁移中断后可安全重试，不产生悬挂引用或不可解密对象。

## 15. 实施拆分

1. 抽象 `ObjectCodec`，实现 none/zstd + AES-GCM，并补充对象元数据。
2. 增加 `cloud_workspace_files`、`cloud_workspace_events` 表和事务存储层。
3. 实现 operation 幂等提交与 cursor 读取 API。
4. GUI 增加 pending 队列、事件应用器和 workspace 级 singleflight。
5. 实现文本三方合并与二进制冲突副本。
6. 增加 session 心跳和维护锁兼容层。
7. 灰度启用 v2，完成指标、故障演练和回滚验证。

## 16. 关键决策总结

本方案的核心不是让多个客户端竞争同一把写锁，而是：

> 不可变内容对象 + 文件级 operation 日志 + 因果版本 + 可解释合并 + 压缩后加密存储。

这样可以同时满足多机协同、冲突可控、离线可恢复和云端空间节省四个目标。
