# maclaw GUI 迁出与迁入设计

## 目标

在 maclaw GUI 的设置中新增 `迁出与迁入` 模块，让用户可以把当前机器上的 maclaw 个人数据迁出到当前登录的 Hub，再在新安装或另一台已注册的 maclaw 客户端上迁入。

迁移范围：

- 当前用户的 maclaw 记忆数据。
- 当前用户的本地知识库数据与相关知识库文件、图片、附件资产。
- 恢复时写入当前机器 maclaw 使用的本地配置、记忆库、知识库数据库与知识库文件目录。

本设计只依赖当前 maclaw GUI 登录或注册到的 Hub，以及 Hub 上的租户、用户、客户端实例身份。它不涉及 hubcenter、skillmarket、能力市场或其它 Hub 管理模块。

## 身份与实例模型

Hub 侧以如下边界隔离迁移数据：

```text
hub + tenant_id + user_id + instance_id
```

同一个 Hub、同一个租户、同一个用户下可以存在多台电脑。每台电脑是一个 maclaw 客户端实例。

实例信息至少包含：

- `instance_id`
- `machine_name`
- `instance_name`
- `os`
- `maclaw_version`
- `last_seen_at`
- `has_export`
- `export_status`
- `export_updated_at`
- `export_size`

GUI 在迁入选择源实例时必须显示机器名。用户依靠机器名确认要从哪台电脑迁入数据。

## Hub 职责边界

Hub 只做加密迁移包的临时中转：

- 保存当前用户当前租户下唯一一份迁移包。
- 保存迁移包元信息、分片状态、hash 和迁移状态机。
- 支持分片上传、分片下载和断点续传。
- 校验分片 hash 与整体 hash。
- 在迁入完全成功后删除服务器上的迁移包。

Hub 不做以下事情：

- 不解析记忆或知识库明文。
- 不参与知识库检索。
- 不把迁移包同步到 hubcenter。
- 不长期备份用户数据。
- 不在不同租户或不同用户之间共享迁移数据。

## 迁出覆盖规则

同一个 Hub、同一个租户、同一个用户下只允许保留一份待迁移数据。

用户点击迁出前，GUI 必须提示：

```text
Hub 上同一账号只能保留一份迁出数据。新的迁出会覆盖旧的迁出数据。请确认旧数据已经不再需要。
```

迁出时用户必须输入两次迁移口令。两次口令只用于确认本次迁出使用的加密口令是否一致，不用于验证旧迁出包。

服务端规则：

- 如果已有 `uploading` 或 `ready` 迁移包，新建迁出会创建新 `export_id` 并覆盖旧迁移包。
- 覆盖必须先让旧迁移包进入 `replaced` 或 `deleting` 状态，再清理旧分片。
- 覆盖不允许影响已经被目标机器 `claim` 且仍在有效 lease 内的 `importing` 迁移包，除非旧 lease 过期。
- 任一用户在同一租户下最终只有一份可见的 `ready` 迁移包。
- 新迁出覆盖旧迁出时，不要求本次口令与旧迁出包口令一致。旧迁出包会被替换，新迁移包使用本次输入的新口令加密。

## 数据包格式

本地先构建明文迁移目录，再压缩、加密、分片上传。

目录建议：

```text
manifest.json
memory/
  entries.jsonl
  metadata.json
knowledge/
  export.jsonl
  assets/
config/
  migration_config_patch.json
```

`manifest.json` 包含：

- `format_version`
- `tenant_id`
- `user_id`
- `source_instance_id`
- `source_machine_name`
- `created_at`
- `maclaw_version`
- `memory_entry_count`
- `knowledge_source_count`
- `asset_count`
- `plain_size`
- `plain_sha256`
- `encrypted_size`
- `encrypted_sha256`
- `chunk_size`
- `chunks[]`

每个 chunk 记录：

- `index`
- `offset`
- `size`
- `sha256`

## 加密与完整性

口令只在本地使用，不上传到 Hub。

建议实现：

- 压缩：`zstd` 或 `gzip`。
- 密钥派生：`Argon2id`，随机 salt。
- 加密：`AES-256-GCM`。
- 明文包 hash：`plain_sha256`。
- 加密包 hash：`encrypted_sha256`。
- 分片 hash：每片独立 `sha256`。

迁出校验：

1. 本地生成明文包并计算 `plain_sha256`。
2. 压缩加密后计算 `encrypted_sha256`。
3. 分片上传，每片带 `sha256`。
4. Hub 校验每片 hash。
5. Hub 合并后校验 `encrypted_sha256`。
6. 校验通过后迁移包进入 `ready`。

迁入校验：

1. 分片下载，每片校验 `sha256`。
2. 本地合并后校验 `encrypted_sha256`。
3. 使用用户口令解密。
4. 校验 `plain_sha256`。
5. 校验 manifest 中各数据文件 hash。
6. 校验通过后导入当前机器本地 maclaw 数据目录。

## 迁出流程

旧机器执行：

1. 用户打开 GUI 设置中的 `迁出与迁入`。
2. GUI 显示当前 Hub、租户、用户、当前机器名。
3. GUI 显示覆盖提醒。
4. 用户输入迁移加密口令并二次确认；两次口令必须一致。
5. MaClawSrv 创建迁移 job。
6. 读取当前用户记忆与当前用户本地知识库。
7. 构建 manifest 与迁移目录。
8. 压缩、使用本次输入的口令加密、计算 hash。
9. 向 Hub 创建迁出记录。
10. 分片上传，支持失败重试和断点续传。
11. Hub 完成整体校验后返回 `ready`。
12. GUI 显示迁出完成。

迁出进度阶段：

```text
scanning -> packing -> compressing -> encrypting -> uploading -> verifying -> ready
```

## 迁入流程

新机器或目标机器执行：

1. maclaw 客户端注册或登录到同一个 Hub。
2. GUI 打开 `迁出与迁入`。
3. 拉取同 Hub、同租户、同用户下有迁移包的实例列表。
4. 实例列表显示机器名、更新时间、大小、状态。
5. 用户选择源机器实例。
6. 用户输入迁移加密口令。
7. 目标机器向 Hub claim 迁移包。
8. 分片下载，支持断点续传。
9. 本地校验加密包 hash。
10. 解密、解压、校验明文包 hash。
11. 将数据导入当前机器 maclaw 的本地配置、记忆库、知识库数据库与知识库文件目录。
12. 本地导入完全成功后，客户端调用 Hub `complete`。
13. Hub 标记迁入成功并删除服务器上的迁移数据。
14. Hub 删除完成后返回完成。
15. GUI 显示迁入完成。

迁入进度阶段：

```text
claiming -> downloading -> verifying -> decrypting -> unpacking -> importing -> completing -> cleaning -> done
```

## 一次性迁入与清理

迁移包只能成功迁入一次。

状态机：

```text
uploading -> ready -> importing -> imported -> deleting -> deleted
                    -> failed
                    -> expired
                    -> replaced
```

规则：

- `ready` 可以被目标实例 claim。
- claim 后进入 `importing`，并设置 lease。
- 目标实例崩溃或断网时，lease 过期后可回到 `ready`。
- 本地导入未完全成功时，不允许调用成功 complete。
- Hub 只有收到目标实例的 complete 后才进入 `imported`。
- 进入 `imported` 后不允许再次下载或再次 claim。
- Hub 清理完成后进入 `deleted`。
- 清理失败时保持 `deleting` 并后台重试，但不允许再次迁入。

## API 草案

以下 API 属于当前登录 Hub，不属于 hubcenter。

```text
GET  /api/v1/migration/instances
POST /api/v1/migration/exports
GET  /api/v1/migration/exports/current
PUT  /api/v1/migration/exports/{export_id}/chunks/{index}
GET  /api/v1/migration/exports/{export_id}/chunks/{index}/status
POST /api/v1/migration/exports/{export_id}/complete-upload

POST /api/v1/migration/imports/{export_id}/claim
GET  /api/v1/migration/imports/{export_id}/chunks/{index}
POST /api/v1/migration/imports/{export_id}/complete
POST /api/v1/migration/imports/{export_id}/abort
```

所有 API 都必须从 Hub 登录态或机器 token 解析：

- `tenant_id`
- `user_id`
- `instance_id`
- `machine_name`

客户端不能通过请求体伪造这些身份字段。

## GUI 设计

设置页新增 tab：

```text
迁出与迁入
```

迁出区：

- 当前 Hub。
- 当前租户。
- 当前用户。
- 当前机器名。
- 数据范围：记忆、本地知识库、知识库文件。
- 旧迁出覆盖提示。
- 加密口令。
- 确认口令。
- `开始迁出` 按钮。
- 进度条与阶段文案。

迁入区：

- 可迁入实例列表。
- 每个实例显示机器名、实例名、系统、最近在线时间、迁出时间、包大小。
- 源实例选择。
- 加密口令。
- `开始迁入` 按钮。
- 进度条与阶段文案。

迁入文案应强调：

```text
迁入会把所选机器的记忆与知识库恢复到当前机器 maclaw 的本地数据中。
```

## 本地导入策略

记忆导入：

- 写入当前机器当前用户的本地记忆库。
- 保留内容、分类、标签、时间、来源元数据。
- 将 owner 归属映射为当前 Hub 用户。
- ID 冲突时生成新 ID，并记录 `imported_from_id`。
- 受保护记忆遵循现有保护规则。

知识库导入：

- 写入当前机器当前用户的本地知识库数据库。
- 恢复 sources、nodes、cards、facts、labels、source links、import batches。
- 恢复图片与附件资产到当前机器知识库资产目录。
- 将 tenant/user 归属映射为当前登录 Hub 的 `tenant_id + user_id`。
- ID 冲突时生成新 ID，并维护 source/card/fact/asset 引用映射。
- 导入前创建本地安全备份，失败时回滚。

配置恢复：

- 只恢复迁移所需的本地数据路径和知识库相关本地状态。
- 不覆盖当前机器的 Hub 登录态、机器实例 ID、机器 token、LLM token、代理密码等敏感配置。
- 不覆盖当前机器特有路径，除非它是知识库资产恢复目标目录。

## 失败与断点续传

上传断点：

- 客户端可查询 chunk 状态。
- 已上传且 hash 匹配的 chunk 不重复上传。
- hash 不匹配的 chunk 重新上传。

下载断点：

- 客户端保留本地临时下载目录。
- 已下载且 hash 匹配的 chunk 不重复下载。
- hash 不匹配的 chunk 删除后重新下载。

失败恢复：

- 打包、加密失败只影响本地 job。
- 上传失败可重试。
- Hub 合并校验失败进入 `failed`，需重新迁出。
- 迁入导入失败不清理 Hub 数据。
- complete 请求失败时客户端可重试；服务端 complete 必须幂等。

## 测试要点

- GUI 设置页出现 `迁出与迁入` tab。
- 迁出前显示“新迁出会覆盖旧迁出”的提醒。
- 迁出时必须输入两次口令，且两次口令不一致时不能开始迁出。
- 新迁出覆盖旧迁出时，口令可以与旧迁出不同，新迁移包必须使用本次输入口令加密。
- 实例列表显示机器名。
- 只能列出同 Hub、同租户、同用户的实例。
- 同用户只能有一份 `ready` 迁移包。
- 新迁出覆盖旧迁出。
- 上传分片 hash 错误会被拒绝。
- 合并后整体 hash 错误会被拒绝。
- 下载后 encrypted hash 与 plain hash 都必须校验。
- 口令错误不能导入。
- 本地导入失败不会删除 Hub 数据。
- 本地导入成功后才清理 Hub 数据。
- 迁移包成功迁入一次后不能再次迁入。
- 迁入落点是当前机器 maclaw 的本地配置、记忆库、知识库文件。
