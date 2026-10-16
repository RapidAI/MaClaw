# 云端工作区任务实现原理与设计评审

> 评审日期：2026-09-01
> 评审范围：`gui` 云端工作区挂载/同步/sidecar、`hub/internal/cloudworkspace` 存储与租约、`hub/internal/httpapi` API、SQLite migration。
> 结论：实现已经按“多设备接续、非并发编辑”收敛为 `v1-sequential` 单一运行协议，并完成服务端实例身份、fencing、网络分区降级、统一 retained quota、跨 Hub 进程提交崩溃矩阵、一致性 backup 归档/恢复原语、可选的本地缓存静态加密和撤权密钥销毁，以及可选的跨设备小时级带宽配额（11.30，默认关闭、租户可配）。11.31 移除了评审认定的过度设计：密钥轮换/退休子系统、持续备份调度器、审计 tombstone 与 retention 原语、purge 两阶段 intent（改为 GC 孤儿目录回收）、环境 profile 和 sidecar CAS 状态机。v2 多写入接口仍保持禁用。灾备编排（定时、异地、告警、恢复演练）与密钥托管均由部署方在进程外完成；按真实规模量化 RPO/RTO 仍是部署方事项。

## 1. 当前实现原理

### 1.1 任务与缓存映射

1. GUI 在创建任务时取得 `workspace_id`，缓存目录为 `GetDataDir()/cloud-workspaces/{tenant}/{workspace}`；同一进程先向 Hub 换取服务端签发的 instance session，客户端不再自报可信实例身份。
2. 缓存目录作为任务的 `workingDir`；任务记录、Tab 会话、sticky/checkpoint 仍通过本机任务目录和 sidecar 通道关联。writer cache 由 workspace actor 和本机进程锁保护，其他设备明确以 read-only 打开。
3. `PrepareCloudWorkspace` 先申请 Hub lease，再依据本地 `.maclaw-cloud/state.json`（包括空 manifest 的 initialized 基线）与远端 manifest 决定 Pull、Push 或冲突提示；成功后启动 fsnotify watcher 和 heartbeat。网络分区或 lease 过期会自动停止上传并降级只读。
4. 文件对象按 SHA-256 寻址，Hub 以带 key-id 的 AES-GCM v2 envelope 加密落盘；大文件先写分块 staging，完成时拼接、校验 hash，并按统一 retained quota 计量。
5. 对话、任务信息、workbench、checkpoint 通过四个命名 sidecar 单独加密保存；写入是 lease 保护下的 `If-Match` CAS（revision = 明文 SHA-256）+ 原子文件替换，revision 与 task binding 在同一事务提交；canonical 文件缺失时回退读取旧版 immutable revision 文件（11.31 简化前布局）。

### 1.2 运行时同步协议（已收敛）

- **v1-sequential（唯一运行协议）**：`manifest` 整体替换，`if_match_revision` 做乐观版本保护；写入要求 active lease + server-issued instance session + fencing token。
- **受保护的增量优化**：`manifest-delta` 只是在同一事务内批量修改 manifest、引用计数、用量和 snapshot，仍是单写者提交，不产生 per-file event。
- **只读历史表**：`cloud_workspace_files`、`cloud_workspace_events`、`/events`、`/operations` 仅用于迁移审计；运行时 endpoint 直接返回 `PROTOCOL_MISMATCH`，不会参与 manifest、配额、GC 或客户端追赶。

旧版评审中提到的 `Shared=true`、无 lease watcher 和 v2 operation 流程均已删除或封死。任何未来多机协同必须保持“一个 writer、多个 reader”，不以恢复并发编辑为隐含目标。

## 2. 高风险问题（P0/P1）

说明：本节保留 2026-08-31 初始评审中的证据，便于追溯缺陷来源。P0-1、P0-3、P1-2、P1-3 随 v2 下线转为“历史数据迁移与下线”问题；P0-2、P1-1、P1-4、P1-5 已在 11.8–11.15 的实现批次中修复。当前验收状态和未闭环边界以文档末尾 11.31（含 11.31.4 自审修复）为准；11.15–11.17、11.19、11.25–11.28 中被 11.31 移除的能力按历史记录阅读。

### P0-1：v2 操作没有更新 manifest、引用计数和工作区用量

**证据**：`ApplyOperation` 只写 `cloud_workspace_files`、`cloud_workspace_events`（`hub/internal/cloudworkspace/operations.go:138-176`），没有更新 `cloud_workspace_manifest_entries`、`cloud_workspaces.used_bytes/file_count/manifest_revision`，也没有调整 `cloud_workspace_objects.ref_count`。

**影响**：

- `GET /manifest` 仍返回旧 v1 清单，旧客户端看不到 v2 修改；迁移兼容承诺失效。
- entitlement 中的 `used_bytes`、文件数和 revision 长期不准，配额 UI 可能显示“未占用”。
- GC 按 `ref_count=0` 删除对象（`hub/internal/cloudworkspace/gc.go:330-381`），而 v2 文件表仍引用这些对象，最终出现“事件存在但对象已被回收”的不可恢复数据丢失。

**建议**：选定唯一事实来源并强制投影：

1. v2 operation 与文件表、对象引用、工作区计数在同一事务内更新；
2. 为 v1 manifest 建立事务性 projection（或让 v1 读取 v2 snapshot）；
3. GC 查询 v2 文件表中的非 tombstone 引用，不能只看 manifest 的 `ref_count`；
4. 增加“对象存在、文件表、manifest、用量”一致性巡检和修复命令。

对当前“顺序多机”目标，最小且安全的选择不是补齐 projection，而是停止 v2 写入；历史 v2 数据只在迁移阶段折算为一次 manifest snapshot，之后不再双写。

### P0-2：压缩对象的数据库元数据会被 `INSERT OR IGNORE` 吃掉，读取可能必然失败

**证据**：上传流程先由 `PrepareObjectPut` 插入一行 `cloud_workspace_objects`（仅填 `size_bytes`）；随后 `BlobStore.Put` 压缩并调用 `recordObjectMeta`，但该函数使用 `INSERT OR IGNORE`（`hub/internal/cloudworkspace/blob.go:287-304, 386-395`）。读取时压缩类型和明文长度完全依赖该行（`blob.go:342-349`）。

**影响**：大于 1 KiB 且压缩收益足够的文件，磁盘中实际是 zstd 数据，但数据库仍是 `compression=none/plain_size=0`。读取会把压缩字节当明文做 SHA-256，返回 `ErrBlobCorrupt`；即使未触发错误，`stored_size` 也不准确。

**建议**：将对象元数据写入改为 `UPDATE ... WHERE workspace_id+sha` 或 `INSERT ... ON CONFLICT DO UPDATE`，并在“写文件 + 写元数据”之间增加崩溃恢复状态。补充 >1 KiB 可压缩文件的端到端测试。

### P0-3：删除事件遇到本地修改时被静默吞掉

**证据**：`PullEvents` 检测到远端 delete 且 `localChanged=true` 时直接 `continue`（`gui/cloud_workspace_sync.go:275-282`）。游标已在前面推进，因此该事件不会再次重放。

**影响**：本地文件继续存在，但云端已删除；没有冲突副本、冲突记录或用户提示，下一次同步无法自动收敛，属于静默分叉。

**建议**：删除/修改并发必须生成 tombstone conflict：保留本地文件，写入冲突目录或冲突数据库记录；只有用户选择后才推进该路径的基线。冲突事件需要可查询、可重试，不能仅靠日志。

### P1-1：lease 冲突自动降级为 Shared，但释放路径仍调用需要 lease 的 v1 Push

**证据**：`prepareCloudWorkspace` 在 `CLOUD_WORKSPACE_IN_USE` 时设置 `Shared=true`（`gui/cloud_workspace_mount.go:960-982`）；但 `releaseCloudWorkspace` 无论是否 shared 都调用 `proto.Push`（`mount.go:726-737`），该 API 最终走要求 lease 的 `ReplaceManifest`。

**影响**：共享挂载释放时 Push 得到 `ErrLeaseRequired`，默认路径重新把 mount 放回全局表并重启 watcher/heartbeat（对 shared mount 没有 lease），导致任务无法正常关闭，缓存状态和远端状态也可能长期不一致。

**建议**：短期直接移除 Shared 自动降级，严格执行 v1 独占模型；若要启用 v2，多写模式必须有独立的 release/flush 流程（`PushOperations` + sidecar），且 shared mount 不能调用 v1 manifest Push。

### P1-2：v2 冲突并未实现设计文档承诺的三方合并

**证据**：`ApplyOperation` 只比较 `base_file_revision`，不匹配就写 rejected event（`hub/internal/cloudworkspace/operations.go:144-166`）；GUI `materializeConflict` 只下载远端对象保存为 `.conflict-*`（`gui/cloud_workspace_sync.go:335-375`）。

**影响**：不同文件并发修改可以工作，但同一文本文件的非重叠修改不会自动合并；删除/修改也没有统一冲突状态机。所谓 v2 “multi-writer” 实际是“按文件拒绝 + 客户端临时副本”，用户需要手工恢复，且冲突信息不持久化。

**建议**：明确产品语义：要么把 v2 定义为“冲突副本模式”，更新文档和 UI；要么实现带 base 内容的三方合并、二进制双版本、冲突解决 API 和可恢复状态。

当前需求选择第三种更简单的边界：禁用 v2 并发写入；仅在 lease takeover 时检查 dirty cache，交由用户丢弃或导出，不引入在线 merge。

### P1-3：v1 缓存迁移到 v2 时缺少文件 revision 基线

**证据**：v1 `Pull` 只写 `LastPushedRevision`；`LastEntries`/`FileRevisions` 不会被填充。v2 首次 `PushOperations` 因 `state.FileRevisions` 为空，会对已有文件生成空 base revision（`gui/cloud_workspace_sync.go:48-112`）。Hub 首次 operation 又会先 bootstrap 出 `legacy_*` revision（`hub/internal/cloudworkspace/operations.go:54-69`）。

**影响**：已有 v1 工作区切换到 v2 后，未修改文件也可能全部被判为冲突；当 legacy 文件的 `updated_seq=0` 时，冲突事件 `ConflictSeq` 为 0，GUI 无法定位正确的远端版本。

**建议**：首次启用 v2 时生成一次带 file revision 的 snapshot，并原子写入本地 `LastEntries/FileRevisions/LastEventSeq`；不允许用“空 base”冒充已同步基线。

在裁剪方案中不再做 v1→v2 runtime bootstrap；若历史上已有 v2 状态，只做离线校验后生成一次 manifest/snapshot，失败则标记 `RECOVERY_REQUIRED`。

### P1-4：sidecar flush 失败不会阻止 lease 释放

**证据**：`prepareCloudWorkspace` 和 `releaseCloudWorkspace` 在文件 Push 成功后调用 `flushCloudWorkspaceSidecars`；sidecar 出错时只记录日志，随后仍可能把 mount 视为成功并 DELETE lease（`gui/cloud_workspace_mount.go`）。

**影响**：文件已经切换到新 revision，但 task/session/workbench/checkpoint 仍是旧版本；下一台设备接手后出现“文件正确、任务上下文回退”。由于 lease 已释放，原设备也没有可靠的 pending 队列可以继续补写。

**建议**：把 sidecar flush 纳入释放的 commit barrier：任何一个 sidecar 未返回 `committed` 都必须保留 lease、停止新写入并进入 `RECOVERY`；重试成功后才允许释放。每个 sidecar 使用独立 revision/CAS，不能用“文件 Push 成功”代替整体任务提交成功。

### P1-5：写入响应没有统一的 durable idempotency 契约

**证据**：workspace 创建、manifest/sidecar 写入和对象 finalize 没有统一的 `Idempotency-Key + payload_hash + 原响应` 记录；网络超时后客户端只能再次发请求或依赖 `If-Match` 猜测上一次是否提交成功。

**影响**：用户重复点击或连接在提交后断开时，客户端无法区分“未提交”和“已提交但响应丢失”；可能重复创建 workspace/task、重复生成 snapshot，或错误地把一次成功写入报告为失败。

**建议**：服务端按 API/workspace/session 保存短期幂等记录；相同 key 重试返回原 committed 响应，不同 payload 明确返回 `IDEMPOTENCY_KEY_REUSED`。客户端 pending 队列以该 key 作为唯一重试句柄。

## 3. 中风险问题（P2）

### P2-1：分块 staging 可被用来绕过逻辑配额并耗尽磁盘

`PutObjectChunk` 对每个 chunk 只做单块大小和卷剩余空间检查，累计配额在 `CompleteObject` 才校验。攻击者可以为大量 hash 创建 staging 目录，未完成对象不计入工作区用量，直到 hourly GC 才清理（`hub/internal/cloudworkspace/sync.go:145-165`、`blob.go:407-430`）。

建议增加每用户/租户 staging 总量、hash 数量和并发上传上限；创建 staging reservation，超时主动回收；将 staging 字节纳入磁盘配额和指标。

### P2-2：sidecar 并发控制只在单个 Hub 进程内有效

`session.json` 通过全局 `sync.Mutex` 合并（`hub/internal/cloudworkspace/sidecar.go`），没有 ETag/版本条件写。多进程/多副本 Hub 会发生读-改-写丢更新；会话历史还会静默截断到 4000 条。

建议使用数据库版本号或 `If-Match` CAS，冲突时重试合并；对历史采用分页/增量日志，不要无提示丢弃旧消息。

### P2-3：删除的文件系统清理与数据库删除不是可恢复事务

`HardDeleteDeletedWorkspace` 先删除磁盘目录，再删除数据库记录；`RemoveWorkspace` 对多个 `RemoveAll` 错误直接忽略（`hub/internal/cloudworkspace/blob.go:151-164`）。进程崩溃或部分权限失败会留下“数据库存在但对象缺失”或“数据库已删但磁盘残留”。

建议引入 `deleting` 状态和可重入 GC job：先标记、分批删除、校验目录为空后再删元数据；所有清理错误必须保留并可重试。

### P2-4：API/测试契约与实现已经分叉

当前 `GetManifest` 明确跳过 lease 校验（`hub/internal/cloudworkspace/manifest.go:142-162`），但现有测试 `TestCloudWorkspaceManifestAndObjectLeaseRequired` 仍期望无 lease 返回失败；运行 `go test ./hub/internal/cloudworkspace ./hub/internal/httpapi ./gui -run 'Test.*CloudWorkspace|TestBlobStore'` 时该测试失败。

建议先冻结协议版本，分别维护 v1/v2 handler 测试矩阵，避免通过修改测试掩盖语义变化。

### P2-5：软删除后仍可创建分块 staging

**证据**：`PutObjectChunk` 和 `CompleteObject` 只调用 `GetOwned`，而 `GetOwned` 同时返回 active/deleted workspace；分块写入在 `CompleteObject` 前不会经过 `requireActiveOwned`（`hub/internal/cloudworkspace/sync.go`、`store.go`）。

**影响**：workspace 已软删除或正在 purge 时，仍可持续创建 `.part` 目录占用 Hub 磁盘；这些 staging 不会进入正常用量统计，且恢复/清理时没有明确归属。

**建议**：所有上传阶段（包括 chunk、complete、重试查询）统一要求 active workspace + writer session；软删除事务同时冻结并标记 staging，purge/GC 只清理已冻结且可审计的 reservation。

### P2-6：无关 workspace 被全局 prepare 锁串行阻塞

**证据**：GUI 使用全局 `cloudWorkspacePrepareMu` 包住 Pull/Push 和本地状态写入；一个大 workspace 的网络同步会阻塞其他 workspace 的打开和接手。

**影响**：单个大文件或 Hub 慢请求即可让所有云端任务表现为“卡住”，并增加 lease 过期、用户重复点击和重复 acquire 的概率。

**建议**：将锁降为 workspace 粒度的 actor；全局只保留短时 map/索引锁。不同 workspace 可以并行，单 workspace 内仍保持严格串行。

## 4. 改进后的目标架构（顺序多机协同）

本阶段把 v1 `manifest + exclusive lease` 作为唯一文件事实来源。多机协同通过“只读查看 + 写入交接”实现，不通过 Shared mount 或 v2 per-file operation 实现。

```text
设备 A（writer）                         设备 B（read-only）
watcher → workspace actor                entitlement/manifest/sidecar 读取
              │                                      │
              ├─ staging 上传对象                    │ 请求接手/等待
              ├─ lease + fencing 校验                 ▼
              ├─ manifest + usage/ref 事务       获取 lease 后重新 reconcile
              └─ sidecar CAS                         │
                         └──── Hub 唯一事实来源 ──────┘
```

### 4.1 产品模式和协议协商

| 模式 | 服务端事实来源 | 客户端行为 | 状态 |
| --- | --- | --- | --- |
| `v1-sequential` | manifest + lease + sidecar revision | 一台写入，其余只读；通过释放/接手切换 | 首发默认 |
| `v2-multi-writer` | files + events + snapshot | 文件级 CAS、冲突解决、并发写入 | 后续独立项目 |

`GET /entitlement` 应返回 `sync_protocol`、`server_revision`、当前 writer、lease 到期时间和 `read_only_allowed`。客户端收到 lease 409 只能进入 `READONLY/WAITING_HANDOFF`，不能设置 `Shared=true`、启动 watcher 或调用 v2 operations。协议模式必须由服务端明确下发，不能由客户端根据错误码猜测。GUI 应提供独立的 `OpenReadOnlyCloudWorkspace`（或等价 mode 参数），不要让 `PrepareCloudWorkspace` 在“写入失败”时隐式改变语义。

### 4.2 顺序写入的一致性不变量

一次成功的文件提交必须满足：

1. 请求持有当前 `workspace_id + client_instance_id + fencing_token`，且 lease 未过期；
2. 对象先完成 staging、校验明文 hash，再进入 `ready`；未完成对象对 manifest 不可见；
3. manifest entries、`used_bytes/file_count/manifest_revision` 与对象引用在同一数据库事务中更新；
4. 成功提交同时写入不可变 snapshot 根（至少保留最近版本），该根与 manifest revision 属于同一事务；
5. sidecar 写入带 `sidecar_revision`/CAS，并受同一 writer lease 保护；
6. 服务端返回已提交 revision 后，客户端才原子写本地 `state.json`；
7. 释放 lease 只能发生在 watcher 停止且所有已确认写入 flush 完成之后，并由 Hub 校验 `last_committed_revision`；
8. 旧 lease 或旧 fencing token 的迟到请求必须返回 `FENCED`，不能覆盖新 writer 的提交。

对象字节本身不与 SQLite 共用事务，但必须采用 `staging → ready → deleting` 状态机、fsync/校验和可重入 reconciliation，避免出现“DB 有记录但对象不存在”。当工作区很大时，可用受 lease 保护的 `manifest-delta` 批量提交减少全量 manifest 传输；该 API 最终仍只更新 manifest/snapshot 这一条事实链。

`manifest_hash` 必须由服务端按规范化后的 `(path, sha256, size)` 排序计算；客户端接手、snapshot restore 和备份恢复都要校验该 hash，不能只相信一个递增 revision。

### 4.3 Lease、交接和 takeover

建议把 lease 从“机器占用标记”升级为“可交接的写入会话”：

| 状态 | 进入条件 | 允许写入 |
| --- | --- | --- |
| `active` | acquire 成功并持续 heartbeat | 仅当前 session |
| `releasing` | 客户端开始停止 watcher/flush | 仅 flush 队列；新写入返回 `DRAINING` |
| `expired` | TTL 超时且未完成释放 | 禁止旧 session |
| `stolen` | 新 session takeover 成功 | 仅新 fencing token |

最小 API 语义：

- `POST /leases`：获取、续租或在明确用户确认后 takeover；请求显式携带 `mode=acquire|takeover`，返回 `lease_id`、`client_instance_id`、`fencing_token`、`expires_at`；
- `POST /leases/{id}/handoff-request`：记录接手请求并通知当前 writer（没有实时通道时由 entitlement 轮询发现）；
- `DELETE /leases/{id}`：幂等释放，携带 `last_committed_revision`；Hub 只有确认该 revision 已是 workspace 当前 revision 时才标记释放，flush 失败不得标记为成功；
- 所有 manifest、object finalize、sidecar、task binding 写入都携带 lease/session/fencing 信息并由服务端校验。

takeover 不能只依赖“收到 409 后强制重试”：新 writer 必须先确认旧 lease 已过期或用户明确批准，获取新 fencing token，再拉取远端最新 manifest。旧设备恢复联网后自动转为 read-only，并只能把本地脏缓存导出。

过期判断、TTL 和 fencing token 只以 Hub 的服务端时间和事务结果为准，客户端本地时钟不能决定“已过期”。对时钟漂移、网络分区和重复 takeover 要有明确的 grace period 与审计记录。

### 4.4 客户端同步状态机

```text
DISCOVER_READONLY
       │ 用户点击接手
       ▼
WAITING_HANDOFF ── lease 有效 ──► 继续只读
       │ lease 已释放/过期
       ▼
ACQUIRING(session + fencing)
       ▼
RECONCILING(manifest + sidecars + local state)
       │ 基线一致/用户确认脏缓存处理
       ▼
ACTIVE_WRITER ── 文件变化 ──► FLUSHING ──► ACTIVE_WRITER
       │ 关闭/交接
       ▼
RELEASING ── flush 失败 ──► RECOVERY（保留 lease/pending）
       │ flush 成功
       ▼
RELEASED/CLOSED
```

每个 workspace 必须有一个本地 actor/串行队列，统一处理 watcher、手动同步、sidecar、heartbeat、关闭和 takeover。Pull 写入期间抑制 watcher；状态文件保存 `workspace_id/protocol/client_instance_id/lease_id/fencing_token/base_revision/dirty/pending`，采用临时文件 + fsync + rename 原子更新。

### 4.5 只读设备与脏缓存语义

只读设备可以查看 manifest、sidecar、任务状态和当前 writer，但不能：

- 启动文件 watcher；
- 上传对象或修改 manifest；
- 写入 task/workbench/checkpoint/session sidecar；
- 运行会改变 workspace 的 Agent/coding loop。

发送消息、修改任务标题、运行 Agent、保存 checkpoint 都属于写入，必须先完成 handoff。只读工作树不能直接复用可能带有本地脏数据的 writer cache；应使用只读快照/临时目录，或明确标记“本地缓存版本”和“云端版本”不一致。

接手时按以下顺序处理本地缓存：

1. 无本地缓存：下载完整 manifest；
2. 本地 `dirty=false` 且 `base_revision` 落后：直接拉取新版本；
3. 本地 `dirty=true` 或基线不明：阻止自动写入，提供“丢弃并拉取 / 导出为新 workspace / 取消”；
4. 拉取完成并写入新 baseline 后，才启动 watcher。

### 4.6 MVP 数据模型

首发只需要补齐以下字段/表，不必先迁移 v2 events：

- `cloud_workspace_leases`：`client_instance_id`、`fencing_token`、`state`、`last_committed_revision`、`handoff_requested_at`；
- `cloud_workspace_task_bindings`：`workspace_id UNIQUE`、服务端稳定的 `cloud_task_id`、`device_task_id` 投影、`version`、`updated_at`；本地 task id 不能直接作为跨设备主键；
- `cloud_workspace_objects`：`object_state`、`plain_size_bytes`、`stored_size_bytes`、`compression`、`encryption_version`；
- `cloud_workspace_snapshots`：`snapshot_id`、`manifest_revision`、`manifest_hash`、`created_by_session`、`created_at`、`retention_class`；
- `cloud_workspace_sidecars`：每个 sidecar 的 `revision`、`payload_hash`、`updated_by_session`；
- `cloud_workspace_idempotency_keys`：`workspace_id`、`session_id`、`key`、`payload_hash`、最终响应和过期时间；响应丢失时可安全重放，复用同一 key 提交不同 payload 必须拒绝；
- 本地 state：`protocol/workspace_id/client_instance_id/base_revision/dirty/pending/last_flush_error`；watcher 自愈另外持久化 `reconcile_required/reconcile_reason/reconcile_at`，只有完整 scan + manifest commit 后清除。

`task.json` 必须至少包含 `workspace_id`、稳定的 `cloud_task_id` 和 `binding_version`；`Name/Mode/Tag` 只是可重建的 UI 投影。设备本地生成的 task row id 不得写入云端作为跨设备主键。

`cloud_workspace_events`、`workspace_seq`、`snapshot_seq` 和三方 merge 可作为未来 v2 的独立迁移，不应在 MVP 中与 manifest 双写。

### 4.7 未来 v2 的隔离原则

如果未来要支持并发编辑，必须另起协议版本并满足 files/events、manifest projection、workspace 内 cursor、冲突状态和 snapshot retention 的完整不变量。v2 客户端不能与 `v1-sequential` workspace 混用写入；灰度失败时只回滚到“单写者 + read-only”，不能尝试在运行时切换事实来源。

## 5. 建议实施顺序

1. **协议收敛**：关闭 `Shared` 自动降级和 v2 写入；把 `v1-sequential` 固定为默认能力，第二台设备进入只读/等待接手。
2. **身份与互斥**：增加 `client_instance_id`、session 和 fencing token；每个 workspace 建立 OS 进程锁及单线程同步 actor。
3. **文件提交可靠性**：修复压缩元数据 upsert；对象采用 `staging/ready/deleting` 状态；manifest、快照根、用量和引用在同一事务中更新；补 reconciliation 和可重入 GC。
4. **交接流程**：实现获取、等待、handoff-request、正常释放和 TTL takeover；A 释放前必须停止 watcher 并 flush，B 获取后必须重新 reconcile。
5. **任务与 sidecar**：增加服务端 workspace-task 唯一 binding；sidecar 写入使用 revision/CAS 且受 writer lease 保护；删除、恢复、撤权与任务 loop 使用统一状态机。
6. **运维与安全**：补备份/恢复演练、密钥轮换、路径 collision 检查、staging/磁盘配额、快照保留/恢复、审计和指标。
7. **未来并发（可选）**：顺序模式稳定后，再单独设计 v2 multi-writer；不得在运行时把同一 workspace 从 manifest 事实来源切换为 files/events。

**回滚条件**：出现对象丢失、旧 session 写入成功、接手后基线不一致、flush 已确认但 revision 丢失、binding 重复或 pending 无法收敛时，自动回到 `READONLY`/`v1-sequential`，保留本地缓存和服务端对象供修复，不能直接删除数据。

## 6. 必补测试清单

### 6.1 顺序多机 MVP（发布前必须通过）

- A 正常释放后，B 获取 lease，文件树、sidecar、任务标题与 A 最后一次提交完全一致。
- A 进程崩溃，B 在 TTL 后 takeover；A 恢复联网后的迟到 Push、sidecar 和 heartbeat 均被 fencing 拒绝。
- 第二台设备处于 read-only 时，watcher、手动保存、Agent 和关闭流程都不会产生写请求。
- 同一机器启动两个 GUI 时，第二个进程无法获得同一 workspace 的本地写锁。
- 接手时本地 `dirty=false` 可增量拉取；`dirty=true` 或基线不明时必须阻止自动覆盖，并能丢弃、导出或取消。
- manifest PUT 成功但 lease release 超时，重试不会重复写，也不会丢失已确认 revision。
- `manifest-delta` 在 If-Match/fencing 下原子应用 put/delete；与全量 manifest 提交产生完全相同的 revision、快照、用量和引用结果。
- manifest/sidecar 响应丢失后使用相同 `Idempotency-Key` 重试只返回原 committed 响应；同 key 不同 payload 被拒绝。
- 文件 Push 成功但任一 sidecar flush 失败时，lease 不释放；修复后重试能补齐 sidecar，再完成交接。
- 每次 manifest 提交都有不可变 snapshot 根；从任一保留 snapshot 恢复会生成新 revision，历史版本不被修改。
- 快照、导出分支和 staging 计入物理保留配额；超过上限时按明确策略拒绝新版本或淘汰最旧版本，不能静默耗尽磁盘。
- 可压缩大文件上传→Hub 重启→下载，明文 hash、压缩 metadata 和 stored size 一致。
- 对象文件、对象 DB 行、manifest、used_bytes/ref_count 在随机崩溃后可巡检并自动修复。
- 多 Hub 进程并发更新 sidecar 时，CAS 冲突可重试且不会丢历史；clear/append 顺序可解释。
- workspace 删除、恢复、撤权期间，任务 loop 和 watcher 按状态机停止；恢复后必须重新授权。
- Windows 大小写/保留名、Unicode 规范化、长路径 collision 在上传前被拒绝并提示。
- 创建/恢复/删除重试只产生一个 workspace-task binding，不留下孤儿 workspace。
- workspace 软删除或 purge 期间，chunk/complete/upload retry 全部被拒绝且已有 staging 可回收。
- 两个不同 workspace 可并行 prepare；同一 workspace 的命令仍严格按队列顺序执行。

### 6.2 未来 v2（不阻塞 MVP）

- v2 put/delete 后 files、events、manifest projection、used_bytes、ref_count 五者一致；
- 事件 retention、snapshot、cursor 过期和冲突解决 API；
- 同一文本非重叠修改的三方合并、二进制冲突和删除-修改冲突。

## 7. 设计验收门槛

以下条件全部满足，才可把 `v1-sequential` 标记为可用：

- **交接正确性**：正常释放、TTL takeover、重复 acquire/release、Hub 重启后，任意时刻最多一个有效 writer；
- **fencing 正确性**：旧 machine/session/token 的所有写请求均失败，且失败不会改变 manifest、sidecar 或用量；
- **提交完整性**：文件、sidecar 和 task binding 任一 flush 失败都不会释放 lease；交接只在全部返回 `committed` 后完成；
- **幂等性**：所有写请求在响应丢失、重试和重复点击下只产生一次 revision/快照/用量变化；payload 不一致的 key 会被拒绝；
- **数据一致性**：对象为 `ready` 后才可被 manifest 引用；manifest、用量、引用、sidecar revision 可巡检并自动修复；
- **增量等价性**：`manifest-delta` 与全量 manifest 对同一输入产生相同最终树、revision、snapshot 和配额结果；不存在第二份文件事实来源；
- **可恢复性**：每次已确认提交都有可校验的 snapshot 根；误删、误覆盖可恢复为新 revision，GC 不会删除保留版本所需对象；
- **配额一致性**：当前树、保留快照、导出分支和 staging 的物理占用可解释、可限流，不会绕过 tenant/workspace 磁盘上限；
- **本地可靠性**：Pull/Push/Flush/Release/watcher 串行，进程崩溃或网络抖动不会丢失已确认 revision；
- **只读隔离**：read-only 设备不能上传对象、写 manifest、写 sidecar 或运行改变 workspace 的任务；
- **任务一致性**：一个 workspace 只有一个服务端 binding，创建/恢复/删除具有幂等结果；
- **安全与运维**：跨租户、过期 session、路径 collision、超额 staging 被拒绝并审计；支持备份恢复和单 workspace 修复；
- **用户可解释**：当前 writer、最后提交时间、接手等待、脏缓存处理和失败原因均在 UI 可见。

在此之后，产品文案可使用“跨设备接续同一云端任务（单写者）”；仍不应使用“多人同时编辑同一工作区”。

## 8. 进一步的根本性缺陷

前述 P0/P1 主要是“当前实现会出错”的问题；下面这些问题更接近系统边界和信任模型。一旦产品要承诺“云端长期保存、跨设备持续运行、多机同时编辑”，它们不能靠补几个重试或 UI 提示解决，必须改变数据模型、协议或部署形态。

### 8.1 根本缺陷优先级

| 级别 | 根本问题 | 不解决的结果 |
| --- | --- | --- |
| **R0** | 双协议事实来源、对象/DB 非原子提交、workspace/task 无服务端唯一绑定、lease 不是 session/process fencing、`client_instance_id` 未绑定认证主体、写入响应不可安全重放 | 数据可能永久分叉或丢失；两个客户端互相覆盖；无法证明一次写入到底属于哪个会话 |
| **R1** | 路径 canonicalization 不完整、事件/manifest 无 snapshot 保留边界、staging/GC 无全局预算、快照用量未纳入配额、sidecar 不是可并发日志、创建/删除无补偿状态机 | 特定平台或长期运行后出现不可重放、不可恢复、不可诊断的问题 |
| **R2** | Hub 本地盘/SQLite 单点、运行环境未建模、本地缓存安全边界、撤权与大工作区体验不足 | “云端”承诺不成立，灾备、换机执行结果和敏感数据处理不可控 |

上线多写入前，R0 必须全部关闭；R1 至少要有可观测的失败模式和人工修复工具。

### 8.2 身份、绑定与信任边界

#### R0-1：machine_id 不是客户端实例，fencing 粒度过粗

**现状/证据**：`deriveMachineID`（`hub/internal/auth/identity_service.go`）由 user+clientID 稳定派生；lease 表只保存 `machine_id`。同一台机器上的两个 GUI 进程、旧进程残留和新进程会被视为同一个持有者。

**风险**：本地两个进程可以同时操作同一缓存；旧进程在新进程获得“同机” lease 后仍可能继续 Push。Hub 无法区分“同一机器的合法续租”与“已经失效的进程”。

**目标设计**：每次启动生成不可预测的 `client_instance_id`，lease 绑定 `workspace_id + client_instance_id`，所有写入携带单调递增的 fencing token；本地 cache 以 workspace 为粒度加 OS 级进程锁。机器级身份只用于设备授权和审计，不用于并发互斥。

#### R0-2：v2 的 client_instance_id 是可伪造字段

**现状/证据**：`Operation.ClientInstanceID` 从请求 JSON 直接写入 `cloud_workspace_events`；handler 没有把它与 bearer token、session 或已注册实例做绑定。

**风险**：任意已授权客户端可以伪造另一个实例的 ID，使其他客户端把该事件当作“自己产生”而跳过；也无法可靠追踪写入来源、吊销单个实例或判断旧实例是否仍在线。

**目标设计**：认证层签发短期 session（含 `client_instance_id`、协议版本和过期时间），服务端从 token 取得实例 ID，忽略/拒绝 body 中不一致的值；事件记录 `session_id`、`machine_id`、`user_id`，撤销 session 后拒绝新 operation。

#### R0-3：workspace 与 task 的 1:1 关系没有服务端约束

**现状/证据**：GUI 主要依赖本地 `cloudWorkspaceTaskByID`、标签和 sidecar；`CreateCloudWorkspace` 与 `CreateTaskWithCloudWorkspace` 是两个独立调用。Hub 没有 `workspace_id UNIQUE` 的 durable binding 表。

**风险**：两台设备或两个进程都可能发现“没有任务”并各自创建本地任务，随后竞争写 `task.json`；恢复、删除、重命名时无法判断哪个 task 是权威记录。

**目标设计**：增加 `cloud_workspace_task_bindings(workspace_id UNIQUE, task_id, version, updated_at)`，由服务端以 CAS/idempotency 创建或恢复；本地任务只是该绑定的缓存投影。绑定变更、解绑和迁移都写审计事件。

#### R0-4：创建流程不是跨 Hub 与本地任务的原子流程

**现状/证据**：前端先 POST 创建 active workspace，再准备本地 cache、申请 lease、创建 task；任一步失败都可能留下 active 但无人绑定的 workspace。删除也同时涉及 Hub、缓存目录和本地任务，缺少统一事务号。

**风险**：孤儿 workspace 长期占 quota；重试可能产生重复任务或重复 sidecar；失败发生在“已扣配额、未落本地记录”之间时无法自动判断应重试还是回滚。

**目标设计**：提供幂等的 `CreateWorkspaceTask` 编排接口，或把流程显式建模为 `provisioning → active → deleting/failed`；配套补偿 job、幂等键和可查询的 operation status。客户端不再把“HTTP 201”当成整个业务已完成。

### 8.3 并发、事务与本地一致性

#### R0-5：文件、对象、事件、用量仍不是同一事实提交

**现状/证据**：`PutObject/PutObjectChunk` 先写文件并单独写对象行，`ApplyOperation` 再写 files/events；压缩元数据依赖后续 `recordObjectMeta`。进程崩溃可落在任意中间状态。

**风险**：DB 有对象记录但磁盘无文件、磁盘有文件但 DB 无记录、files 指向已删除对象、used_bytes 与实际对象不一致。仅靠启动时扫描无法区分“尚未完成上传”与“已确认提交”。

**目标设计**：对象使用 `staging → ready → deleting` 状态；先写临时文件、fsync/校验，再在事务中 finalize metadata 与引用，提交后才对 operation 可见。启动和 GC 都执行 reconciliation，并把无法自动修复的项置为 quarantine，而不是静默删除。

#### R0-6：本地同步没有真正的串行化边界

**现状/证据**：`cloudWorkspaceHeldMount.mu` 只保护 mount 字段；watcher debounce、手动同步、关闭 release、sidecar flush 可以并行执行。Pull 落盘期间没有统一抑制 watcher 的机制。

**风险**：Pull 写文件触发新的 Push；release 与 watcher 同时提交；旧任务把 `state.json` 写回，覆盖新 cursor、pending operation 或 conflict 状态，重启后出现重复上传或漏事件。

**目标设计**：每 workspace 一个 actor/单线程队列，所有 Pull、Push、Flush、Release 和状态写入都作为串行命令；批量 Pull 期间设置 `suppress_watch`，状态采用 generation/CAS，任何旧命令不能覆盖新 generation。

#### R1-1：路径身份没有跨平台 canonicalization

**现状/证据**：manifest 和 `cloudWorkspaceSafeRelPath` 以大小写敏感的字符串比较；Windows 本地文件系统大小写不敏感，macOS 还存在 Unicode NFC/NFD 差异；没有检测 Windows 保留名、路径长度和大小写碰撞。

**风险**：远端同时存在 `README.md` 与 `readme.md` 时，落地到 Windows 会互相覆盖；不同 Unicode 形式在不同设备上被当成不同文件；Pull 后无法构造与 manifest 一致的工作树。

**目标设计**：服务端定义唯一 `path_key`（分隔符、Unicode 规范化、大小写折叠和平台保留规则），写入时拒绝同一 workspace 内 collision；客户端在挂载前报告不可落地路径，而不是静默覆盖。

### 8.4 事件、冲突与长期会话

#### R1-2：事件日志没有 retention、snapshot 和 cursor 失效协议

**现状/证据**：API 只有 `GET /events?after_seq`，没有 snapshot endpoint、保留窗口、compact boundary 或 `cursor too old` 错误；`seq` 还是跨 workspace 的全局自增值。

**风险**：事件无限增长；未来为控成本删除旧事件时，落后客户端可能静默漏变更；跨 workspace 的稀疏 seq 让运维难以判断单 workspace 是否真正追平。

**目标设计**：使用 workspace 内 `workspace_seq`，维护 `snapshot_seq` 和 `min_retained_seq`；游标早于保留边界必须返回 `CURSOR_EXPIRED`，客户端强制下载并校验快照 hash 后再继续追事件。快照生成和 compact 要有 checkpoint 与审计记录。

#### R1-3：冲突副本路径不稳定，可能被再次上传

**现状/证据**：GUI 用时间戳生成 `path.conflict-<timestamp>`；没有 server-side `conflict_id/status`，也没有保证该路径被 watcher 忽略。

**风险**：同一纳秒窗口或重试可能产生难以关联的多个副本；副本被 watcher 当普通文件扫描后再次上传，形成冲突递归；用户无法知道某个副本是否已解决。

**目标设计**：服务端生成稳定 `conflict_id`，客户端统一写入 `.maclaw-conflicts/<path>.<conflict_id>` 并强制 ignore；冲突记录保存 base/local/remote 引用、创建者和状态，`resolve-conflict` 是新的幂等 operation。

#### R1-4：sidecar 是整份 snapshot，不适合并发和长期历史

**现状/证据**：`session.json` 通过 Hub 进程内 mutex 做读-改-写，按 raw JSON hash 去重，并在 4000 条后直接截断；没有 revision/CAS、分页事件或跨副本顺序。

**风险**：多 Hub 进程仍会丢更新；两个客户端同时 clear/append 的顺序不可验证；长对话被静默删掉，且每次写都需要重加密整份 JSON，延迟和 IO 随历史线性增长。

**目标设计**：conversation 使用 append-only event log（带 monotonic revision、author、timestamp），定期生成 snapshot；写入携带 `If-Match`/CAS，读取支持分页和增量，clear 是带 revision 的 tombstone。

### 8.5 平台可靠性与生命周期

#### R1-5：staging、对象和事件缺少统一回收预算

**现状/证据**：分块 staging 仅按单 chunk/卷剩余空间检查，未完成 hash 不计入 workspace/tenant 用量；对象 GC 依赖 ref_count，事件和冲突副本也没有共同的保留策略。

**风险**：少量请求即可制造大量 `.part` 目录耗尽磁盘；即使文件逻辑配额正常，物理盘也会被 staging、冲突、快照和 sidecar 吃满；GC 可能在客户端仍需重放时删除数据。

**目标设计**：按 user/tenant/workspace 建立 staging reservation、并发数、对象数和总字节上限；reservation 过期可重入回收，物理盘配额纳入 admission；GC 只处理 `ready` 且不被当前文件、快照、冲突或 pending operation 引用的对象。

#### R1-6：软删、硬删与运行中任务没有统一状态机

**现状/证据**：workspace 删除、缓存删除、任务隐藏/归档、Agent/coding loop 停止是多个独立动作；撤权或删除后本地任务仍可能保留 cloud tag 和 watcher。

**风险**：正在运行的 loop 继续写入已删除 workspace；恢复期间旧缓存又被上传；权限撤销后 UI 仍显示可编辑，失败只在后台日志中出现。

**目标设计**：服务端发出 `active/deleting/deleted/revoked` 状态事件；客户端进入 `READONLY → DRAINING → CLOSED`，先取消任务 loop、停止 watcher、处理 pending，再删除或保留加密缓存。恢复必须重新授权并验证快照版本。

#### R1-7：只有当前 manifest，没有可恢复的版本快照

**现状/证据**：`manifest_revision` 只标识当前树；旧 manifest 行会被整体替换，旧对象在 `ref_count=0` 后可被 GC。顺序交接虽然避免了并发冲突，却把一次误操作直接变成“当前真相”，没有服务端 undo/rollback 边界。

**风险**：设备 A 释放后，设备 B 误删或批量覆盖文件，用户无法恢复到 A 的最后一次已知版本；对象内容可能仍在磁盘上，但没有可靠的 manifest 根指针，无法证明哪些对象属于哪个版本。

**目标设计**：每次成功 manifest 提交先生成不可变 `cloud_workspace_snapshots(snapshot_id, revision, manifest_hash, created_by, created_at)`，至少保留最近 N 个版本或按时间保留；GC 将快照根、当前 manifest、pending/export 分支都视为引用。提供只读版本浏览和 `restore-snapshot`（生成新 revision，不原地改历史）。

#### R1-8：快照保留会绕过现有 used_bytes 配额

**现状/证据**：当前 `used_bytes` 只统计 manifest 当前树；一旦保留历史 snapshot，旧对象即使仍被快照引用也不在该字段中，tenant quota 与物理磁盘占用会分叉。

**风险**：用户看见“未超配额”，Hub 磁盘却被历史版本、导出分支和冲突对象耗尽；为修复磁盘而提前 GC 又会破坏承诺的回滚能力。

**目标设计**：同时维护逻辑工作区用量与物理保留用量（当前树 + snapshots + staging）；明确快照是否计费、每 workspace/tenant 的保留上限和超限策略。配额检查、entitlement 展示和 GC 必须使用同一套保留根计算。

#### R2-1：Hub 本地 SQLite + 本地 blob 是单点，不是可扩展的云端存储

**现状/证据**：对象文件与 master key 位于 Hub 本地目录，元数据在 SQLite；没有对象存储接口、跨副本锁、备份校验或 RPO/RTO 定义。

**风险**：磁盘损坏、主机迁移或多副本部署会同时影响所有 workspace；复制 SQLite 文件或 blob 目录不能保证一致快照，master.key 丢失则全部数据不可解密。

**目标设计**：短期明确产品是“Hub 本地持久化”并提供备份/恢复演练；中期抽象 `ObjectStore`、元数据主库和 KMS/secret manager，记录 key version，支持轮换迁移、跨区备份和启动时可解密性检查。

#### R2-2：文件同步没有执行环境语义

**现状/证据**：跨设备只同步文件；`.git/node_modules/venv/build` 等通常不传输，编译器、依赖、环境变量和 OS/arch 由本机决定。

**风险**：同一任务在两台机器上文件树相同但编译、测试、Agent 行为不同；用户会把环境差异误判为同步丢失或冲突。

**目标设计**：保存 toolchain/runtime profile、lockfile hash、OS/arch 与关键环境变量声明；启动时执行环境诊断并显示差异。需要可复现执行时，提供容器或远程执行后端，而不是继续扩大文件同步协议。

#### R2-3：本地缓存与 Hub 信任边界没有明确

**现状/证据**：云端明文文件会落到普通本地目录；Hub 端是“持有主密钥即可解密”的服务端加密，不是端到端加密；下载、缓存删除和审计策略未定义。

**风险**：同机其他用户、备份软件或恶意进程可读取缓存；Hub 管理员或备份副本可看到明文；撤权后旧缓存仍可能长期存在。

**目标设计**：先写威胁模型并明确“Hub-trust 还是 E2E”；本地 cache 可选 DPAPI/Keychain/credential vault 加密，敏感路径分级，记录下载与删除审计，撤权后按策略擦除本地密钥而非只隐藏任务。

#### R2-4：大工作区的算法和背压边界不足

**现状/证据**：扫描/hash 以整棵工作树为主，单次 manifest 上限 20,000 entries、单对象 64 MiB；watcher 对编辑器原子替换和批量生成只能 debounce 后重新扫描。

**风险**：大仓库每次保存都产生高 CPU/IO 和大量 HTTP；watcher 队列溢出后无法知道遗漏哪些路径；没有进度、取消和带宽公平策略。

**目标设计**：本地维护 mtime/size/file-id/hash index，批量提交 operation，事件和对象上传有并发/带宽配额与背压；watcher 丢失或重启时显式进入 `RECONCILE_REQUIRED`，向用户展示进度和可取消状态。

### 8.6 由这些缺陷推导出的产品边界

在 session fencing、服务端 binding、对象状态机和顺序交接闭环之前，产品只能承诺“单设备本地使用”。完成这些 R0 基础后，可以承诺“同一用户跨设备接续（单写者、Hub-trust 加密）”；只有进一步完成 files/events、snapshot、cursor retention 和可恢复冲突模型，才有资格描述为“多机并发编辑”。灾备、E2E、可复现执行环境则决定能否进一步称为“云端持续工作区”，它们不是同步 API 的附属功能。

## 9. 推荐落地目标：顺序多机协同（非并发编辑）

如果业务需要的是“在电脑 A 上开始、在电脑 B 上继续”，而不是两台设备同时改同一份文件，没必要先承担 v2 多写入的复杂度。可以把 workspace 定义为一个跨设备共享的持久化任务，把“协同”限定为接续、查看和交接。

### 9.1 产品契约

| 场景 | 允许行为 | 服务端语义 |
| --- | --- | --- |
| 设备 A 已打开 | 读写文件、更新 sidecar、运行 Agent | A 持有唯一 writer lease |
| 设备 B 查看 | 浏览 manifest、读取 sidecar、查看任务状态 | B 为 read-only，不产生写请求 |
| B 请求接手 | 显示当前持有者并等待释放 | 不自动绕过 lease，不进入 Shared |
| A 正常关闭 | 停止 watcher → flush 文件/sidecar → 释放 lease | flush 成功后才允许下一个 writer |
| A 崩溃/断网 | 等待 TTL，之后由 B 执行 takeover | 新 fencing token 使 A 的迟到写入全部失败 |
| B 接手时本地有脏数据 | 提示丢弃、导出为新 workspace 或人工导入 | 不做隐式覆盖，不把脏缓存当成最新版本 |

这里的“多机”是多设备访问同一个云端事实来源；“单写者”是并发控制策略，不应在客户端表现为“共享挂载”。

### 9.2 最小协议与状态机

```text
DISCOVER(read-only)
      │ 用户点击“接手”
      ▼
WAITING_HANDOFF ── lease 仍有效 ──┐
      │ lease 已释放/过期           │
      ▼                             │
ACQUIRING(client_instance_id, fencing_token)
      │
      ▼
RECONCILING(manifest + sidecars + local state)
      │ 成功
      ▼
ACTIVE_WRITER ── 文件变化 ──► FLUSHING ──► ACTIVE_WRITER
      │                              │
      └─ 关闭/交接 ──────────────────┘
                     ▼
                  RELEASED
```

实现上只需要把现有 v1 lease/manifest 流程收敛清楚：

1. `GET entitlement/manifest/sidecar` 可以无 lease 读取，但所有写入（对象 finalize、manifest、sidecar、任务绑定）必须携带有效 `session_id + fencing_token`。
2. 客户端收到 lease 409 只进入 `READONLY/WAITING_HANDOFF`，不能设置 `Shared=true`，也不能启动 watcher 或 v2 operations。
3. 获取 lease 后先拉取最新 manifest 和 sidecar，再比较本地 `base_revision`；只有基线一致或用户明确选择处理脏缓存，才能进入 `ACTIVE_WRITER`。
4. 释放顺序固定为“停止 watcher → 串行 flush → 原子写本地 state → DELETE lease”。flush 失败时保留 mount 和 pending 状态，不能释放一个仍有未确认写入的 lease。
5. lease 记录增加 `client_instance_id`、单调递增 `fencing_token` 和 `last_committed_revision`；所有 v1 PUT 在服务端校验 token，旧实例即使网络延迟到达也只能得到 `FENCED`。

### 9.3 可以暂缓的 v2 能力

在顺序多机协同模式下，以下能力不是首批必需，可以先关闭或仅 shadow 记录：

- `cloud_workspace_files/events` 的多写入事实来源；
- 同一文本文件的三方自动合并；
- 删除-修改的在线冲突合并；
- workspace 内事件游标和长历史 replay（manifest revision 足以支撑交接）。

但对象 `staging/ready` 状态、sidecar CAS、服务端 task binding、进程级 fencing、备份恢复仍然是必需的可靠性基础，不能因为“不做并发编辑”而省略。

### 9.4 顺序交接的实施顺序

1. **协议收敛**：关闭 `Shared` 自动降级和 v2 写入；v1 manifest 作为唯一文件事实来源。
2. **身份与本地互斥**：引入 `client_instance_id`/fencing token；每个 workspace 建立 OS 进程锁和单线程同步队列。
3. **交接体验**：增加“当前设备/最后提交时间/接手”状态；第二台设备默认只读，支持等待、通知和超时 takeover。
4. **任务绑定与编排**：增加服务端 `workspace_id UNIQUE` binding；创建、恢复、删除使用幂等 operation 和补偿状态机。
5. **可靠性收尾**：对象状态机与 reconciliation、sidecar CAS、撤权/删除时停止任务 loop、Hub 备份恢复演练。
6. **再评估并发**：只有顺序交接在崩溃、网络抖动、重复请求、跨平台路径和大文件场景稳定后，才单独立项 v2 multi-writer。

### 9.5 必补验收用例

- A 正常释放后，B 获取 lease，文件树、sidecar、任务标题与 A 最后一次提交完全一致；
- A 进程崩溃，B 在 TTL 后 takeover，A 的延迟 Push 被 fencing 拒绝；
- 同一机器启动两个 GUI 时，第二个进程无法获得同一 workspace 的本地写锁；
- B 处于 read-only 时，watcher、手动保存和关闭流程都不会产生写 operation；
- 接手时检测到本地 dirty cache，用户选择“丢弃/导出/取消”后结果可解释且可恢复；
- manifest PUT 成功但 lease release 超时，重试不会重复写、不会丢失已确认版本；
- Hub 重启或磁盘恢复后，workspace/task binding、对象元数据和 sidecar 均可重新校验。

采用这个目标后，产品可以明确宣传为“跨设备接续同一云端任务”，而不是“多人同时编辑同一工作区”。

## 10. 实现前必须冻结的协议契约

前面的设计已经选定“顺序多机协同”，但仍有几项如果不写成契约，开发时很容易重新滑回 Shared/v2 混用。以下内容应在实现前冻结并转成接口测试。

### 10.1 范围与非目标

- 支持同一用户在多台已授权设备之间接续同一个 workspace/task；允许并发读取，不允许并发写入。
- 不支持不同用户共享写入，不支持离线后继续自动合并，不支持同一文件的在线三方 merge。
- 设备失联后，原设备可以继续本地工作，但 lease 过期后不得自动上传；恢复联网只能进入只读或导出分支。
- Agent、消息发送、任务重命名、workbench/checkpoint 更新都算写入，必须持有 writer lease。

### 10.2 API 契约（MVP）

以下路径省略统一前缀 `/api/v1/cloud-workspaces/{workspace_id}`；字段名和响应语义需要直接转成 Hub/GUI 契约测试。

| API | lease 要求 | 成功语义 | 幂等键/条件 |
| --- | --- | --- | --- |
| `GET /entitlement`、`GET /manifest`、`GET /sidecar` | 无 | 返回当前 server revision、writer 和只读数据；manifest 附带 `manifest_hash` | 无 |
| `POST /leases` | 无；同一 session 重试返回原 lease | 返回 `lease_id + fencing_token + expires_at` | `Idempotency-Key`；请求显式声明 `mode=acquire|takeover`，不再用裸 `force=true` |
| `POST /leases/{id}/handoff-request` | 已认证即可 | 记录一次接手请求并通知 writer | `(workspace_id, requester_session, open)` 唯一 |
| `PUT /objects/{sha}`、`POST /objects/{sha}/complete` | writer session；complete 再次校验 token | 对象进入 `ready`，未 ready 不可被 manifest 引用 | SHA + upload id |
| `PUT /manifest` | 当前 lease + fencing token + `If-Match` | DB durable commit 后返回新 revision 和 snapshot_id | `Idempotency-Key + If-Match`；同 key 重试返回原响应 |
| `POST /manifest-delta`（可选优化） | 当前 lease + fencing token + `If-Match` | 在同一事务中应用 put/delete 列表并返回新 revision/snapshot_id | `Idempotency-Key + If-Match`；不得产生独立事实来源 |
| `PUT /sidecars/{name}` | 当前 lease + sidecar `If-Match` | CAS 成功后返回 sidecar revision | `Idempotency-Key + If-Match` |
| `DELETE /leases/{id}` | 当前 session/token | 校验 `last_committed_revision` 后幂等释放 | lease id + fencing token + last revision |
| `POST /snapshots/{id}/restore` | 当前 lease | 以新 manifest revision 恢复，不修改历史 snapshot | `Idempotency-Key` |
| `POST /cloud-workspace-tasks` | 无（创建远端 provisioning 记录） | 在同一事务创建 workspace + 唯一 task binding，返回 `operation_id` | `Idempotency-Key + payload_hash` |
| `GET /cloud-workspace-tasks/{operation_id}` | 无 | 查询 `provisioning/active/failed`，用于超时恢复 | operation id |
| `POST /cloud-workspace-tasks/{operation_id}/complete` | 创建者会话（若已有 lease 则校验 fencing） | 本地 task/sidecar 已提交后将 operation 置为 `active` | `Idempotency-Key` |
| `POST /cloud-workspace-tasks/{operation_id}/abort` | 创建者会话（若已有 lease 则校验 fencing） | 补偿删除 workspace、解绑 task，operation 置为 `failed` | `Idempotency-Key + reason` |

所有写 API 都必须在响应中区分 `committed` 与 `accepted/queued`；只有 `committed=true` 才允许客户端推进本地 baseline。服务端应保证返回 committed 后，SQLite 事务已提交且对象/sidecar 文件已完成必要的 fsync。

`Idempotency-Key` 必须绑定请求 payload hash、workspace、session 和 API 名称；网络超时后的重试先查询原响应，不能因为 `If-Match` 仍相同而再次生成 snapshot 或重复扣用量。

编排期间 workspace 行保持 `active` 以便 GUI 先申请 writer lease 并写入本地 sidecar；`operation.state=provisioning` 才是“本地任务尚未确认”的权威标记。超时补偿只处理无有效 lease 的 provisioning operation，避免回收正在接手的工作区。

### 10.3 错误码到客户端状态的固定映射

| 错误码 | 客户端动作 | 是否自动重试 |
| --- | --- | --- |
| `LEASE_BUSY` | `READONLY/WAITING_HANDOFF`，展示当前 writer | 仅轮询状态，不重试写入 |
| `FENCED` | 立即停止 watcher 和所有写请求，进入 `READONLY` | 不自动重试原请求 |
| `REVISION_CONFLICT` | 重新拉 manifest；若本地 dirty，进入人工处理 | 不覆盖本地文件 |
| `FLUSH_REQUIRED` | 保留 lease，继续串行 flush；失败则进入 `RECOVERY` | 仅重试 flush，不释放 lease |
| `READ_ONLY` / `REVOKED` | 禁止写入，停止 Agent loop | 重新授权后由用户触发 |
| `DRAINING` | 停止新变更，只等待当前 flush 完成 | 不重试新写入 |
| `OBJECT_STAGING` / `INCOMPLETE` | 保留 pending，查询上传状态或重新上传 | 有上限的退避重试 |
| `SNAPSHOT_NOT_FOUND` | 刷新 entitlement，提示版本已被清理 | 不循环重试 |
| `PROVISIONING` / `DELETING` | 禁止创建新 task 或新写入 | 等待状态事件 |
| `PROTOCOL_MISMATCH` | 按服务端返回的能力降级到只读或提示升级 | 不重试原写请求 |
| `IDEMPOTENCY_KEY_REUSED` | 标记请求错误，生成新 key 前先拉取当前 revision | 不重试原 payload |

错误码、HTTP status 和 UI 文案要在 Hub/GUI 共享的契约测试中固定，不能继续依赖字符串匹配。

### 10.4 数据库迁移与兼容策略

1. 先增加 nullable 的 session/fencing/snapshot/binding 字段和表，再发布只读代码；旧行按 `legacy` 标记，不伪造新的 session。
2. 第二阶段发布 `v1-sequential` 客户端：缺少 `sync_protocol` 的旧 Hub 只能进入安全只读模式；客户端永远不因 409 自行启用 Shared。
3. 第三阶段切换写入：manifest/sidecar 只写新路径，禁止 v1/v2 双写；v2 表只保留 shadow 或历史数据，不参与 GC/配额事实来源。
4. 所有迁移脚本可重复执行，binding 和 snapshot 有唯一约束；回滚只回滚客户端能力开关，不回滚已提交的 manifest revision 或删除对象。

兼容矩阵必须明确：

| 客户端 | Hub | 结果 |
| --- | --- | --- |
| 新 `v1-sequential` | 新 Hub | 正常读写/交接 |
| 新 `v1-sequential` | 旧 Hub（无 protocol/fencing 字段） | 只读，提示升级；不得猜测为 Shared |
| 旧客户端 | 新 Hub | 仅允许带 lease 的 v1 manifest API；v2 operations/Shared 请求被拒绝或强制只读 |
| v2 客户端 | `v1-sequential` workspace | 明确 `PROTOCOL_MISMATCH`，不能双写 |

建议所有写请求带 `X-Cloud-Workspace-Protocol` 和 `X-Cloud-Workspace-Session`；Hub 按 workspace 能力开关校验，避免旧客户端绕过 entitlement 直接调用新接口。

### 10.5 可观测性和运维门槛

至少需要按 workspace 记录：当前 writer/session、lease 剩余时间、handoff 等待时长、takeover 次数、fenced 请求数、flush p50/p95、dirty-cache 拒绝数、snapshot 数/最旧可恢复版本、staging 字节、孤儿对象数、reconciliation 队列长度和最后一次错误。

每次 acquire、handoff、fence、manifest commit、sidecar commit、snapshot restore、revoke 和 purge 都写不可变审计事件。SLO（例如接手时延、已确认提交丢失率、恢复时间）必须由产品明确目标后再配置，不能把 90 秒 TTL 当成可靠性指标。

### 10.6 仍需产品确认的三个选择

- **离线策略（推荐）**：默认“lease 过期即停止自动上传”，并提供导出为新 workspace；若允许离线继续，必须把修改写入独立分支，不能默认为静默覆盖。
- **快照保留**：最近 N 个版本、按天保留，还是按 workspace 配额计费；GC 必须以该策略为根引用。
- **只读体验**：只读设备使用远端临时快照目录，还是下载到本地缓存后加 OS ACL；两者都要明确磁盘上限和清理时机。

## 11. v1/v2 合并与裁剪决策

### 11.1 结论先行

对于当前需求，不应继续维护“v1 single-writer + v2 multi-writer”两套可写协议。最终运行时只保留一个 `v1-sequential`：

```text
manifest（当前文件树）
   + exclusive writer lease（单写者）
   + client session/fencing（防旧进程迟到写入）
   + snapshots（版本恢复）
   + sidecar CAS（任务上下文一致）
   + idempotency（超时可安全重放）
```

v2 中只有与“可靠的顺序交接”直接相关的能力进入主干；并发编辑专属的数据模型和 API 不进入首发产品。

### 11.2 v1 与 v2 对比

| 维度 | v1 当前实现 | v2 当前实现/设想 | 本需求的选择 |
| --- | --- | --- | --- |
| 写入模型 | 整体 manifest + exclusive lease | 按文件 operation，可无 lease 多写 | **保留 v1 单写者** |
| 文件事实来源 | `cloud_workspace_manifest_entries` | `cloud_workspace_files` + events | **manifest 唯一事实来源** |
| 并发控制 | `If-Match revision` + lease | `base_file_revision`/CAS | **manifest revision + fencing** |
| 冲突处理 | revision 不一致时整体处理 | rejected event、冲突副本、三方 merge | **不做在线 merge；脏缓存显式导出/丢弃** |
| 增量同步 | Pull/Push 全量 manifest | events + workspace cursor | **保留“delta batch”优化，但不保留 events/cursor** |
| 重试语义 | 目前不完整 | `op_id` 幂等 | **合并为通用 idempotency ledger** |
| 对象上传 | 整体/分块 staging，元数据存在缺陷 | operation 依赖对象已存在 | **保留 staging，补 ready 状态和 reconciliation** |
| sidecar | 文件快照；session 特殊合并 | 多写设想，无可靠 CAS | **统一 revision/CAS，全部受 writer lease 保护** |
| 历史恢复 | 当前没有稳定快照根 | 事件可重放但尚未有 retention | **增加 manifest snapshots，不采用 event replay 作为首发依赖** |
| 适用场景 | 单设备写入、跨设备接续 | 多设备同时编辑 | **跨设备接续** |

### 11.3 应合并进主干的 v2 价值

1. **对象生命周期**：保留分块上传、`staging → ready → deleting`、hash 校验、磁盘/租户 reservation 和可重入 GC；这些是大文件和崩溃恢复的基础，与是否并发编辑无关。
2. **幂等请求**：把 v2 的 `op_id` 思路泛化为 API/workspace/session 维度的 `Idempotency-Key + payload_hash + 原响应`，覆盖 create、upload complete、manifest、sidecar、release 和 snapshot restore。
3. **CAS 版本保护**：保留 `If-Match`，并给 sidecar 增加独立 revision；CAS 失败只表示 stale client，不能自动覆盖。
4. **会话级 fencing**：吸收 v2 的 `client_instance_id` 字段，但由认证 session 签发并绑定，不能接受客户端任意上报的身份。
5. **不可变历史与审计**：保留 append-only 的生命周期/提交审计，但把它与文件同步 events 分离；manifest snapshot 是恢复根，审计事件不参与文件树投影。
6. **本地 actor 与背压**：采用 v2 设计中按 workspace 串行处理的思路，统一 watcher、Pull、Push、sidecar、heartbeat 和 release；不同 workspace 可并行。
7. **增量提交**：吸收 v2 按文件收集变更的性能优势，增加受 lease 保护的 `manifest-delta` batch API；Hub 在一个事务中把 put/delete 应用到当前 manifest 并生成新 snapshot。它是“单写者的批量补丁”，不是无 lease 的 operation/event 协议。

### 11.4 应删除或彻底禁用的 v2 设计

以下设计对“非并发编辑”没有收益，反而会继续制造双事实来源，应从首发运行时删除；代码删除可分阶段进行，但不能再被调用：

- `cloud_workspace_files` 作为当前文件真相，以及 v1 manifest projection；
- `cloud_workspace_events`、`workspace_seq`、`snapshot_seq` 作为客户端追赶协议；
- `PushOperations`、`PullEvents`、`/operations`、`/events` 写入/读取 API；
- 任何允许无 lease 提交的 per-file operation；如需保留增量上传，只实现受 `If-Match + fencing` 保护的 `manifest-delta` batch；
- lease 冲突时的 `Shared=true`、无 lease watcher 和 v2 写入旁路；
- `base_file_revision`、`new_file_revision`、`conflict_of_seq` 等 per-file merge 字段；
- `.conflict-*` 临时副本、三方 merge、删除-修改在线冲突状态机；
- v1→v2 bootstrap、双写 projection、projection checkpoint 和运行时协议自动切换；
- 为支持并发编辑而引入的全量事件 retention/cursor 复杂度（manifest snapshot 保留替代）。

如果历史数据中已经存在 v2 events/files，先导出为只读审计或离线迁移包；不得让它们继续参与 manifest、配额或 GC 的事实计算。

### 11.5 首发运行时的最终组件清单

**Hub 保留**：

- workspace/status/quota；
- manifest entries + revision/hash；
- `manifest-delta` batch（仅作为 manifest 的受 lease 保护的写入优化）；
- immutable objects + staging reservation；
- manifest snapshots + restore；
- sidecar blobs + revision/CAS；
- leases + session/fencing；
- workspace-task binding；
- idempotency ledger；
- lifecycle/audit events（仅审计）。

**GUI 保留**：

- writer/read-only 两种明确打开模式；
- workspace actor 和本地进程锁；
- dirty/base revision 检测；
- handoff、等待、takeover、recovery 状态；
- flush commit barrier 和可恢复 pending 队列。

**首发不部署**：

- per-file event tailing；
- Shared mount；
- 在线 conflict copy/merge UI；
- v2 projection/replay worker。

### 11.6 删除实施和回滚边界

1. **禁用**：先通过 feature flag 和服务端 capability 禁止新客户端访问 v2 写入；旧客户端命中 v2 endpoint 返回 `PROTOCOL_MISMATCH` 或只读。
2. **观察**：连续一个完整发布周期确认 v2 operation/event 流量为零，保留审计指标和告警。
3. **迁移**：将仍有价值的 v2 文件状态折算为一次 manifest snapshot；无法确定一致性的 workspace 标记为 `RECOVERY_REQUIRED`，不自动覆盖。
4. **下线**：删除 GUI v2 调用、Hub v2 handler 和 projection job；数据库表先只读保留，经过 retention 后再 drop。
5. **回滚**：只能回滚到 `v1-sequential + read-only`，不能重新打开 Shared 或双写；已提交 manifest/snapshot/object 永不因回滚而删除。

### 11.7 这次裁剪后的验收标准

- 代码和配置中不存在默认开启的 `Shared` 或 v2 写入路径；
- 同一 workspace 在任意时刻最多一个有效 writer，其他设备可以稳定只读；
- A→B 正常交接、崩溃 takeover、旧 session fencing、sidecar flush barrier 和重复请求全部可验证；
- manifest、snapshot、对象状态、sidecar revision、task binding 的一致性可巡检和恢复；
- v2 表/endpoint 即使存在，也不会参与当前文件树、配额、GC 或客户端状态；
- 用户文案统一为“跨设备接续同一云端任务（单写者）”，不再暗示并发编辑。

### 11.8 本轮实现进度（2026-08-31）

已落地的首批改动：

- `PrepareCloudWorkspace` 不再把租约冲突降级为 `Shared=true`；仅在用户明确确认后执行 force takeover，取消则保持占用错误。
- 新增 `PrepareCloudWorkspaceReadOnly`，`SyncCloudWorkspaceFiles` 在无 writer 时使用只读挂载；只读挂载不启动 watcher、heartbeat 或写请求。
- watcher 和浏览同步只走 manifest Pull/Push，不再调用 `PullEvents`；删除文件也统一通过受租约保护的 manifest Push。
- 同一 workspace 的 prepare、Pull、Push、sidecar flush、Release 通过 workspace 级锁串行；不同 workspace 不再被全局锁互相阻塞。
- Release 将 sidecar flush 纳入提交屏障：flush 失败保留 lease 和恢复挂载，不再静默释放；删除 lease 失败也保留 heartbeat。
- object chunk/complete/whole PUT 统一要求 active workspace + writer lease；staging 对象增加 `object_state`，完成写入使用 upsert 修正压缩元数据。
- manifest 返回规范化 `manifest_hash`，成功提交在同一事务写入不可变 snapshot 根；本地 state 保存完整 `LastEntries` 基线。
- lease 表增加 `client_instance_id`、`fencing_token`、状态和交接字段；GUI 请求携带进程级 instance id，Hub 对同机不同 session 的迟到写入进行拒绝。

当时的待办（幂等账本、workspace-task binding、staging aggregate reservation）已在 11.10 落地；最终 GA 仍以跨进程/网络分区故障注入验收为门槛。

### 11.9 本轮增量实现（2026-08-31，v1/v2 收敛）

在上一轮基础上，已将以下设计直接落到代码和接口：

- **fencing token 贯穿写请求**：租约响应返回 `fencing_token`；GUI 挂载在每个工作区写请求发送 `X-Cloud-Workspace-Fencing`，Hub 将 token 与 `client_instance_id` 一起校验。旧会话迟到写入返回 `FENCED`，不会覆盖新 writer。
- **sidecar revision/CAS**：新增 `cloud_workspace_sidecars` 元数据表，revision 使用明文 SHA-256；GET 返回 `ETag`，PUT 必须携带 `If-Match`（新 sidecar 为空），冲突返回 `CLOUD_WORKSPACE_REVISION_CONFLICT`。`session.json` 不再通过 Hub 端无条件 merge，避免交接时复活旧历史。
- **manifest-delta**：新增 `POST /api/v1/cloud-workspaces/{id}/manifest-delta`，一次性提交 put/delete 列表，仍由 manifest revision、租约和 fencing 保护，并在同一事务更新用量、引用计数和 snapshot；GUI 在 Hub 支持时自动使用该批量接口，旧 Hub 回退整体 PUT。
- **snapshot restore**：snapshot 现在同时保存文件条目（`cloud_workspace_snapshot_entries`），新增 `POST .../snapshots/{snapshot_id}/restore`，恢复会生成新的 manifest revision/snapshot，不改写历史根，并重新校验对象状态和 `manifest_hash`。
- **通用幂等账本（首批）**：新增 `cloud_workspace_idempotency` 表和可重放原响应的存储 API；manifest PUT 已接入 `Idempotency-Key + payload_hash`，重复请求返回原 committed 响应，复用不同 payload 返回 `IDEMPOTENCY_KEY_REUSED`。
- **跨进程写锁**：GUI writer cache 创建 `.maclaw-cloud/writer.lock`，使用 `O_EXCL` 抢占并按保守 TTL 回收崩溃残留；活动 writer 的 lease heartbeat 同步刷新锁文件 liveness，避免长时间运行的健康进程被误判为 stale；只读挂载不持有该锁。
- **v2 endpoint 下线**：`/events` 与 `/operations` 仍保留历史代码和表用于迁移审计，但运行时直接返回 `PROTOCOL_MISMATCH`，不再参与 manifest、配额、GC 或客户端同步。
- **释放提交屏障**：DELETE lease 可携带 `X-Cloud-Workspace-Last-Revision`，Hub 只在该 revision 已成为工作区当前 revision 时释放；GUI 在 Push 后保存该 revision。
- **GC 根引用与 staging 回收**：snapshot 条目现在作为对象 GC 的保护根；过期分块目录回收时同步删除 `object_state=staging` 的数据库 reservation，避免恢复所需对象被误删或配额被永久占用。

因此，v1/v2 的合并边界已经从“运行时双协议”改为“v1-sequential 主干 + v2 可靠性零件”：只吸收 CAS、fencing、幂等、snapshot、增量 batch 和对象状态机；per-file events、无 lease operation、Shared mount、在线 merge 全部禁用。11.10 已补齐幂等账本、workspace-task binding 和 staging aggregate reservation；剩余工作聚焦跨进程/网络分区/旧 token 迟到写入的集成验收。

### 11.10 本轮分步实现（2026-08-31，继续收敛）

在 11.9 的基础上，本轮完成了以下可运行能力：

- **sidecar 跨进程 CAS**：sidecar 采用“不可变 revision 文件 + SQLite pending/commit 指针”。先登记 pending，再 fsync 文件，最后以事务切换指针；并发 Hub 进程不会在 CAS 失败后覆盖最终内容。保留 `<name>.enc` 兼容副本，但读取以已提交 revision 为准。
- **workspace-task 唯一绑定**：新增 `cloud_workspace_task_bindings`（`workspace_id` 与 `cloud_task_id` 唯一、版本 CAS、设备 task id 投影），并提供 GET/PUT/DELETE binding API。`task.json` 写入会自动创建/更新绑定；entitlement 在 sidecar 缺失时回退到绑定表。
- **staging 聚合 reservation**：新增按 workspace/hash/chunk 的 reservation 表；chunk 重试按旧大小做 delta，不重复计费；同时检查 workspace/tenant staging 字节和 hash 数量，GC 与 workspace purge 会回收 reservation。
- **幂等覆盖扩展**：create、restore/delete workspace、acquire/release lease、manifest/delta、whole/chunk/complete object、sidecar、snapshot restore、task binding 均接入 `Idempotency-Key + payload_hash`。服务端按 API 前缀隔离 key，响应可重放，payload 不一致拒绝。
- **只读缓存隔离**：只读挂载使用独立的 `cloud-workspaces-readonly/<tenant>/<workspace>/<client_instance>` 目录；停止挂载自动清理，不与 writer cache 共享文件树或进程锁。
- **对象可见性约束**：只有 `object_state=ready` 的对象可被 `Has/Get` 或 manifest 引用；chunk complete 使用既有 staging reservation，不再重复扣除容量；写入路径统一传递 fencing token。
- **交接请求与租约语义**：新增 `POST /leases/handoff-request`，只记录等待设备和请求时间，不改变写入权；heartbeat/release 同样校验协议头。lease release 的 409 不再被客户端吞掉，必须进入恢复或只读状态。
- **删除与恢复安全**：workspace 删除会校验 `client_instance_id + fencing_token`，同机旧进程不能删除新会话的工作区；对象 GC 改为 `ready → deleting → 删除元数据`，物理 unlink 失败时保留可重试行。
- **物理目录巡检**：GC 每轮对 `.part` 文件和 `cloud_workspace_staging_chunks` 做可重入 reconciliation，崩溃后采纳近期孤儿文件、修正大小并清理长期缺失 reservation；无效 chunk 不进入配额账本。
- **任务绑定原子性**：`task.json` 的 binding 更新延迟到 sidecar immutable 文件 finalize 事务，与 sidecar 指针一起提交/回滚，避免“绑定已更新但 sidecar 写失败”的半提交。
- **本机 writer 锁生命周期**：release flush 期间保留 `writer.lock` 和 mount 身份，确保 Push 请求仍带 fencing；只有远端 lease 决策完成后才释放本机锁，网络/CAS 失败会保留恢复挂载。锁文件写入不可预测 owner token，释放和 heartbeat 都校验 owner，避免旧进程迟到清理或刷新后继进程的锁。活动 mount 的 lease heartbeat 同步刷新 `writer.lock` 的 liveness，避免长时间运行的健康进程被 TTL 误判为 stale；若本机锁丢失则立即停止写入并降级只读。
- **watcher 丢事件自愈**：fsnotify 队列溢出、backend error 或新目录监视失败时，mount 标记 `reconcile_required`，向 UI 发出显式状态事件并调度一次完整 manifest scan/Push；成功后清除标记，避免把 watcher 错误静默当成同步完成。
- **workspace/task 创建补偿状态机**：新增 `POST /api/v1/cloud-workspace-tasks`，在 Hub 事务中创建 workspace + 唯一 binding 并返回 `provisioning` operation；本地 task/sidecar 成功后调用 `/complete`，失败调用 `/abort` 将 workspace 软删除并移除 binding。`Idempotency-Key` 可重放创建、完成和补偿，`GET /api/v1/cloud-workspace-tasks/{operation_id}` 可在超时/重启后查询。

仍未达到最终 GA 的项目：网络分区自动降级的端到端验收、以及对所有写 API 的跨 Hub 进程故障注入测试。`cloud_workspace_files/events` 继续只读保留，不能重新启用为运行时事实来源。

### 11.11 本轮一致性加固（2026-09-01）

针对上一版评审中仍存在的两个根本性窗口，本轮做了收敛：

- **幂等 pending 不再按时间自动抢占**：旧实现把超过 5 分钟的 `pending` 记录直接改派给新客户端；如果原请求已经提交副作用、仅在返回响应前崩溃，重试会再次执行同一写操作。本轮改为：`pending` 不设自动过期，保持 `IN_PROGRESS`，只允许原处理流程调用 `Finish/Cancel`（或 workspace purge 显式清理）；过期回收只删除 `committed` 响应，pending 不由 GC 静默删除。带有 durable operation 的接口（当前为 task provisioning）必须通过 operation status 做恢复，而不是发起第二个 mutation。
- **provisioning 崩溃恢复**：task provisioning 额外把 `Idempotency-Key + payload_hash` 写入 operation 行。若 Hub 在 workspace/binding 事务提交后、通用账本返回前崩溃，重试会查找并重放原 operation，而不是因为 pending 账本再次创建 workspace。
- **sidecar 提交指针自愈**：当 SQLite 中已存在 committed revision、但 immutable 文件因磁盘恢复或人工清理缺失时，读取仍返回缺失；持有该 revision 的 writer 现在可用 `If-Match` 直接写入新 revision，恢复 sidecar，而不会被永久 revision conflict 卡死。空 `If-Match` 仍被拒绝，避免无基线客户端覆盖未知内容。
- **对象孤儿回收**：对象文件先于 `cloud_workspace_objects` 元数据提交时会暂时不可见；GC 现在扫描对象目录，经过 grace window 仍没有 metadata 的旧 `.enc` 会被删除，近期文件保留给可能仍在提交事务的上传者。
- **跨平台路径边界**：manifest 入口现在拒绝 Windows 保留设备名、尾随点/空格，并以 Unicode NFC + 大小写折叠检测 `README.md`/`readme.md` 等跨设备 collision；原始合法拼写仍保留在 manifest 中。
- **客户端预检一致**：GUI scan/Pull 在上传或落盘前复用同一 portable path 规则并检测 collision，避免先上传对象、再由 Hub 拒绝 manifest，或在 Windows 落盘时静默覆盖。
- **下线代码裁剪**：`/events` 与 `/operations` handler 仅保留鉴权后 `PROTOCOL_MISMATCH` 响应，移除其后的不可达历史实现，降低误调用和静态检查噪声。
- **协议头覆盖 provisioning**：GUI 对 `/cloud-workspace-tasks` 及 `/cloud-workspaces` 全部请求显式发送 `X-Cloud-Workspace-Protocol: v1-sequential`；含 workspace id 的请求继续附带 lease/session/fencing，创建阶段没有 workspace id 时也不会落入未协商协议。
- **前端补偿测试**：Sidebar 创建绑定工作区的成功、打开失败补偿、仅缓存路径识别三条路径均改用 `Provision → local task → Complete/Abort` 断言，避免旧 `CreateCloudWorkspace/DeleteCloudWorkspace` 测试掩盖编排状态机回归。

本轮新增/通过的回归覆盖：stale pending 不可抢占、过期幂等只清理 committed、已提交 sidecar 文件缺失后的 CAS 修复、前端 provisioning 流程 150 项测试。最终 GA 门槛仍为网络分区端到端故障注入，以及跨 Hub 进程在每个写 API 的提交前/提交后崩溃测试；在这两项完成前，不应宣称“已确认提交零丢失”。

截至 11.11 仍需明确列为后续根本性工作（其中实例身份已在 11.12 落地）：

1. **实例身份签发（11.12 已完成）**：11.11 时 `client_instance_id` 仍由 GUI 生成并通过 header 上报，只能解决误用/迟到写入，不能抵抗已授权客户端冒认实例。11.12 已改为 Hub 签发短期 session token，并从 token 解析实例。
2. **快照物理配额**：`used_bytes` 仍表示当前 manifest 逻辑大小；快照保留对象、staging 和孤儿 quarantine 的物理占用需要独立计量，并在 workspace/tenant admission 与 UI entitlement 中使用同一保留根计算。
3. **灾备边界**：本实现仍是 Hub 本地 SQLite + blob 目录；上线前需完成一致性备份、master key 托管/轮换和恢复演练，定义 RPO/RTO 后再称为长期云端存储。

### 11.12 本轮实例身份与网络分区闭环（2026-09-01）

本轮按 11.11 的剩余根本性问题继续实现，完成了“服务端签发实例身份”和第一组网络分区端到端验收，并在测试中暴露、修复了三个会导致旧 epoch 复活或本地修改丢失的设计缺陷。

- **Hub 签发实例 session**：新增 `cloud_workspace_instance_sessions`。`POST /api/v1/cloud-workspace-sessions` 使用机器凭据签发服务端生成的 `client_instance_id` 和 256-bit opaque token；数据库只保存 token SHA-256，不保存明文。session 绑定 tenant、user、machine、`v1-sequential` 协议和 15 分钟空闲过期时间。
- **实例身份不再信任客户端 header**：除 entitlement 探测外，云工作区 API 必须携带 `X-Cloud-Workspace-Instance-Session`。Hub 从 session 记录恢复 `client_instance_id`；客户端若同时上报不一致的 `X-Cloud-Workspace-Instance`，请求返回 `CLOUD_WORKSPACE_SESSION_INVALID`。通用机器认证不再解析云工作区实例/fencing header。
- **滑动过期与写放大控制**：session 只在剩余 TTL 进入后半段时续期，避免每个 object chunk、manifest GET/PUT 都争用 SQLite writer；活跃 heartbeat 足以维持 session，长期离线实例会自然失效。
- **单实例吊销**：新增 `DELETE /api/v1/cloud-workspace-sessions/{session_id}`。吊销只影响该 process session，不影响同一机器上的其他实例；GUI 正常退出在释放所有 workspace 后执行 best-effort revoke，网络失败时仍由短 TTL 收敛。
- **GUI 会话接入**：GUI 首次非 entitlement 请求自动签发 session，同一 Hub/机器凭据下进程内复用；不再发送自生成实例身份。Hub 返回 session 失效时清除本地缓存，当前请求失败，不在请求内部静默换身份重放写操作。
- **provisioning 提交屏障补齐**：`complete` 在新实例协议下必须同时匹配 active lease、client session 和 fencing token；GUI 先读取 durable operation 得到 workspace，再附加该 workspace 的 lease epoch。`abort` 在有 active lease 时同样要求 fencing；若从未取得 lease，仅允许最初签发的实例执行补偿，避免 Prepare 失败后泄漏配额。
- **过期租约不可原 epoch 续命**：修复 `Acquire` 先判断“同 machine/session”再判断过期的问题。同一实例在 TTL 后重新获取写权也会释放旧 lease、生成新 lease id 和更大的 fencing token；旧 heartbeat 和迟到 manifest 写入均返回 `FENCED`。
- **Acquire 不再复用内容型幂等键**：GUI 过去仅按 `{force:false}` 计算固定 `Idempotency-Key`，会在 24 小时内重放已经释放或过期的旧 lease 响应。Acquire 本身对同一 live instance 是事务可重入的：响应丢失后的再次调用会返回同一个 active lease/epoch；因此客户端不再给 Acquire 使用 payload-derived ledger key，避免网络恢复时拿到陈旧 lease id/fencing token。
- **网络分区自动降级与恢复**：新增 GUI HTTP 端到端测试，模拟 heartbeat 持续 503 直至 lease 过期，验证 watcher/自动上传停止、mount 降级只读、直接 Push 也被本地 guard 拒绝。用户再次 Prepare 时，旧只读 mount 会安全释放本机 writer lock，保留缓存内容，并以新 fencing epoch 重新接管。
- **空 manifest 基线不再丢失**：此前初次 Pull 的远端 revision 和 entries 都为空时，`omitempty` 会把空基线序列化成“没有基线”；分区期间新增的本地文件在恢复时会被当作首次打开并 Pull 删除。state 现增加 `baseline_initialized`，即使远端是空树也能识别本地 dirty，按确认结果用新 epoch Push，而不是静默覆盖。
- **只读后的旁路写入封死**：`pushCloudWorkspace` 现在要求存在 writable、non-releasing mount；即使某个 UI 调用绕过 watcher 调度，也不能在自动降级后继续上传。

本轮新增验收覆盖：

1. session 由 Hub 生成、明文 token 不落库、绑定 machine/protocol、空闲续期、到期失效和单实例吊销；
2. 缺少 session、伪造 instance header、已吊销 token 均被拒绝；
3. A 获取 lease、B takeover 后，A 的旧 instance session + 旧 fencing token 的迟到 manifest 写入被拒绝，B 可正常提交；
4. 同一 instance 在 lease 过期后重新 Acquire 得到新 lease id/更大 fencing token，旧 epoch 不可复活；
5. heartbeat 网络分区 → TTL 到期 → GUI 自动只读 → 禁止 Push → 网络恢复 → 新 epoch 接管并保留/提交本地 dirty 文件。

本轮仍不把系统标记为“已确认提交零丢失”。剩余 GA 根问题按优先级为：

1. **跨 Hub 进程崩溃矩阵**：对 object whole/chunk/finalize、manifest/delta/snapshot、sidecar、binding、lease、provisioning 在事务提交前、提交后响应前分别注入进程退出，并用第二个 Hub 进程验证重放与 reconciliation。
2. **统一物理保留配额（11.13 已完成）**：将当前 manifest、snapshot roots、staging reservation 和未来 quarantine 纳入 workspace/tenant retained bytes；admission、entitlement 和 GC 必须使用同一计算口径。
3. **Hub 灾备**：实现 SQLite + blob + master key 的一致性备份、密钥托管/轮换、恢复演练和可量化 RPO/RTO。

### 11.13 本轮统一保留配额（2026-09-01）

本轮继续按 11.12 的优先级实现“统一物理保留配额”。这里的“物理保留”指对象层实际需要保留的唯一内容及未完成上传，不再用 manifest 路径大小之和近似；计费单位固定为**唯一对象的明文大小**，而不是压缩/加密后的文件大小。选择明文大小是为了让上传前可以确定性准入，并避免压缩算法、加密封装或密钥轮换改变用户配额。Hub 磁盘的真实 ciphertext 空间仍由 volume free space 检查和运维指标独立保护。

统一口径如下：

```text
logical_bytes
  = 当前 manifest 每个 path 的 size 之和

retained_bytes
  = workspace 内非 staging 对象的唯一明文大小
  + whole-object staging reservation
  + chunk staging reservation

snapshot_retained_bytes
  = 被保留 snapshot 引用、但不再被当前 manifest 引用的唯一 ready 对象

unreferenced_retained_bytes
  = 未被当前 manifest/snapshot 引用但尚未完成 GC 的 ready/deleting/quarantine 对象
```

同一 hash 在同一 workspace 的对象表中只计一次；两个 manifest path 指向同一对象时，`logical_bytes` 按两个文件路径计量，`retained_bytes` 只计一份内容。当前 manifest 与 snapshot 同时引用同一对象时归入 current，不重复归入 `snapshot_retained_bytes`。软删除 workspace 在 7 天恢复期内继续占用 tenant retained quota，只有物理目录删除成功且数据库 purge 事务提交后才释放。

本轮代码收敛点：

- 新增 live `RetainedUsage` 查询，以 manifest、`cloud_workspace_objects`、snapshot entries 和 staging chunks 为唯一来源；不新增可漂移的 `retained_bytes` 缓存列。GC 删除对象、snapshot retention、staging reconciliation 和 workspace purge 完成后，下一次查询自然得到新用量。
- `PrepareObjectPutWithSession` 和 `ReserveStagingChunk` 在同一个 `BEGIN IMMEDIATE` 中按 retained delta 做 workspace/tenant 准入。相同 reservation 重试不重复扣费；缩小或完成既有 reservation 即使当前已超配额也允许继续，避免超限后无法清理。
- whole-object 的 `staging` 行现在记录明文 reservation 并支持可重入更新；chunk complete 从已计费 chunks 转为同大小 ready object。对已经 ready 的 hash 再上传 chunk 会成为幂等 no-op，并清理遗留 part/reservation。
- tenant 创建 workspace 与 task provisioning 的磁盘门槛改用 retained usage；当前 manifest 的 `used_bytes` 字段继续保留原逻辑语义，避免破坏旧客户端和旧 API。
- entitlement 在顶层、活动 workspace 和软删除 workspace 同时返回 `logical_bytes`、`retained_bytes`、`snapshot_retained_bytes`、`staging_bytes`、`unreferenced_retained_bytes`，并返回 `tenant_max_total_bytes`。GUI 工作区列表优先显示 retained bytes，对旧 Hub 自动回退 `used_bytes`。
- 管理端 settings preview 和运行指标增加同一组 retained 分类；旧 `used_bytes` 仍作为兼容字段，新的字段直接来自 live 根计算。
- `deleting` 对象在物理 unlink 成功前继续计费；未知的未来非 staging 状态（包括 quarantine）也 fail-closed 纳入 retained 总量，不能通过新增对象状态绕过 quota。

新增回归覆盖验证了：重复 path 的对象去重、snapshot 独占对象、deleting/quarantine、whole/chunk staging 的分类；snapshot 与软删除对象会阻止 workspace/tenant 超额上传；相同 reservation 重试不双计、缩小 reservation 可在超限状态下继续；entitlement 保持旧 `used_bytes` 兼容语义并输出新的 live retained 字段。

完成本节后仍不能宣称“已确认提交零丢失”。GA 根问题收敛为：

1. **跨 Hub 进程崩溃矩阵**：覆盖 object whole/chunk/finalize、manifest/delta/snapshot、sidecar、binding、lease 和 provisioning 的提交前/提交后响应前进程退出。
2. **一致性灾备与密钥生命周期**：实现 SQLite + blob + master key 的原子备份边界、外部密钥托管/轮换、恢复演练和量化 RPO/RTO。

### 11.14 本轮跨 Hub 进程崩溃闭环（2026-09-01）

本轮完成 11.13 排在首位的提交崩溃矩阵，并在测试前先修复了矩阵会暴露的通用幂等缺口。旧写路径是三个独立事务：`BeginIdempotency(pending) → mutation COMMIT → FinishIdempotency(committed response)`。进程若在 mutation 已提交、`Finish` 之前退出，第二个 Hub 只能看到永久 pending；直接抢占 pending 又会重复副作用。因此，单纯增加故障注入测试不能解决问题，必须先改变提交协议。

新的原子幂等协议如下：

```text
请求开始：只读查询 committed response，不创建 pending
    ↓
文件预写 / provisional reservation（如 object、chunk、sidecar）
    ↓
BEGIN IMMEDIATE
    ├─ 再查同 key 的并发 winner
    ├─ 执行业务最终 mutation
    ├─ 编码与真实 HTTP 首次响应完全相同的 status/body
    ├─ INSERT idempotency(status=committed, response)
    └─ COMMIT                         ← mutation 与响应收据同一提交点
```

如果两个 Hub 同时以相同 key 开始，它们都可以做可安全覆盖的文件预写，但最终 SQLite 事务由唯一键串行化：winner 原子提交 mutation + response；loser 在事务开始或插入收据时读到 winner，回滚自己的数据库 mutation并重放原响应。不同 payload 复用 key 仍返回 `IDEMPOTENCY_KEY_REUSED`。11.11 以前遗留的 pending 行继续 fail-closed，不因本轮协议而被自动抢占；只有新原子协议不再制造 pending。

本轮代码收敛点：

- manifest replace、manifest delta、snapshot restore、task binding put/delete、lease acquire/handoff/heartbeat/release、workspace/task provisioning begin/complete/abort，以及 workspace lifecycle 的最终事务，都可在同一事务内写入幂等 response receipt。handler 末尾保留的旧 `FinishIdempotency` 仅作兼容确认，不再承担正确性。
- object whole/finalize 改为先原子写密文文件，再在一个最终事务内完成 `object_state=ready`、codec metadata、staging 释放和 response receipt；最终事务再次校验 active lease/session/fencing，堵住文件 I/O 期间 takeover 的迟到提交。
- 若进程留下“文件存在但 codec metadata 未提交”的对象孤儿，重试会从同一明文重新压缩/加密并 finalize，不能把压缩密文误标为 `compression=none`。ready 对象的幂等路径只补齐大小，不覆盖已提交 codec。
- chunk PUT 的 quota reservation 是 provisional；part 文件写完后新增最终 acceptance 事务，再次校验 writer epoch、reservation 大小并提交 response receipt。提交前退出留下的相同 part/reservation 可由第二进程确定性覆盖并完成。
- sidecar 相同 `pending_revision` 现在可由重试立即接续。旧实现必须等待 stale grace，即使前一进程写的是完全相同的 immutable 内容，也会在崩溃后暂时返回 revision conflict。
- standalone task binding 将 lease/session/fencing 校验移入 binding 最终事务；旧实现先在一个事务外检查 lease，再更新 binding，takeover 可以卡在两者之间。
- 原 HTTP JSON response（包括字段名、状态码和结尾换行）在最终事务内生成并保存。object 旧路径曾把 Go struct 的大写字段存入 ledger，却在首次请求返回小写 API 字段，本轮增加 exact-body replay 回归并统一修复；sidecar replay 同时恢复 `ETag`/`Cache-Control`，204 replay 不再错误附加 JSON content type。

新增真实进程测试使用同一个 Go 测试二进制作为独立 Hub helper：每个进程重新打开同一 SQLite WAL、blob root 和 key directory，不共享内存或数据库连接。仅当最终 mutation 已 stage response receipt 时，测试 hook 才在 `before_commit` 或 `after_commit` 直接 `os.Exit`。随后由另一个进程验证：

1. `before_commit`：SQLite 最终 mutation 与 response receipt 一起回滚；object/chunk/sidecar 允许只留下不可见 provisional 文件或 reservation，第二进程可安全 reconciliation 后提交一次；
2. `after_commit`：第二进程直接取得 committed 原响应，不再次执行业务 mutation；
3. 每个 case 最终只有一个 committed receipt、一个逻辑副作用，binding version、active lease、provision operation 和 manifest/snapshot 不重复推进。

矩阵共覆盖 16 类写操作、32 个进程退出点：object whole/chunk/finalize，manifest put/delta/snapshot restore，sidecar，task binding put/delete，lease acquire/handoff/heartbeat/release，provisioning begin/complete/abort。另有同进程并发 winner/loser 测试验证 loser 回滚与不同 payload 冲突。

完成本节后，“单机持久层上的提交前/提交后进程崩溃”已闭环；仍不能将其外推为灾难场景零丢失。最终 GA 根问题只剩：

1. **一致性灾备边界**：SQLite WAL/checkpoint、blob immutable files 与 master key 必须进入同一个可恢复备份代际，不能分别复制后假定一致。
2. **密钥生命周期**：master key 外部托管、版本化 envelope、在线轮换和旧版本恢复必须实现并演练。
3. **量化恢复承诺**：以真实备份恢复到新 Hub，校验 manifest/snapshot/object/sidecar 全树和 retained usage，给出并持续验证 RPO/RTO；完成前文案只能是“进程崩溃已验证”，不能是“灾备零丢失”。

### 11.15 本轮一致性备份与恢复代际（2026-09-01）

本轮没有另建 Cloud Workspace 专用导出格式，而是加固 Hub 已存在的 `hub backup create / inspect` 和 `hub restore`。原实现虽然用 `VACUUM INTO` 得到了单独一致的 SQLite 文件，但随后直接遍历在线 `data` 目录：SQLite 快照之后若 GC 删除旧对象、或新对象在目录已遍历位置之后才提交，归档中的数据库根与 blob 文件可能跨代；manifest 也只有路径和大小，没有内容摘要；restore 会边解包边覆盖目标，后半段校验失败时目标已经被部分修改。

新的 backup v2 把 SQLite、Cloud Workspace 文件树和恢复密钥身份收敛成一个明确代际：

```text
BEGIN IMMEDIATE                         // 只阻塞 SQLite writer，读请求继续
    ↓ backup cut = manifest.created_at
SQLite online-backup API → 临时 hub.db  // 不复制 live db/wal/shm
    ↓
copy data/cloud-workspaces → 临时代际   // objects/sidecars/staging/master.key
ROLLBACK writer barrier                 // manifest.write_pause_ms 到此结束
    ↓
quick_check + Cloud Workspace 全根校验
    ↓
逐文件写 tar.gz，并记录 size + SHA-256
    ↓
manifest.json 最后写入 generation_id / completed_at / duration_ms
```

这里使用 writer barrier，而不是“前后各拍一次快照再比较”的乐观重试。原因是 ready object 和已提交 sidecar 虽然是 immutable，但 GC 物理 unlink、workspace purge 与 metadata 根之间仍有窗口；write barrier 把会改变恢复根的事务挡在 backup cut 之后。对象/sidecar 文件在其 metadata 提交后不可原地修改，因此 barrier 内完成文件复制后就可以释放写锁；对临时代际的解密、hash 和 retained usage 复算在锁外进行，避免把全树校验时间计入业务写暂停。

归档 manifest v2 新增：

- `generation_id`、`created_at`（精确 backup cut）、`completed_at`、`duration_ms` 和 `write_pause_ms`；
- SQLite 在归档内的确定路径和一致性协议 `sqlite-online-backup+cws-write-barrier-v1`；
- 每个 entry 的 SHA-256，restore 拒绝未声明、重复、缺失、长度不符或 hash 不符的文件；
- Cloud Workspace 的 workspace/object/snapshot/sidecar 数量、统一 retained usage，以及 master key provider/key id；key id 是 key material 的 SHA-256，不泄露密钥。

归档自身先写到输出目录同卷的临时文件，完成 gzip/tar close 和文件 sync 后才 rename 发布；已存在的目标文件不会被截断覆盖。在线数据目录遍历同时排除 live DB、`-wal/-shm/-journal`、正在生成的临时归档和 Cloud Workspace live root，后者只能从 barrier 内的临时代际进入归档。

master key 的备份边界现在是显式的：文件 provider 的 `master.key` 与 blob 在同一 `data` 代际中归档；`MACLAW_CWS_MASTER_KEY` 环境 provider 不会把明文 secret 塞进 tar，只记录 provider 和 key id，restore 进程必须从外部重新提供同一密钥，否则在修改目标之前失败。即使环境 provider 目录中残留旧的本地 key 文件，backup v2 也会将其排除，避免把历史 secret 误打进归档。这样不会用“恢复时生成一把新 key”掩盖密钥丢失，但它也不等于已经完成外部 KMS 托管。

restore 改为三段式：

1. 在目标同卷的临时目录完整解包，验证路径边界、entry SHA-256 和 SQLite `quick_check`；
2. 用恢复 SQLite 重新遍历当前 manifest 与所有 snapshot roots，要求每个引用都有 ready metadata；所有 ready object（包括已完成上传但尚未进入 manifest 的 retained object）都逐个解密并复核明文 SHA/size，所有 committed sidecar 都逐个解密并复核 revision，同时校验 snapshot manifest hash、workspace logical counters、object `ref_count` 和统一 retained usage；任何失败都不会修改目标；
3. Hub 停止后以 top-level rename 激活候选代际。数据库和 `cloud-workspaces` 同在 `data` 目录，所以二者作为一个目录切换；旧目录保留到全部切换成功才删除，错误会反向 rename。未进入归档的本地日志等文件先合入候选代际，旧 SQLite 的 `-wal/-shm/-journal` 明确丢弃，不能污染恢复快照。

恢复演练测试使用真实 WAL SQLite migrations 和真实加密 BlobStore，创建 workspace、object、manifest、自动 snapshot、task sidecar、task binding 与 active provisioning 行；备份后恢复到全新 Hub root，再验证对象明文、sidecar、binding/provisioning、snapshot 根和 retained usage 完全一致。另有确定性 writer-barrier 测试证明 backup cut 之后等待的写事务不会混入该代际，以及篡改 entry 时 restore 在目标 mutation 前失败。v1 归档仍可读取和恢复，但因历史格式没有逐文件 SHA-256，只能获得 SQLite/Cloud Workspace 结构校验，不能提升为 v2 的传输完整性保证。

RPO/RTO 从本轮开始可量化，但不虚构与部署规模无关的固定数字：

- 单个代际的 `created_at` 是可恢复数据截点；归档完成时的即时 RPO 是 `completed_at - created_at`，运行时还必须加上“最近成功归档的调度间隔和异地上传延迟”；
- backup manifest 的 `write_pause_ms` 可单独设告警，避免把压缩/上传总耗时误认为 writer 停顿；
- dry-run 和真实 restore 都返回 `completed_at / duration_ms`，生产演练应分别记录验证耗时和切换耗时；业务 RTO 还需加 Hub 启动、health check 和客户端重连时间。

因此本轮可以声明“本地 backup v2 的 SQLite + Cloud Workspace + 文件密钥是一致可恢复代际，并有真实恢复演练”，仍不能声明“灾备零丢失”或给出通用 GA SLO。剩余工作进一步收敛为：

1. **密钥生命周期（11.16）**：版本化 ciphertext envelope、active key 轮换、旧 key 恢复和安全退休已完成；可插拔外部 KMS/secret provider 仍只有接入契约，没有具体厂商适配器。
2. **持续灾备运营（11.17）**：已把 backup v2 接入定时、异地目标接口、保留/删除策略和告警；生产数据量上的 dry-run + 新 Hub 恢复及 RPO/RTO 分位数仍需部署方执行。
3. **集中审计运营（11.24--11.27）**：Hub 留存、硬删除 tombstone、分页导出和显式 retention 原语已完成；具体 SIEM/WORM 接线、导出凭据与保留期限仍由部署方治理。

### 11.16 本轮密钥生命周期、版本化封装与在线轮换（2026-09-01）

本轮把密钥从“一个长期 `master.key`”收敛为可审计的版本集合，并让密文自己携带所需的 key id。目标是支持跨设备接续和灾备恢复，而不是在应用层暴露密钥或引入并发编辑协议。

#### 11.16.1 密钥提供者边界

`cloudworkspace.KeyProvider` 是唯一的密钥注入边界，接口只返回 Hub 进程内使用的 `KeyMaterial`，对外状态只返回 SHA-256 指纹：

```go
type KeyProvider interface {
    ProviderName() string
    ActiveKey(context.Context) (KeyMaterial, error)
    Key(context.Context, string) (KeyMaterial, error)
    AllKeys(context.Context) ([]KeyMaterial, error)
    Descriptor(context.Context) (MasterKeyDescriptor, error)
}
```

对象和 sidecar 都通过 `BlobStore.Keys` 注入 provider；备份/恢复还可通过 `CreateOptions.KeyProvider` / `RestoreOptions.KeyProvider` 注入同一个 provider。未注入时按以下顺序选择：

1. `MACLAW_CWS_KEYRING`：JSON `{ "active": "<base64>", "previous": ["<base64>", ...] }`，适合由 secret manager 渲染的短期进程配置；
2. `MACLAW_CWS_MASTER_KEY`：单个 32 字节 base64 key，只读兼容旧部署；
3. 文件 provider：`data/cloud-workspaces/master-keyring.json`，权限 0600。

当前仓库没有具体 AWS KMS、Azure Key Vault、HashiCorp Vault 等适配器。外部 KMS 只需实现 `KeyProvider`；若要在线轮换/退休，还需实现 `RotatableKeyProvider` / `RetirableKeyProvider`，并由 provider 自己保证密钥版本的 durable 提交。环境 provider 因此只读，CLI 不会假装替它写回 KMS。

文件 provider 首次读取旧 `master.key` 时建立 keyring，旧文件保留到该 key 被安全退休；新 key 只追加到 keyring，不覆盖旧 key。keyring JSON 中不记录明文之外的额外秘密元数据，写入使用同目录临时文件 + 原子 rename。

#### 11.16.2 版本化 ciphertext envelope

新密文格式为：

```text
8 bytes  magic = MCWSENV\x02
32 bytes key id = SHA-256(key material)
N bytes   AES-GCM(nonce || ciphertext || tag)
```

数据库 `encryption_version` 记录 `aes-gcm-v2:<key-id>`；key id 只用于定位版本，不可反推出 key material。AAD 仍绑定 `tenant_id|user_id|workspace_id`，sidecar 另外绑定名称，避免跨租户、跨用户、跨 workspace 或跨 sidecar 复制密文后被接受。

读取规则是 fail-closed：带 v2 magic 的截断/未知 envelope 不会降级为旧格式；旧 raw AES-GCM 密文只有在没有 v2 magic 时才按 provider 的 active→previous 顺序尝试解密。解密成功后仍复核对象明文 SHA-256 或 sidecar revision。这样既能无停机读取历史对象，又不会把格式损坏伪装成合法 v1 数据。

#### 11.16.3 在线轮换和崩溃恢复

`BlobStore.RotateEncryption` 在一个 SQLite `BEGIN IMMEDIATE` 写屏障内完成元数据根的一致切点：

```text
BeginRotation → durable rotation_key_id + active_key_id
    ↓
遍历 objects/*.enc 和 sidecars/*.enc
    ↓
按 AAD 解密，再用新 active key 原子重写 envelope
    ↓
更新 ready object 的 encryption_version
    ↓
COMMIT → 清除 rotation_key_id
```

文件本身采用原子写，旧 key 在轮换期间继续保留。因此进程在任意文件之后退出都不会让已写文件不可读：下次执行 `rotate` 会识别 `rotation_key_id` 并从剩余混合代际继续。轮换期间只阻塞 SQLite writer，读取和不同于元数据写入的文件预读仍可继续；运维需要监控写屏障持续时间和文件扫描量。

轮换遍历严格识别 `{tenant}/{user}/{workspace}/objects/{sha}.enc` 与 sidecar revision 文件；未知 `.enc` 路径、符号链接、错误 key id 或无法解密的文件都会中止，不跳过问题继续“部分成功”。

#### 11.16.4 旧 key 安全退休

`RetireEncryptionKey` 不是删除按钮，而是带前置证明的 fail-closed 操作：

- 目标 key 不能是 active 或仍在 rotation marker 中；
- 目录中不能存在 legacy raw ciphertext；
- 不能有任何 v2 envelope 仍标记该 key；
- provider 必须实现 `RetirableKeyProvider`，外部 KMS 的禁用/销毁由其控制面完成；
- 文件 provider 先原子提交缩减后的 keyring，再删除内容相同的旧 `master.key` recovery copy。

任一条件不满足都会保留原 key 并返回错误。退休前应先执行 `hub cloud-workspace keys status` 和 `rotate`，再把返回的 active/key count 写入变更记录；不能通过删除 keyring 文件来“强制”退休，因为那会破坏恢复能力。

#### 11.16.5 CLI 和运维流程

新增本地管理入口：

```text
hub cloud-workspace keys status --config <config.yaml> [--json]
hub cloud-workspace keys rotate --config <config.yaml> [--json]
hub cloud-workspace keys retire --config <config.yaml> --key-id <sha256> [--json]
```

推荐流程：

1. `status` 保存当前 provider、active fingerprint、key count 和 rotation marker；
2. 在低峰执行 `rotate`，等待 `rotation_key_id` 清空并抽样读取对象/sidecar；
3. 重新执行 backup v2，确认同一代际包含完整 keyring 和全部 ciphertext；
4. 仅在旧 key 不再被任何 envelope 使用、且恢复演练已验证新 key 后执行 `retire`；
5. 对环境/KMS provider，在外部 secret/KMS 控制面完成等价步骤，Hub CLI 只读并明确拒绝本地写操作。

CLI 的 rotate/retire 会持有 SQLite 写屏障，不能与在线 Hub 的高峰写入混为普通后台任务。命令输出和 backup manifest 只含 provider、key id、数量和版本，不含 base64 key material。

#### 11.16.6 本轮边界与下一步

本轮已完成文件 provider 的版本化封装、旧 raw ciphertext 兼容、在线轮换、崩溃后可恢复 marker、旧 key 安全退休、备份归档分类和 CLI 管理入口；新增测试覆盖 envelope/AAD、keyring 首次迁移、rotation/resume、legacy 拒绝退休和退休后 recovery copy 清理。

截至 11.16 尚未完成、随后在 11.17 收敛的部分和仍保留的边界：

- 没有具体云厂商 KMS adapter、远端 key escrow 或 HSM 证明；`KeyProvider` 只是接入契约；
- 定时备份、保留策略、dry-run 和告警调度已在 11.17 完成；异地不可变存储、密钥销毁审批和生产规模轮换演练仍需部署方完成；
- 旧 v1 backup 可以恢复 raw ciphertext，但只有在恢复环境提供同一旧 key 时才可解密；环境 secret 丢失必须 fail-closed；
- RPO/RTO 仍需按部署规模实测，不能从本地轮换或单次恢复测试外推“零丢失”。

因此 11.16 阶段的正确文案是：“Cloud Workspace 支持版本化密文、可恢复的在线 key rotation 和显式安全退休；外部 KMS 与持续灾备运营尚待接入。”最终状态以 11.17 的持续灾备运营章节为准。

### 11.17 本轮持续灾备运营（2026-09-01）

本轮把 11.15/11.16 的一次性 `backup create` 接入一个可停止、可观测、不会并发重入的后台调度器。调度器默认关闭；只有配置 `backup.enabled: true` 才会在 Hub 启动后运行。它不复用其它业务 ticker，也不把长时间归档放入请求处理路径。

#### 11.17.1 调度与幂等

调度器启动时在没有成功代际时执行一次即时备份，之后按 `interval_seconds` 运行。每次执行使用独立的 `backup.Create` 调用和唯一输出文件名；同一进程内的手工触发与 ticker 触发通过互斥锁串行化，重叠调用返回 `ErrSchedulerRunning`。新增 `hub backup run --config <config.yaml>` 作为策略感知的手工入口，它与后台调度共享分布式锁、异地发布、保留清理、dry-run 和状态账本；原 `hub backup create` 仍保留为不执行保留策略的底层归档原语。Hub 退出时先取消调度器并等待当前备份在关闭上下文内结束。

多 Hub 进程共享同一 SQLite 数据库时，`NewSchedulerFromConfig` 默认接入 `SQLiteRunLock`：它按主数据库绝对路径派生独立的 `<hub.db>.backup-lock.db` sidecar，在 sidecar 中以带 TTL 的 durable row 抢占并在运行期间续租，释放时按 owner 条件删除，旧进程迟到释放不会删除新 owner。锁 sidecar 不属于业务恢复代际，备份归档会明确排除其数据库及 `-wal/-shm/-journal` 文件；更重要的是，备份在主数据库上持有 `BEGIN IMMEDIATE` writer barrier 时，sidecar 仍可写入心跳，不会因主库写锁阻塞而提前过期。`backup.distributed_lock_lease_seconds` 控制崩溃后的接管窗口，仍必须大于该部署可观测到的最长锁/归档异常恢复窗口；若归档可能超过此时长，应改用具备独立续租通道的外部锁。若各 Hub 使用独立数据库或跨区域部署，仍必须给 `SchedulerOptions.DistributedLock` 注入 Kubernetes Lease、Consul session 等真正跨主机锁；SQLite sidecar 不能替代共识系统。未注入时只保证单进程不重入，不会假装已经完成跨主机 leader election；锁被其它节点持有时本轮返回 `ErrSchedulerLocked`，不计为备份失败。

调度状态写入 `output_dir/scheduler-status.json`，采用临时文件、`fsync`、原子 rename；状态只包含时间、归档路径、SHA-256、generation id、写暂停、dry-run duration、本地失败和异地失败详情及连续失败次数，不含密钥或业务内容。即使本地代际成功但异地发布失败，`LastSuccessAt` 仍记录本地恢复点，同时保留 `LastOffsiteError` 作为 degraded 信号，下一次异地发布成功后清除。`hub backup status` 和全局管理员接口 `GET /api/admin/backups/status` 都只读该文件，不打开 Hub 数据库；接口明确区分 `enabled`、`available` 和 `error`，便于 systemd/Kubernetes probe、管理台或外部监控使用。

#### 11.17.2 异地/不可变存储边界

新增 `backup.ArchiveDestination` 接口：部署方可以注入 S3 Object Lock、WORM、磁带网关或其它异地实现；发布请求携带归档文件的大小和 SHA-256，目标必须对同一 key 执行“相同摘要幂等、不同摘要拒绝覆盖”。接口的凭据、跨地域复制、对象锁合规和最终一致性不由 Hub 猜测或代管。`app.BootstrapWithOptions` / `backup.NewSchedulerFromConfigWithDependencies` 现在提供显式接线边界，可在不改 YAML 的情况下注入异地目标、外部锁、KMS `KeyProvider` 和告警 sink；普通 `Bootstrap` 仍使用本地安全默认值。

仓库内仅提供 `FileArchiveDestination` 作为挂载卷/测试实现。它使用同目录临时文件 + `fsync` + 原子 rename，并在发布前复核源文件摘要；它不代表真正异地或不可变存储。配置 `backup.offsite_dir` 时调度器会发布到该目录；没有成功的异地发布且 `require_offsite_for_deletion=true` 时，Hub 不删除本地旧归档，宁可暂时超出本地保留数。

#### 11.17.3 保留和删除策略

本地归档只匹配 `maclaw-hub-backup-*.tar.gz`，按文件修改时间保留 `retain_local` 个；状态文件、临时文件和人工放入目录的其它文件永不删除。仓库提供的文件异地目标同样只把该前缀的归档交给保留清理，挂载卷中其它 `.tar.gz` 不会被误删。异地目标在显式配置 `retain_offsite` 后按 `CreatedAt/key` 排序调用接口删除，删除动作始终在成功发布之后发生。异地实现可以在 `Delete` 内执行对象锁到期、审批或拒绝删除；Hub 将拒绝视为本次调度失败并告警。

归档生成时会跳过整个输出目录（而不仅是当前临时文件），避免把旧归档和 `scheduler-status.json` 再次打包进下一代；输出目录位于 data 目录外时则不会误跳过其它 data 文件。若手工把输出直接放在 Hub `data` 根目录，系统会 fail-closed 拒绝，而不是冒险递归打包整个 live tree。归档发布和异地文件发布都使用原子“不可覆盖”操作，重复 key 只有在摘要和大小完全一致时才幂等成功。

这些策略只作用于归档副本，不触碰在线 SQLite、Cloud Workspace 对象或 keyring。任何删除失败都会保留剩余副本并记录失败，不通过 `RemoveAll` 清空目录。

#### 11.17.4 dry-run restore、RPO/RTO 与告警

每次成功归档按 `dry_run_interval_seconds` 周期执行一次 `Restore(..., DryRun:true)`。若未配置目标目录，使用临时目录，完整走归档 SHA-256、SQLite `quick_check`、Cloud Workspace 全树和 key provider 校验，不会修改在线 Hub。演练失败不会伪装成备份失败，但会发出 `restore_dry_run_failed`，并保留最近成功归档状态。

调度器通过 `AlertSink` 输出以下稳定事件类型：

- `backup_status_unreadable`：调度状态文件损坏或不可读；Hub 仍会尝试备份并在成功后原子重建状态；
- `backup_failed`：创建、校验、保留清理或状态持久化失败，并附连续失败次数；
- `offsite_failed`：异地发布或异地保留清理失败；
- `restore_dry_run_failed`：周期性恢复演练失败；
- `write_pause_exceeded`：归档 manifest 的 `write_pause_ms` 超过阈值；
- `restore_duration_exceeded`：dry-run restore 耗时超过 `alert_restore_duration_ms`（RTO 代理阈值）；
- `rpo_exceeded`：本次执行开始时，距上一次成功代际超过 `alert_rpo_seconds`。

Hub 默认将事件写入结构化日志，同时暴露 `AlertSink` 供部署方接入 SMTP、PagerDuty 或 Prometheus；不会在核心包内绑定某个第三方告警 SDK。`created_at`、`completed_at`、`write_pause_ms` 和 restore `duration_ms` 仍是量化 RPO/RTO 的原始样本：生产 SLO 必须加上调度间隔、异地上传延迟、Hub 启动与客户端重连时间，不能从默认值推导通用承诺。

#### 11.17.5 配置示例与边界

```yaml
backup:
  enabled: true
  interval_seconds: 86400
  output_dir: ./data/backups
  include_logs: false
  retain_local: 7
  retain_offsite: 30
  offsite_dir: /mnt/immutable-hub-backups
  require_offsite_for_deletion: true
  dry_run_interval_seconds: 604800
  alert_rpo_seconds: 172800
  alert_write_pause_ms: 5000
  alert_restore_duration_ms: 30000
  distributed_lock_lease_seconds: 1800
```

`offsite_dir` 只是文件目标示例，且必须位于 Hub `data` 目录之外；生产应替换为实现 `ArchiveDestination` 的不可变异地适配器。相同 SQLite 数据库的多 Hub 进程已默认使用 `SQLiteRunLock`，通过 `distributed_lock_lease_seconds` 控制崩溃接管窗口；跨区域或独立数据库仍必须注入 Kubernetes Lease、Consul 等共识锁，不能把 SQLite row 当作跨主机 leader election。调度器不会自动生成 KMS 凭据、不会把环境 provider 的 secret 写入归档，也不会在异地上传失败时删除本地唯一副本。当前仍需部署方完成：多区域对象锁/版本保护、密钥销毁审批、告警接线、容量预算，以及按真实数据量周期性执行新 Hub 恢复演练并记录 RPO/RTO 分位数。

因此当前正确文案更新为：“Cloud Workspace 已具备版本化密文、可恢复 key rotation、安全退休、一致性 backup v2 和可观测的持续备份调度；异地不可变存储、告警后端与生产级 RPO/RTO 仍由部署方接入和实测。”

### 11.18 本轮 watcher 自愈持久化与本地锁竞态收敛（2026-09-01）

前一轮 watcher 自愈只在内存 mount 上设置 `reconcile_required`。如果 fsnotify 队列溢出后 GUI 在完整扫描前退出，重启时仅凭 `manifest_revision` 无法证明本地所有路径都被观察过；当树恰好与远端摘要相同或变化发生在 debounce 窗口内时，系统可能把不完整的观察误当成已完成同步。

本轮将自愈标记写入 writer cache 的 `.maclaw-cloud/state.json`：

- `reconcile_required`、`reconcile_reason`、`reconcile_at` 采用临时文件 + fsync + rename 原子落盘；
- watcher backend error、队列溢出、新目录监视失败都会设置该标记，并向前端发出 `reconcile_required=true` 事件及一次 warning toast；
- 下次 `PrepareCloudWorkspace` 在获取新 fencing epoch 后先恢复该标记，再按远端 revision/本地 baseline 执行完整 scan；成功的 manifest Pull/Push（包括 no-op commit）才清除标记；
- 只读隔离缓存不持久化该标记，避免临时目录删除后留下虚假的恢复信号；
- 标记写入失败不会放行隐式覆盖，mount 仍保持内存中的 fail-closed 状态并等待下一次显式接手。

同时收敛 writer.lock 的 owner 校验竞态。旧路径是“读 owner → Remove”，旧进程可能在校验后、删除前撞上新进程的 stale takeover，从而误删后继 owner 的锁。本轮新增同目录 `writer.lock.guard` 的 O_EXCL 短期互斥：获取、stale 回收、owner-scoped release 和 heartbeat 的检查/修改均在 guard 内完成；guard 自身按保守 TTL 可回收，无法取得 guard 时释放路径宁可放弃删除，heartbeat 则降级为只读，避免破坏后继 writer。

新增回归覆盖：

1. watcher 标记跨进程重启可读取，成功 manifest commit 后清空 reason/时间；
2. 只读隔离缓存不会写入 durable reconcile marker；
3. writer.lock 后继 owner takeover 时，旧 owner 的延迟 release 不会删除新锁；
4. guard 残留可在 TTL 后回收，活动 guard 不会被并发 acquire 抢占。

这一步只解决本地 watcher/锁的持久化与竞态，不改变“单写者、非并发编辑”协议，也不替代跨 Hub 共识锁、外部 KMS、不可变异地存储或生产规模 RPO/RTO 演练。当前对外文案继续使用“跨设备接续同一云端任务（单写者）”。

### 11.19 本轮执行环境 profile 与差异诊断（2026-09-01）

为关闭 R2-2 的“文件树一致但执行结果不同”盲区，本轮在现有 `task.json` sidecar 中增加可选 `environment_profile`，不引入新的文件事实来源或并发协议。profile 只保存诊断所需的非秘密摘要：

- 当前 Hub 客户端的 `OS/arch/runtime`；
- 根目录下常见 toolchain 声明（`.nvmrc`、`.python-version`、`.tool-versions`、`rust-toolchain*`）的截断文本；
- 常见 lockfile（`go.mod/go.sum`、npm/pnpm/yarn、Cargo、Poetry、Pipfile、requirements）的 SHA-256 + 大小；
- 受限环境变量白名单（`NODE_ENV`、`GOFLAGS`、`GOPROXY`、`RUSTFLAGS`、`PYTHONPATH`、`VIRTUAL_ENV` 等）的 SHA-256，绝不上传原值。

写入 task sidecar 时由当前设备采集 profile；接手设备读取远端 profile 后重新采集本机 profile，若 OS/架构、runtime、toolchain、lockfile 或关键环境变量摘要有差异，GUI 显示 warning toast，提醒用户先安装/切换依赖或改用远程/容器执行。`collected_at` 不参与比较，避免每次 flush 因时间变化产生伪差异；扫描跳过 `.git/node_modules/vendor/.venv`，并限制最多 128 个元数据文件、单文件 16 MiB，防止 profile 变成大工作区全量扫描。

该 profile 是“差异可见性”而非可复现构建保证：系统不会自动安装依赖、同步密钥或上传环境变量明文。需要可复现执行时仍应接入容器/远程执行后端；R2-3 本地缓存加密与撤权密钥擦除、R2-4 hash index/带宽背压/可取消 reconcile 仍是后续边界。新增测试覆盖 profile 的 lockfile 摘要、秘密不落盘和差异分类，确保 task sidecar 旧数据仍可正常解析。

### 11.20 本轮增量扫描、传输背压与可取消同步（2026-09-01）

本轮继续收口 R2-4，但不改变单写者协议：

- writer cache 在 `.maclaw-cloud/hash-index.json` 保存 `path → size/mtime/file-id/SHA-256`。扫描命中相同元数据时复用摘要，原子替换或元数据变化会重新 hash；索引损坏、缺失或写入失败只退化为全量 hash，不影响正确性。扫描使用可取消的分块读取，并在文件被编辑器替换时有限重试，拒绝发布混合代际。
- 单 workspace watcher 采用一项 coalescing 队列：运行中再次变化只留下一个 pending pass，不创建无界 goroutine；跨 workspace 的对象上传受两个全局 transfer slot 限制，等待 slot 也遵循 context 取消，避免单个大工作区占满 Hub/磁盘 IO。
- 后台同步发出 `cloud-workspace-sync-progress`（scan/upload/download/done），包含文件/字节进度和终态；`CancelCloudWorkspaceSync(workspace_id)` 停止当前可取消边界，取消或失败会保留 `reconcile_required`，下次接手先完整扫描。侧边栏显示进度并提供取消按钮。
- 下载、push、远程删除、远程 purge 和本地 cache purge 追加到机器本地 `cloud-workspace-audit.jsonl`（仅标识、版本、文件数/字节数，不含内容/密钥），日志 4 MiB 轮换并以 0600 创建；审计写失败不会把已完成的同步回滚。

验证：新增 hash-index 复用/失效/损坏回退及审计脱敏测试；GUI Cloud Workspace 回归、Wails binding drift 与 TypeScript 检查通过。该切片仍不是本地文件透明加密：工作区为任务执行而保持明文，撤权后的密钥擦除、DPAPI/Keychain/credential-vault 接入和下载/删除审计的集中后端留存仍需后续实现。R2-4 的进度是客户端可取消与有界背压，尚未提供跨进程/跨设备的全局带宽配额或可恢复的持久 hash index（当前索引是 cache-local，可丢失重建）。

### 11.21 本轮同步代际一致性与接管竞态加固（2026-09-01）

本轮继续围绕 v1-sequential 的“单写者、非并发编辑”边界，修复了同步实现中两个容易被忽略的 TOCTOU 窗口，以及租约被接管时后台回调仍可能存活的竞态。

#### 11.21.1 上传前后的文件代际校验

扫描阶段得到的 SHA-256 不能单独作为提交事实：编辑器可能在扫描结束、对象上传开始之前原子替换文件；也可能在对象上传完成、manifest 提交之前再次替换文件。旧代码在远端对象恰好已经存在时甚至不会重新读取本地文件，可能把旧扫描结果写进新 manifest。

现在每个待发布路径都会在上传前记录 `size/mtime/file-id`，读取实际字节后再次校验长度和 SHA-256，并在上传完成后复核文件身份。远端已有同摘要对象的路径也会做身份复核；如果检测到替换、删除或大小变化，本轮 Push 立即失败，保留 `reconcile_required`，不会提交混合代际。远端树与本地树看似相同的 no-op Push 同样复核扫描时的文件身份，避免 watcher 变化被 no-op 快速路径吞掉。

#### 11.21.2 下载覆盖和删除的并发保护

Pull 现在先记录本地非忽略文件的初始身份。每个远端文件在网络下载前、下载完成后都会确认本地仍是同一个文件；已存在且摘要相同的文件使用可取消 hash，并在 hash 后再次复核身份。远端不存在的本地文件只有在整个下载期间保持未变化时才允许删除；下载期间新建或编辑的文件会使 Pull 失败而不是被静默覆盖/删除。

这仍不是操作系统级文件锁：外部进程可以在最后一次 stat 和 `rename` 之间写入文件，底层文件系统也可能不提供稳定 file-id。因此该机制的承诺是“发现可见的替换/修改就 fail-closed”，而不是宣称对任意恶意并发写入具备事务快照。需要强一致编辑快照时，应在执行环境层使用容器快照或显式暂停写入。

#### 11.21.3 Fencing 后台同步生命周期

心跳收到 `FENCED` 或租约过期时，mount 先停止 watcher、取消后台 sync，并等待已发布的 `syncDone` 再允许接管方复用本地 cache。同步回调自身不能等待自己的完成 channel，因此使用 no-wait fencing 分支；普通心跳/关闭路径使用 wait 分支。`syncDone` 直到回调的 pending 决策完成才清空，同时 mount 增加 `stopped` 标记，防止旧回调在本机 writer lock 释放后重新创建 debounce timer。

取消、fencing、文件代际变化和下载并发冲突都会保留 durable `reconcile_required`，下一次显式 Prepare 重新扫描并以新 fencing epoch 接手；不会回退到 `Shared` 或无 lease 写入。

Force purge 现在会先停止本进程持有的 mount，再按专用目录边界清理 writer cache 和所有 `cloud-workspaces-readonly/{tenant}/{workspace}/{instance}` 隔离副本；不会因为只删除 canonical writer 路径而把浏览缓存遗留在磁盘上。成功的 entitlement 响应若把 workspace 放入 deleted（撤权）集合，也会触发同一清理路径；网络/5xx 错误不会触发破坏性删除。清理失败会返回错误并写入脱敏后的审计原因码。

同时收紧本地 cache ID 校验：拒绝 `.`、`..`、首尾空白和分隔符，避免任何由本地 API 传入的 workspace 标识逃逸到专用目录之外。

#### 11.21.4 验证与边界

新增回归覆盖：

1. scan 后文件替换会阻止 Push，且不会调用对象上传；
2. Pull 下载期间本地编辑会被拒绝，编辑内容保持不变；
3. fencing/steal 处理等待已发布 sync 完成，且不会触发自等待死锁；
4. 既有 hash-index、背压、取消、watcher 自愈和网络分区测试继续通过。

本轮不改变“跨设备接续同一云端任务（单写者）”产品契约，也不声称完成本地工作区透明加密、DPAPI/Keychain、跨设备带宽配额、持久 hash index 或 E2E。R2-3/R2-4 的这些边界仍需独立设计和部署验证。

### 11.22 本轮永久删除 fail-closed 与本地缓存路径加固（2026-09-01）

本轮继续补齐删除和本地路径的安全边界：

1. **删除操作不再吞掉本地释放失败**：软删除和永久删除前如果当前进程仍持有 writer mount，会先撤销该任务 loop 的后续语义尝试，再完成最终 Push、sidecar flush 和 lease release。任一步失败都会返回错误、保留可恢复 mount，并且不会调用远端删除接口（软删除或 `/purge`）；避免在唯一的本地未同步副本仍存在时先删除云端数据。普通 Hide/关闭路径仍可采用保留 lease 的恢复策略，只有显式删除 API 使用该 fail-closed 规则。
2. **404 purge 仍执行本地清理**：Hub 已经删除工作区时把 404 视为幂等成功，但继续删除 writer 与所有只读隔离缓存，避免远端“已不存在”被误当作可以跳过本地隐私清理。
3. **workspace/tenant 本地路径防御纵深**：所有写入入口拒绝路径型、控制字符、Windows 保留设备名、尾随点和超长 workspace ID；租户值即使来自异常配置也先映射为稳定 SHA-256 目录名。创建缓存目录逐级 `Lstat`，拒绝符号链接/reparse point；读取已有缓存和 purge 前复核 `EvalSymlinks` 仍位于专用根目录，Pull/PullEvents 写入远端嵌套路径前也逐级复核父目录，避免 lexical `Join` 检查被目录链接绕过。

新增回归覆盖 ForceDelete 404 仍清理所有副本、本地最终 Push 失败时不触发远端 purge、workspace ID 及租户目录的路径边界。该加固仍不等同于操作系统级文件锁或透明加密：工作区在执行期间保持明文，SSD 上的删除也不提供取证级擦除保证；DPAPI/Keychain/credential-vault、撤权后的密钥擦除和跨设备配额仍需后续接入。

### 11.23 本轮本地缓存静态加密与撤权密钥销毁（2026-09-01）

本轮补齐 R2-3 中最容易被忽略的本地边界：Hub 端对象已经加密，并不意味着 GUI 的 writer cache 也安全。现在可以通过 `cloud_workspace_cache_encryption: true`（或环境变量 `MACLAW_CLOUD_WORKSPACE_CACHE_ENCRYPTION=1`）开启本地缓存保护。

#### 11.23.1 明文窗口与密钥来源

- 执行期间仍使用普通目录，Agent、编辑器和 watcher 不需要改造；停止 writer mount 时才执行 seal，避免把密文目录误交给运行时。
- 每个 `tenant + workspace` 使用 32 字节独立密钥，存放在 Windows Credential Manager、macOS Keychain 或其它 `go-keyring` 后端；缓存目录不写入密钥。keyring 不可用时 seal/unseal fail-closed，不生成可恢复不了的临时密钥。
- 密钥指纹是 SHA-256，仅用于诊断和防止错误 key 被接受；当前实现不声称 E2E，Hub 管理员仍可读取服务端明文对象。

#### 11.23.2 文件级 envelope 与崩溃恢复

seal 将工作区中所有常规文件（包括 `.maclaw-cloud/state.json` 和 hash index）拆成 1 MiB 分块，用 AES-GCM 独立认证后写入 `.maclaw-cloud/cache-sealed-files/`。路径映射、权限、大小和明文 SHA-256 再作为加密 manifest 写入 `cache-sealed.v1`；密文文件名不包含原始路径。写入流程是“临时目录 → fsync 文件 → 原子 rename → marker → 删除明文”，marker 之前崩溃只会留下可清理的孤儿临时目录，marker 之后崩溃则由下一次 Prepare 继续 unseal。

unseal 必须先持有本机 writer lock，完整解密并校验每个文件的 AEAD、大小和 SHA-256；发现 marker 损坏、key 被旋转/销毁、额外明文文件或目标文件在恢复期间变化时直接失败，不覆盖脏数据。这样保护的是“未挂载时磁盘副本”，不是操作系统级事务快照。

#### 11.23.3 删除与撤权

ForceDelete 和成功的撤权本地 purge 在删除所有 writer/read-only 副本后，额外删除该 workspace 的 keyring 条目。任何副本路径不安全、删除失败或 keyring 删除失败都会返回错误并写审计；远端已 404 仍继续执行本地清理。普通 Release 不删除密钥，后续重新接手仍可解密历史缓存；只有显式永久删除或 Hub 已确认 revoked 才进入不可逆密钥销毁。

#### 11.23.4 配置、验证与剩余边界

本地加密默认关闭以兼容旧安装，首次启用时会在第一次 Release 生成 key 并 seal，第一次 Prepare 自动 unseal。新增测试覆盖 seal/unseal 往返、key 销毁后的 fail-closed、额外明文拒绝和 keyring 注入；GUI Cloud Workspace 回归、race、Wails binding drift 与 TypeScript 检查继续通过。

该切片仍有明确边界：不提供外部 KMS 的具体 adapter、跨设备共享同一 GUI keyring、E2E、操作系统级 secure erase 或透明的在线随机访问加密文件系统；磁盘删除只保证逻辑不可再打开，不能承诺取证级擦除。生产启用前应确认终端已部署可用的 DPAPI/Keychain/credential-vault 后端，并把 keyring backup/recovery 纳入与 Hub backup v2 相同的恢复演练。

### 11.24 本轮 Hub 集中审计留存（2026-09-01）

本轮补齐 11.20 中“审计仅在机器本地”的根本缺口。GUI 仍先把事件追加到本机 `cloud-workspace-audit.jsonl`，同时通过有界后台队列 best-effort 上报 Hub；本地记录不会因网络分区或 Hub 失败而回滚，队列满时也不创建无界 goroutine。

Hub 新增 `cloud_workspace_audit_events` 表以及：

- `POST /api/v1/cloud-workspaces/{id}/audit`：写入不要求当前 lease，但必须通过机器认证、实例 session、租户所有权和 `v1-sequential` 协议协商。服务端使用 server time，`event_id` 唯一，重复上报幂等；
- `GET /api/v1/cloud-workspaces/{id}/audit?after_id=&limit=`：按递增数据库游标分页，避免依赖客户端时钟。记录包含机器/实例、操作、结果、revision、文件/字节计数和有限 reason code，不含路径、正文、URL、token 或密钥。

服务端对 operation/outcome/reason、revision、计数和 event id 做 allowlist/长度校验；未知的自由文本不会落库。审计行不与 workspace 建立外键，永久 purge 后仍可保留历史证据；具体保留期和归档由部署方按合规要求配置，当前实现不自动删除审计历史。该接口用于集中诊断和合规留痕，不把审计写入提升为文件提交事务的一部分：上报失败只产生本机可观测的 degraded 信号，不阻塞或伪造同步成功。

新增回归覆盖：重复 event id 只产生一条记录、跨用户读取被拒绝、自由文本/路径型字段 fail-closed、HTTP 上报与游标分页。仍需部署方接入审计导出、保留/删除策略和告警后端；集中留存也不等于 E2E，Hub 管理员仍可读取元数据。

### 11.25 本轮永久删除后的审计授权闭环（2026-09-01）

11.24 的集中审计还有一个时序缺口：`remote_purge` 成功后工作区行会被硬删除，而审计写入原先仍通过 `GetOwned` 校验。这样 GUI 在收到 204 后再异步上报的最终 purge 记录会得到 `404`，正好丢失最需要保留的“已永久删除/已清理”证据；同时为了绕过这个问题而放宽到“仅凭 workspace id 写审计”又会破坏租户隔离。

本轮增加 `cloud_workspace_audit_tombstones`：

- `HardDeleteDeleted` 在同一个 SQLite `BEGIN IMMEDIATE` 事务中先写入 `workspace_id + tenant_id + user_id + purged_at` 的最小 ownership anchor，再删除 workspace、manifest、snapshot、sidecar、object 和 lease 行；文件清理失败时事务不会提交，anchor 不会伪造为已完成 purge。后台 GC 的 `HardDeleteExpired` 使用同一 anchor，离线设备重连后也不会因自动过期清理而丢失最终审计授权。
- `RecordAudit` 和 `ListAudit` 先检查当前 workspace ownership；若 workspace 已被硬删除，再仅凭匹配的 tenant/user tombstone 放行审计读写。该 anchor 不授予 lease、对象、manifest 或 restore 权限，只能访问无内容的审计记录。
- tombstone 不含文件内容、路径、token 或密钥，也不参与当前文件树、配额和 GC。它与审计表一起由部署方 retention/归档策略治理，避免为“审计可追溯”重新保留工作区数据。
- 新增有界 `PruneAudit(before, batch)` retention 原语：每批最多删除 10000 条 cutoff 前的审计行，只在某个 tombstone 已过期且没有任何保留审计行时才删除 ownership anchor。它不触碰 workspace/object/manifest 数据，也不会默认自动运行；部署方必须先完成所需导出，再由合规调度器显式调用并重复到本批删除数为零。

新增回归覆盖：硬删除后 owner 仍能追加并分页读取 `remote_purge/already_gone`，其它用户即使知道 workspace id 仍被拒绝；retention 删除过期事件后同时收敛无引用 tombstone，迟到设备不能重新复活已超出保留期的审计流。这样 GUI 的 best-effort 上报在正常 purge、Hub 已返回 404 的幂等 purge 两种路径都能落入集中审计；上报仍不是文件提交事务的一部分，网络分区时继续依赖本地 JSONL。当前仍需部署方接入审计导出和告警，并选择具体保留期；代码不会替合规策略猜测默认删除时间。

### 11.26 本轮 purge 两阶段提交与恢复重试（2026-09-01）

在 11.22 的 fail-closed 之后仍存在一个跨介质 TOCTOU：硬删除先 unlink 本地对象目录，再提交 SQLite 删除；如果进程在两步之间崩溃，重启后没有 durable 信号知道应该继续清理，反之若 SQLite 最终提交失败，也可能留下“工作区行仍在但对象已不在”的半状态。恢复和显式 purge 现在统一使用两阶段 intent：

- 新增 `cloud_workspace_purge_intents`，由 `EnsurePurgeIntent` 在 `BEGIN IMMEDIATE` 中写入，并把 workspace 状态从 `deleted` 原子切换到 `purging`；`Restore` 不接受 `purging`，因此不会在 unlink 期间重新激活同一 workspace。
- GUI/GC 只有在 intent durable 后才删除文件；随后 `HardDeleteDeleted`/`HardDeleteExpired` 在同一 SQLite 事务中写审计 tombstone、删除 manifest/snapshot/sidecar/object/lease 等元数据，并清除 intent。任一 DB 步骤失败都会回滚，intent 和 `purging` 行留给下一次重试。
- `Sweep` 每轮优先消费最多 1000 个 pending intent；进程崩溃、unlink 权限错误或最终提交失败都能在后续轮次继续，已恢复为 active 的异常 intent 会被安全清除而不会删除工作区文件。对象 GC 同时排除 `purging` 状态，避免全量 purge 与单对象回收互相竞态。

新增回归覆盖：Blob 根目录暂不可用时第一次 sweep 只留下 `purging + intent`，恢复目录后下一轮自动完成删除；intent 存在时 restore 被拒绝。这样“文件删除”和“数据库删除”之间从不可恢复窗口收敛为可观测、可重试状态，但仍不把本地文件系统变成跨设备事务存储；生产仍需监控 pending intent 数量和 GC failure 日志。

### 11.27 本轮审计管理员分页导出（2026-09-01）

11.25/11.26 已经保证硬删除后的审计授权和 purge 恢复，但运维侧仍缺少不依赖机器 token 的集中导出入口。本轮新增租户管理员 API：

```text
GET /api/admin/cloud-workspaces/audit/export
    ?format=csv|jsonl
    &workspace_id=<optional>
    &after_id=<exclusive durable id>
    &limit=1..10000
```

导出严格要求 `tenant` scope 的管理员；`tenant_id` 取自已认证的管理员上下文，不能通过 query 覆盖。`workspace_id` 只是同租户内过滤条件，不会把已删除工作区重新授予对象、manifest、lease 或 restore 权限。全局管理员必须先切换到明确的租户管理员身份，避免“全局默认租户”误导出。

CSV 返回表头和一页记录，JSONL 每行一个记录；两种格式均通过 `X-Audit-Next-After-ID` 与 `X-Audit-Has-More` 返回下一页游标。服务端一次最多读取 `limit+1` 行判断是否还有更多，默认 1000、上限 10000，避免大租户导出占满 SQLite 内存或连接。重复请求使用同一游标即可安全重试；审计行的递增数据库 `id` 是唯一分页事实，不依赖客户端时间。

导出字段只包含 `id/event_id/tenant_id/user_id/workspace_id/client_instance_id/machine_id/operation/outcome/revision/files/bytes/detail/created_at`。`operation/outcome/detail/revision` 沿用服务端 allowlist；CSV 额外中和换行和常见电子表格公式前缀，防止导入时执行；不会返回路径、正文、URL、token、密钥或自由文本。管理员导出本身不写入文件提交事务，也不改变 retention；部署方仍应先把分页结果归档到合规存储，再显式调用 `PruneAudit(before,batch)`，并监控导出失败、pending purge intent 与审计队列积压。

本轮新增回归覆盖：租户管理员 CSV/JSONL 导出、游标分页和 workspace 过滤；无管理员或 global scope 直接调用被拒绝；查询只返回当前租户的记录。该入口是运维导出原语，不代表已经接入具体 SIEM、WORM 或自动 retention 调度，生产仍需由部署方配置导出凭据、加密传输、保留期限和告警策略。

同时，`GET /api/admin/cloud-workspaces/metrics` 对租户管理员改用 tenant-scoped 投影：工作区/租约/审计行、审计 tombstone 和 `pending_purge_intents` 只统计当前租户；物理卷可用空间及进程级同步计数仍明确标为安装级指标。这样管理台可以直接对 pending intent 和审计积压设置告警，而不会因读取配额或 GC 统计泄露其它租户的工作区用量。跨租户汇总仍应由全局监控系统在受控环境中完成，核心 API 不提供“默认租户”兜底导出。

`Metrics` 另增加 `gc_failures` 单调计数；每次 GC 失败事件（包括 purge intent 重试失败）都会递增，同时继续写入既有 `failure_event_logs`。该计数只表示进程生命周期内的发生次数，重启后归零，不能替代 durable failure log 的历史查询；生产告警应同时观察 `gc_failures`、`pending_purge_intents` 和失败日志的最近时间。

为避免租约历史表增长后指标与管理查询退化为全表扫描，SQLite migration 额外建立 `(tenant_id, released_at, expires_at)` 复合索引；它只优化查询路径，不改变租约唯一约束或过期/接管语义。

### 11.28 本轮租户隔离的审计 retention 运维入口（2026-09-01）

仅有 `PruneAudit(before,batch)` 原语仍不足以安全接入运维：该原语是全局调用，若直接暴露给租户管理员会产生跨租户删除风险。本轮新增 tenant-scoped 版本 `PruneAuditForTenant`，并接入：

```text
POST /api/admin/cloud-workspaces/audit/prune
{
  "before": "<RFC3339 timestamp>",
  "batch": 1000
}
```

入口只接受 `tenant` scope 管理员，租户 ID 取自认证上下文，不能由请求体或 query 覆盖。`before` 和 `batch` 均为必填；未来时间戳、空租户和超过 10000 的批次直接拒绝，避免时钟/配置错误导致“删除全部”。每次调用最多删除一批审计记录，并在同一事务内只收敛当前租户下已无保留审计行的 tombstone；不会触碰 workspace、manifest、object、snapshot 或 purge intent。成功调用写入现有 admin audit log，记录 cutoff、批次和删除数量，不记录任何审计正文。

该接口仍是显式运维原语，不自动运行 retention，也不替代“先分页导出到合规存储、再按策略删除”的流程。部署方应由合规调度器按租户重复调用直到 `events=0`，并对 retention 失败、导出遗漏和 pending purge intent 设置告警。全局管理员若需处理多个租户，应在受控流程中逐租户授权，不能依赖默认租户兜底。

### 11.29 本轮 GC 写入竞态、幂等响应与 Hub 路径防御（2026-09-01）

本轮针对实现审计又收口了三类根本风险：

1. **对象 GC 与新写入的竞态**：对象进入 `deleting` 后，新的 `PrepareObjectPut` 不再把该行重置为 `staging`；调用方收到可重试的 `CLOUD_WORKSPACE_OBJECT_DELETING`，等待 GC 完成后再上传。这样 GC 的“标记 → unlink → 删除元数据”期间不会出现 ready 元数据指向已被删除文件的状态，也不需要放宽单写者协议。
2. **Complete 幂等响应不完整**：对已完成对象重复调用 `complete` 时，从 durable metadata 返回真实明文大小，避免客户端因大小为 0 误判上传进度或配额。
3. **Hub 本地路径与 sidecar 指针 fail-closed**：tenant/user/workspace 目录段拒绝控制字符、冒号、首尾空白/点及 Windows 保留设备名；sidecar 的 `storage_file` 必须是单一安全文件名，损坏或导入的 `..`/绝对路径直接返回 `ErrBlobCorrupt`，同时数据库读取遵循请求 context，避免取消请求后继续占用连接。

此外，SQLite migration 不再无条件吞掉所有 `already exists` 错误；只有显式 `IF NOT EXISTS` 的语句或 `ALTER TABLE` 的重复列可忽略，冲突对象定义会立即失败，防止半升级数据库被误当作成功。新增回归覆盖上述路径和对象删除状态，未改变 v1-sequential 的单写者、多读者契约。

审计事件 ID 也改为“同 ID 必须同 payload”：重试相同事件仍幂等，若同一工作区复用 ID 但 operation/outcome/revision/计数或实例身份不同则返回 `AUDIT_EVENT_ID_REUSED`，避免恶意或误配置覆盖真实审计语义。

### 11.30 本轮跨进程/跨设备带宽配额（2026-09-01）

R2-4 最后一块未闭环的仓库内功能是"跨进程/跨设备的全局带宽配额"：此前 GUI 只有进程内的两个 transfer slot，Hub 的 `sync_bytes_up/down` 也只是进程内计数器，不落库、不跨进程共享、不做任何拒绝。本轮把它收敛为 Hub 端按 UTC 整点滚动窗口的持久配额，不改变 v1-sequential 协议。

#### 11.30.1 计量模型

- 新增 `cloud_workspace_bandwidth_usage` 表，主键 `(tenant_id, user_id, window_start)`，`window_start` 为 UTC 整点（RFC3339，字典序即时间序）。没有后台滚动任务：admit 时按当前时刻截断到整点定位窗口行，同事务内惰性删除 48 小时前的旧窗口。
- `admitBandwidthDelta` 只在 `BEGIN IMMEDIATE` 写事务内执行"读当前值 → 判断 → UPSERT 增量"，共享同一 SQLite 的多个 Hub 进程因此串行化，不会出现两个设备同时越过限额。限额为 0 表示该级不限，两级都为 0 时 Service 短路、完全不触碰数据库，存量部署的热路径零开销。
- 租户级限额用同事务 `SUM` 聚合同租户当前窗口的所有用户行，不需要双写或汇总表。
- 配置走既有租户 settings（`bandwidth_user_bytes_per_hour` / `bandwidth_tenant_bytes_per_hour`），默认 0 = 不限；clamp 只设上限，0 保留为显式"不限"，不像容量字段那样被替换为默认值。

#### 11.30.2 计费口径

与 retained quota 一样按明文字节计费，压缩/加密参数不影响账单：

- 整对象 PUT 与分块 PUT 在 lease 校验后按请求字节前置计入；**失败或中止不退款**，幂等重传（ready 对象的重复 PUT/chunk）也计入——字节确实跨过了网络，保守方向避免重试循环放大传输。
- GET object 先用 `cloud_workspace_objects` 的 durable 明文大小做准入，超限直接 429，不读取、不解密 blob；staging/deleting 行与缺失对象维持原有 `ErrBlobNotFound` 语义。
- sidecar GET/PUT 计入（上限 64 MiB，可能是大对象）；sidecar 无 durable 大小列，下载按实际明文长度后置计入，但超限窗口仍 fail-closed 返回错误。
- manifest/delta/audit/lease 等小请求不计入。

超限响应是 429 + `CLOUD_WORKSPACE_BANDWIDTH`，body 携带 `retry_after_seconds` 并设置 `Retry-After` 头，值为当前窗口剩余秒数。拒绝计入独立的 `bandwidth_rejections` 指标（不混入 `quota_rejections`）；metrics 另暴露当前窗口的 `bandwidth_window_bytes_up/down`，租户管理员投影只统计本租户。

#### 11.30.3 GUI 行为

`cloudWorkspaceAPIError` 把 429 映射为带重试窗口的 `errCloudWorkspaceBandwidthLimited`（提示"云端同步带宽已达本小时限额，窗口重置后自动重试"）。后台同步遇到该错误时保留 `reconcile_required`、不再立即重排 pending，而是在 mount 上 coalesce 一个 `retry_after` 到期的单次重试 timer（clamp 到 1s–1h）；timer 随 release/fencing/取消一起停止。到期前的新 watcher 事件只更新 pending，不形成热循环；手动 Prepare/接手不拦截，让服务端 429 直接反馈。

#### 11.30.4 验证与边界

新增回归覆盖：窗口内累计与超限拒绝（含 retry_after 范围）、用户/租户两级限额、跨窗口重置、0=不限且不写计量行、旧窗口惰性清理、16 并发 admit 不超限额（单写连接串行化）、PUT/GET/sidecar 三条 HTTP 路径的 429 契约与 `Retry-After` 头、metrics 的租户投影与隔离、GUI 错误映射与 retry delay 提取。

边界声明：这是窗口配额 + 客户端退避，不是令牌桶速率整形；多 Hub 使用独立数据库或跨区域部署时用量不共享（与 `SQLiteRunLock` 相同的前提）；失败上传消耗配额是刻意的保守语义。至此开头结论中的"跨设备全局配额"已落地为可选、默认关闭的租户级开关；剩余根问题收敛为外部 KMS 适配、不可变异地存储适配、告警后端、跨区域共识锁接入和按真实规模量化 RPO/RTO。

### 11.31 本轮减法：移除过度设计（2026-09-02）

对 11.15–11.30 做了一次反向评审，结论是若干子系统属于"先建能力等需求"的过度设计：它们服务的威胁模型或合规深度在 Hub-trust、单 Hub SQLite 部署下并不存在真实需求方。本轮移除这些实现，保留与之兼容的数据读取路径。本节取代 11.15–11.17、11.19、11.25–11.28 中对应能力的"已完成"描述。

#### 11.31.1 移除清单与保留面

1. **密钥生命周期（取代 11.16 的轮换/退休部分）**：删除 `RotateEncryption`/`RetireEncryptionKey`、rotation marker、`Rotatable/RetirableKeyProvider` 接口和 `hub cloud-workspace keys status/rotate/retire` 三个 CLI。保留：envelope v2 密文格式与 key id 读取（既有密文可继续解密）、文件/env 单 key 加载、旧 `master.key` 与既有 keyring 的读取兼容。密钥保护收敛为"单 master key + 随备份同卷保护"；需要 KMS 的部署应自行在进程外注入环境变量，仓库不再内置生命周期管理。
2. **持续备份调度（取代 11.17）**：删除 `backup.Scheduler`、`SQLiteRunLock`、`ArchiveDestination`/`FileArchiveDestination`、`AlertSink`、dry-run/retention/offsite 逻辑、`hub backup run` 与 `hub backup status` CLI、`GET /api/admin/backups/status` 管理接口，以及 `backup:` YAML 配置段中除 `output_dir`/`include_logs` 外的全部键。保留 `hub backup create/inspect/restore` 原语（一致性代际归档与三段式恢复不变）。持续调度由部署方用外部 cron/计划任务调用 `backup create` 实现；异地复制、对象锁、告警都属于部署脚本职责。
3. **审计 tombstone 与 retention 原语（取代 11.25/11.28）**：删除 `cloud_workspace_audit_tombstones` 表、硬删除时的授权锚点写入、`PruneAudit`/`PruneAuditForTenant` 和 `POST /api/admin/cloud-workspaces/audit/prune`。保留审计事件表、best-effort 上报、游标分页和管理员导出。**语义变化**：工作区硬删除后，迟到的审计上报返回 `ErrNotFound`；最终 purge 证据以设备本地 `cloud-workspace-audit.jsonl` 为准。审计行的保留/删除由部署方直接按 `created_at` 管理数据库。
4. **Purge 两阶段 intent（取代 11.26）**：删除 `cloud_workspace_purge_intents` 表、`StatusPurging` 状态、`Ensure/List/RemovePurgeIntent` 及 Sweep 的 intent 消费。硬删除回到"先删文件、后删元数据"的直接顺序；崩溃残留由 GC Sweep 新增的**孤儿目录 reconciliation** 回收：扫描 blob root 下三层目录，凡是没有任何 `cloud_workspaces` 行且超过宽限期的 workspace 目录直接删除（密钥目录显式排除）。`Restore` 不再需要 fence purging 状态。metrics 中的 `audit_tombstones`、`pending_purge_intents` 字段随之移除。
5. **执行环境 profile（取代 11.19）**：删除 GUI 端 profile 采集、远端比对和差异 toast；task sidecar 恢复为不含 `environment_profile` 的普通 JSON，旧数据中的该字段被自然忽略。
6. **Sidecar CAS 状态机简化（取代 11.8 起的 immutable revision 机制）**：sidecar 写入简化为 lease 校验 + If-Match（revision = 明文 SHA-256）+ 原子写 canonical 文件 + 单事务提交 DB revision 与 task binding；删除 immutable revision 文件、pending 标记、stale grace、DB 指针读取路径和 session 合并逻辑（单写者 lease 下不存在跨进程合并场景）。HTTP API 语义不变。

#### 11.31.2 保留不动的部分

v1-sequential 协议、实例 session 与 fencing、网络分区降级、统一 retained quota、增量扫描/hash index/背压/可取消、本地缓存静态加密与撤权密钥销毁（11.23，默认关闭）、集中审计留存与导出、带宽配额（11.30，默认关闭）、backup v2 归档/恢复原语。这些是"多设备接续"的直接需求。

#### 11.31.3 兼容与升级说明

- 既有数据库中的 `cloud_workspace_audit_tombstones`、`cloud_workspace_purge_intents` 表不再创建也不再读写；存量部署升级后这两张表成为无害的孤儿表，可手工 DROP。
- 旧 YAML 中被删除的 `backup.*` 键会被 YAML 解析器忽略，不报错。
- 既有 v2 envelope 密文与 keyring 文件继续可读；不会再产生新的 key 版本。
- 移除的 HTTP 端点返回 404；移除的 CLI 子命令显示用法帮助。

本轮验证：cloudworkspace/store/sqlite/backup/cmd/hub 包测试全绿，新增孤儿目录扫描回归（含宽限期与密钥目录排除），post-purge 审计拒绝语义改为显式断言。GUI 未受影响（Wails binding 无变化）。

#### 11.31.4 减法后的自审修复（2026-09-02）

对本轮改动做第二轮 review 后又收口两处：

1. **sidecar 写入顺序**：简化后的初版是"先覆写 canonical 文件、事务失败再删除"，这把旧设计的安全语义弄反了——删除失败路径会连同**上一版已提交内容**一起删掉。改为三段式：密文写入同目录临时文件并 fsync → 单事务提交 revision/binding → `fileutil.RenameAtomicFile` 落位（Windows 下带重试的覆盖 rename）。事务失败只丢弃临时文件；提交后 rename 失败则客户端按旧 baseline 重试 CAS 自愈。新增回归：DROP 表制造提交失败后 canonical 仍是旧内容、目录无残留临时文件。
2. **旧部署读取兜底**：11.31 之前 canonical 文件只是末尾的 best-effort 兼容副本，可能漏写；最新已提交内容只存在于 immutable revision 文件。读取路径在 canonical 缺失时回退到 DB 行的 revision 指针取 `name.<revision>.enc`（revision 校验为 SHA-256、name 为白名单，路径不可逃逸），避免升级后把已提交的 sidecar 误报为丢失。篡改的 DB 指针回归为 `ErrBlobNotFound`。

另做一处热路径优化：带宽配额默认关闭时，`GetObject` 不再做 `ObjectPlainSize` 预检查询，保持既有读取路径零额外开销。

第三轮自审（同日）又收口三处：

1. **中断轮换的 keyring 兼容**：旧版二进制若在轮换中途崩溃，`master-keyring.json` 会留下 `rotation_key_id != active_key_id` 的标记；删减后的加载器原先直接拒绝这种 keyring，等于把该部署的所有密文锁死。现在把 marker 视为"被中断的轮换"：只要它指向 keyring 中仍存在的 key 就以其为有效 active 继续新写入（混合代际文件本就由 envelope key id 逐文件定位），marker 指向未知 key 才 fail-closed。新增两个回归。
2. **孤儿目录扫描收窄**：生产环境 `KeyDir` 就是 blob root 本身，原先的三层目录遍历可能把 root 下任何多层嵌套的非租户目录误判为孤儿 workspace 删除。现在只有名字带 `cws_` 前缀且通过路径段校验的目录才参与孤儿判定，key 目录按绝对路径比较排除。
3. **死代码清扫**：删除 CLI 移除后无调用方的 `DescribeMasterKey`；确认 backup 包只含 `Create/Inspect/Restore` 原语族，无 scheduler-status 残留。
4. **管理后台带宽字段**：`cloud-workspace-tab.js` 的设置卡片新增"人均每小时传输（MiB/h）"和"租户每小时传输（GiB/h）"两个编辑框，0 表示不限；在此之前 settings PUT 的全量替换语义会让 UI 保存把 API 配置的带宽限额静默重置为 0，现已随保存回传并做范围校验（服务端 clamp 上限一致）。
5. **race 验证与测试环境对齐**：全量 `go test -race`（cloudworkspace、guiapp CloudWorkspace）通过。期间暴露 `newTestWorkspaceStore` 用 4 个写连接，与生产 `max_write_open_conns: 1` 的拓扑不一致，导致并发 `BEGIN IMMEDIATE` 在 race 时序下以 SQLITE_BUSY 失败；测试基建已改为单写连接（crash matrix 测试保留自己的多连接配置）。
