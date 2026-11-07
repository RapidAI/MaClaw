# GoPDF2

[![Go Reference](https://pkg.go.dev/badge/github.com/VantageDataChat/GoPDF2.svg)](https://pkg.go.dev/github.com/VantageDataChat/GoPDF2)
![Version](https://img.shields.io/badge/version-2.0.0-blue)

**[English](README.md) | [中文](README_zh.md)**

GoPDF2 是一个用 Go 编写的 PDF 生成库。基于 [gopdf](https://github.com/signintech/gopdf) 开发，新增了 HTML 渲染、打开并修改已有 PDF 等功能。

需要 Go 1.13+。

## 功能特性

- Unicode 子集字体嵌入（中文、日文、韩文等），自动子集化以最小化文件体积
- **打开并修改已有 PDF** — 通过 `OpenPDF` 导入所有页面，在其上叠加新内容（文字、图片、HTML、绘图），然后保存
- **HTML 转 PDF 渲染** — 通过 `InsertHTMLBox` 支持 `<b>`、`<i>`、`<u>`、`<s>`、`<p>`、`<h1>`–`<h6>`、`<font>`、`<span style>`、`<img>`（文件与 base64 Data URI）、`<ul>`/`<ol>`（嵌套、`start`/`type`）、`<hr>`、`<center>`、`<a>`、`<blockquote>`、`<pre>`/`<code>`、完整 `<table>`（colspan/rowspan、列宽、标题、单元格样式）、`<style>` 样式块（标签/类/ID 选择器）、中文避头尾换行、两端对齐等；多页流式排版使用 `InsertHTMLPaginated`
- **水印** — 通过 `AddWatermarkText` / `AddWatermarkImage` 添加文字和图片水印，支持透明度、旋转、平铺
- **PDF 注释** — 通过 `AddAnnotation` 添加便签、高亮、下划线、删除线、矩形、圆形、自由文本等注释
- **页面操作** — 提取页面（`ExtractPages`）、合并 PDF（`MergePages`）、删除页面（`DeletePage` / `DeletePages`）、复制页面（`CopyPage`）、移动页面（`MovePage`）
- **页面信息查询** — 查询页面尺寸（`GetPageSize`、`GetAllPageSizes`）、源 PDF 页数（`GetSourcePDFPageCount`）
- **页面裁切框** — 通过 `SetPageCropBox` / `GetPageCropBox` / `ClearPageCropBox` 定义页面可见区域
- **页面旋转** — 通过 `SetPageRotation` / `GetPageRotation` 设置页面显示旋转角度
- **页面重排** — 通过 `SelectPages`、`SelectPagesFromFile`、`SelectPagesFromBytes` 重新排列页面顺序
- **嵌入文件** — 通过 `AddEmbeddedFile` 将文件附加到 PDF（在阅读器的附件面板中显示）
- **扩展纸张尺寸** — A0–A10、B0–B10、letter、legal、tabloid、ledger 及横向变体，通过 `PaperSize(name)` 查询
- **几何工具** — `RectFrom`（包含/相交/合并）、`Matrix`（2D 变换）、`Distance`（距离计算）
- **内容元素 CRUD** — 列出、查询、删除、添加、修改、重定位页面上的单个内容元素（文本、图片、线条、矩形、椭圆等），通过 `GetPageElements`、`DeleteElement`、`ModifyTextElement`、`ModifyElementPosition`、`InsertLineElement`、`InsertRectElement`、`ClearPage` 等方法
- **PDF 版本控制** — 通过 `SetPDFVersion` / `GetPDFVersion` 设置输出 PDF 版本（1.4–2.0）
- **垃圾回收** — 通过 `GarbageCollect` 移除已删除的空对象，压缩文档体积
- **页面标签** — 通过 `SetPageLabels` 定义自定义页码编号（罗马数字、字母、十进制，支持前缀）
- **类型化对象 ID** — `ObjID` 包装器，提供类型安全的 PDF 对象引用
- **增量保存** — 通过 `IncrementalSave` 仅写入修改过的对象，大幅提升大文档保存速度
- **XMP 元数据** — 通过 `SetXMPMetadata` 嵌入完整的 XMP 元数据流（Dublin Core、PDF/A 等）
- **文档克隆** — 通过 `Clone` 深拷贝 GoPdf 实例，独立修改互不影响
- **文档清洗** — 通过 `Scrub` 移除敏感元数据、XMP、嵌入文件、页面标签
- **可选内容组（图层）** — 通过 `AddOCG` / `GetOCGs` 添加 PDF 图层，支持选择性显示/隐藏
- **页面布局与页面模式** — 通过 `SetPageLayout` / `SetPageMode` 控制阅读器显示方式
- **文档统计** — 通过 `GetDocumentStats` / `GetFonts` 查看文档结构信息
- **目录/书签** — 通过 `GetTOC` / `SetTOC` 读写层级化的大纲书签树
- **文本提取** — 通过 `ExtractTextFromPage` / `ExtractPageText` 从已有 PDF 中提取带位置信息的文本；`ExtractPageLines` / `GroupIntoTextLines` 按位置重建单词与行（支持逐字形 Tj 流、字距空格、连字、对象流 ObjStm、预定义 CMap 及 GBK/Big5/Shift-JIS/EUC 传统编码）
- **公式渲染** — LaTeX 精简子集（上下标、分数、根号、二项式、希腊字母、常用算符），通过 `DrawFormula` / `MeasureFormula`
- **图片提取** — 通过 `ExtractImagesFromPage` / `ExtractImagesFromAllPages` 从已有 PDF 中提取图片及元数据
- **表单字段 (AcroForm)** — 通过 `AddFormField` / `AddTextField` / `AddCheckbox` / `AddDropdown` 添加交互式表单字段（文本、复选框、下拉框、单选、按钮、签名）
- **数字签名** — 通过 `SignPDF` / `VerifySignature` 进行 PKCS#7 签名与验证
- 绘制线条、椭圆、矩形（支持圆角）、曲线、多边形、折线、扇形
- 插入图片（JPEG、PNG），支持遮罩、裁剪、旋转、透明度
- PDF 密码保护
- 字体字距调整（Kerning）
- 导入已有 PDF 页面
- 表格布局
- 页眉/页脚回调
- Trim-box 支持
- 占位文本（先占位后填充）

## 安装

```bash
go get -u github.com/VantageDataChat/GoPDF2
```

## 快速开始

### 输出文本

```go
package main

import (
    "log"
    "github.com/VantageDataChat/GoPDF2"
)

func main() {
    pdf := gopdf.GoPdf{}
    pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
    pdf.AddPage()

    if err := pdf.AddTTFFont("myfont", "path/to/font.ttf"); err != nil {
        log.Fatal(err)
    }
    if err := pdf.SetFont("myfont", "", 14); err != nil {
        log.Fatal(err)
    }

    pdf.Cell(nil, "你好，世界！")
    pdf.WritePdf("hello.pdf")
}
```

### InsertHTMLBox — 将 HTML 渲染到 PDF

函数签名：

```go
func (gp *GoPdf) InsertHTMLBox(x, y, w, h float64, htmlStr string, opt HTMLBoxOption) (float64, error)
```

示例：

```go
package main

import (
    "log"
    "github.com/VantageDataChat/GoPDF2"
)

func main() {
    pdf := gopdf.GoPdf{}
    pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
    pdf.AddPage()

    // 加载字体 — 仅实际使用的字形会被嵌入（子集化）
    if err := pdf.AddTTFFont("regular", "NotoSansSC-Regular.ttf"); err != nil {
        log.Fatal(err)
    }
    if err := pdf.AddTTFFontWithOption("bold", "NotoSansSC-Bold.ttf", gopdf.TtfOption{Style: gopdf.Bold}); err != nil {
        log.Fatal(err)
    }

    html := `
    <h2>GoPDF2 HTML 渲染示例</h2>
    <p>支持<b>加粗</b>、<i>斜体</i>、<u>下划线</u>和
       <font color="#e74c3c">彩色文字</font>。</p>
    <ul>
        <li>自动换行与分段</li>
        <li>有序/无序列表</li>
        <li>图片插入</li>
    </ul>
    <hr/>
    <p style="font-size:10pt; color:gray">
        字体子集嵌入 — 仅包含文档中实际使用的字符，有效控制文件大小。
    </p>`

    endY, err := pdf.InsertHTMLBox(40, 40, 515, 750, html, gopdf.HTMLBoxOption{
        DefaultFontFamily: "regular",
        DefaultFontSize:   12,
        BoldFontFamily:    "bold",
    })
    if err != nil {
        log.Fatal(err)
    }
    _ = endY // 渲染结束后的 Y 坐标

    pdf.WritePdf("html_example.pdf")
}
```

#### HTMLBoxOption 配置项

| 字段 | 类型 | 说明 |
|---|---|---|
| `DefaultFontFamily` | `string` | HTML 未指定字体时使用的默认字体族（必填） |
| `DefaultFontSize` | `float64` | 默认字号，单位为磅（默认 12） |
| `DefaultColor` | `[3]uint8` | 默认文字颜色 `{R, G, B}` |
| `LineSpacing` | `float64` | 额外行间距，单位为文档单位 |
| `BoldFontFamily` | `string` | `<b>` / `<strong>` 使用的字体族 |
| `ItalicFontFamily` | `string` | `<i>` / `<em>` 使用的字体族 |
| `BoldItalicFontFamily` | `string` | 粗斜体使用的字体族 |

#### 支持的 HTML 标签

| 标签 | 效果 |
|---|---|
| `<b>`、`<strong>` | 加粗 |
| `<i>`、`<em>` | 斜体 |
| `<u>` | 下划线 |
| `<br>` | 换行 |
| `<p>`、`<div>` | 段落 |
| `<h1>` – `<h6>` | 标题 |
| `<font color="..." size="..." face="...">` | 字体样式 |
| `<span style="...">` | 内联 CSS（color、font-size、font-family、font-weight、font-style、text-decoration、text-align） |
| `<img src="..." width="..." height="...">` | 图片（本地文件路径） |
| `<ul>`、`<ol>`、`<li>` | 列表 |
| `<hr>` | 水平线 |
| `<center>` | 居中文本 |
| `<a href="...">` | 链接（蓝色下划线文字） |
| `<blockquote>` | 缩进引用块 |
| `<sub>`、`<sup>` | 下标/上标 |

### 字体子集化与文件大小控制

GoPDF2 默认使用**字体子集化**。调用 `AddTTFFont` 时会解析完整的 TTF 文件，但输出 PDF 时仅嵌入文档中实际使用的字形。这对中日韩字体尤为重要 — 一个 CJK 字体文件可能有 10-20 MB，但生成的 PDF 只会包含所用字符对应的几 KB 数据。

无需额外配置，子集化是自动完成的。

### 文字颜色

```go
// RGB 模式
pdf.SetTextColor(156, 197, 140)
pdf.Cell(nil, "彩色文字")

// CMYK 模式
pdf.SetTextColorCMYK(0, 6, 14, 0)
pdf.Cell(nil, "CMYK 文字")
```

### 插入图片

```go
pdf.Image("gopher.jpg", 200, 50, nil)
```

### 链接

```go
pdf.SetXY(30, 40)
pdf.Text("链接到 example.com")
pdf.AddExternalLink("http://example.com/", 27.5, 28, 125, 15)
```

### 页眉与页脚

```go
pdf.AddHeader(func() {
    pdf.SetY(5)
    pdf.Cell(nil, "页眉")
})
pdf.AddFooter(func() {
    pdf.SetY(825)
    pdf.Cell(nil, "页脚")
})
```

### 绘图

```go
// 线条
pdf.SetLineWidth(2)
pdf.SetLineType("dashed")
pdf.Line(10, 30, 585, 30)

// 椭圆
pdf.Oval(100, 200, 500, 500)

// 多边形
pdf.SetStrokeColor(255, 0, 0)
pdf.SetFillColor(0, 255, 0)
pdf.Polygon([]gopdf.Point{{X: 10, Y: 30}, {X: 585, Y: 200}, {X: 585, Y: 250}}, "DF")

// 圆角矩形
pdf.Rectangle(196.6, 336.8, 398.3, 379.3, "DF", 3, 10)

// 折线（开放路径，不闭合）
pdf.SetStrokeColor(0, 0, 255)
pdf.Polyline([]gopdf.Point{{X: 10, Y: 400}, {X: 100, Y: 350}, {X: 200, Y: 420}, {X: 300, Y: 380}})

// 扇形（饼形）
pdf.SetFillColor(255, 128, 0)
pdf.Sector(300, 500, 80, 0, 90, "FD")
```

### 旋转

```go
pdf.Rotate(270.0, 100.0, 100.0)
pdf.Text("旋转文字")
pdf.RotateReset()
```

### 透明度

```go
pdf.SetTransparency(gopdf.Transparency{Alpha: 0.5, BlendModeType: ""})
```

### 密码保护

```go
pdf.Start(gopdf.Config{
    PageSize: *gopdf.PageSizeA4,
    Protection: gopdf.PDFProtectionConfig{
        UseProtection: true,
        Permissions:   gopdf.PermissionsPrint | gopdf.PermissionsCopy | gopdf.PermissionsModify,
        OwnerPass:     []byte("owner"),
        UserPass:      []byte("user"),
    },
})
```

### 导入已有 PDF

```go
tpl := pdf.ImportPage("existing.pdf", 1, "/MediaBox")
pdf.UseImportedTemplate(tpl, 50, 100, 400, 0)
```

### 打开并修改已有 PDF

`OpenPDF` 加载已有 PDF，可以在每一页上叠加新内容：

```go
pdf := gopdf.GoPdf{}
err := pdf.OpenPDF("input.pdf", nil)
if err != nil {
    log.Fatal(err)
}

pdf.AddTTFFont("myfont", "font.ttf")
pdf.SetFont("myfont", "", 14)

// 在第 1 页绘制
pdf.SetPage(1)
pdf.SetXY(100, 100)
pdf.Cell(nil, "水印文字")

// 在第 2 页绘制
pdf.SetPage(2)
pdf.SetXY(200, 200)
pdf.Image("stamp.png", 200, 200, nil)

pdf.WritePdf("output.pdf")
```

也可使用 `OpenPDFFromBytes(data, opt)` 和 `OpenPDFFromStream(rs, opt)`。

### 表格

```go
table := pdf.NewTableLayout(10, 10, 25, 5)
table.AddColumn("编号", 50, "left")
table.AddColumn("描述", 200, "left")
table.AddRow([]string{"001", "产品 A"})
table.DrawTable()
```

### 占位文本

```go
pdf.PlaceHolderText("total", 30)
// ... 创建完所有页面后 ...
pdf.FillInPlaceHoldText("total", "5", gopdf.Left)
```

### 水印

为 PDF 页面添加文字或图片水印：

```go
// 单个居中文字水印
pdf.SetPage(1)
pdf.AddWatermarkText(gopdf.WatermarkOption{
    Text:       "机密",
    FontFamily: "myfont",
    FontSize:   48,
    Opacity:    0.3,
    Angle:      45,
    Color:      [3]uint8{200, 200, 200},
})

// 平铺文字水印
pdf.AddWatermarkText(gopdf.WatermarkOption{
    Text:       "草稿",
    FontFamily: "myfont",
    Repeat:     true,
})

// 为所有页面添加文字水印
pdf.AddWatermarkTextAllPages(gopdf.WatermarkOption{
    Text:       "样本",
    FontFamily: "myfont",
})

// 图片水印（居中，30% 透明度）
pdf.AddWatermarkImage("logo.png", 0.3, 200, 200, 0)
```

### 注释

添加 PDF 注释（便签、高亮、形状、自由文本）：

```go
// 便签注释
pdf.AddTextAnnotation(100, 100, "审阅者", "请检查此部分。")

// 高亮注释
pdf.AddHighlightAnnotation(50, 50, 200, 20, [3]uint8{255, 255, 0})

// 自由文本注释
pdf.AddFreeTextAnnotation(100, 200, 250, 30, "重要备注", 14)

// 完整控制
pdf.AddAnnotation(gopdf.AnnotationOption{
    Type:    gopdf.AnnotSquare,
    X:       50,
    Y:       300,
    W:       100,
    H:       50,
    Color:   [3]uint8{0, 0, 255},
    Content: "审阅区域",
})
```

### 页面操作

提取、合并、删除、复制页面：

```go
// 从 PDF 中提取指定页面
newPdf, _ := gopdf.ExtractPages("input.pdf", []int{1, 3, 5}, nil)
newPdf.WritePdf("pages_1_3_5.pdf")

// 合并多个 PDF
merged, _ := gopdf.MergePages([]string{"doc1.pdf", "doc2.pdf"}, nil)
merged.WritePdf("merged.pdf")

// 删除页面（从 1 开始）
pdf.DeletePage(2)

// 批量删除多个页面
pdf.DeletePages([]int{2, 4, 6})

// 移动页面到新位置
pdf.MovePage(3, 1) // 将第 3 页移动到第 1 页

// 复制页面到末尾
newPageNo, _ := pdf.CopyPage(1)
```

### 页面信息查询

查询页面尺寸和元数据：

```go
// 获取指定页面的尺寸
w, h, _ := pdf.GetPageSize(1)

// 获取所有页面尺寸
sizes := pdf.GetAllPageSizes()

// 获取源 PDF 的页数（无需导入）
count, _ := gopdf.GetSourcePDFPageCount("input.pdf")

// 获取源 PDF 的页面尺寸
pageSizes, _ := gopdf.GetSourcePDFPageSizes("input.pdf")
```

### 纸张尺寸

通过名称查询预定义纸张尺寸：

```go
// 按名称查询纸张尺寸（不区分大小写）
size := gopdf.PaperSize("a5")
pdf.Start(gopdf.Config{PageSize: *size})

// 横向变体
sizeL := gopdf.PaperSize("a4-l")

// 支持：a0–a10、b0–b10、letter、legal、tabloid、ledger、
// statement、executive、folio、quarto（加 "-l" 后缀为横向）
names := gopdf.PaperSizeNames()
```

### 页面旋转

设置页面显示旋转角度（不修改页面内容）：

```go
pdf.SetPageRotation(1, 90)   // 第 1 页顺时针旋转 90°
pdf.SetPageRotation(2, 180)  // 第 2 页旋转 180°

angle, _ := pdf.GetPageRotation(1) // 返回 90
```

### 页面裁切框

定义页面的可见区域（区域外的内容被裁切但不会被删除）：

```go
// 设置第 1 页的裁切框 — 仅定义区域内的内容可见
pdf.SetPageCropBox(1, gopdf.Box{Left: 50, Top: 50, Right: 545, Bottom: 792})

// 获取当前裁切框
box, _ := pdf.GetPageCropBox(1)

// 移除裁切框，恢复完整 MediaBox 可见区域
pdf.ClearPageCropBox(1)
```

### 页面重排

重新排列、复制或筛选页面：

```go
// 反转当前文档的页面顺序
newPdf, _ := pdf.SelectPages([]int{3, 2, 1})
newPdf.WritePdf("reversed.pdf")

// 从文件中选择指定页面
newPdf, _ = gopdf.SelectPagesFromFile("input.pdf", []int{1, 3, 5}, nil)

// 复制页面
newPdf, _ = pdf.SelectPages([]int{1, 1, 1})
```

### 嵌入文件

将文件附加到 PDF（在阅读器的附件面板中显示）：

```go
data, _ := os.ReadFile("report.csv")
pdf.AddEmbeddedFile(gopdf.EmbeddedFile{
    Name:        "report.csv",
    Content:     data,
    MimeType:    "text/csv",
    Description: "月度报告数据",
})
```

### 内容元素 CRUD

列出、查询、删除、修改、添加页面上的单个内容元素：

```go
// 列出第 1 页的所有元素
elements, _ := pdf.GetPageElements(1)
for _, e := range elements {
    fmt.Printf("[%d] %s 位于 (%.1f, %.1f)\n", e.Index, e.Type, e.X, e.Y)
}

// 仅获取文本元素
texts, _ := pdf.GetPageElementsByType(1, gopdf.ElementText)

// 按索引删除指定元素
pdf.DeleteElement(1, 0)

// 删除页面上所有线条
removed, _ := pdf.DeleteElementsByType(1, gopdf.ElementLine)

// 删除矩形区域内的元素
pdf.DeleteElementsInRect(1, 0, 0, 100, 100)

// 清空页面所有内容
pdf.ClearPage(1)

// 修改文本内容
pdf.ModifyTextElement(1, 0, "新文本")

// 移动元素到新位置
pdf.ModifyElementPosition(1, 0, 200, 300)

// 在已有页面上插入新元素
pdf.InsertLineElement(1, 10, 400, 500, 400)
pdf.InsertRectElement(1, 50, 420, 200, 50, "DF")
pdf.InsertOvalElement(1, 300, 420, 450, 470)
```

### PDF 版本控制

设置输出 PDF 版本：

```go
pdf.SetPDFVersion(gopdf.PDFVersion20) // 输出 PDF 2.0
v := pdf.GetPDFVersion()              // 返回 PDFVersion20
```

### 垃圾回收

移除已删除的空对象以减小文件体积：

```go
pdf.DeletePage(2)
removed := pdf.GarbageCollect(gopdf.GCCompact)
fmt.Printf("移除了 %d 个无用对象\n", removed)
```

### 页面标签

定义 PDF 阅读器中显示的自定义页码编号：

```go
pdf.SetPageLabels([]gopdf.PageLabel{
    {PageIndex: 0, Style: gopdf.PageLabelRomanLower, Start: 1},  // i, ii, iii
    {PageIndex: 3, Style: gopdf.PageLabelDecimal, Start: 1},     // 1, 2, 3, ...
    {PageIndex: 10, Style: gopdf.PageLabelAlphaUpper, Prefix: "附录 ", Start: 1},
})
```

### XMP 元数据

嵌入丰富的 XMP 元数据（Dublin Core、PDF/A 合规等）：

```go
pdf.SetXMPMetadata(gopdf.XMPMetadata{
    Title:       "2025 年度报告",
    Creator:     []string{"张三"},
    Description: "公司年度财务报告",
    Subject:     []string{"财务", "报告"},
    CreatorTool: "GoPDF2",
    Producer:    "GoPDF2",
    CreateDate:  time.Now(),
    ModifyDate:  time.Now(),
    PDFAPart:    1,
    PDFAConformance: "B",
})
```

### 增量保存

仅保存修改过的对象，大文档更新更快：

```go
originalData, _ := os.ReadFile("input.pdf")
pdf := gopdf.GoPdf{}
pdf.OpenPDFFromBytes(originalData, nil)
pdf.SetPage(1)
pdf.SetXY(100, 100)
pdf.Text("新增文字")

result, _ := pdf.IncrementalSave(originalData, nil)
os.WriteFile("output.pdf", result, 0644)
```

### 文档克隆

深拷贝文档实例，独立修改互不影响：

```go
clone, _ := pdf.Clone()
clone.SetPage(1)
clone.SetXY(100, 100)
clone.Text("仅在克隆中")
clone.WritePdf("clone.pdf")
```

### 文档清洗

移除 PDF 中的敏感元数据和附件：

```go
pdf.Scrub(gopdf.DefaultScrubOption())
pdf.GarbageCollect(gopdf.GCCompact)
pdf.WritePdf("scrubbed.pdf")

// 选择性清洗
pdf.Scrub(gopdf.ScrubOption{
    Metadata:    true,
    XMLMetadata: true,
})
```

### 可选内容组（图层）

添加 PDF 图层，支持选择性显示/隐藏：

```go
watermark := pdf.AddOCG(gopdf.OCG{
    Name:   "水印",
    Intent: gopdf.OCGIntentView,
    On:     true,
})

draft := pdf.AddOCG(gopdf.OCG{
    Name:   "草稿备注",
    Intent: gopdf.OCGIntentDesign,
    On:     false,
})

layers := pdf.GetOCGs()
```

### 页面布局与页面模式

控制 PDF 阅读器的显示方式：

```go
// 设置页面布局
pdf.SetPageLayout(gopdf.PageLayoutTwoColumnLeft)
layout := pdf.GetPageLayout()

// 设置页面模式（打开时显示哪个面板）
pdf.SetPageMode(gopdf.PageModeUseOutlines) // 显示书签面板
mode := pdf.GetPageMode()
```

### 文档统计

查看文档结构信息：

```go
stats := pdf.GetDocumentStats()
fmt.Printf("页数: %d, 字体: %d, 图片: %d\n",
    stats.PageCount, stats.FontCount, stats.ImageCount)

fonts := pdf.GetFonts()
for _, f := range fonts {
    fmt.Printf("字体: %s, 已嵌入: %v\n", f.Family, f.IsEmbedded)
}
```

### 目录/书签

读写层级化的大纲书签树：

```go
// 设置层级化目录
pdf.SetTOC([]gopdf.TOCItem{
    {Level: 1, Title: "第一章", PageNo: 1},
    {Level: 2, Title: "第 1.1 节", PageNo: 1, Y: 200},
    {Level: 2, Title: "第 1.2 节", PageNo: 2},
    {Level: 1, Title: "第二章", PageNo: 3},
})

// 读取目录
toc := pdf.GetTOC()
for _, item := range toc {
    fmt.Printf("L%d: %s -> 第 %d 页\n", item.Level, item.Title, item.PageNo)
}
```

## API 参考

参见 [docs/API.md](docs/API.md)（English）或 [docs/API_zh.md](docs/API_zh.md)（中文）。

## GoPDF2 与 PyMuPDF 对比

参见 [docs/COMPARISON.md](docs/COMPARISON.md)（English）或 [docs/COMPARISON_zh.md](docs/COMPARISON_zh.md)（中文），了解两者的详细功能差异。

## 更新日志

参见 [CHANGELOG.md](CHANGELOG.md) 了解版本历史。

## 许可证

MIT
