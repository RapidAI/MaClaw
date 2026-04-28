# LLM Transient Error 指数退避重试 + 错误恢复设计

## 问题描述

maclaw 在执行任务时，如果 LLM 服务返回可恢复的服务端错误（HTTP 429/408/5xx/overload），agent loop 直接报错退出。用户说"继续吧"后，maclaw 不知道之前在做什么任务——"失忆"。

## 设计原则

**不按 HTTP 状态码逐个添加重试逻辑**。正确的抽象是"可恢复的服务端暂时不可用"（transient server error），统一一个分类、一套退避策略。

错误分为两大类：
- **Transient**（服务端暂时不可用）：429、408、5xx、overload、quota exceeded → 长退避（5s → 10s → 20s）
- **Network**（客户端连接问题）：timeout、connection refused、reset → 短退避（1s → 2s → 4s）

不可恢复的错误（400 bad request、401 unauthorized、403 forbidden）不重试。

## 架构

```
isTransientServerError()     ← 单一数据源：所有可恢复服务端错误的识别
  ↑                            覆盖 429/408/5xx/overload/quota/智谱code:1234
  |
  ├── AdaptiveRetry.Classify() → FailureTransient → Decide() → 5s/10s/20s 退避
  |
  ├── isRetryableLLMError()    → fallback 路径（无 AdaptiveRetry 时）
  |
  └── isRateLimitError()       → 保留为窄别名，用于进度消息区分
```

## 覆盖的错误类型

| 错误 | 示例消息 | 分类 | 退避策略 |
|------|---------|------|---------|
| HTTP 429 | "请求过于频繁" | Transient | 5s → 10s → 20s |
| HTTP 408 | "Request Timeout" | Transient | 5s → 10s → 20s |
| HTTP 500 | "服务端错误" | Transient | 5s → 10s → 20s |
| HTTP 502 | "网关错误" | Transient | 5s → 10s → 20s |
| HTTP 503 | "服务暂时不可用" | Transient | 5s → 10s → 20s |
| HTTP 504 | "网关超时" | Transient | 5s → 10s → 20s |
| Overloaded | "server is overloaded" | Transient | 5s → 10s → 20s |
| Quota | "quota exceeded" | Transient | 5s → 10s → 20s |
| 智谱 code:1234 | "网络错误" | Transient | 5s → 10s → 20s |
| Timeout | "context deadline exceeded" | Network | 1s → 2s → 4s |
| Connection refused | "connection refused" | Network | 1s → 2s → 4s |
| HTTP 401 | "认证失败" | Permission | 不重试 |
| HTTP 403 | "拒绝访问" | Permission | 不重试 |
| HTTP 400 | "Bad Request" | Args | 不重试 |
