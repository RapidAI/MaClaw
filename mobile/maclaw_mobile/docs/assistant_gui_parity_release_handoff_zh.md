# AI 助手 GUI 对齐 — 发布与真机验收交接

面向：**本迭代已完成的 App + Hub 改动**如何一起上线并验收。  
配套计划：`assistant_gui_parity_dev_plan_zh.md`。

---

## 1. 交付物一览

| 组件 | 状态 | 路径 / 说明 |
|------|------|-------------|
| Mobile release APK | 已本地构建（含 4.3 任务卡） | 见下方 **§1.1 当前 APK** |
| Hub 二进制包 | 已本地构建 / 打包 | 见下方 **§1.2 当前 Hub**（**未**自动部署到生产） |
| Hub `MobileSearch` | 代码已就绪 | `hub/internal/httpapi/mobile_handlers.go` |
| 运行时边界 | 通过 | `python tool/verify_runtime_boundary.py`（无 corelib 进 APK） |

### 1.1 当前 APK（2026-07-11 13:16）

| 项 | 值 |
|----|-----|
| 路径 | `mobile/maclaw_mobile/build/app/outputs/flutter-apk/app-release.apk` |
| 大小 | 76.38 MB（80 089 890 bytes） |
| SHA256 | `F81A91C708B9EEE1E16E9A349911D520C059289FC276121F47D8E450680730F6` |
| 包含 | Phase 1–4 + 4.3 任务卡 + **助手→员工任务草稿交接** |

安装：

```powershell
adb install -r mobile\maclaw_mobile\build\app\outputs\flutter-apk\app-release.apk
```

### 1.2 当前 Hub（2026-07-11 12:48）

| 项 | 值 |
|----|-----|
| 可执行文件 | `hub/dist/maclaw-hub.exe` |
| 发布目录 | `hub/package/maclaw-hub/`（exe + configs 示例 + web 静态资源） |
| 大小 | 38.53 MB（40 399 360 bytes） |
| SHA256 | `38D1F3864E249634C426272CA6F982698C69C668FD79C74DB45C75E06789128D` |
| 本地复现 | `cd hub` → `.\build.cmd build` → `.\build.cmd`（package） |
| 单测 | `go test ./internal/httpapi/ -run "MobileSearch|MobileBuild|MobileOpenAI"` → ok |

**部署注意（运维）：**

1. 停旧 Hub 进程 → 备份现有 `maclaw-hub.exe` 与 `configs/config.yaml`  
2. 用本包 **exe** 覆盖目标机二进制；**不要**用包内示例 `config.yaml` 直接覆盖生产配置  
3. 启动后跑 §2.3 curl 烟雾，再装 §1.1 APK 做真机验收  

**能力分层（分开发布也可）：**

| 用户可见能力 | 依赖 |
|--------------|------|
| HTML 清洗、来源折叠、Markdown/表格渲染、打字机 UI | **新 App** |
| 回答后「可以继续」任务卡（草稿 / 员工 / 文档） | **新 App** |
| 结构化总结 prompt、snippet 清洗、非 SERP 兜底 | **新 Hub** |
| `messages[]` 多轮 role 协议 + 上游 SSE 真流 | **新 Hub + 新 App** |
| Hub 侧 agent 工具（`web_search` / `web_fetch`）+ 工具事件展示 | **新 Hub + 新 App** |
| GUI 级 bash/读盘/SSH/浏览器全量工具 | **不做进手机**；重活走数字员工 / 桌面 GUI |

仅装新 App、Hub 未发：多轮仍走 legacy `context`，流式可能退化为假流/JSON；任务卡仍可用。

---

## 2. Hub 部署清单（运维）

> 不自动改生产配置。按你们现有 Hub 发布流程执行；下列为**本仓库本地可复现**步骤。

### 2.0 只读预检脚本（推荐）

```powershell
cd mobile\maclaw_mobile

# 离线：校验本机 APK + Hub 二进制存在，并核对交接文档中的 SHA256
python tool/verify_assistant_parity_release.py --require-package `
  --expect-apk-sha256 F81A91C708B9EEE1E16E9A349911D520C059289FC276121F47D8E450680730F6 `
  --expect-hub-sha256 38D1F3864E249634C426272CA6F982698C69C668FD79C74DB45C75E06789128D

# 可选：对已部署 Hub 做只读烟雾（不写配置、不部署）
python tool/verify_assistant_parity_release.py `
  --hub-url https://YOUR-HUB `
  --viewer-token YOUR_VIEWER_TOKEN `
  --probe-stream
```

单测：`python -m unittest tool/verify_assistant_parity_release_test.py`

### 2.1 变更范围（回滚粒度）

主要文件：

- `hub/internal/httpapi/mobile_handlers.go`
- `hub/internal/httpapi/mobile_handlers_test.go`

对外契约：

- `POST /api/mobile/search` 仍兼容：
  - 旧：`{"query","context":string[],"stream":false}`
  - 新：`{"query","messages":[{"role","content"}],"stream":true}`
- 响应：
  - 非流：JSON `answer` / `citations` / `llm_*` / `status`
  - 流：SSE `event: meta|delta|done|error`（`Content-Type: text/event-stream`）

### 2.2 构建与单测（发布前）

```powershell
cd hub
go test ./internal/httpapi/ -count=1 -run "MobileSearch|MobileBuild|MobileOpenAI"
# 按仓库惯例打包，例如：
.\build.cmd
# 或
.\scripts\build.ps1
```

### 2.3 发布后烟雾（有 Viewer Token 时）

将 `HUB`、`TOKEN` 换成环境值。

**非流多轮：**

```bash
curl -sS -X POST "$HUB/api/mobile/search" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"query":"接着说","messages":[{"role":"user","content":"先问天气"},{"role":"assistant","content":"今天晴"}],"stream":false}'
```

期望：`answer` 为整理后中文总结（非纯 snippet 列表）；`llm_mode` 为 `maclaw_official` 或 `desktop_qr_third_party`。

**流式：**

```bash
curl -sS -N -X POST "$HUB/api/mobile/search" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{"query":"服务器状态如何","stream":true}'
```

期望响应头含 `text/event-stream`；body 依次出现 `event: meta`、`event: delta`、`event: done`。

### 2.4 回滚

回滚到上一版 Hub 二进制即可。旧 App 仅发 `query`/`context` 仍可用；新 App 在旧 Hub 上会忽略 `messages`/`stream` 语义（或走 JSON 回退）。

---

## 3. Mobile 安装

```powershell
cd mobile\maclaw_mobile
# 已有产物时：
adb install -r build\app\outputs\flutter-apk\app-release.apk
```

记录：

- 包路径与安装时间
- `Get-FileHash ...\app-release.apk -Algorithm SHA256`
- 设备型号 / Android 版本

正式签字 QA 仍走 `docs/qa_device_checklist.md` 与 `tool/build_android_release.py` 流水线。

---

## 4. 真机验收清单（本迭代专用）

在 **新 App + 新 Hub** 上勾选。脱敏后再存截图/录屏。

### 4.1 伴侣感与呈现

- [ ] 冷启动 → 登录 → 进入「AI助手」：是聊天问候，不是表单/搜索墙
- [ ] 回复主视觉是**总结**，来源默认折叠为「参考 N 条」
- [ ] 展开来源后无裸露 `&ensp;` / `&#…;`
- [ ] 含表格/标题的 Markdown 能正确渲染（或至少列表可读）

### 4.2 内容质量（Hub）

- [ ] 问「北京今天天气」：有结论 + 要点/表格倾向；非站点 snippet 堆叠
- [ ] 问排查类（如 nginx 502）：结论优先，引用为脚注编号感

### 4.3 多轮、流式与任务卡

- [ ] 第一问后立刻追问「那明天呢？」：第二问答案能接上上下文
- [ ] 发送后气泡出现「助手正在回答…」，文字渐进出现（打字机）
- [ ] 完成后状态变为完成；历史/Tab 内对话仍在
- [ ] 回答下方出现「可以继续」：整理为草稿 / 派给员工 / 打开文档可用
- [ ] 「派给员工」跳到数字员工并预填含【用户问题】【助手结论】的任务草稿

### 4.4 回归

- [ ] 复制回答 / 分享 / 整理为文档草稿可用
- [ ] 引用复制链接可用
- [ ] 语音输入、图/文件导入入口不崩
- [ ] 助手 feature 关闭时发送禁用文案符合预期
- [ ] 深色模式对比度可接受

### 4.5 失败路径

- [ ] 断网或超时：错误提示可读，不白屏
- [ ] 未登录：提示登录

---

## 5. 已知限制（勿当 bug 重开）

| 项 | 说明 |
|----|------|
| 非完整 GUI Agent | 无桌面工具环 / corelib；重活仍走数字员工或桌面 |
| 上游不支持 stream | Hub 会整包回答后 chunk 假流，UI 仍有打字机 |
| 旧 Hub | `messages`/`stream` 可能被忽略；App 会 JSON 回退 |
| Phase 4.4 | Hub 侧更深 agent 工具子集 **未做**（联网搜索已在 Hub） |
| 任务卡 | 回答后有「可以继续」：草稿 / 员工 / 文档（非完整 GUI 卡系统） |

---

## 6. 建议发布顺序

1. **先发 Hub**（或与 App 同窗口），再装 APK — 一次验收吃满全部能力。  
2. 若只能先发 App：仍可验收呈现层（清洗/折叠/Markdown），在记录中注明「Hub 未发，多轮/真流待二次验」。  
3. 验收通过后按 `qa_device_checklist.md` 补签字与 evidence。

---

## 7. 自动化基线（开发机已跑过）

```text
Hub:  go test ./internal/httpapi/ -run "MobileSearch|MobileBuild|MobileOpenAI"  → ok
App:  flutter test assistant_stream / markdown / html / screen               → ok
Bound: python tool/verify_runtime_boundary.py                                   → ok
APK:  build/app/outputs/flutter-apk/app-release.apk                          → built
```
