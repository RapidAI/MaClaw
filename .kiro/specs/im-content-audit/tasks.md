# Implementation Plan: IM 内容审核

## Overview

为 MaClaw Hub 的 IM 出站通道添加内容审核能力。在现有 `OutboundInterceptor` 之后插入 `ContentAuditor` 模块，通过 stdin/stdout JSON 协议调用外部审核程序，根据返回码决定消息放行、拦截、延迟投递或脱敏。同时内置基于关键字匹配的默认审核程序，并在管理后台新增配置界面。

实现语言：Go

## Tasks

- [x] 1. 定义核心类型与接口
  - [x] 1.1 在 `hub/internal/config/config.go` 中添加 `ContentAudit` 配置段
    - 添加 `ContentAudit` struct 到 `Config`，包含 `ProgramPath`、`TimeoutSeconds`、`TimeoutPolicy` 字段
    - 在 `Default()` 中设置默认值：`TimeoutSeconds=30`、`TimeoutPolicy="block"`
    - _Requirements: 1.1, 1.2_
  - [x] 1.2 在 `hub/internal/im/content_auditor.go` 中定义审核相关类型
    - 定义 `AuditAction` 枚举常量（Pass, Block, Delay, Sanitize, ManualReview, Error）
    - 定义 `AuditRequest`、`AuditResponse`、`AuditResult`、`AuditLogEntry`、`ContentAuditDynamicConfig` 结构体
    - 定义 `AuditLogStore` 接口（`WriteLog` 方法）
    - _Requirements: 2.1, 2.2, 3.1-3.8, 5.1_
  - [ ]* 1.3 编写属性测试：审核协议 JSON 往返一致性
    - **Property 5: Audit protocol round-trip**
    - **Validates: Requirements 2.1, 2.2**

- [x] 2. 实现 ContentAuditor 核心逻辑
  - [x] 2.1 实现 `ContentAuditor` 结构体和 `NewContentAuditor` 构造函数
    - 包含 `programPath`、`timeoutSec`、`timeoutPolicy`、`semaphore`（容量 10）、`logStore`、`configProvider` 字段
    - _Requirements: 1.1, 7.4_
  - [x] 2.2 实现 `auditContent` 方法：调用外部审核程序
    - 通过 `exec.CommandContext` 启动外部进程，将 `AuditRequest` JSON 写入 stdin
    - 从 stdout 读取 `AuditResponse` JSON，处理超时（终止进程）、非法 JSON、进程启动失败等错误
    - 使用 semaphore 限制并发进程数
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 7.1, 7.2, 7.3, 7.4_
  - [x] 2.3 实现 `Audit` 方法：返回码到 AuditAction 映射及结果处理
    - 空 `programPath` 直接返回 AuditPass
    - 根据返回码映射：0→Pass, 1→Delay, 2→Block, 3→Block, 4→ManualReview, 5→Sanitize, -1/其他→Error
    - Error 时根据 `timeoutPolicy` 决定放行或拦截
    - 脱敏时用 `sanitized_content` 替换 `resp.Body`
    - 写入审核日志（失败不影响决策）
    - _Requirements: 1.3, 3.1-3.8, 5.1, 5.2, 5.4, 7.1_
  - [ ]* 2.4 编写属性测试：空程序路径直接放行
    - **Property 1: Empty program path passthrough**
    - **Validates: Requirements 1.3, 6.4**
  - [ ]* 2.5 编写属性测试：返回码到 AuditAction 映射
    - **Property 2: Return code to action mapping**
    - **Validates: Requirements 3.1-3.8**
  - [ ]* 2.6 编写属性测试：错误策略降级
    - **Property 3: Error policy fallback**
    - **Validates: Requirements 3.5, 7.1**
  - [ ]* 2.7 编写属性测试：脱敏内容替换
    - **Property 4: Sanitized content replacement**
    - **Validates: Requirements 3.7**
  - [ ]* 2.8 编写属性测试：审核日志完整性
    - **Property 6: Audit log completeness**
    - **Validates: Requirements 5.1, 5.2**
  - [ ]* 2.9 编写属性测试：并发审核 semaphore 限流
    - **Property 9: Concurrent audit semaphore**
    - **Validates: Requirements 7.4**

- [x] 3. 实现延迟审核流程
  - [x] 3.1 在 `ContentAuditor` 中实现延迟审核轮询逻辑
    - 返回码 1 时发送占位消息，启动后台 goroutine 轮询（默认 5 秒间隔，最多 10 次）
    - 轮询结果为 0 时投递原始内容，为 2/3 时发送拦截消息，超过 10 次发送超时消息
    - 需要接收一个 delivery callback 用于异步投递
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5_
  - [ ]* 3.2 编写属性测试：延迟审核最终投递正确结果
    - **Property 14: Delay resolution delivers correct outcome**
    - **Validates: Requirements 4.3, 4.4**

- [x] 4. Checkpoint - 核心审核逻辑验证
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. 实现审核日志持久化
  - [x] 5.1 在 `hub/internal/store/sqlite/migrations.go` 中添加 `audit_logs` 表迁移
    - 创建 `audit_logs` 表及索引（timestamp, user_id, return_code）
    - _Requirements: 5.3_
  - [x] 5.2 在 `hub/internal/im/audit_log_store.go` 中实现 SQLite 版 `AuditLogStore`
    - 实现 `SQLiteAuditLogStore` 结构体，接收 `*sql.DB`
    - 实现 `WriteLog` 方法，将 `AuditLogEntry` 写入 `audit_logs` 表
    - _Requirements: 5.1, 5.3_
  - [ ]* 5.3 编写属性测试：审核日志持久化往返一致性
    - **Property 7: Audit log persistence round-trip**
    - **Validates: Requirements 5.3**

- [x] 6. 集成 ContentAuditor 到 IM Adapter
  - [x] 6.1 在 `hub/internal/im/core.go` 的 `Adapter` 中添加 `contentAuditor` 字段和 setter
    - 添加 `contentAuditor *ContentAuditor` 字段
    - 添加 `SetContentAuditor(ca *ContentAuditor)` 方法
    - _Requirements: 6.3_
  - [x] 6.2 修改 `sendResponse` 方法，在 OutboundInterceptor 之后插入 ContentAuditor 调用
    - OutboundInterceptor 拦截时跳过 ContentAuditor
    - ContentAuditor 返回 Block/ManualReview 时替换 resp
    - ContentAuditor 返回 Sanitize 时替换 resp
    - ContentAuditor 返回 Delay 时发送占位消息并启动后台轮询
    - `programPath` 为空时无任何影响
    - _Requirements: 6.1, 6.2, 6.3, 6.4_
  - [ ]* 6.3 编写属性测试：OutboundInterceptor 拦截时跳过审核
    - **Property 8: Outbound interceptor short-circuits audit**
    - **Validates: Requirements 6.2**

- [x] 7. 在 bootstrap 中装配 ContentAuditor
  - [x] 7.1 修改 `hub/internal/app/bootstrap.go`
    - 从 `cfg.ContentAudit` 读取配置
    - 创建 `SQLiteAuditLogStore`
    - 创建 `ContentAuditor`，configProvider 从 SystemSettings 读取 `content_audit_config`
    - 调用 `imAdapter.SetContentAuditor()`
    - _Requirements: 1.1, 6.3_

- [x] 8. Checkpoint - 集成验证
  - Ensure all tests pass, ask the user if questions arise.

- [x] 9. 实现默认审核程序
  - [x] 9.1 创建 `hub/cmd/audit_program/main.go`
    - 从 stdin 读取 `AuditRequest` JSON
    - type=text 时进行关键字匹配（优先使用 JSON 中的 keywords，其次 `--keywords-file`）
    - type=image/file 时直接返回 code=0
    - 命中关键字返回 code=2，message 包含命中的关键字
    - 将 `AuditResponse` JSON 写入 stdout
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7_
  - [ ]* 9.2 编写属性测试：默认审核程序类型路由
    - **Property 10: Default audit program type-based routing**
    - **Validates: Requirements 8.3, 8.4, 8.5**
  - [ ]* 9.3 编写属性测试：关键字命中消息
    - **Property 11: Default audit program keyword hit message**
    - **Validates: Requirements 8.7**

- [x] 10. 实现管理后台 API
  - [x] 10.1 创建 `hub/internal/httpapi/content_audit_handler.go`
    - 实现 `GetContentAuditConfigHandler`：从 SystemSettings 读取 `content_audit_config`
    - 实现 `UpdateContentAuditConfigHandler`：写入 SystemSettings
    - _Requirements: 9.3, 9.4, 9.5_
  - [x] 10.2 在 `hub/internal/httpapi/router.go` 中注册路由
    - `GET /api/admin/content_audit/config` → RequireAdmin + GetContentAuditConfigHandler
    - `PUT /api/admin/content_audit/config` → RequireAdmin + UpdateContentAuditConfigHandler
    - _Requirements: 9.5_
  - [ ]* 10.3 编写属性测试：关键字从配置透传到 stdin
    - **Property 12: Keywords passthrough from config to stdin**
    - **Validates: Requirements 8.8**
  - [ ]* 10.4 编写属性测试：管理配置持久化往返一致性
    - **Property 13: Admin config persistence round-trip**
    - **Validates: Requirements 9.3, 9.4, 9.6**

- [x] 11. 实现管理后台 Web UI
  - [x] 11.1 在 `hub/web/admin/index.html` 的 IM tab 中添加"内容审核"子 tab
    - 添加子 tab 按钮和对应的 pane 容器
    - 包含：审核程序路径输入框、超时时间输入框、超时策略下拉选择（block/pass）、关键字列表多行文本框
    - 实现加载/保存逻辑，调用 `/api/admin/content_audit/config` API
    - 关键字在 UI 中以多行文本展示，保存时转换为 JSON 数组
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.6_

- [x] 12. Final checkpoint - 全部验证
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- 实现语言为 Go，属性测试使用 `github.com/leanovate/gopter` 库
- 默认审核程序作为独立可执行文件编译在 `hub/cmd/audit_program/`
