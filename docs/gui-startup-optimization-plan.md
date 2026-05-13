# GUI 启动与响应优化方案

## 问题诊断（来自 maclaw.log 2026-05-13）

| 阶段 | 耗时 | 问题 |
|------|------|------|
| `initCoreInfra`（含 Skill 扫描） | **8.0s** | Skill 目录同步扫描阻塞启动 |
| Hub Connect `send_hello` | **45.5s** | 同步阻塞 `startup()` 返回 |
| 总启动到 ready | **53.8s** | 界面完全卡死 53 秒 |
| 用户发消息 → `entry_context` | **5.1s** | UIC 超时 3s + task-context LLM 1.8s |
| 用户发消息 → 第一个 token | **47s** | 含重复 Skill 扫描 + 多次 LLM 调用 |

**用户体验**：启动后界面卡死 53 秒；发消息后 47 秒才看到第一个字。

## 优化目标

- 启动到界面可交互：**< 3 秒**
- 用户发消息到第一个可见反馈（进度提示或 token）：**< 3 秒**
- 用户发消息到第一个 LLM token：**< 5 秒**（取决于 API 延迟）

## 优化方案

### 第一优先级：消除启动阻塞（53s → <3s）

#### 1.1 `createAndWireHubClient` 从同步改为两阶段异步

**当前**：`startup()` 中同步调用 `createAndWireHubClient()`，其中 `hubClient.Connect()` 的 `send_hello` 卡 45 秒。

**优化**：拆分为两阶段：
- **阶段 A（同步，<200ms）**：创建 HubClient + ensureIMHandler + WebSocket dial + auth。认证完成（`auth.ok`）后立即 `markAIAssistantReady()`，`startup()` 返回。
- **阶段 B（异步 goroutine）**：`send_hello`（同步设备信息、Skill 列表、在线状态等）。这些是 Hub 侧的状态同步，不影响用户发消息。

```go
// startup() 中：
go func() {
    hubClient := a.createAndWireHubClientFast() // 阶段 A：dial + auth + ensureIMHandler
    a.markAIAssistantReady()                     // 立即标记 ready
    hubClient.SyncHello()                        // 阶段 B：后台同步
}()
```

**预期收益**：startup() 在 <1s 内返回，界面立即可交互。

#### 1.2 `initCoreInfra` 中 Skill 扫描异步化

**当前**：`NewSkillExecutor` 或 `toolDefGenerator` 构建时同步调用 `ScanSkillDir()`，扫描所有 Skill 目录解析 SKILL.md/skill.yaml，耗时 8 秒。

**优化**：
- `NewSkillExecutor` 构造时不扫描，只记录 Skill 目录路径
- 启动后立即在 goroutine 中执行 `skillExecutor.WarmScan()`
- 在扫描完成前，`skillExecutor.ListSkills()` 返回空列表（降级但不阻塞）
- 扫描完成后通过 `atomic.Bool` 标记就绪，后续调用返回缓存结果

```go
func (a *App) initCoreInfra() {
    // ... 其他初始化 ...
    a.skillExecutor = NewSkillExecutor(a, a.mcpRegistry, a.remoteSessions)
    // 不在这里扫描！
    go a.skillExecutor.WarmScan() // 后台扫描
}
```

**预期收益**：`initCoreInfra` 从 8s 降到 <500ms。

#### 1.3 Skill 扫描结果缓存

**当前**：日志显示 Skill 扫描在启动后执行了 3 次（05:50:37、05:51:35、05:52:15），每次都重新解析文件系统。

**优化**：
- `ScanSkillDir` 结果缓存到 `skillExecutor.cachedSkills`
- 缓存有效期 30 秒（或直到 Skill 安装/删除时手动失效）
- `Route()` 和 `toolDefGenerator.Generate()` 使用缓存结果

```go
type SkillExecutor struct {
    cachedSkills    []NLSkillEntry
    cacheValid      atomic.Bool
    cacheTime       time.Time
    cacheMu         sync.RWMutex
}

func (se *SkillExecutor) ListSkills() []NLSkillEntry {
    se.cacheMu.RLock()
    if se.cacheValid.Load() && time.Since(se.cacheTime) < 30*time.Second {
        defer se.cacheMu.RUnlock()
        return se.cachedSkills
    }
    se.cacheMu.RUnlock()
    // 重新扫描并更新缓存
    se.refreshCache()
    return se.cachedSkills
}
```

**预期收益**：消除重复扫描，每次 `Route()` 调用从 ~1s 降到 <1ms。

---

### 第二优先级：消除消息处理延迟（47s → <5s）

#### 2.1 `resolveIMEntryContext` 中的 UIC 分类超时缩短

**当前**：UIC fusion 的 tree channel 超时 3 秒（`tree channel deadline exceeded (3s)`），加上 embedding 90ms，总计 3.1s。

**优化**：
- Tree channel（LLM 分类）超时从 3s 降到 **1.5s**
- 对短消息（<10 字符如"北京天气"），跳过 UIC fusion，直接走 agent loop
- UIC 结果不影响 agent loop 的正确性（只影响工作流拦截），超时时降级为 `ambiguous` 即可

#### 2.2 `task-context` LLM 调用异步化或取消

**当前**：`resolveIMEntryContext` 中调用 `task-context` LLM（1.8s）判断是 continue 还是 new task。

**优化**：
- 对话历史 <5 条时，跳过 task-context LLM 调用（新会话不需要判断）
- 对话历史 ≥5 条时，task-context 判断与 system prompt 构建并行执行
- 超时 2s，超时后默认 `continue`

#### 2.3 `send_hello` 不阻塞消息处理

**当前**：如果 1.1 的优化后 `send_hello` 仍在后台执行，用户发消息时 Hub 可能还没完成 hello。

**优化**：消息处理不依赖 `send_hello` 完成。`HandleIMMessageWithProgressAndStream` 只需要：
- LLM config（本地配置，无网络依赖）
- IMMessageHandler（已在阶段 A 创建）
- 对话历史（本地文件）

这些在 `auth.ok` 后就全部就绪。

#### 2.4 首条消息立即发送进度提示

**当前**：用户发消息后，前端在收到第一个 `onToken` 之前完全无反馈。

**优化**：在 `handleIMMessageWithLoop` 入口处，如果 `onProgress != nil`，立即发送一条进度提示：

```go
func (h *IMMessageHandler) handleIMMessageWithLoop(...) {
    if onProgress != nil {
        onProgress("正在思考...")
    }
    // ... 后续处理
}
```

**预期收益**：用户发消息后 <100ms 看到"正在思考..."，消除"界面卡死"的感知。

---

### 第三优先级：减少 LLM 调用次数

#### 3.1 消除启动阶段的无效 LLM 调用

**当前日志**：启动后有大量 `[LLM] POST deepseek-reasoner` 调用（05:50:37 到 05:51:56 之间约 15 次），这些是：
- UIC warmup（`text="warmup"`）
- IntentClassifier anchor warmup
- 各种后台 LLM 调用

**优化**：
- UIC warmup 使用更轻量的模型（如 `deepseek-chat` 而非 `deepseek-reasoner`）
- 或者 warmup 使用本地 embedding 而非远程 LLM
- `deepseek-reasoner` 的 thinking 阶段很慢（每次 2-5s），warmup 不应该用它

#### 3.2 工具路由中的 Skill 扫描去重

**当前**：`Route()` 每次调用都触发 `ScanSkillDir`（通过 `skillMatchScore`）。

**优化**：已在 1.3 中通过缓存解决。

---

## 优化后的预期时间线

### 启动流程（目标 <3s）

```
T+0ms     startup() begin
T+2ms     platform + config watcher
T+20ms    LoadConfig (缓存命中)
T+100ms   initCoreInfra (不含 Skill 扫描)
T+150ms   ensureInteractionInfra
T+200ms   NewIMMessageHandler
T+250ms   Hub dial + auth (WebSocket 连接 ~50ms)
T+300ms   markAIAssistantReady → 前端显示"就绪"
          --- startup() 返回，界面可交互 ---
T+300ms+  [后台] Hub send_hello
T+300ms+  [后台] Skill 目录扫描 + 缓存
T+300ms+  [后台] Embedding 模型加载
T+300ms+  [后台] UIC anchor warmup
```

### 消息处理流程（目标 first_token <5s）

```
T+0ms     用户按 Enter
T+50ms    onProgress("正在思考...") → 前端显示
T+100ms   preflight (本地操作)
T+150ms   serialization boundary
T+200ms   resolveIMEntryContext:
            - UIC 分类: 跳过（短消息）或 1.5s 超时
            - task-context: 跳过（历史<5条）或并行执行
T+300ms   executePreparedIMEntry → runAgentLoop
T+400ms   system prompt 构建 (proactive recall ~40ms)
T+500ms   tool routing (缓存命中 <5ms)
T+600ms   LLM HTTP 请求发出
T+1000ms  LLM 第一个 SSE token 到达 → onToken → 前端显示
          --- 用户看到第一个字 ---
```

## 实施优先级

| 序号 | 改动 | 预期收益 | 风险 | 工作量 |
|------|------|---------|------|--------|
| 1 | Hub Connect 两阶段异步 | 启动 -45s | 低（hello 延迟不影响功能） | 中 |
| 2 | Skill 扫描缓存 | 启动 -8s，每次消息 -1s | 低（30s 缓存足够） | 小 |
| 3 | Skill 扫描异步化 | 启动 -8s | 低（降级为空列表） | 小 |
| 4 | 首条消息立即进度提示 | 感知延迟 -100% | 无 | 极小 |
| 5 | UIC 超时缩短 | 消息处理 -1.5s | 低（降级为 ambiguous） | 极小 |
| 6 | task-context 跳过/并行 | 消息处理 -1.8s | 低（默认 continue） | 小 |
| 7 | warmup 用轻量模型 | 启动后台 LLM 开销减少 | 低 | 小 |

**建议实施顺序**：4 → 1 → 2+3 → 5+6 → 7

第 4 项（立即进度提示）是零风险的即时改善——用户发消息后 <100ms 看到反馈，即使后续处理仍需几秒，体验也完全不同。
