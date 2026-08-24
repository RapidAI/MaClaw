# 个人经验沉淀为组织数字资产

| 字段 | 值 |
| --- | --- |
| 文档标题 | 个人经验沉淀为组织数字资产 |
| 日期 | 2026-08-20 |
| 状态 | v1.1 Implemented；**v1.2 设计修订（本文）**：补易用性、安全、功能缺口，待按节落地 |
| 范围 | 个人 `knowledge.db` 业务来源、个人 `coding_knowledge.db` 编码经验 → Hub 投稿 → 管理员**审阅后**审批 → 现有数字资产库单向同步 |
| 关联文档 | `docs/hub-tenant-digital-assets-design-zh.md`、`docs/knowledge-export-share-and-hub-import-design-zh.md` |

---

## Overview

企业数字资产 v1 能把制度/手册变成租户真源，但个人经验进入组织必须绕「Knowledge Share → 管理员粘贴链接导入」。v1.1 把投稿做成数字资产的一等公民：

- 用户在 MacLaw GUI 选择个人业务知识或已确认编码经验，提交到指定企业库；
- 租户管理员审批后写入 Digital Asset Library，再走原有 Hub→客户端单向同步；
- **投稿不等于入库**。客户端不能写回企业真源。

v1.1 已能走通「提交 / 撤回 / 批准 / 拒绝」，但审批几乎是盲批、投稿可灌包、企业侧把「管理员点批准」写成编码经验 `verified`，且同步标签带投稿人邮箱。v1.2 把这条链路收成：**人看得懂、管理员看得完、灌不爆、秘密不进真源**。

本期范围：业务知识库 + 编码/技术经验。记忆、技能、会话摘要、工作流、运行 traces 仍不在范围内。

---

## 原则

1. 组织真源仍只由 Hub 数字资产写。Viewer 只能投稿暂存包，不能改 `content_rev`。
2. 复用 `maclaw.knowledge.package` 与现有 `ImportPackageSources` / changelog / sync，不新开同步协议。
3. 业务与技术同一产品，用 `library_kind` 分库；不在同一库混装。
4. 不替代管理员从 Knowledge Share 导入的路径。
5. 企业库内容禁止反向发布为 Knowledge Share。
6. **批准是治理动作，不是技术鉴定**。管理员批准只表示「允许进入本库」；编码经验不得因此直接标 `verified`。
7. **默认证伪最小化**：同步到同事机器的内容不含投稿人邮箱、本地绝对路径、会话稿、企业回声。
8. **404-on-deny**：库不可见、不接受投稿、非本人投稿，对外一律当不存在。

---

## 角色与入口

| 角色 | 可做 | 不可做 |
| --- | --- | --- |
| 人类 Viewer（GUI，已登录 Hub） | 看可投稿库、提交、看自己的投稿、撤回待审、按拒绝意见修订再投 | 写企业真源、看他人投稿包、改库 ACL |
| 租户管理员 | 全局待审箱 + 每库队列、打开审阅预览后再批准/拒绝、改 `library_kind` / `accepts_submissions` | 用 machine token 操作投稿 API |
| VE / headless / machine token | 只读同步与召回（现有协议） | **不得**调用投稿/撤回；GUI 是唯一投稿客户端 |
| 全局 admin | 默认不审本租户投稿（与数字资产正文一致） | 静默跨租户批准 |

产品句：用户把个人经验交给组织；管理员决定组织是否收下；收下之后按该库 ACL 单向同步。

---

## 概念

| 名称 | 含义 |
| --- | --- |
| Contribute | GUI 把个人经验打成投稿包并提交到指定企业库 |
| Submission | Hub 暂存记录：元数据 + 目的库 + 包文件 + 治理说明 |
| library_kind | `business` 或 `technical`；缺省 `business` |
| submission.kind | `business_knowledge` 或 `coding_experience`；必须与目标库 kind 对齐 |
| experience_class | SOP / 教训事实 / 惯例 / `pattern` / `decision` / `pitfall` / `convention` |
| Review preview | 管理员批准前必看的条目列表 + 截断正文，不是盲批按钮 |

界面文案只用「业务 / 技术」。API 保留上面的稳定枚举，GUI 负责翻译。

---

## 流程

```text
GUI 勾选个人条目 + 必填「对组织的价值」
  -> 本地过滤（session / local_only / 企业回声 / 未确认编码经验）
  -> 默认真敏脱敏后打成 maclaw.knowledge.package
  -> POST /api/digital-assets/submissions
  -> Hub 配额/限流/去重/校验后写入 submissions 表 + 租户目录下的包文件
  -> 管理员在「待审箱」打开审阅预览
  -> 批准：校验包 SHA-256 → ImportPackageSources → content_rev++
     或拒绝：必填原因，投稿人可见
  -> 现有单向 sync → 同事 enterprise_knowledge.db
```

| 动作 | 说明 |
| --- | --- |
| Contribute | 必须显式勾选条目，或显式勾选「投稿全部符合条件的个人条目」。禁止静默整库提交。 |
| Submission | Hub 侧记录 + 目标库 + 包 + 说明；包路径由服务器生成，客户端不可指定 |
| 审阅 | 未打开预览不得启用「批准」 |
| library_kind | 业务库只收业务包，技术库只收编码经验 |

---

## 状态机

```text
submitted ──批准且导入成功──► approved
submitted ──拒绝──► rejected
submitted ──本人撤回──► withdrawn
submitted ──批准但导入失败──► import_failed
import_failed ──再次批准──► approved
import_failed ──拒绝──► rejected
rejected / withdrawn ──修订后再投──► submitted（新 ID，parent_id 指向旧记录）
```

不存在单独的 `imported` 状态：批准与入库是同一治理事务。导入失败必须可重试，不能卡死。

权限：

- 投稿：本租户人类 Viewer；目标库 `status=active`、`accepts_submissions=true`、kind 匹配、ACL 可见。
- 审批：`tenant_owner` / `tenant_admin`。
- 拒绝必须写原因（建议 10–2000 字，与能力市场拒稿一致）。
- 批准成功后走 `advanceContentAfterImportLocked`（`content_rev++`）。
- 本人只能看见/撤回自己的 `submitted`；他人 ID 一律 404。

---

## 分库与召回

| Kind | 接受投稿 | 客户端召回 |
| --- | --- | --- |
| business | `business_knowledge`（制度/文档/可检索业务经验） | 企业知识库 Tab / auto-recall |
| technical | `coding_experience` | **只读**合并进 coding recall，**不写**本地 `coding_knowledge.db` |

不做 mixed 库。需要两类经验时建两个库。

技术库召回规则：

- 只合并 `active` 或库内明确标记可用的编码经验；
- 去重键：`title + trigger_condition`（大小写不敏感）；本地经验优先；
- 企业条目标 `enterprise coding experience`，仅作只读提示；
- **批准后的编码经验 status = `active`，confidence 用初始值，recall/success/failure 归零**。`verified` 仍留给组织内二次确认或后续治理动作，避免「点一下批准 = 全员高信任召回」。

---

## 投稿包

- 格式：`maclaw.knowledge.package`。
- 业务排除：`session` / `local_only`、企业回声（`dal_` / `sub_` 前缀、`enterprise://` / `submission://`、`enterprise_import_kind=*`）。
- 编码仅 `active` / `verified`；`SanitizeExperienceForExport`；`ProjectPath` 只保留末级目录名；若变空则 scope 降为 `universal`；组织侧统计从 0 计。
- 条目上限 200，包上限 20MB（租户可下调，不可上调超过硬顶）。
- 业务投稿默认 `redact_sensitive=true`；用户关闭时 GUI 二次确认。
- 说明（summary）必填，最少 10 个可见字符。

入库后 **允许** 的治理标签（服务器写入，覆盖客户端同名标签）：

- `enterprise_import_kind=experience_submission`
- `submission_id=...`
- `submitted_by_user=<user_id>`（**不要**把邮箱写入会同步的 labels）
- `experience_domain=business|technical`
- `experience_class=...`

投稿人邮箱、Hub token、本机绝对路径只留在 Hub 的 `digital_asset_submissions` 行，不同步到客户端。

---

## API

用户侧（Viewer Token，且调用方为 GUI）：

- `GET /api/digital-assets/libraries/contributable?kind=`
- `POST /api/digital-assets/submissions`
- `GET /api/digital-assets/submissions`（我的投稿）
- `GET /api/digital-assets/submissions/{id}`（仅本人；含拒绝原因，不含他人包）
- `POST /api/digital-assets/submissions/{id}/withdraw`
- `POST /api/digital-assets/submissions/{id}/resubmit`（v1.2：仅 `rejected` / `withdrawn` 本人）

管理员：

- `GET /api/admin/digital-assets/submissions`（默认 `status=submitted,import_failed`；可按库过滤）
- `GET /api/admin/digital-assets/submissions/{id}`（元数据 + **审阅预览**：每条 title / kind / 截断正文 / 标签；默认不回完整包）
- `GET /api/admin/digital-assets/submissions/{id}/package`（显式下载原文，写审计）
- `POST /api/admin/digital-assets/submissions/{id}/approve`
- `POST /api/admin/digital-assets/submissions/{id}/reject`

创建/更新库继续带 `library_kind`、`accepts_submissions`。同步协议 `/api/digital-assets/sync/*` 不变。

错误：ACL / 非本人 / 库不接受投稿 → **404** `NOT_FOUND`。校验失败 → 400。状态不允许 → 409。超配额 / 超频 → 429。

---

## 易用性（v1.2）

### 投稿人（GUI）

1. **先选后投**。知识库与编码经验都要勾选；「投稿全部符合条件项」必须是单独复选框，默认关。
2. **目标库用下拉，不用手填 ID**。无库时说明「管理员尚未开放可投稿的业务/技术库」，并链到 Hub 登录配置。
3. **说明框解释写什么**：例如「同事在什么情况下会用到、不要写客户真名」。
4. **我的投稿常驻**：状态（待审 / 已通过 / 已拒绝 / 已撤回 / 导入失败）、拒绝原因、撤回、按意见再投。打开导出 Tab / 编码经验页即加载，不必先点投稿。
5. **Hub 未配置**时禁用投稿按钮，文案指向「设置 → 远程 Hub」，不要只抛 `hub_url is required`。
6. 业务与编码两处文案对齐，避免一处「可整库提交」、一处「必须勾选」。

### 管理员（Hub Admin）

1. **全局待审箱**（数字资产 Tab 顶栏）：跨库列出 `submitted` + `import_failed`，按时间倒序。每库详情里仍保留本库队列。
2. **批准前必须打开审阅预览**：标题、说明、投稿人、条目数、包大小、每条截断正文。预览未开时批准按钮禁用。
3. **创建库**用业务/技术单选，不要让管理员手打 `business` / `technical`。
4. 导入槽位被其它 job 占用时，提示「当前租户正在导入，请稍后重试」，不要把失败写成「投稿无效」。
5. 拒绝原因回写给投稿人；批准可写内部备注（默认同步不可见）。

---

## 安全性（v1.2）

| 项 | 锁定 |
| --- | --- |
| 鉴权 | 投稿/撤回/我的列表：**人类 Viewer Token only**。machine token、纯 VE 调用拒绝。批准/拒绝：tenant admin。 |
| 租户 | 一律 `viewer.TenantID` / Admin 当前租户；禁止客户端传 tenant。 |
| 404-on-deny | 保持；含「库不接受投稿」。 |
| 包落盘 | 路径固定 `{host.Root()}/{tenant}/submissions/{id}.json`；文件模式 `0600`；`PackageRef` 不得接受客户端输入。批准前校验文件在该目录内且 SHA-256 与记录一致。 |
| 配额 | 每用户同时 `submitted` ≤ 20；每用户每天新投稿 ≤ 30；每租户投稿目录体积纳入库配额。超限 429。 |
| 限流 | 每用户投稿 10 次 / 10 分钟（与同步 `PerUserPullRPM` 分开计数）。 |
| 去重 | 同一库已有相同 `package_sha256` 且状态为 `submitted` / `approved` → 409，提示「相同内容已在审或已入库」。 |
| 真敏 | 业务默认脱敏；编码走 `SanitizeExperienceForExport`。服务端再拒 session / local_only / 企业回声。 |
| PII | 同步标签只用 `submitted_by_user`。邮箱仅 Hub 管理端可见。 |
| 编码信任 | 批准 → `active`，不是 `verified`。 |
| 包生命周期 | `withdrawn` / `rejected` 包保留 30 天后删除文件，行留审计字段；`approved` 包可按租户备份策略另存，不长期放世界可读目录。 |
| 审计 | submit / withdraw / resubmit / approve / reject / package-download 写入 Admin audit（谁、哪库、submission_id、字节数）。 |
| XSS | Admin 预览继续 `escapeHtml`；标题与说明不当 HTML。 |
| VE | 不实现投稿 UI；即使持有绑定用户 Viewer Token，产品约定不代投。后续若要防伪，给 Viewer Token 加 `client_kind`。 |

v1.1 实现缺口（对照代码，落地 v1.2 时优先修）：

- 包文件现为 `0644`，应改为 `0600`。
- 批准时未复核 SHA-256。
- 同步标签写入了 `submitted_by=<email>`。
- 编码经验批准后被标成 `verified`。
- 无投稿配额、限流、同内容去重、审计、管理员正文预览、本人 GET-by-id、修订再投。
- 管理员创建库 kind 仍是自由文本 prompt。

---

## 功能性（v1.2）

1. **审阅预览 API**：管理员能看到每条 title、kind、截断正文（建议 2KB/条）、标签；完整包需显式下载。
2. **全局待审列表**：不点进每个库也能清队列。
3. **修订再投**：拒绝/撤回后带 `parent_id`，管理员能对照上一版拒绝原因。
4. **批准可改投目标库**（API 已有 `library_id`）：仅允许同 kind、同租户、仍接受投稿的库；UI 默认保持原目标，改库需确认。
5. **导入失败可重试**：`import_failed` 保留原包，再次批准不新建投稿。
6. **批准结果回写** `import_job_id` + 导入 source id 前缀 `sub_{submission_id}_`，便于以后下架该次投稿。
7. **租户设置**（可并入现有 `digital_assets` settings，不必新表）：`max_pending_submissions_per_user`、`max_submissions_per_user_per_day`、`submission_package_ttl_days`、`require_submission_preview`（默认 true）。
8. **不做**：记忆/技能/工作流投稿；条目级 ACL；通过后与个人库 / Share 双向联动；客户端或 VE 直接写企业库；把编码经验压成大模型散句。

---

## 非目标（保持）

- 记忆 / 技能 / 工作流 / 会话自动投稿
- 单条经验独立于库的 ACL
- 投稿通过后与个人库 / Share 双向联动
- 客户端或 VE 直接写企业库
- 把编码经验压成大模型散句

---

## GUI 要点

业务（知识库导出 Tab）：

- 「投稿到组织」打开对话框：目标业务库、标题可选、价值说明必填、已选条数或「全部符合条件项」复选框。
- 「我的投稿」展示状态与拒绝原因；`submitted` 可撤回。

编码经验：

- 仅 `active` / `verified` 可勾选；目标技术库 + 价值说明必填。
- 企业技术库只读合并进 recall，不写入 `coding_knowledge.db`。

两处失败都要变成可执行空态（未登录 Hub / 无库 / 未选条目），不要只弹底层错误串。

---

## Admin 要点

- 建库：名称 + **业务/技术单选** + 默认接受投稿。
- 库详情：kind、接受投稿开关（随 ACL 保存）、本库队列。
- Tab 级待审箱：跨库待审 + 导入失败。
- 审阅抽屉：预览 → 批准 / 拒绝。拒绝必填原因。

---

## 测试

- 业务包不能投技术库，反之亦然。
- session / `dal_` / 企业回声被拒。
- 编码缺 `trigger_condition` 被拒。
- 非本人撤回 404；非 `submitted` 撤回 409。
- 批准提升 `content_rev`，source id 带 `sub_` 前缀。
- 批准后编码 status=`active`，不同步投稿人邮箱。
- 相同 SHA-256 再投 409。
- 超 pending 配额 429。
- machine token 投稿 401。
- 管理员未取预览时，产品层不允许批准（UI）；API 仍可批准以便脚本，但审计必须留下「无预览下载」标记——**产品默认走 UI 预览**。
- 404-on-deny：无 ACL 的库不出现在 contributable，直接 POST 该库 ID 返回 404。

---

## 落地顺序

1. 安全修补（文件权限、SHA-256、标签去邮箱、编码 status、限流配额、去重）。
2. 管理员审阅预览 + 全局待审箱 + 创建库单选。
3. GUI 先选后投、Hub 空态、我的投稿常驻、拒绝后再投。
4. 包 TTL 清理与审计补齐。
