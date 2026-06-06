# 知识库图片支持 — 任务列表

## P0: 基础设施 + 核心格式

### T1: 数据模型 + 资产存储
- **描述**: 新增 `SourceKindImage`、DocumentNode type="image" 常量、Metadata 键常量；实现 `ImageAssetManager`（保存原图 + 生成缩略图/预览图）
- **涉及文件**: `corelib/knowledge/types.go`, `corelib/knowledge/image_assets.go` (新建)
- **依赖**: 无
- **优先级**: P0
- **工作量**: 半天

### T2: 图片描述接口 + CompositeImageDescriber
- **描述**: 定义 `ImageDescriber` 接口、`ImageHints`/`ImageDescription` 类型；实现 `CompositeImageDescriber`（决策逻辑：Vision verified → Vision，否则 → OCR + 上下文推断）
- **涉及文件**: `corelib/knowledge/image_describe.go` (新建)
- **依赖**: T1
- **优先级**: P0
- **工作量**: 半天

### T3: Vision LLM 客户端 + Health Check
- **描述**: 实现 `VisionDescriber`（OpenAI 兼容接口调用 + JSON 解析）；`HealthCheck` 方法（发送测试图片验证 API）；运行时失败降级（清除 verified 标记）
- **涉及文件**: `corelib/knowledge/vision.go` (新建)
- **依赖**: T2
- **优先级**: P0
- **工作量**: 半天

### T4: OCR 集成
- **描述**: 在 knowledge 包中集成 `browser.RapidOCRSidecar`——读取图片 → base64 → `Recognize()` → 拼接 OCR 文字；处理 OCR 不可用时的降级（只用上下文推断）
- **涉及文件**: `corelib/knowledge/image_describe.go`
- **依赖**: T2
- **优先级**: P0
- **工作量**: 2小时

### T5: DOCX 内嵌图片提取
- **描述**: 解析 `word/_rels/document.xml.rels` 获取 rId→media 映射；解析 `document.xml` 中 `<w:drawing>` 定位图片段落位置；从 zip 提取图片 bytes；创建 image DocumentNode 带上下文 Metadata
- **涉及文件**: `corelib/knowledge/parse_docx_images.go` (新建), `corelib/knowledge/parse.go` (调用入口)
- **依赖**: T1
- **优先级**: P0
- **工作量**: 1天

### T6: PPTX 内嵌图片提取
- **描述**: 遍历 `ppt/slides/slideN.xml` 解析 `<p:pic>` 元素；从 `ppt/_rels/slideN.xml.rels` 获取图片路径；从 zip 提取图片；创建 image DocumentNode 关联到幻灯片编号和标题
- **涉及文件**: `corelib/knowledge/parse_pptx_images.go` (新建), `corelib/knowledge/parse.go` (调用入口)
- **依赖**: T1
- **优先级**: P0
- **工作量**: 1天

### T7: PDF 内嵌图片提取
- **描述**: 引入 `pdfcpu` 依赖；调用 `api.ExtractImagesFile` 提取图片到临时目录；按页码关联到 textNode；过滤小图（<50x50px）；创建 image DocumentNode
- **涉及文件**: `corelib/knowledge/parse_pdf_images.go` (新建), `corelib/knowledge/parse.go` (调用入口), `go.mod`
- **依赖**: T1
- **优先级**: P0
- **工作量**: 1天

### T8: 导入管线集成（内嵌图片）
- **描述**: 在 `importSingleFile` 中，文档解析后调用 extractXxxImages → ImageAssetManager.SaveImage → ImageDescriber.Describe → 合并 imageNodes 到 allNodes → 正常 distill+index
- **涉及文件**: `corelib/knowledge/store.go`
- **依赖**: T2, T3, T4, T5, T6, T7
- **优先级**: P0
- **工作量**: 半天

### T9: 独立图片文件导入 + 两遍扫描关联
- **描述**: `DefaultIncludeExts` 新增图片扩展名（受 `include_images` 配置控制）；实现 `BuildImageReferenceMap`（扫描 Markdown/HTML 图片引用）；`ScanDirectory` 分两遍：先文本后图片；图片 Source 创建 + 描述生成
- **涉及文件**: `corelib/knowledge/scan_images.go` (新建), `corelib/knowledge/scan.go`, `corelib/knowledge/store.go`
- **依赖**: T8
- **优先级**: P0
- **工作量**: 1天

### T10: 配置项 + TUI CLI 支持
- **描述**: `config.json` 新增 `knowledge_vision_llm` 和 `knowledge_include_images` 字段；TUI CLI `knowledge import` 新增 `--include-images` 参数；`knowledge status` 显示图片统计
- **涉及文件**: `corelib/app_config.go`, `tui/commands/knowledge.go`, `tui/app.go`
- **依赖**: T9
- **优先级**: P0
- **工作量**: 半天

---

## P1: 完善 + 检索增强 + GUI

### T11: .doc OLE2 图片提取
- **描述**: 解析 OLE2 compound file 的 sector chain；定位 Pictures stream；按 blob header 拆分图片（识别 PNG/JPEG/EMF/WMF magic bytes）；EMF/WMF 标记为矢量不做 OCR；关联到文档级
- **涉及文件**: `corelib/knowledge/parse_doc_images.go` (新建)
- **依赖**: T8
- **优先级**: P1
- **工作量**: 1.5天

### T12: Agent 搜索结果图片信息
- **描述**: `knowledge_search` 工具返回结果中，图片 Card 标注 `[图片]` 前缀 + 原图路径 + OCR 文字 + 关联文档信息；`knowledge_context_pack` 包含图片 Card
- **涉及文件**: `corelib/knowledge/text.go`, `tui/knowledge_tools.go`
- **依赖**: T9
- **优先级**: P1
- **工作量**: 半天

### T13: 自动召回增强
- **描述**: `appendKnowledgeAutoRecall` 中图片 Card 结果标注 `[图片]` 前缀和原图路径；提示 Agent 可用 `send_file` 发送
- **涉及文件**: `tui/knowledge_autorecall.go`, `corelib/agentservice/knowledge_integration.go`
- **依赖**: T12
- **优先级**: P1
- **工作量**: 2小时

### T14: GUI 知识库列表缩略图
- **描述**: Source 列表中 kind=image 的行显示 thumb_120 缩略图；文档 Source 显示内嵌图片数量；图片类型图标
- **涉及文件**: `gui/frontend/` 相关组件
- **依赖**: T9
- **优先级**: P1
- **工作量**: 1天

### T15: GUI Source 详情页图片预览
- **描述**: 图片 Source 详情展示 preview_480 + 描述 + OCR 文字 + 关联文档链接；文档 Source 详情中 Node 树显示 image 节点缩略图
- **涉及文件**: `gui/frontend/` 相关组件
- **依赖**: T14
- **优先级**: P1
- **工作量**: 1天

### T16: GUI Vision LLM 配置面板
- **描述**: 知识库设置页新增 Vision LLM 配置区：BaseURL/APIKey/Model 输入框 + "测试连接"按钮（调用 HealthCheck）+ 状态显示（verified/未验证/失败）
- **涉及文件**: `gui/frontend/` 相关组件, `gui/app_wails_bindings.go`（暴露 HealthCheck 方法）
- **依赖**: T3
- **优先级**: P1
- **工作量**: 半天

### T17: 批量处理限流 + 异步后处理
- **描述**: 导入超过 50 张图片时，Vision 描述改为异步后处理（先 OCR 完成导入，后台 goroutine 逐张跑 Vision 更新 Card）；并发控制 semaphore；rate limiter
- **涉及文件**: `corelib/knowledge/store.go`, `corelib/knowledge/image_describe.go`
- **依赖**: T8
- **优先级**: P1
- **工作量**: 半天

---

## P2: 服务端 + 维护 + 扩展

### T18: maclawsrv HTTP API
- **描述**: 新增 thumbnail/image/upload 三个端点；图片上传走 multipart/form-data；返回标准 JSON 响应
- **涉及文件**: `MaClawSrv/http_knowledge.go`
- **依赖**: T9
- **优先级**: P2
- **工作量**: 1天

### T19: maclawsrv 用户知识库页面图片展示
- **描述**: 前端列表中图片 Source 加载缩略图；导入 UI 支持图片上传
- **涉及文件**: `MaClawSrv/` 前端模板
- **依赖**: T18
- **优先级**: P2
- **工作量**: 1天

### T20: Doctor 图片完整性检查
- **描述**: 新增诊断项：原图文件存在性、缩略图缺失自动重生成、Vision 描述为空标记、孤立资产目录清理
- **涉及文件**: `corelib/knowledge/doctor.go`
- **依赖**: T9
- **优先级**: P2
- **工作量**: 半天

### T21: .xls 图片提取
- **描述**: 从 OLE2 Drawing Group Records 提取图片（复杂度极高，实用价值低）
- **涉及文件**: `corelib/knowledge/parse_xls_images.go` (新建)
- **依赖**: T11
- **优先级**: P2
- **工作量**: 2天

### T22: capabilities.go 更新
- **描述**: `Capabilities()` 新增 `SourceKindImage` 格式声明；更新现有格式的 Notes 说明"支持内嵌图片提取"
- **涉及文件**: `corelib/knowledge/capabilities.go`
- **依赖**: T9
- **优先级**: P2
- **工作量**: 1小时

---

## 验收标准

- [ ] 导入含图片的 .docx → Source 详情中可见内嵌图片缩略图和 OCR 文字
- [ ] 导入含图片的 .pptx → 每张幻灯片的图片被关联到正确的 slide 编号
- [ ] 导入含图片的 .pdf → 图片被关联到正确的页码
- [ ] 目录导入（含 --include-images）→ 独立图片被关联到引用它的 .md 文件
- [ ] `knowledge_search("架构图")` → 返回图片 Card，包含原图路径
- [ ] 配置 Vision LLM + 测试通过 → 图片描述走 Vision（高质量语义描述）
- [ ] Vision 未配置 → 图片描述走 OCR + 上下文（基础可用）
- [ ] Vision 运行时失败 → 自动降级到 OCR，清除 verified 标记
- [ ] GUI 知识库列表 → 图片 Source 显示缩略图
- [ ] 删除图片 Source → 对应的 knowledge_assets 目录被清理
