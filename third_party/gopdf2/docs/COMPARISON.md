# GoPDF2 vs GoPDF vs PyMuPDF Feature Comparison

**[English](COMPARISON.md) | [中文](COMPARISON_zh.md)**

This document provides a detailed comparison between GoPDF2 (pure Go, fork of GoPDF), GoPDF (signintech/gopdf, pure Go), and PyMuPDF (Python bindings for the MuPDF C library).

> GoPDF2 is a pure Go implementation evolved from GoPDF, with no CGO dependencies. GoPDF (signintech/gopdf) is the original pure Go PDF generation library. PyMuPDF wraps the MuPDF C library via CGO, giving it access to a full rendering engine.
> The three libraries have different focuses: GoPDF2 specializes in PDF generation, editing, and manipulation; GoPDF focuses on PDF generation; PyMuPDF excels at PDF parsing, extraction, and rendering.

---

## Overview

| Category | GoPDF2 | GoPDF | PyMuPDF | Notes |
|----------|--------|-------|---------|-------|
| **Pure Go / No CGO** | ✅ | ✅ | ❌ | GoPDF2 and GoPDF need no C compiler |
| **PDF Generation** | ✅ | ✅ | ✅ | All three can create PDFs from scratch |
| **Open/Modify Existing PDF** | ✅ | ❌ (import only) | ✅ | GoPDF can only import pages via gofpdi |
| **HTML Rendering to PDF** | ✅ | ❌ | ✅ | GoPDF2 has built-in HTML parser |
| **Text Extraction** | ✅ | ❌ | ✅ | GoPDF2 has pure-Go content stream parser |
| **Page Rendering (Rasterization)** | ❌ | ❌ | ✅ | Requires MuPDF rendering engine |
| **OCR** | ❌ | ❌ | ✅ | Requires Tesseract + MuPDF |
| **SVG/HTML/EPUB Conversion** | ❌ | ❌ | ✅ | Requires MuPDF multi-format support |
| **Image Extraction** | ✅ | ❌ | ✅ | GoPDF2 has pure-Go image extraction |
| **Form Fields (AcroForm)** | ✅ | ❌ | ✅ | GoPDF2 supports text, checkbox, dropdown, radio, button, signature fields |
| **Digital Signatures** | ✅ | ❌ | ✅ | GoPDF2 supports PKCS#7 signing and verification |

---

## Detailed Comparison

### 1. Document Lifecycle

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Create blank PDF | `Start(Config)` | `Start(Config)` | `Document()` |
| Open from file | `OpenPDF(path)` | ❌ | `Document(path)` |
| Open from memory | `OpenPDFFromBytes(data)` | ❌ | `Document(stream=data)` |
| Open from stream | `OpenPDFFromStream(rs)` | ❌ | `Document(stream=data)` |
| Save to file | `WritePdf(path)` | `WritePdf(path)` | `save(path)` |
| Save to memory | `GetBytesPdf()` | `GetBytesPdf()` | `tobytes()` |
| Incremental save | ✅ `IncrementalSave()` | ❌ | ✅ `saveIncr()` |
| Document cloning | ✅ `Clone()` | ❌ | ❌ (manual serialize) |
| Close document | Auto GC | Auto GC | `close()` |
| Open encrypted PDF | ✅ `OpenPDF(Password)` | ❌ | ✅ `authenticate()` |
| Multi-format support (XPS/EPUB/HTML) | ❌ | ❌ | ✅ |

### 2. Page Management

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Add new page | ✅ `AddPage()` | ✅ `AddPage()` | ✅ `new_page()` |
| Delete page | ✅ `DeletePage(n)` | ❌ | ✅ `delete_page(n)` |
| Batch delete pages | ✅ `DeletePages()` | ❌ | ✅ `delete_pages()` |
| Copy page (reference) | ✅ `CopyPage(n)` | ❌ | ✅ `copy_page()` |
| Full copy page | ✅ `CopyPage(n)` | ❌ | ✅ `fullcopy_page()` |
| Move page | ✅ `MovePage()` | ❌ | ✅ `move_page()` |
| Page reordering/selection | ✅ `SelectPages()` | ❌ | ✅ `select()` |
| Extract pages to new doc | ✅ `ExtractPages()` | ❌ | ✅ `select()` + `save()` |
| Merge multiple PDFs | ✅ `MergePages()` | ❌ | ✅ `insert_pdf()` |
| Import PDF pages | ✅ `ImportPage()` | ✅ `ImportPage()` | ✅ `insert_pdf()` |
| Insert arbitrary format | ❌ | ❌ | ✅ `insert_file()` |
| Page rotation | ✅ `SetPageRotation()` | ❌ | ✅ `set_rotation()` |
| Get page size | ✅ `GetPageSize()` | ❌ | ✅ `page.rect` |
| Get all page sizes | ✅ `GetAllPageSizes()` | ❌ | Manual iteration |
| Source PDF page count | ✅ `GetSourcePDFPageCount()` | ❌ | ✅ `page_count` |
| Page cropbox | ✅ `SetPageCropBox()` | ❌ | ✅ `page_cropbox()` |
| Chapter navigation (EPUB) | ❌ | ❌ | ✅ `chapter_page_count()` etc. |

### 3. Text & Fonts

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Write text | ✅ `Cell()` / `Text()` | ✅ `Cell()` / `Text()` | ✅ `insert_text()` |
| Multi-line text | ✅ `MultiCell()` | ✅ `MultiCell()` | ✅ `insert_textbox()` |
| Text measurement | ✅ `MeasureTextWidth()` | ✅ `MeasureTextWidth()` | ✅ `get_text_length()` |
| Word wrap | ✅ `SplitTextWithWordWrap()` | ✅ `SplitTextWithWordWrap()` | ✅ Built-in |
| TTF font embedding | ✅ Auto-subsetting | ✅ Auto-subsetting | ✅ |
| Font subsetting | ✅ Automatic | ✅ Automatic | ✅ `subset_fonts()` |
| Kerning | ✅ | ✅ | ✅ |
| Text extraction | ✅ `ExtractPageText()` | ❌ | ✅ `get_text()` |
| Text search | ✅ `SearchText()` | ❌ | ✅ `search_for()` |
| Text block/dict extraction | ✅ `ExtractTextFromPage()` | ❌ | ✅ `get_text("dict")` |
| Font info query | ✅ `GetFonts()` | ❌ | ✅ `get_page_fonts()` |
| Font extraction | ✅ `ExtractFontsFromPage()` | ❌ | ✅ `extract_font()` |
| CJK font support | ✅ | ✅ | ✅ |

### 4. Drawing

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Lines | ✅ `Line()` | ✅ `Line()` | ✅ `draw_line()` |
| Rectangles | ✅ `Rectangle()` | ✅ `Rectangle()` | ✅ `draw_rect()` |
| Rounded rectangles | ✅ | ✅ | ❌ (manual) |
| Ovals/Circles | ✅ `Oval()` | ✅ `Oval()` | ✅ `draw_circle()` / `draw_oval()` |
| Polygons | ✅ `Polygon()` | ✅ `Polygon()` | ✅ `draw_polygon()` |
| Bezier curves | ✅ `Curve()` | ✅ `Curve()` | ✅ `draw_bezier()` |
| Polylines | ✅ `Polyline()` | ❌ | ✅ `draw_polyline()` |
| Sectors | ✅ `Sector()` | ❌ | ✅ `draw_sector()` |
| Dash/line types | ✅ `SetLineType()` | ✅ `SetLineType()` | ✅ |
| Fill/stroke colors | ✅ | ✅ | ✅ |
| Rotation | ✅ `Rotate()` | ✅ `Rotate()` | ✅ |
| Transparency | ✅ `SetTransparency()` | ✅ `SetTransparency()` | ✅ |
| Graphics state save/restore | ✅ `SaveGraphicsState()` | ✅ `SaveGraphicsState()` | ✅ |

### 5. Images

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Insert image (JPEG/PNG) | ✅ `Image()` | ✅ `Image()` | ✅ `insert_image()` |
| Insert from memory | ✅ `ImageFrom()` | ✅ `ImageFrom()` | ✅ |
| Image mask/crop | ✅ | ✅ | ✅ |
| Image rotation | ✅ | ❌ | ✅ |
| Image transparency | ✅ | ✅ | ✅ |
| Image extraction | ✅ `ExtractImagesFromPage()` | ❌ | ✅ `extract_image()` |
| Image info query | ✅ `ExtractImagesFromPage()` | ❌ | ✅ `get_page_images()` |
| Image deletion | ✅ `DeleteImages()` | ❌ | ✅ `delete_image()` |
| Image recompression | ✅ `RecompressImages()` | ❌ | ✅ `rewrite_images()` |
| SVG insertion | ✅ `ImageSVG()` | ❌ | ✅ |
| Pixmap rendering | ✅ `RenderPageToImage()` | ❌ | ✅ `get_pixmap()` |

### 6. Annotations

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Text (sticky note) | ✅ `AddTextAnnotation()` | ❌ | ✅ `add_text_annot()` |
| Highlight | ✅ `AddHighlightAnnotation()` | ❌ | ✅ `add_highlight_annot()` |
| Underline | ✅ | ❌ | ✅ `add_underline_annot()` |
| Strikeout | ✅ | ❌ | ✅ `add_strikeout_annot()` |
| Rectangle | ✅ | ❌ | ✅ `add_rect_annot()` |
| Circle | ✅ | ❌ | ✅ `add_circle_annot()` |
| Free text | ✅ `AddFreeTextAnnotation()` | ❌ | ✅ `add_freetext_annot()` |
| Ink | ✅ `AddInkAnnotation()` | ❌ | ✅ `add_ink_annot()` |
| Polyline | ✅ `AddPolylineAnnotation()` | ❌ | ✅ `add_polyline_annot()` |
| Polygon | ✅ `AddPolygonAnnotation()` | ❌ | ✅ `add_polygon_annot()` |
| Line | ✅ `AddLineAnnotation()` | ❌ | ✅ `add_line_annot()` |
| Stamp | ✅ `AddStampAnnotation()` | ❌ | ✅ `add_stamp_annot()` |
| Squiggly | ✅ `AddSquigglyAnnotation()` | ❌ | ✅ `add_squiggly_annot()` |
| Caret | ✅ `AddCaretAnnotation()` | ❌ | ✅ `add_caret_annot()` |
| File attachment | ✅ `AddFileAttachmentAnnotation()` | ❌ | ✅ `add_file_annot()` |
| Redaction | ✅ `AddRedactAnnotation()` | ❌ | ✅ `add_redact_annot()` |
| Delete annotation | ✅ `DeleteAnnotation()` | ❌ | ✅ `delete_annot()` |
| Modify annotation | ✅ `ModifyAnnotation()` | ❌ | ✅ Annot class methods |
| Iterate annotations | ✅ `GetAnnotations()` | ❌ | ✅ `annots()` |
| Apply redactions | ✅ `ApplyRedactions()` | ❌ | ✅ `apply_redactions()` |

### 7. Watermarks

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Text watermark | ✅ `AddWatermarkText()` | ❌ | Manual implementation |
| Image watermark | ✅ `AddWatermarkImage()` | ❌ | Manual implementation |
| All-pages watermark | ✅ `AddWatermarkTextAllPages()` | ❌ | Manual implementation |
| Tiled watermark | ✅ `Repeat: true` | ❌ | Manual implementation |
| Watermark opacity/rotation | ✅ | ❌ | Manual implementation |

> GoPDF2 provides ready-to-use watermark APIs. GoPDF has no watermark support. PyMuPDF requires manual composition via drawing APIs.

### 8. TOC / Bookmarks

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Get TOC | ✅ `GetTOC()` | ❌ | ✅ `get_toc()` |
| Set TOC | ✅ `SetTOC()` | ❌ | ✅ `set_toc()` |
| Hierarchical TOC | ✅ | ❌ | ✅ |
| Add single bookmark | ✅ `AddOutline()` | ✅ `AddOutline()` | ✅ |
| Modify single bookmark | ✅ `ModifyBookmark()` | ❌ | ✅ `set_toc_item()` |
| Delete single bookmark | ✅ `DeleteBookmark()` | ❌ | ✅ `del_toc_item()` |
| Bookmark color/bold/italic | ✅ `SetBookmarkStyle()` | ❌ | ✅ |
| Bookmark collapse control | ✅ `SetBookmarkStyle()` | ❌ | ✅ `collapse` parameter |

### 9. Metadata

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Standard metadata (Info) | ✅ `SetInfo()` | ❌ | ✅ `set_metadata()` |
| XMP metadata | ✅ `SetXMPMetadata()` | ❌ | ✅ `set_xml_metadata()` |
| Get XMP metadata | ✅ `GetXMPMetadata()` | ❌ | ✅ `get_xml_metadata()` |
| PDF/A compliance | ✅ Via XMP | ❌ | ✅ |
| Dublin Core | ✅ | ❌ | ✅ |

### 10. Embedded Files

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Add embedded file | ✅ `AddEmbeddedFile()` | ❌ | ✅ `embfile_add()` |
| Get embedded file | ✅ `GetEmbeddedFile()` | ❌ | ✅ `embfile_get()` |
| Delete embedded file | ✅ `DeleteEmbeddedFile()` | ❌ | ✅ `embfile_del()` |
| Update embedded file | ✅ `UpdateEmbeddedFile()` | ❌ | ✅ `embfile_upd()` |
| Embedded file info | ✅ `GetEmbeddedFileInfo()` | ❌ | ✅ `embfile_info()` |
| List embedded files | ✅ `GetEmbeddedFileNames()` | ❌ | ✅ `embfile_names()` |
| Embedded file count | ✅ `GetEmbeddedFileCount()` | ❌ | ✅ `embfile_count()` |

### 11. Optional Content Groups (OCG/Layers)

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Add OCG | ✅ `AddOCG()` | ❌ | ✅ `add_ocg()` |
| Get all OCGs | ✅ `GetOCGs()` | ❌ | ✅ `get_ocgs()` |
| Layer configurations | ✅ `AddLayerConfig()` / `GetLayerConfigs()` | ❌ | ✅ `add_layer()` / `get_layers()` |
| Switch layers | ✅ `SwitchLayer()` | ❌ | ✅ `switch_layer()` |
| OCMD (membership dict) | ✅ `AddOCMD()` / `GetOCMD()` | ❌ | ✅ `set_ocmd()` / `get_ocmd()` |
| Layer UI config | ✅ `SetLayerUIConfig()` | ❌ | ✅ `layer_ui_configs()` |
| Batch set OCG states | ✅ `SetOCGStates()` | ❌ | ✅ `set_layer()` |

### 12. Page Layout & Display

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Page layout | ✅ `SetPageLayout()` | ❌ | ✅ `set_pagelayout()` |
| Page mode | ✅ `SetPageMode()` | ❌ | ✅ `set_pagemode()` |
| MarkInfo | ✅ `SetMarkInfo()` | ❌ | ✅ `set_markinfo()` |
| Page labels | ✅ `SetPageLabels()` | ❌ | ✅ `set_page_labels()` |
| Get page labels | ✅ `GetPageLabels()` | ❌ | ✅ `get_page_labels()` |
| Find pages by label | ✅ `FindPagesByLabel()` | ❌ | ✅ `get_page_numbers()` |

### 13. Security & Encryption

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Password protection | ✅ `PDFProtectionConfig` | ✅ `SetProtection()` | ✅ `save(encryption=...)` |
| Permission control | ✅ | ✅ | ✅ |
| Open encrypted PDF | ✅ `OpenPDF(Password)` | ❌ | ✅ `authenticate()` |
| Encryption method selection | ✅ RC4 (V1/V2), AES-128, AES-256 | ✅ RC4 | ✅ Multiple standards |
| Digital signatures | ✅ `SignPDF()` / `VerifySignature()` | ❌ | ✅ `get_sigflags()` |

### 14. Document Scrubbing & Optimization

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Document scrubbing | ✅ `Scrub()` | ❌ | ✅ `scrub()` |
| Garbage collection | ✅ `GarbageCollect()` | ❌ | ✅ `save(garbage=N)` |
| Object deduplication | ✅ `GCDedup` | ❌ | ✅ `save(garbage=3)` |
| Document statistics | ✅ `GetDocumentStats()` | ❌ | Manual counting |
| Linearization (Web optimize) | ✅ `Linearize()` | ❌ | ✅ `save(linear=True)` |
| Content stream cleaning | ✅ `CleanContentStreams()` | ❌ | ✅ `save(clean=True)` |
| Image recompression | ✅ `RecompressImages()` | ❌ | ✅ `rewrite_images()` |
| Colorspace conversion | ✅ `ConvertColorspace()` | ❌ | ✅ `recolor()` |

### 15. Content Element Operations

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| List page elements | ✅ `GetPageElements()` | ❌ | ❌ (requires stream parsing) |
| Filter elements by type | ✅ `GetPageElementsByType()` | ❌ | ❌ |
| Delete single element | ✅ `DeleteElement()` | ❌ | ❌ |
| Delete elements by type | ✅ `DeleteElementsByType()` | ❌ | ❌ |
| Delete elements in rect | ✅ `DeleteElementsInRect()` | ❌ | ❌ |
| Clear page | ✅ `ClearPage()` | ❌ | ❌ (requires stream rewrite) |
| Modify text element | ✅ `ModifyTextElement()` | ❌ | ❌ |
| Modify element position | ✅ `ModifyElementPosition()` | ❌ | ❌ |
| Insert new elements | ✅ `InsertLineElement()` etc. | ❌ | ❌ |

> Content Element CRUD is unique to GoPDF2. GoPDF2 maintains a structured content cache in memory, enabling direct manipulation of individual elements. Neither GoPDF nor PyMuPDF can manipulate content at the element level.

### 16. Low-level PDF Operations

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| PDF version control | ✅ `SetPDFVersion()` | ❌ | Via header |
| Typed object IDs | ✅ `ObjID` | ❌ | ✅ xref system |
| Object count | ✅ `GetObjectCount()` | ❌ | ✅ `xref_length()` |
| Read object definition | ✅ `ReadObject()` | ❌ | ✅ `xref_object()` |
| Modify object definition | ✅ `UpdateObject()` | ❌ | ✅ `update_object()` |
| Read/write dict keys | ✅ `GetDictKey()` / `SetDictKey()` | ❌ | ✅ `xref_get_key()` / `xref_set_key()` |
| Read/write stream data | ✅ `GetStream()` / `SetStream()` | ❌ | ✅ `xref_stream()` / `update_stream()` |
| Copy objects | ✅ `CopyObject()` | ❌ | ✅ `xref_copy()` |
| PDF Catalog access | ✅ `GetCatalog()` | ❌ | ✅ `pdf_catalog()` |
| PDF Trailer access | ✅ `GetTrailer()` | ❌ | ✅ `pdf_trailer()` |

### 17. Journalling (Undo/Redo)

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Enable journalling | ✅ `JournalEnable()` | ❌ | ✅ `journal_enable()` |
| Undo/Redo | ✅ `JournalUndo()` / `JournalRedo()` | ❌ | ✅ `journal_undo()` / `journal_redo()` |
| Save/Load journal | ✅ `JournalSave()` / `JournalLoad()` | ❌ | ✅ `journal_save()` / `journal_load()` |
| Named operations | ✅ `JournalStartOp()` | ❌ | ✅ `journal_start_op()` |

### 18. Form Fields (Widgets/AcroForm)

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Add form field | ✅ `AddFormField()` | ❌ | ✅ `add_widget()` |
| Add text field | ✅ `AddTextField()` | ❌ | ✅ `add_widget()` |
| Add checkbox | ✅ `AddCheckbox()` | ❌ | ✅ `add_widget()` |
| Add dropdown | ✅ `AddDropdown()` | ❌ | ✅ `add_widget()` |
| Add signature field | ✅ `AddSignatureField()` | ❌ | ✅ `add_widget()` |
| Get form fields | ✅ `GetFormFields()` | ❌ | ✅ Widget class |
| Delete form field | ✅ `DeleteFormField()` | ❌ | ✅ `delete_widget()` |
| Modify form values | ✅ `ModifyFormFieldValue()` | ❌ | ✅ Widget class |
| Form PDF detection | ✅ `IsFormPDF()` | ❌ | ✅ `is_form_pdf` |
| Bake annotations/fields | ✅ `BakeAnnotations()` | ❌ | ✅ `bake()` |

### 19. Other Features

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| HTML rendering | ✅ `InsertHTMLBox()` | ❌ | ✅ Story class |
| Table layout | ✅ `NewTableLayout()` | ✅ `NewTableLayout()` | ❌ (manual) |
| Header/footer callbacks | ✅ `AddHeader()` / `AddFooter()` | ✅ `AddHeader()` / `AddFooter()` | ❌ (manual) |
| Placeholder text | ✅ `PlaceHolderText()` | ✅ `PlaceHolderText()` | ❌ |
| Paper size lookup | ✅ `PaperSize()` | ❌ | ✅ `paper_size()` |
| Geometry utilities | ✅ `RectFrom` / `Matrix` | ❌ | ✅ `Rect` / `Matrix` |
| Links | ✅ `AddExternalLink()` | ✅ `AddExternalLink()` | ✅ `insert_link()` |
| Anchors | ✅ `SetAnchor()` | ✅ `SetAnchor()` | ✅ |
| Font container reuse | ✅ `FontContainer` | ✅ `FontContainer` | ❌ |
| Compression level control | ✅ `SetCompressLevel()` | ✅ `SetCompressLevel()` | ✅ `save(deflate=True)` |
| Trim-box | ✅ | ❌ | ✅ |
| Arabic text support | ✅ | ✅ | ✅ |
| CMYK colors | ✅ | ✅ | ✅ |
| Clip polygon | ✅ | ✅ | ✅ |

### 20. Link Management

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Add external link | ✅ `AddExternalLink()` | ✅ `AddExternalLink()` | ✅ `insert_link()` |
| Add internal link | ✅ `AddInternalLink()` | ❌ | ✅ `insert_link()` |
| Get links on page | ✅ `GetLinks()` / `GetLinksOnPage()` | ❌ | ✅ `get_links()` |
| Delete link | ✅ `DeleteLink()` / `DeleteLinkOnPage()` | ❌ | ✅ `delete_link()` |
| Delete all links | ✅ `DeleteAllLinks()` / `DeleteAllLinksOnPage()` | ❌ | ✅ Manual iteration |
| Extract links from PDF | ✅ `ExtractLinks()` / `ExtractLinksFromPage()` | ❌ | ✅ `get_links()` |

### 21. Text Replacement

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Search and replace text | ✅ `ReplaceText()` | ❌ | ❌ (manual) |
| Case-insensitive replace | ✅ `CaseInsensitive` option | ❌ | ❌ |
| Page-specific replace | ✅ `Pages` option | ❌ | ❌ |
| Max replacements limit | ✅ `MaxReplacements` option | ❌ | ❌ |
| Hex string replacement | ✅ | ❌ | ❌ |

> Text replacement is unique to GoPDF2. It operates on raw content streams and handles both literal and hex-encoded strings.

### 22. Text Extraction Formats

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Plain text | ✅ `FormatText` | ❌ | ✅ `get_text("text")` |
| Text blocks | ✅ `FormatBlocks` | ❌ | ✅ `get_text("blocks")` |
| Words with positions | ✅ `FormatWords` | ❌ | ✅ `get_text("words")` |
| HTML output | ✅ `FormatHTML` | ❌ | ✅ `get_text("html")` |
| JSON output | ✅ `FormatJSON` | ❌ | ❌ |

### 23. Enhanced Redaction

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Basic redaction | ✅ `ApplyRedactions()` | ❌ | ✅ `apply_redactions()` |
| Custom fill color | ✅ `ApplyRedactionsEnhanced()` | ❌ | ✅ |
| Overlay text on redaction | ✅ `OverlayText` option | ❌ | ✅ |
| Overlay text color/size | ✅ `OverlayColor` / `OverlayFontSize` | ❌ | ✅ |
| Text-based redaction | ✅ `RedactText()` | ❌ | ❌ (manual) |

> `RedactText()` combines SearchText + redaction in one call — unique to GoPDF2.

### 24. Page Transformations

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Scale page content | ✅ `ScalePage()` / `ScalePageInPDF()` | ❌ | ✅ Via Matrix |
| Rotate page content | ✅ `TransformPage()` / `RotatePageInPDF()` | ❌ | ✅ Via Matrix |
| Combined transforms | ✅ `TransformPageInPDF()` | ❌ | ✅ Via Matrix |
| Skew transform | ✅ `SkewMatrix()` | ❌ | ✅ |
| Matrix inverse | ✅ `Matrix.Inverse()` | ❌ | ✅ `~matrix` |
| Transform rectangle | ✅ `Matrix.TransformRect()` | ❌ | ✅ |
| Parse matrix string | ✅ `ParseMatrix()` | ❌ | ❌ |

### 25. PDF/A Validation

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| PDF/A-1b validation | ✅ `ValidatePDFA()` | ❌ | ❌ |
| PDF/A-2b validation | ✅ `ValidatePDFA()` | ❌ | ❌ |
| XMP metadata check | ✅ | ❌ | ❌ |
| Font embedding check | ✅ | ❌ | ❌ |
| JavaScript check | ✅ | ❌ | ❌ |
| Transparency check | ✅ | ❌ | ❌ |
| Encryption check | ✅ | ❌ | ❌ |

> PDF/A validation is unique to GoPDF2. Neither GoPDF nor PyMuPDF provides built-in PDF/A compliance checking.

### 26. PDF Comparison

| Feature | GoPDF2 | GoPDF | PyMuPDF |
|---------|--------|-------|---------|
| Compare two PDFs | ✅ `ComparePDF()` | ❌ | ❌ |
| Text content diff | ✅ `CompareText` option | ❌ | ❌ |
| Image count diff | ✅ `CompareImages` option | ❌ | ❌ |
| Font diff | ✅ `CompareFonts` option | ❌ | ❌ |
| Metadata diff | ✅ `CompareMetadata` option | ❌ | ❌ |
| Page size diff | ✅ | ❌ | ❌ |
| Text added/removed/moved | ✅ | ❌ | ❌ |

> PDF comparison is unique to GoPDF2. No other pure-Go PDF library provides document-level diff capabilities.

---

## GoPDF2 Unique Strengths (vs both GoPDF and PyMuPDF)

1. **Open/Modify Existing PDF** — Full read-write access to existing PDFs, not just import pages
2. **Content Element CRUD** — Direct manipulation of individual page elements (text, images, lines, etc.) — not possible in GoPDF or PyMuPDF
3. **Ready-to-use Watermark API** — Text/image watermarks, tiling, all-pages — one line of code
4. **Text/Image Extraction** — Pure Go content stream parser for extracting text and images
5. **Annotations** — Full annotation support (20+ types) with create/read/modify/delete
6. **Form Fields (AcroForm)** — Create and manipulate form fields (text, checkbox, dropdown, signature)
7. **Digital Signatures** — PKCS#7 signing and verification
8. **Document Cloning** — One-line deep copy
9. **Incremental Save** — Save changes without rewriting the entire file
10. **Low-level PDF Operations** — Direct access to PDF objects, dictionaries, streams
11. **Journalling (Undo/Redo)** — Full undo/redo with named operations
12. **Document Optimization** — Scrubbing, garbage collection, linearization, content stream cleaning
13. **Embedded Files** — Full CRUD for embedded file attachments
14. **OCG/Layers** — Optional content groups with layer management
15. **HTML Rendering** — Built-in HTML parser for PDF generation
16. **Document Statistics** — One-line document structure overview
17. **AES Encryption** — AES-128 and AES-256 encryption in addition to RC4
18. **Link CRUD** — Full link management: get, delete, extract links from existing PDFs
19. **Text Replacement** — Search and replace text in PDF content streams with case-insensitive and page-specific options
20. **Multi-format Text Extraction** — Extract text as plain text, blocks, words, HTML, or JSON
21. **Enhanced Redaction** — Custom fill colors, overlay text, and text-based redaction (`RedactText()`)
22. **Page Transformations** — Scale, rotate, skew page content with combined matrix transforms
23. **PDF/A Validation** — Built-in PDF/A compliance checking (1b, 1a, 2b, 2a)
24. **PDF Comparison** — Document-level diff: text, images, fonts, metadata, page sizes

## GoPDF Strengths

1. **Pure Go** — No CGO, simple compilation, easy cross-compilation
2. **Lightweight PDF Generation** — Focused, simple API for creating PDFs
3. **Table Layout** — Built-in table generator
4. **Header/Footer Callbacks** — Auto-execute on every page
5. **Font Container Reuse** — Share parsed fonts across documents
6. **Password Protection** — RC4 encryption support
7. **Import PDF Pages** — Via gofpdi integration
8. **Arabic Text Support** — Built-in Arabic shaping

## PyMuPDF Unique Strengths

1. **Full-fidelity Page Rendering** — High-quality PDF rendering via MuPDF engine
2. **OCR** — Optical character recognition via Tesseract
3. **Multi-format Support** — Open XPS, EPUB, HTML, SVG, images, etc.
4. **Redaction Annotations** — Securely and permanently remove sensitive content

---

## Recommended Use Cases

| Scenario | Recommendation |
|----------|---------------|
| Generate PDF reports in Go projects | GoPDF2 or GoPDF |
| Simple PDF generation (text, images, tables) | GoPDF (lightweight) or GoPDF2 |
| Overlay content on existing PDFs | GoPDF2 |
| Fine-grained page element manipulation | GoPDF2 |
| Batch watermarking | GoPDF2 |
| Containerized / no C compiler environments | GoPDF2 or GoPDF |
| Extract text/images from PDFs | GoPDF2 (basic) / PyMuPDF (advanced) |
| PDF to image conversion | PyMuPDF |
| OCR text recognition | PyMuPDF |
| Form filling and processing | GoPDF2 |
| Multi-format document conversion | PyMuPDF |
| Low-level PDF object manipulation | GoPDF2 or PyMuPDF |
| Redaction / secure content removal | GoPDF2 or PyMuPDF |
| Digital signatures | GoPDF2 |
| Merge/split PDFs | GoPDF2 |
| Search and replace text in PDFs | GoPDF2 |
| PDF/A compliance checking | GoPDF2 |
| Compare two PDF documents | GoPDF2 |
| Page scaling/rotation/transformation | GoPDF2 or PyMuPDF |
| AES-encrypted PDFs | GoPDF2 or PyMuPDF |
| Extract links from PDFs | GoPDF2 or PyMuPDF |

---

## Future Expansion Possibilities

Features theoretically implementable in pure Go, with varying complexity:

| Feature | Difficulty | Notes |
|---------|-----------|-------|
| ~~More annotation types~~ | ~~Medium~~ | ✅ Implemented — Ink, polyline, polygon, line, stamp, squiggly, caret, file attachment, redaction, etc. |
| MarkInfo | Low | ✅ Implemented — `SetMarkInfo()`, `GetMarkInfo()` |
| ~~Embedded file read/delete~~ | ~~Medium~~ | ✅ Implemented — `GetEmbeddedFile()`, `DeleteEmbeddedFile()`, `UpdateEmbeddedFile()`, `GetEmbeddedFileInfo()`, `GetEmbeddedFileNames()`, `GetEmbeddedFileCount()` |
| ~~Form fields (AcroForm)~~ | ~~High~~ | ✅ Implemented — `AddFormField()`, `AddTextField()`, `AddCheckbox()`, `AddDropdown()` |
| ~~Text extraction~~ | ~~Very High~~ | ✅ Implemented — `ExtractTextFromPage()`, `ExtractPageText()` |
| ~~Image extraction~~ | ~~Very High~~ | ✅ Implemented — `ExtractImagesFromPage()`, `ExtractImagesFromAllPages()` |
| ~~Digital signatures~~ | ~~High~~ | ✅ Implemented — `SignPDF()`, `VerifySignature()` |
| Page rendering | Not feasible | ✅ Basic rendering implemented — `RenderPageToImage()` (lightweight; full-fidelity requires MuPDF) |
| OCR | Not feasible | Requires Tesseract or similar |
| Journalling (undo/redo) | High | ✅ Implemented — `JournalEnable()`, `JournalUndo()`, `JournalRedo()`, `JournalSave()`, `JournalLoad()` |
| Linearization | High | ✅ Implemented — `Linearize()` (simplified web optimization) |
| ~~AES encryption~~ | ~~Medium~~ | ✅ Implemented — `SetEncryption()` with AES-128 and AES-256 support |
| ~~Link CRUD~~ | ~~Medium~~ | ✅ Implemented — `GetLinks()`, `DeleteLink()`, `ExtractLinks()` |
| ~~Text replacement~~ | ~~High~~ | ✅ Implemented — `ReplaceText()` with case-insensitive and page-specific options |
| ~~Multi-format text extraction~~ | ~~Medium~~ | ✅ Implemented — `ExtractTextFormatted()` with Text, Blocks, Words, HTML, JSON formats |
| ~~Enhanced redaction~~ | ~~Medium~~ | ✅ Implemented — `ApplyRedactionsEnhanced()`, `RedactText()` |
| ~~Page transformations~~ | ~~Medium~~ | ✅ Implemented — `ScalePage()`, `TransformPage()`, `ScalePageInPDF()`, `RotatePageInPDF()` |
| ~~PDF/A validation~~ | ~~High~~ | ✅ Implemented — `ValidatePDFA()` for PDF/A-1b, 1a, 2b, 2a |
| ~~PDF comparison~~ | ~~High~~ | ✅ Implemented — `ComparePDF()` with text, image, font, metadata diff |
| EPUB → PDF conversion | Very High | Possible via existing HTML parser; EPUB is HTML+CSS packaged |
| Advanced page rendering | Very High | Gradients, patterns, Type3 fonts, blend modes |