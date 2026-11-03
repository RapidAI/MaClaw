# GoPDF2 API 参考手册

**[English](API.md) | [中文](API_zh.md)**

包 `gopdf` — [github.com/VantageDataChat/GoPDF2](https://github.com/VantageDataChat/GoPDF2)

---

## 类型定义

### GoPdf

PDF 文档的主结构体。

```go
type GoPdf struct { /* 内部字段 */ }
```

### Config

```go
type Config struct {
    Unit              int                 // UnitPT, UnitMM, UnitCM, UnitIN, UnitPX
    ConversionForUnit float64             // 自定义转换系数（覆盖 Unit）
    TrimBox           Box                 // 所有页面的默认裁切框
    PageSize          Rect                // 默认页面尺寸
    Protection        PDFProtectionConfig // 密码保护设置
}
```

### Rect

```go
type Rect struct {
    W float64 // 宽度
    H float64 // 高度
}
```

### Point

```go
type Point struct {
    X float64
    Y float64
}
```

### CellOption

```go
type CellOption struct {
    Align        int            // Left|Center|Right|Top|Bottom|Middle
    Border       int            // Left|Top|Right|Bottom|AllBorders
    Float        int            // Right|Bottom
    Transparency *Transparency
    BreakOption  *BreakOption
}
```

### HTMLBoxOption

```go
type HTMLBoxOption struct {
    DefaultFontFamily    string    // 必填。HTML 未指定字体时使用的默认字体族。
    DefaultFontSize      float64   // 默认字号（磅），默认 12。
    DefaultColor         [3]uint8  // 默认文字颜色 {R, G, B}。
    LineSpacing          float64   // 额外行间距（文档单位）。
    BoldFontFamily       string    // <b>/<strong> 使用的字体族。
    ItalicFontFamily     string    // <i>/<em> 使用的字体族。
    BoldItalicFontFamily string    // 粗斜体使用的字体族。
}
```

### TtfOption

```go
type TtfOption struct {
    UseKerning                bool              // 是否启用字距调整
    Style                     int               // Regular|Bold|Italic
    OnGlyphNotFound           func(r rune)      // 字形缺失时的调试回调
    OnGlyphNotFoundSubstitute func(r rune) rune // 字形缺失时的替换回调
}
```

### Transparency

```go
type Transparency struct {
    Alpha         float64       // 0.0（完全透明）到 1.0（不透明）
    BlendModeType BlendModeType // 如 "/Normal"、"/Multiply" 等
}
```

### 预定义页面尺寸

`PageSizeA0`–`PageSizeA10`、`PageSizeA3Landscape`–`PageSizeA10Landscape`、`PageSizeB0`–`PageSizeB10`、`PageSizeLetter`、`PageSizeLetterLandscape`、`PageSizeLegal`、`PageSizeLegalLandscape`、`PageSizeTabloid`、`PageSizeLedger`、`PageSizeStatement`、`PageSizeExecutive`、`PageSizeFolio`、`PageSizeQuarto`

### 单位常量

`UnitPT`（磅）、`UnitMM`（毫米）、`UnitCM`（厘米）、`UnitIN`（英寸）、`UnitPX`（像素）

### 字体样式常量

`Regular` (0)、`Italic` (1)、`Bold` (2)、`Underline` (4)

### 对齐常量

`Left`、`Right`、`Top`、`Bottom`、`Center`、`Middle`

---

## 文档生命周期

```go
func (gp *GoPdf) Start(config Config)
func (gp *GoPdf) AddPage()
func (gp *GoPdf) AddPageWithOption(opt PageOption)
func (gp *GoPdf) WritePdf(pdfPath string) error
func (gp *GoPdf) Write(w io.Writer) error
func (gp *GoPdf) WriteTo(w io.Writer) (int64, error)
func (gp *GoPdf) GetBytesPdf() []byte
func (gp *GoPdf) GetBytesPdfReturnErr() ([]byte, error)
```

---

## 字体管理

```go
func (gp *GoPdf) AddTTFFont(family string, ttfpath string) error
func (gp *GoPdf) AddTTFFontWithOption(family string, ttfpath string, option TtfOption) error
func (gp *GoPdf) AddTTFFontByReader(family string, rd io.Reader) error
func (gp *GoPdf) AddTTFFontData(family string, fontData []byte) error
func (gp *GoPdf) SetFont(family string, style string, size interface{}) error
func (gp *GoPdf) SetFontWithStyle(family string, style int, size interface{}) error
func (gp *GoPdf) SetFontSize(fontSize float64) error
func (gp *GoPdf) KernOverride(family string, fn FuncKernOverride) error
```

字体会自动子集化 — 输出 PDF 中仅嵌入文档实际使用的字形，有效控制文件大小。

---

## 文本

```go
func (gp *GoPdf) Cell(rectangle *Rect, text string) error
func (gp *GoPdf) CellWithOption(rectangle *Rect, text string, opt CellOption) error
func (gp *GoPdf) MultiCell(rectangle *Rect, text string) error
func (gp *GoPdf) MultiCellWithOption(rectangle *Rect, text string, opt CellOption) error
func (gp *GoPdf) Text(text string) error
func (gp *GoPdf) MeasureTextWidth(text string) (float64, error)
func (gp *GoPdf) MeasureCellHeightByText(text string) (float64, error)
func (gp *GoPdf) SplitText(text string, width float64) ([]string, error)
func (gp *GoPdf) SplitTextWithWordWrap(text string, width float64) ([]string, error)
func (gp *GoPdf) IsFitMultiCell(rectangle *Rect, text string) (bool, float64, error)
```

---

## HTML 渲染

```go
func (gp *GoPdf) InsertHTMLBox(x, y, w, h float64, htmlStr string, opt HTMLBoxOption) (float64, error)
func (gp *GoPdf) InsertHTMLPaginated(x, y, w float64, htmlStr string, opt HTMLBoxOption) (float64, error)
```

将 HTML 内容渲染到 PDF 页面的指定矩形区域内。`InsertHTMLBox` 在盒子高度处截断内容流，但只要一行的**顶部起点**在盒内就会正常绘制，只有起点越过盒底的行才会被丢弃——紧贴高度的盒子（如恢复默认 `<p>` 间距后的 18pt 品牌行）也能保住首行。inline `<img>` 参与行高：光标会跳过整幅位图，溢出检查与分页也都按图片高度比较，后续文字不会压在图片上。`InsertHTMLPaginated` 让内容自动跨页流动——内容超过 `opt.PageBottom` 时自动添加新页并从 `opt.PageTop` 继续渲染，支持表格表头跨页重复（`opt.RepeatTableHeaders`）与 `opt.OnPageBreak` 回调。

**参数：** `x, y`（左上角坐标）、`w, h`（区域尺寸）、`htmlStr`（HTML 字符串）、`opt`（渲染选项）。

**返回值：** 最后渲染内容之后的 Y 坐标。

**支持的标签：** `<b>`、`<strong>`、`<i>`、`<em>`、`<u>`、`<s>`/`<strike>`/`<del>`（真实删除线）、`<br>`、`<p>`、`<div>`、`<h1>`-`<h6>`、`<font>`、`<span>`、`<small>`、`<big>`、`<img>`（文件路径与 base64 Data URI）、`<ul>`/`<ol>`（嵌套、`start`、`type` 十进制/字母/罗马数字）、`<hr>`、`<center>`、`<a>`（含样式与可点击注释）、`<blockquote>`、`<sub>`、`<sup>`、`<pre>`/`<code>`/`<tt>`、`<style>`、`<script>`（忽略）、完整 `<table>`（`<thead>`/`<tbody>`/`<tfoot>`/`<tr>`/`<th>`/`<td>`/`<caption>`/`<colgroup>`/`<col>`、colspan/rowspan、单元格对齐与背景色）

**CSS：** `<style>` 样式块支持 标签 / `.class` / `#id` 选择器（优先级 id > class > 标签）；内联 `style="..."` 覆盖层叠规则。

**支持的 CSS 属性：** `color`、`background-color`、`font-size`、`font-family`、`font-weight`、`font-style`、`text-decoration`、`text-align`（含 `justify` 两端对齐）、`text-transform`、`text-indent`、`line-height`、`letter-spacing`、`vertical-align`、`width`（表格/单元格/图片）、`margin-top`、`margin-bottom`、`white-space`

**中文换行：** CJK 文本按字符断行并遵循避头尾规则（行首不出现标点、行尾不出现开括号）。

**块级背景：** 块级元素的 `background-color` 为每一行绘制整宽色带（正确的层级顺序、分页安全）。

**表格分页：** `InsertHTMLPaginated` 以行组为单位断页——由 rowspan 连接的行不会分散到两个页面（超尺寸组回退为行级断页）；设置 `RepeatTableHeaders` 后每个续页重复 `<thead>` 表头。

**颜色格式：** `#RGB`、`#RRGGBB`、`rgb(r,g,b)`、`rgba(r,g,b,a)`（alpha 忽略）、CSS 命名颜色

**字号格式：** `12pt`、`16px`、`1.5em`、`150%`、命名尺寸（small、medium、large 等）

---

## 表格布局

```go
func (gp *GoPdf) NewTableLayout(startX, startY, rowHeight float64, maxRows int) TableLayout
```

```go
type TableLayout interface {
    AddColumn(header string, width float64, align string)
    AddRow(row []string)
    AddStyledRow(row []RowCell)
    SetTableStyle(style CellStyle)
    SetHeaderStyle(style CellStyle)
    SetCellStyle(style CellStyle)
    DrawTable() error
    SetAutoRowHeight(auto bool) // 行高随内容自动增长
    SetPaginate(paginate bool)  // 跨页表格并重复表头
}
```

- `SetAutoRowHeight(true)` 按换行后的单元格文本测量并自动增高行（`rowHeight` 成为最小值）。
- `SetPaginate(true)` 当行越过下边距时自动换页并在每一续页重绘表头行。
- `NewRowCellSpan(content, style, colspan)` 创建跨多列的合并单元格。

---

## 公式渲染

```go
func (gp *GoPdf) DrawFormula(x, y float64, latex string, fontSize float64, opt *FormulaOption) (float64, error)
func (gp *GoPdf) MeasureFormula(latex string, fontSize float64) (width float64, height float64, err error)
```

以实用的 LaTeX 精简子集渲染公式：

- 上下标：`x^2`、`a_i`、`x^{n+1}`
- 分数：`rac{a}{b}`
- 根号（带上划线）：`\sqrt{x}`
- 二项式（带括号）：`inom{n}{k}`
- 希腊字母：`lpha` ... `\Omega`
- 运算符与符号：`\sum`、`\int`、`\prod`、`	imes`、`\cdot`、`\pm`、`\leq`、`\geq`、`
eq`、`pprox`、`\infty`、`
ightarrow`、`\partial`、`
abla` 等
- 函数名：`\sin`、`\cos`、`\log`、`\lim` 等

`FormulaOption` 可指定字体族、颜色与斜体。默认使用当前字体；建议选用数学符号覆盖良好的字体（STIX、DejaVu、Liberation）。

---

## 位置控制

```go
func (gp *GoPdf) SetX(x float64)
func (gp *GoPdf) GetX() float64
func (gp *GoPdf) SetY(y float64)
func (gp *GoPdf) GetY() float64
func (gp *GoPdf) SetXY(x, y float64)
func (gp *GoPdf) Br(h float64)
func (gp *GoPdf) SetNewY(y float64, h float64)
func (gp *GoPdf) SetNewYIfNoOffset(y float64, h float64)
func (gp *GoPdf) SetNewXY(y float64, x, h float64)
```

---

## 颜色

```go
func (gp *GoPdf) SetTextColor(r, g, b uint8)
func (gp *GoPdf) SetTextColorCMYK(c, m, y, k uint8)
func (gp *GoPdf) SetStrokeColor(r, g, b uint8)
func (gp *GoPdf) SetFillColor(r, g, b uint8)
func (gp *GoPdf) SetGrayFill(grayScale float64)
func (gp *GoPdf) SetGrayStroke(grayScale float64)
```

---

## 绘图

```go
func (gp *GoPdf) Line(x1, y1, x2, y2 float64)
func (gp *GoPdf) Oval(x1, y1, x2, y2 float64)
func (gp *GoPdf) Polygon(points []Point, style string)
func (gp *GoPdf) Polyline(points []Point)
func (gp *GoPdf) Sector(cx, cy, r, startDeg, endDeg float64, style string)
func (gp *GoPdf) Rectangle(x0, y0, x1, y1 float64, style string, radius float64, radiusPointNum int) error
func (gp *GoPdf) RectFromUpperLeft(x, y, w, h float64)
func (gp *GoPdf) RectFromUpperLeftWithStyle(x, y, w, h float64, style string)
func (gp *GoPdf) Curve(x0, y0, x1, y1, x2, y2, x3, y3 float64, style string)
func (gp *GoPdf) SetLineWidth(width float64)
func (gp *GoPdf) SetLineType(linetype string)
```

`Polyline` 绘制一条开放折线（仅描边，路径不闭合）。`Sector` 绘制扇形（饼形），由圆心、半径、起始/结束角度（度，从正 X 轴逆时针）定义。style 参数：`"D"`（描边）、`"F"`（填充）、`"DF"`/`"FD"`（描边+填充）。

---

## 图片

```go
func (gp *GoPdf) Image(picPath string, x, y float64, rect *Rect) error
func (gp *GoPdf) ImageFrom(img image.Image, x, y float64, rect *Rect) error
func (gp *GoPdf) ImageByHolder(img ImageHolder, x, y float64, rect *Rect) error
func (gp *GoPdf) ImageByHolderWithOptions(img ImageHolder, opts ImageOptions) error
func ImageHolderByPath(path string) (ImageHolder, error)
func ImageHolderByBytes(b []byte) (ImageHolder, error)
func ImageHolderByReader(r io.Reader) (ImageHolder, error)
```

---

## 旋转与透明度

```go
func (gp *GoPdf) Rotate(angle, x, y float64)
func (gp *GoPdf) RotateReset()
func (gp *GoPdf) SetTransparency(transparency Transparency) error
func (gp *GoPdf) ClearTransparency()
```

---

## 边距

```go
func (gp *GoPdf) SetMargins(left, top, right, bottom float64)
func (gp *GoPdf) Margins() (float64, float64, float64, float64)
```

---

## 链接、锚点、页眉、页脚

```go
func (gp *GoPdf) AddExternalLink(url string, x, y, w, h float64)
func (gp *GoPdf) AddInternalLink(anchor string, x, y, w, h float64)
func (gp *GoPdf) SetAnchor(name string)
func (gp *GoPdf) AddHeader(f func())
func (gp *GoPdf) AddFooter(f func())
```

---

## 导入已有 PDF

```go
func (gp *GoPdf) ImportPage(sourceFile string, pageno int, box string) int
func (gp *GoPdf) UseImportedTemplate(tplid int, x, y, w, h float64)
func (gp *GoPdf) ImportPagesFromSource(source interface{}, box string) error
```

---

## 打开并修改已有 PDF

```go
func (gp *GoPdf) OpenPDF(pdfPath string, opt *OpenPDFOption) error
func (gp *GoPdf) OpenPDFFromBytes(pdfData []byte, opt *OpenPDFOption) error
func (gp *GoPdf) OpenPDFFromStream(rs *io.ReadSeeker, opt *OpenPDFOption) error
```

打开已有 PDF 并导入所有页面，使新内容可以叠加在原有页面之上。原始页面内容作为背景保留，之后可使用任何绘图方法（Text、Cell、Image、InsertHTMLBox、Line 等）叠加新内容。

调用 `OpenPDF` 后，使用 `SetPage(n)`（从 1 开始）切换页面，绘制内容，最后调用 `WritePdf` 保存。

### OpenPDFOption

```go
type OpenPDFOption struct {
    Box        string               // PDF 页面框："/MediaBox"（默认）、"/CropBox" 等。
    Protection *PDFProtectionConfig // 可选，输出 PDF 的密码保护设置。
}
```

---

## 占位文本

```go
func (gp *GoPdf) PlaceHolderText(placeHolderName string, placeHolderWidth float64) error
func (gp *GoPdf) FillInPlaceHoldText(placeHolderName string, text string, align int) error
```

---

## 页面管理与单位转换

```go
func (gp *GoPdf) GetNumberOfPages() int
func (gp *GoPdf) SetPage(pageno int) error
func (gp *GoPdf) UnitsToPoints(u float64) float64
func (gp *GoPdf) PointsToUnits(u float64) float64
```

---

## 其他

```go
func (gp *GoPdf) SetInfo(info PdfInfo)
func (gp *GoPdf) SetCompressLevel(level int)
func (gp *GoPdf) SetCharSpacing(charSpacing float64) error
func (gp *GoPdf) IsCurrFontContainGlyph(r rune) (bool, error)
func (gp *GoPdf) SaveGraphicsState()
func (gp *GoPdf) RestoreGraphicsState()
func (gp *GoPdf) AddOutline(title string)
```

---

## FontContainer（可复用字体缓存）

```go
type FontContainer struct { /* ... */ }
func (fc *FontContainer) AddTTFFont(family string, ttfpath string) error
func (fc *FontContainer) AddTTFFontByReader(family string, rd io.Reader) error
func (fc *FontContainer) AddTTFFontData(family string, fontData []byte) error
func (gp *GoPdf) AddTTFFontFromFontContainer(family string, container *FontContainer) error
```

预先解析字体并在多个 `GoPdf` 实例间复用，提升性能。

---

## 水印

```go
func (gp *GoPdf) AddWatermarkText(opt WatermarkOption) error
func (gp *GoPdf) AddWatermarkImage(imgPath string, opacity float64, imgW, imgH float64, angle float64) error
func (gp *GoPdf) AddWatermarkTextAllPages(opt WatermarkOption) error
func (gp *GoPdf) AddWatermarkImageAllPages(imgPath string, opacity float64, imgW, imgH float64, angle float64) error
```

### WatermarkOption

```go
type WatermarkOption struct {
    Text           string    // 水印文字（必填）
    FontFamily     string    // 字体族（必填，需预先加载）
    FontSize       float64   // 字号，单位磅（默认 48）
    Angle          float64   // 旋转角度，单位度（默认 45）
    Color          [3]uint8  // RGB 颜色（默认：浅灰色）
    Opacity        float64   // 0.0–1.0（默认 0.3）
    Repeat         bool      // 是否平铺
    RepeatSpacingX float64   // 平铺水平间距（默认 150）
    RepeatSpacingY float64   // 平铺垂直间距（默认 150）
}
```

---

## 注释

```go
func (gp *GoPdf) AddAnnotation(opt AnnotationOption)
func (gp *GoPdf) AddTextAnnotation(x, y float64, title, content string)
func (gp *GoPdf) AddHighlightAnnotation(x, y, w, h float64, color [3]uint8)
func (gp *GoPdf) AddFreeTextAnnotation(x, y, w, h float64, text string, fontSize float64)
```

### AnnotationOption

```go
type AnnotationOption struct {
    Type         AnnotationType // AnnotText, AnnotHighlight, AnnotUnderline, AnnotStrikeOut, AnnotSquare, AnnotCircle, AnnotFreeText
    X, Y, W, H  float64        // 注释矩形区域（文档单位）
    Title        string         // 作者名（便签注释）
    Content      string         // 注释文本内容
    Color        [3]uint8       // RGB 颜色（默认：黄色）
    Opacity      float64        // 0.0–1.0（默认 1.0）
    Open         bool           // 弹出窗口是否初始打开（文本注释）
    FontSize     float64        // 自由文本字号（默认 12）
}
```

---

## 页面操作

```go
func (gp *GoPdf) DeletePage(pageNo int) error
func (gp *GoPdf) DeletePages(pages []int) error
func (gp *GoPdf) CopyPage(pageNo int) (int, error)
func (gp *GoPdf) MovePage(from, to int) error
func ExtractPages(pdfPath string, pages []int, opt *OpenPDFOption) (*GoPdf, error)
func ExtractPagesFromBytes(pdfData []byte, pages []int, opt *OpenPDFOption) (*GoPdf, error)
func MergePages(pdfPaths []string, opt *OpenPDFOption) (*GoPdf, error)
func MergePagesFromBytes(pdfDataSlices [][]byte, opt *OpenPDFOption) (*GoPdf, error)
```

`DeletePages` 在单次操作中删除多个页面。页码从 1 开始；重复页码会被忽略；按倒序删除以保持正确编号。不能删除所有页面。

`MovePage` 通过 `SelectPages` 重排将页面从一个位置移动到另一个位置。

---

## 页面信息查询

```go
func (gp *GoPdf) GetPageSize(pageNo int) (w, h float64, err error)
func (gp *GoPdf) GetAllPageSizes() []PageInfo
func GetSourcePDFPageCount(pdfPath string) (int, error)
func GetSourcePDFPageCountFromBytes(pdfData []byte) (int, error)
func GetSourcePDFPageSizes(pdfPath string) (map[int]PageInfo, error)
func GetSourcePDFPageSizesFromBytes(pdfData []byte) (map[int]PageInfo, error)
```

### PageInfo

```go
type PageInfo struct {
    Width      float64 // 页面宽度（磅）
    Height     float64 // 页面高度（磅）
    PageNumber int     // 页码（从 1 开始）
}
```

---

## 纸张尺寸查询

```go
func PaperSize(name string) *Rect
func PaperSizeNames() []string
```

按名称查询预定义纸张尺寸（不区分大小写）。支持的名称：`a0`–`a10`、`b0`–`b10`、`letter`、`legal`、`tabloid`、`ledger`、`statement`、`executive`、`folio`、`quarto`。加 `-l` 后缀为横向（如 `a4-l`、`letter-l`）。返回 Rect 的副本；名称无法识别时返回 nil。

---

## 页面旋转

```go
func (gp *GoPdf) SetPageRotation(pageNo int, angle int) error
func (gp *GoPdf) GetPageRotation(pageNo int) (int, error)
```

设置或获取页面的显示旋转角度。`angle` 必须是 90 的倍数（0、90、180、270）。此操作设置页面字典中的 `/Rotate` 条目，告诉 PDF 阅读器如何显示页面，但不修改页面内容。

---

## 页面裁切框

```go
func (gp *GoPdf) SetPageCropBox(pageNo int, box Box) error
func (gp *GoPdf) GetPageCropBox(pageNo int) (*Box, error)
func (gp *GoPdf) ClearPageCropBox(pageNo int) error
```

设置、获取或移除页面的 CropBox。CropBox 定义页面在显示或打印时的可见区域。CropBox 之外的内容会被裁切（隐藏）但不会被删除。

`pageNo` 从 1 开始。Box 坐标使用文档单位（PDF 坐标系中 (0,0) 为左下角）。

`GetPageCropBox` 在未设置 CropBox 时返回 nil。`ClearPageCropBox` 移除 CropBox，恢复完整 MediaBox 作为可见区域。

---

## 页面重排

```go
func (gp *GoPdf) SelectPages(pages []int) (*GoPdf, error)
func SelectPagesFromFile(pdfPath string, pages []int, opt *OpenPDFOption) (*GoPdf, error)
func SelectPagesFromBytes(pdfData []byte, pages []int, opt *OpenPDFOption) (*GoPdf, error)
```

在新文档中重新排列页面。页码从 1 开始，可以重复。当前文档导出为字节后，仅按指定顺序重新导入选定的页面。

---

## 嵌入文件

```go
func (gp *GoPdf) AddEmbeddedFile(ef EmbeddedFile) error
```

将文件附加到 PDF 文档。文件会显示在 PDF 阅读器的附件面板中。

### EmbeddedFile

```go
type EmbeddedFile struct {
    Name        string    // 显示名称（必填）
    Content     []byte    // 文件内容（必填）
    MimeType    string    // MIME 类型（如 "text/plain"、"application/pdf"）
    Description string    // 可选描述
    ModDate     time.Time // 修改日期（默认：当前时间）
}
```

---

## 几何工具

### RectFrom

```go
type RectFrom struct {
    X, Y, W, H float64
}

func (r RectFrom) Contains(px, py float64) bool
func (r RectFrom) ContainsRect(other RectFrom) bool
func (r RectFrom) Intersects(other RectFrom) bool
func (r RectFrom) Intersection(other RectFrom) RectFrom
func (r RectFrom) Union(other RectFrom) RectFrom
func (r RectFrom) IsEmpty() bool
func (r RectFrom) Area() float64
func (r RectFrom) Center() Point
func (r RectFrom) Normalize() RectFrom
```

带位置的矩形，支持几何运算。

### Matrix

```go
type Matrix struct {
    A, B, C, D, E, F float64
}

func IdentityMatrix() Matrix
func TranslateMatrix(tx, ty float64) Matrix
func ScaleMatrix(sx, sy float64) Matrix
func RotateMatrix(degrees float64) Matrix
func (m Matrix) Multiply(other Matrix) Matrix
func (m Matrix) TransformPoint(x, y float64) (float64, float64)
func (m Matrix) IsIdentity() bool
```

2D 仿射变换矩阵。变换公式：`x' = a*x + c*y + e`，`y' = b*x + d*y + f`。

### Distance

```go
func Distance(p1, p2 Point) float64
```

计算两点之间的欧几里得距离。

---

## 内容元素 CRUD

### ContentElementType

```go
type ContentElementType int

const (
    ElementText             ContentElementType // 文本
    ElementImage            ContentElementType // 图片
    ElementLine             ContentElementType // 线条
    ElementRectangle        ContentElementType // 矩形
    ElementOval             ContentElementType // 椭圆
    ElementPolygon          ContentElementType // 多边形
    ElementCurve            ContentElementType // 贝塞尔曲线
    ElementPolyline         ContentElementType // 折线
    ElementSector           ContentElementType // 扇形
    ElementImportedTemplate ContentElementType // 导入的模板
    ElementLineWidth        ContentElementType // 线宽设置
    ElementLineType         ContentElementType // 线型设置
    // ... 更多类型（ColorRGB、ColorCMYK、Rotate 等）
    ElementUnknown          ContentElementType // 未知类型
)

func (t ContentElementType) String() string
```

### ContentElement

```go
type ContentElement struct {
    Index    int                // 页面内容缓存中的 0 基索引
    Type     ContentElementType // 元素类型
    X, Y     float64           // 主要位置坐标
    X2, Y2   float64           // 次要位置坐标（线条、椭圆）
    Width    float64           // 宽度（矩形、图片）
    Height   float64           // 高度（矩形、图片）
    Text     string            // 文本内容（仅文本元素）
    FontSize float64           // 字号（仅文本元素）
}
```

### 查询方法

```go
func (gp *GoPdf) GetPageElements(pageNo int) ([]ContentElement, error)
func (gp *GoPdf) GetPageElementsByType(pageNo int, elemType ContentElementType) ([]ContentElement, error)
func (gp *GoPdf) GetPageElementCount(pageNo int) (int, error)
```

### 删除方法

```go
func (gp *GoPdf) DeleteElement(pageNo int, elementIndex int) error
func (gp *GoPdf) DeleteElementsByType(pageNo int, elemType ContentElementType) (int, error)
func (gp *GoPdf) DeleteElementsInRect(pageNo int, rx, ry, rw, rh float64) (int, error)
func (gp *GoPdf) ClearPage(pageNo int) error
```

### 修改方法

```go
func (gp *GoPdf) ModifyTextElement(pageNo int, elementIndex int, newText string) error
func (gp *GoPdf) ModifyElementPosition(pageNo int, elementIndex int, x, y float64) error
```

### 插入方法

```go
func (gp *GoPdf) InsertLineElement(pageNo int, x1, y1, x2, y2 float64) error
func (gp *GoPdf) InsertRectElement(pageNo int, x, y, w, h float64, style string) error
func (gp *GoPdf) InsertOvalElement(pageNo int, x1, y1, x2, y2 float64) error
func (gp *GoPdf) InsertElementAt(pageNo int, elementIndex int, newElement ICacheContent) error
func (gp *GoPdf) ReplaceElement(pageNo int, elementIndex int, newElement ICacheContent) error
```

---

## PDF 版本控制

```go
type PDFVersion int

const (
    PDFVersion14 PDFVersion = 14 // PDF 1.4
    PDFVersion15 PDFVersion = 15 // PDF 1.5
    PDFVersion16 PDFVersion = 16 // PDF 1.6
    PDFVersion17 PDFVersion = 17 // PDF 1.7（默认）
    PDFVersion20 PDFVersion = 20 // PDF 2.0
)

func (v PDFVersion) String() string  // "1.7"
func (v PDFVersion) Header() string  // "%PDF-1.7"

func (gp *GoPdf) SetPDFVersion(v PDFVersion)
func (gp *GoPdf) GetPDFVersion() PDFVersion
```

---

## 垃圾回收

```go
type GarbageCollectLevel int

const (
    GCNone    GarbageCollectLevel = 0 // 不执行
    GCCompact GarbageCollectLevel = 1 // 移除空对象，重新编号
    GCDedup   GarbageCollectLevel = 2 // 额外去重相同对象
)

func (gp *GoPdf) GarbageCollect(level GarbageCollectLevel) int
func (gp *GoPdf) GetObjectCount() int
func (gp *GoPdf) GetLiveObjectCount() int
```

`GarbageCollect` 移除 `DeletePage` 等操作留下的空/已删除对象。返回移除的对象数量。

---

## 页面标签

```go
type PageLabelStyle string

const (
    PageLabelDecimal    PageLabelStyle = "D" // 1, 2, 3, ...
    PageLabelRomanUpper PageLabelStyle = "R" // I, II, III, ...
    PageLabelRomanLower PageLabelStyle = "r" // i, ii, iii, ...
    PageLabelAlphaUpper PageLabelStyle = "A" // A, B, C, ...
    PageLabelAlphaLower PageLabelStyle = "a" // a, b, c, ...
    PageLabelNone       PageLabelStyle = ""  // 无编号
)

type PageLabel struct {
    PageIndex int            // 此标签范围起始的 0 基页面索引
    Style     PageLabelStyle // 编号样式
    Prefix    string         // 可选前缀（如 "附录 "）
    Start     int            // 起始编号（默认 1）
}

func (gp *GoPdf) SetPageLabels(labels []PageLabel)
func (gp *GoPdf) GetPageLabels() []PageLabel
```

---

## 对象 ID

```go
type ObjID int

func (id ObjID) Index() int     // 0 基数组索引
func (id ObjID) Ref() int       // 1 基 PDF 对象引用号
func (id ObjID) RefStr() string // "5 0 R"
func (id ObjID) IsValid() bool
```

PDF 对象标识符的类型化包装器，提供比原始 int 索引更安全的类型检查。

---

## XMP 元数据

```go
type XMPMetadata struct {
    // Dublin Core
    Title       string
    Creator     []string
    Description string
    Subject     []string
    Rights      string
    Language    string

    // XMP 基本属性
    CreatorTool string
    CreateDate  time.Time
    ModifyDate  time.Time

    // PDF 特定属性
    Producer string
    Keywords string
    Trapped  string // "True"、"False"、"Unknown"

    // PDF/A 合规
    PDFAConformance string // "A"、"B"、"U"
    PDFAPart        int    // 1、2、3

    // 自定义属性
    Custom map[string]string
}

func (gp *GoPdf) SetXMPMetadata(meta XMPMetadata)
func (gp *GoPdf) GetXMPMetadata() *XMPMetadata
```

在 PDF 目录中嵌入 XMP 元数据流。支持 Dublin Core、XMP 基本属性、PDF 特定属性和 PDF/A 合规属性。

---

## 增量保存

```go
func (gp *GoPdf) IncrementalSave(originalData []byte, modifiedIndices []int) ([]byte, error)
func (gp *GoPdf) WriteIncrementalPdf(pdfPath string, originalData []byte, modifiedIndices []int) error
```

将修改过的对象作为增量更新追加到原始 PDF 数据之后。如果 `modifiedIndices` 为 nil，则写入所有对象。对于大文档，这比完全重写快得多。

---

## 文档克隆

```go
func (gp *GoPdf) Clone() (*GoPdf, error)
```

通过序列化和重新导入创建 GoPdf 实例的深拷贝。克隆完全独立——对一个的修改不会影响另一个。页眉/页脚回调不会被克隆。

---

## 文档清洗

```go
func (gp *GoPdf) Scrub(opt ScrubOption)
func DefaultScrubOption() ScrubOption
```

移除 PDF 中的敏感数据。灵感来自 PyMuPDF 的 `Document.scrub()` 方法。

### ScrubOption

```go
type ScrubOption struct {
    Metadata      bool // 移除标准 PDF 元数据（/Info 字典）
    XMLMetadata   bool // 移除 XMP 元数据流
    EmbeddedFiles bool // 移除所有嵌入文件附件
    PageLabels    bool // 移除页面标签定义
}
```

`DefaultScrubOption()` 返回所有字段均为 `true` 的 `ScrubOption`。

清洗后，调用 `GarbageCollect(GCCompact)` 并保存以确保已移除的数据被物理清除。

---

## 可选内容组（图层）

```go
func (gp *GoPdf) AddOCG(ocg OCG) OCG
func (gp *GoPdf) GetOCGs() []OCG
```

添加 PDF 图层（可选内容组），支持选择性显示/隐藏。图层会显示在 PDF 阅读器的图层面板中。

### OCG

```go
type OCG struct {
    Name     string    // 图层显示名称
    Intent   OCGIntent // 可见性意图（"View" 或 "Design"）
    On       bool      // 是否初始可见
}
```

### OCGIntent

```go
type OCGIntent string

const (
    OCGIntentView   OCGIntent = "View"   // 用于查看
    OCGIntentDesign OCGIntent = "Design" // 用于设计
)
```

---

## 页面布局与页面模式

```go
func (gp *GoPdf) SetPageLayout(layout PageLayout)
func (gp *GoPdf) GetPageLayout() PageLayout
func (gp *GoPdf) SetPageMode(mode PageMode)
func (gp *GoPdf) GetPageMode() PageMode
```

控制 PDF 阅读器打开文档时的显示方式。

### PageLayout

```go
type PageLayout string

const (
    PageLayoutSinglePage      PageLayout = "SinglePage"      // 每次显示一页
    PageLayoutOneColumn       PageLayout = "OneColumn"       // 连续单列
    PageLayoutTwoColumnLeft   PageLayout = "TwoColumnLeft"   // 双列，奇数页在左
    PageLayoutTwoColumnRight  PageLayout = "TwoColumnRight"  // 双列，奇数页在右
    PageLayoutTwoPageLeft     PageLayout = "TwoPageLeft"     // 双页，奇数页在左
    PageLayoutTwoPageRight    PageLayout = "TwoPageRight"    // 双页，奇数页在右
)
```

### PageMode

```go
type PageMode string

const (
    PageModeUseNone        PageMode = "UseNone"        // 不显示面板（默认）
    PageModeUseOutlines    PageMode = "UseOutlines"    // 显示书签面板
    PageModeUseThumbs      PageMode = "UseThumbs"      // 显示缩略图面板
    PageModeFullScreen     PageMode = "FullScreen"     // 全屏模式
    PageModeUseOC          PageMode = "UseOC"          // 显示图层面板
    PageModeUseAttachments PageMode = "UseAttachments" // 显示附件面板
)
```

---

## 文档统计

```go
func (gp *GoPdf) GetDocumentStats() DocumentStats
func (gp *GoPdf) GetFonts() []FontInfo
```

### DocumentStats

```go
type DocumentStats struct {
    PageCount          int        // 总页数
    ObjectCount        int        // PDF 对象总数
    LiveObjectCount    int        // 非空对象数
    FontCount          int        // 字体对象数
    ImageCount         int        // 图片对象数
    ContentStreamCount int        // 内容流对象数
    HasOutlines        bool       // 是否有书签
    HasEmbeddedFiles   bool       // 是否有附件
    HasXMPMetadata     bool       // 是否设置了 XMP 元数据
    HasPageLabels      bool       // 是否定义了页面标签
    HasOCGs            bool       // 是否定义了可选内容组
    PDFVersion         PDFVersion // 配置的 PDF 版本
}
```

### FontInfo

```go
type FontInfo struct {
    Family     string // 字体族名称
    Style      int    // 字体样式（Regular、Bold、Italic）
    IsEmbedded bool   // 字体文件是否已嵌入
    Index      int    // 内部对象索引
}
```

---

## 目录/书签

```go
func (gp *GoPdf) GetTOC() []TOCItem
func (gp *GoPdf) SetTOC(items []TOCItem) error
```

读写文档的大纲（书签）树。`GetTOC` 返回带层级信息的扁平列表。`SetTOC` 替换整个大纲树。

### TOCItem

```go
type TOCItem struct {
    Level  int     // 层级（1 = 顶层）
    Title  string  // 书签标题文本
    PageNo int     // 目标页码（从 1 开始）
    Y      float64 // 目标页面上的垂直位置（距顶部的磅数）
}
```

`SetTOC` 验证规则：
- 第一项必须为 `Level` 1。
- 层级最多只能比前一项增加 1。
- 验证失败时返回 `ErrInvalidTOCLevel`。

---

## 文本提取

```go
func ExtractTextFromPage(pdfData []byte, pageIndex int) ([]ExtractedText, error)
func ExtractTextFromAllPages(pdfData []byte) (map[int][]ExtractedText, error)
func ExtractPageText(pdfData []byte, pageIndex int) (string, error)
func ExtractPageLines(pdfData []byte, pageIndex int) ([]TextLine, error)
func ExtractAllPagesLines(pdfData []byte) (map[int][]TextLine, error)
func MergeTextRuns(runs []ExtractedText) []ExtractedText
func GroupIntoTextLines(runs []ExtractedText) []TextLine
```

从已有 PDF 文件中提取文本内容。这些是包级函数（非 GoPdf 方法）。

- `ExtractTextFromPage` — 从单个页面（0 起始索引）提取带位置、字体、字号信息的文本项。
- `ExtractTextFromAllPages` — 从所有页面提取文本，返回页面索引到文本项的映射。
- `ExtractPageText` — 便捷包装，将页面所有文本作为单个字符串返回。
- `ExtractPageLines` — 将碎片化的文本项按位置合并为可读的单词与行（推荐用于逐字形 Tj 流的真实 PDF）。
- `ExtractAllPagesLines` — 解析一次 PDF 并返回每一页的合并行（优先于循环调用 `ExtractPageLines`）。
- `MergeTextRuns` / `GroupIntoTextLines` — 底层的位置感知合并函数。

提取管线支持对象流（PDF 1.5+）、**Form XObject**（`/Subtype /Form` 型印章/信头中的嵌套文本会被提取）、预定义 CMap、/Differences 字形名、WinAnsi/MacRoman/PDFDoc 编码、传统 GBK/Big5/Shift-JIS/EUC 编码、/Widths 与 CID /W 字形宽度、TJ 字距空格、以及 PDF 2.0 的 UTF-16BE/UTF-8 文本字符串。

**坐标与图形状态。** 所有位置都按页面磅值单位返回，`Y` **从页面顶端量起**（自上而下坐标系）。提取器遵循 PDF 图形状态栈：`q`…`Q` 组内的 `cm` 与当前矩阵复合运算，`Q` 会恢复原矩阵——`q <cm> /Im Do Q` 绘制图片后放置的文本，位置保持其自身用户空间坐标，不会继承图片的变换矩阵。`ExtractTextFromPage` 返回的每一项位于其起点；`MergeTextRuns` 将逐字形碎片合并成完整字符串。

> 注意：ledongthuc/pdf 兼容回退路径（仅在 raw 解析器无法产出文本时使用）返回的
> `Y` 仍是自下而上的旧约定。涉及位置坐标时请以 raw 管线的结果为准。

### ExtractedText

```go
type ExtractedText struct {
    Text     string  // 提取的文本字符串
    X        float64 // 水平位置（页面单位，向右增大）
    Y        float64 // 行基线，从页面顶端量起（自上而下坐标）
    FontName string  // PDF 字体资源名称（如 "LiberationSerif-Regular"）
    FontSize float64 // 字号（磅）
    Width    float64 // 估算步进宽度（由 MergeTextRuns 设定；未知时为 0）
}
```

### TextLine

```go
type TextLine struct {
    Y     float64    // 行基线（页面自上而下坐标）
    Words []TextWord // 合并后的单词，从左到右
    Text  string     // 带单词间距的整行文本
}
```

---

## 图片提取

```go
func ExtractImagesFromPage(pdfData []byte, pageIndex int) ([]ExtractedImage, error)
func ExtractImagesFromAllPages(pdfData []byte) (map[int][]ExtractedImage, error)
```

从已有 PDF 文件中提取图片元数据和数据。这些是包级函数。

位置约定与文本提取一致：`X`/`Y` 表示图片**左上角**在页面上的位置，`Y` 从页面顶端量起；`DisplayWidth`/`DisplayHeight` 为页面单位下的渲染尺寸。图片提取器维护完整图形状态栈，`q`…`Q` 分组与复合矩阵已经作用于放置坐标。

### ExtractedImage

```go
type ExtractedImage struct {
    Name             string  // XObject 资源名称（如 "/Im1"）
    Width            int     // 图片宽度（像素）
    Height           int     // 图片高度（像素）
    BitsPerComponent int     // 每颜色分量位数
    ColorSpace       string  // 颜色空间名称（如 "DeviceRGB"）
    Filter           string  // 压缩过滤器（如 "DCTDecode"）
    Data             []byte  // 原始图片数据
    ObjNum           int     // PDF 对象编号
    X, Y             float64 // 页面左上角位置（Y 从页面顶端量起）
    DisplayWidth     float64 // 页面上的渲染宽度
    DisplayHeight    float64 // 页面上的渲染高度
}

func (img *ExtractedImage) GetImageFormat() string
```

`GetImageFormat` 根据过滤器返回可能的图片格式："jpeg"、"jp2"、"tiff"、"png" 或 "raw"。

---

## 表单字段 (AcroForm)

```go
func (gp *GoPdf) AddFormField(field FormField) error
func (gp *GoPdf) AddTextField(name string, x, y, w, h float64) error
func (gp *GoPdf) AddCheckbox(name string, x, y, size float64, checked bool) error
func (gp *GoPdf) AddDropdown(name string, x, y, w, h float64, options []string) error
func (gp *GoPdf) AddSignatureField(name string, x, y, w, h float64) error
func (gp *GoPdf) GetFormFields() []FormField
```

向 PDF 页面添加交互式表单字段（Widget）。字段在 PDF 阅读器中显示为可交互元素。

### FormFieldType

```go
const (
    FormFieldText      FormFieldType = iota // 文本输入
    FormFieldCheckbox                       // 复选框
    FormFieldRadio                          // 单选按钮
    FormFieldChoice                         // 下拉框/列表
    FormFieldButton                         // 按钮
    FormFieldSignature                      // 签名
)
```

### FormField

```go
type FormField struct {
    Type        FormFieldType
    Name        string     // 唯一字段标识符
    X, Y        float64   // 左上角位置
    W, H        float64   // 宽度和高度
    Value       string     // 默认值
    FontFamily  string     // 字体族（需预加载）
    FontSize    float64    // 字号（默认 12）
    Options     []string   // Choice 字段的选项列表
    MaxLen      int        // 文本字段最大字符数
    Multiline   bool       // 多行文本输入
    ReadOnly    bool       // 只读
    Required    bool       // 必填
    Color       [3]uint8   // 文本颜色 [R, G, B]
    BorderColor [3]uint8   // 边框颜色 [R, G, B]
    FillColor   [3]uint8   // 背景填充颜色 [R, G, B]
    HasBorder   bool       // 绘制边框
    HasFill     bool       // 绘制背景填充
    Checked     bool       // 复选框初始状态
}
```

---

## 数字签名

```go
func (gp *GoPdf) SignPDF(cfg SignatureConfig, w io.Writer) error
func (gp *GoPdf) SignPDFToFile(cfg SignatureConfig, path string) error
func VerifySignature(pdfData []byte) ([]SignatureVerifyResult, error)
func VerifySignatureFromFile(path string) ([]SignatureVerifyResult, error)
```

使用 PKCS#7 分离签名对 PDF 文档进行数字签名，并验证已有签名。

### SignatureConfig

```go
type SignatureConfig struct {
    Certificate      *x509.Certificate   // 签名证书
    CertificateChain []*x509.Certificate // 可选的中间证书链
    PrivateKey       crypto.Signer       // RSA 或 ECDSA 私钥
    Reason           string              // 签名原因
    Location         string              // 签名地点
    ContactInfo      string              // 签名者联系信息
    Name             string              // 签名者姓名（默认使用证书 CN）
    SignTime         time.Time           // 签名时间（默认当前时间）
    SignatureFieldName string            // 字段名称（默认 "Signature1"）
    Visible          bool                // 可见签名外观
    X, Y, W, H      float64             // 可见签名矩形
    PageNo           int                 // 页码（从 1 开始）
}
```

### 辅助函数

```go
func LoadCertificateFromPEM(certPath string) (*x509.Certificate, error)
func LoadPrivateKeyFromPEM(keyPath string) (crypto.Signer, error)
func ParseCertificatePEM(data []byte) (*x509.Certificate, error)
func ParsePrivateKeyPEM(data []byte) (crypto.Signer, error)
```
