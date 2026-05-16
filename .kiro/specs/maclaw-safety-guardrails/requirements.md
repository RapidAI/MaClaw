# 需求文档

## 简介

增强 MaClaw 安全护栏系统，扩展现有 `corelib/security/` 包的防护范围，使其能够覆盖更多危险操作类型（如环境变量篡改、凭据泄露、容器逃逸等），并通过可配置的扩展机制允许用户和项目自定义护栏规则。现有系统已具备 Firewall、RiskAnalyzer、PolicyEngine、AuditLog、LLMReview 五大组件，本次增强聚焦于：扩大内置风险模式覆盖面、引入可配置的护栏扩展点、增强审计与告警能力。

## 术语表

- **Firewall（防火墙）**: 统一安全检查入口，整合 RiskAnalyzer、PolicyEngine、AuditLog 进行工具执行前的安全拦截
- **RiskAnalyzer（风险分析器）**: 基于正则模式的风险评估引擎，对工具调用进行风险等级判定
- **PolicyEngine（策略引擎）**: 基于优先级排序的策略规则评估器，决定 allow/deny/ask/audit 动作
- **AuditLog（审计日志）**: 按日期分割、支持大小轮转和 30 天保留的审计日志系统
- **LLMReview（LLM 审查）**: 利用大语言模型对高风险操作进行二次安全审查
- **RiskPattern（风险模式）**: 定义工具名匹配和参数匹配的正则规则，用于风险识别
- **PolicyRule（策略规则）**: 定义工具模式、参数模式、风险等级到策略动作的映射规则
- **GuardrailProfile（护栏配置档）**: 一组命名的护栏规则集合，可通过配置文件加载和切换
- **CallContext（调用上下文）**: 包含用户消息、会话 ID、近期审批记录等上下文信息
- **SecurityPolicyMode（安全策略模式）**: standard/strict/relaxed 三种内置策略模式

## 需求

### 需求 1：扩展内置风险模式覆盖范围

**用户故事：** 作为系统管理员，我希望安全护栏能识别更多类型的危险操作，以便在 LLM 代理执行工具时拦截潜在的系统破坏行为。

#### 验收标准

1. WHEN 工具调用参数包含凭据泄露模式（如 `cat ~/.ssh/id_rsa`、`cat /etc/shadow`、读取 `.env` 文件中的密钥），THE RiskAnalyzer SHALL 将该调用评估为 high 或 critical 风险等级
2. WHEN 工具调用参数包含容器/虚拟化逃逸命令（如 `docker run --privileged`、`nsenter`、`chroot`），THE RiskAnalyzer SHALL 将该调用评估为 critical 风险等级
3. WHEN 工具调用参数包含网络侦察命令（如 `nmap`、`masscan`、端口扫描模式），THE RiskAnalyzer SHALL 将该调用评估为 high 风险等级
4. WHEN 工具调用参数包含磁盘格式化或分区操作命令（如 `mkfs`、`fdisk`、`diskpart`），THE RiskAnalyzer SHALL 将该调用评估为 critical 风险等级
5. WHEN 工具调用参数包含内核模块加载命令（如 `insmod`、`modprobe`），THE RiskAnalyzer SHALL 将该调用评估为 critical 风险等级
6. WHEN 工具调用参数包含定时任务篡改命令（如 `crontab -e`、写入 `/etc/cron.d/`），THE RiskAnalyzer SHALL 将该调用评估为 high 风险等级
7. WHEN 工具调用参数包含 Git 强制推送或历史重写命令（如 `git push --force`、`git rebase` 到远程分支），THE RiskAnalyzer SHALL 将该调用评估为 medium 风险等级

### 需求 2：可配置的护栏扩展机制

**用户故事：** 作为项目开发者，我希望能通过配置文件自定义安全护栏规则，以便根据项目特定需求扩展或调整安全策略。

#### 验收标准

1. THE Firewall SHALL 支持从项目目录下的 `.maclaw/guardrail-profiles/` 路径加载自定义护栏配置档（GuardrailProfile）
2. WHEN 自定义护栏配置档文件为合法 JSON 格式且包含有效的 RiskPattern 数组，THE RiskAnalyzer SHALL 将这些自定义模式追加到现有模式列表中参与风险评估
3. WHEN 自定义护栏配置档文件包含自定义 PolicyRule 数组，THE PolicyEngine SHALL 将这些规则按优先级合并到现有规则集中
4. WHEN 自定义护栏配置档文件格式无效或包含非法正则表达式，THE Firewall SHALL 记录错误日志并跳过该配置档，继续使用已有规则运行
5. THE Firewall SHALL 支持通过 `guardrail_mode` 配置字段在 standard、strict、relaxed 三种内置模式间切换
6. WHEN 多个护栏配置档同时存在，THE PolicyEngine SHALL 按文件名字母序加载，后加载的同名规则覆盖先加载的规则
7. THE GuardrailProfile 配置档 SHALL 支持 `enabled` 字段，WHEN 该字段为 false，THE Firewall SHALL 跳过该配置档

### 需求 3：增强审计与实时告警

**用户故事：** 作为安全运维人员，我希望安全护栏在拦截危险操作时能产生实时告警，并在审计日志中记录更丰富的上下文信息，以便事后追溯和分析。

#### 验收标准

1. WHEN PolicyEngine 的评估结果为 deny 或 ask，THE AuditLog SHALL 在审计条目中额外记录触发的 RiskPattern 名称列表和匹配的参数片段
2. WHEN 风险等级为 critical，THE Firewall SHALL 通过告警回调通知注册的告警接收方
3. THE Firewall SHALL 支持注册多个告警回调函数（AlertCallback），WHEN critical 事件发生时依次调用所有已注册的回调
4. IF 告警回调执行失败，THEN THE Firewall SHALL 记录回调失败日志但继续执行后续回调，确保告警链路不中断
5. THE AuditLog SHALL 在审计条目中记录 LLMReview 的审查结论（verdict）和解释（explanation），WHEN LLMReview 参与了该次安全检查
6. WHEN 审计日志查询请求包含 `risk_patterns` 过滤条件，THE AuditLog SHALL 支持按触发的风险模式名称进行过滤查询

### 需求 4：LLM 安全审查增强

**用户故事：** 作为系统管理员，我希望 LLM 安全审查能覆盖更多场景并提供更可靠的回退机制，以便在 LLM 不可用时仍能保障安全。

#### 验收标准

1. WHEN RiskAnalyzer 评估结果为 high 或 critical 且 LLM 已配置，THE Firewall SHALL 自动触发 LLMReview 进行二次审查
2. WHEN LLMReview 返回 dangerous 判定，THE Firewall SHALL 将该操作视为 deny，无论 PolicyEngine 的原始评估结果
3. WHEN LLMReview 调用超时（超过 10 秒），THE Firewall SHALL 回退到基于规则的判定（RuleBasedFallback），并在审计日志中记录超时事件
4. THE LLMReview SHALL 在安全审查提示词中包含项目路径和当前会话的近期操作历史，以便 LLM 做出更准确的判断
5. WHILE LLM 后端未配置或不可用，THE Firewall SHALL 使用 RuleBasedFallback 进行判定，确保安全检查链路完整

### 需求 5：护栏配置的热加载与验证

**用户故事：** 作为开发者，我希望修改护栏配置后无需重启应用即可生效，并且配置错误不会导致系统崩溃。

#### 验收标准

1. WHEN `.maclaw/guardrail-profiles/` 目录下的配置文件发生变更，THE Firewall SHALL 在 5 秒内检测到变更并重新加载配置
2. THE Firewall SHALL 在加载配置前对所有正则表达式进行预编译验证，IF 正则表达式编译失败，THEN THE Firewall SHALL 拒绝该条规则并记录错误日志
3. WHEN 配置热加载成功，THE Firewall SHALL 在审计日志中记录一条配置变更事件，包含加载的配置档名称和规则数量
4. WHEN 配置热加载过程中发生错误，THE Firewall SHALL 保留当前生效的配置不变，确保系统持续运行
5. THE Firewall SHALL 提供 `ValidateProfile` 方法，WHEN 传入护栏配置档内容，THE Firewall SHALL 返回验证结果（包含错误列表），不实际应用该配置

### 需求 6：会话级安全上下文增强

**用户故事：** 作为系统管理员，我希望安全护栏能基于会话的累积行为进行风险评估，以便检测渐进式攻击模式。

#### 验收标准

1. THE Firewall SHALL 维护每个会话的工具调用历史（最近 50 条），用于累积风险评估
2. WHEN 同一会话在 5 分钟内连续触发 3 次及以上 high 或 critical 风险评估，THE Firewall SHALL 自动将该会话的安全模式升级为 strict
3. WHEN 会话安全模式被自动升级为 strict，THE AuditLog SHALL 记录一条会话安全升级事件
4. THE Firewall SHALL 支持通过 `ResetSessionSecurity` 方法手动重置会话的安全模式到默认值
5. WHEN 会话结束（调用 ClearSession），THE Firewall SHALL 同时清除该会话的工具调用历史和安全模式升级状态

### 需求 7：进程级沙箱隔离

**用户故事：** 作为系统管理员，我希望 MaClaw 代理执行的工具调用能在沙箱环境中运行，以便即使工具行为超出预期，也不会对宿主系统造成不可逆的损害。

#### 验收标准

1. THE Firewall SHALL 支持 `sandbox` 配置项，可选值为 `none`（不隔离）、`os`（操作系统级沙箱）、`docker`（Docker 容器沙箱）
2. WHEN sandbox 模式为 `os` 且运行在 macOS 上，THE Firewall SHALL 使用 Seatbelt（sandbox-exec）对工具进程施加文件系统和网络访问限制
3. WHEN sandbox 模式为 `os` 且运行在 Linux 上，THE Firewall SHALL 使用 Landlock + seccomp-bpf 对工具进程施加文件系统和系统调用限制
4. WHEN sandbox 模式为 `os` 且运行在 Windows 上，THE Firewall SHALL 使用 Job Object 对工具进程施加资源和权限限制
5. WHEN sandbox 模式为 `docker`，THE Firewall SHALL 在受限 Docker 容器中执行工具调用，容器默认挂载项目目录为只读，`/tmp` 为可写
6. WHEN 沙箱初始化失败（如 Docker 未安装、权限不足），THE Firewall SHALL 回退到 `none` 模式并在审计日志中记录沙箱降级事件
7. THE GuardrailProfile SHALL 支持 `sandbox_overrides` 字段，允许按工具名称或风险等级覆盖默认沙箱模式
8. WHEN sandbox 模式不为 `none`，THE AuditLog SHALL 在审计条目中记录实际使用的沙箱类型和隔离参数

### 需求 8：网络访问分级控制

**用户故事：** 作为系统管理员，我希望能对工具调用的网络访问进行五级分级控制，以便在保障功能可用性的同时防止未授权的网络通信，并满足企业内网限定等合规场景。

#### 网络访问级别定义

| 级别 | 说明 |
|------|------|
| `none` | 完全禁止网络访问 |
| `intranet` | 仅允许内网访问（RFC1918 私有地址段 + 自定义内网域名列表） |
| `allowlist` | 白名单模式，仅放行指定域名/端口 |
| `audit` | 全部放行，但所有外网访问记录审计日志 |
| `full` | 不限制、不审计 |

#### 系统必要通信豁免

无论网络级别设置为何值，以下系统必要通信始终放行（system bypass）：
- MaClaw 自身的 LLM 后端地址（对话、安全审查等核心功能所需的 API endpoint）
- 编程工具中配置的 LLM 地址（如 Codex、Claude Code 等 remote 编程工具连接的 LLM API endpoint）
- 已配置的 Hub 地址
- 已配置的 HubCenter 地址

这些地址从运行时配置中自动提取，无需用户手动维护，确保 MaClaw 核心功能和编程工具在任何网络级别下均可正常工作。

#### 验收标准

1. THE GuardrailProfile SHALL 支持 `network` 配置字段，可选值为 `none`、`intranet`、`allowlist`、`audit`、`full`
2. THE Firewall SHALL 维护一个系统豁免列表（system bypass list），自动包含当前已配置的 LLM 后端地址、Hub 地址和 HubCenter 地址，这些地址在任何网络级别下均不受拦截
3. WHEN LLM 后端配置、Hub 地址或 HubCenter 地址发生变更，THE Firewall SHALL 自动更新系统豁免列表
4. WHEN network 模式为 `intranet`，THE Firewall SHALL 仅允许访问 RFC1918 私有地址段（10.0.0.0/8、172.16.0.0/12、192.168.0.0/16）、localhost 和系统豁免列表中的地址
5. WHEN network 模式为 `intranet`，THE GuardrailProfile SHALL 支持 `intranet_domains` 字段，定义额外允许的企业内网域名列表（如 `["*.corp.example.com", "git.internal.io"]`）
6. WHEN network 模式为 `allowlist`，THE GuardrailProfile SHALL 支持 `network_allowlist` 字段，定义允许访问的域名和端口列表（如 `["api.openai.com:443", "registry.npmjs.org:443", "pypi.org:443"]`）
7. WHEN 工具调用尝试访问不在白名单/内网范围/系统豁免列表中的网络地址，THE Firewall SHALL 阻止该网络请求并记录审计日志
8. WHEN network 模式为 `audit`，THE Firewall SHALL 允许所有网络访问，但对每次外网访问（非 RFC1918、非 localhost、非系统豁免列表）在审计日志中记录目标地址、端口和工具名称
9. THE SecurityPolicyMode 与 network 模式 SHALL 存在默认映射关系：strict → `none`、standard → `allowlist`、relaxed → `audit`；`full` 和 `intranet` 需显式配置
10. WHEN 用户在 GuardrailProfile 中显式指定 network 模式，THE Firewall SHALL 使用用户指定值覆盖 SecurityPolicyMode 的默认映射
11. THE Firewall SHALL 提供 `AddToNetworkAllowlist` 和 `RemoveFromNetworkAllowlist` 方法，支持运行时动态调整白名单
12. THE Firewall SHALL 提供 `AddIntranetDomain` 和 `RemoveIntranetDomain` 方法，支持运行时动态调整内网域名列表
13. WHEN network 模式发生变更，THE AuditLog SHALL 记录网络策略变更事件，包含变更前后的级别
14. THE GuardrailProfile SHALL 支持 `network_allowlist_presets` 字段，提供预定义的白名单模板（如 `llm-apis`、`package-managers`、`version-control`），方便快速配置

### 需求 9：安全策略设置入口

**用户故事：** 作为用户，我希望能在 GUI 设置面板和 TUI 命令行中统一管理安全策略（包括护栏模式、沙箱配置、网络分级），以便直观地查看和调整安全配置。

#### 验收标准

1. THE GUI 设置面板 SHALL 在「安全策略」区域展示当前的护栏模式（standard/strict/relaxed）、沙箱模式（none/os/docker）和网络访问级别（none/intranet/allowlist/audit/full）
2. THE GUI 设置面板 SHALL 支持通过下拉选择切换护栏模式、沙箱模式和网络访问级别，变更即时生效
3. WHEN 网络访问级别为 `allowlist`，THE GUI 设置面板 SHALL 展示当前白名单列表，并支持添加/删除域名端口条目
4. WHEN 网络访问级别为 `intranet`，THE GUI 设置面板 SHALL 展示当前内网域名列表，并支持添加/删除内网域名条目
5. WHEN 网络访问级别为 `allowlist`，THE GUI 设置面板 SHALL 展示可用的白名单预设模板（如 `llm-apis`、`package-managers`），支持一键应用
6. THE TUI SHALL 扩展 `maclaw-tui policy` 命令，支持 `policy set --network <level>`、`policy set --sandbox <mode>`、`policy set --guardrail-mode <mode>` 子命令
7. THE TUI SHALL 扩展 `maclaw-tui policy allowlist add/remove <domain:port>` 和 `policy intranet add/remove <domain>` 子命令
8. WHEN 用户通过 GUI 或 TUI 修改安全策略，THE Firewall SHALL 在审计日志中记录策略变更事件，包含变更来源（gui/tui）和变更内容
9. THE SkillSecurityPolicy 结构 SHALL 扩展 `NetworkLevel` 字段（替代原有的 `NetworkAccess` 的 allow/deny/ask 三态），映射到五级网络访问控制
10. THE GUI 设置面板 SHALL 提供「审计日志」Tab 页，展示安全审计日志列表，支持按时间范围、风险等级、工具名称和策略动作进行过滤查询
11. THE 审计日志 Tab SHALL 以表格形式展示审计条目，包含时间戳、工具名称、风险等级、策略动作、结果等关键字段，点击条目可展开查看完整详情（参数、触发的风险模式、LLM 审查结论等）
12. THE TUI SHALL 扩展 `maclaw-tui audit list` 命令，支持 `--level <risk_level>`、`--tool <name>`、`--since <time>` 过滤参数，以及 `--json` 格式输出
