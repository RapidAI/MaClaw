# FAQ - 常见问题解答

## 1. 为什么系统托盘图标点击无反应？
在较早版本中，如果后台操作（如文件读写）阻塞了主线程，可能会导致托盘图标暂时失去响应。当前版本已通过异步处理和线程锁定优化了此问题。如果仍遇到此类情况，请尝试重启程序。

## 2. 如何使用自定义模型 (Custom Model)？
1. 在侧边栏选择对应的 AI 工具（如 Claude）。
2. 点击“模型设置”。
3. 选择“Custom”标签。
4. 输入您的模型名称（例如 `claude-3-5-sonnet-20241022`）。
5. 输入兼容协议的 API 端点地址（Endpoint）。
6. 输入 API Key 并保存。

## 3. 我的 API Key 无法工作？
程序中预设的部分模型快捷选择**仅支持各厂商提供的特定编程专用 API Key**。
如果您使用的是通用型 API Key，请使用 **“Custom”** 模式进行配置，并手动输入对应的模型名称和 API 端点地址。

## 4. 配置文件保存在哪里？
MaClaw（码卡龙） 的配置文件保存在您的用户主目录下，路径为 `~/.maclaw/config.json`。
各个 AI 工具的原生设置（如 Claude 的 `~/.claude/settings.json`）也会根据配置进行自动同步。

## 5. 如何更新 AI 命令行工具？
每次启动程序时，MaClaw（码卡龙） 会自动检查已支持工具（如 `claude-code`, `codex`, `opencode`）的版本。如果有新版本，它会自动为您尝试更新。您也可以在启动时的进度窗口中查看具体的执行状态。

## 6. 环境检查失败怎么办？
如果 Node.js 或工具安装失败，请检查您的网络连接。在中国大陆地区，程序会自动尝试使用国内镜像源以加快下载速度。如果自动安装持续失败，建议根据提示手动安装对应的运行环境。

## 7. 怎么使用各工具的原始模型服务？
在服务商中选择 **AICoderMirror**, 填写 API Key 即可。该服务商为原厂服务转发商，可以方便地访问原生模型服务。

## 8. 怎么使用各工具自身提供的服务？
在工具的服务商中选择 **“原厂” (Original)**，即恢复各自原始状态。在这种模式下，MaClaw（码卡龙） 会自动清除所有相关的自定义代理配置、环境变量以及工具自身的配置文件（如 `~/.claude`），您可以直接使用工具原有的认证和调用方式。

## 9. Yolo 模式是什么意思？
设置 Yolo 模式后，编程工具不再每次操作文件或系统时询问，提升编程体验。但是请注意，该选项有风险，存在误杀文件的可能，只适合专家使用。

## 10. MaClaw（码卡龙）支持哪些工具？
目前支持 **Claude Code**, **OpenAI Codex**, **OpenCode**, **CodeBuddy** 以及 **Qoder CLI**。您可以在侧边栏快速切换并针对每个工具进行独立配置。

## 11. 为什么切换服务商后，工具的行为没有改变？
请确保您在切换服务商后点击了主界面的 **“Launch”** 按钮重新启动工具。MaClaw（码卡龙） 会在启动前根据您的最新配置自动同步环境。如果仍有问题，可以尝试在服务商中先切换到 **“Original”** 模式以清除旧配置，然后再切换回目标服务商。

## 12. Qoder CLI 中“原厂”与“Qoder”服务商的区别是什么？
*   **原厂 (Original)**：表示使用 Qoder CLI 默认的认证方式，即通过**浏览器登录**进行授权。
*   **Qoder**：表示使用**个人令牌 (Personal Access Token)** 进行认证。您可以在 Qoder 官网获取令牌并填入 MaClaw（码卡龙），这种方式适合在无法打开浏览器或需要快速部署的环境下使用。

## 14. 程序支持哪些 Python 环境？
目前主要支持 **Conda/Anaconda** 环境。在项目设置中开启 “Python 项目” 后，MaClaw（码卡龙） 会扫描系统中的 conda 环境供您选择。启动时会自动执行环境切换。

## 15. 为什么需要管理员权限启动？
某些项目可能涉及系统级文件操作或受限目录访问，使用管理员权限启动可以避免工具因权限不足而报错。该功能目前仅支持 Windows 系统。

## 16. 为什么 Codex 无法安装某些技能？
Codex 目前仅支持 **Zip 包**格式的技能。如果您尝试安装 Skill ID (Address) 类型的技能（如 `@org/skill`），系统会提示不兼容。此类技能仅支持在 Claude Code 中使用。请尝试获取该技能的 Zip 包版本进行安装。

## 17. 技能是所有工具共享的吗？
是的。通过 **Zip 包**添加的技能会存储在全局仓库中，**Claude**, **Codex** 等所有支持技能的工具均可自动识别并使用。您只需添加一次，即可在任意工具中调用。

## 18. AI 助手/工作流生成 PPT 或长代码时中途停止（tool call 被截断）

### 现象

在 PPT 设计工作流的生成阶段，或任何需要 AI 生成大量代码/内容的场景中，AI 助手反复尝试调用 `write_file`/`bash` 等工具但参数被截断（日志中出现 `truncated tool call (invalid JSON)` + `finish_reason=`），最终 agent loop 停止。

### 根因

使用 Thinking/Reasoning 模型（如 DeepSeek V4 Flash）时，模型在复杂请求（大 context）下的 reasoning 阶段会有 **60-90 秒的静默期**（不产出任何 SSE 数据）。如果 HubCenter 前面的 Nginx 反向代理的 `proxy_read_timeout` 设置不够大（默认 60 秒），Nginx 会在 reasoning 静默期内认为上游无响应，主动切断 SSE 连接，导致 tool call JSON 不完整。

### 解决方案

**1. Hub 的 Nginx 配置**（已在 hub_manual.md 第 12 节示例中正确设置 3600s）：

```nginx
location / {
    proxy_pass http://127.0.0.1:9399;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

**2. HubCenter 的 Nginx 配置（关键！容易遗漏）**：

HubCenter 需要转发 LLM 请求到后端 API（DeepSeek/GLM 等），其中 `/api/llm/` 路径必须有足够大的超时：

```nginx
server {
    listen 443 ssl http2;
    server_name hubs.example.com;

    # ... SSL 配置 ...

    # LLM 代理路径 - 必须设置大超时
    location /api/llm/ {
        proxy_pass http://127.0.0.1:9499;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;

        # ⚠️ 关键：Thinking 模型 reasoning 阶段可能静默 60-180 秒
        proxy_read_timeout 600s;
        proxy_send_timeout 600s;
        proxy_connect_timeout 30s;

        # ⚠️ 关键：SSE 流式响应必须禁用缓冲
        proxy_buffering off;
    }

    # 其他 API 路径
    location / {
        proxy_pass http://127.0.0.1:9499;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
```

**3. 各层超时要求汇总**：

| 层级 | 配置项 | 最低要求 | 说明 |
|------|--------|---------|------|
| Hub Nginx | proxy_read_timeout | 600s | Hub → 客户端的 SSE 流 |
| HubCenter Nginx | proxy_read_timeout | 600s | HubCenter → 后端 LLM API 的 SSE 流 |
| HubCenter Nginx | proxy_buffering | off | SSE 流必须禁用缓冲 |
| Hub Go 代码 | MaClawProviderClient.TimeoutSec | 600（默认） | Hub → HubCenter 的 HTTP 超时 |
| HubCenter Go 代码 | proxyStreamingHTTPClient | ResponseHeaderTimeout=600s | HubCenter → 后端 API 首字节超时 |

**4. 验证方法**：

在 HubCenter 服务器上直接测试后端 API 是否正常响应长输出请求：

```bash
curl -X POST https://api.deepseek.com/v1/chat/completions \
  -H "Authorization: Bearer YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"Write a 500-line Python script"}],"tools":[{"type":"function","function":{"name":"write_file","description":"Write to file","parameters":{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}}}],"max_tokens":65536,"stream":true}' \
  --max-time 600 -o /dev/null -w "HTTP %{http_code} Time %{time_total}s Size %{size_download}\n"
```

如果直连后端正常但通过 HubCenter Nginx 失败，问题就在 Nginx 超时配置。

## 19. 不同模型对长内容生成的影响

| 模型类型 | 是否有 reasoning 静默期 | 长 tool call 风险 | 建议 |
|---------|----------------------|-----------------|------|
| GLM-5.1/5.2 | ❌ 无 | 低（65K output 全给内容） | 直接使用，无需特殊配置 |
| DeepSeek V4 Flash (thinking) | ✅ 有（60-180s） | 高（reasoning 消耗 output 预算） | 确保 Nginx 超时 ≥ 600s |
| DeepSeek V4 (非 thinking) | ❌ 无 | 低 | 同 GLM |
| Kimi K2 / Qwen thinking | ✅ 可能有 | 中 | 确保 Nginx 超时 ≥ 600s |

---
*更多问题请访问 GitHub Issues：[RapidAI/cceasy/issues](https://github.com/RapidAI/cceasy/issues)*
