# GoPDF2 vs GoPDF vs PyMuPDF 功能对比

**[English](COMPARISON.md) | [中文](COMPARISON_zh.md)**

本文档详细对比 GoPDF2（纯 Go，GoPDF 的演进版本）、GoPDF（signintech/gopdf，纯 Go）与 PyMuPDF（基于 MuPDF C 库的 Python 绑定）的功能差异。

> GoPDF2 是从 GoPDF 演进而来的纯 Go 实现，无 CGO 依赖。GoPDF（signintech/gopdf）是原始的纯 Go PDF 生成库。PyMuPDF 通过 CGO 绑定 MuPDF C 库，可调用底层渲染引擎。
> 三者定位不同：GoPDF2 专注于 PDF 生成、编辑与操作；GoPDF 专注于 PDF 生成；PyMuPDF 侧重于 PDF 解析、提取与渲染。

---

## 功能对比总览

| 功能类别 | GoPDF2 | GoPDF | PyMuPDF | 说明 |
|---------|--------|-------|---------|------|
| **纯 Go / 无 CGO** | ✅ | ✅ | ❌ | GoPDF2 和 GoPDF 无需 C 编译器 |
| **PDF 生成** | ✅ | ✅ | ✅ | 三者均可从零创建 PDF |
| **打开/修改已有 PDF** | ✅ | ❌（仅导入页面） | ✅ | GoPDF 仅能通过 gofpdi 导入页面 |
| **HTML 渲染到 PDF** | ✅ | ❌ | ✅ | GoPDF2 内置 HTML 解析器 |
| **文本提取** | ✅ | ❌ | ✅ | GoPDF2 内置纯 Go 内容流解析器 |
| **页面渲染（光栅化）** | ❌ | ❌ | ✅ | 需要 MuPDF 渲染引擎 |
| **OCR 文字识别** | ❌ | ❌ | ✅ | 需要 Tesseract + MuPDF |
| **SVG/HTML/EPUB 转换** | ❌ | ❌ | ✅ | 需要 MuPDF 多格式支持 |
| **图片提取** | ✅ | ❌ | ✅ | GoPDF2 内置纯 Go 图片提取 |
| **表单字段 (AcroForm)** | ✅ | ❌ | ✅ | GoPDF2 支持文本、复选框、下拉框、单选、按钮、签名字段 |
| **数字签名** | ✅ | ❌ | ✅ | GoPDF2 支持 PKCS#7 签名与验证 |

---

## 详细功能对比

### 1. 文档生命周期

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 创建空白 PDF | `Start(Config)` | `Start(Config)` | `Document()` |
| 从文件打开 | `OpenPDF(path)` | ❌ | `Document(path)` |
| 从内存打开 | `OpenPDFFromBytes(data)` | ❌ | `Document(stream=data)` |
| 从流打开 | `OpenPDFFromStream(rs)` | ❌ | `Document(stream=data)` |
| 保存到文件 | `WritePdf(path)` | `WritePdf(path)` | `save(path)` |
| 保存到内存 | `GetBytesPdf()` | `GetBytesPdf()` | `tobytes()` |
| 增量保存 | ✅ `IncrementalSave()` | ❌ | ✅ `saveIncr()` |
| 文档克隆 | ✅ `Clone()` | ❌ | ❌（需手动序列化） |
| 关闭文档 | 自动 GC | 自动 GC | `close()` |
| 打开加密 PDF | ✅ `OpenPDF(Password)` | ❌ | ✅ `authenticate()` |
| 多格式支持 (XPS/EPUB/HTML) | ❌ | ❌ | ✅ |

### 2. 页面管理

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 添加新页面 | ✅ `AddPage()` | ✅ `AddPage()` | ✅ `new_page()` |
| 删除页面 | ✅ `DeletePage(n)` | ❌ | ✅ `delete_page(n)` |
| 批量删除页面 | ✅ `DeletePages()` | ❌ | ✅ `delete_pages()` |
| 复制页面（引用） | ✅ `CopyPage(n)` | ❌ | ✅ `copy_page()` |
| 完整复制页面 | ✅ `CopyPage(n)` | ❌ | ✅ `fullcopy_page()` |
| 移动页面 | ✅ `MovePage()` | ❌ | ✅ `move_page()` |
| 页面重排/选择 | ✅ `SelectPages()` | ❌ | ✅ `select()` |
| 提取页面到新文档 | ✅ `ExtractPages()` | ❌ | ✅ `select()` + `save()` |
| 合并多个 PDF | ✅ `MergePages()` | ❌ | ✅ `insert_pdf()` |
| 插入其他 PDF 页面 | ✅ `ImportPage()` | ✅ `ImportPage()` | ✅ `insert_pdf()` |
| 插入任意格式文档 | ❌ | ❌ | ✅ `insert_file()` |
| 页面旋转 | ✅ `SetPageRotation()` | ❌ | ✅ `set_rotation()` |
| 获取页面尺寸 | ✅ `GetPageSize()` | ❌ | ✅ `page.rect` |
| 获取所有页面尺寸 | ✅ `GetAllPageSizes()` | ❌ | 需遍历 |
| 获取源 PDF 页数 | ✅ `GetSourcePDFPageCount()` | ❌ | ✅ `page_count` |
| 页面裁切框 | ✅ `SetPageCropBox()` | ❌ | ✅ `page_cropbox()` |
| 章节导航 (EPUB) | ❌ | ❌ | ✅ `chapter_page_count()` 等 |

### 3. 文本与字体

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 写入文本 | ✅ `Cell()` / `Text()` | ✅ `Cell()` / `Text()` | ✅ `insert_text()` |
| 多行文本 | ✅ `MultiCell()` | ✅ `MultiCell()` | ✅ `insert_textbox()` |
| 文本测量 | ✅ `MeasureTextWidth()` | ✅ `MeasureTextWidth()` | ✅ `get_text_length()` |
| 自动换行 | ✅ `SplitTextWithWordWrap()` | ✅ `SplitTextWithWordWrap()` | ✅ 内置 |
| TTF 字体嵌入 | ✅ 自动子集化 | ✅ 自动子集化 | ✅ |
| 字体子集化 | ✅ 自动 | ✅ 自动 | ✅ `subset_fonts()` |
| 字距调整 (Kerning) | ✅ | ✅ | ✅ |
| 文本提取 | ✅ `ExtractPageText()` | ❌ | ✅ `get_text()` |
| 文本搜索 | ✅ `SearchText()` | ❌ | ✅ `search_for()` |
| 文本块/字典提取 | ✅ `ExtractTextFromPage()` | ❌ | ✅ `get_text("dict")` |
| 字体信息查询 | ✅ `GetFonts()` | ❌ | ✅ `get_page_fonts()` |
| 字体提取 | ✅ `ExtractFontsFromPage()` | ❌ | ✅ `extract_font()` |
| CJK 字体支持 | ✅ | ✅ | ✅ |

### 4. 绘图

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 线条 | ✅ `Line()` | ✅ `Line()` | ✅ `draw_line()` |
| 矩形 | ✅ `Rectangle()` | ✅ `Rectangle()` | ✅ `draw_rect()` |
| 圆角矩形 | ✅ | ✅ | ❌（需手动绘制） |
| 椭圆/圆 | ✅ `Oval()` | ✅ `Oval()` | ✅ `draw_circle()` / `draw_oval()` |
| 多边形 | ✅ `Polygon()` | ✅ `Polygon()` | ✅ `draw_polygon()` |
| 贝塞尔曲线 | ✅ `Curve()` | ✅ `Curve()` | ✅ `draw_bezier()` |
| 折线 | ✅ `Polyline()` | ❌ | ✅ `draw_polyline()` |
| 扇形 | ✅ `Sector()` | ❌ | ✅ `draw_sector()` |
| 虚线/线型 | ✅ `SetLineType()` | ✅ `SetLineType()` | ✅ |
| 填充色/描边色 | ✅ | ✅ | ✅ |
| 旋转 | ✅ `Rotate()` | ✅ `Rotate()` | ✅ |
| 透明度 | ✅ `SetTransparency()` | ✅ `SetTransparency()` | ✅ |
| 图形状态保存/恢复 | ✅ `SaveGraphicsState()` | ✅ `SaveGraphicsState()` | ✅ |

### 5. 图片

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 插入图片 (JPEG/PNG) | ✅ `Image()` | ✅ `Image()` | ✅ `insert_image()` |
| 从内存插入图片 | ✅ `ImageFrom()` | ✅ `ImageFrom()` | ✅ |
| 图片遮罩/裁剪 | ✅ | ✅ | ✅ |
| 图片旋转 | ✅ | ❌ | ✅ |
| 图片透明度 | ✅ | ✅ | ✅ |
| 图片提取 | ✅ `ExtractImagesFromPage()` | ❌ | ✅ `extract_image()` |
| 图片信息查询 | ✅ `ExtractImagesFromPage()` | ❌ | ✅ `get_page_images()` |
| 图片删除 | ✅ `DeleteImages()` | ❌ | ✅ `delete_image()` |
| 图片重压缩 | ✅ `RecompressImages()` | ❌ | ✅ `rewrite_images()` |
| SVG 插入 | ✅ `ImageSVG()` | ❌ | ✅ |
| Pixmap 渲染 | ✅ `RenderPageToImage()` | ❌ | ✅ `get_pixmap()` |

### 6. 注释 (Annotations)

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 文本注释（便签） | ✅ `AddTextAnnotation()` | ❌ | ✅ `add_text_annot()` |
| 高亮 | ✅ `AddHighlightAnnotation()` | ❌ | ✅ `add_highlight_annot()` |
| 下划线 | ✅ | ❌ | ✅ `add_underline_annot()` |
| 删除线 | ✅ | ❌ | ✅ `add_strikeout_annot()` |
| 矩形注释 | ✅ | ❌ | ✅ `add_rect_annot()` |
| 圆形注释 | ✅ | ❌ | ✅ `add_circle_annot()` |
| 自由文本 | ✅ `AddFreeTextAnnotation()` | ❌ | ✅ `add_freetext_annot()` |
| 墨迹注释 | ✅ `AddInkAnnotation()` | ❌ | ✅ `add_ink_annot()` |
| 折线注释 | ✅ `AddPolylineAnnotation()` | ❌ | ✅ `add_polyline_annot()` |
| 多边形注释 | ✅ `AddPolygonAnnotation()` | ❌ | ✅ `add_polygon_annot()` |
| 线条注释 | ✅ `AddLineAnnotation()` | ❌ | ✅ `add_line_annot()` |
| 图章注释 | ✅ `AddStampAnnotation()` | ❌ | ✅ `add_stamp_annot()` |
| 波浪线注释 | ✅ `AddSquigglyAnnotation()` | ❌ | ✅ `add_squiggly_annot()` |
| 插入符注释 | ✅ `AddCaretAnnotation()` | ❌ | ✅ `add_caret_annot()` |
| 文件附件注释 | ✅ `AddFileAttachmentAnnotation()` | ❌ | ✅ `add_file_annot()` |
| 涂改注释 (Redaction) | ✅ `AddRedactAnnotation()` | ❌ | ✅ `add_redact_annot()` |
| 删除注释 | ✅ `DeleteAnnotation()` | ❌ | ✅ `delete_annot()` |
| 修改注释属性 | ✅ `ModifyAnnotation()` | ❌ | ✅ Annot 类方法 |
| 注释遍历 | ✅ `GetAnnotations()` | ❌ | ✅ `annots()` |
| 应用涂改 | ✅ `ApplyRedactions()` | ❌ | ✅ `apply_redactions()` |

### 7. 水印

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 文字水印 | ✅ `AddWatermarkText()` | ❌ | 需手动实现 |
| 图片水印 | ✅ `AddWatermarkImage()` | ❌ | 需手动实现 |
| 全页面水印 | ✅ `AddWatermarkTextAllPages()` | ❌ | 需手动实现 |
| 平铺水印 | ✅ `Repeat: true` | ❌ | 需手动实现 |
| 水印透明度/旋转 | ✅ | ❌ | 需手动实现 |

> GoPDF2 提供开箱即用的水印 API。GoPDF 不支持水印功能。PyMuPDF 需要通过绘图 API 手动组合实现。

### 8. 书签/目录 (TOC/Outlines)

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 获取目录 | ✅ `GetTOC()` | ❌ | ✅ `get_toc()` |
| 设置目录 | ✅ `SetTOC()` | ❌ | ✅ `set_toc()` |
| 层级化目录 | ✅ | ❌ | ✅ |
| 添加单个书签 | ✅ `AddOutline()` | ✅ `AddOutline()` | ✅ |
| 修改单个书签 | ✅ `ModifyBookmark()` | ❌ | ✅ `set_toc_item()` |
| 删除单个书签 | ✅ `DeleteBookmark()` | ❌ | ✅ `del_toc_item()` |
| 书签颜色/粗体/斜体 | ✅ `SetBookmarkStyle()` | ❌ | ✅ |
| 书签折叠控制 | ✅ `SetBookmarkStyle()` | ❌ | ✅ `collapse` 参数 |

### 9. 元数据

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 标准元数据 (Info) | ✅ `SetInfo()` | ❌ | ✅ `set_metadata()` |
| XMP 元数据 | ✅ `SetXMPMetadata()` | ❌ | ✅ `set_xml_metadata()` |
| 获取 XMP 元数据 | ✅ `GetXMPMetadata()` | ❌ | ✅ `get_xml_metadata()` |
| PDF/A 合规 | ✅ XMP 中设置 | ❌ | ✅ |
| Dublin Core | ✅ | ❌ | ✅ |

### 10. 嵌入文件

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 添加嵌入文件 | ✅ `AddEmbeddedFile()` | ❌ | ✅ `embfile_add()` |
| 获取嵌入文件 | ✅ `GetEmbeddedFile()` | ❌ | ✅ `embfile_get()` |
| 删除嵌入文件 | ✅ `DeleteEmbeddedFile()` | ❌ | ✅ `embfile_del()` |
| 更新嵌入文件 | ✅ `UpdateEmbeddedFile()` | ❌ | ✅ `embfile_upd()` |
| 嵌入文件信息 | ✅ `GetEmbeddedFileInfo()` | ❌ | ✅ `embfile_info()` |
| 嵌入文件列表 | ✅ `GetEmbeddedFileNames()` | ❌ | ✅ `embfile_names()` |
| 嵌入文件数量 | ✅ `GetEmbeddedFileCount()` | ❌ | ✅ `embfile_count()` |

### 11. 可选内容组 (OCG/图层)

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 添加 OCG | ✅ `AddOCG()` | ❌ | ✅ `add_ocg()` |
| 获取所有 OCG | ✅ `GetOCGs()` | ❌ | ✅ `get_ocgs()` |
| 图层配置 | ✅ `AddLayerConfig()` / `GetLayerConfigs()` | ❌ | ✅ `add_layer()` / `get_layers()` |
| 切换图层 | ✅ `SwitchLayer()` | ❌ | ✅ `switch_layer()` |
| OCMD（成员字典） | ✅ `AddOCMD()` / `GetOCMD()` | ❌ | ✅ `set_ocmd()` / `get_ocmd()` |
| 图层 UI 配置 | ✅ `SetLayerUIConfig()` | ❌ | ✅ `layer_ui_configs()` |
| 批量设置 OCG 状态 | ✅ `SetOCGStates()` | ❌ | ✅ `set_layer()` |

### 12. 页面布局与显示

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 页面布局 | ✅ `SetPageLayout()` | ❌ | ✅ `set_pagelayout()` |
| 页面模式 | ✅ `SetPageMode()` | ❌ | ✅ `set_pagemode()` |
| MarkInfo | ✅ `SetMarkInfo()` | ❌ | ✅ `set_markinfo()` |
| 页面标签 | ✅ `SetPageLabels()` | ❌ | ✅ `set_page_labels()` |
| 获取页面标签 | ✅ `GetPageLabels()` | ❌ | ✅ `get_page_labels()` |
| 按标签查找页面 | ✅ `FindPagesByLabel()` | ❌ | ✅ `get_page_numbers()` |

### 13. 安全与加密

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 密码保护 | ✅ `PDFProtectionConfig` | ✅ `SetProtection()` | ✅ `save(encryption=...)` |
| 权限控制 | ✅ | ✅ | ✅ |
| 打开加密 PDF | ✅ `OpenPDF(Password)` | ❌ | ✅ `authenticate()` |
| 加密方法选择 | ✅ RC4 (V1/V2)、AES-128、AES-256 | ✅ RC4 | ✅ 多种加密标准 |
| 数字签名 | ✅ `SignPDF()` / `VerifySignature()` | ❌ | ✅ `get_sigflags()` |

### 14. 文档清洗与优化

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 文档清洗 | ✅ `Scrub()` | ❌ | ✅ `scrub()` |
| 垃圾回收 | ✅ `GarbageCollect()` | ❌ | ✅ `save(garbage=N)` |
| 对象去重 | ✅ `GCDedup` | ❌ | ✅ `save(garbage=3)` |
| 文档统计 | ✅ `GetDocumentStats()` | ❌ | 需手动统计 |
| 线性化 (Web 优化) | ✅ `Linearize()` | ❌ | ✅ `save(linear=True)` |
| 内容流清理 | ✅ `CleanContentStreams()` | ❌ | ✅ `save(clean=True)` |
| 图片重压缩 | ✅ `RecompressImages()` | ❌ | ✅ `rewrite_images()` |
| 颜色空间转换 | ✅ `ConvertColorspace()` | ❌ | ✅ `recolor()` |

### 15. 内容元素操作

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 列出页面元素 | ✅ `GetPageElements()` | ❌ | ❌（需解析内容流） |
| 按类型筛选元素 | ✅ `GetPageElementsByType()` | ❌ | ❌ |
| 删除单个元素 | ✅ `DeleteElement()` | ❌ | ❌ |
| 按类型删除元素 | ✅ `DeleteElementsByType()` | ❌ | ❌ |
| 区域删除元素 | ✅ `DeleteElementsInRect()` | ❌ | ❌ |
| 清空页面 | ✅ `ClearPage()` | ❌ | ❌（需重写内容流） |
| 修改文本元素 | ✅ `ModifyTextElement()` | ❌ | ❌ |
| 修改元素位置 | ✅ `ModifyElementPosition()` | ❌ | ❌ |
| 插入新元素 | ✅ `InsertLineElement()` 等 | ❌ | ❌ |

> GoPDF2 的内容元素 CRUD 是独有功能，因为 GoPDF2 在内存中维护结构化的内容缓存，可以直接操作单个元素。GoPDF 和 PyMuPDF 均无法进行元素级操作。

### 16. PDF 底层操作

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| PDF 版本控制 | ✅ `SetPDFVersion()` | ❌ | 通过 header 控制 |
| 类型化对象 ID | ✅ `ObjID` | ❌ | ✅ xref 系统 |
| 对象数量统计 | ✅ `GetObjectCount()` | ❌ | ✅ `xref_length()` |
| 读取对象定义 | ✅ `ReadObject()` | ❌ | ✅ `xref_object()` |
| 修改对象定义 | ✅ `UpdateObject()` | ❌ | ✅ `update_object()` |
| 读取/写入字典键 | ✅ `GetDictKey()` / `SetDictKey()` | ❌ | ✅ `xref_get_key()` / `xref_set_key()` |
| 读取/写入流数据 | ✅ `GetStream()` / `SetStream()` | ❌ | ✅ `xref_stream()` / `update_stream()` |
| 复制对象 | ✅ `CopyObject()` | ❌ | ✅ `xref_copy()` |
| PDF Catalog 访问 | ✅ `GetCatalog()` | ❌ | ✅ `pdf_catalog()` |
| PDF Trailer 访问 | ✅ `GetTrailer()` | ❌ | ✅ `pdf_trailer()` |

### 17. 日志/撤销 (Journalling)

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 启用日志 | ✅ `JournalEnable()` | ❌ | ✅ `journal_enable()` |
| 撤销/重做 | ✅ `JournalUndo()` / `JournalRedo()` | ❌ | ✅ `journal_undo()` / `journal_redo()` |
| 保存/加载日志 | ✅ `JournalSave()` / `JournalLoad()` | ❌ | ✅ `journal_save()` / `journal_load()` |
| 操作命名 | ✅ `JournalStartOp()` | ❌ | ✅ `journal_start_op()` |

### 18. 表单字段 (Widgets/AcroForm)

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 添加表单字段 | ✅ `AddFormField()` | ❌ | ✅ `add_widget()` |
| 添加文本字段 | ✅ `AddTextField()` | ❌ | ✅ `add_widget()` |
| 添加复选框 | ✅ `AddCheckbox()` | ❌ | ✅ `add_widget()` |
| 添加下拉框 | ✅ `AddDropdown()` | ❌ | ✅ `add_widget()` |
| 添加签名字段 | ✅ `AddSignatureField()` | ❌ | ✅ `add_widget()` |
| 获取表单字段 | ✅ `GetFormFields()` | ❌ | ✅ Widget 类 |
| 删除表单字段 | ✅ `DeleteFormField()` | ❌ | ✅ `delete_widget()` |
| 修改表单值 | ✅ `ModifyFormFieldValue()` | ❌ | ✅ Widget 类 |
| 表单 PDF 检测 | ✅ `IsFormPDF()` | ❌ | ✅ `is_form_pdf` |
| 固化注释/表单 | ✅ `BakeAnnotations()` | ❌ | ✅ `bake()` |

### 19. 其他功能

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| HTML 渲染 | ✅ `InsertHTMLBox()` | ❌ | ✅ Story 类 |
| 表格布局 | ✅ `NewTableLayout()` | ✅ `NewTableLayout()` | ❌（需手动实现） |
| 页眉/页脚回调 | ✅ `AddHeader()` / `AddFooter()` | ✅ `AddHeader()` / `AddFooter()` | ❌（需手动实现） |
| 占位文本 | ✅ `PlaceHolderText()` | ✅ `PlaceHolderText()` | ❌ |
| 纸张尺寸查询 | ✅ `PaperSize()` | ❌ | ✅ `paper_size()` |
| 几何工具 | ✅ `RectFrom` / `Matrix` | ❌ | ✅ `Rect` / `Matrix` |
| 链接 | ✅ `AddExternalLink()` | ✅ `AddExternalLink()` | ✅ `insert_link()` |
| 锚点 | ✅ `SetAnchor()` | ✅ `SetAnchor()` | ✅ |
| 字体容器复用 | ✅ `FontContainer` | ✅ `FontContainer` | ❌ |
| 压缩级别控制 | ✅ `SetCompressLevel()` | ✅ `SetCompressLevel()` | ✅ `save(deflate=True)` |
| Trim-box | ✅ | ❌ | ✅ |
| 阿拉伯文支持 | ✅ | ✅ | ✅ |
| CMYK 颜色 | ✅ | ✅ | ✅ |
| 裁剪多边形 | ✅ | ✅ | ✅ |

### 20. 链接管理

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 添加外部链接 | ✅ `AddExternalLink()` | ✅ `AddExternalLink()` | ✅ `insert_link()` |
| 添加内部链接 | ✅ `AddInternalLink()` | ❌ | ✅ `insert_link()` |
| 获取页面链接 | ✅ `GetLinks()` / `GetLinksOnPage()` | ❌ | ✅ `get_links()` |
| 删除链接 | ✅ `DeleteLink()` / `DeleteLinkOnPage()` | ❌ | ✅ `delete_link()` |
| 删除所有链接 | ✅ `DeleteAllLinks()` / `DeleteAllLinksOnPage()` | ❌ | ✅ 需手动遍历 |
| 从 PDF 提取链接 | ✅ `ExtractLinks()` / `ExtractLinksFromPage()` | ❌ | ✅ `get_links()` |

### 21. 文本替换

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 搜索并替换文本 | ✅ `ReplaceText()` | ❌ | ❌（需手动实现） |
| 大小写不敏感替换 | ✅ `CaseInsensitive` 选项 | ❌ | ❌ |
| 指定页面替换 | ✅ `Pages` 选项 | ❌ | ❌ |
| 最大替换次数限制 | ✅ `MaxReplacements` 选项 | ❌ | ❌ |
| 十六进制字符串替换 | ✅ | ❌ | ❌ |

> 文本替换是 GoPDF2 的独有功能，直接操作原始内容流，支持字面字符串和十六进制编码字符串。

### 22. 文本提取格式

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 纯文本 | ✅ `FormatText` | ❌ | ✅ `get_text("text")` |
| 文本块 | ✅ `FormatBlocks` | ❌ | ✅ `get_text("blocks")` |
| 带位置的单词 | ✅ `FormatWords` | ❌ | ✅ `get_text("words")` |
| HTML 输出 | ✅ `FormatHTML` | ❌ | ✅ `get_text("html")` |
| JSON 输出 | ✅ `FormatJSON` | ❌ | ❌ |

### 23. 增强涂改

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 基础涂改 | ✅ `ApplyRedactions()` | ❌ | ✅ `apply_redactions()` |
| 自定义填充颜色 | ✅ `ApplyRedactionsEnhanced()` | ❌ | ✅ |
| 涂改区域覆盖文本 | ✅ `OverlayText` 选项 | ❌ | ✅ |
| 覆盖文本颜色/大小 | ✅ `OverlayColor` / `OverlayFontSize` | ❌ | ✅ |
| 基于文本的涂改 | ✅ `RedactText()` | ❌ | ❌（需手动实现） |

> `RedactText()` 将文本搜索与涂改合并为一次调用，是 GoPDF2 的独有功能。

### 24. 页面变换

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 缩放页面内容 | ✅ `ScalePage()` / `ScalePageInPDF()` | ❌ | ✅ 通过 Matrix |
| 旋转页面内容 | ✅ `TransformPage()` / `RotatePageInPDF()` | ❌ | ✅ 通过 Matrix |
| 组合变换 | ✅ `TransformPageInPDF()` | ❌ | ✅ 通过 Matrix |
| 倾斜变换 | ✅ `SkewMatrix()` | ❌ | ✅ |
| 矩阵求逆 | ✅ `Matrix.Inverse()` | ❌ | ✅ `~matrix` |
| 变换矩形 | ✅ `Matrix.TransformRect()` | ❌ | ✅ |
| 解析矩阵字符串 | ✅ `ParseMatrix()` | ❌ | ❌ |

### 25. PDF/A 验证

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| PDF/A-1b 验证 | ✅ `ValidatePDFA()` | ❌ | ❌ |
| PDF/A-2b 验证 | ✅ `ValidatePDFA()` | ❌ | ❌ |
| XMP 元数据检查 | ✅ | ❌ | ❌ |
| 字体嵌入检查 | ✅ | ❌ | ❌ |
| JavaScript 检查 | ✅ | ❌ | ❌ |
| 透明度检查 | ✅ | ❌ | ❌ |
| 加密检查 | ✅ | ❌ | ❌ |

> PDF/A 验证是 GoPDF2 的独有功能。GoPDF 和 PyMuPDF 均不提供内置的 PDF/A 合规性检查。

### 26. PDF 比较

| 功能 | GoPDF2 | GoPDF | PyMuPDF |
|------|--------|-------|---------|
| 比较两个 PDF | ✅ `ComparePDF()` | ❌ | ❌ |
| 文本内容差异 | ✅ `CompareText` 选项 | ❌ | ❌ |
| 图片数量差异 | ✅ `CompareImages` 选项 | ❌ | ❌ |
| 字体差异 | ✅ `CompareFonts` 选项 | ❌ | ❌ |
| 元数据差异 | ✅ `CompareMetadata` 选项 | ❌ | ❌ |
| 页面尺寸差异 | ✅ | ❌ | ❌ |
| 文本增删/移动 | ✅ | ❌ | ❌ |

> PDF 比较是 GoPDF2 的独有功能。没有其他纯 Go PDF 库提供文档级别的差异对比能力。

---

## GoPDF2 独有优势（相对于 GoPDF 和 PyMuPDF）

1. **打开/修改已有 PDF** — 完整的读写访问已有 PDF，不仅仅是导入页面
2. **内容元素 CRUD** — 可直接操作页面上的单个内容元素（文本、图片、线条等），GoPDF 和 PyMuPDF 均无法做到
3. **开箱即用的水印 API** — 文字/图片水印、平铺、全页面，一行代码搞定
4. **文本/图片提取** — 纯 Go 内容流解析器，可提取文本和图片
5. **注释** — 完整的注释支持（20+ 种类型），支持创建/读取/修改/删除
6. **表单字段 (AcroForm)** — 创建和操作表单字段（文本、复选框、下拉框、签名）
7. **数字签名** — PKCS#7 签名与验证
8. **文档克隆** — 一行代码深拷贝
9. **增量保存** — 无需重写整个文件即可保存更改
10. **PDF 底层操作** — 直接访问 PDF 对象、字典、流
11. **日志/撤销** — 完整的撤销/重做，支持命名操作
12. **文档优化** — 清洗、垃圾回收、线性化、内容流清理
13. **嵌入文件** — 完整的嵌入文件附件 CRUD
14. **OCG/图层** — 可选内容组与图层管理
15. **HTML 渲染** — 内置 HTML 解析器用于 PDF 生成
16. **文档统计** — 一行代码获取文档结构概览
17. **AES 加密** — 支持 AES-128 和 AES-256 加密，不仅限于 RC4
18. **链接 CRUD** — 完整的链接管理：获取、删除、从已有 PDF 提取链接
19. **文本替换** — 在 PDF 内容流中搜索并替换文本，支持大小写不敏感和指定页面
20. **多格式文本提取** — 提取文本为纯文本、文本块、单词、HTML 或 JSON 格式
21. **增强涂改** — 自定义填充颜色、覆盖文本、基于文本的涂改（`RedactText()`）
22. **页面变换** — 缩放、旋转、倾斜页面内容，支持组合矩阵变换
23. **PDF/A 验证** — 内置 PDF/A 合规性检查（1b、1a、2b、2a）
24. **PDF 比较** — 文档级差异对比：文本、图片、字体、元数据、页面尺寸

## GoPDF 优势

1. **纯 Go 实现** — 无 CGO 依赖，编译简单，交叉编译友好
2. **轻量级 PDF 生成** — 专注、简洁的 API 用于创建 PDF
3. **表格布局** — 内置表格生成器
4. **页眉/页脚回调** — 自动在每页执行
5. **字体容器复用** — 多文档共享字体解析结果
6. **密码保护** — RC4 加密支持
7. **导入 PDF 页面** — 通过 gofpdi 集成
8. **阿拉伯文支持** — 内置阿拉伯文字形处理

## PyMuPDF 独有优势

1. **高保真页面渲染** — 通过 MuPDF 引擎实现高质量 PDF 渲染
2. **OCR** — 结合 Tesseract 进行文字识别
3. **多格式支持** — 可打开 XPS、EPUB、HTML、SVG、图片等格式
4. **涂改注释** — 安全地永久删除敏感内容

---

## 适用场景建议

| 场景 | 推荐 |
|------|------|
| Go 项目中生成 PDF 报告 | GoPDF2 或 GoPDF |
| 简单 PDF 生成（文本、图片、表格） | GoPDF（轻量）或 GoPDF2 |
| 在已有 PDF 上叠加内容 | GoPDF2 |
| 需要精细操作页面元素 | GoPDF2 |
| 批量添加水印 | GoPDF2 |
| 容器化/无 C 编译器环境 | GoPDF2 或 GoPDF |
| 从 PDF 提取文本/图片 | GoPDF2（基础）/ PyMuPDF（高级） |
| PDF 转图片 | PyMuPDF |
| OCR 文字识别 | PyMuPDF |
| 表单填写与处理 | GoPDF2 |
| 多格式文档转换 | PyMuPDF |
| 需要底层 PDF 对象操作 | GoPDF2 或 PyMuPDF |
| 涂改/安全删除敏感内容 | GoPDF2 或 PyMuPDF |
| 数字签名 | GoPDF2 |
| 合并/拆分 PDF | GoPDF2 |
| 搜索并替换 PDF 中的文本 | GoPDF2 |
| PDF/A 合规性检查 | GoPDF2 |
| 比较两个 PDF 文档 | GoPDF2 |
| 页面缩放/旋转/变换 | GoPDF2 或 PyMuPDF |
| AES 加密 PDF | GoPDF2 或 PyMuPDF |
| 从 PDF 提取链接 | GoPDF2 或 PyMuPDF |

---

## 未来可扩展方向

以下功能在纯 Go 中理论上可实现，但复杂度较高：

| 功能 | 难度 | 说明 |
|------|------|------|
| ~~更多注释类型~~ | ~~中~~ | ✅ 已实现 — 墨迹、折线、多边形、线条、图章、波浪线、插入符、文件附件、涂改等 |
| MarkInfo | 低 | ✅ 已实现 — `SetMarkInfo()`、`GetMarkInfo()` |
| 嵌入文件读取/删除 | ~~中~~ | ✅ 已实现 — `GetEmbeddedFile()`、`DeleteEmbeddedFile()`、`UpdateEmbeddedFile()`、`GetEmbeddedFileInfo()`、`GetEmbeddedFileNames()`、`GetEmbeddedFileCount()` |
| ~~表单字段 (AcroForm)~~ | ~~高~~ | ✅ 已实现 — `AddFormField()`、`AddTextField()`、`AddCheckbox()`、`AddDropdown()` |
| ~~文本提取~~ | ~~极高~~ | ✅ 已实现 — `ExtractTextFromPage()`、`ExtractPageText()` |
| ~~图片提取~~ | ~~极高~~ | ✅ 已实现 — `ExtractImagesFromPage()`、`ExtractImagesFromAllPages()` |
| ~~数字签名~~ | ~~高~~ | ✅ 已实现 — `SignPDF()`、`VerifySignature()` |
| 页面渲染 | 不可行 | ✅ 基础渲染已实现 — `RenderPageToImage()`（轻量级；高保真渲染需要 MuPDF） |
| OCR | 不可行 | 需要 Tesseract 或类似引擎 |
| 日志/撤销 | 高 | ✅ 已实现 — `JournalEnable()`、`JournalUndo()`、`JournalRedo()`、`JournalSave()`、`JournalLoad()` |
| 线性化 | 高 | ✅ 已实现 — `Linearize()`（简化版 Web 优化） |
| ~~AES 加密~~ | ~~中~~ | ✅ 已实现 — `SetEncryption()` 支持 AES-128 和 AES-256 |
| ~~链接 CRUD~~ | ~~中~~ | ✅ 已实现 — `GetLinks()`、`DeleteLink()`、`ExtractLinks()` |
| ~~文本替换~~ | ~~高~~ | ✅ 已实现 — `ReplaceText()` 支持大小写不敏感和指定页面 |
| ~~多格式文本提取~~ | ~~中~~ | ✅ 已实现 — `ExtractTextFormatted()` 支持 Text、Blocks、Words、HTML、JSON 格式 |
| ~~增强涂改~~ | ~~中~~ | ✅ 已实现 — `ApplyRedactionsEnhanced()`、`RedactText()` |
| ~~页面变换~~ | ~~中~~ | ✅ 已实现 — `ScalePage()`、`TransformPage()`、`ScalePageInPDF()`、`RotatePageInPDF()` |
| ~~PDF/A 验证~~ | ~~高~~ | ✅ 已实现 — `ValidatePDFA()` 支持 PDF/A-1b、1a、2b、2a |
| ~~PDF 比较~~ | ~~高~~ | ✅ 已实现 — `ComparePDF()` 支持文本、图片、字体、元数据差异对比 |
| EPUB → PDF 转换 | 极高 | 可通过已有 HTML 解析器实现；EPUB 本质是 HTML+CSS 打包 |
| 高级页面渲染 | 极高 | 渐变、Pattern、Type3 字体、混合模式 |