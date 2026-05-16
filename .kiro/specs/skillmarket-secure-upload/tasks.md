# Implementation Plan: SkillMarket 安全上传

## Overview

基于需求文档和设计文档，将 SkillMarket 安全上传功能拆解为增量式编码任务。实现顺序为：数据模型 → 核心服务 → API 层 → 客户端适配 → Web 前端 → 集成联调。所有代码使用 Go 语言，Web 前端使用 HTML/JS。

## Tasks

- [x] 1. 数据模型与存储层
  - [x] 1.1 创建 SkillMarket 用户数据模型和存储接口
    - 新建 `hubcenter/internal/skillmarket/` 目录
    - 创建 `types.go`，定义 `SkillMarketUser`、`CreditsTransaction`、`SkillSubmission` 结构体
    - 创建 `user_repository.go`，定义 `UserRepository` 接口（EnsureAccount、GetByEmail、UpdateStatus 等）
    - _Requirements: 6.1, 6.2, 7.2, 7.3_

  - [x] 1.2 创建 Credits 数据模型和存储接口
    - 在 `types.go` 中定义 `CreditsTransaction` 结构体
    - 创建 `credits_repository.go`，定义 `CreditsRepository` 接口（GetBalance、AddTransaction、ListTransactions）
    - _Requirements: 8.1, 8.2, 8.3_

  - [x] 1.3 实现 SQLite 存储层
    - 创建 `hubcenter/internal/skillmarket/sqlite/` 目录
    - 实现 `user_repo_sqlite.go`：SQLite 版 UserRepository，包含建表迁移
    - 实现 `credits_repo_sqlite.go`：SQLite 版 CreditsRepository，包含建表迁移
    - 实现 `submission_repo_sqlite.go`：SQLite 版 SubmissionRepository，包含建表迁移
    - _Requirements: 6.1, 8.1, 1.2_

  - [x]* 1.4 编写数据模型单元测试
    - 测试 UserRepository CRUD 操作
    - 测试 CreditsRepository 余额计算和交易记录
    - 测试 SubmissionRepository 状态流转
    - _Requirements: 6.1, 8.1, 1.2_

- [x] 2. skill.yaml 解析与验证模块
  - [x] 2.1 实现 SkillMetadata 解析器
    - 创建 `hubcenter/internal/skillmarket/metadata.go`
    - 实现 `ParseSkillYAML(data []byte) (*SkillMetadata, error)` 函数
    - 实现 `FormatSkillYAML(meta *SkillMetadata) ([]byte, error)` 函数
    - 支持 Extra 字段保留未识别字段（round-trip 安全）
    - _Requirements: 13.1, 13.2, 13.3, 13.4_

  - [x]* 2.2 编写 SkillMetadata round-trip 属性测试
    - **Property 1: Round-trip consistency** — 对任意有效 SkillMetadata，ParseSkillYAML(FormatSkillYAML(m)) 应产生等价对象
    - **Validates: Requirements 13.3**

  - [x] 2.3 实现 Skill 包验证器
    - 创建 `hubcenter/internal/skillmarket/validator.go`
    - 实现 `ValidatePackage(sandboxDir string) (*ValidationResult, error)`
    - 实现 `ValidateYAML(path string) []ValidationError`
    - 实现 `ValidatePython(path string) []ValidationError`（调用 py_compile）
    - 实现 `ValidateShell(path string) []ValidationError`（调用 bash -n）
    - _Requirements: 4.1, 4.2, 4.3, 4.4_

  - [x]* 2.4 编写验证器单元测试
    - 测试有效/无效 YAML 文件的验证结果
    - 测试有效/无效 Python 文件的验证结果
    - 测试有效/无效 Shell 脚本的验证结果
    - _Requirements: 4.1, 4.2, 4.3, 4.4_

- [x] 3. Checkpoint - 确保数据模型和解析模块测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. 核心服务层
  - [x] 4.1 实现 UserService（延迟验证账户）
    - 创建 `hubcenter/internal/skillmarket/user_service.go`
    - 实现 `EnsureAccount`：email 不存在则创建 unverified 账户
    - 实现 `VerifyAccount`：升级为 verified，支持接管已有 unverified 账户
    - 实现 `GetAccount`：获取账户信息
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 7.1, 7.2, 7.3_

  - [x] 4.2 实现 CreditsService（Credits 计费）
    - 创建 `hubcenter/internal/skillmarket/credits_service.go`
    - 实现 `GetBalance`、`Debit`（购买扣款）、`Credit`（收益入账）
    - 实现 `TopUp`（充值，仅 verified）、`Withdraw`（提现，仅 verified）
    - 余额不足时返回明确错误
    - `Debit` 使用 `BEGIN IMMEDIATE` 事务确保余额检查和扣款的原子性，防止并发扣款导致余额变负
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 30.1_

  - [x]* 4.3 编写 CreditsService 属性测试
    - **Property 2: Credits 守恒** — 对任意交易序列，所有 Debit 和 Credit 操作后余额应等于初始余额加上所有 Credit 减去所有 Debit
    - **Validates: Requirements 8.1, 8.2, 8.3**

  - [x] 4.4 实现 Submission Processor（异步处理）
    - 创建 `hubcenter/internal/skillmarket/processor.go`
    - 实现 `Enqueue(submissionID string)` 将提交加入 channel 队列
    - 实现 `Run(ctx context.Context)` 后台 goroutine 消费队列
    - 实现 `processOne`：解压到 Sandbox（使用 SafeUnzip）→ 解析 skill.yaml → 语法验证 → 发布或标记失败 → 发送邮件通知 → defer 清理 Sandbox
    - 实现 `SafeUnzip`：安全解压，检查解压比率（≤20x）、总大小（≤500MB）、单文件大小（≤50MB）、文件数量（≤1000），超限中止并标记失败
    - _Requirements: 1.2, 1.3, 1.4, 2.3, 2.4, 3.1, 3.2, 3.3, 3.4, 24.1, 24.2, 24.3, 24.4_

  - [x]* 4.5 编写 Processor 单元测试
    - 测试有效 zip 包的完整处理流程（pending → success）
    - 测试无效 zip 包的失败处理（pending → failed）
    - 测试缺少 skill.yaml 的拒绝逻辑
    - 测试 zip 炸弹防护（解压比率超限、总大小超限、文件数量超限）
    - 测试 Sandbox 清理（成功和失败后临时目录均被删除）
    - _Requirements: 1.2, 1.3, 2.3, 2.4, 4.4, 24.1, 24.2, 24.3, 24.4_

- [x] 5. 下载加密模块
  - [x] 5.1 实现 Crypto Module
    - 创建 `hubcenter/internal/skillmarket/crypto.go`
    - 实现 `EnsureRSAKeyPair(dataDir string)`：首次启动时检查密钥文件是否存在，不存在则生成 RSA-2048 密钥对并写入 `data/rsa_private.pem` 和 `data/rsa_public.pem`，已存在则直接加载，不覆盖
    - 实现 `EncryptForDownload(zipData []byte, userID string, rsaPrivKey *rsa.PrivateKey) (*EncryptedPackage, error)`
    - 密钥派生：`PBKDF2(salt, user_id, 100000, 32, SHA256)`
    - 对称加密：AES-256-GCM
    - Salt 保护：RSA-OAEP 加密
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [x] 5.2 实现客户端解密函数
    - 在 `gui/skillhub_client.go` 或新建 `gui/skill_crypto.go` 中实现
    - 实现 `DecryptDownload(pkg *EncryptedPackage, userID string, rsaPubKey *rsa.PublicKey) ([]byte, error)`
    - 实现公钥获取逻辑：本地无缓存时调用 `GET /api/v1/crypto/pubkey` 获取并缓存到 `~/.maclaw/skillmarket_pubkey.pem`
    - _Requirements: 5.1, 5.2, 5.6, 5.7, 5.8_

  - [x]* 5.3 编写加密 round-trip 属性测试
    - **Property 3: 加密解密 round-trip** — 对任意 zipData 和 userID，DecryptDownload(EncryptForDownload(zipData, userID, privKey), userID, pubKey) == zipData
    - **Validates: Requirements 5.3, 5.4, 5.5, 5.6**

  - [x]* 5.4 编写加密安全性测试
    - 测试不同 userID 无法解密同一加密包
    - 测试错误公钥无法解密 salt
    - _Requirements: 5.3, 5.6_

- [x] 6. Checkpoint - 确保核心服务和加密模块测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. HubCenter HTTP API 层
  - [x] 7.1 实现上传提交 API
    - 在 `hubcenter/internal/httpapi/` 中创建 `skillmarket_handlers.go`
    - 实现 `POST /api/v1/skills/submit`：接收 multipart/form-data（zip + email），验证 zip 基本有效性，保存到 pending/，返回 submission_id
    - 实现 `GET /api/v1/skills/submissions/{id}`：查询提交状态
    - _Requirements: 1.1, 1.2, 2.1, 2.2, 2.3, 2.4_

  - [x] 7.2 实现加密下载 API
    - 实现 `GET /api/v1/skills/{id}/download`：检查 Skill 状态（trial 状态跳过收费）→ 检查 Credits 余额（付费 Skill）→ 加密 → 返回 EncryptedPackage
    - 付费 Skill 自动扣款并给上传者入账
    - 下载成功后原子递增 Skill 的 download_count
    - trial 状态 Skill 免费下载，不扣 Credits
    - _Requirements: 5.2, 5.3, 5.4, 5.5, 8.2, 8.3, 8.4, 26.1, 26.2, 27.1, 27.2_

  - [x] 7.3 实现账户管理 API
    - 实现 `POST /api/v1/account/ensure`：延迟创建账户
    - 实现 `GET /api/v1/account/{email}`：获取账户信息
    - 实现 `POST /api/v1/account/verify`：验证账户
    - _Requirements: 6.1, 6.2, 7.1, 7.2, 7.3_

  - [x] 7.4 实现 Credits API
    - 实现 `GET /api/v1/credits/balance`：查询余额
    - 实现 `GET /api/v1/credits/transactions`：查询交易记录
    - 实现 `POST /api/v1/credits/topup`：充值（仅 verified）
    - 实现 `POST /api/v1/credits/withdraw`：提现（仅 verified）
    - _Requirements: 8.1, 8.5, 11.1, 11.2, 11.3_

  - [x] 7.5 实现公钥分发 API
    - 实现 `GET /api/v1/crypto/pubkey`：返回 RSA 公钥
    - _Requirements: 5.1_

  - [x] 7.6 注册所有新路由
    - 在 HubCenter 的路由注册处（参考现有 skill_handlers.go 的注册方式）添加所有新端点
    - _Requirements: 12.1, 12.3_

  - [x]* 7.7 编写 API 集成测试
    - 测试上传提交完整流程（submit → 查询状态）
    - 测试账户创建和验证流程
    - 测试 Credits 扣款和余额不足拒绝
    - _Requirements: 1.1, 6.1, 8.4_

- [x] 8. Checkpoint - 确保 API 层测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 9. MaClaw_Client 适配
  - [x] 9.1 扩展 SkillHubClient 上传功能
    - 在 `gui/skillhub_client.go` 中实现 `SubmitSkill` 方法（multipart/form-data 上传 zip）
    - 实现 `GetSubmissionStatus` 方法
    - 替代原有 `Publish` 方法的调用路径
    - _Requirements: 1.1, 2.1_

  - [x] 9.2 实现加密下载流程
    - 实现 `DownloadEncrypted` 方法：调用加密下载 API → 解密 → 保存加密包到磁盘 → 首次使用时解密到临时目录
    - 集成公钥获取和缓存
    - _Requirements: 5.6, 5.7_

  - [x] 9.3 实现账户状态展示
    - 实现 `GetAccountInfo` 方法
    - 在设置页面集成账户状态展示（email、Credits 余额、验证状态）
    - _Requirements: 10.1, 10.2_

  - [x] 9.4 实现 Credits 不足通知
    - 在搜索结果中检测付费 Skill 且余额不足的情况
    - 通过 IM 或邮件通知用户，包含 Skill 名称、所需 Credits 和当前余额
    - _Requirements: 9.1, 9.2_

- [x] 10. HubCenter Web 前端
  - [x] 10.1 创建账户管理页面
    - 在 `hubcenter/web/skillmarket/` 下创建账户页面
    - 实现 Credits 余额展示、交易记录列表
    - 实现身份验证表单（email/手机号验证）
    - _Requirements: 7.1, 11.1, 11.4_

  - [x] 10.2 创建充值/提现页面
    - 实现充值操作界面（仅 verified 用户可用）
    - 实现提现操作界面（仅 verified 用户可用）
    - _Requirements: 11.2, 11.3_

- [x] 11. 集成与联调
  - [x] 11.1 HubSkillMeta 扩展
    - 在 `hubcenter/internal/skill/types.go` 中为 `HubSkillMeta` 添加 `Price`、`UploaderID`、`DownloadCount`、`PreWithdrawnStatus` 字段
    - 实现 `SkillStore.IncrementDownloadCount` 原子递增方法
    - 更新 `SkillStore.Publish` 以支持新字段
    - 更新客户端 `gui/skillhub_client.go` 中对应的类型定义
    - _Requirements: 8.2, 8.3, 26.1, 26.2, 29.2_

  - [x] 11.2 端到端流程联调
    - 确保上传 → 异步处理 → 邮件通知完整流程可用
    - 确保下载 → 加密 → 解密 → 安装完整流程可用
    - 确保账户创建 → 验证 → 充值 → 购买完整流程可用
    - _Requirements: 1.1, 1.4, 5.6, 6.1, 7.2, 8.3_

  - [x]* 11.3 编写端到端集成测试
    - 测试完整上传-处理-下载流程
    - 测试未验证用户权限限制
    - 测试 Credits 购买流程（余额充足/不足）
    - _Requirements: 1.1, 6.3, 6.4, 8.4_

- [x] 12. Final checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 13. 评分与试用期数据模型
  - [x] 13.1 创建评分数据模型和存储接口
    - 在 `hubcenter/internal/skillmarket/types.go` 中定义 `Rating`、`RatingStats` 结构体
    - 创建 `rating_repository.go`，定义 `RatingRepository` 接口（Upsert、GetBySkill、GetStats）
    - 创建 `skill_ratings` 表，主键为 (skill_id, email) 实现去重
    - _Requirements: 17.1, 17.2, 17.3, 17.4_

  - [x] 13.2 创建管理员配置数据模型和存储接口
    - 创建 `admin_config` 表，存储 trial_duration 和 auto_publish_threshold
    - 创建 `config_repository.go`，定义 `ConfigRepository` 接口（Get、Set）
    - _Requirements: 14.4, 14.7_

  - [x] 13.3 扩展 HubSkillMeta 状态字段
    - 在 `HubSkillMeta` 中添加 `Status`（支持 pending/trial/published/pending_review/rejected/withdrawn/superseded）和 `TrialExpireAt` 字段
    - 更新 Skill Store 的查询和更新方法以支持新状态
    - Skill 状态变更使用乐观锁：`UPDATE ... WHERE id = ? AND status = ?`，affected rows 为 0 则返回并发冲突错误
    - _Requirements: 14.1, 14.2, 30.3_

  - [x] 13.4 实现 SQLite 评分存储层
    - 实现 `rating_repo_sqlite.go`：SQLite 版 RatingRepository，包含建表迁移
    - 实现 UPSERT 语义（INSERT ON CONFLICT UPDATE）实现 email 去重覆盖
    - _Requirements: 17.1, 17.2_

  - [x]* 13.5 编写评分数据模型单元测试
    - 测试 Rating UPSERT 去重逻辑（同 email 覆盖旧评分）
    - 测试 RatingStats 计算（含 0 分的平均分和评价人数）
    - 测试 admin_config CRUD 操作
    - _Requirements: 17.1, 17.2, 17.3, 17.4_

- [x] 14. 评分与试用期核心服务
  - [x] 14.1 实现 Rating Service
    - 创建 `hubcenter/internal/skillmarket/rating_service.go`
    - 实现 `SubmitRating`：验证 score 范围 (-2~+2)，检查上传者自评（email == uploader_email 则拒绝），UPSERT 评分，-2 分触发紧急下架
    - 实现 `GetStats`：返回去重后评价人数和平均分（含 0 分）
    - 实现 `GetRatings`：返回 Skill 的所有去重后评分列表
    - _Requirements: 16.6, 17.1, 17.2, 17.3, 17.4, 18.1, 28.1, 28.2, 30.2_

  - [x] 14.2 实现 Trial Manager
    - 创建 `hubcenter/internal/skillmarket/trial_manager.go`
    - 实现 `OnSkillValidated`：将 Skill 状态设为 trial，设置到期时间（当前时间 + trial_duration）
    - 实现 `CheckAutoPublish`：检查 ≥ threshold 评价 & 平均分 ≥ 0
    - 实现 `ProcessExpiredTrials`：定期扫描到期 trial Skill，转为 pending_review
    - 实现 `AdminApprove` 和 `AdminReject`：管理员审核操作
    - _Requirements: 14.1, 14.4, 14.5, 14.6, 14.7, 15.2, 15.3_

  - [x] 14.3 集成 Trial Manager 到 Submission Processor
    - 修改 `processor.go` 的 `processOne` 方法：语法验证通过后调用 `TrialManager.OnSkillValidated` 而非直接 Publish
    - _Requirements: 14.1_

  - [x] 14.4 实现试用期到期定时任务
    - 在 HubCenter 启动时启动后台 goroutine，定期调用 `ProcessExpiredTrials`
    - _Requirements: 14.6_

  - [x]* 14.5 编写 Rating Service 属性测试
    - **Property 4: 评分去重一致性** — 对同一 (skill_id, email) 多次提交评分后，GetStats 返回的 unique_raters 应等于去重后的 email 数量
    - **Property 5: 平均分计算正确性** — GetStats 返回的 average_score 应等于所有去重后评分的算术平均值
    - **Validates: Requirements 17.1, 17.3, 17.4**

  - [x]* 14.6 编写 Trial Manager 单元测试
    - 测试自动上架条件判定（达标/未达标）
    - 测试试用期到期处理
    - 测试 -2 评分紧急下架
    - 测试管理员批准/拒绝流程
    - _Requirements: 14.5, 14.6, 15.2, 15.3, 18.1_

- [x] 15. Checkpoint - 确保评分和试用期核心服务测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 16. 评分与试用期 API 层
  - [x] 16.1 实现评分提交 API
    - 在 `hubcenter/internal/httpapi/skillmarket_handlers.go` 中添加
    - 实现 `POST /api/v1/skills/{id}/rate`：接收 email + score，调用 RatingService.SubmitRating
    - 实现 `GET /api/v1/skills/{id}/ratings`：返回评分统计
    - _Requirements: 16.6, 17.1_

  - [x] 16.2 实现管理员审核 API
    - 实现 `GET /api/v1/admin/skills/pending-review`：查询待审核 Skill 列表
    - 实现 `POST /api/v1/admin/skills/{id}/approve`：管理员批准
    - 实现 `POST /api/v1/admin/skills/{id}/reject`：管理员拒绝
    - _Requirements: 15.1, 15.2, 15.3_

  - [x] 16.3 实现管理员配置 API
    - 实现 `PUT /api/v1/admin/config/trial`：更新 trial_duration 和 auto_publish_threshold
    - _Requirements: 14.4, 14.7_

  - [x] 16.4 注册新路由
    - 在路由注册处添加评分、管理员审核和配置相关端点
    - _Requirements: 12.1, 12.3_

  - [x]* 16.5 编写评分 API 集成测试
    - 测试评分提交和去重流程
    - 测试 -2 评分触发紧急下架
    - 测试管理员审核流程
    - _Requirements: 16.6, 17.1, 18.1, 15.2, 15.3_

- [x] 17. MaClaw_Client 自动评分适配
  - [x] 17.1 实现 MaClaw Evaluator
    - 创建 `gui/skill_evaluator.go`
    - 实现 `Evaluate(result *SkillExecutionResult) int`：根据执行结果生成评分
    - 安全告警 → -2, 错误 → -1, 无效果 → 0, 成功 → +1, 超预期 → +2
    - _Requirements: 16.1, 16.2, 16.3, 16.4, 16.5_

  - [x] 17.2 实现评分提交客户端方法
    - 在 `gui/skillhub_client.go` 中实现 `SubmitRating` 方法
    - 在 Skill 执行完成后自动调用 Evaluate → SubmitRating
    - _Requirements: 16.6_

  - [x]* 17.3 编写 Evaluator 单元测试
    - 测试各种执行结果到评分的映射
    - _Requirements: 16.1, 16.2, 16.3, 16.4, 16.5_

- [x] 18. 管理后台前端扩展
  - [x] 18.1 扩展管理后台审核页面
    - 在 `hubcenter/web/skillmarket/` 下添加待审核 Skill 列表页面
    - 展示 Skill 详情、试用期评分数据、评分分布
    - 提供批准/拒绝操作按钮
    - _Requirements: 15.1, 15.2, 15.3, 15.4_

  - [x] 18.2 添加试用期配置管理页面
    - 在管理后台添加 trial_duration 和 auto_publish_threshold 配置界面
    - _Requirements: 14.4, 14.7_

  - [x] 18.3 扩展 Skill 列表展示
    - 在 Skill 列表页面为 trial 状态的 Skill 添加 "试用中" 标识
    - 展示评分统计信息（评价人数、平均分）
    - _Requirements: 14.2, 14.3_

- [x] 19. 评分与试用期集成联调
  - [x] 19.1 端到端流程联调
    - 确保上传 → 语法验证 → 进入 trial → 评分 → 自动上架完整流程可用
    - 确保 -2 评分 → 紧急下架 → 管理员审核完整流程可用
    - 确保试用期到期 → pending_review → 管理员审核完整流程可用
    - _Requirements: 14.1, 14.5, 14.6, 16.6, 18.1, 18.2, 18.3_

  - [x]* 19.2 编写端到端集成测试
    - 测试完整试用期生命周期（trial → published）
    - 测试恶意评分紧急下架流程
    - 测试试用期到期未达标流程
    - 测试 0 分评价计入平均分和评价人数
    - _Requirements: 14.5, 14.6, 17.3, 18.1_

- [x] 20. Final checkpoint - 确保所有测试通过（含新增评分和试用期功能）
  - Ensure all tests pass, ask the user if questions arise.

- [x] 21. 自动上传触发与版本管理
  - [x] 21.1 扩展 HubSkillMeta 版本字段
    - 在 `HubSkillMeta` 中添加 `Version`、`Fingerprint`（uploader_email + skill_name）、`SupersededBy` 字段
    - 在 `skill_submissions` 表中添加 `fingerprint` 字段
    - 添加 `superseded` 到 Skill 状态枚举
    - _Requirements: 20.1, 20.2, 20.4_

  - [x] 21.2 实现 Version Manager
    - 创建 `hubcenter/internal/skillmarket/version_manager.go`
    - 实现 `ResolveSubmission`：根据 (uploader_email, skill_name) 查询是否已存在，返回 isUpgrade 和 nextVersion
    - 实现 `SupersedeOldVersion`：新版本 published 后将旧版本标记为 superseded
    - 实现 `GetVersionHistory`：返回某个 Fingerprint 的所有版本
    - _Requirements: 20.1, 20.2, 20.3, 20.4, 20.6, 20.7_

  - [x] 21.3 集成 Version Manager 到 Submission Processor
    - 修改 `processor.go` 的 `processOne`：语法验证通过后先调用 `VersionManager.ResolveSubmission` 判断新建/升级
    - 新建 → 创建 Skill version=1 → 进入 trial
    - 升级 → 创建新版本 version++ → 进入 trial，旧版本保持可用
    - _Requirements: 20.2, 20.3, 20.5_

  - [x] 21.4 集成 Version Manager 到 Trial Manager
    - 修改 Trial Manager：新版本 published 时调用 `SupersedeOldVersion` 替换旧版本
    - _Requirements: 20.5, 20.6_

  - [x] 21.5 实现 MaClaw Auto Upload Trigger
    - 创建 `gui/auto_upload_trigger.go`
    - 实现 `SkillUsageTracker`：跟踪每个本地 Skill 的执行次数和评分历史
    - 实现 `ShouldUpload`：判断执行次数 ≥ 3 且最近评分平均 ≥ +1 且本地版本有变更
    - 实现 `CheckAndTrigger`：Skill 执行完成后自动检查并触发上传
    - _Requirements: 19.1, 19.2, 19.3, 19.4_

  - [x]* 21.6 编写版本管理属性测试
    - **Property 6: 版本单调递增** — 对同一 Fingerprint 的连续提交，version 应严格递增
    - **Property 7: 去重一致性** — 对同一 (uploader_email, skill_name)，任意时刻最多只有一个非 superseded 的 published 版本
    - **Validates: Requirements 20.1, 20.3, 20.4, 20.6**

  - [x]* 21.7 编写 Auto Upload Trigger 单元测试
    - 测试满足条件时触发上传
    - 测试不满足条件时跳过（执行次数不足、评分不够、无变更）
    - _Requirements: 19.1, 19.2, 19.3, 19.4_

- [x] 22. 版本管理 API 与前端
  - [x] 22.1 实现版本历史 API
    - 实现 `GET /api/v1/skills/{id}/versions`：返回版本历史列表
    - _Requirements: 20.7_

  - [x] 22.2 扩展 Skill 详情页展示版本信息
    - 在 Skill 详情页展示当前版本号和版本历史
    - _Requirements: 20.7_

- [x] 23. Checkpoint - 确保自动上传和版本管理测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 24. 信誉等级体系
  - [x] 24.1 创建 Uploader Tier 数据模型和存储接口
    - 在 `hubcenter/internal/skillmarket/types.go` 中定义 `UploaderTier` 结构体
    - 创建 `tier_repository.go`，定义 `TierRepository` 接口（Get、Upsert、RecalculateStats）
    - 创建 `uploader_tiers` 表，包含 user_id、tier、published_count、avg_rating、total_downloads
    - 在 `admin_config` 表中添加等级阈值和限制的默认配置项
    - _Requirements: 23.2, 23.3, 23.6_

  - [x] 24.2 实现 TierService
    - 创建 `hubcenter/internal/skillmarket/tier_service.go`
    - 实现 `RecalculateTier`：根据 published_count、avg_rating、total_downloads 重新计算等级（可升可降）
    - 实现 `GetTier`：获取当前等级
    - 实现 `GetLimits`：根据等级返回对应的上传大小限制和频率限制
    - 默认等级限制：Tier 1 = 10MB/5h/20d, Tier 2 = 20MB/10h/40d, Tier 3 = 50MB/20h/80d, Tier 4 = 100MB/50h/200d
    - _Requirements: 23.2, 23.3, 23.4, 23.5, 23.6_

  - [x] 24.3 集成 TierService 到状态变更触发点
    - 在 Trial Manager 的 `AdminApprove`、`AdminReject`、`CheckAutoPublish` 中调用 `RecalculateTier`
    - 在 Version Manager 的 `SupersedeOldVersion` 中调用 `RecalculateTier`
    - 在 Rating Service 的 `SubmitRating` 中调用 `RecalculateTier`
    - _Requirements: 23.5_

  - [x]* 24.4 编写 TierService 属性测试
    - **Property 8: 等级单调性** — 对于 published_count、avg_rating、total_downloads 均递增的输入序列，计算出的 tier 应单调不减
    - **Property 9: 等级降级正确性** — 当指标下降到低于当前等级阈值时，tier 应相应降低
    - **Validates: Requirements 23.2, 23.5**

- [x] 25. 上传频率限制与大小限制
  - [x] 25.1 实现 RateLimiter
    - 创建 `hubcenter/internal/skillmarket/rate_limiter.go`
    - 实现 `CheckRateLimit(ctx, email) error`：查询最近 1h/24h 有效提交数（排除 failed），对比 Tier 限制
    - 超限时返回包含 retry_after 时间的错误
    - _Requirements: 22.1, 22.2, 22.3, 22.4, 22.5_

  - [x] 25.2 集成频率限制和大小限制到上传 API
    - 修改 `POST /api/v1/skills/submit` handler：在 zip 验证之前先调用 `RateLimiter.CheckRateLimit`
    - 在 zip 验证之前检查 zip 大小 vs `TierService.GetLimits` 返回的 MaxUploadSize
    - 频率超限返回 429 + retry_after，大小超限返回 413 + 当前等级和允许最大大小
    - _Requirements: 22.1, 22.3, 23.1, 23.7_

  - [x]* 25.3 编写频率限制单元测试
    - 测试未超限时允许提交
    - 测试超限时拒绝并返回正确的 retry_after
    - 测试 failed 提交不计入频率统计
    - 测试不同 Tier 的频率限制差异
    - _Requirements: 22.1, 22.2, 22.3, 22.4, 22.5_

- [x] 26. 上传者主动下架
  - [x] 26.1 扩展 Skill 状态枚举
    - 在 HubSkillMeta 的状态枚举中添加 `withdrawn`
    - 更新 Skill Store 查询：列表和搜索排除 withdrawn 状态
    - _Requirements: 21.3, 21.4_

  - [x] 26.2 实现下架 API
    - 在 `hubcenter/internal/httpapi/skillmarket_handlers.go` 中添加 `POST /api/v1/skills/{id}/withdraw`
    - 权限检查：请求 email 必须匹配 Skill 的 uploader_email
    - 状态限制：仅 trial 或 published 状态可下架
    - 下架后调用 `TierService.RecalculateTier`
    - _Requirements: 21.1, 21.2, 21.3, 21.5_

  - [x] 26.3 实现 "我的 Skill" 管理页面
    - 在 `hubcenter/web/skillmarket/` 下创建我的 Skill 管理页面
    - 展示上传者名下所有 Skill 及状态
    - 提供下架操作按钮（仅 trial/published 状态可用）
    - _Requirements: 21.1, 21.2_

  - [x] 26.4 实现信誉等级查询 API 和页面展示
    - 实现 `GET /api/v1/account/{email}/tier`：返回当前等级和各项指标
    - 在账户管理页面展示信誉等级信息
    - _Requirements: 23.2_

  - [x] 26.5 注册新路由
    - 注册下架、等级查询、管理员等级配置相关路由
    - _Requirements: 12.1, 12.3_

  - [x]* 26.6 编写下架功能测试
    - 测试上传者下架自己的 Skill（trial → withdrawn, published → withdrawn）
    - 测试非上传者无法下架他人 Skill
    - 测试 withdrawn Skill 不出现在列表中
    - 测试下架后 Tier 重新计算
    - _Requirements: 21.2, 21.3, 21.4, 21.5_

- [x] 27. 被拒绝 Skill 重新提交
  - [x] 27.1 扩展 VersionManager 支持 rejected 状态重新提交
    - 修改 `VersionManager.ResolveSubmission`：当同 Fingerprint 最新版本为 rejected 时，仍作为版本升级处理
    - 新版本进入 trial，旧 rejected 版本保持不变
    - 新版本试用期评价从零开始
    - _Requirements: 24.1, 24.2, 24.3_

  - [x]* 27.2 编写重新提交测试
    - 测试 rejected Skill 重新提交后版本递增
    - 测试新版本进入 trial 且评价独立
    - _Requirements: 24.1, 24.2, 24.3_

- [x] 28. 管理员等级配置
  - [x] 28.1 实现管理员等级配置 API
    - 实现 `PUT /api/v1/admin/config/tiers`：更新等级阈值和限制参数
    - 实现 `GET /api/v1/admin/config/tiers`：查询当前等级配置
    - _Requirements: 23.6_

  - [x] 28.2 扩展管理后台配置页面
    - 在管理后台添加信誉等级阈值和限制的配置界面
    - 包含各等级的晋升条件（published_count、avg_rating、downloads）和限制值（max_size、rate_limit）
    - _Requirements: 23.6_

- [x] 29. 新功能集成联调
  - [x] 29.1 端到端流程联调
    - 确保上传频率限制 → 大小限制 → 正常上传完整流程可用
    - 确保上传者下架 → Tier 重新计算完整流程可用
    - 确保 rejected Skill 重新提交 → 版本升级 → 重新进入 trial 完整流程可用
    - 确保 Tier 升级/降级在各触发场景下正确计算
    - _Requirements: 21.1-21.5, 22.1-22.5, 23.1-23.7, 24.1-24.3_

  - [x]* 29.2 编写新功能端到端集成测试
    - 测试频率限制完整流程
    - 测试大小限制与 Tier 联动
    - 测试下架与 Tier 降级联动
    - 测试 rejected 重新提交完整流程
    - _Requirements: 21.1-21.5, 22.1-22.5, 23.1-23.7, 24.1-24.3_

- [x] 30. Final checkpoint - 确保所有测试通过（含信誉等级、频率限制、下架、重新提交）
  - Ensure all tests pass, ask the user if questions arise.

- [x] 31. 智能搜索功能（Requirement 32）
  - [x] 31.1 创建 FTS5 全文索引
    - 在 HubCenter SQLite 迁移中创建 `skill_fts` FTS5 虚拟表（索引 name、description、tags）
    - 创建 INSERT/DELETE/UPDATE 触发器保持 FTS 索引与 `hub_skill_meta` 主表同步
    - 为已有数据执行一次性全量索引重建
    - _Requirements: 32.3_

  - [x] 31.2 实现 SearchService
    - 创建 `hubcenter/internal/skillmarket/search_service.go`
    - 实现 `Search(ctx, query, tags, topN) ([]SearchResult, error)`
    - 实现 FTS5 MATCH 查询 + 质量排序公式：`score = fts_rank * -0.5 + avg_rating * 0.2 + log(downloads+1) * 0.2 + recency * 0.1`
    - 支持 tags 过滤（AND 逻辑）
    - 仅返回 status IN ('trial', 'published') 的 Skill
    - _Requirements: 32.3, 32.4, 32.5, 32.8_

  - [x] 31.3 实现搜索 API handler
    - 在 `hubcenter/internal/httpapi/skillmarket_handlers.go` 中添加 `GET /api/v1/skills/search` handler
    - 参数：q（关键词）、tags（逗号分隔）、top_n（默认 20）
    - 返回 SearchResult 数组，包含 name、description、tags、score、price、status、avg_rating、download_count
    - 注册路由
    - _Requirements: 32.2, 32.5_

  - [x] 31.4 实现 MaClaw 端智能搜索模块
    - 创建 `gui/skill_searcher.go`
    - 实现 `SkillSearcher.SearchAndInstall`：本地 LLM 提炼关键词 → 调用搜索 API → 本地 LLM 精选 → 自动下载安装
    - 搜索无结果时记录日志并继续，不中断任务
    - _Requirements: 32.1, 32.2, 32.6, 32.7, 32.9_

  - [x]* 31.5 编写搜索功能测试
    - 测试 FTS5 索引创建和同步触发器
    - 测试搜索排序公式正确性
    - 测试空结果处理
    - 测试 tags 过滤逻辑
    - _Requirements: 32.3, 32.4, 32.5, 32.9_

- [x] 32. 自动 Tag 生成功能（Requirement 33）
  - [x] 32.1 实现 TagGenerator
    - 创建 `gui/tag_generator.go`
    - 实现 `TagGenerator.GenerateTags(ctx, skillDir) (*GeneratedMetadata, error)`
    - 读取 skill.yaml 和关联脚本文件 → 本地 LLM 分析 → 生成 name、description、tags、triggers
    - Tags 分功能类和领域类
    - 保留已有非空字段，仅补全缺失字段
    - _Requirements: 33.1, 33.2, 33.3, 33.5, 33.6_

  - [x] 32.2 集成 TagGenerator 到上传流程
    - 修改 `Auto_Upload_Trigger.CheckAndTrigger`：上传前调用 `TagGenerator.GenerateTags`
    - 将生成的元数据写回 skill.yaml
    - Tag 生成失败时降级为使用原有元数据继续上传
    - _Requirements: 33.4, 33.5_

  - [x]* 32.3 编写 TagGenerator 单元测试
    - 测试已有字段保留逻辑
    - 测试缺失字段补全逻辑
    - 测试 Tag 分类（功能类 + 领域类）
    - _Requirements: 33.3, 33.6_

- [x] 33. Checkpoint - 确保搜索和 Tag 生成功能测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 34. 排行榜功能（Requirement 34）
  - [x] 34.1 实现 LeaderboardService
    - 创建 `hubcenter/internal/skillmarket/leaderboard_service.go`
    - 实现 `GetTop(ctx, sortBy, limit) ([]LeaderboardEntry, error)`
    - 支持 sort=rating|downloads|newest 三种排序
    - 仅包含 published 状态的 Skill
    - limit 范围 1~50，默认 10
    - _Requirements: 34.1, 34.2, 34.3, 34.4, 34.5_

  - [x] 34.2 实现排行榜 API handler
    - 在 `hubcenter/internal/httpapi/skillmarket_handlers.go` 中添加 `GET /api/v1/skills/top` handler
    - 参数：sort（默认 rating）、limit（默认 10）
    - 注册路由
    - _Requirements: 34.1, 34.2, 34.3_

  - [x] 34.3 实现 MaClaw 端排行榜浏览
    - 在 `gui/skillhub_client.go` 中添加 `BrowseLeaderboard` 方法
    - 支持 MaClaw 主动调用浏览热门 Skill
    - _Requirements: 34.6_

  - [x]* 34.4 编写排行榜功能测试
    - 测试三种排序方式的正确性
    - 测试 limit 参数边界（0、1、50、51）
    - 测试仅返回 published 状态 Skill
    - _Requirements: 34.1, 34.2, 34.3, 34.4_

- [x] 35. 经济系统启动（Requirement 35）
  - [x] 35.1 扩展数据模型支持新经济系统
    - 在 `skillmarket_users` 表中新增 `voucher_remaining`（INTEGER DEFAULT 3）、`voucher_expire_at`（DATETIME）、`settled`（INTEGER DEFAULT 0）、`pending_settlement`（INTEGER DEFAULT 0）、`debt`（INTEGER DEFAULT 0）字段
    - 创建 `purchase_records` 表（id、buyer_email、skill_id、purchased_version、price_paid、purchase_type ['purchase'|'upgrade'|'voucher']、status ['active'|'refunded']、created_at）
    - 扩展 `credits_transactions` 表的 type 字段支持新类型：purchase、earning、topup、withdraw、upgrade、refund、platform_fee
    - _Requirements: 35.6, 35.7, 35.8, 35.15, 35.16_

  - [x] 35.2 实现平台抽成 30/70 分成逻辑
    - 修改下载 handler 的扣费逻辑：买家扣全价，上传者入账 70%（标记为 settled 或 pending_settlement），平台记录 30% 手续费
    - 新增 `RecordPlatformFee` 方法记录平台收入（type='platform_fee'）
    - _Requirements: 35.1, 35.15_

  - [x] 35.3 实现买断制与版本升级 50% 折扣
    - 实现 `GetPurchaseRecord(buyerEmail, skillID)` 查询购买历史
    - 修改下载 handler：同一用户下载同一 Skill 新版本时，按 price * 50% 收费（type='upgrade'）
    - 创建 Purchase_Record 记录每次成功购买
    - _Requirements: 35.4, 35.5, 35.6_

  - [x] 35.4 实现 Free_Trial_Voucher（免费体验券）
    - 修改 `UserService.EnsureAccount`：新用户创建时设置 voucher_remaining=3、voucher_expire_at=now+7天
    - 实现 `UseVoucher(ctx, userID, skillID) error`：检查剩余次数 > 0 且未过期且 Skill 未声明 required_env，扣减 1 次，不扣 Credits，不给上传者入账
    - 修改下载 handler：付费 Skill 下载前检查体验券可用性，可用则走体验券逻辑
    - 体验券下载不创建 Purchase_Record（后续购买按全价）
    - _Requirements: 35.7, 35.8, 35.9, 35.10, 35.11, 35.12_

  - [x] 35.5 实现 settled vs pending_settlement 余额区分
    - 修改 `CreditsService.Credit`：根据是否需要 API Key 交付，分别计入 settled 或 pending_settlement
    - 修改 `CreditsService.Withdraw`：仅允许提现 settled 部分，不含 pending_settlement
    - 实现 `SettlePending(ctx, userID, amount)`：将 pending_settlement 转为 settled（API Key 交付后调用）
    - _Requirements: 35.16_

  - [x] 35.6 集成经济系统到现有下载流程
    - 修改 `GET /api/v1/skills/{id}/download` handler：集成平台抽成、体验券、版本升级折扣、Purchase_Record 创建
    - 确保试用期 Skill 免费下载逻辑不变（与 Requirement 27 一致）
    - 确保平台零支出原则：无 bonus Credits、无首次发布奖励、无下载里程碑奖励、无每日免费下载
    - _Requirements: 35.1, 35.3, 35.4, 35.13_

  - [x]* 35.7 编写经济系统属性测试
    - **Property 10: 平台抽成守恒** — 对任意付费下载，buyer_debit == uploader_credit + platform_fee，且 platform_fee == floor(price * 0.3)
    - **Validates: Requirements 35.1, 35.15**

  - [x]* 35.8 编写经济系统单元测试
    - 测试平台抽成计算（30% 平台 / 70% 上传者）
    - 测试版本升级 50% 折扣
    - 测试体验券发放、使用、过期、用完
    - 测试体验券不适用于 required_env Skill
    - 测试体验券下载不创建 Purchase_Record
    - 测试 settled vs pending_settlement 提现限制
    - 测试零支出原则（无 bonus、无奖励、无免费下载）
    - _Requirements: 35.1-35.16_

- [x] 36. 经济系统集成联调
  - [x] 36.1 端到端流程联调
    - 确保新用户注册 → 获得 3 次体验券 → 使用体验券下载付费 Skill（不扣 Credits、不给上传者入账）完整流程可用
    - 确保付费下载 → 平台抽成 30% → 上传者入账 70% → settled 完整流程可用
    - 确保版本升级 → 50% 折扣 → Purchase_Record 更新完整流程可用
    - 确保 settled 提现 → pending_settlement 不可提现完整流程可用
    - _Requirements: 35.1-35.16_

  - [x]* 36.2 编写经济系统端到端集成测试
    - 测试完整付费下载流程（含抽成和体验券）
    - 测试新用户注册到首次购买完整流程
    - 测试上传者从上传到获得收益到提现完整流程
    - _Requirements: 35.1-35.16_

- [x] 37. Checkpoint - 确保经济系统所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 38. 自动定价功能（Requirement 36）
  - [x] 38.1 实现 pricing_mode 配置项
    - 在 MaClaw 系统设置中添加 `pricing_mode` 配置项（auto|free|fixed），默认 auto
    - 添加 `fixed_price` 配置项（当 pricing_mode=fixed 时使用）
    - _Requirements: 36.1, 36.5, 36.6, 36.7_

  - [x] 38.2 扩展 TagGenerator 支持自动定价
    - 修改 `gui/tag_generator.go` 的 `GenerateTags` 方法：在 LLM 分析时同时生成 price 建议
    - 定价参考区间：极简 Skill → 0，普通 Skill → 5~15 Credits，复杂 Skill → 20~50 Credits
    - 复用 Tag 生成的 LLM 调用，不额外增加 LLM 请求
    - 如果 skill.yaml 已有非零 price 且 pricing_mode=auto，保留原有 price
    - _Requirements: 36.2, 36.3, 36.4, 36.8_

  - [x] 38.3 集成定价到上传流程
    - 修改 `Auto_Upload_Trigger.CheckAndTrigger`：根据 pricing_mode 决定定价策略
    - auto → 使用 TagGenerator 生成的 price
    - free → 强制 price=0
    - fixed → 使用 fixed_price 配置值
    - _Requirements: 36.1, 36.5, 36.6_

  - [x]* 38.4 编写自动定价单元测试
    - 测试三种 pricing_mode 的行为
    - 测试已有 price 保留逻辑
    - 测试定价区间合理性
    - _Requirements: 36.1-36.8_

- [x] 39. 安全扫描模块（Requirement 37）
  - [x] 39.1 实现静态安全扫描器
    - 创建 `hubcenter/internal/skillmarket/security_scanner.go`
    - 实现 `ScanPackage(sandboxDir string) (*SecurityReport, error)`
    - 实现硬编码密钥/Token 检测（正则匹配 API_KEY、SECRET、TOKEN、PASSWORD 等模式）
    - 实现危险操作检测（rm -rf、format、DROP TABLE 等）
    - 实现外部网络调用检测（curl、wget、requests、http.Get 等）
    - 实现 Shell 命令执行检测（os.system、subprocess、exec 等）
    - _Requirements: 37.1, 37.2_

  - [x] 39.2 实现 Security_Label 生成和存储
    - 定义 Security_Label 枚举：network_access、file_system_access、shell_exec、hardcoded_secrets、database_access
    - 实现 `GenerateLabels(report *SecurityReport) []string`：根据扫描结果生成标签列表
    - 如果检测到 hardcoded_secrets，标记 Submission 为失败并通知上传者
    - _Requirements: 37.3, 37.10_

  - [x] 39.3 扩展 HubSkillMeta 支持安全字段
    - 在 `HubSkillMeta` 中添加 `SecurityLabels []string`、`Permissions []string`、`RequiredEnv []string` 字段
    - 从 skill.yaml 解析 `permissions` 和 `required_env` 字段
    - 在 Skill 列表和详情 API 中返回 security_labels
    - _Requirements: 37.4, 37.5, 37.6_

  - [x] 39.4 集成安全扫描到异步处理流程
    - 修改 `processor.go` 的 `processOne`：语法验证通过后执行安全扫描
    - hardcoded_secrets → 标记失败并通知
    - 其他标签 → 记录到 HubSkillMeta.SecurityLabels
    - _Requirements: 37.1, 37.10_

  - [x] 39.5 实现 MaClaw 端安全策略
    - 在 MaClaw 系统设置中添加安全策略配置：每种权限类型支持 allow/deny/ask 三种模式
    - 实现 `SecurityPolicyChecker`：执行 Skill 前检查 Security_Label vs 安全策略
    - deny → 拒绝执行并记录日志
    - ask → 通过 IM 询问用户，用户拒绝则不执行
    - _Requirements: 37.7, 37.8, 37.9_

  - [x]* 39.6 编写安全扫描测试
    - 测试各类风险项检测（硬编码密钥、危险操作、网络调用、Shell 执行）
    - 测试 Security_Label 生成正确性
    - 测试 hardcoded_secrets 触发失败
    - 测试 MaClaw 安全策略 allow/deny/ask 行为
    - _Requirements: 37.1-37.10_

- [x] 40. API Key 池分发（Requirement 38）
  - [x] 40.1 创建 API Key 数据模型
    - 创建 `api_keys` 表（id、skill_id、env_name、encrypted_key、status ['available'|'assigned'|'refunded']、buyer_email、assigned_at、created_at）
    - 创建 `pending_key_orders` 表（id、purchase_record_id、skill_id、buyer_email、status ['pending_key'|'key_delivered'|'cancelled']、created_at）
    - _Requirements: 38.4, 38.8_

  - [x] 40.2 实现 API Key 加密存储
    - 实现 `EncryptAPIKey(key string) (string, error)`：使用 AES-256-GCM 加密（复用 HubCenter RSA 密钥派生的对称密钥）
    - 实现 `DecryptAPIKey(encrypted string) (string, error)`：解密
    - 实现批量上传 API Key 接口：解析每行一个 Key，加密后存储
    - _Requirements: 38.3_

  - [x] 40.3 实现 APIKeyPoolService
    - 创建 `hubcenter/internal/skillmarket/apikey_service.go`
    - 实现 `AssignKey(ctx, skillID, buyerEmail, envName) (*APIKey, error)`：从 available 池中分配一个 Key，标记为 assigned
    - 实现 `CreatePendingOrder(ctx, purchaseRecordID, skillID, buyerEmail) error`：池耗尽时创建 pending_key 订单
    - 实现 `FulfillPendingOrders(ctx, skillID) (int, error)`：补货后按购买时间顺序自动分配，pending → key_delivered，pending_settlement → settled
    - 实现 `GetStockStatus(ctx, skillID) string`：返回 "充足"/"紧张"/"缺货"
    - _Requirements: 38.5, 38.8, 38.9, 38.12_

  - [x] 40.4 集成 API Key 分配到购买流程
    - 修改下载 handler：付费下载声明了 required_env 的 Skill 时，尝试分配 API Key
    - 有 available Key → 分配 + settled + 邮件通知买家
    - 无 available Key → 创建 pending_key 订单 + pending_settlement + 邮件通知买家等待
    - _Requirements: 38.5, 38.6, 38.8_

  - [x] 40.5 实现补货自动分配
    - 在卖家上传新 API Key 时自动调用 `FulfillPendingOrders`
    - 分配成功后：pending_key → key_delivered，pending_settlement → settled，邮件通知买家
    - _Requirements: 38.9_

  - [x] 40.6 实现库存状态计算和低库存通知
    - 实现库存状态：available >= 总量 20% 且 >= 5 → "充足"，否则 → "紧张"，available == 0 → "缺货"
    - 低库存时触发指数回退邮件通知（Task 41）通知卖家补货
    - 在 Skill 详情 API 中返回 api_key_stock_status
    - _Requirements: 38.7, 38.12, 25.6_

  - [x] 40.7 实现卖家 API Key 管理页面
    - 在 `hubcenter/web/skillmarket/` 下为声明了 required_env 的 Skill 添加 "API Key 管理" 页面
    - 支持批量上传 API Key（每行一个，关联 env_name）
    - 展示分配记录（Key → 买家 email、分配时间、状态）
    - 展示待发 Key 订单列表（买家 email、购买时间、等待时长）
    - _Requirements: 38.1, 38.2, 38.11, 38.15_

  - [x] 40.8 实现 MaClaw 端 API Key 配置
    - 修改 MaClaw：首次执行需要 API Key 的 Skill 且本地未配置时，通过 IM 询问用户提供 Key
    - 用户回复后存入本地记忆/配置，后续执行直接使用
    - _Requirements: 38.13, 38.14_

  - [x] 40.9 实现 API Key 相关 API 端点
    - `POST /api/v1/skills/{id}/apikeys/upload`：卖家批量上传 API Key
    - `GET /api/v1/skills/{id}/apikeys/status`：查询库存状态和分配记录
    - `GET /api/v1/skills/{id}/apikeys/pending`：查询待发 Key 订单
    - 注册路由
    - _Requirements: 38.1, 38.2, 38.12, 38.15_

  - [x]* 40.10 编写 API Key 池属性测试
    - **Property 11: API Key 分配唯一性** — 对任意 Key，最多只能被分配给一个买家（assigned 状态唯一）
    - **Property 12: pending 订单 FIFO** — pending_key 订单按购买时间先后顺序分配
    - **Validates: Requirements 38.5, 38.9**

  - [x]* 40.11 编写 API Key 池单元测试
    - 测试正常分配流程（available → assigned）
    - 测试池耗尽时创建 pending_key 订单
    - 测试补货后自动分配 pending 订单
    - 测试库存状态计算（充足/紧张/缺货）
    - 测试同一买家多次购买获取多个 Key
    - _Requirements: 38.4-38.12, 38.18_

- [x] 41. 指数回退邮件通知（Requirement 39）
  - [x] 41.1 创建通知序列数据模型
    - 创建 `notification_sequences` 表（id、notification_type、target_email、trigger_context、sent_count、next_send_at、is_active、created_at、updated_at）
    - _Requirements: 39.6_

  - [x] 41.2 实现 NotificationService
    - 创建 `hubcenter/internal/skillmarket/notification_service.go`
    - 实现 `StartSequence(ctx, notificationType, targetEmail, triggerContext, emailContent) error`：创建新序列并立即发送第 1 封
    - 实现 `StopSequence(ctx, notificationType, triggerContext) error`：停止条件满足时终止序列
    - 实现 `ProcessPendingNotifications(ctx) error`：扫描到期通知并发送，更新 next_send_at
    - 实现间隔计算：第 n 封间隔 = 2^(n-2) 小时（n >= 2），最多 10 封
    - _Requirements: 39.1, 39.2, 39.3, 39.4, 39.5_

  - [x] 41.3 实现停止条件检查
    - API Key 补货通知：available 数量恢复到阈值以上时停止
    - pending_key 积压通知：pending_key 订单清零时停止
    - 停止后重新触发时开启新序列（sent_count 从 0 开始）
    - _Requirements: 39.5, 39.7_

  - [x] 41.4 集成到后台调度器
    - 在 HubCenter 后台定时任务中（与试用期到期扫描复用同一调度器）添加 `ProcessPendingNotifications` 调用
    - _Requirements: 39.8_

  - [x]* 41.5 编写通知服务属性测试
    - **Property 13: 间隔指数递增** — 对任意通知序列，第 n 封的间隔应为 2^(n-2) 小时（n >= 2）
    - **Property 14: 最多 10 封** — 对任意通知序列，sent_count 不超过 10
    - **Validates: Requirements 39.3, 39.4**

  - [x]* 41.6 编写通知服务单元测试
    - 测试首次触发立即发送
    - 测试间隔计算正确性（1h→2h→4h→8h→...）
    - 测试 10 封上限
    - 测试停止条件终止序列
    - 测试重新触发开启新序列
    - _Requirements: 39.1-39.8_

- [x] 42. 退款机制（Requirement 40）
  - [x] 42.1 实现 RefundService
    - 创建 `hubcenter/internal/skillmarket/refund_service.go`
    - 实现 `ProcessRefund(ctx, purchaseRecordID, adminEmail, reason string) error`
    - _Requirements: 40.1, 40.2_

  - [x] 42.2 实现退款核心逻辑
    - 退还买家 Credits（原始支付金额）
    - 扣回平台手续费（30%）
    - 扣回上传者收益（70%）：余额不足时标记为 debt，后续收入自动抵扣
    - 记录退款交易（type='refund'，包含原始 purchase_record_id）
    - 标记 Purchase_Record 为 refunded（后续购买按全价）
    - _Requirements: 40.2, 40.3, 40.4, 40.7, 40.8_

  - [x] 42.3 实现退款关联 API Key 处理
    - 已分配 Key（assigned）→ 标记为 refunded + 邮件通知卖家建议作废
    - pending_key 订单 → 取消订单 + 扣回 pending_settlement
    - _Requirements: 40.5, 40.6_

  - [x] 42.4 实现卖家负债自动抵扣
    - 修改 `CreditsService.Credit`：入账时检查 debt > 0，自动抵扣后剩余部分入账
    - _Requirements: 40.4_

  - [x] 42.5 实现管理后台退款界面
    - 在管理后台添加退款操作页面
    - 展示购买详情（买家、Skill、金额、API Key 分配状态）
    - 管理员确认后执行退款
    - 退款完成后邮件通知买家
    - _Requirements: 40.9, 40.10_

  - [x] 42.6 实现退款 API 端点
    - `POST /api/v1/admin/refund`：管理员发起退款（参数：purchase_record_id、reason）
    - `GET /api/v1/admin/purchases`：查询购买记录列表（支持按 buyer_email、skill_id 筛选）
    - 注册路由
    - _Requirements: 40.1, 40.9_

  - [x]* 42.7 编写退款属性测试
    - **Property 15: 退款守恒** — 对任意退款操作，buyer_refund == original_price，platform_debit == floor(original_price * 0.3)，uploader_debit == original_price - floor(original_price * 0.3)
    - **Validates: Requirements 40.2, 40.3, 40.4**

  - [x]* 42.8 编写退款单元测试
    - 测试正常退款三方 Credits 回退
    - 测试上传者余额不足时负债标记
    - 测试负债自动抵扣
    - 测试退款关联 assigned API Key → refunded
    - 测试退款关联 pending_key 订单 → 取消
    - 测试退款后 Purchase_Record 标记为 refunded
    - _Requirements: 40.1-40.10_

- [x] 43. Checkpoint - 确保自动定价、安全扫描、API Key 池、通知、退款所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 44. 新功能集成联调
  - [x] 44.1 端到端流程联调
    - 确保自动定价 → 上传 → 安全扫描 → Security_Label 生成完整流程可用
    - 确保付费下载（含 API Key 分配）→ 平台抽成 → settled 完整流程可用
    - 确保 API Key 池耗尽 → pending_key → 补货自动分配 → key_delivered 完整流程可用
    - 确保低库存 → 指数回退通知 → 补货后停止通知完整流程可用
    - 确保管理员退款 → 三方 Credits 回退 → API Key refunded → 负债处理完整流程可用
    - 确保 MaClaw 安全策略 deny/ask 行为正确
    - _Requirements: 35.1-35.16, 36.1-36.8, 37.1-37.10, 38.1-38.18, 39.1-39.8, 40.1-40.10_

  - [x]* 44.2 编写新功能端到端集成测试
    - 测试完整付费下载流程（含 API Key 分配和体验券）
    - 测试 API Key 池管理完整流程
    - 测试退款完整流程
    - 测试安全扫描和策略控制完整流程
    - _Requirements: 35.1-35.16, 36.1-36.8, 37.1-37.10, 38.1-38.18, 39.1-39.8, 40.1-40.10_

- [x] 45. Final checkpoint - 确保所有测试通过（含经济系统、自动定价、安全扫描、API Key 池、通知、退款）
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties (round-trip, Credits 守恒, 加密一致性, 评分去重一致性, 平均分计算正确性)
- 所有 HubCenter 侧代码在 `hubcenter/` 目录下，客户端代码在 `gui/` 目录下
- 加密模块参考现有 `hub/internal/httpapi/clawnet_key_handler.go` 的 AES-256-GCM 实现模式
- 架构约束：HubCenter 独立运行，不依赖 Hub 直连（Requirements 12.1, 12.2, 12.3）
- Skill 状态流转：pending → trial → published / pending_review → published / rejected
- 评分去重以 (skill_id, email) 为主键，UPSERT 覆盖旧评分
- 0 分评价计入评价人数和平均分计算
- 管理员可配置参数：trial_duration（默认 7 天）、auto_publish_threshold（默认 5 个不同 email）
- Tasks 13-20 为新增的评分与试用期功能任务，依赖 Tasks 1-12 的基础设施
- Tasks 21-23 为自动上传触发与版本管理任务，依赖 Tasks 1-12 和 13-20 的基础设施
- Skill 去重以 (uploader_email, skill_name) 为 Fingerprint，重复上传自动作为版本升级
- 版本升级期间旧 published 版本继续可用，新版本 published 后旧版本标记为 superseded
- 自动上传由 MaClaw 自主触发，条件：执行 ≥ 3 次、评分平均 ≥ +1、本地有变更
- Skill 状态流转新增 superseded 状态：published → superseded（被新版本替代时）
- Skill 状态流转新增 withdrawn 状态：trial/published → withdrawn（上传者主动下架）
- Tasks 24-30 为新增的信誉等级、频率限制、下架、重新提交功能任务，依赖 Tasks 1-23 的基础设施
- Uploader Tier 信誉等级可升可降，基于 published_count、avg_rating、total_downloads 三个维度
- 默认等级限制：Tier 1 = 10MB/5h/20d, Tier 2 = 20MB/10h/40d, Tier 3 = 50MB/20h/80d, Tier 4 = 100MB/50h/200d
- 频率限制仅计算 pending/processing/success 状态的提交，不计算 failed
- 被拒绝 Skill 重新提交作为版本升级处理，新版本试用期评价从零开始
- Tasks 31-34 为智能搜索、自动 Tag 生成、排行榜功能任务，依赖 Tasks 1-30 的基础设施
- 智能搜索：服务端零 LLM 依赖，使用 SQLite FTS5 粗筛 + MaClaw 端本地 LLM 精选
- 搜索排序公式：score = fts_rank * -0.5 + avg_rating * 0.2 + log(downloads+1) * 0.2 + recency * 0.1
- 自动 Tag 生成：MaClaw 本地 LLM 在上传前分析 Skill 内容，生成功能类和领域类 tags，写入 skill.yaml
- 排行榜支持 rating/downloads/newest 三种排序，仅包含 published 状态 Skill
- Tasks 35-37 为经济系统功能任务（平台零支出原则），依赖 Tasks 1-34 的基础设施
- 经济系统：平台抽成 30%/上传者 70%，买断制 + 版本升级 50% 折扣
- 新用户赠送 3 次免费体验券（Free_Trial_Voucher），7 天有效，不是 Credits，不产生提现负债
- 体验券不适用于声明了 required_env 的 Skill（无法触发 API Key 分配）
- 平台零支出原则：无 bonus Credits、无首次发布奖励、无下载里程碑奖励、无每日免费下载
- Credits 交易类型：purchase、earning、topup、withdraw、upgrade、refund、platform_fee
- 卖家余额区分 settled（可提现）和 pending_settlement（待交付，不可提现）
- purchase_records 表记录购买历史，用于版本升级折扣判定
- Tasks 38 为自动定价功能，pricing_mode 支持 auto/free/fixed 三种模式
- Tasks 39 为安全扫描模块，静态扫描生成 Security_Label，MaClaw 端安全策略控制
- Tasks 40 为 API Key 池分发管理，卖家上传 Key → 买家购买时自动分配 → 缺货时 pending_key
- Tasks 41 为指数回退邮件通知机制，间隔 = 2^(n-2) 小时，最多 10 封
- Tasks 42 为退款机制，管理员发起，三方 Credits 回退 + API Key refunded 标记 + 负债处理
