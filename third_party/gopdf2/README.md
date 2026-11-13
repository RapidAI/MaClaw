# GoPDF2

[![Go Reference](https://pkg.go.dev/badge/github.com/VantageDataChat/GoPDF2.svg)](https://pkg.go.dev/github.com/VantageDataChat/GoPDF2)
![Version](https://img.shields.io/badge/version-2.0.0-blue)

**[English](README.md) | [中文](README_zh.md)**

GoPDF2 is a Go library for generating PDF documents. Forked from [gopdf](https://github.com/signintech/gopdf), it adds HTML rendering support, the ability to open and modify existing PDFs, and other enhancements.

Requires Go 1.13+.

## Features

- Unicode subfont embedding (Chinese, Japanese, Korean, etc.) with automatic subsetting to minimize file size
- **Open and modify existing PDFs** via `OpenPDF` — import all pages, overlay new content (text, images, HTML, drawings), and save
- **HTML-to-PDF rendering** via `InsertHTMLBox` — supports `<b>`, `<i>`, `<u>`, `<s>`, `<p>`, `<h1>`–`<h6>`, `<font>`, `<span style>`, `<img>` (files and base64 data URIs), `<ul>`/`<ol>` (nested, `start`/`type`), `<hr>`, `<center>`, `<a>`, `<blockquote>`, `<pre>`/`<code>`, full `<table>` (colspan/rowspan, column widths, caption, per-cell styling), `<style>` blocks with tag/class/id selectors, CJK-aware line breaking with kinsoku rules, justified text, and more; multi-page flow via `InsertHTMLPaginated`
- **Watermark** — text and image watermarks with opacity, rotation, and tiling via `AddWatermarkText` / `AddWatermarkImage`
- **PDF annotations** — sticky notes, highlights, underlines, strikeouts, squares, circles, and free text via `AddAnnotation`
- **Page manipulation** — extract pages (`ExtractPages`), merge PDFs (`MergePages`), delete pages (`DeletePage` / `DeletePages`), copy pages (`CopyPage`), move pages (`MovePage`)
- **Page inspection** — query page sizes (`GetPageSize`, `GetAllPageSizes`), source PDF page count (`GetSourcePDFPageCount`)
- **Page crop box** — define the visible area of a page via `SetPageCropBox` / `GetPageCropBox` / `ClearPageCropBox`
- **Page rotation** — set display rotation for pages via `SetPageRotation` / `GetPageRotation`
- **Page reordering** — rearrange pages via `SelectPages`, `SelectPagesFromFile`, `SelectPagesFromBytes`
- **Embedded files** — attach files to PDF via `AddEmbeddedFile` (shown in viewer's attachment panel)
- **Extended paper sizes** — A0–A10, B0–B10, letter, legal, tabloid, ledger with landscape variants via `PaperSize(name)`
- **Geometry utilities** — `RectFrom` with Contains/Intersects/Union, `Matrix` for 2D transforms, `Distance`
- **Content element CRUD** — list, query, delete, add, modify, and reposition individual content elements (text, images, lines, rectangles, ovals, etc.) on any page via `GetPageElements`, `DeleteElement`, `ModifyTextElement`, `ModifyElementPosition`, `InsertLineElement`, `InsertRectElement`, `ClearPage`, and more
- **PDF version control** — set output PDF version (1.4–2.0) via `SetPDFVersion` / `GetPDFVersion`
- **Garbage collection** — remove null/deleted objects and compact the document via `GarbageCollect`
- **Page labels** — define custom page numbering (Roman, alphabetic, decimal with prefixes) via `SetPageLabels`
- **Typed object IDs** — `ObjID` wrapper for type-safe PDF object references
- **Incremental save** — write only modified objects via `IncrementalSave` for fast saves on large documents
- **XMP metadata** — embed full XMP metadata streams (Dublin Core, PDF/A, etc.) via `SetXMPMetadata`
- **Document cloning** — deep copy a GoPdf instance via `Clone` for independent modifications
- **Document scrubbing** — remove sensitive metadata, XMP, embedded files, page labels via `Scrub`
- **Optional Content Groups (Layers)** — add PDF layers for selective visibility via `AddOCG` / `GetOCGs`
- **Page layout & page mode** — control viewer display via `SetPageLayout` / `SetPageMode`
- **Document statistics** — inspect document structure via `GetDocumentStats` / `GetFonts`
- **TOC / Bookmarks** — read and write hierarchical outline trees via `GetTOC` / `SetTOC`
- **Document handle** — parse once, query many via `OpenPDFDocument`: `Lines` / `Paragraphs` / `Words` / `Drawings` / `Tables` / `Images` / `Search` / `Info` / `TOC` with per-page caching (up to 40× faster than repeated package-level calls)
- **Text extraction** — extract text with positions from existing PDFs via `ExtractTextFromPage` / `ExtractPageText`; position-aware line reconstruction via `ExtractPageLines` / `GroupIntoTextLines` (per-glyph Tj streams, kerned spaces, ligatures, ObjStm PDFs, predefined CMaps and legacy GBK/Big5/Shift-JIS/EUC encodings); column-aware reading order for two-column pages; paragraph reconstruction with hyphenation joins via `ExtractPageParagraphs`; region-scoped text via `ExtractTextInRect` / `ExtractTextWordsInRect`
- **Vector graphics extraction** — lines, polylines, rects and curves with stroke/fill colors and widths via `ExtractPageDrawings`
- **Table detection** — detect ruled tables and extract their cell text via `ExtractTablesFromPage` (grid from border lines, filled from position-aware words)
- **Document metadata** — read title/author/subject/keywords/creator/producer/dates via `GetPDFInfo`; write via `SetInfo`
- **TOC / outline reading** — extract the bookmark tree of existing PDFs (level/title/page/Y) via `ExtractTOC`
- **Rotated pages** — `/Rotate` is honored across text, image, drawing extraction and rendering (coordinates report visual space)
- **Dehyphenated search** — match text across hyphenated line wraps via `SearchTextWithOptions`
- **Formula rendering** — LaTeX-lite subset (sub/superscripts, fractions, square roots, binomials, Greek letters, operators) via `DrawFormula` / `MeasureFormula`
- **Image extraction** — extract images with metadata from existing PDFs via `ExtractImagesFromPage` / `ExtractImagesFromAllPages`
- **Form fields (AcroForm)** — add interactive form fields (text, checkbox, dropdown, radio, button, signature) via `AddFormField` / `AddTextField` / `AddCheckbox` / `AddDropdown`
- **Digital signatures** — sign PDFs with PKCS#7 and verify signatures via `SignPDF` / `VerifySignature`
- Draw lines, ovals, rectangles (with rounded corners), curves, polygons, polylines, sectors
- Draw images (JPEG, PNG) with mask, crop, rotation, and transparency
- Password protection
- Font kerning
- Import existing PDF pages
- Table layout
- Header / footer callbacks
- Trim-box support
- Placeholder text (fill-in-later pattern)

## Installation

```bash
go get -u github.com/VantageDataChat/GoPDF2
```

## Quick Start

### Print Text

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

    pdf.Cell(nil, "Hello, World!")
    pdf.WritePdf("hello.pdf")
}
```

### InsertHTMLBox — Render HTML into PDF

The signature:

```go
func (gp *GoPdf) InsertHTMLBox(x, y, w, h float64, htmlStr string, opt HTMLBoxOption) (float64, error)
```

Example:

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

    // Load fonts — only glyphs actually used will be embedded (subset)
    if err := pdf.AddTTFFont("regular", "NotoSansSC-Regular.ttf"); err != nil {
        log.Fatal(err)
    }
    if err := pdf.AddTTFFontWithOption("bold", "NotoSansSC-Bold.ttf", gopdf.TtfOption{Style: gopdf.Bold}); err != nil {
        log.Fatal(err)
    }

    html := `
    <h2>GoPDF2 HTML Rendering</h2>
    <p>Supports <b>bold</b>, <i>italic</i>, <u>underline</u> and
       <font color="#e74c3c">colored text</font>.</p>
    <ul>
        <li>Auto line wrapping</li>
        <li>Ordered/unordered lists</li>
        <li>Image insertion</li>
    </ul>
    <hr/>
    <p style="font-size:10pt; color:gray">
        Font subsetting — only characters actually used are embedded.
    </p>`

    endY, err := pdf.InsertHTMLBox(40, 40, 515, 750, html, gopdf.HTMLBoxOption{
        DefaultFontFamily: "regular",
        DefaultFontSize:   12,
        BoldFontFamily:    "bold",
    })
    if err != nil {
        log.Fatal(err)
    }
    _ = endY // Y position after rendered content

    pdf.WritePdf("html_example.pdf")
}
```

#### HTMLBoxOption

| Field | Type | Description |
|---|---|---|
| `DefaultFontFamily` | `string` | Font family when HTML does not specify one (required) |
| `DefaultFontSize` | `float64` | Default font size in points (default 12) |
| `DefaultColor` | `[3]uint8` | Default text color `{R, G, B}` |
| `LineSpacing` | `float64` | Extra line spacing in document units |
| `BoldFontFamily` | `string` | Font family for `<b>` / `<strong>` |
| `ItalicFontFamily` | `string` | Font family for `<i>` / `<em>` |
| `BoldItalicFontFamily` | `string` | Font family for bold+italic |

#### Supported HTML Tags

| Tag | Effect |
|---|---|
| `<b>`, `<strong>` | Bold |
| `<i>`, `<em>` | Italic |
| `<u>` | Underline |
| `<br>` | Line break |
| `<p>`, `<div>` | Paragraph |
| `<h1>` – `<h6>` | Headings |
| `<font color="..." size="..." face="...">` | Font styling |
| `<span style="...">` | Inline CSS (color, font-size, font-family, font-weight, font-style, text-decoration, text-align) |
| `<img src="..." width="..." height="...">` | Image (local file path) |
| `<ul>`, `<ol>`, `<li>` | Lists |
| `<hr>` | Horizontal rule |
| `<center>` | Centered text |
| `<a href="...">` | Link (blue underlined text) |
| `<blockquote>` | Indented block |
| `<sub>`, `<sup>` | Subscript / superscript |

### Font Subsetting & File Size

GoPDF2 uses **font subsetting** by default. When you call `AddTTFFont`, the full TTF is parsed, but only the glyphs for characters actually used in the document are embedded in the output PDF. This is especially important for CJK fonts which can be 10-20 MB — the resulting PDF will only contain the few KB needed for the characters you used.

No extra configuration is needed; subsetting is automatic.

### Text Color

```go
// RGB
pdf.SetTextColor(156, 197, 140)
pdf.Cell(nil, "colored text")

// CMYK
pdf.SetTextColorCMYK(0, 6, 14, 0)
pdf.Cell(nil, "CMYK text")
```

### Image

```go
pdf.Image("gopher.jpg", 200, 50, nil)
```

### Links

```go
pdf.SetXY(30, 40)
pdf.Text("Link to example.com")
pdf.AddExternalLink("http://example.com/", 27.5, 28, 125, 15)
```

### Header & Footer

```go
pdf.AddHeader(func() {
    pdf.SetY(5)
    pdf.Cell(nil, "header")
})
pdf.AddFooter(func() {
    pdf.SetY(825)
    pdf.Cell(nil, "footer")
})
```

### Drawing

```go
// Line
pdf.SetLineWidth(2)
pdf.SetLineType("dashed")
pdf.Line(10, 30, 585, 30)

// Oval
pdf.Oval(100, 200, 500, 500)

// Polygon
pdf.SetStrokeColor(255, 0, 0)
pdf.SetFillColor(0, 255, 0)
pdf.Polygon([]gopdf.Point{{X: 10, Y: 30}, {X: 585, Y: 200}, {X: 585, Y: 250}}, "DF")

// Rounded rectangle
pdf.Rectangle(196.6, 336.8, 398.3, 379.3, "DF", 3, 10)

// Polyline (open path, not closed)
pdf.SetStrokeColor(0, 0, 255)
pdf.Polyline([]gopdf.Point{{X: 10, Y: 400}, {X: 100, Y: 350}, {X: 200, Y: 420}, {X: 300, Y: 380}})

// Sector (pie/fan shape)
pdf.SetFillColor(255, 128, 0)
pdf.Sector(300, 500, 80, 0, 90, "FD")
```

### Rotation

```go
pdf.Rotate(270.0, 100.0, 100.0)
pdf.Text("rotated")
pdf.RotateReset()
```

### Transparency

```go
pdf.SetTransparency(gopdf.Transparency{Alpha: 0.5, BlendModeType: ""})
```

### Password Protection

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

### Import Existing PDF

```go
tpl := pdf.ImportPage("existing.pdf", 1, "/MediaBox")
pdf.UseImportedTemplate(tpl, 50, 100, 400, 0)
```

### Open and Modify Existing PDF

`OpenPDF` loads an existing PDF so you can draw new content on top of every page:

```go
pdf := gopdf.GoPdf{}
err := pdf.OpenPDF("input.pdf", nil)
if err != nil {
    log.Fatal(err)
}

pdf.AddTTFFont("myfont", "font.ttf")
pdf.SetFont("myfont", "", 14)

// Draw on page 1
pdf.SetPage(1)
pdf.SetXY(100, 100)
pdf.Cell(nil, "Watermark text")

// Draw on page 2
pdf.SetPage(2)
pdf.SetXY(200, 200)
pdf.Image("stamp.png", 200, 200, nil)

pdf.WritePdf("output.pdf")
```

Also available as `OpenPDFFromBytes(data, opt)` and `OpenPDFFromStream(rs, opt)`.

### Table

```go
table := pdf.NewTableLayout(10, 10, 25, 5)
table.AddColumn("CODE", 50, "left")
table.AddColumn("DESCRIPTION", 200, "left")
table.AddRow([]string{"001", "Product A"})
table.DrawTable()
```

### Placeholder Text

```go
pdf.PlaceHolderText("total", 30)
// ... after all pages created ...
pdf.FillInPlaceHoldText("total", "5", gopdf.Left)
```

### Watermark

Add text or image watermarks to PDF pages:

```go
// Single centered text watermark
pdf.SetPage(1)
pdf.AddWatermarkText(gopdf.WatermarkOption{
    Text:       "CONFIDENTIAL",
    FontFamily: "myfont",
    FontSize:   48,
    Opacity:    0.3,
    Angle:      45,
    Color:      [3]uint8{200, 200, 200},
})

// Tiled text watermark across the page
pdf.AddWatermarkText(gopdf.WatermarkOption{
    Text:       "DRAFT",
    FontFamily: "myfont",
    Repeat:     true,
})

// Apply text watermark to all pages
pdf.AddWatermarkTextAllPages(gopdf.WatermarkOption{
    Text:       "SAMPLE",
    FontFamily: "myfont",
})

// Image watermark (centered, 30% opacity)
pdf.AddWatermarkImage("logo.png", 0.3, 200, 200, 0)
```

### Annotations

Add PDF annotations (sticky notes, highlights, shapes, free text):

```go
// Sticky note
pdf.AddTextAnnotation(100, 100, "Reviewer", "Please check this section.")

// Highlight
pdf.AddHighlightAnnotation(50, 50, 200, 20, [3]uint8{255, 255, 0})

// Free text directly on the page
pdf.AddFreeTextAnnotation(100, 200, 250, 30, "Important note", 14)

// Full control via AddAnnotation
pdf.AddAnnotation(gopdf.AnnotationOption{
    Type:    gopdf.AnnotSquare,
    X:       50,
    Y:       300,
    W:       100,
    H:       50,
    Color:   [3]uint8{0, 0, 255},
    Content: "Review area",
})
```

### Page Manipulation

Extract, merge, delete, and copy pages:

```go
// Extract specific pages from a PDF
newPdf, _ := gopdf.ExtractPages("input.pdf", []int{1, 3, 5}, nil)
newPdf.WritePdf("pages_1_3_5.pdf")

// Merge multiple PDFs
merged, _ := gopdf.MergePages([]string{"doc1.pdf", "doc2.pdf"}, nil)
merged.WritePdf("merged.pdf")

// Delete a page (1-based)
pdf.DeletePage(2)

// Batch delete multiple pages
pdf.DeletePages([]int{2, 4, 6})

// Move a page to a new position
pdf.MovePage(3, 1) // move page 3 to become page 1

// Copy a page to the end
newPageNo, _ := pdf.CopyPage(1)
```

### Page Inspection

Query page sizes and metadata:

```go
// Get page size of a specific page
w, h, _ := pdf.GetPageSize(1)

// Get all page sizes
sizes := pdf.GetAllPageSizes()

// Get page count from a source PDF without importing
count, _ := gopdf.GetSourcePDFPageCount("input.pdf")

// Get page sizes from a source PDF
pageSizes, _ := gopdf.GetSourcePDFPageSizes("input.pdf")
```

### Paper Sizes

Use predefined paper sizes by name:

```go
// Look up paper size by name (case-insensitive)
size := gopdf.PaperSize("a5")
pdf.Start(gopdf.Config{PageSize: *size})

// Landscape variant
sizeL := gopdf.PaperSize("a4-l")

// Available: a0–a10, b0–b10, letter, legal, tabloid, ledger,
// statement, executive, folio, quarto (append "-l" for landscape)
names := gopdf.PaperSizeNames()
```

### Page Rotation

Set display rotation for pages (does not modify content):

```go
pdf.SetPageRotation(1, 90)   // rotate page 1 by 90° clockwise
pdf.SetPageRotation(2, 180)  // rotate page 2 by 180°

angle, _ := pdf.GetPageRotation(1) // returns 90
```

### Page CropBox

Define the visible area of a page (content outside is clipped but not removed):

```go
// Set crop box on page 1 — only the defined area is visible
pdf.SetPageCropBox(1, gopdf.Box{Left: 50, Top: 50, Right: 545, Bottom: 792})

// Get the current crop box
box, _ := pdf.GetPageCropBox(1)

// Remove crop box, restoring full MediaBox visibility
pdf.ClearPageCropBox(1)
```

### Page Reordering

Rearrange, duplicate, or subset pages:

```go
// Reverse page order of current document
newPdf, _ := pdf.SelectPages([]int{3, 2, 1})
newPdf.WritePdf("reversed.pdf")

// Select specific pages from a file
newPdf, _ = gopdf.SelectPagesFromFile("input.pdf", []int{1, 3, 5}, nil)

// Duplicate a page
newPdf, _ = pdf.SelectPages([]int{1, 1, 1})
```

### Embedded Files

Attach files to the PDF (shown in viewer's attachment panel):

```go
data, _ := os.ReadFile("report.csv")
pdf.AddEmbeddedFile(gopdf.EmbeddedFile{
    Name:        "report.csv",
    Content:     data,
    MimeType:    "text/csv",
    Description: "Monthly report data",
})
```

### Content Element CRUD

List, query, delete, modify, and add individual content elements on any page:

```go
// List all elements on page 1
elements, _ := pdf.GetPageElements(1)
for _, e := range elements {
    fmt.Printf("[%d] %s at (%.1f, %.1f)\n", e.Index, e.Type, e.X, e.Y)
}

// Get only text elements
texts, _ := pdf.GetPageElementsByType(1, gopdf.ElementText)

// Delete a specific element by index
pdf.DeleteElement(1, 0)

// Delete all lines from a page
removed, _ := pdf.DeleteElementsByType(1, gopdf.ElementLine)

// Delete elements within a rectangular area
pdf.DeleteElementsInRect(1, 0, 0, 100, 100)

// Clear all content from a page
pdf.ClearPage(1)

// Modify text content
pdf.ModifyTextElement(1, 0, "New text")

// Move an element to a new position
pdf.ModifyElementPosition(1, 0, 200, 300)

// Insert new elements on an existing page
pdf.InsertLineElement(1, 10, 400, 500, 400)
pdf.InsertRectElement(1, 50, 420, 200, 50, "DF")
pdf.InsertOvalElement(1, 300, 420, 450, 470)
```

### PDF Version Control

Set the output PDF version:

```go
pdf.SetPDFVersion(gopdf.PDFVersion20) // output PDF 2.0
v := pdf.GetPDFVersion()              // returns PDFVersion20
```

### Garbage Collection

Remove null/deleted objects to reduce file size:

```go
pdf.DeletePage(2)
removed := pdf.GarbageCollect(gopdf.GCCompact)
fmt.Printf("Removed %d unused objects\n", removed)
```

### Page Labels

Define custom page numbering displayed in PDF viewers:

```go
pdf.SetPageLabels([]gopdf.PageLabel{
    {PageIndex: 0, Style: gopdf.PageLabelRomanLower, Start: 1},  // i, ii, iii
    {PageIndex: 3, Style: gopdf.PageLabelDecimal, Start: 1},     // 1, 2, 3, ...
    {PageIndex: 10, Style: gopdf.PageLabelAlphaUpper, Prefix: "Appendix ", Start: 1},
})
```

### XMP Metadata

Embed rich XMP metadata (Dublin Core, PDF/A conformance, etc.):

```go
pdf.SetXMPMetadata(gopdf.XMPMetadata{
    Title:       "Annual Report 2025",
    Creator:     []string{"John Doe"},
    Description: "Company annual report",
    Subject:     []string{"finance", "report"},
    CreatorTool: "GoPDF2",
    Producer:    "GoPDF2",
    CreateDate:  time.Now(),
    ModifyDate:  time.Now(),
    PDFAPart:    1,
    PDFAConformance: "B",
})
```

### Incremental Save

Save only modified objects for fast updates on large documents:

```go
originalData, _ := os.ReadFile("input.pdf")
pdf := gopdf.GoPdf{}
pdf.OpenPDFFromBytes(originalData, nil)
pdf.SetPage(1)
pdf.SetXY(100, 100)
pdf.Text("Added text")

result, _ := pdf.IncrementalSave(originalData, nil)
os.WriteFile("output.pdf", result, 0644)
```

### Document Cloning

Deep copy a document for independent modifications:

```go
clone, _ := pdf.Clone()
clone.SetPage(1)
clone.SetXY(100, 100)
clone.Text("Only in clone")
clone.WritePdf("clone.pdf")
```

### Document Scrubbing

Remove sensitive metadata and attachments from a PDF:

```go
pdf.Scrub(gopdf.DefaultScrubOption())
pdf.GarbageCollect(gopdf.GCCompact)
pdf.WritePdf("scrubbed.pdf")

// Selective scrubbing
pdf.Scrub(gopdf.ScrubOption{
    Metadata:    true,
    XMLMetadata: true,
})
```

### Optional Content Groups (Layers)

Add PDF layers for selective visibility:

```go
watermark := pdf.AddOCG(gopdf.OCG{
    Name:   "Watermark",
    Intent: gopdf.OCGIntentView,
    On:     true,
})

draft := pdf.AddOCG(gopdf.OCG{
    Name:   "Draft Notes",
    Intent: gopdf.OCGIntentDesign,
    On:     false,
})

layers := pdf.GetOCGs()
```

### Page Layout & Page Mode

Control how the PDF viewer displays the document:

```go
// Set page layout
pdf.SetPageLayout(gopdf.PageLayoutTwoColumnLeft)
layout := pdf.GetPageLayout()

// Set page mode (which panel opens)
pdf.SetPageMode(gopdf.PageModeUseOutlines) // show bookmarks panel
mode := pdf.GetPageMode()
```

### Document Statistics

Inspect document structure:

```go
stats := pdf.GetDocumentStats()
fmt.Printf("Pages: %d, Fonts: %d, Images: %d\n",
    stats.PageCount, stats.FontCount, stats.ImageCount)

fonts := pdf.GetFonts()
for _, f := range fonts {
    fmt.Printf("Font: %s, Embedded: %v\n", f.Family, f.IsEmbedded)
}
```

### TOC / Bookmarks

Read and write hierarchical outline trees:

```go
// Set a hierarchical TOC
pdf.SetTOC([]gopdf.TOCItem{
    {Level: 1, Title: "Chapter 1", PageNo: 1},
    {Level: 2, Title: "Section 1.1", PageNo: 1, Y: 200},
    {Level: 2, Title: "Section 1.2", PageNo: 2},
    {Level: 1, Title: "Chapter 2", PageNo: 3},
})

// Read back the TOC
toc := pdf.GetTOC()
for _, item := range toc {
    fmt.Printf("L%d: %s -> page %d\n", item.Level, item.Title, item.PageNo)
}
```

## API Reference

See [docs/API.md](docs/API.md) (English) or [docs/API_zh.md](docs/API_zh.md) (中文).

## GoPDF2 vs PyMuPDF

See [docs/COMPARISON.md](docs/COMPARISON.md) (English) or [docs/COMPARISON_zh.md](docs/COMPARISON_zh.md) (中文) for a detailed feature comparison.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for version history.

## License

MIT
