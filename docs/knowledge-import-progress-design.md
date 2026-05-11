# 知识库导入进度与状态设计

## 现状分析

### 已有基础设施

**后端**：
- `KnowledgeStartImportDirectory()` 启动异步 goroutine，返回 `KnowledgeImportJob`（含 job ID）
- `SetImportProgressCallback` 在每个文件处理完后回调，更新 `knowledgeImportJobs` sync.Map
- `DirectoryImportResult` 已有完整的进度字段：`TotalFiles`、`ProcessedFiles`、`ImportedFiles`、`SkippedFiles`、`FailedFiles`、`CurrentFile`、`Status`
- `KnowledgeImportJobStatus(id)` 供前端轮询

**前端**：
- `KnowledgeSettingsPanel.tsx` 的 Ingest tab 有"导入目录"和"导入文件"两个区块
- `importJob` state + `useEffect` 1500ms 轮询 `KnowledgeImportJobStatus`
- `ImportJobSummary` 组件显示 processed/imported/skipped/failed 四个计数器
- 项目已有 `EventsEmit` + `EventsOn` 实时事件推送模式（ASR 下载、embedding 下载、工具安装等）
- 项目已有 Toast 通知系统（`show-toast` 事件）

### 当前问题

1. **无进度条**：只有文本计数器，用户无法直观感知完成百分比
2. **无百分比指示**：没有 `processed / total` 的比例显示
3. **离开页面后无通知**：用户切换到其他 tab 后，导入完成/失败无任何提示
4. **状态不够醒目**：running/completed/failed 状态只是文本，没有颜色/图标区分
5. **文件导入无进度**：`ImportFiles`（选择文件导入）是同步调用，大量文件时界面卡死无反馈
6. **错误详情不可见**：`failed_files` 只有计数，用户不知道哪些文件失败、为什么失败
7. **导入操作嵌在设置面板中**：作为知识库的核心操作，导入功能被挤在 Ingest tab 的一个小区块里，不够突出

---

## 设计方案：弹出式导入对话框（Import Dialog）

### 设计理念

导入是知识库的**主要操作入口**，应该有独立的、大气的交互空间。采用**弹出式对话框（Modal Dialog）**承载整个导入流程：

- **聚焦感**：Modal 遮罩让用户专注于导入操作，不被其他 UI 干扰
- **空间充裕**：对话框宽度 640px+，有足够空间展示进度条、文件列表、统计信息
- **状态完整**：从选择文件 → 配置参数 → 导入进行中 → 完成/失败，全流程在一个对话框内完成
- **可最小化**：导入进行中时可以最小化对话框继续其他操作，通过 Toast + 浮动指示器感知进度

---

### 一、交互流程（三步式对话框）

#### Step 1: 选择导入源

对话框打开后的首屏，大按钮选择导入方式：

```
┌──────────────────────────────────────────────────────────────┐
│  ╳                                                            │
│                                                              │
│         📥  导入知识到知识库                                    │
│                                                              │
│  ┌─────────────────────┐    ┌─────────────────────┐          │
│  │                     │    │                     │          │
│  │    📁               │    │    📄               │          │
│  │                     │    │                     │          │
│  │   选择目录           │    │   选择文件           │          │
│  │                     │    │                     │          │
│  │  扫描整个目录下的     │    │  选择一个或多个       │          │
│  │  文档文件            │    │  文档文件            │          │
│  │                     │    │                     │          │
│  └─────────────────────┘    └─────────────────────┘          │
│                                                              │
│  支持格式：PDF, DOCX, XLSX, CSV, Markdown, TXT               │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

点击后：
- "选择目录" → 弹出系统目录选择器 → 进入 Step 2
- "选择文件" → 弹出系统文件选择器（多选）→ 进入 Step 2

#### Step 2: 配置与预检

选择完文件/目录后，显示预检结果和配置选项：

```
┌──────────────────────────────────────────────────────────────┐
│  ← 返回                                          ╳           │
│                                                              │
│  📁 D:\Documents\研究论文                                     │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  📊 预检结果                                            │  │
│  │                                                        │  │
│  │  发现 47 个文件                                         │  │
│  │  ├ PDF: 23 个                                          │  │
│  │  ├ DOCX: 12 个                                         │  │
│  │  ├ Markdown: 8 个                                      │  │
│  │  └ 其他: 4 个                                          │  │
│  │                                                        │  │
│  │  预计大小: 156 MB                                       │  │
│  │  ⚠️ 2 个文件将被跳过（超过大小限制）                      │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  ─── 导入选项 ───                                            │
│                                                              │
│  保存范围:  [项目 ▾]     主题提示:  [____________]            │
│  标签:      [____________]                                   │
│  ☑ 递归子目录   ☑ 自动标签   ☐ 仅预检                        │
│                                                              │
│  ▸ 高级选项（扩展名过滤、排除规则、文件大小限制）               │
│                                                              │
│              [取消]                    [开始导入 →]            │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

#### Step 3: 导入进行中 + 完成

点击"开始导入"后，对话框切换为进度视图：

```
┌──────────────────────────────────────────────────────────────┐
│                                              [最小化 ▁]  ╳   │
│                                                              │
│         🔄  正在导入知识...                                    │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  ████████████████████████░░░░░░░░░░░░  62%             │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  📄 当前: research/transformer-architecture-2024.pdf          │
│                                                              │
│  ┌──────────┬──────────┬──────────┬──────────┐              │
│  │  ✅ 29   │  ⏭️ 3    │  ❌ 1    │  📁 47   │              │
│  │  已导入   │  已跳过   │  失败    │  总计    │              │
│  └──────────┴──────────┴──────────┴──────────┘              │
│                                                              │
│  ─── 处理日志 ───                                            │
│  ✅ papers/attention-is-all-you-need.pdf                     │
│  ✅ papers/bert-pretraining.pdf                              │
│  ⏭️ papers/draft-v1.pdf (重复内容)                           │
│  ❌ papers/corrupted.pdf (文件损坏，无法解析)                  │
│  ✅ papers/gpt4-technical-report.pdf                         │
│  ...                                                         │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

导入完成后：

```
┌──────────────────────────────────────────────────────────────┐
│                                                          ╳   │
│                                                              │
│         ✅  导入完成                                          │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  ████████████████████████████████████████  100%        │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  ┌──────────┬──────────┬──────────┬──────────┐              │
│  │  ✅ 43   │  ⏭️ 3    │  ❌ 1    │  📁 47   │              │
│  │  已导入   │  已跳过   │  失败    │  总计    │              │
│  └──────────┴──────────┴──────────┴──────────┘              │
│                                                              │
│  ⚠️ 1 个文件导入失败:                                         │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  ❌ papers/corrupted.pdf                               │  │
│  │     错误: 文件损坏，PDF 解析失败                         │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│              [查看知识库]              [关闭]                  │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

---

### 二、最小化模式 + 全局感知

用户在 Step 3 点击"最小化"后，对话框收起为设置面板底部的**浮动进度条**：

```
┌─────────────────────────────────────────────────────────────┐
│  [设置面板其他内容...]                                        │
│                                                              │
│  ...                                                         │
│                                                              │
├─────────────────────────────────────────────────────────────┤
│  📥 导入中 62% ████████░░░░ 29/47 文件    [展开]             │
└─────────────────────────────────────────────────────────────┘
```

点击"展开"恢复完整对话框。

同时，无论对话框是否打开：
- **Toast 通知**：导入完成/失败时全局 Toast
- **知识库 Tab 徽章**：Ingest tab 标签上显示蓝色脉冲点

---

### 三、后端改动

#### 3.1 新增 EventsEmit 进度推送（`gui/app_knowledge.go`）

在 `KnowledgeStartImportDirectory` 的 goroutine 中（有 `a` 引用），通过 `SetImportProgressCallback` 同时推送进度事件：

```go
go func(jobID string, req knowledge.DirectoryImportRequest) {
    store, err := a.openKnowledgeStore()
    if err != nil { /* ... */ }
    defer store.Close()
    
    store.SetImportProgressCallback(func(progress knowledge.DirectoryImportResult) {
        updateKnowledgeImportJobProgress(jobID, progress)
        if a.ctx != nil {
            a.emitKnowledgeImportProgress(jobID, progress)
        }
    })
    
    result, err := store.ImportDirectory(a.knowledgeContext(), req)
    finishKnowledgeImportJob(jobID, result, err)
    
    // 完成通知（在 goroutine 内部，有 a 引用）
    a.emitKnowledgeImportDone(jobID, result, err)
}(job.ID, req)
```

新增 `emitKnowledgeImportProgress` 方法，按 jobID 隔离节流（支持未来多任务并行）：

```go
// App 结构体新增字段：
// knowledgeProgressThrottle sync.Map  // map[string]time.Time，key=jobID

func (a *App) emitKnowledgeImportProgress(jobID string, progress knowledge.DirectoryImportResult) {
    now := time.Now()
    // 节流：500ms 内最多推送一次，但 completed/failed 状态立即推送
    isFinal := progress.Status == knowledge.ImportStatusCompleted || progress.Status == knowledge.ImportStatusFailed
    if !isFinal {
        if last, ok := a.knowledgeProgressThrottle.Load(jobID); ok {
            if now.Sub(last.(time.Time)) < 500*time.Millisecond {
                return
            }
        }
    }
    a.knowledgeProgressThrottle.Store(jobID, now)
    
    runtime.EventsEmit(a.ctx, "knowledge:import-progress", map[string]interface{}{
        "job_id":           jobID,
        "status":           progress.Status,
        "total_files":      progress.TotalFiles,
        "processed_files":  progress.ProcessedFiles,
        "imported_files":   progress.ImportedFiles,
        "skipped_files":    progress.SkippedFiles,
        "failed_files":     progress.FailedFiles,
        "current_file":     progress.CurrentFile,
        // 逐文件日志数据（前端累积到 log entries）
        // 注意：仅在非 final 状态时有值。final 事件由 emitKnowledgeImportDone 单独处理。
        "last_item_path":   progress.LastItemPath,   // 刚处理完的文件路径（区别于 current_file 是下一个）
        "last_item_status": progress.LastItemStatus,  // "imported" | "skipped" | "failed"
        "last_item_reason": progress.LastItemReason,  // 跳过/失败原因
    })
    
    if isFinal {
        a.knowledgeProgressThrottle.Delete(jobID)
    }
}
```

**注意**：`DirectoryImportResult` 需新增 `LastItemPath`、`LastItemStatus` 和 `LastItemReason` 字段（见 3.4 节），在 `importScannedItems` 的 `markImportItemProcessed` 中填充。`LastItemPath` 是刚处理完的文件路径（区别于 `CurrentFile` 是即将处理的下一个文件）。节流可能跳过中间文件的事件，但前端日志只是辅助信息，不要求 100% 完整——最终统计数字（imported/skipped/failed）始终准确。

#### 3.2 导入完成/失败时推送 Toast 事件

新增 `emitKnowledgeImportDone` 方法（在 goroutine 内部调用，有 `a` 引用）：

```go
func (a *App) emitKnowledgeImportDone(jobID string, result knowledge.DirectoryImportResult, err error) {
    if a.ctx == nil {
        return
    }
    if err != nil || result.Status == knowledge.ImportStatusFailed {
        runtime.EventsEmit(a.ctx, "show-toast", map[string]interface{}{
            "message":  fmt.Sprintf("知识库导入失败：%s", truncateError(err)),
            "type":     "error",
            "duration": 5000,
        })
    } else {
        msg := fmt.Sprintf("知识库导入完成：%d 个文件已导入", result.ImportedFiles)
        if result.FailedFiles > 0 {
            msg = fmt.Sprintf("知识库导入完成：%d 个文件已导入，%d 个失败", result.ImportedFiles, result.FailedFiles)
        }
        runtime.EventsEmit(a.ctx, "show-toast", map[string]interface{}{
            "message":  msg,
            "type":     "success",
            "duration": 4000,
        })
    }
}
```

**设计要点**：Toast 推送在 goroutine 内部（有 `a` 引用），不在包级函数 `finishKnowledgeImportJob` 中。`finishKnowledgeImportJob` 只负责更新 sync.Map 状态，不做 IO/事件推送。

#### 3.3 `ImportFiles` 改为异步

新增 `KnowledgeStartImportFiles` 方法，与 `KnowledgeStartImportDirectory` 对称，让文件导入也有进度反馈。

#### 3.4 `DirectoryImportResult` 新增字段

```go
type DirectoryImportResult struct {
    // ... 现有字段 ...
    FailedItems    []ImportFailedItem `json:"failed_items,omitempty"`    // 最多前 20 条
    LastItemPath   string             `json:"last_item_path,omitempty"`  // 刚处理完的文件路径
    LastItemStatus string             `json:"last_item_status,omitempty"` // "imported" | "skipped" | "failed"
    LastItemReason string             `json:"last_item_reason,omitempty"` // 跳过/失败原因（如"重复内容"、"文件损坏"）
    ExtCounts      map[string]int     `json:"ext_counts,omitempty"`      // 按扩展名分组的文件数量（预检时填充）
}

type ImportFailedItem struct {
    FilePath string `json:"file_path"`
    Error    string `json:"error"`
}
```

**`LastItemPath` / `LastItemStatus` / `LastItemReason`**：在 `importScannedItems` 的 `markImportItemProcessed` 中，根据 item 的处理结果填充到 `result` 中，供 `emitImportProgress` 推送给前端。`LastItemPath` 使用 `item.RelativePath`（相对路径，比绝对路径更简洁）。前端累积这些逐文件状态到处理日志数组。注意节流会跳过部分事件，日志不保证 100% 完整，但统计数字始终准确。

**`ExtCounts`**：在 `ScanDirectory` / `ScanFiles`（dry-run 预检）中统计各扩展名的文件数量，供 Step 2 展示文件类型分布。

**`FailedItems`**：在 `importScannedItems` 中，文件导入失败时追加到列表（上限 20 条，避免内存膨胀）。

#### 3.5 预检接口（复用现有）

Step 2 的预检直接调用已有的 `KnowledgeScanDirectory`（设置 `DryRun=true`）和 `KnowledgeScanFiles`，不需要新增接口。前端在选择目录/文件后调用：

```tsx
// 目录预检
const scanResult = await KnowledgeScanDirectory({ ...importPayload(), dry_run: true });
// 文件预检
const scanResult = await KnowledgeScanFiles({ ...importPayload(), dry_run: true }, selectedFiles);
```

预检结果中的 `ExtCounts` 字段（3.4 节新增）提供文件类型分布数据。

---

### 四、前端改动

#### 4.1 新增 `KnowledgeImportDialog` 组件

独立的 Modal 组件，管理三步流程的状态机：

```tsx
type ImportDialogStep = 'choose' | 'configure' | 'progress' | 'done';

type ImportDialogProps = {
    open: boolean;
    onClose: () => void;
    onMinimize: (job: ImportJob, logEntries: LogEntry[]) => void; // 通知父组件进入最小化
    initialJob?: ImportJob | null;      // 从最小化恢复时传入的 job 数据
    initialLog?: LogEntry[];            // 从最小化恢复时传入的日志数据
    t: TFunc;
    lang: string;
};

function KnowledgeImportDialog({ open, onClose, onMinimize, t, lang }: ImportDialogProps) {
    const [step, setStep] = useState<ImportDialogStep>('choose');
    const [importMode, setImportMode] = useState<'directory' | 'files'>('directory');
    const [selectedPath, setSelectedPath] = useState('');
    const [selectedFiles, setSelectedFiles] = useState<string[]>([]);
    const [scanResult, setScanResult] = useState<ImportResult | null>(null);
    const [job, setJob] = useState<ImportJob | null>(null);
    const [logEntries, setLogEntries] = useState<LogEntry[]>([]);
    const [config, setConfig] = useState({ /* 导入配置 */ });
    
    // Step 1 → 选择文件/目录后自动进入 Step 2 并触发预检
    // Step 2 → 点击"开始导入"进入 Step 3
    // Step 3 → 导入完成后 step 变为 'done'
    
    // EventsOn 实时监听进度 + 累积日志
    useEffect(() => {
        if (!job?.id) return;
        const cleanup = EventsOn('knowledge:import-progress', (data: any) => {
            if (data.job_id !== job.id) return;
            setJob(prev => prev ? { ...prev, status: data.status, result: { ...prev.result, ...data } } : prev);
            // 累积处理日志
            if (data.last_item_path && data.last_item_status) {
                setLogEntries(prev => [...prev, {
                    relativePath: data.last_item_path,
                    status: data.last_item_status,
                    reason: data.last_item_reason || '',
                }]);
            }
            // 完成时切换到 done 步骤
            if (data.status === 'completed' || data.status === 'failed') {
                setStep('done');
            }
        });
        return () => { cleanup(); };
    }, [job?.id]);
    
    // 关闭/最小化逻辑
    const handleClose = () => {
        if (step === 'progress' && job?.status === 'running') {
            // 导入进行中 → 通知父组件最小化（传递 job 和 log 数据）
            onMinimize(job, logEntries);
            return;
        }
        // done/choose/configure 状态 → 直接关闭，重置状态
        setStep('choose');
        setJob(null);
        setLogEntries([]);
        setScanResult(null);
        onClose();
    };
    
    if (!open) return null;
    
    return (
        <Modal open={true} onClose={handleClose} width={640}>
            {step === 'choose' && <ChooseSourceStep ... />}
            {step === 'configure' && <ConfigureStep ... />}
            {(step === 'progress' || step === 'done') && <ProgressStep ... />}
        </Modal>
    );
}
```

**父组件使用**：

```tsx
// KnowledgeSettingsPanel 中
const [showImportDialog, setShowImportDialog] = useState(false);
const [minimizedJob, setMinimizedJob] = useState<{ job: ImportJob; log: LogEntry[] } | null>(null);

// 最小化回调：关闭对话框，保存 job 数据到父组件
const handleMinimize = (job: ImportJob, log: LogEntry[]) => {
    setShowImportDialog(false);
    setMinimizedJob({ job, log });
};

// 展开回调：打开对话框，恢复 job 数据
const handleExpand = () => {
    setShowImportDialog(true);
    setMinimizedJob(null);
};

// 浮动条：根据 minimizedJob 渲染
{minimizedJob && (
    <FloatingProgressBar job={minimizedJob.job} onClick={handleExpand} />
)}
```

**关键设计**：
- 最小化时对话框**关闭**（`open=false`），job 数据**提升到父组件** state 中。浮动条从父组件 state 渲染。
- 展开时对话框**重新打开**，通过 props 将 job 数据传回对话框恢复状态。
- 这避免了"组件 return null 但 hooks 仍运行"的隐晦行为——关闭就是关闭，数据在父组件中安全保存。
- 浮动条自己监听 `EventsOn` 更新 `minimizedJob` 中的进度数据。
- **父组件约束**：设置面板关闭时，如果有 `minimizedJob`，浮动条提升到 App 级别 fixed position 层渲染。

#### 4.2 触发入口

在 KnowledgeSettingsPanel 的 Ingest tab 中，用一个醒目的大按钮替代现有的分散 UI：

```tsx
{activeTab === 'ingest' && (
    <div style={ingestHeroStyle}>
        <button style={importHeroButtonStyle} onClick={() => setShowImportDialog(true)}>
            📥 {t('Import Documents', '导入文档')}
        </button>
        <p style={subtitleStyle}>
            {t('Import files or directories into your knowledge base', 
               '将文件或目录导入到知识库中')}
        </p>
    </div>
)}

<KnowledgeImportDialog 
    open={showImportDialog} 
    onClose={() => setShowImportDialog(false)} 
    t={t} lang={lang} 
/>
```

#### 4.3 最小化浮动条

对话框最小化后，在设置面板底部固定显示浮动进度条：

```tsx
{minimizedImport && (
    <div style={floatingBarStyle} onClick={() => setShowImportDialog(true)}>
        📥 {t('Importing', '导入中')} {percent}% 
        <div style={miniProgressStyle}>
            <div style={{ width: `${percent}%`, ...miniProgressFillStyle }} />
        </div>
        {processed}/{total} {t('files', '文件')}
        <button style={expandButtonStyle}>{t('Expand', '展开')}</button>
    </div>
)}
```

#### 4.4 处理日志（实时滚动）

Step 3 的处理日志区域，从 `EventsOn` 事件中的 `last_item_*` 字段累积，自动滚动到底部：

```tsx
type LogEntry = {
    relativePath: string;
    status: 'imported' | 'skipped' | 'failed';
    reason: string;
};

function ImportLog({ entries, maxVisible = 200 }: { entries: LogEntry[]; maxVisible?: number }) {
    const bottomRef = useRef<HTMLDivElement>(null);
    useEffect(() => { bottomRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [entries.length]);
    
    // 只渲染最近 maxVisible 条，避免大量 DOM 节点
    const visible = entries.length > maxVisible ? entries.slice(-maxVisible) : entries;
    const hidden = entries.length - visible.length;
    
    return (
        <div style={logContainerStyle}>
            {hidden > 0 && <div style={logHiddenStyle}>... 还有 {hidden} 条记录</div>}
            {visible.map((entry, i) => (
                <div key={i} style={logEntryStyle}>
                    {entry.status === 'imported' && '✅'}
                    {entry.status === 'skipped' && '⏭️'}
                    {entry.status === 'failed' && '❌'}
                    {' '}{entry.relativePath}
                    {entry.reason && <span style={logReasonStyle}>({entry.reason})</span>}
                </div>
            ))}
            <div ref={bottomRef} />
        </div>
    );
}
```

**数据来源**：`EventsOn('knowledge:import-progress')` 事件中的 `last_item_path` + `last_item_status` + `last_item_reason` 字段（见 3.1 节），前端在 `useEffect` 中累积到 `logEntries` state 数组。

**性能保护**：只渲染最近 200 条 DOM 节点。导入 1000+ 文件时不会因为 DOM 过多导致卡顿。

---

### 五、状态流转

```
[对话框关闭]
    │ 用户点击"导入文档"按钮
    ▼
[Step 1: choose] ─── 选择目录/文件 ───→ [Step 2: configure]
                                              │
                                              │ 点击"开始导入"
                                              ▼
                                        [Step 3: progress]
                                              │
                                    ┌─────────┼─────────┐
                                    │         │         │
                                    ▼         ▼         ▼
                              [最小化]   [完成/done]  [失败/done]
                                 │         │            │
                                 │         ▼            ├──→ [重试] → 回到 Step 3（重新调用 StartImport）
                                 │    [关闭/查看]       │
                                 │                     ▼
                                 │                  [关闭]
                                 ▼
                           [浮动进度条]
                                 │ 点击展开
                                 ▼
                           [Step 3: progress]（对话框重新打开，恢复 job 数据）
```

---

### 六、交互细节

| 场景 | 行为 |
|------|------|
| 用户点击"导入文档" | 弹出对话框 Step 1，两个大卡片选择导入方式 |
| 选择目录后 | 自动触发预检（dry-run scan），显示文件统计 |
| 预检中 | "开始导入"按钮 disabled，显示"扫描中..." |
| 预检完成 | 显示文件数量/类型/大小，"开始导入"按钮可用 |
| 点击"开始导入" | 切换到 Step 3 进度视图，进度条从 0% 开始 |
| 导入进行中 | 进度条实时更新 + 当前文件名 + 统计数字 + 处理日志 |
| 点击"最小化" | 对话框收起为底部浮动条，继续显示进度 |
| 点击浮动条"展开" | 恢复完整对话框 |
| 导入完成 | 进度条 100%，按钮变为"关闭"/"查看知识库"。颜色区分：全部成功=绿色 ✅，部分失败=橙色 ⚠️，全部失败=红色 ❌ |
| 有失败文件 | 完成界面显示失败文件列表（路径 + 错误原因） |
| 点击"查看知识库" | 关闭对话框，切换到 Sources tab |
| 用户在导入中关闭对话框 | 自动最小化为浮动进度条（不弹确认框，减少摩擦） |
| 用户在任何页面 | 导入完成/失败时 Toast 通知 |

---

### 七、实现优先级

| 优先级 | 改动 | 工作量 |
|--------|------|--------|
| P0 | `KnowledgeImportDialog` 组件（三步流程） | 前端 ~300 行 |
| P0 | 进度条 + 百分比 + 状态颜色 + 统计卡片 | 前端 ~100 行 |
| P0 | EventsEmit 实时推送（带节流） | 后端 ~30 行 |
| P0 | 导入完成/失败 Toast 通知 | 后端 ~15 行 |
| P1 | 处理日志（实时滚动） | 前端 ~60 行 + 后端 ~10 行 |
| P1 | 最小化浮动进度条 | 前端 ~50 行 |
| P1 | 失败文件详情列表 | 前端 ~40 行 + 后端 ~20 行 |
| P1 | 预检结果展示（文件类型分布） | 前端 ~40 行 |
| P2 | `ImportFiles` 改为异步 | 后端 ~40 行 |
| P2 | "高级选项"折叠面板 | 前端 ~30 行 |

---

### 八、不做的事情（本期）

- **预估剩余时间**：文件大小差异极大，预估不准确反而误导。百分比 + 当前文件名已足够。
- **取消导入**：需要 context cancel 贯穿整个 pipeline，改动面大。可作为后续迭代。对话框 Step 3 预留"取消"按钮位置（disabled + tooltip "即将支持"），架构上 goroutine 已有 `a.knowledgeContext()` 可以扩展为 per-job context。
- **拖拽导入**：Wails 的拖拽支持有限，且系统文件选择器已经足够好用。
- **多任务并行**：当前单任务。如果未来需要，扩展为 `importJobs[]` 数组 + 对话框内 tab 切换。节流已按 jobID 隔离（见 3.1），后端无需改动。
- **导入历史记录**：已有 `KnowledgeListImportBatches` 接口，可在 Sources tab 中查看。对话框不重复展示。

---

### 九、与现有 UI 的关系

- **Ingest tab 保留**：现有的"保存文本"和"保存 URL"功能保留在 Ingest tab 中（它们是轻量操作，不需要对话框）
- **"导入文件"和"导入目录"区块**：替换为一个醒目的"📥 导入文档"大按钮，点击后弹出对话框
- **`ImportJobSummary` 组件**：保留作为 fallback（对话框关闭后，如果有活跃 job，在 Ingest tab 中仍显示简要状态）
- **轮询逻辑**：保留 5s 轮询作为兜底，但主要依赖 EventsOn 实时推送
