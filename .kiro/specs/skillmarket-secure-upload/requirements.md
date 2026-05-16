# Requirements Document

## Introduction

本文档定义 MaClaw 技能安全上传到 SkillMarket（原 SkillHub）的完整需求，涵盖异步上传流程、服务器端处理与验证、下载加密、延迟验证认证方案、Credits 计费体系、Skill 试用期生命周期管理、MaClaw 自动评分体系、智能搜索（FTS5 + 本地 LLM）、自动 Tag 生成、排行榜、经济系统（平台抽成、买断制、自动定价）、安全标签与权限控制、API Key 池分发管理、退款机制、以及指数回退邮件通知。

系统涉及三个主要组件：MaClaw 客户端（桌面 Agent）、HubCenter（公网服务器）、Hub（内网服务器）。Hub 与 HubCenter 不能直连，MaClaw 客户端可同时访问两者。

## Glossary

- **MaClaw_Client**: MaClaw 桌面 Agent 客户端，用户的本地运行环境，可同时访问 Hub 和 HubCenter
- **HubCenter**: 公网服务器，承载 SkillMarket 服务、用户账户管理、Credits 体系和 Web 前端
- **Hub**: 内网服务器，提供本地认证和 IM 通知能力，与 HubCenter 不能直连
- **SkillMarket**: 面向用户的技能市场品牌名称（代码中可逐步从 SkillHub 迁移）
- **Submission**: 一次技能上传提交，包含唯一 submission_id 和处理状态
- **Skill_Package**: 用户上传的 zip 包，包含 skill.yaml 和数据文件（Python 脚本、Shell 脚本等）
- **skill.yaml**: Skill 包中的元数据描述文件，包含 name、description、tags、triggers 等字段
- **Skill_ID**: 服务器端生成的唯一技能标识符，用于付费标识和下载引用
- **Sandbox**: 服务器端隔离的临时目录，用于解压和验证上传的 Skill 包
- **Unverified_Account**: 未验证账户，通过 email 自动创建，权限受限
- **Verified_Account**: 已验证账户，通过 HubCenter 网页完成 email 或手机号验证
- **Credits**: SkillMarket 内部虚拟货币，用于购买付费 Skill、上传者收益结算
- **RSA_Private_Key**: HubCenter 持有的 RSA 私钥，用于加密下载包中的 salt
- **RSA_Public_Key**: MaClaw_Client 持有的 RSA 公钥，用于解密下载包中的 salt
- **AES_Key**: 由 salt + user_id 派生的对称加密密钥，用于加密/解密 Skill zip 包
- **Trial_State**: 试用状态，Skill 通过语法验证后进入的中间状态，默认持续 1 周（可配置）
- **trial_duration**: 管理员可配置的试用期时长参数，默认值为 7 天
- **auto_publish_threshold**: 管理员可配置的自动上架阈值参数，默认值为 5 个不同 email 的评价
- **Pending_Review_State**: 待人工审核状态，试用期到期未达标或收到恶意评分时进入
- **Rating**: MaClaw 自动评分，取值范围为 -2、-1、0、+1、+2 的整数评分
- **MaClaw_Evaluator**: MaClaw 内置的自动评估模块，在 Skill 执行后根据结果自动生成 Rating
- **Auto_Upload_Trigger**: MaClaw 内置的自动上传触发策略，根据 Skill 使用频率和评分自主决定是否上传
- **Skill_Fingerprint**: 由 (uploader_email, skill_name) 组成的唯一标识，用于服务器端去重和版本管理
- **Skill_Version**: Skill 的版本号，首次上传为 1，同一 Skill_Fingerprint 重复上传时自动递增
- **Uploader_Tier**: 上传者信誉等级（1-4），基于已发布 Skill 数量、平均评分、总下载量计算，影响上传大小限制和频率限制
- **Rate_Limit**: 上传频率限制，按 Uploader_Tier 动态调整，防止滥用上传接口
- **Withdrawn_State**: 上传者主动下架状态，Skill 不再对外展示和下载
- **Zip_Bomb**: 恶意压缩包，解压后体积远超压缩包大小，用于耗尽服务器磁盘空间
- **download_count**: Skill 的累计下载次数，用于信誉等级计算和排名展示
- **FTS5**: SQLite 全文搜索引擎，用于 HubCenter 服务端对 Skill 的 name、description、tags 进行文本匹配
- **SearchService**: HubCenter 搜索服务，基于 FTS5 粗筛和质量排序公式返回候选结果
- **SkillSearcher**: MaClaw 端智能搜索模块，使用本地 LLM 提炼关键词并从搜索结果中精选最匹配的 Skill
- **TagGenerator**: MaClaw 端自动 Tag 生成模块，使用本地 LLM 分析 Skill 内容生成元数据
- **LeaderboardService**: HubCenter 排行榜服务，支持按评分、下载量、最新上传排序
- **Platform_Fee**: 平台手续费，付费下载时从交易金额中抽取 30%
- **Free_Trial_Voucher**: 免费体验券，新用户注册时赠送 3 次，7 天有效，不是 Credits，不产生提现负债
- **pricing_mode**: MaClaw 系统设置中的定价模式，支持 auto（本地 LLM 自动定价）、free（全部免费）、fixed（固定价格）
- **Security_Label**: 安全标签，上传时静态扫描生成，标识 Skill 的权限需求（如 network_access、file_system_access、shell_exec）
- **API_Key_Pool**: 卖家在 HubCenter 上传的一批 API Key，购买时从池中分配给买家
- **API_Key_Assignment**: 一次 API Key 分配记录，绑定 skill_id、buyer_email、api_key，一旦分配不回收
- **Exponential_Backoff_Notification**: 指数回退邮件通知机制，首次立即发送，后续按指数递增间隔发送（1h→2h→4h→...），最多 10 封，满足停止条件后终止
- **Purchase_Record**: 用户购买 Skill 的记录，包含 buyer_email、skill_id、purchased_version、purchase_type（purchase/upgrade/voucher），用于版本升级折扣判定
- **settled**: 卖家收益中已完成交付的部分，可提现
- **pending_settlement**: 卖家收益中尚未完成交付（如待发 API Key）的部分，不可提现

## Requirements

### Requirement 1: 异步上传提交

**User Story:** As a MaClaw 用户, I want MaClaw 自主决定将优质 Skill 上传到 SkillMarket, so that 其他用户可以发现和使用我的技能，无需人工干预。

#### Acceptance Criteria

1. WHEN MaClaw_Client 提交一个 Skill_Package（zip 格式，包含 skill.yaml 和数据文件），THE HubCenter SHALL 接收该 zip 包并返回一个唯一的 submission_id
2. WHEN HubCenter 成功接收 Skill_Package，THE HubCenter SHALL 将该 Submission 的状态设置为 "pending"
3. THE HubCenter SHALL 在后台异步处理已接收的 Skill_Package，处理过程不阻塞上传响应
4. WHEN Skill_Package 处理完成（通过或失败），THE HubCenter SHALL 通过邮件通知上传用户，邮件内容包含处理结果和失败原因（如适用）
5. THE 上传过程 SHALL 由 MaClaw_Client 自主触发，不需要人工干预

### Requirement 2: 上传包格式与约束

**User Story:** As a MaClaw 用户, I want 上传标准格式的 Skill 包, so that 服务器可以正确解析和验证我的技能。

#### Acceptance Criteria

1. THE MaClaw_Client SHALL 以明文 zip 格式上传 Skill_Package 到 HubCenter
2. THE Skill_Package SHALL 包含一个 skill.yaml 文件作为元数据描述
3. IF Skill_Package 不包含 skill.yaml 文件，THEN THE HubCenter SHALL 拒绝该提交并返回描述性错误信息
4. IF Skill_Package 的 zip 格式无效或损坏，THEN THE HubCenter SHALL 拒绝该提交并返回描述性错误信息

### Requirement 3: 服务器端解压与元数据提取

**User Story:** As a SkillMarket 运营者, I want 服务器自动解析上传的 Skill 包, so that 技能元数据可用于展示和 AI 搜索。

#### Acceptance Criteria

1. WHEN HubCenter 开始处理一个 Skill_Package，THE HubCenter SHALL 将 zip 包解压到一个隔离的 Sandbox 目录
2. WHEN 解压完成，THE HubCenter SHALL 解析 skill.yaml 文件并提取 name、description、tags、triggers 字段作为元数据
3. WHEN 元数据提取成功，THE HubCenter SHALL 生成一个唯一的 Skill_ID 用于付费标识和下载引用
4. THE HubCenter SHALL 以明文形式存储通过验证的 Skill_Package

### Requirement 4: 语法验证

**User Story:** As a SkillMarket 运营者, I want 服务器自动验证上传文件的语法正确性, so that 只有合格的 Skill 才能发布到市场。

#### Acceptance Criteria

1. WHEN Skill_Package 包含 YAML 文件，THE HubCenter SHALL 对每个 YAML 文件执行语法检查
2. WHEN Skill_Package 包含 Python 文件（.py），THE HubCenter SHALL 使用 py_compile 对每个 Python 文件执行语法检查
3. WHEN Skill_Package 包含 Shell 脚本文件（.sh），THE HubCenter SHALL 使用 bash -n 对每个 Shell 脚本执行语法检查
4. IF 任何文件未通过语法验证，THEN THE HubCenter SHALL 将该 Submission 标记为失败，并记录具体的文件名和错误信息

### Requirement 5: 下载加密

**User Story:** As a MaClaw 用户, I want 下载的 Skill 包经过加密保护, so that 技能内容只能被授权用户使用。

#### Acceptance Criteria

1. THE HubCenter SHALL 在首次启动时检查 RSA-2048 密钥对是否存在，IF 不存在 THEN 自动生成并持久化存储，IF 已存在 THEN 直接加载，SHALL NOT 覆盖已有密钥对
2. WHEN MaClaw_Client 本地未缓存 RSA_Public_Key，THE MaClaw_Client SHALL 通过 HubCenter API 获取公钥并缓存到本地
3. WHEN MaClaw_Client 请求下载一个 Skill，THE HubCenter SHALL 生成一个随机 salt
4. WHEN HubCenter 生成 salt 后，THE HubCenter SHALL 使用 AES 算法以 salt 和请求用户的 user_id 派生对称密钥，加密整个 Skill zip 包
5. WHEN HubCenter 完成 zip 包加密后，THE HubCenter SHALL 使用 RSA_Private_Key 加密 salt
6. THE HubCenter SHALL 将 encrypted_salt 和 encrypted_zip 作为下载响应返回给 MaClaw_Client
7. WHEN MaClaw_Client 收到加密下载包，THE MaClaw_Client SHALL 使用缓存的 RSA_Public_Key 解密 salt，再使用 salt 和 user_id 派生对称密钥解密 zip 包
8. THE MaClaw_Client SHALL 将加密包完整保存在磁盘，仅在首次使用时解密到临时目录

### Requirement 6: 延迟验证账户创建

**User Story:** As a MaClaw 用户, I want 首次访问 SkillMarket 时自动获得账户, so that 无需提前注册即可开始使用基本功能。

#### Acceptance Criteria

1. WHEN MaClaw_Client 首次访问 SkillMarket 且提供 email 地址，THE HubCenter SHALL 使用该 email 自动创建一个 Unverified_Account
2. THE HubCenter SHALL 使用 email 作为用户的唯一身份标识
3. WHILE 用户账户处于 Unverified_Account 状态，THE HubCenter SHALL 允许该用户浏览 Skill 列表
4. WHILE 用户账户处于 Unverified_Account 状态，THE HubCenter SHALL 允许该用户下载免费 Skill
5. WHILE 用户账户处于 Unverified_Account 状态，THE HubCenter SHALL 允许该用户上传 Skill
6. WHILE 用户账户处于 Unverified_Account 状态，THE HubCenter SHALL 允许该用户积累 Credits
7. WHILE 用户账户处于 Unverified_Account 状态，THE HubCenter SHALL 允许该用户使用 Free_Trial_Voucher 下载付费 Skill（但不允许使用 Credits 购买付费 Skill）

### Requirement 7: 账户验证与接管

**User Story:** As a 用户, I want 通过 HubCenter 网页验证身份后获得完整权限, so that 可以进行充值、提现和购买付费 Skill。

#### Acceptance Criteria

1. THE HubCenter SHALL 提供 Web 前端页面，允许用户通过 email 或手机号完成身份验证
2. WHEN 用户完成身份验证，THE HubCenter SHALL 将该账户状态从 Unverified_Account 升级为 Verified_Account
3. WHEN 验证时发现已存在使用相同 email 的 Unverified_Account，THE HubCenter SHALL 直接接管该未验证账户，保留其已有数据（上传的 Skill、积累的 Credits）
4. WHILE 用户账户处于 Verified_Account 状态，THE HubCenter SHALL 允许该用户执行充值操作
5. WHILE 用户账户处于 Verified_Account 状态，THE HubCenter SHALL 允许该用户执行提现操作
6. WHILE 用户账户处于 Verified_Account 状态，THE HubCenter SHALL 允许该用户购买付费 Skill

### Requirement 8: Credits 计费体系

**User Story:** As a SkillMarket 用户, I want 通过 Credits 购买和出售技能, so that 技能创作者可以获得收益，消费者可以获取优质技能。

#### Acceptance Criteria

1. THE HubCenter SHALL 为每个用户维护一个 Credits 余额账户
2. WHEN 上传者的 Skill 被其他用户付费下载，THE HubCenter SHALL 将对应的 Credits 收益计入上传者的余额
3. WHEN 用户下载一个付费 Skill，THE HubCenter SHALL 从该用户的 Credits 余额中扣除对应费用
4. IF 用户的 Credits 余额不足以购买目标 Skill，THEN THE HubCenter SHALL 拒绝该下载请求并返回余额不足的提示信息
5. WHILE 用户账户处于 Unverified_Account 状态，THE HubCenter SHALL 拒绝充值和提现操作并提示用户先完成身份验证

### Requirement 9: Credits 不足通知

**User Story:** As a MaClaw 用户, I want 在 Credits 不足时收到通知, so that 可以及时充值以购买需要的付费 Skill。

#### Acceptance Criteria

1. WHEN MaClaw_Client 搜索到付费 Skill 且用户 Credits 余额不足以购买，THE MaClaw_Client SHALL 通过 IM 或邮件通知用户前往 HubCenter 进行身份认证和充值
2. THE MaClaw_Client SHALL 在通知中包含目标 Skill 名称、所需 Credits 数量和当前余额信息

### Requirement 10: 账户状态展示

**User Story:** As a MaClaw 用户, I want 在客户端查看我的 SkillMarket 账户状态, so that 随时了解自己的账户信息。

#### Acceptance Criteria

1. THE MaClaw_Client SHALL 在设置页面显示用户的 SkillMarket 账户状态，包含 email 地址、Credits 余额和验证状态（已验证/未验证）
2. WHEN 用户的账户状态或 Credits 余额发生变化，THE MaClaw_Client SHALL 在下次查看设置页面时展示最新信息

### Requirement 11: HubCenter Web 管理前端

**User Story:** As a 用户, I want 通过 HubCenter 网页管理我的 SkillMarket 账户, so that 可以查看 Credits、验证身份、充值和提现。

#### Acceptance Criteria

1. THE HubCenter SHALL 提供 Web 前端页面，允许用户查看 Credits 余额和交易记录
2. THE HubCenter SHALL 提供 Web 前端页面，允许已验证用户执行充值操作
3. THE HubCenter SHALL 提供 Web 前端页面，允许已验证用户执行提现操作
4. THE HubCenter SHALL 提供 Web 前端页面，允许用户查看已上传的 Skill 列表及其收益统计

### Requirement 12: 架构约束遵循

**User Story:** As a 系统架构师, I want 系统遵循既定的网络拓扑约束, so that 安全边界得到保障。

#### Acceptance Criteria

1. THE HubCenter SHALL 作为公网服务独立运行，不依赖与 Hub 的直接网络连接
2. THE MaClaw_Client SHALL 能够同时访问 Hub（内网）和 HubCenter（公网）
3. THE HubCenter SHALL 独立处理所有 SkillMarket 相关的业务逻辑，包括账户管理、Skill 存储和 Credits 结算

### Requirement 13: skill.yaml 解析与格式化

**User Story:** As a 开发者, I want skill.yaml 的解析和格式化保持一致性, so that 元数据在存储和展示过程中不会丢失或变形。

#### Acceptance Criteria

1. WHEN HubCenter 接收到包含 skill.yaml 的 Skill_Package，THE HubCenter SHALL 将 skill.yaml 解析为结构化的 SkillMetadata 对象
2. THE HubCenter SHALL 能够将 SkillMetadata 对象格式化回有效的 YAML 文本
3. FOR ALL 有效的 SkillMetadata 对象，解析 skill.yaml 后格式化再解析 SHALL 产生等价的对象（round-trip 属性）
4. IF skill.yaml 包含无法识别的字段，THEN THE HubCenter SHALL 保留这些字段而不丢弃

### Requirement 14: Skill 试用期生命周期

**User Story:** As a SkillMarket 运营者, I want Skill 通过语法验证后进入试用期而非直接发布, so that 社区用户可以在正式上架前试用并评价，确保 Skill 质量。

#### Acceptance Criteria

1. WHEN Skill_Package 通过语法验证，THE HubCenter SHALL 将该 Skill 的状态设置为 "trial"，而非直接设置为 "published"
2. WHILE Skill 处于 Trial_State，THE HubCenter SHALL 在 Skill 列表和详情页标注 "试用中" 标识
3. WHILE Skill 处于 Trial_State，THE HubCenter SHALL 允许所有用户浏览和下载该 Skill
4. THE HubCenter SHALL 使用管理员可配置的 trial_duration 参数控制试用期时长，默认值为 7 天
5. WHEN 试用期内满足以下全部条件：（a）至少 auto_publish_threshold 个不同 email 的用户提交了 Rating，且（b）所有 Rating 的平均分 ≥ 0，THE HubCenter SHALL 将该 Skill 的状态从 "trial" 变更为 "published"
6. WHEN 试用期到期且未满足自动上架条件，THE HubCenter SHALL 将该 Skill 的状态从 "trial" 变更为 "pending_review"
7. THE HubCenter SHALL 使用管理员可配置的 auto_publish_threshold 参数控制自动上架所需的最少评价人数，默认值为 5

### Requirement 15: 管理员人工审核

**User Story:** As a SkillMarket 管理员, I want 对未达标的 Skill 进行人工审核, so that 可以决定是否允许其正式上架。

#### Acceptance Criteria

1. WHILE Skill 处于 Pending_Review_State，THE HubCenter SHALL 在管理后台展示该 Skill 的详细信息、试用期评分数据和用户反馈
2. WHEN 管理员批准一个处于 Pending_Review_State 的 Skill，THE HubCenter SHALL 将该 Skill 的状态变更为 "published"
3. WHEN 管理员拒绝一个处于 Pending_Review_State 的 Skill，THE HubCenter SHALL 将该 Skill 的状态变更为 "rejected"
4. THE HubCenter SHALL 在管理员完成审核后通过邮件通知 Skill 上传者审核结果

### Requirement 16: MaClaw 自动评分

**User Story:** As a MaClaw 用户, I want MaClaw 在执行 Skill 后自动生成评分, so that 无需手动评价即可为社区贡献质量反馈。

#### Acceptance Criteria

1. WHEN MaClaw_Client 完成一次 Skill 执行，THE MaClaw_Evaluator SHALL 根据执行结果自动生成一个 Rating，取值范围为 -2、-1、0、+1、+2
2. WHEN Skill 执行成功且达到预期效果，THE MaClaw_Evaluator SHALL 生成 +1 或 +2 的 Rating
3. WHEN Skill 执行后无明显效果，THE MaClaw_Evaluator SHALL 生成 0 的 Rating
4. WHEN Skill 执行过程中出现错误或崩溃，THE MaClaw_Evaluator SHALL 生成 -1 的 Rating
5. WHEN Skill 执行过程中触发安全告警（危险操作、数据窃取尝试），THE MaClaw_Evaluator SHALL 生成 -2 的 Rating
6. WHEN MaClaw_Evaluator 生成 Rating 后，THE MaClaw_Client SHALL 将该 Rating 连同用户 email 提交到 HubCenter

### Requirement 17: 评分存储与去重

**User Story:** As a SkillMarket 运营者, I want 评分按 email 去重并正确计算平均分, so that 评分数据真实反映社区对 Skill 的评价。

#### Acceptance Criteria

1. THE HubCenter SHALL 以 email 为维度对同一 Skill 的 Rating 进行去重，每个 email 仅保留最新一次 Rating
2. WHEN 同一 email 对同一 Skill 提交新的 Rating，THE HubCenter SHALL 用新 Rating 覆盖该 email 之前的 Rating
3. THE HubCenter SHALL 将 Rating 为 0 的评价计入评价人数和平均分计算
4. THE HubCenter SHALL 在计算平均分时使用所有去重后的 Rating（包括 0 分）的算术平均值

### Requirement 18: 恶意评分紧急下架

**User Story:** As a SkillMarket 运营者, I want 收到恶意评分的 Skill 立即下架, so that 存在安全风险的 Skill 不会继续被用户使用。

#### Acceptance Criteria

1. WHEN 任意用户对一个 Skill 提交 Rating 为 -2（恶意），THE HubCenter SHALL 立即将该 Skill 的状态变更为 "pending_review"，无论该 Skill 当前处于 "trial" 还是 "published" 状态
2. WHEN Skill 因 -2 Rating 进入 Pending_Review_State，THE HubCenter SHALL 通过邮件通知 Skill 上传者和管理员
3. WHEN 试用期结束时 Skill 的平均 Rating 低于 0，THE HubCenter SHALL 将该 Skill 的状态变更为 "pending_review"

### Requirement 19: MaClaw 自动上传触发

**User Story:** As a MaClaw 用户, I want MaClaw 自主判断哪些 Skill 值得上传, so that 优质的本地 Skill 能自动分享到 SkillMarket 而无需人工操作。

#### Acceptance Criteria

1. THE MaClaw_Client SHALL 跟踪每个本地 Skill 的执行次数和自动评分历史
2. WHEN 一个本地 Skill 满足以下全部条件：（a）被成功执行至少 3 次，且（b）最近执行的自动评分平均值 ≥ +1，THE Auto_Upload_Trigger SHALL 自动触发上传流程
3. WHEN Auto_Upload_Trigger 决定上传一个 Skill，THE MaClaw_Client SHALL 自动将该 Skill 打包为 Skill_Package 并提交到 HubCenter，无需用户确认
4. THE MaClaw_Client SHALL 在上传前检查该 Skill 是否已上传过（本地记录），IF 已上传且本地版本无变更 THEN SHALL 跳过上传

### Requirement 20: 服务器端去重与版本升级

**User Story:** As a SkillMarket 运营者, I want 服务器自动识别重复上传并作为版本升级处理, so that 同一 Skill 的迭代更新不会产生重复条目。

#### Acceptance Criteria

1. THE HubCenter SHALL 使用 Skill_Fingerprint（uploader_email + skill_name）作为 Skill 的唯一标识进行去重
2. WHEN HubCenter 收到一个 Skill_Package 且该 Skill_Fingerprint 在系统中不存在，THE HubCenter SHALL 创建新的 Skill 记录，version 设为 1
3. WHEN HubCenter 收到一个 Skill_Package 且该 Skill_Fingerprint 在系统中已存在，THE HubCenter SHALL 将此次提交作为版本升级处理，version 自动递增
4. WHEN 版本升级时，THE HubCenter SHALL 保留历史版本记录，但仅最新版本对外可见和可下载
5. WHEN 版本升级时旧版本处于 "published" 状态，THE HubCenter SHALL 在新版本通过 trial 流程之前保持旧版本继续可用
6. WHEN 新版本通过 trial 流程并变更为 "published" 状态，THE HubCenter SHALL 将旧版本标记为 "superseded"，新版本替代旧版本对外展示
7. THE HubCenter SHALL 在 Skill 详情页展示版本号和版本历史


### Requirement 21: 上传者主动下架

**User Story:** As a Skill 上传者, I want 通过 HubCenter Web 管理平台主动下架自己的 Skill, so that 可以在发现问题或不再维护时及时撤回。

#### Acceptance Criteria

1. THE HubCenter Web 前端 SHALL 提供 "我的 Skill" 管理页面，展示上传者名下所有 Skill 及其当前状态
2. WHEN 上传者通过 email 登录 HubCenter Web 后，THE HubCenter SHALL 允许上传者对自己名下处于 "trial" 或 "published" 状态的 Skill 执行下架操作
3. WHEN 上传者执行下架操作，THE HubCenter SHALL 将该 Skill 的状态变更为 "withdrawn"
4. WHILE Skill 处于 "withdrawn" 状态，THE HubCenter SHALL 不在 Skill 列表中展示该 Skill，且不允许新用户下载
5. THE HubCenter SHALL 仅允许 Skill 的原始上传者（email 匹配）执行下架操作

### Requirement 22: 上传频率限制

**User Story:** As a SkillMarket 运营者, I want 限制同一用户的上传频率, so that 防止滥用上传接口和资源浪费。

#### Acceptance Criteria

1. THE HubCenter SHALL 对同一 email 的上传提交实施频率限制
2. THE HubCenter SHALL 使用管理员可配置的频率限制参数，默认值为每小时 5 次、每天 20 次
3. IF 上传者的提交频率超过限制，THEN THE HubCenter SHALL 拒绝该提交并返回描述性错误信息，包含下次可提交的时间
4. THE HubCenter SHALL 根据上传者的 Uploader Tier（信誉等级）调整频率限制，高等级用户享有更宽松的限制
5. THE HubCenter SHALL 在频率限制检查中仅计算状态为 "pending"、"processing"、"success" 的提交，不计算 "failed" 的提交

### Requirement 23: 上传大小限制与信誉等级体系

**User Story:** As a SkillMarket 运营者, I want 根据上传者信誉等级动态调整上传限制, so that 优质贡献者获得更大的上传空间和更宽松的频率限制。

#### Acceptance Criteria

1. THE HubCenter SHALL 对上传的 Skill_Package zip 包实施大小限制，默认最大 10MB
2. THE HubCenter SHALL 为每个上传者维护一个 Uploader Tier（信誉等级），基于以下维度计算：（a）已发布 Skill 数量，（b）所有 Skill 的平均评分，（c）所有 Skill 的总下载量
3. THE HubCenter SHALL 支持至少 4 个信誉等级，每个等级对应不同的上传大小限制：Tier 1（默认）= 10MB，Tier 2 = 20MB，Tier 3 = 50MB，Tier 4 = 100MB
4. THE HubCenter SHALL 根据 Uploader Tier 动态调整上传频率限制（Requirement 22），高等级用户享有更宽松的频率限制
5. THE HubCenter SHALL 支持信誉等级降级：当上传者的评分指标下降（如 Skill 被下架、平均评分降低）时，Tier 应相应降低
6. THE HubCenter SHALL 使用管理员可配置的等级阈值参数，允许管理员调整各等级的晋升/降级条件和对应的限制值
7. IF 上传的 Skill_Package 超过当前 Uploader Tier 允许的大小限制，THEN THE HubCenter SHALL 拒绝该提交并返回描述性错误信息，包含当前等级和允许的最大大小

### Requirement 24: Sandbox 清理与 Zip 炸弹防护

**User Story:** As a SkillMarket 运营者, I want 服务器自动清理临时文件并防护恶意压缩包, so that 服务器磁盘空间和安全得到保障。

#### Acceptance Criteria

1. WHEN Skill_Package 处理完成（无论成功或失败），THE HubCenter SHALL 立即删除对应的 Sandbox 临时目录
2. THE HubCenter SHALL 在解压 zip 包时检查解压后的总大小，IF 解压后总大小超过 zip 包大小的 20 倍或超过 500MB（以较小者为准），THEN SHALL 中止解压并将该 Submission 标记为失败
3. THE HubCenter SHALL 在解压 zip 包时限制单个文件的最大大小为 50MB，IF 任何单个文件超过此限制，THEN SHALL 中止解压并将该 Submission 标记为失败
4. THE HubCenter SHALL 在解压 zip 包时限制文件总数不超过 1000 个，IF 文件数量超过此限制，THEN SHALL 中止解压并将该 Submission 标记为失败

### Requirement 25: Skill 搜索与列表

**User Story:** As a MaClaw 用户, I want 搜索和浏览 SkillMarket 中的 Skill, so that 可以发现和获取需要的技能。

#### Acceptance Criteria

1. THE HubCenter SHALL 提供 Skill 列表 API，返回所有处于 "trial" 或 "published" 状态的 Skill，不包含 "withdrawn"、"rejected"、"pending_review"、"superseded" 状态的 Skill
2. THE HubCenter SHALL 支持按 name、description、tags 进行关键词搜索
3. THE HubCenter SHALL 支持按状态（trial/published）、价格（免费/付费）、评分范围进行筛选
4. THE HubCenter SHALL 在列表结果中包含 Skill 的 name、description、tags、status、price、average_rating、download_count、version、security_labels 信息
5. THE HubCenter SHALL 支持分页查询，默认每页 20 条
6. THE HubCenter SHALL 在 Skill 详情 API 中额外返回 api_key_stock_status（充足/紧张/缺货，仅对声明了 required_env 的 Skill）

### Requirement 26: 下载量统计

**User Story:** As a SkillMarket 运营者, I want 准确统计每个 Skill 的下载量, so that 下载数据可用于信誉等级计算和 Skill 排名。

#### Acceptance Criteria

1. WHEN 用户成功下载一个 Skill（加密包生成并返回成功），THE HubCenter SHALL 将该 Skill 的下载计数加 1
2. THE HubCenter SHALL 为每个 Skill 维护一个 download_count 字段
3. THE HubCenter SHALL 在计算 Uploader Tier 时使用上传者所有 published 状态 Skill 的 download_count 总和作为 total_downloads
4. THE HubCenter SHALL 在 Skill 详情和列表中展示 download_count

### Requirement 27: 试用期 Skill 限免

**User Story:** As a SkillMarket 运营者, I want 试用期的 Skill 免费提供给所有用户, so that 社区用户可以无门槛试用并评价新 Skill。

#### Acceptance Criteria

1. WHILE Skill 处于 Trial_State，THE HubCenter SHALL 忽略该 Skill 的 price 设置，允许所有用户免费下载
2. WHEN Skill 从 Trial_State 变更为 "published" 状态，THE HubCenter SHALL 恢复该 Skill 的 price 设置，后续下载按 price 收费
3. THE HubCenter SHALL 在 Skill 列表和详情的 API 响应中为 trial 状态的 Skill 标注 "限免" 标识（如 `trial_free: true`）
4. THE HubCenter Web 前端 SHALL 在 Skill 列表和详情页为 trial 状态的 Skill 显示 "试用中·限免" 标识
5. THE HubCenter Web 前端 SHALL 不提供 Skill 下载功能，所有 Skill 详情页 SHALL 标注 "仅 MaClaw 自动下载"
6. THE HubCenter Web 前端 SHALL 仅作为 Skill 浏览、账户管理和管理后台使用，下载操作仅通过 MaClaw_Client API 完成

### Requirement 28: 上传者自评限制

**User Story:** As a SkillMarket 运营者, I want 禁止上传者给自己的 Skill 评分, so that 评分数据不被上传者操纵。

#### Acceptance Criteria

1. WHEN 用户提交 Rating 时，IF 该用户的 email 与目标 Skill 的 uploader_email 相同，THEN THE HubCenter SHALL 拒绝该评分并返回描述性错误信息
2. THE HubCenter SHALL 在评分 API 中执行此检查，确保上传者的自评不会被记录

### Requirement 29: Withdrawn Skill 重新上架

**User Story:** As a Skill 上传者, I want 在下架后可以重新上架未修改的 Skill, so that 不需要重新上传即可恢复 Skill 的可用状态。

#### Acceptance Criteria

1. THE HubCenter Web 前端 SHALL 在 "我的 Skill" 管理页面为处于 "withdrawn" 状态的 Skill 提供 "重新上架" 操作按钮
2. WHEN 上传者对 withdrawn 状态的 Skill 执行重新上架操作，THE HubCenter SHALL 将该 Skill 的状态恢复为下架前的状态（"trial" 或 "published"）
3. THE HubCenter SHALL 仅允许 Skill 的原始上传者（email 匹配）执行重新上架操作
4. IF Skill 在 withdrawn 期间有新版本被上传（即存在更新的版本），THEN THE HubCenter SHALL 拒绝重新上架并提示用户已有新版本
5. WHEN 重新上架的 Skill 恢复为 "trial" 状态时，THE HubCenter SHALL 重新计算 trial 到期时间（从重新上架时刻起 + trial_duration），不沿用原到期时间

### Requirement 30: 并发安全

**User Story:** As a 系统架构师, I want 关键业务操作具备并发安全保障, so that 在高并发场景下数据一致性不被破坏。

#### Acceptance Criteria

1. WHEN 执行 Credits 扣款操作，THE HubCenter SHALL 使用数据库事务确保余额检查和扣款的原子性，防止并发扣款导致余额变为负数
2. WHEN 执行 Rating UPSERT 操作，THE HubCenter SHALL 使用数据库级别的原子操作（INSERT ON CONFLICT UPDATE）确保并发评分不会产生重复记录
3. WHEN 执行 Skill 状态变更操作，THE HubCenter SHALL 使用乐观锁或事务确保并发状态变更不会产生冲突

### Requirement 31: 被拒绝 Skill 重新提交

**User Story:** As a Skill 上传者, I want 在 Skill 被拒绝后修改并重新提交, so that 可以根据审核反馈改进 Skill 并再次尝试上架。

#### Acceptance Criteria

1. WHEN 上传者重新提交一个与已被 "rejected" 状态 Skill 具有相同 Skill_Fingerprint 的 Skill_Package，THE HubCenter SHALL 将此次提交作为版本升级处理，version 自动递增
2. WHEN 被拒绝 Skill 的新版本通过语法验证，THE HubCenter SHALL 将新版本状态设为 "trial"，重新进入试用期流程
3. THE HubCenter SHALL 保留被拒绝版本的历史记录，但不影响新版本的试用期评价


### Requirement 32: MaClaw 智能搜索（全自动）

**User Story:** As a MaClaw 用户, I want MaClaw 在执行任务时自动搜索并安装所需 Skill, so that 无需人工干预即可获取最匹配的能力扩展。

#### Acceptance Criteria

1. WHEN MaClaw_Client 在执行任务过程中发现需要某项能力，THE MaClaw_Client SHALL 使用本地 LLM 从任务上下文中提炼搜索关键词和 tags
2. WHEN MaClaw_Client 生成搜索关键词后，THE MaClaw_Client SHALL 调用 HubCenter 搜索 API `GET /api/v1/skills/search?q=...&tags=...&top_n=20` 获取候选结果
3. THE HubCenter SHALL 使用 SQLite FTS5 全文索引对 Skill 的 name、description、tags 进行文本匹配
4. THE HubCenter SHALL 使用综合质量排序公式计算每个匹配 Skill 的 score：`score = fts_rank * -0.5 + avg_rating * 0.2 + log(downloads + 1) * 0.2 + recency * 0.1`，其中 fts_rank 为 FTS5 返回的负数排名值（越小越相关，乘以 -0.5 转为正向），recency 为归一化的时间新鲜度（0~1）
5. THE HubCenter 搜索 API SHALL 返回 top_n 条结果（默认 20），每条结果包含 name、description、tags、score、price、status、avg_rating、download_count 字段
6. WHEN MaClaw_Client 收到搜索结果后，THE MaClaw_Client SHALL 使用本地 LLM 从 top_n 候选中精选最匹配当前任务需求的 Skill
7. WHEN MaClaw_Client 精选出目标 Skill 后，THE MaClaw_Client SHALL 自动下载、安装并使用该 Skill，全程无需人工干预
8. THE HubCenter 搜索 API SHALL 零 LLM 依赖，所有智能筛选由 MaClaw_Client 本地 LLM 完成
9. IF HubCenter 搜索 API 未返回任何匹配结果，THEN THE MaClaw_Client SHALL 记录搜索失败日志并继续执行任务，不中断当前流程

### Requirement 33: 自动 Tag 生成（全自动）

**User Story:** As a MaClaw 用户, I want MaClaw 在上传 Skill 前自动生成高质量的元数据, so that Skill 在 SkillMarket 中更容易被搜索和发现。

#### Acceptance Criteria

1. WHEN MaClaw_Client 准备上传一个 Skill_Package，THE MaClaw_Client SHALL 使用本地 LLM 分析 Skill 内容（skill.yaml 和关联脚本文件）
2. WHEN 本地 LLM 分析完成，THE MaClaw_Client SHALL 自动生成或补全 skill.yaml 中的 name、description、tags、triggers 字段
3. THE MaClaw_Client SHALL 将 tags 分为功能类（如 "文件管理"、"数据处理"、"网络请求"）和领域类（如 "开发工具"、"办公自动化"、"系统运维"）
4. THE MaClaw_Client SHALL 在上传前将生成的元数据写入 skill.yaml 文件，确保 HubCenter 接收到的 Skill_Package 已包含完整元数据
5. THE 自动 Tag 生成过程 SHALL 由 MaClaw_Client 本地 LLM 完成，人类全程不参与
6. IF skill.yaml 中已存在 name、description、tags、triggers 字段且内容非空，THEN THE MaClaw_Client SHALL 保留原有内容，仅补全缺失字段

### Requirement 34: 排行榜

**User Story:** As a MaClaw 用户, I want 浏览 SkillMarket 热门 Skill 排行榜, so that 可以主动发现高质量和流行的 Skill。

#### Acceptance Criteria

1. THE HubCenter SHALL 提供排行榜 API `GET /api/v1/skills/top?sort=rating|downloads|newest&limit=10`
2. THE HubCenter SHALL 支持按以下维度排序：rating（平均评分从高到低）、downloads（下载量从高到低）、newest（上传时间从新到旧）
3. THE HubCenter 排行榜 API SHALL 默认返回 10 条结果，支持通过 limit 参数调整（最大 50）
4. THE HubCenter 排行榜 API SHALL 仅包含处于 "published" 状态的 Skill
5. THE HubCenter 排行榜 API SHALL 返回每条结果的 name、description、tags、avg_rating、download_count、price、uploader_email、created_at 字段
6. THE MaClaw_Client SHALL 支持主动调用排行榜 API 浏览热门 Skill，并根据需要自动下载安装

### Requirement 35: 经济系统启动

**User Story:** As a SkillMarket 运营者, I want 建立零平台支出的经济体系, so that 平台只收钱不付钱，上传者通过定价获得收益，消费者有合理的付费和体验机制。

#### Acceptance Criteria

1. WHEN 用户付费下载一个 Skill，THE HubCenter SHALL 从交易金额中抽取 30% 作为平台手续费，剩余 70% 计入上传者 Credits 余额
2. THE 上传者 SHALL 在 skill.yaml 中通过 price 字段设置 Skill 价格（单位：Credits），price 为 0 表示免费
3. WHILE Skill 处于 Trial_State，THE HubCenter SHALL 忽略 price 设置允许免费下载（与 Requirement 27 一致），WHEN Skill 变更为 "published" 状态后恢复收费
4. THE 经济模型 SHALL 采用买断制：用户付费下载后永久可用该版本，版本升级需再次付费
5. WHEN 同一用户下载同一 Skill 的新版本（version > 已购版本），THE HubCenter SHALL 按新版本 price 的 50% 收取升级费用
6. THE HubCenter SHALL 为每次成功的付费下载维护 Purchase_Record（buyer_email、skill_id、purchased_version、purchase_type），用于版本升级折扣判定
7. WHEN 新用户首次创建账户（通过 EnsureAccount），THE HubCenter SHALL 赠送 3 次免费体验券（Free_Trial_Voucher），有效期 7 天
8. THE Free_Trial_Voucher SHALL 不是 Credits，不可提现，不产生平台支出负债，仅用于免费下载付费 Skill
9. WHEN 用户使用 Free_Trial_Voucher 下载付费 Skill，THE HubCenter SHALL 扣减 1 次体验券额度，不扣 Credits，不给上传者入账（平台零支出）
10. THE Free_Trial_Voucher SHALL 不适用于声明了 `required_env` 的 Skill（因为体验券不给上传者入账，无法触发 API Key 分配，对买卖双方均不合理）
11. WHEN Free_Trial_Voucher 过期（超过 7 天）或 3 次用完，THE HubCenter SHALL 不再提供免费下载，后续下载按正常价格扣费
12. 通过 Free_Trial_Voucher 下载的版本 SHALL 不计入 Purchase_Record，后续该用户购买同一 Skill 时按全价收费（不享受升级折扣）
13. THE HubCenter SHALL 不提供任何形式的 bonus Credits、首次发布奖励、下载里程碑奖励或每日免费下载（平台零支出原则）
14. Credits 汇率参考：1 Credit ≈ 0.1 元人民币
15. THE HubCenter SHALL 在 Credits 交易记录中记录交易类型（purchase、earning、topup、withdraw、upgrade、refund、platform_fee），确保所有资金流向可追溯
16. WHEN 用户执行提现操作，THE HubCenter SHALL 仅允许提现 settled 部分的 Credits（不含 pending_settlement），确保未完成交付的收益不可提前提现


### Requirement 36: 自动定价

**User Story:** As a MaClaw 用户, I want MaClaw 在上传 Skill 时自动设置合理价格, so that 无需手动定价即可参与 SkillMarket 经济体系。

#### Acceptance Criteria

1. THE MaClaw_Client SHALL 在系统设置中提供 pricing_mode 配置项，支持三种模式：auto（默认）、free、fixed
2. WHEN pricing_mode 为 auto，THE MaClaw_Client SHALL 在上传前使用本地 LLM 根据 Skill 复杂度自动计算 price 并写入 skill.yaml
3. THE 自动定价 SHALL 在 Tag 生成时顺便完成（复用同一次 LLM 分析），不额外增加 LLM 调用
4. THE 自动定价 SHALL 遵循以下参考区间：极简 Skill（单文件、简单逻辑）→ 免费（price=0），普通 Skill（多文件、中等逻辑）→ 5~15 Credits，复杂 Skill（多文件、外部依赖、复杂逻辑）→ 20~50 Credits
5. WHEN pricing_mode 为 free，THE MaClaw_Client SHALL 将所有上传 Skill 的 price 设为 0
6. WHEN pricing_mode 为 fixed，THE MaClaw_Client SHALL 使用系统设置中的 fixed_price 值作为所有上传 Skill 的 price
7. THE pricing_mode 默认值 SHALL 为 auto，因为无人干预是核心使用模式，大部分用户不会手动设置 price
8. IF skill.yaml 中已存在非零 price 字段且 pricing_mode 为 auto，THEN THE MaClaw_Client SHALL 保留原有 price，不覆盖


### Requirement 37: Skill 安全标签与权限控制

**User Story:** As a SkillMarket 运营者, I want 对上传的 Skill 进行安全扫描并标注权限需求, so that 买家购买前了解 Skill 的安全风险，MaClaw 执行时可以控制权限。

#### Acceptance Criteria

1. WHEN HubCenter 处理 Skill_Package 时，THE HubCenter SHALL 对包内所有脚本文件执行静态安全扫描
2. THE 静态安全扫描 SHALL 检测以下风险项：硬编码密钥/Token（正则匹配常见模式）、危险操作（rm -rf、format、DROP TABLE 等）、外部网络调用（curl、wget、requests、http.Get 等）、Shell 命令执行（os.system、subprocess、exec 等）
3. WHEN 扫描完成，THE HubCenter SHALL 为该 Skill 生成安全标签（Security_Label）列表，可能的标签包括：`network_access`、`file_system_access`、`shell_exec`、`hardcoded_secrets`、`database_access`
4. THE HubCenter SHALL 在 Skill 列表和详情 API 中返回 Security_Label 列表，供买家购买前查看
5. THE skill.yaml SHALL 支持 `permissions` 字段，上传者可声明 Skill 所需权限（如 `permissions: [network, filesystem]`）
6. THE skill.yaml SHALL 支持 `required_env` 字段，声明 Skill 运行所需的环境变量/API Key 名称列表（如 `required_env: [OPENAI_API_KEY]`）
7. THE MaClaw_Client SHALL 在系统设置中提供安全策略配置，对每种权限类型支持三种模式：allow（允许）、deny（拒绝）、ask（执行前通过 IM 询问用户）
8. WHEN MaClaw_Client 执行一个 Skill 且该 Skill 的 Security_Label 包含被设为 deny 的权限类型，THE MaClaw_Client SHALL 拒绝执行并记录日志
9. WHEN MaClaw_Client 执行一个 Skill 且该 Skill 的 Security_Label 包含被设为 ask 的权限类型，THE MaClaw_Client SHALL 通过 IM 询问用户是否允许，用户拒绝则不执行
10. IF 静态扫描检测到 `hardcoded_secrets`，THEN THE HubCenter SHALL 将该 Submission 标记为失败并通知上传者移除硬编码密钥


### Requirement 38: API Key 池分发

**User Story:** As a Skill 卖家, I want 在 HubCenter 上管理 API Key 池, so that 买家购买后自动获得 API Key，无需手动沟通分发。

#### Acceptance Criteria

1. THE HubCenter Web 前端 SHALL 在 "我的 Skill" 管理页面为声明了 `required_env` 的 Skill 提供 "API Key 管理" 入口
2. WHEN 卖家进入 API Key 管理页面，THE HubCenter SHALL 允许卖家批量上传 API Key（每行一个），每个 Key 关联一个 env_name（对应 skill.yaml 中的 required_env 条目）
3. THE HubCenter SHALL 加密存储所有上传的 API Key（AES-256-GCM，密钥由 HubCenter 管理）
4. THE HubCenter SHALL 为每个 API Key 维护状态：available（可分配）、assigned（已分配）、refunded（已退款，建议作废）
5. WHEN 买家成功购买（付费下载）一个声明了 `required_env` 的 Skill 且 API Key 池有 available 的 Key，THE HubCenter SHALL 立即从池中分配一个 available 状态的 Key，标记为 assigned 并绑定买家 email，同时将卖家收益标记为 settled（可提现）
6. WHEN API Key 分配成功，THE HubCenter SHALL 通过邮件将分配的 API Key 发送给买家，邮件包含 Key 值、对应的 env_name 和使用说明
7. WHEN API Key 池中 available 数量低于总量的 20%（或低于 5 个，取较大者），THE HubCenter SHALL 使用指数回退邮件通知机制（Requirement 39）通知卖家补充 API Key，停止条件为 available 数量恢复到阈值以上
8. WHEN 买家成功购买一个声明了 `required_env` 的 Skill 且 API Key 池已耗尽（available 数量为 0），THE HubCenter SHALL 仍允许购买并正常扣除买家 Credits，但该订单标记为 `pending_key`（待发 Key），卖家收益标记为 `pending_settlement`（应收款，不可提现）
9. WHEN 卖家补充新的 API Key 到池中，THE HubCenter SHALL 自动检查是否存在 `pending_key` 状态的订单，IF 存在 THEN 按购买时间先后顺序自动分配 Key → 标记订单为 `key_delivered` → 将对应卖家收益从 `pending_settlement` 转为 `settled` → 通过邮件通知买家 Key 已发送
10. THE HubCenter SHALL 在卖家的 Credits 余额中区分 `settled`（可提现）和 `pending_settlement`（应收款，不可提现）两种状态，提现时仅允许提现 settled 部分
11. THE HubCenter Web 前端 SHALL 在卖家的 "我的 Skill" 管理页面展示待发 Key 订单列表（买家 email、购买时间、等待时长），提醒卖家及时补货
12. THE HubCenter SHALL 在 Skill 详情 API 和 Web 页面中展示 API Key 库存状态（充足/紧张/缺货），供买家购买前参考
13. WHEN MaClaw_Client 首次执行一个需要 API Key 的 Skill 且本地未配置对应 env，THE MaClaw_Client SHALL 通过 IM 询问用户提供 API Key（用户从购买邮件中获取）
14. WHEN 用户通过 IM 回复 API Key，THE MaClaw_Client SHALL 将该 Key 存入本地记忆/配置，后续执行同一 Skill 时直接使用
15. THE HubCenter Web 前端 SHALL 在 API Key 管理页面展示分配记录（哪个 Key 分配给了哪个买家 email、分配时间、状态）
16. WHEN 买家退款时，THE HubCenter SHALL 将对应的 API Key 分配记录标记为 refunded，将卖家对应收益从 settled 扣回，并通过邮件通知卖家建议作废该 Key（包含 Key 值和买家 email）
17. THE HubCenter SHALL 不负责实际作废 API Key，作废操作由卖家在外部服务中自行完成
18. 同一买家 SHALL 可以多次购买同一 Skill 获取多个 API Key（如为团队多台机器使用）


### Requirement 39: 指数回退邮件通知

**User Story:** As a SkillMarket 运营者, I want 重要通知使用指数回退策略发送, so that 在不骚扰用户的前提下确保关键信息被注意到。

#### Acceptance Criteria

1. THE HubCenter SHALL 提供通用的指数回退邮件通知机制，适用于所有需要持续提醒的场景（如 API Key 补货、pending_key 订单积压等）
2. WHEN 触发条件首次满足，THE HubCenter SHALL 立即发送第 1 封通知邮件
3. THE 后续通知 SHALL 按指数递增间隔发送：第 2 封间隔 1 小时，第 3 封间隔 2 小时，第 4 封间隔 4 小时，以此类推（间隔 = 2^(n-2) 小时，n 为第 n 封邮件）
4. THE HubCenter SHALL 对每个通知序列最多发送 10 封邮件，达到上限后停止发送
5. WHEN 停止条件满足（如库存恢复到阈值以上、pending_key 订单清零），THE HubCenter SHALL 立即终止该通知序列，不再发送后续邮件
6. THE HubCenter SHALL 为每个通知序列维护状态记录：notification_type、target_email、trigger_context（如 skill_id）、sent_count、next_send_at、is_active
7. WHEN 停止条件满足后又重新触发（如库存再次低于阈值），THE HubCenter SHALL 开启新的通知序列，sent_count 从 0 重新计数
8. THE HubCenter SHALL 通过后台定时任务（与试用期到期扫描复用同一调度器）检查并发送到期的通知邮件


### Requirement 40: 退款机制

**User Story:** As a SkillMarket 管理员, I want 通过管理后台处理退款请求, so that 在买家遇到问题时可以退还 Credits 并妥善处理关联的 API Key。

#### Acceptance Criteria

1. THE HubCenter SHALL 仅支持管理员通过管理后台发起退款操作，买家和卖家不能自助退款
2. WHEN 管理员对一笔购买记录执行退款，THE HubCenter SHALL 将购买金额（买家实际支付的 Credits）退还到买家余额
3. WHEN 退款涉及平台手续费，THE HubCenter SHALL 从平台收入中扣回对应的 30% 手续费
4. WHEN 退款涉及上传者收益，THE HubCenter SHALL 从上传者余额中扣回对应的 70% 收益（如果上传者余额不足，标记为负债，后续收入自动抵扣）
5. WHEN 退款涉及已分配的 API Key（Requirement 38），THE HubCenter SHALL 将对应的 API_Key_Assignment 标记为 refunded，并通过邮件通知卖家建议作废该 Key
6. WHEN 退款涉及 pending_key 状态的订单，THE HubCenter SHALL 取消该订单的 pending_key 状态，将卖家对应的 pending_settlement 收益扣回，不再为该订单分配 Key
7. THE HubCenter SHALL 在退款后将对应的 Purchase_Record 标记为 refunded，该用户后续下载同一 Skill 时按全价收费（不享受升级折扣）
8. THE HubCenter SHALL 在 Credits 交易记录中记录退款交易（type = 'refund'），包含原始购买记录 ID，确保退款可追溯
9. THE HubCenter Web 管理后台 SHALL 提供退款操作界面，展示购买详情（买家、Skill、金额、API Key 分配状态），管理员确认后执行退款
10. THE HubCenter SHALL 在退款完成后通过邮件通知买家退款结果（退还金额、退款原因）
