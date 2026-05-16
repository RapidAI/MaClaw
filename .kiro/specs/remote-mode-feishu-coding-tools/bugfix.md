# Bugfix Requirements Document

## Introduction

当用户通过飞书机器人向 MaClaw Agent 发送编程指令时，Agent 调用 `create_session` 工具创建编程会话（claude、codex、opencode、gemini 等），所有请求均被 `RemoteEnabled` 配置检查拦截，返回 "remote mode is disabled" 错误。

消息流程为：飞书 bot → Hub → WebSocket → 桌面客户端 `IMMessageHandler.toolCreateSession()` → `StartRemoteSessionForProject()`。该函数在 `remote_mobile_launch.go` 中检查 `cfg.RemoteEnabled`，若为 false 则直接拒绝。同样的检查也存在于 `remote_status.go` 中的 `StartRemoteSession()` 和 `StartRemoteHandoffSession()`。

问题的核心在于：消息能通过 Hub WebSocket 到达桌面端，本身就证明远程通道是通的。对来自 IM 渠道的请求强制要求 `RemoteEnabled = true` 是不合理的。而 `write_file`、`bash` 等本地工具不经过此检查，所以能正常工作。

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN Agent 通过飞书/IM 渠道调用 `create_session` 工具且桌面端 `RemoteEnabled` 为 false THEN `StartRemoteSessionForProject()` 返回 "remote mode is disabled" 错误，会话创建失败

1.2 WHEN Agent 通过飞书/IM 渠道调用 `create_session` 工具且桌面端 `RemoteEnabled` 为 false THEN `StartRemoteSession()` 返回 "remote mode is disabled" 错误，会话创建失败

1.3 WHEN Agent 通过飞书/IM 渠道调用 `create_session` 工具且桌面端 `RemoteEnabled` 为 false THEN `StartRemoteHandoffSession()` 返回 "remote mode is disabled" 错误，会话创建失败

1.4 WHEN 所有编程工具（claude、codex、opencode、gemini）均被 RemoteEnabled 检查拦截 THEN Agent 只能退回使用 write_file + bash 原始方式完成编程任务，用户体验严重降级

### Expected Behavior (Correct)

2.1 WHEN Agent 通过飞书/IM 渠道调用 `create_session` 工具 THEN `StartRemoteSessionForProject()` SHALL 跳过 `RemoteEnabled` 检查，正常创建编程工具会话

2.2 WHEN Agent 通过飞书/IM 渠道调用 `create_session` 工具 THEN `StartRemoteSession()` SHALL 跳过 `RemoteEnabled` 检查，正常创建编程工具会话

2.3 WHEN Agent 通过飞书/IM 渠道调用 `create_session` 工具 THEN `StartRemoteHandoffSession()` SHALL 跳过 `RemoteEnabled` 检查，正常创建编程工具会话

2.4 WHEN Agent 通过飞书/IM 渠道调用编程工具（claude、codex、opencode、gemini） THEN 系统 SHALL 正常启动对应工具会话，Agent 能使用专业编程工具完成任务

### Unchanged Behavior (Regression Prevention)

3.1 WHEN 用户在桌面端 UI 手动启动远程会话且 `RemoteEnabled` 为 false THEN 系统 SHALL CONTINUE TO 返回 "remote mode is disabled" 错误，阻止会话创建

3.2 WHEN 用户在桌面端 UI 手动启动远程会话且 `RemoteEnabled` 为 true THEN 系统 SHALL CONTINUE TO 正常创建远程会话

3.3 WHEN Agent 通过飞书/IM 渠道使用本地工具（write_file、bash 等） THEN 系统 SHALL CONTINUE TO 正常执行，不受 RemoteEnabled 配置影响

3.4 WHEN 移动端通过 Hub 发起远程会话且 `RemoteEnabled` 为 true THEN 系统 SHALL CONTINUE TO 正常创建远程会话
