# Mobile 录音入库与会议纪要设计

## 1. 背景与结论

Mobile 端已经具备录音、断点上传、Hub 持久化、转写和会议纪要生成能力；Desktop 的“Mobile 文稿库”也已经能够展示 Hub 共享文稿。但两者目前是两套相邻数据：录音上传后只存在于会议录音记录中，转写稿和纪要才会被写为文稿。因此，Desktop 用户无法在文稿库中发现刚上传的原始音频，也无法从文稿库统一发起处理。

本方案将 **会议录音作为 Mobile 文稿库中的一类媒体条目**，使其在 Hub 上传完成后自动可见。Desktop 是音频的处理和查看中心；Mobile 继续以录音工具为主，只提供录音历史和轻量管理，不承载复杂的文稿库浏览、编辑或纪要操作。

默认处理策略为：用户在 Desktop 的音频详情中点击“生成会议纪要”，系统先转写，再根据转写生成纪要。完成后保留三类相互关联的资产：原始音频、逐字稿、会议纪要。

## 2. 目标与非目标

### 目标

- 手机录音成功上传并完成归档后，自动出现在 Desktop 的“Mobile 文稿库”。
- 文稿库统一展示普通文档和音频，但音频有明确类型、时长、大小、处理状态和播放器。
- 对音频提供一个主操作“生成会议纪要”；该操作自动完成转写与纪要生成。
- 同时保留、可打开并相互跳转：原始音频、逐字稿、会议纪要。
- Mobile 端可继续查看本人的录音记录，并进行删除、重试上传等基本文件管理；不新增复杂的文稿处理流程。
- 保持现有的 Hub 鉴权、租户/用户隔离、录音保留期与 Worker 安全边界。

### 非目标（首版不做）

- 不在 Mobile 的“文档”页完整显示或编辑共享文稿库。
- 不在手机端提供纪要编辑器、批量处理、复杂检索或音频剪辑。
- 不将原始音频直接发送给 LLM；纪要 Worker 仍然只接收经过 ASR 验证的逐字稿。
- 不改变现有录音格式、分片上传协议或 Worker 调用契约。
- 不支持 Desktop 直接上传任意音频作为会议录音的反向入口；该能力可作为后续扩展单独评估。

## 3. 用户与主流程

### 角色分工

| 终端 | 核心职责 | 不承担的职责 |
| --- | --- | --- |
| Mobile | 录音、上传、查看上传/处理状态、删除录音、必要时重试 | 文稿库深度浏览、纪要生成、编辑逐字稿/纪要 |
| Hub | 录音事实源、存储、权限、异步编排、状态广播、派生产物关联 | 前端呈现逻辑 |
| Desktop | 发现音频、播放、发起纪要、查看逐字稿/纪要、下载或删除 | 录制、绕过 Hub 直接读取音频 |

### 主流程

```text
Mobile 录音
  -> 分片上传到 Hub
  -> complete 校验成功，状态为 uploaded
  -> Hub 将录音发布为 Mobile 文稿库的 audio 条目
  -> Desktop 刷新“Mobile 文稿库”，可看到音频
  -> 点击音频，试听 / 下载 / 删除 / “生成会议纪要”
  -> Hub: 转写 Worker
  -> Hub: 会议纪要 Worker
  -> 生成并关联“逐字稿”和“会议纪要”文稿
  -> Desktop 实时更新状态，可打开两个派生文稿
```

### 处理状态

| 状态 | Desktop 呈现 | 可用动作 |
| --- | --- | --- |
| `uploading` / `finalizing` | “上传中”，不出现在默认文稿库列表；在 Mobile 录音历史可见 | 等待、重试上传（按既有能力） |
| `uploaded` | “可生成会议纪要” | 播放、下载、删除、生成会议纪要 |
| `processing` | 明确展示“正在转写”或“正在生成会议纪要”及进度 | 查看进度；禁止重复发起和删除 |
| `ready`（仅归档） | “音频已归档” | 播放、下载、删除、生成会议纪要 |
| `ready`（已处理） | “会议纪要已生成” | 播放、打开逐字稿、打开会议纪要、下载、删除 |
| `failed` | 展示失败原因和“重试生成会议纪要” | 播放、下载、重试、删除 |
| 原始音频已清理/过期 | “原始音频不可用，结果文稿仍保留” | 打开已有逐字稿/纪要；不允许重新处理 |

## 4. 信息模型与数据归属

### 4.1 统一库视图，不合并物理实体

不建议把音频二进制复制进现有 `mobileDocumentDraftRecord`，也不建议把会议录音伪造成 Markdown 文稿。Hub 继续以会议录音记录保存音频生命周期，以文稿记录保存 Markdown 生命周期；新增的是一个给 Desktop 消费的 **统一库读取模型**。

```text
mobileMeetingRecording（原始音频及处理状态）
  ├─ recording_id
  ├─ title / purpose / content_type / duration_sec / size_bytes
  ├─ audio_available / retention_until
  ├─ transcript_draft_id ──> mobileDocumentDraftRecord（逐字稿）
  └─ minutes_draft_id ────> mobileDocumentDraftRecord（会议纪要）

MobileLibraryItem（列表/详情投影，不单独持久化）
  ├─ type: document | audio
  ├─ source_id: draft_id | recording_id
  ├─ title / updated_at / preview
  ├─ audio 元数据（仅 audio）
  └─ relationships（逐字稿、会议纪要）
```

### 4.2 新增/扩展字段

建议 Desktop 与 Hub 的库接口返回以下统一结构；字段以 snake_case 对外。

```json
{
  "id": "meeting_1740000000000",
  "type": "audio",
  "title": "产品评审会-2026-07-24",
  "updated_at": "2026-07-24T09:30:00Z",
  "preview": "产品评审与行动项",
  "audio": {
    "content_type": "audio/wav",
    "size_bytes": 48234423,
    "duration_sec": 3662,
    "available": true,
    "download_url": "/api/mobile/meeting-recordings/meeting_.../audio"
  },
  "processing": {
    "status": "ready",
    "mode": "minutes",
    "progress": 1,
    "message": "meeting minutes ready",
    "failure_code": ""
  },
  "derived_documents": {
    "transcript_draft_id": "mobdoc_...",
    "minutes_draft_id": "mobdoc_..."
  },
  "retention_until": "2026-08-23T09:30:00Z"
}
```

普通文稿仍返回现有字段，并增加 `type: "document"`。旧客户端仍可继续使用现有 `/api/mobile/documents/drafts`；统一库接口采用新路径，避免修改旧接口的语义。

### 4.3 生命周期与删除语义

- 音频上传成功（`complete` 后状态为 `uploaded`）即自动出现在统一库；不等待用户点击“归档”。
- “生成会议纪要”会以 `mode=minutes` 调用既有处理链；Hub 已有的处理过程会创建逐字稿与纪要文稿，并写回两个 Draft ID。
- 删除“音频条目”默认删除会议录音及其原始音频，**不级联删除**已经生成的逐字稿和纪要。删除确认框必须说明该规则。
- 若用户希望删除所有结果，分别删除关联的文稿；首版不提供“一键删除全部”。
- 现有 30 天原始音频清理策略保持不变。到期清理仅令音频不可播放/不可重试，已生成的文稿和关联关系继续存在。

## 5. API 与 Hub 实现设计

### 5.1 建议接口

| 接口 | 用途 | 说明 |
| --- | --- | --- |
| `GET /api/mobile/library/items?limit=&cursor=&types=document,audio` | Desktop 统一列表 | 合并当前用户的普通文稿与完成上传的会议录音，按 `updated_at` 倒序 |
| `GET /api/mobile/library/items/{itemId}` | Desktop 详情 | 根据条目类型返回完整文稿或完整录音投影 |
| `GET /api/mobile/meeting-recordings/{recordingId}/audio` | 安全播放/下载 | 必须鉴权、核对 owner/tenant、检查音频仍在保留期；支持 `Range` 以便播放器拖动 |
| `POST /api/mobile/meeting-recordings/{recordingId}/process` | 生成纪要 | 复用现有接口；Desktop 固定提交 `{ "mode": "minutes" }` |
| `DELETE /api/mobile/meeting-recordings/{recordingId}` | 删除录音 | 复用现有接口与终态校验 |

首版不需要为统一库另建写入接口。音频条目来自会议录音域，文稿条目来自现有文稿域。

### 5.2 Hub 实施点

1. 在 `hub/internal/httpapi/mobile_meeting_recordings.go` 增加音频下载 Handler。它复用 `authenticateViewerRequest` 与 owner 校验；禁止路径参数拼接为文件路径；仅使用由录音内容类型决定的受控文件名。
2. 在文稿库读取侧新增 `MobileLibraryItemsHandler` 与详情 Handler，调用 `mobileEnsureStateLoaded` 后合并：
   - 当前 owner 的 `mobileDocuments.drafts`；
   - 当前 owner 的会议录音，过滤 `uploading`、`finalizing` 和不存在有效完成音频的记录；
   - 已过期音频若存在派生产物，仍作为“结果可用、音频不可用”的历史条目返回；无派生产物的过期条目默认不显示。
3. 给 `mobileMeetingRecordingPayload` 增加可选的 `audio_download_url`，并保持已有响应字段兼容；统一库 API 以更稳定的 `MobileLibraryItem` 返回。
4. 复用既有 `mobileStoreMeetingResultDocuments`，不重复实现转写稿/纪要写入。需要补充该函数的幂等保护：同一 `recording_id + result_kind` 在重试或进程恢复时更新/复用同一文稿，而非产生重复纪要。
5. 保持现有 realtime `meeting_recording` 广播；Desktop 订阅后只更新对应音频条目。若 Desktop 不具备该订阅，轮询刷新仍是可用兜底。
6. 将会议录音持久化从纯内存/状态文件的实现边界与统一读取模型隔离，为后续对象存储或队列替换保留空间。

### 5.3 权限、安全与成本

- 所有库列表、详情、音频下载、处理和删除都以 Viewer Token 鉴权，并按 `tenant_id + owner_id` 过滤。
- 音频下载绝不暴露本地绝对路径，不生成无鉴权的公开 URL；音频文件缺失返回稳定的业务错误，例如 `AUDIO_NOT_AVAILABLE`。
- `Range` 响应需限制在真实文件大小内，设置正确 `Content-Type`、`Content-Length` 和 `Content-Disposition`；下载文件名使用安全化的录音标题加受控扩展名。
- 同一录音仅允许一个运行中的处理任务，沿用现有 `NOT_READY` 冲突语义；Desktop 必须禁用重复点击。
- 仅在用户点击“生成会议纪要”后启动 ASR/纪要 Worker。Capabilities 接口显示不可用时，Desktop 隐藏或禁用该按钮，并说明 Hub 未配置相应处理能力。
- 操作日志至少记录：录音上传完成、处理发起、处理完成/失败、原始音频删除、录音删除；日志不写入音频内容和逐字稿正文。

## 6. Desktop 交互设计

### 6.1 总体布局

保留截图中“左侧列表 + 右侧预览”的成熟结构，避免将音频再拆出一个独立页面。列表支持类型图标和筛选，右侧根据条目类型呈现不同详情。

```text
Mobile 文稿库
  [搜索] [全部 | 文档 | 音频]                          [刷新]

  左：按更新时间排列的条目列表       右：所选条目详情
  ├─ 文档：标题、来源、摘要          ├─ 文档：现有 Markdown/原件预览
  └─ 音频：标题、时长、大小、状态    └─ 音频：播放器、状态、关联文稿、操作
```

音频使用与文档明显不同的“波形/声波”语义图标；不要用红色表达普通等待状态。选中状态和主要操作沿用产品现有钢蓝色系，失败和删除才使用红色。

### 6.2 音频详情区

默认详情按以下顺序组织，处理动作应高于辅助信息：

1. 标题、上传时间、时长、大小和“来自 Mobile 录音”来源说明。
2. 原生音频播放器：播放/暂停、进度、倍速、时长。播放器不存在可用音频时替换为明确的不可用说明。
3. 状态区：
   - 未处理：主按钮“生成会议纪要”，辅助文字“将先转写音频，再生成会议纪要”。
   - 处理中：进度与当前步骤；按钮替换为不可点击的状态按钮。
   - 完成：显示“逐字稿”“会议纪要”两个关联入口，主按钮不再出现。
   - 失败：展示简短的可行动原因，显示“重新生成会议纪要”。
4. 次级操作：下载原始音频、删除录音。删除必须使用二次确认。
5. 保留期提示：接近或超过 `retention_until` 时才显示，避免每个条目都产生噪声。

### 6.3 关键文案

| 场景 | 推荐文案 |
| --- | --- |
| 未处理按钮 | `生成会议纪要` |
| 未处理说明 | `将先转写音频，再生成可编辑的会议纪要。` |
| 转写中 | `正在转写录音…` |
| 纪要生成中 | `正在生成会议纪要…` |
| 处理完成 | `已生成逐字稿和会议纪要。` |
| 失败 | `处理未完成。原始音频仍保留，可检查配置后重试。` |
| 音频过期 | `原始音频已按保留策略清理；已生成的文稿仍可查看。` |
| 删除确认 | `删除此录音？原始音频将被删除，已生成的逐字稿和会议纪要会保留。` |

### 6.4 无障碍与边界状态

- 所有状态同时使用文字、图标和颜色；进度区域使用 `aria-live="polite"`。
- 音频播放器保留键盘操作与可见焦点，错误/处理状态不只依赖颜色。
- 长标题、文件名和错误信息截断显示但保留完整 Tooltip/可访问名称。
- 详情加载时使用骨架或“正在加载”，而非空白区域；列表为空时区分“尚无共享文稿”和“筛选无结果”。
- 点击关联文稿时在同一面板切换选中条目；提供“返回录音”轻量返回入口，避免用户迷失上下文。

## 7. Mobile 端简化策略

Mobile 端不需要把统一库放进“文档”主导航。保留录音入口及录音历史，并在每条录音上展示最小可用的信息：标题、时间、时长、上传/处理状态、是否已生成纪要。

允许的操作：

- 删除本地或 Hub 录音（处理中的录音遵守 Hub 的禁止删除规则）；
- 失败时重试上传；
- 处理完成后仅提供“查看会议纪要”的轻量跳转/只读查看（可选），不提供编辑、重新生成或复杂文稿操作。

这样既让用户确认录音“已经在 Hub”，也避免把手机从录音工具膨胀为第二个 Desktop 工作台。

## 8. 实施拆分与验收

### Phase 1：Hub 统一读取与安全音频访问

- 定义 `MobileLibraryItem` DTO 和统一列表/详情接口。
- 实现受鉴权保护、支持 Range 的音频流接口。
- 将完成上传的录音投影到统一库；保留既有文稿接口兼容性。
- 补齐 Hub 单元测试：租户/所有者隔离、列表排序与过滤、音频不存在、Range、状态过滤、过期保留、删除约束。

### Phase 2：Desktop 文稿库音频体验

- 扩展 `gui/mobile_documents.go` 与 Wails 绑定，读取统一库并调用会议录音处理/删除接口。
- 扩展 `gui/frontend/src/components/layout/MobileDocumentsPanel.tsx` 的列表项、详情面板、播放器、处理状态与关联文稿跳转。
- Desktop 处理完成后通过实时事件或短轮询刷新相应条目。
- 增加前端测试：按钮可见性、处理态禁用、失败重试、音频不可用、关联文稿跳转、删除确认语义。

### Phase 3：Mobile 轻量历史管理

- 在既有录音界面补齐已上传/已处理状态与删除、上传重试。
- 明确不引入文稿库复杂入口；如提供查看纪要，仅提供只读承接。
- 添加 Flutter 请求与状态测试，覆盖删除受限、上传恢复、处理结果展示。

### 验收矩阵

| 场景 | 预期结果 |
| --- | --- |
| 手机录制并上传一段音频 | 完成上传后 Desktop 刷新即可看到一条音频条目 |
| Desktop 点击生成会议纪要 | 状态依次显示转写、生成纪要，且不允许重复提交 |
| 成功完成 | 原始音频可播放/下载；可分别打开逐字稿和会议纪要 |
| ASR 或纪要 Worker 失败 | 显示可行动失败信息；在音频仍可用时可重试 |
| Hub 未配置 Worker | 不展示可执行的纪要动作，说明处理能力不可用 |
| 音频过期后 | 原始音频不可播放和不可重试；已有逐字稿/纪要仍可打开 |
| 删除录音 | 原始音频和录音记录删除，派生文稿保留且仍可访问 |
| 其他用户或其他租户访问 | 看不到条目，不能下载、处理或删除 |

## 9. 现有实现基础与变更锚点

当前实现已具备本方案的关键后端链路：会议录音通过 Hub 分片上传并异步处理；`minutes` 模式先转写再生成纪要；成功后会创建逐字稿和纪要文稿并记录 `TranscriptDraftID`、`MinutesDraftID`。因此首版的主要工作是补齐“音频作为可见库条目”的读取、下载和 Desktop 交互，而不是重新发明录音处理流水线。

建议优先修改和验证以下位置：

- `hub/internal/httpapi/mobile_meeting_recordings.go`：录音投影、音频下载、处理幂等性、状态与测试。
- `hub/internal/httpapi/mobile_handlers.go`：复用已有文稿读取与 Draft 生命周期；新增统一库 Handler 时保持旧文稿 API 兼容。
- `hub/internal/httpapi/router.go`：注册新统一库和音频流路由。
- `gui/mobile_documents.go`：Desktop 到 Hub 的统一库、处理、删除调用及 DTO。
- `gui/frontend/src/components/layout/MobileDocumentsPanel.tsx`：列表类型、音频详情、播放器与处理操作。
- `mobile/maclaw_mobile/lib/features/meeting_recording/`：只做轻量录音历史与文件管理增强。

## 10. 最终决策

采用“**Hub 会议录音是音频事实源，Mobile 文稿库是统一可见视图**”的边界。Desktop 在同一文稿库面板中承担音频发现、播放和会议纪要处理；Mobile 专注录制、上传和简单管理。这样可以最大化复用现有 ASR/纪要与文稿生成链路，避免复制音频和文稿数据，也让用户从手机录音到 Desktop 产出会议纪要的路径清晰、可追溯且成本可控。
