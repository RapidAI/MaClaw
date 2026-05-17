# MaClaw Hub 企业管理设计草案

## 1. 目标

企业管理页应从“配置项集合”转为“以组织架构为中心的策略解释器”。管理员点击全局、部门、组或用户时，系统需要回答三个问题：

- 这个对象是谁，处在组织中的哪里。
- 它最终生效的策略、权限、模型额度和能力包是什么。
- 这些配置来自全局、部门继承，还是对象自身覆盖。

## 2. 信息架构

### 2.1 全局

全局对象代表企业默认配置，适合承载：

- 集中安全策略开关。
- 组织架构开关。
- 默认部门。
- 全局数据外发、网络、沙箱、YOLO、智能路由策略。
- 全局必装 Skill/MCP 能力包。
- 新用户准入和默认策略。

### 2.2 部门/组

部门或安全组是企业管理的主要操作对象。点击部门后展示：

- 基本信息：名称、路径、成员数、子部门数、组 ID。
- 生效策略：继承自全局或上级部门的策略，以及本部门覆盖项。
- 能力包：全局必装 + 部门必装 + 部门禁止 + 版本约束。
- 成员摘要：人数、分页入口、搜索入口、批量移动入口。
- 最近变更：策略、成员、能力包相关管理操作。

成员列表不直接平铺在主页面，必须放在弹窗或抽屉中，避免组织页被大量用户记录撑爆。

### 2.3 用户

点击用户后展示该用户最终落地状态：

- 用户身份：邮箱、状态、SN、所属部门路径。
- 生效策略：全局 + 部门 + 用户覆盖后的最终结果。
- 下发 Skill/MCP 明细：应装、已装、缺失、版本不符、禁止但存在。
- 设备与会话：设备数、在线状态、最近活跃。
- 审计和风险：外发拦截、安装失败、策略变更。

## 3. 策略模型

策略采用三层继承：

1. 全局默认策略。
2. 部门/组覆盖策略。
3. 用户例外策略。

每个策略项在 UI 中都必须显示来源：

- 继承自全局。
- 继承自某部门。
- 本对象覆盖。

当前已支持的策略项包括：

- 文件外发。
- 图片外发。
- Gossip。
- YOLO 模式。
- 智能路由。
- 护栏模式。
- 沙箱模式。
- 网络级别。
- Skill 来源控制。

后续可扩展：

- 模型白名单。
- Token 额度。
- 并发限制。
- 远程控制权限。
- 项目/知识库访问范围。

## 4. 模型服务组策略

模型服务组也应纳入企业策略，并支持全局、部门/组、用户三层指定：

- 全局模型服务组：作为企业级默认模型服务能力。
- 部门/组模型服务组：覆盖或补充全局配置，适合研发、法务、运营等不同部门。
- 用户模型服务组：少量个人例外，优先级最高。
- 未指定时：回退到“模型服务设置”中的新用户默认模型服务组。

右侧对象详情应展示模型服务组的生效来源：

- 用户直接绑定。
- 当前部门/组绑定。
- 上级部门/全局继承。
- 新用户默认模型服务组回退。

用户明细还应展示该模型服务组实际授权出的模型路由、默认模型、额度/有效期和未激活原因。

## 5. Skill/MCP 能力包策略

能力包策略应独立成组，但和组织策略一起下发。建议支持：

- 必须安装。
- 推荐安装。
- 禁止安装。
- 固定版本。
- 自动升级。
- 需管理员审批。

能力包对象字段建议：

- id
- type: skill 或 mcp
- name
- source: skillhub、clawhub、github、internal
- required_version
- install_policy: required、recommended、blocked
- scope: global、group、user

用户明细中需要展示合规状态：

- 已合规。
- 缺失。
- 版本过旧。
- 被禁止。
- 安装失败。

## 6. 页面布局

V1 页面布局：

- 顶部：低密度状态指标。
- 左侧：组织树。
- 右侧：对象详情。
- 弹窗：部门成员列表、成员移动、默认部门选择。

右侧对象详情分区：

- 概览。
- 生效策略。
- Skill/MCP 能力包。
- 成员/设备。
- 审计。

V1 先完成概览、生效策略、能力包占位、成员弹窗。审计和设备详情进入 V2。

## 7. V1 实施范围

- 将安全管理页标题和语义调整为“企业组织与策略”。
- 以组织树为主入口。
- 点击部门/组后，右侧展示对象详情、生效策略和能力包明细区。
- 点击部门/用户后，右侧展示当前生效的模型服务组；若未绑定，则明确显示回退到新用户默认模型服务组。
- 部门成员不再平铺，改为弹窗列表。
- 成员弹窗支持搜索、分页、移除、点击查看用户生效策略。
- 点击用户后，右侧展示该用户的最终生效策略和能力包状态占位。

## 8. 后续阶段

- V2：能力包策略后端模型、安装状态上报、合规检测。
- V3：SSO/OIDC/SCIM、企业通讯录同步。
- V4：审计报表、成本预算、部门级额度和审批流。

## 9. 当前已落地补充

本轮实现已超过最初 V1 占位范围，当前企业管理页应按以下能力验收：

- Tab 名称调整为“企业管理”。
- 主视图以组织树为中心，右侧按全局、部门、用户展示对象详情。
- 部门成员列表移入弹窗，支持搜索、分页、CSV/JSON 导出和点击查看用户明细。
- 全局、部门、用户均可查看生效安全策略、策略来源、模型服务组来源和 Skill/MCP 能力包明细。
- 模型服务组继承优先级为用户绑定、最近部门绑定、全局绑定、新用户默认模型服务组。
- 模型服务组选择器提供清空覆盖操作，便于部门或用户快速回到继承链路。
- 用户详情展示模型路由、默认模型、额度/有效期、设备与会话摘要。
- 能力包区支持必装、推荐、禁止策略，以及合规状态筛选和导出；状态筛选覆盖风险汇总和额外安装，便于单独复核未被策略覆盖的客户端能力包。
- V2 合规结果除 JSON 外新增 CSV 导出，字段包含导出时间、快照 checksum、筛选条件、汇总计数、托管/额外安装行、策略来源、期望版本、已装版本、安装状态和最后上报时间，便于审计人员直接进入表格或 SIEM 复核。
- V2 合规 CSV 导出补充 snapshot_id，并在导出成功提示里展示该 ID，便于审计沟通时把浏览器下载文件、JSON 快照和外部工单串联起来。
- V2 合规 JSON/CSV 导出会同步写入 snapshot_summary、quality_score、warn_count、error_count、warning_severity_counts 和 checksum 上下文；页面合规面板也直接展示质量分与 error/warn 分级，并沿用同一个 summary_scope/full_total 口径；后端会在启用筛选时返回 filtered_summary；未筛选时保持全量口径，避免全量快照被误标为 filtered。筛选后的可见结果、质量分、CSV 行内汇总计数、JSON snapshot_summary、summary_scope、filtered_total/full_total 和导出快照保持一致，便于审计评分和分级复核。
- 合规质量分会把额外安装纳入风险分母；当托管策略全部合规但存在未被策略覆盖的客户端能力包时，quality 仍为 partial，quality_score 也会下降，避免出现 partial 但 100/100 的误导性快照。
- 当管理员只关闭“显示额外安装”但不选择状态筛选时，后端同样返回 filtered_summary，用 filtered_summary.total 保持托管策略行总数，用 filtered_summary.unmanaged_installed=0 明确表示额外安装已被排除。
- 合规接口会把未知 status 筛选值规范化为全量视图，避免拼写错误让审计导出变成空结果。
- 合规接口只有在 include_unmanaged 明确为 false、0、no 或 off 时才排除额外安装；其它未知值按全量视图处理，避免拼写错误导致额外安装被意外隐藏。
- 前端会将合规状态筛选和 stale_after_hours 过期阈值规范化后再请求和导出，确保页面、后端计算和审计文件使用同一个整数小时口径。
- 前端下发策略展示、合规 fallback 计算、保存请求和导出也会统一 trim 并规范化 policy，避免历史异常值或带空白的旧值在浏览器侧被误判为推荐策略。
- 合规面板的筛选摘要会同时展示托管策略行已显示/总数与额外安装已显示/总数，并使用中英文文案，避免只看托管行数量时误判额外安装是否已被纳入当前视图。
- 能力包下发策略在创建、读取和生效策略计算时都会规范化为 required/recommended/blocked；历史异常值会按 required 处理，避免旧数据影响合规口径。
- 能力包选择时会自动带入市场当前版本，管理员仍可手动改为固定版本或留空使用 auto。
- 能力包下发区会显示当前下发范围，并在创建策略时附带部门名称和组织路径上下文。
- 最近变更页支持按操作、日期、关键词过滤，并支持 JSON/CSV 导出。
- 对象总快照导出包含概览、策略、模型服务、能力包、成员和审计上下文。
- 对象审计区展示快照范围覆盖状态，导出前可看到概览、策略、模型服务、能力包、成员和审计上下文是否已加载。
- 对象总快照会额外保存当前对象在已加载审计结果中的匹配记录数和匹配记录列表。
- 审计范围只有在最近变更加载完成后才标记为已包含，快照中记录 loaded_at 和 loaded_count。
- 快照范围会同时写入 included、missing 和 not_applicable 分区，导出文件可以直接判断缺少哪些上下文。
- 对象总快照包含 snapshot_summary，汇总策略项、模型服务组、能力包、成员、子部门和审计命中数量。
- 快照范围和 snapshot_summary 同时包含 complete、completeness_ratio、included_count 和 applicable_count，用于快速判断快照完整度。
- 快照范围和 snapshot_summary 同时包含 quality，按 complete、partial、incomplete 标记快照质量。
- 快照质量还包含 quality_score 和 warning_severity_counts，用于自动审计评分和告警分级。
- 对象总快照导出前会执行质量预检；当快照不是 complete 时，确认框会提示缺失分区、质量分和 warning/info 摘要，避免管理员误导出半成品快照。
- 导出成功后会在对象审计区保留最近一次快照导出的 snapshot_id、类型、时间和 checksum，并提供复制 ID/校验值按钮；登记簿每条记录也可复制 ID 和 checksum，方便审计留痕和跨系统沟通。
- 浏览器会维护最近 20 条快照导出登记簿，并可导出 snapshot_export_registry JSON，用于把对象快照、策略快照、能力包快照和审计快照串成一次管理员操作留痕。
- 快照导出登记簿会持久化到浏览器本地存储，刷新页面后仍可追溯，并支持管理员主动清空本地登记簿。
- 登记簿页面会展示本地保留的全部最近 20 条记录，避免页面只显示前几条而导出包含更多记录导致审计复核口径不一致。
- 清空登记簿时会同时重置本地搜索和风险筛选状态，避免下一次导出记录被旧条件隐藏。
- 快照导出登记簿每条记录会保留对象、组织路径、quality_score、summary_scope、filtered_total/full_total 和 warning_severity_counts，登记簿列表中可直接扫描低质量、有告警或来自筛选视图的导出记录。
- 快照导出登记簿支持按全部、低质量/告警、error、warn、筛选导出过滤，导出登记簿 JSON/CSV 时会保留 registry_filter，便于按风险等级或导出口径复核。
- 登记簿支持“仅看低质量/告警”筛选，并可按最新、低质量优先、告警数优先、类型排序；导出登记簿时会保留 registry_filter、registry_sort、registry_total_count 和 registry_count，便于单独复核有风险的快照导出。
- 登记簿同时支持按 snapshot_id、对象、组织路径、checksum 和风险关键词搜索，导出登记簿时会保留 registry_query，方便将一次专项复核的搜索条件一起留档。
- 登记簿列表和导出 JSON 会计算 registry_summary 和 registry_total_summary，包含总数、风险数、平均质量分、类型分布、导出口径分布和告警分级汇总；列表顶部会直接展示 scope 与 error/warn/info 分级汇总，并支持按导出口径排序，便于管理员快速判断导出健康度。
- 登记簿除 JSON 外还支持导出 CSV，字段覆盖 registry_exported_at、registry_checksum、registry_checksum_algorithm、registry_filter、registry_query、registry_sort、registry_total_count、registry_count、registry_issue_count、registry_avg_quality_score、registry_scope_counts、registry_filtered_count、registry_all_scope_count、registry_warn_count、registry_info_count、registry_error_count、registry_rank、snapshot_id、对象、组织路径、质量分、summary_scope、filtered_total/full_total、告警分级和 checksum，并带 UTF-8 BOM、批次 checksum 和 checksum 算法，方便进入表格或 SIEM 流程复核并还原筛选排序上下文。
- 登记簿从本地存储恢复时会规范化数值和告警分级计数，CSV 公式防护会覆盖前导空白的 =/+/-/@ 字段。
- 登记簿质量分会被限制在 0-100，告警计数会被限制为非负值，避免异常本地数据影响健康度汇总。
- 登记簿导出会在当前搜索/筛选无匹配时提示而不生成空文件，CSV 字段会避免被表格软件误解为公式。
- 登记簿自身的 JSON/CSV 导出不会反写到登记簿，避免因复核导出操作产生递归噪声。
- 对象总快照包含 snapshot_warnings、snapshot_warning_details 和 warning_count，用于提示缺失分区、未加载审计或当前对象没有审计命中，并给出 severity、section 和说明。

## 10. 快照与审计导出契约

企业管理相关 JSON 导出统一带有以下元数据，便于审计留档和后续导入校验：

- snapshot_schema_version: 快照结构版本。
- snapshot_type: 快照类型，例如 enterprise_object、security_policy、model_service、capability_packages、capability_compliance、audit_logs。
- snapshot_id: 由类型、对象和导出时间生成的稳定标识。
- exported_from: 固定为 maclaw_hub_enterprise_management。
- exported_by: 当前管理员用户名和邮箱快照。
- exported_at: ISO 时间。
- snapshot_checksum_algorithm: 当前为 fnv1a32-stable-json。
- snapshot_checksum: 基于稳定 JSON 字段排序计算的轻量校验值。
- 导出成功提示会显示 snapshot_id，便于管理员把浏览器下载文件和审计沟通记录对应起来。
- snapshot_context: 记录导出时的语言、页面路由、时区偏移、企业管理子页、当前选中对象、部门路径和审计加载时间，便于跨地区审计排查。

对象总快照还会在顶层记录 object_group_path 和 object_group_path_ids，便于单独查看导出文件时还原用户或部门所在组织路径。

对象总快照额外包含 snapshot_sections，用于声明本次导出是否包含概览、策略、模型服务、能力包、成员和审计上下文。后续如果接入服务端归档或签名，可直接沿用该契约补充服务端签名字段。
