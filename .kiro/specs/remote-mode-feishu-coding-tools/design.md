# Remote Mode Feishu Coding Tools Bugfix Design

## Overview

飞书/IM 渠道发起的编程工具会话（claude、codex、opencode、gemini）被 `cfg.RemoteEnabled` 检查无条件拦截，返回 "remote mode is disabled"。修复方案是在三个会话创建函数中引入 `LaunchSource` 感知：当请求来源为 IM/Agent 渠道时跳过 `RemoteEnabled` 检查，桌面端 UI 调用继续执行原有检查。

## Glossary

- **Bug_Condition (C)**: 请求来源为 IM/Agent 渠道（非桌面端 UI），且 `cfg.RemoteEnabled` 为 false 时，会话创建被错误拦截
- **Property (P)**: IM/Agent 渠道的请求应跳过 `RemoteEnabled` 检查，正常创建编程工具会话
- **Preservation**: 桌面端 UI 发起的会话创建必须继续受 `RemoteEnabled` 配置约束
- **`StartRemoteSessionForProject()`**: `remote_mobile_launch.go` 中的函数，接收 `RemoteStartSessionRequest` 创建远程会话，当前在第 ~66 行无条件检查 `cfg.RemoteEnabled`
- **`StartRemoteSession()`**: `remote_status.go` 中的函数（~line 408），用于桌面端启动远程会话，同样无条件检查 `cfg.RemoteEnabled`
- **`StartRemoteHandoffSession()`**: `remote_status.go` 中的函数（~line 454），用于 handoff 场景启动远程会话，同样无条件检查 `cfg.RemoteEnabled`
- **`RemoteLaunchSource`**: `remote_types.go` 中定义的类型，包含 `desktop`、`mobile`、`handoff`、`ai` 四种来源常量
- **`toolCreateSession()`**: `im_message_handler.go` 中的 IM 工具处理函数（~line 2055），调用 `StartRemoteSessionForProject()` 创建会话

## Bug Details

### Bug Condition

当 IM/Agent 渠道（飞书 bot → Hub → WebSocket → 桌面客户端）发起编程工具会话创建请求时，三个会话创建函数均无条件检查 `cfg.RemoteEnabled`。消息能通过 Hub WebSocket 到达桌面端本身就证明远程通道是通的，对 IM 渠道强制要求 `RemoteEnabled = true` 是不合理的。

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type SessionCreationRequest
  OUTPUT: boolean

  RETURN input.launchSource IN ['mobile', 'ai', 'handoff']
         AND config.RemoteEnabled == false
         AND input.tool IN ['claude', 'codex', 'opencode', 'gemini']
END FUNCTION
```

### Examples

- 用户通过飞书发送 "用 claude 帮我重构 main.go"，Agent 调用 `create_session(tool="claude")`，`StartRemoteSessionForProject()` 检查 `cfg.RemoteEnabled == false`，返回 "remote mode is disabled"。预期：应跳过检查，正常创建 claude 会话。
- 用户通过飞书发送 "用 codex 修复这个 bug"，Agent 调用 `create_session(tool="codex")`，同样被拦截。预期：应正常创建 codex 会话。
- 用户通过飞书发送 "用 gemini 分析代码"，Agent 调用 `create_session(tool="gemini")`，同样被拦截。预期：应正常创建 gemini 会话。
- 用户在桌面端 UI 点击 "启动远程会话" 且 `RemoteEnabled == false`，应继续返回 "remote mode is disabled"（正确行为，不应改变）。

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- 桌面端 UI 手动启动远程会话时，`RemoteEnabled` 检查必须继续生效
- 桌面端 UI 在 `RemoteEnabled == true` 时正常创建远程会话的行为不变
- IM 渠道使用本地工具（write_file、bash 等）不受影响
- 移动端在 `RemoteEnabled == true` 时正常创建远程会话的行为不变
- `buildRemoteLaunchSpec()`、`remoteSessions.Create()` 等下游逻辑不变

**Scope:**
所有来源为 `RemoteLaunchSourceDesktop` 的请求应完全不受此修复影响，继续执行原有的 `RemoteEnabled` 检查逻辑。

## Hypothesized Root Cause

Based on the bug description, the most likely issues are:

1. **缺少来源感知的 RemoteEnabled 检查**: 三个函数 (`StartRemoteSessionForProject`、`StartRemoteSession`、`StartRemoteHandoffSession`) 均使用 `if !cfg.RemoteEnabled { return error }` 无条件拦截，没有区分请求来源。

2. **`RemoteStartSessionRequest` 缺少 `LaunchSource` 字段**: 当前 `RemoteStartSessionRequest` 结构体没有 `LaunchSource` 字段，`StartRemoteSessionForProject()` 硬编码 `spec.LaunchSource = RemoteLaunchSourceMobile`，无法让调用方传递真实来源。

3. **`toolCreateSession()` 未传递来源信息**: `im_message_handler.go` 中的 `toolCreateSession()` 构造 `RemoteStartSessionRequest` 时没有设置来源字段，导致函数内部无法判断请求是否来自 IM 渠道。

4. **`StartRemoteSession()` 和 `StartRemoteHandoffSession()` 无来源参数**: 这两个函数签名中没有 `LaunchSource` 参数，无法区分桌面端调用和 IM/Agent 调用。

## Correctness Properties

Property 1: Bug Condition - IM/Agent 渠道跳过 RemoteEnabled 检查

_For any_ session creation request where the launch source is non-desktop (`mobile`, `ai`, or `handoff`) and `cfg.RemoteEnabled` is false, the fixed session creation functions SHALL skip the `RemoteEnabled` check and proceed to create the session normally, returning a valid `RemoteSessionView` without error.

**Validates: Requirements 2.1, 2.2, 2.3, 2.4**

Property 2: Preservation - 桌面端 RemoteEnabled 检查不变

_For any_ session creation request where the launch source is `desktop` (or unspecified, defaulting to desktop) and `cfg.RemoteEnabled` is false, the fixed session creation functions SHALL continue to return "remote mode is disabled" error, preserving the existing access control behavior for desktop UI calls.

**Validates: Requirements 3.1, 3.2, 3.4**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `remote_mobile_launch.go`

**Struct**: `RemoteStartSessionRequest`

**Specific Changes**:
1. **添加 `LaunchSource` 字段到 `RemoteStartSessionRequest`**: 新增 `LaunchSource RemoteLaunchSource` 字段，允许调用方指定请求来源。

**Function**: `StartRemoteSessionForProject()`

2. **条件化 RemoteEnabled 检查**: 将 `if !cfg.RemoteEnabled { return error }` 改为仅在 `req.LaunchSource` 为 `desktop`（或空值）时执行检查。使用 `req.LaunchSource` 而非硬编码来设置 `spec.LaunchSource`。

---

**File**: `remote_status.go`

**Function**: `StartRemoteSession()`

3. **添加 `launchSource` 参数**: 在函数签名中新增 `launchSource RemoteLaunchSource` 参数，条件化 `RemoteEnabled` 检查。

**Function**: `StartRemoteHandoffSession()`

4. **添加 `launchSource` 参数**: 同上，在函数签名中新增 `launchSource RemoteLaunchSource` 参数，条件化 `RemoteEnabled` 检查。

---

**File**: `im_message_handler.go`

**Function**: `toolCreateSession()`

5. **传递 IM 来源**: 构造 `RemoteStartSessionRequest` 时设置 `LaunchSource: RemoteLaunchSourceAI`（或 `RemoteLaunchSourceMobile`），标识请求来自 IM 渠道。

---

**所有调用点更新**: 检查 `StartRemoteSession()` 和 `StartRemoteHandoffSession()` 的所有调用点，桌面端调用传入 `RemoteLaunchSourceDesktop`，IM/Agent 调用传入对应来源。

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: 编写测试用例模拟 IM 渠道调用 `StartRemoteSessionForProject()`，在 `RemoteEnabled = false` 的配置下观察返回错误。在未修复代码上运行以确认 bug 存在。

**Test Cases**:
1. **IM 渠道 Claude 会话**: 模拟 IM 渠道调用 `StartRemoteSessionForProject(tool="claude")`，`RemoteEnabled=false`（will fail on unfixed code）
2. **IM 渠道 Codex 会话**: 模拟 IM 渠道调用 `StartRemoteSessionForProject(tool="codex")`，`RemoteEnabled=false`（will fail on unfixed code）
3. **IM 渠道 Gemini 会话**: 模拟 IM 渠道调用 `StartRemoteSessionForProject(tool="gemini")`，`RemoteEnabled=false`（will fail on unfixed code）
4. **IM 渠道 OpenCode 会话**: 模拟 IM 渠道调用 `StartRemoteSessionForProject(tool="opencode")`，`RemoteEnabled=false`（will fail on unfixed code）

**Expected Counterexamples**:
- 所有 IM 渠道请求在 `RemoteEnabled=false` 时返回 "remote mode is disabled" 错误
- Possible causes: 无条件 `RemoteEnabled` 检查，缺少来源感知

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := StartRemoteSessionForProject_fixed(input)
  ASSERT result.error == nil OR result.error != "remote mode is disabled"
  ASSERT result.session IS valid RemoteSessionView
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT StartRemoteSessionForProject_original(input) = StartRemoteSessionForProject_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain
- It catches edge cases that manual unit tests might miss
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for desktop-source calls with `RemoteEnabled=false` and `RemoteEnabled=true`, then write property-based tests capturing that behavior.

**Test Cases**:
1. **桌面端 RemoteEnabled=false 保持拦截**: 验证桌面端来源在 `RemoteEnabled=false` 时继续返回错误
2. **桌面端 RemoteEnabled=true 正常创建**: 验证桌面端来源在 `RemoteEnabled=true` 时正常创建会话
3. **IM 渠道 RemoteEnabled=true 正常创建**: 验证 IM 来源在 `RemoteEnabled=true` 时行为不变
4. **默认来源（空值）按桌面端处理**: 验证未指定来源时默认为桌面端行为

### Unit Tests

- 测试 `StartRemoteSessionForProject()` 在不同 `LaunchSource` + `RemoteEnabled` 组合下的行为
- 测试 `StartRemoteSession()` 在不同 `launchSource` + `RemoteEnabled` 组合下的行为
- 测试 `StartRemoteHandoffSession()` 在不同 `launchSource` + `RemoteEnabled` 组合下的行为
- 测试 `RemoteStartSessionRequest` 的 `LaunchSource` 字段正确传递到 `LaunchSpec`

### Property-Based Tests

- 生成随机 `RemoteLaunchSource` 和 `RemoteEnabled` 组合，验证：非桌面来源跳过检查，桌面来源执行检查
- 生成随机工具名和项目路径，验证 `LaunchSource` 传递不影响其他参数处理
- 生成随机配置组合，验证桌面端行为完全不变

### Integration Tests

- 端到端测试：模拟飞书消息 → `toolCreateSession()` → `StartRemoteSessionForProject()` 完整流程
- 测试 `remote_hub_client.go` 中 `handleSessionStart` 解析 `LaunchSource` 字段
- 测试多个调用点（orchestrator.go、app_nl_skills.go、swarm_agent_scheduler.go）正确传递来源
