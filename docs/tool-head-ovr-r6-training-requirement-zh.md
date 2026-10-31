# 工具分类头 r6 训练需求

运行时只认工具分类头里的类名和概率。`im_message`、`send_to_im`、`send_file` 不会被当成发信。

训练脚本是 `maclaw_reranker/scripts/qwen3_toolhead.py`。产品侧加载与注入在 `guiapp/app_local_intent.go`、`corelib/tool/toolhead.go`、`guiapp/semantic_tool_head.go`。

## 目标

重训第一方 one-vs-rest 工具头，让语义计划已经接好的工具各自有一个训练过的类。优先补齐四个邮箱工具。

现头是 `tool_head_ovr_qwen3go_r5.json`（`core49@81887a3273d7`，冻结于 2026-10-25）。49 个名字里 43 个训练过，没有下面四个类：

- `mailbox_send`
- `mailbox_query`
- `mailbox_delete`
- `mailbox_configure`

生产失败句是「将信息整理成pdf发珐znsoft@163.com」。现头分数是 `generate_pdf` 1.00、`im_message` 0.68、`send_file` 0.46、`send_to_im` 0.45。写门槛是 0.78，所以计划里只有 PDF。

「向znsoft@163.com发邮件，报告北京天气。」被打成 `im_message` 0.98、`send_to_im` 0.98、`send_file` 0.78。

在已冻结的 r5 上单加一行 `mailbox_send` 已经试过，分不开：正例最低约 0.24，负例最高约 0.43；平衡 logistic 的留出间隙约 −0.006。空权重会得到 NaN，比较时会假装通过。不要再交这种补丁行。按 r5 的配方整表重训：OvR logistic，C=10，balanced。

## 交付格式

沿用 `maclaw-tool-head-ovr-v1`。

| 字段 | 值 |
|---|---|
| 嵌入器 | 现网 `Qwen3-Embedding-0.6B` |
| `embed_role` | `classification` |
| 维度 | 1024 |
| 查询向量 | RoleClassification 的 pooled last token，再做 L2 归一化 |
| `embedder_model_id` | 必须等于现网 `embedding.ModelID()`，否则 `Bind` 拒绝加载 |
| `theta` | 0.30 |
| `top_k` | 5 |
| 新类 `trained` | `true`。未训练行的概率恒为 0，不会进入 top-k |

新文件名用 `tool_head_ovr_qwen3go_r6.json`。不要覆盖 r5。产品现在只自动加载 `tool_head_ovr_qwen3go_r5.json`，`release_assets.json` 还钉着 r5 的 sha256 `7fe77c0fdccdb1f9b9a0e91cda1210e2cef00bca350348748501bdbccde2f059`。文件名和校验和都要产品侧改过才会生效。请同时给出文件大小和 sha256。

类名字符串必须和本文完全一致。语义注入用 `semanticPetitionableCapabilities` 做精确查找，对不上的名字只记 `unmapped`，不会变成能力。

保留现有 49 个名字。不要把 `im_message` 改成 `mailbox_send`，也不要合并 `send_to_im` 或 `send_file`。

## 运行时怎么用这个分数

语义计划把 top-k 并进能力需求，规则如下。训练门槛按这张表，而不是只看 argmax。

- 生效工具和特权读要概率至少 0.78（`ReviewedIntentMinimumConfidence`）才加入计划。四个邮箱类都走这道门槛。`mailbox_query` 虽然是读，仍然要 0.78。
- 普通只读过 `theta` 0.30 就会进入计划。`current_datetime` 属于这一档。
- `send_file`、`send_to_im`、`asr` 即使分数高也会被按「缺现成输入」拦住，头不能凭空造出投递或转写。
- 加进去的需求是可选的。头的分数不会改写意图 `Primary`。
- top-k 是 5。PDF 加发信这种句子里，`generate_pdf` 和 `mailbox_send` 必须同时落在这 5 个里面，并且都达到 0.78。

## 必须新增的类

留出集不能和训练集重合。四个邮箱类的留出正例都要达到 0.78。

### mailbox_send

正例：

- 给对方发一封邮件
- 回复这封邮件
- 把这封邮件转发出去
- 用工作邮箱发这封信
- send an email to them
- reply to this email
- forward this message by email
- send it from the default mailbox
- 向 znsoft@163.com 发邮件，报告北京天气
- 把北京天气整理成 PDF 发到 znsoft@163.com
- 将信息整理成pdf发珐znsoft@163.com

必须压低的句子：发给我；把这个文件发送给我；生成一份 PDF 文档并发给我；北京天气，生成 pdf；看看我的邮件；删除这封邮件；添加一个邮箱账号；发到微信。

### mailbox_query

正例：看看我的邮件；打开收件箱看看未读；搜一下这周的邮件；读一下这封邮件；search my inbox；show unread email。

必须压低：发邮件、删除邮件、配置邮箱、生成 PDF。

### mailbox_delete

正例：删除这封邮件；清空收件箱；把邮箱里的这封删掉。

必须压低：发邮件、读邮件、配置邮箱。

### mailbox_configure

正例：添加一个邮箱账号；配置 SMTP；修改邮箱服务器和端口。

必须压低：发邮件、读邮件、删除邮件。

### 同一张表里一并加上

这些名字今天也不在已训练类里。同一轮重训加上，避免下一轮再缺：

- `delete_file`
- `git_status`
- `git_commit`
- `current_datetime`
- `session_search`
- `knowledge_maintain`
- `manage_template`
- `list_sessions`
- `manage_config`

`current_datetime` 是普通只读，过 0.30 就会进入计划。它的留出负例要低于 0.30，避免占满 top-5。

`browser` 和 `computer_use` 不要加。这两类只跟意图标签走。

已有的 6 个未训练名字保持 `trained=false`：`ask_user`、`import_mcp_servers`、`list_mcp_tools`、`manage_skill`、`read_tool_result`、`write_excel`。没有正例就不要把它们标成训练过。

## 验收

用生产同一套嵌入方式，对留出句输出完整概率，不要只报 argmax。权重里出现 NaN 或 Inf 直接失败。

`mailbox_send` 的留出正例最低概率 ≥ 0.78，留出硬负例最高概率 ≤ 0.45。正例最低减去负例最高的间隙要大于 0。

| 句子 | 要求 |
|---|---|
| 将信息整理成pdf发珐znsoft@163.com | `generate_pdf` ≥ 0.78，`mailbox_send` ≥ 0.78，两者都在 top-5。`mailbox_send` 高于 `im_message`、`send_to_im`、`send_file` |
| 把北京天气整理成 PDF 发到 znsoft@163.com | 同上 |
| 向znsoft@163.com发邮件，报告北京天气。 | `mailbox_send` ≥ 0.78，并且是 `mailbox_send`、`im_message`、`send_to_im`、`send_file` 里最高的 |
| 北京天气，生成pdf | `mailbox_send` ≤ 0.45。`web_search` 和 `generate_pdf` 仍 ≥ 0.78 |
| 生成一份PDF文档并发给我 | `generate_pdf` ≥ 0.78，`mailbox_send` ≤ 0.45 |
| 看看我的邮件 | `mailbox_query` ≥ 0.78，`mailbox_send` ≤ 0.45 |
| 删除这封邮件 | `mailbox_delete` ≥ 0.78，另外三个邮箱类 ≤ 0.45 |
| 添加一个邮箱账号 | `mailbox_configure` ≥ 0.78，另外三个邮箱类 ≤ 0.45 |

现有类不能退步：天气查询仍是 `web_search` 领先，纯 PDF 仍是 `generate_pdf` 领先，即时通讯仍是 `im_message` 或 `send_to_im` 领先，而不是 `mailbox_send`。
