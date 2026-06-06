# 知识库图片支持 — 技术设计文档

## 架构概览

```
导入管线（增强后）
─────────────────────────────────────────────────────────
文件/目录/URL
  │
  ├─ 文本文件 → ParseDocumentNodes() → 提取内嵌图片 ─┐
  │                                                    │
  └─ 图片文件 → 两遍扫描关联 ──────────────────────────┤
                                                       ▼
                                            ImageProcessor
                                            ┌─────────────────┐
                                            │ 1. 保存原图      │
                                            │ 2. 生成缩略图    │
                                            │ 3. 图片描述生成   │
                                            │    ├ Vision LLM  │
                                            │    └ OCR+上下文   │
                                            │ 4. 创建 Node+Card │
                                            └─────────────────┘
                                                       │
                                                       ▼
                                            FTS5 + 向量索引（正常文本管线）
```

## 模块设计

### 1. 图片描述策略（`corelib/knowledge/image_describe.go`）

```go
// ImageDescriber 统一图片描述接口
type ImageDescriber interface {
    Describe(ctx context.Context, imagePath string, hints ImageHints) (ImageDescription, error)
    Close()
}

type ImageHints struct {
    FileName      string   // 原始文件名
    ContextBefore string   // 前文（截断 200 rune）
    ContextAfter  string   // 后文（截断 200 rune）
    AltText       string   // Markdown ![alt] 或 OOXML alt 属性
    ParentTitle   string   // 所属章节/幻灯片/页面标题
    PageNumber    int      // 所在页码（PDF/PPTX）
    SourceTitle   string   // 所属文档标题
}

type ImageDescription struct {
    Title       string   // 短标题（"系统架构图"）
    Description string   // 详细描述（2-4句话）
    OCRText     string   // OCR 提取的文字内容
    Entities    []string // 识别到的实体
}

// CompositeImageDescriber 两层策略
type CompositeImageDescriber struct {
    vision  *VisionDescriber       // 可选，nil 或未验证时不用
    ocr     browser.OCRProvider    // RapidOCR sidecar
}

func (c *CompositeImageDescriber) Describe(ctx, path, hints) (ImageDescription, error) {
    // 决策：Vision 已配置且 verified → Vision LLM
    //       否则 → OCR + 上下文推断
    if c.vision != nil && c.vision.IsVerified() {
        desc, err := c.vision.Describe(ctx, path, hints)
        if err == nil {
            // Vision 成功，补充 OCR 文字（Vision 可能漏字）
            if c.ocr != nil {
                desc.OCRText = c.ocrImage(path)
            }
            return desc, nil
        }
        // Vision 运行时失败 → 清除 verified 标记，降级
        c.vision.ClearVerified()
        log.Printf("[knowledge-image] vision failed, falling back to OCR: %v", err)
    }
    
    // OCR + 上下文推断
    return c.describeWithOCR(ctx, path, hints)
}
```

### 2. Vision LLM（`corelib/knowledge/vision.go`）

```go
type VisionLLMConfig struct {
    Enabled    bool   `json:"enabled"`
    BaseURL    string `json:"base_url"`     // OpenAI 兼容 endpoint
    APIKey     string `json:"api_key"`
    Model      string `json:"model"`        // "gpt-4o-mini", "glm-4v-flash", "qwen-vl-plus"
    MaxTokens  int    `json:"max_tokens"`   // 默认 500
    TimeoutSec int    `json:"timeout_sec"`  // 默认 30
    Verified   bool   `json:"verified"`     // health check 通过标记
}

type VisionDescriber struct {
    cfg    *VisionLLMConfig
    client *http.Client
}

// HealthCheck 发送测试图片验证 API 可用性
// 配置保存时调用，通过后设 Verified=true
func (v *VisionDescriber) HealthCheck(ctx context.Context) error {
    // 生成 1x1 红色 PNG 测试图
    // 发送请求验证返回格式正确
    // 通过 → cfg.Verified = true
    // 失败 → 返回错误，cfg.Verified 不变
}

// IsVerified 运行时检查
func (v *VisionDescriber) IsVerified() bool {
    return v.cfg != nil && v.cfg.Enabled && v.cfg.Verified
}

// ClearVerified 运行时失败后降级
func (v *VisionDescriber) ClearVerified() {
    if v.cfg != nil {
        v.cfg.Verified = false
        // 持久化到 config.json
    }
}

// Describe 调用 Vision LLM
func (v *VisionDescriber) Describe(ctx, imagePath, hints) (ImageDescription, error) {
    // 读取图片 → base64
    // 构建 OpenAI 兼容请求（messages + image_url content block）
    // 解析 JSON 响应 → ImageDescription
}
```

### 3. 图片资产管理（`corelib/knowledge/image_assets.go`）

```go
const (
    ThumbSize   = 120  // 列表缩略图边长
    PreviewWidth = 480 // 详情预览宽度
)

// ImageAssetManager 管理图片资产的存储和生命周期
type ImageAssetManager struct {
    baseDir string  // ~/.maclaw/knowledge_assets/
}

// SaveImage 保存图片原文件 + 生成缩略图
// 返回资产目录路径（相对于 baseDir）
func (m *ImageAssetManager) SaveImage(sourceID, imagePath string) (*ImageAsset, error) {
    // 1. 创建目录 baseDir/{sourceID}/
    // 2. 复制原图为 original.{ext}
    // 3. 解码图片（Go image 标准库）
    // 4. 生成 thumb_120.jpg（Lanczos 缩放）
    // 5. 生成 preview_480.jpg（等比缩放）
    // 返回 ImageAsset{OriginalPath, ThumbPath, PreviewPath, Width, Height, Format, SizeBytes}
}

// DeleteAssets 删除 Source 的所有图片资产
func (m *ImageAssetManager) DeleteAssets(sourceID string) error {
    return os.RemoveAll(filepath.Join(m.baseDir, sourceID))
}

// RegenerateThumb 重新生成缺失的缩略图
func (m *ImageAssetManager) RegenerateThumb(sourceID string) error { ... }

type ImageAsset struct {
    OriginalPath string // 原图绝对路径
    ThumbPath    string // 缩略图绝对路径
    PreviewPath  string // 预览图绝对路径
    Width        int
    Height       int
    Format       string // "png", "jpeg", "gif", "bmp", "webp"
    SizeBytes    int64
}
```

### 4. 文档内嵌图片提取

#### 4.1 DOCX（`corelib/knowledge/parse_docx_images.go`）

```go
// extractDOCXImages 从 DOCX zip 中提取图片并关联到段落位置
func extractDOCXImages(source Source, zipPath string, textNodes []DocumentNode) ([]DocumentNode, error) {
    // 1. 打开 zip
    // 2. 解析 word/_rels/document.xml.rels → 建立 rId → media/imageN.ext 映射
    // 3. 解析 word/document.xml → 找 <w:drawing> 中的 r:embed 属性
    //    - 记录图片在段落序列中的位置（第 N 个段落之后）
    //    - 提取 <wp:docPr> 中的 descr/title 属性作为 alt text
    // 4. 对每张图片：
    //    - 从 zip 读取图片 bytes
    //    - 确定上下文：位置前后的 textNode 的 Text
    //    - 创建 ImageHints{ContextBefore, ContextAfter, AltText, ParentTitle}
    //    - 返回 DocumentNode{Type:"image", Metadata:{...}}
}
```

#### 4.2 PPTX（`corelib/knowledge/parse_pptx_images.go`）

```go
// extractPPTXImages 从 PPTX zip 中提取图片并关联到幻灯片
func extractPPTXImages(source Source, zipPath string, textNodes []DocumentNode) ([]DocumentNode, error) {
    // 1. 打开 zip
    // 2. 遍历 ppt/slides/slideN.xml
    // 3. 对每张 slide 解析 <p:pic> → 获取 r:embed → ppt/media/imageN.ext
    // 4. 上下文：幻灯片标题 + 同 slide 的文本框内容
    // 5. 创建 DocumentNode{Type:"image", Page:slideNum, Metadata:{...}}
}
```

#### 4.3 PDF（`corelib/knowledge/parse_pdf_images.go`）

```go
// extractPDFImages 使用 pdfcpu 从 PDF 中提取嵌入图片
func extractPDFImages(source Source, filePath string, textNodes []DocumentNode) ([]DocumentNode, error) {
    // 1. pdfcpu.ExtractImagesFile(filePath, tmpDir, nil)
    // 2. 遍历提取的图片文件（按页码命名）
    // 3. 上下文：同页的 textNode 内容
    // 4. 过滤小图片（< 50x50 px）— 通常是装饰性图标
    // 5. 创建 DocumentNode{Type:"image", Page:pageNum, Metadata:{...}}
}
```

#### 4.4 .doc OLE2（P1 阶段，`corelib/knowledge/parse_doc_images.go`）

```go
// extractDOCImages 从 OLE2 compound file 中提取 Pictures stream
func extractDOCImages(source Source, filePath string) ([]DocumentNode, error) {
    // 1. 解析 OLE2 compound file header + sector chain
    // 2. 定位 Pictures stream（或 Data stream 中的图片块）
    // 3. 按 blob header 拆分各图片（magic bytes 识别 PNG/JPEG/EMF/WMF）
    // 4. EMF/WMF → 标记为矢量图，不做 OCR
    // 5. 上下文：关联到文档级（.doc 精确位置关联复杂度极高）
    // 6. 创建 DocumentNode{Type:"image", Metadata:{...}}
}
```

### 5. 独立图片导入 + 两遍扫描关联（`corelib/knowledge/scan_images.go`）

```go
// BuildImageReferenceMap 第一遍扫描：从已解析的文本 Source 中提取图片引用
func BuildImageReferenceMap(store *SQLiteStore, batchSources []Source) map[string][]ImageReference {
    // 遍历所有文本 Source 的 DocumentNode
    // 在 Node.Text 中搜索图片引用：
    //   - Markdown: ![alt](relative/path.png)
    //   - HTML: <img src="relative/path.png" alt="...">
    //   - LaTeX: \includegraphics{path.png}
    // 返回 map[相对路径][]ImageReference
}

type ImageReference struct {
    SourceID      string // 引用文档的 Source ID
    NodeID        string // 引用处的 Node ID
    AltText       string // alt 属性
    ContextBefore string // 引用处前文
    ContextAfter  string // 引用处后文
    SectionTitle  string // 所在章节标题
}

// ResolveImageDescription 第二遍扫描：为独立图片生成描述
func ResolveImageDescription(imgRelPath string, refs []ImageReference, hints ImageHints) ImageHints {
    if len(refs) > 0 {
        // 有引用：用引用处上下文做描述
        best := refs[0]
        hints.ContextBefore = best.ContextBefore
        hints.ContextAfter = best.ContextAfter
        hints.AltText = best.AltText
        hints.ParentTitle = best.SectionTitle
    }
    // 无引用：hints 保持原始值（文件名+目录名）
    return hints
}
```

### 6. DocumentNode 扩展

```go
// DocumentNode.Type 新增值
const (
    NodeTypeImage = "image"  // 图片节点
)

// DocumentNode.Metadata 新增键
const (
    MetaImageAssetPath  = "image_asset_path"   // 图片资产路径（相对于 knowledge_assets/）
    MetaImageWidth      = "image_width"
    MetaImageHeight     = "image_height"
    MetaImageFormat     = "image_format"       // "png"/"jpeg"/"gif"/"bmp"/"webp"
    MetaImageOCRText    = "ocr_text"           // OCR 提取文字（独立于 Node.Text）
    MetaImageRefSource  = "ref_source_id"      // 引用此图片的文档 Source ID
    MetaImageRefNode    = "ref_node_id"        // 引用此图片的文档 Node ID
    MetaImageVector     = "image_vector_type"  // "raster"/"vector"（EMF/WMF/SVG 为 vector）
)
```

### 7. 导入管线集成

```go
// store.go 中 importSingleFile 增强
func (s *SQLiteStore) importSingleFile(ctx, source, filePath, kind, opts) error {
    // 现有逻辑：ParseDocumentNodes → distill → index
    nodes, err := ParseDocumentNodes(source, filePath, kind)
    
    // 新增：提取内嵌图片
    var imageNodes []DocumentNode
    switch kind {
    case SourceKindDOCX:
        imageNodes, _ = extractDOCXImages(source, filePath, nodes)
    case SourceKindPPTX:
        imageNodes, _ = extractPPTXImages(source, filePath, nodes)
    case SourceKindPDF:
        imageNodes, _ = extractPDFImages(source, filePath, nodes)
    case SourceKindDOC:
        imageNodes, _ = extractDOCImages(source, filePath)
    }
    
    // 处理每张内嵌图片
    for _, imgNode := range imageNodes {
        imgBytes := imgNode.Metadata["_raw_bytes"]  // 临时字段，处理完删除
        asset, _ := s.imageAssets.SaveImage(source.ID, imgBytes)
        imgNode.Metadata[MetaImageAssetPath] = asset.OriginalPath
        // 调用 ImageDescriber 生成描述
        desc, _ := s.imageDescriber.Describe(ctx, asset.OriginalPath, buildHints(imgNode))
        imgNode.Text = formatImageNodeText(desc)
        imgNode.Title = desc.Title
        imgNode.Metadata[MetaImageOCRText] = desc.OCRText
    }
    
    // 合并文本节点和图片节点
    allNodes := append(nodes, imageNodes...)
    // 继续正常管线：distill → index
}
```

### 8. 搜索结果格式化增强（`corelib/knowledge/text.go`）

```go
func FormatSearchResult(card Card, node DocumentNode) string {
    if node.Type == NodeTypeImage {
        return fmt.Sprintf("[图片] %s\n  描述: %s\n  OCR文字: %s\n  图片路径: %s\n  关联文档: %s",
            card.Title,
            card.Claim,
            node.Metadata[MetaImageOCRText],
            node.Metadata[MetaImageAssetPath],
            node.Metadata[MetaImageRefSource],
        )
    }
    // 原有文本格式化逻辑
}
```

### 9. 配置结构

```json
// ~/.maclaw/config.json 新增
{
  "knowledge_vision_llm": {
    "enabled": false,
    "base_url": "",
    "api_key": "",
    "model": "",
    "max_tokens": 500,
    "timeout_sec": 30,
    "verified": false
  },
  "knowledge_include_images": false
}
```

### 10. 批量处理限流

```go
const (
    imageDescribeConcurrency = 2   // Vision/OCR 并发上限
    visionRatePerSecond      = 5   // Vision LLM 每秒请求上限
    asyncThreshold           = 50  // 超过此数量则 Vision 描述改为异步
)

// 超过 asyncThreshold 时：
// - 导入阶段：只用 OCR + 上下文完成基础描述 → Source.Status = "parsed"
// - 后台异步：Vision LLM 补充高质量描述 → Source.Status = "distilled"
```

## 新增依赖

| 库 | 用途 | 备注 |
|----|------|------|
| `github.com/pdfcpu/pdfcpu` | PDF 图片提取 | 纯 Go，零 CGO |
| `golang.org/x/image/draw` | 缩略图 Lanczos 缩放 | Go 标准扩展 |
| 已有 `archive/zip` | DOCX/PPTX 图片读取 | Go 标准库 |
| 已有 `RapidOCRSidecar` | 图中文字提取 | Python sidecar |

## 不修改的部分

- Card 蒸馏管线（`distill.go`）— 图片的 DocumentNode.Text 已包含描述文本，正常走 Card 蒸馏
- FTS5 索引— 图片描述作为普通文本被索引
- 向量搜索— 图片 Card 的 Embedding 从描述文本生成
- Fact 提取— 从图片描述中可提取实体关系（如"A→B→C"的架构关系）
