# GoPDF2 API Reference

**[English](API.md) | [中文](API_zh.md)**

Package `gopdf` — [github.com/VantageDataChat/GoPDF2](https://github.com/VantageDataChat/GoPDF2)

---

## Types

### GoPdf

The main struct for creating PDF documents.

```go
type GoPdf struct { /* unexported fields */ }
```

### Config

```go
type Config struct {
    Unit              int                 // UnitPT, UnitMM, UnitCM, UnitIN, UnitPX
    ConversionForUnit float64             // Custom conversion factor (overrides Unit)
    TrimBox           Box                 // Default trim box for all pages
    PageSize          Rect                // Default page size
    Protection        PDFProtectionConfig // Password protection settings
}
```

### Rect

```go
type Rect struct {
    W float64 // Width
    H float64 // Height
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
    DefaultFontFamily    string    // Required. Font family when HTML does not specify one.
    DefaultFontSize      float64   // Default font size in points (default 12).
    DefaultColor         [3]uint8  // Default text color {R, G, B}.
    LineSpacing          float64   // Extra line spacing in document units.
    BoldFontFamily       string    // Font family for <b>/<strong>.
    ItalicFontFamily     string    // Font family for <i>/<em>.
    BoldItalicFontFamily string    // Font family for bold+italic.
}
```

### TtfOption

```go
type TtfOption struct {
    UseKerning                bool
    Style                     int               // Regular|Bold|Italic
    OnGlyphNotFound           func(r rune)      // Debug callback when glyph is missing
    OnGlyphNotFoundSubstitute func(r rune) rune // Substitution callback
}
```

### Transparency

```go
type Transparency struct {
    Alpha         float64       // 0.0 (fully transparent) to 1.0 (opaque)
    BlendModeType BlendModeType // e.g. "/Normal", "/Multiply", etc.
}
```

### Predefined Page Sizes

`PageSizeA0`–`PageSizeA10`, `PageSizeA3Landscape`–`PageSizeA10Landscape`, `PageSizeB0`–`PageSizeB10`, `PageSizeLetter`, `PageSizeLetterLandscape`, `PageSizeLegal`, `PageSizeLegalLandscape`, `PageSizeTabloid`, `PageSizeLedger`, `PageSizeStatement`, `PageSizeExecutive`, `PageSizeFolio`, `PageSizeQuarto`

### Unit Constants

`UnitPT`, `UnitMM`, `UnitCM`, `UnitIN`, `UnitPX`

### Font Style Constants

`Regular` (0), `Italic` (1), `Bold` (2), `Underline` (4)

### Alignment Constants

`Left`, `Right`, `Top`, `Bottom`, `Center`, `Middle`

---

## Document Lifecycle

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

## Font Management

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

Fonts are automatically subsetted — only glyphs used in the document are embedded.

---

## Text

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

## HTML Rendering

```go
func (gp *GoPdf) InsertHTMLBox(x, y, w, h float64, htmlStr string, opt HTMLBoxOption) (float64, error)
func (gp *GoPdf) InsertHTMLPaginated(x, y, w float64, htmlStr string, opt HTMLBoxOption) (float64, error)
```

Render HTML content into a rectangular area. `InsertHTMLBox` clips at the
box height (legacy behavior); `InsertHTMLPaginated` flows content across
pages automatically — when content would pass `opt.PageBottom` a new page is
added and rendering resumes at `opt.PageTop`, with optional repeated table
headers (`opt.RepeatTableHeaders`) and an `opt.OnPageBreak` callback.

**Parameters:** `x, y` (top-left corner), `w, h` (box size), `htmlStr` (HTML string), `opt` (rendering options).

**Returns:** the Y position after the last rendered content.

**Supported tags:** `<b>`, `<strong>`, `<i>`, `<em>`, `<u>`, `<s>`/`<strike>`/`<del>` (real strikethrough), `<br>`, `<p>`, `<div>`, `<h1>`-`<h6>`, `<font>`, `<span>`, `<small>`, `<big>`, `<img>` (file paths and base64 data URIs), `<ul>`/`<ol>` with nesting, `start` and `type` (decimal/alpha/roman), `<hr>`, `<center>`, `<a>` (styled and clickable), `<blockquote>`, `<sub>`, `<sup>`, `<pre>`/`<code>`/`<tt>`, `<style>`, `<script>` (ignored), full `<table>` with `<thead>`/`<tbody>`/`<tfoot>`/`<tr>`/`<th>`/`<td>`/`<caption>`/`<colgroup>`/`<col>`, colspan/rowspan, per-cell alignment and background.

**CSS:** `<style>` blocks with tag / `.class` / `#id` selectors (specificity: id > class > tag), inline `style="..."` overrides.

**Supported CSS properties:** `color`, `background-color`, `font-size`, `font-family`, `font-weight`, `font-style`, `text-decoration`, `text-align` (including `justify`), `text-transform`, `text-indent`, `line-height`, `letter-spacing`, `vertical-align`, `width` (tables/cells/images), `margin-top`, `margin-bottom`, `white-space`

**CJK line breaking:** CJK text wraps per character with kinsoku rules (no line-starting punctuation, no line-ending open brackets).

**Block backgrounds:** `background-color` on block elements paints full-width bands behind each line (correct z-order, pagination-safe).

**Table pagination:** `InsertHTMLPaginated` keeps rows joined by rowspan cells together across page breaks (row-group keep-together; oversized groups fall back to row-level breaking) and repeats `<thead>` rows when `RepeatTableHeaders` is set.

**Block keep-together:** blocks styled `break-inside: avoid` / `page-break-inside: avoid` move wholly to the next page when they would straddle a boundary (own lines plus nested blocks are estimated, including default paragraph margins).

**Color formats:** `#RGB`, `#RRGGBB`, `rgb(r,g,b)`, `rgba(r,g,b,a)` (alpha ignored), CSS named colors

**Font size formats:** `12pt`, `16px`, `1.5em`, `150%`, named sizes (small, medium, large, etc.)

---

## Table Layout

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
    SetAutoRowHeight(auto bool) // grow rows to fit wrapped content
    SetPaginate(paginate bool)  // multi-page tables with repeated headers
}
```

- `SetAutoRowHeight(true)` measures wrapped cell text and grows each row to fit (`rowHeight` becomes the minimum).
- `SetPaginate(true)` starts a new page when a row would cross the bottom margin and redraws the header row on every continued page.
- `NewRowCellSpan(content, style, colspan)` builds cells spanning multiple columns.

---

## Formula Rendering

```go
func (gp *GoPdf) DrawFormula(x, y float64, latex string, fontSize float64, opt *FormulaOption) (float64, error)
func (gp *GoPdf) MeasureFormula(latex string, fontSize float64) (width float64, height float64, err error)
```

Render a formula using a practical LaTeX-lite subset:

- superscripts and subscripts: `x^2`, `a_i`, `x^{n+1}`
- fractions: `rac{a}{b}`
- square roots with overline: `\sqrt{x}`
- binomials with parentheses: `inom{n}{k}`
- Greek letters: `lpha` ... `\Omega`
- operators and symbols: `\sum`, `\int`, `\prod`, `	imes`, `\cdot`, `\pm`, `\leq`, `\geq`, `
eq`, `pprox`, `\infty`, `
ightarrow`, `\partial`, `
abla`, and more
- function names: `\sin`, `\cos`, `\log`, `\lim`, ...

`FormulaOption` selects the font family, color, and italic style. The current
font is used by default; a font with good Unicode math coverage (STIX, DejaVu,
Liberation) is recommended.

---
## Position

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

## Color

```go
func (gp *GoPdf) SetTextColor(r, g, b uint8)
func (gp *GoPdf) SetTextColorCMYK(c, m, y, k uint8)
func (gp *GoPdf) SetStrokeColor(r, g, b uint8)
func (gp *GoPdf) SetFillColor(r, g, b uint8)
func (gp *GoPdf) SetGrayFill(grayScale float64)
func (gp *GoPdf) SetGrayStroke(grayScale float64)
```

---

## Drawing

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

`Polyline` draws an open path through a series of points (stroke only, path is not closed). `Sector` draws a pie/fan shape defined by center, radius, and start/end angles in degrees (counter-clockwise from positive X axis). Style: `"D"` (stroke), `"F"` (fill), `"DF"`/`"FD"` (both).

---

## Image

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

## Rotation & Transparency

```go
func (gp *GoPdf) Rotate(angle, x, y float64)
func (gp *GoPdf) RotateReset()
func (gp *GoPdf) SetTransparency(transparency Transparency) error
func (gp *GoPdf) ClearTransparency()
```

---

## Margins

```go
func (gp *GoPdf) SetMargins(left, top, right, bottom float64)
func (gp *GoPdf) Margins() (float64, float64, float64, float64)
```

---

## Links, Anchors, Header, Footer

```go
func (gp *GoPdf) AddExternalLink(url string, x, y, w, h float64)
func (gp *GoPdf) AddInternalLink(anchor string, x, y, w, h float64)
func (gp *GoPdf) SetAnchor(name string)
func (gp *GoPdf) AddHeader(f func())
func (gp *GoPdf) AddFooter(f func())
```

---

## Import Existing PDF

```go
func (gp *GoPdf) ImportPage(sourceFile string, pageno int, box string) int
func (gp *GoPdf) UseImportedTemplate(tplid int, x, y, w, h float64)
func (gp *GoPdf) ImportPagesFromSource(source interface{}, box string) error
```

---

## Open and Modify Existing PDF

```go
func (gp *GoPdf) OpenPDF(pdfPath string, opt *OpenPDFOption) error
func (gp *GoPdf) OpenPDFFromBytes(pdfData []byte, opt *OpenPDFOption) error
func (gp *GoPdf) OpenPDFFromStream(rs *io.ReadSeeker, opt *OpenPDFOption) error
```

Open an existing PDF and import all pages so that new content can be drawn on top of them. The original page content is preserved as a background; any drawing method (Text, Cell, Image, InsertHTMLBox, Line, etc.) can then overlay new content.

After calling `OpenPDF`, use `SetPage(n)` (1-based) to switch pages, draw content, then call `WritePdf` to save.

### OpenPDFOption

```go
type OpenPDFOption struct {
    Box        string               // PDF box: "/MediaBox" (default), "/CropBox", etc.
    Protection *PDFProtectionConfig // Optional password protection for output.
}
```

---

## Placeholder Text

```go
func (gp *GoPdf) PlaceHolderText(placeHolderName string, placeHolderWidth float64) error
func (gp *GoPdf) FillInPlaceHoldText(placeHolderName string, text string, align int) error
```

---

## Page Management & Unit Conversion

```go
func (gp *GoPdf) GetNumberOfPages() int
func (gp *GoPdf) SetPage(pageno int) error
func (gp *GoPdf) UnitsToPoints(u float64) float64
func (gp *GoPdf) PointsToUnits(u float64) float64
```

---

## Miscellaneous

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

## FontContainer (Reusable Font Cache)

```go
type FontContainer struct { /* ... */ }
func (fc *FontContainer) AddTTFFont(family string, ttfpath string) error
func (fc *FontContainer) AddTTFFontByReader(family string, rd io.Reader) error
func (fc *FontContainer) AddTTFFontData(family string, fontData []byte) error
func (gp *GoPdf) AddTTFFontFromFontContainer(family string, container *FontContainer) error
```

Pre-parse fonts once and reuse across multiple `GoPdf` instances for better performance.

---

## Watermark

```go
func (gp *GoPdf) AddWatermarkText(opt WatermarkOption) error
func (gp *GoPdf) AddWatermarkImage(imgPath string, opacity float64, imgW, imgH float64, angle float64) error
func (gp *GoPdf) AddWatermarkTextAllPages(opt WatermarkOption) error
func (gp *GoPdf) AddWatermarkImageAllPages(imgPath string, opacity float64, imgW, imgH float64, angle float64) error
```

### WatermarkOption

```go
type WatermarkOption struct {
    Text           string    // Watermark text (required)
    FontFamily     string    // Font family (required, must be pre-loaded)
    FontSize       float64   // Font size in points (default 48)
    Angle          float64   // Rotation angle in degrees (default 45)
    Color          [3]uint8  // RGB color (default: light gray)
    Opacity        float64   // 0.0–1.0 (default 0.3)
    Repeat         bool      // Tile across the page
    RepeatSpacingX float64   // Horizontal spacing for tiling (default 150)
    RepeatSpacingY float64   // Vertical spacing for tiling (default 150)
}
```

---

## Annotations

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
    X, Y, W, H  float64        // Annotation rectangle in document units
    Title        string         // Author name (for sticky notes)
    Content      string         // Annotation text content
    Color        [3]uint8       // RGB color (default: yellow)
    Opacity      float64        // 0.0–1.0 (default 1.0)
    Open         bool           // Initially open popup (text annotations)
    FontSize     float64        // Font size for FreeText (default 12)
}
```

---

## Page Manipulation

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

`DeletePages` removes multiple pages in a single operation. Pages are 1-based; duplicates are ignored; pages are deleted in reverse order to maintain correct numbering. Cannot delete all pages.

`MovePage` moves a page from one position to another by reordering via `SelectPages`.

---

## Page Inspection

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
    Width      float64 // Page width in points
    Height     float64 // Page height in points
    PageNumber int     // 1-based page number
}
```

---

## Paper Size Lookup

```go
func PaperSize(name string) *Rect
func PaperSizeNames() []string
```

Look up predefined paper sizes by name (case-insensitive). Supported names: `a0`–`a10`, `b0`–`b10`, `letter`, `legal`, `tabloid`, `ledger`, `statement`, `executive`, `folio`, `quarto`. Append `-l` for landscape (e.g. `a4-l`, `letter-l`). Returns a copy of the Rect; returns nil if name is not recognized.

---

## Page Rotation

```go
func (gp *GoPdf) SetPageRotation(pageNo int, angle int) error
func (gp *GoPdf) GetPageRotation(pageNo int) (int, error)
```

Set or get the display rotation for a page. `angle` must be a multiple of 90 (0, 90, 180, 270). This sets the `/Rotate` entry in the page dictionary — it tells PDF viewers how to display the page but does not modify the page content.

---

## Page CropBox

```go
func (gp *GoPdf) SetPageCropBox(pageNo int, box Box) error
func (gp *GoPdf) GetPageCropBox(pageNo int) (*Box, error)
func (gp *GoPdf) ClearPageCropBox(pageNo int) error
```

Set, get, or remove the CropBox for a page. The CropBox defines the visible area of the page when displayed or printed. Content outside the CropBox is clipped (hidden) but not removed.

`pageNo` is 1-based. Box coordinates are in document units (PDF coordinate system where (0,0) is bottom-left).

`GetPageCropBox` returns nil if no CropBox is set. `ClearPageCropBox` removes the CropBox, restoring the full MediaBox as the visible area.

---

## Page Reordering

```go
func (gp *GoPdf) SelectPages(pages []int) (*GoPdf, error)
func SelectPagesFromFile(pdfPath string, pages []int, opt *OpenPDFOption) (*GoPdf, error)
func SelectPagesFromBytes(pdfData []byte, pages []int, opt *OpenPDFOption) (*GoPdf, error)
```

Rearrange pages in a new document. Pages are 1-based and may be repeated. The current document is exported to bytes, then only the selected pages are re-imported in the specified order.

---

## Embedded Files

```go
func (gp *GoPdf) AddEmbeddedFile(ef EmbeddedFile) error
```

Attach a file to the PDF document. The file appears in the PDF viewer's attachment panel.

### EmbeddedFile

```go
type EmbeddedFile struct {
    Name        string    // Display name (required)
    Content     []byte    // File content (required)
    MimeType    string    // MIME type (e.g. "text/plain", "application/pdf")
    Description string    // Optional description
    ModDate     time.Time // Modification date (default: now)
}
```

---

## Geometry

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

A positioned rectangle with geometric operations.

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

2D affine transformation matrix. Transformation: `x' = a*x + c*y + e`, `y' = b*x + d*y + f`.

### Distance

```go
func Distance(p1, p2 Point) float64
```

Euclidean distance between two points.

---

## Content Element CRUD

### ContentElementType

```go
type ContentElementType int

const (
    ElementText             ContentElementType
    ElementImage            ContentElementType
    ElementLine             ContentElementType
    ElementRectangle        ContentElementType
    ElementOval             ContentElementType
    ElementPolygon          ContentElementType
    ElementCurve            ContentElementType
    ElementPolyline         ContentElementType
    ElementSector           ContentElementType
    ElementImportedTemplate ContentElementType
    ElementLineWidth        ContentElementType
    ElementLineType         ContentElementType
    // ... and more (ColorRGB, ColorCMYK, Rotate, etc.)
    ElementUnknown          ContentElementType
)

func (t ContentElementType) String() string
```

### ContentElement

```go
type ContentElement struct {
    Index    int                // 0-based position in the page's content cache
    Type     ContentElementType // Element type
    X, Y     float64           // Primary position
    X2, Y2   float64           // Secondary position (lines, ovals)
    Width    float64           // Width (rectangles, images)
    Height   float64           // Height (rectangles, images)
    Text     string            // Text content (text elements only)
    FontSize float64           // Font size (text elements only)
}
```

### Query Methods

```go
func (gp *GoPdf) GetPageElements(pageNo int) ([]ContentElement, error)
func (gp *GoPdf) GetPageElementsByType(pageNo int, elemType ContentElementType) ([]ContentElement, error)
func (gp *GoPdf) GetPageElementCount(pageNo int) (int, error)
```

### Delete Methods

```go
func (gp *GoPdf) DeleteElement(pageNo int, elementIndex int) error
func (gp *GoPdf) DeleteElementsByType(pageNo int, elemType ContentElementType) (int, error)
func (gp *GoPdf) DeleteElementsInRect(pageNo int, rx, ry, rw, rh float64) (int, error)
func (gp *GoPdf) ClearPage(pageNo int) error
```

### Modify Methods

```go
func (gp *GoPdf) ModifyTextElement(pageNo int, elementIndex int, newText string) error
func (gp *GoPdf) ModifyElementPosition(pageNo int, elementIndex int, x, y float64) error
```

### Insert Methods

```go
func (gp *GoPdf) InsertLineElement(pageNo int, x1, y1, x2, y2 float64) error
func (gp *GoPdf) InsertRectElement(pageNo int, x, y, w, h float64, style string) error
func (gp *GoPdf) InsertOvalElement(pageNo int, x1, y1, x2, y2 float64) error
func (gp *GoPdf) InsertElementAt(pageNo int, elementIndex int, newElement ICacheContent) error
func (gp *GoPdf) ReplaceElement(pageNo int, elementIndex int, newElement ICacheContent) error
```

---

## PDF Version Control

```go
type PDFVersion int

const (
    PDFVersion14 PDFVersion = 14 // PDF 1.4
    PDFVersion15 PDFVersion = 15 // PDF 1.5
    PDFVersion16 PDFVersion = 16 // PDF 1.6
    PDFVersion17 PDFVersion = 17 // PDF 1.7 (default)
    PDFVersion20 PDFVersion = 20 // PDF 2.0
)

func (v PDFVersion) String() string  // "1.7"
func (v PDFVersion) Header() string  // "%PDF-1.7"

func (gp *GoPdf) SetPDFVersion(v PDFVersion)
func (gp *GoPdf) GetPDFVersion() PDFVersion
```

---

## Garbage Collection

```go
type GarbageCollectLevel int

const (
    GCNone    GarbageCollectLevel = 0 // No-op
    GCCompact GarbageCollectLevel = 1 // Remove null objects, renumber
    GCDedup   GarbageCollectLevel = 2 // Also deduplicate identical objects
)

func (gp *GoPdf) GarbageCollect(level GarbageCollectLevel) int
func (gp *GoPdf) GetObjectCount() int
func (gp *GoPdf) GetLiveObjectCount() int
```

`GarbageCollect` removes null/deleted objects left by `DeletePage` and other operations. Returns the number of objects removed.

---

## Page Labels

```go
type PageLabelStyle string

const (
    PageLabelDecimal    PageLabelStyle = "D" // 1, 2, 3, ...
    PageLabelRomanUpper PageLabelStyle = "R" // I, II, III, ...
    PageLabelRomanLower PageLabelStyle = "r" // i, ii, iii, ...
    PageLabelAlphaUpper PageLabelStyle = "A" // A, B, C, ...
    PageLabelAlphaLower PageLabelStyle = "a" // a, b, c, ...
    PageLabelNone       PageLabelStyle = ""  // No numbering
)

type PageLabel struct {
    PageIndex int            // 0-based page index where this range starts
    Style     PageLabelStyle // Numbering style
    Prefix    string         // Optional prefix (e.g. "Appendix ")
    Start     int            // Starting number (default 1)
}

func (gp *GoPdf) SetPageLabels(labels []PageLabel)
func (gp *GoPdf) GetPageLabels() []PageLabel
```

---

## Object ID

```go
type ObjID int

func (id ObjID) Index() int     // 0-based array index
func (id ObjID) Ref() int       // 1-based PDF object reference
func (id ObjID) RefStr() string // "5 0 R"
func (id ObjID) IsValid() bool
```

Typed wrapper for PDF object identifiers, providing type safety over raw int indices.

---

## XMP Metadata

```go
type XMPMetadata struct {
    // Dublin Core
    Title       string
    Creator     []string
    Description string
    Subject     []string
    Rights      string
    Language    string

    // XMP Basic
    CreatorTool string
    CreateDate  time.Time
    ModifyDate  time.Time

    // PDF-specific
    Producer string
    Keywords string
    Trapped  string // "True", "False", "Unknown"

    // PDF/A conformance
    PDFAConformance string // "A", "B", "U"
    PDFAPart        int    // 1, 2, 3

    // Custom properties
    Custom map[string]string
}

func (gp *GoPdf) SetXMPMetadata(meta XMPMetadata)
func (gp *GoPdf) GetXMPMetadata() *XMPMetadata
```

Embeds an XMP metadata stream in the PDF catalog. Supports Dublin Core, XMP Basic, PDF-specific, and PDF/A conformance properties.

---

## Incremental Save

```go
func (gp *GoPdf) IncrementalSave(originalData []byte, modifiedIndices []int) ([]byte, error)
func (gp *GoPdf) WriteIncrementalPdf(pdfPath string, originalData []byte, modifiedIndices []int) error
```

Appends only modified objects to the original PDF data as an incremental update. If `modifiedIndices` is nil, all objects are written. This is significantly faster than a full rewrite for large documents.

---

## Document Cloning

```go
func (gp *GoPdf) Clone() (*GoPdf, error)
```

Creates a deep copy of the GoPdf instance by serializing and re-importing. The clone is fully independent — changes to one do not affect the other. Header/footer callbacks are not cloned.

---

## Document Scrubbing

```go
func (gp *GoPdf) Scrub(opt ScrubOption)
func DefaultScrubOption() ScrubOption
```

Remove potentially sensitive data from the PDF. Inspired by PyMuPDF's `Document.scrub()`.

### ScrubOption

```go
type ScrubOption struct {
    Metadata      bool // Remove standard PDF metadata (/Info dictionary)
    XMLMetadata   bool // Remove XMP metadata stream
    EmbeddedFiles bool // Remove all embedded file attachments
    PageLabels    bool // Remove page label definitions
}
```

`DefaultScrubOption()` returns a `ScrubOption` with all fields set to `true`.

After scrubbing, call `GarbageCollect(GCCompact)` and save to ensure removed data is physically purged.

---

## Optional Content Groups (Layers)

```go
func (gp *GoPdf) AddOCG(ocg OCG) OCG
func (gp *GoPdf) GetOCGs() []OCG
```

Add PDF layers (Optional Content Groups) for selective visibility. Layers appear in the PDF viewer's layer panel.

### OCG

```go
type OCG struct {
    Name     string    // Display name of the layer
    Intent   OCGIntent // Visibility intent ("View" or "Design")
    On       bool      // Initially visible
}
```

### OCGIntent

```go
type OCGIntent string

const (
    OCGIntentView   OCGIntent = "View"   // For viewing purposes
    OCGIntentDesign OCGIntent = "Design" // For design purposes
)
```

---

## Page Layout & Page Mode

```go
func (gp *GoPdf) SetPageLayout(layout PageLayout)
func (gp *GoPdf) GetPageLayout() PageLayout
func (gp *GoPdf) SetPageMode(mode PageMode)
func (gp *GoPdf) GetPageMode() PageMode
```

Control how the PDF viewer displays the document when opened.

### PageLayout

```go
type PageLayout string

const (
    PageLayoutSinglePage      PageLayout = "SinglePage"      // One page at a time
    PageLayoutOneColumn       PageLayout = "OneColumn"       // Continuous column
    PageLayoutTwoColumnLeft   PageLayout = "TwoColumnLeft"   // Two columns, odd on left
    PageLayoutTwoColumnRight  PageLayout = "TwoColumnRight"  // Two columns, odd on right
    PageLayoutTwoPageLeft     PageLayout = "TwoPageLeft"     // Two pages, odd on left
    PageLayoutTwoPageRight    PageLayout = "TwoPageRight"    // Two pages, odd on right
)
```

### PageMode

```go
type PageMode string

const (
    PageModeUseNone        PageMode = "UseNone"        // No panel (default)
    PageModeUseOutlines    PageMode = "UseOutlines"    // Bookmarks panel
    PageModeUseThumbs      PageMode = "UseThumbs"      // Thumbnails panel
    PageModeFullScreen     PageMode = "FullScreen"     // Full-screen mode
    PageModeUseOC          PageMode = "UseOC"          // Layers panel
    PageModeUseAttachments PageMode = "UseAttachments" // Attachments panel
)
```

---

## Document Statistics

```go
func (gp *GoPdf) GetDocumentStats() DocumentStats
func (gp *GoPdf) GetFonts() []FontInfo
```

### DocumentStats

```go
type DocumentStats struct {
    PageCount          int        // Total number of pages
    ObjectCount        int        // Total number of PDF objects
    LiveObjectCount    int        // Number of non-null objects
    FontCount          int        // Number of font objects
    ImageCount         int        // Number of image objects
    ContentStreamCount int        // Number of content stream objects
    HasOutlines        bool       // Document has bookmarks
    HasEmbeddedFiles   bool       // Document has attachments
    HasXMPMetadata     bool       // XMP metadata is set
    HasPageLabels      bool       // Page labels are defined
    HasOCGs            bool       // Optional content groups are defined
    PDFVersion         PDFVersion // Configured PDF version
}
```

### FontInfo

```go
type FontInfo struct {
    Family     string // Font family name
    Style      int    // Font style (Regular, Bold, Italic)
    IsEmbedded bool   // Whether the font file is embedded
    Index      int    // Internal object index
}
```

---

## TOC / Bookmarks

```go
func (gp *GoPdf) GetTOC() []TOCItem
func (gp *GoPdf) SetTOC(items []TOCItem) error
```

Read and write the document's outline (bookmark) tree. `GetTOC` returns a flat list with hierarchy levels. `SetTOC` replaces the entire outline tree.

### TOCItem

```go
type TOCItem struct {
    Level  int     // Hierarchy level (1 = top level)
    Title  string  // Bookmark title text
    PageNo int     // 1-based target page number
    Y      float64 // Vertical position on target page (points from top)
}
```

`SetTOC` validation rules:
- The first item must have `Level` 1.
- Levels may increase by at most 1 from one item to the next.
- Returns `ErrInvalidTOCLevel` on validation failure.

---

## Text Extraction

```go
func ExtractTextFromPage(pdfData []byte, pageIndex int) ([]ExtractedText, error)
func ExtractTextFromAllPages(pdfData []byte) (map[int][]ExtractedText, error)
func ExtractPageText(pdfData []byte, pageIndex int) (string, error)
func ExtractPageLines(pdfData []byte, pageIndex int) ([]TextLine, error)
func ExtractAllPagesLines(pdfData []byte) (map[int][]TextLine, error)
func MergeTextRuns(runs []ExtractedText) []ExtractedText
func GroupIntoTextLines(runs []ExtractedText) []TextLine
```

Extract text content from existing PDF files. These are package-level functions (not methods on GoPdf).

- `ExtractTextFromPage` — extracts text items with position, font, and size from a single page (0-based index).
- `ExtractTextFromAllPages` — extracts text from all pages, returning a map of page index to text items.
- `ExtractPageText` — convenience wrapper that returns all text from a page as a single string.
- `ExtractPageLines` — merges fragmentary runs into readable words and lines (recommended for real-world PDFs with per-glyph text operators).
- `ExtractAllPagesLines` — parses the PDF once and returns merged lines for every page (prefer over looping `ExtractPageLines`).
- `MergeTextRuns` / `GroupIntoTextLines` — the underlying position-aware merging helpers.

The extraction pipeline supports object streams (PDF 1.5+), **encrypted documents** (`ExtractTextFromPageWithPassword` / `ExtractTextFromAllPagesWithPassword`; RC4 R2/R3 and AES-128/256 R4/R6), **Form XObjects** (text nested in /Subtype /Form stamps/letterheads is extracted), predefined CMaps, /Differences glyph names, WinAnsi/MacRoman/PDFDoc encodings, legacy GBK/Big5/Shift-JIS/EUC encodings, glyph advances from /Widths and CID /W arrays, TJ kerning spaces, and PDF 2.0 UTF-16BE/UTF-8 text strings.

### ExtractedText

```go
type ExtractedText struct {
    Text     string  // The extracted text string
    X        float64 // Horizontal position
    Y        float64 // Vertical position
    FontName string  // PDF font resource name (e.g. "LiberationSerif-Regular")
    FontSize float64 // Font size in points
    Width    float64 // Estimated advance width (set by MergeTextRuns; 0 when unknown)
}
```

### TextLine

```go
type TextLine struct {
    Y     float64    // Baseline of the line (top-down page coordinates)
    Words []TextWord // Merged words, left to right
    Text  string     // The line text with spacing between words
}
```

## Document Handle (parse once, query many)

```go
doc, err := gopdf.OpenPDFDocument(pdfData)   // or OpenPDFDocumentFromFile(path)
defer nil
n := doc.PageCount()
info := doc.Info()                            // PdfInfo (cached)
toc := doc.TOC()                              // outline tree (cached)
lines, _ := doc.Lines(0)                      // reading-order lines (cached)
paras, _ := doc.Paragraphs(0)                 // reconstructed paragraphs (cached)
words, _ := doc.Words(0)                      // position-aware words (cached)
drawings, _ := doc.Drawings(0)                // vector graphics (cached)
tables, _ := doc.Tables(0)                    // detected tables (cached; reuses drawings+lines)
images, _ := doc.Images(0)                    // image placements (cached)
text, _ := doc.Text(0)                        // newline-joined line text
txt, _ := doc.TextInRect(0, gopdf.ClipRect{...})
res := doc.Search("query", gopdf.SearchOptions{Dehyphenate: true})
```

`PDFDoc` parses the file once and caches every per-page result, so
walking pages through several APIs decompresses each content stream
exactly once — measured 42× faster than three package-level calls per
page on a graphics-heavy corpus document.

### Paragraphs, Regions and Vector Graphics

```go
func ExtractPageParagraphs(pdfData []byte, pageIndex int) ([]TextParagraph, error)
func ExtractAllPagesParagraphs(pdfData []byte) (map[int][]TextParagraph, error)
func ExtractTextInRect(pdfData []byte, pageIndex int, clip ClipRect) (string, error)
func ExtractTextWordsInRect(pdfData []byte, pageIndex int, clip ClipRect) ([]TextWord, error)
func WordsInRect(words []TextWord, clip ClipRect) []TextWord
func ExtractPageDrawings(pdfData []byte, pageIndex int) ([]Drawing, error)
func ExtractDrawingsFromFile(path string, pageIndex int) ([]Drawing, error)
```

### Table Detection

```go
func ExtractTablesFromPage(pdfData []byte, pageIndex int) ([]Table, error)
func ExtractTablesFromFile(path string, pageIndex int) ([]Table, error)

type TableCell struct {
    X, Y, W, H float64
    Text       string
}
type Table struct {
    X, Y, W, H float64 // bounding box
    ColCount   int
    Rows       [][]TableCell
}
func (t *Table) ToCSV() string
```

`ToCSV` renders the detected table as RFC 4180 CSV.

Detects ruled tables (PyMuPDF `find_tables` parity): border lines are grouped into crossing-connected components (each component is a candidate table), clustered into row/column grids, and cells are filled with position-aware words. Coordinates honor `/Rotate`. Borderless tables are not detected — there is no grid to cluster.

- `ExtractPageParagraphs` — the highest-level text API: merges reading-order lines back into paragraphs. Line wraps join with spaces, hyphenated splits join without the hyphen (`"transduc-" + "tion"` → `"transduction"`), first-line indents and terminal punctuation start new paragraphs.
- `ExtractTextInRect` / `ExtractTextWordsInRect` — region-scoped extraction; coordinates are visual page points (top-down, honoring `/Rotate`).
- `ExtractPageDrawings` — vector graphics extraction: lines, polylines, rects and bezier curves with stroke/fill colors (RGB and DeviceCMYK), stroke width and paint op. Bezier curves are flattened. Walks form XObjects and honors `/Rotate`.

### Text Search with Options

```go
func SearchTextWithOptions(pdfData []byte, query string, opt SearchOptions) ([]TextSearchResult, error)
func SearchTextOnPageWithOptions(pdfData []byte, pageIndex int, query string, opt SearchOptions) ([]TextSearchResult, error)

type SearchOptions struct {
    CaseInsensitive bool
    Dehyphenate     bool // match across hyphenated line wraps
}
```

With `Dehyphenate: true`, a query like "computational" matches text wrapped as `"computa-"` / `"tional"`; the reported rectangle is the line containing the match start.

### Document Metadata (read)

```go
func GetPDFInfo(pdfData []byte) (PdfInfo, error)
func GetPDFInfoFromFile(path string) (PdfInfo, error)
```

Reads the `/Info` dictionary: title, author, subject, keywords, creator, producer, creation and modification dates. The writer side is `SetInfo(PdfInfo)`.

### TOC of Existing PDFs (read)

```go
func ExtractTOC(pdfData []byte) ([]TOCEntry, error)
func ExtractTOCFromFile(path string) ([]TOCEntry, error)

type TOCEntry struct {
    Level int     // hierarchy depth (1 = top)
    Title string
    Page  int     // 1-based destination page (0 when unresolved)
    Y     float64 // destination position in points from the page top
}
```

Walks the outline tree of an existing PDF. (The writer side is `SetTOC` / `AddOutline` / `GetTOC` on a GoPdf instance.)

### Clipped Rendering

```go
opt := gopdf.RenderOption{DPI: 150, Clip: &gopdf.ClipRect{X: 100, Y: 100, W: 200, H: 120}}
img, err := gopdf.RenderPageToImage(data, 0, opt)
```

`Clip` restricts rendering to a region of the page in visual points; rendering honors `/Rotate` (the output canvas matches the visual page orientation, and 2× supersampling is applied for hairline fidelity).

---

## Image Extraction

```go
func ExtractImagesFromPage(pdfData []byte, pageIndex int) ([]ExtractedImage, error)
func ExtractImagesFromAllPages(pdfData []byte) (map[int][]ExtractedImage, error)
```

Extract image metadata and data from existing PDF files. These are package-level functions.

### ExtractedImage

```go
type ExtractedImage struct {
    Name             string  // XObject resource name (e.g. "/Im1")
    Width            int     // Image width in pixels
    Height           int     // Image height in pixels
    BitsPerComponent int     // Bits per color component
    ColorSpace       string  // Color space name (e.g. "DeviceRGB")
    Filter           string  // Compression filter (e.g. "DCTDecode")
    Data             []byte  // Raw image data
    ObjNum           int     // PDF object number
    X, Y             float64 // Position on the page
    DisplayWidth     float64 // Rendered width on the page
    DisplayHeight    float64 // Rendered height on the page
}

func (img *ExtractedImage) GetImageFormat() string
```

`GetImageFormat` returns the likely image format based on the filter: "jpeg", "jp2", "tiff", "png", or "raw".

---

## Form Fields (AcroForm)

```go
func (gp *GoPdf) AddFormField(field FormField) error
func (gp *GoPdf) AddTextField(name string, x, y, w, h float64) error
func (gp *GoPdf) AddCheckbox(name string, x, y, size float64, checked bool) error
func (gp *GoPdf) AddDropdown(name string, x, y, w, h float64, options []string) error
func (gp *GoPdf) AddSignatureField(name string, x, y, w, h float64) error
func (gp *GoPdf) GetFormFields() []FormField
```

Add interactive form fields (widgets) to PDF pages. Fields appear as interactive elements in PDF viewers.

### FormFieldType

```go
const (
    FormFieldText      FormFieldType = iota // Text input
    FormFieldCheckbox                       // Checkbox
    FormFieldRadio                          // Radio button
    FormFieldChoice                         // Dropdown/list
    FormFieldButton                         // Push button
    FormFieldSignature                      // Signature
)
```

### FormField

```go
type FormField struct {
    Type        FormFieldType
    Name        string     // Unique field identifier
    X, Y        float64   // Top-left corner position
    W, H        float64   // Width and height
    Value       string     // Default value
    FontFamily  string     // Font family (must be pre-loaded)
    FontSize    float64    // Font size (default 12)
    Options     []string   // Choices for Choice fields
    MaxLen      int        // Max character length for text fields
    Multiline   bool       // Multi-line text input
    ReadOnly    bool       // Non-editable
    Required    bool       // Required field
    Color       [3]uint8   // Text color [R, G, B]
    BorderColor [3]uint8   // Border color [R, G, B]
    FillColor   [3]uint8   // Background fill color [R, G, B]
    HasBorder   bool       // Draw border
    HasFill     bool       // Draw background fill
    Checked     bool       // Initial checkbox state
}
```

---

## Digital Signatures

```go
func (gp *GoPdf) SignPDF(cfg SignatureConfig, w io.Writer) error
func (gp *GoPdf) SignPDFToFile(cfg SignatureConfig, path string) error
func VerifySignature(pdfData []byte) ([]SignatureVerifyResult, error)
func VerifySignatureFromFile(path string) ([]SignatureVerifyResult, error)
```

Sign PDF documents with PKCS#7 detached signatures and verify existing signatures.

### SignatureConfig

```go
type SignatureConfig struct {
    Certificate      *x509.Certificate   // Signing certificate
    CertificateChain []*x509.Certificate // Optional intermediate chain
    PrivateKey       crypto.Signer       // RSA or ECDSA private key
    Reason           string              // Reason for signing
    Location         string              // Signing location
    ContactInfo      string              // Signer contact info
    Name             string              // Signer name (defaults to cert CN)
    SignTime         time.Time           // Signing time (defaults to now)
    SignatureFieldName string            // Field name (defaults to "Signature1")
    Visible          bool                // Visible signature appearance
    X, Y, W, H      float64             // Visible signature rectangle
    PageNo           int                 // 1-based page number
}
```

### Helper Functions

```go
func LoadCertificateFromPEM(certPath string) (*x509.Certificate, error)
func LoadPrivateKeyFromPEM(keyPath string) (crypto.Signer, error)
func ParseCertificatePEM(data []byte) (*x509.Certificate, error)
func ParsePrivateKeyPEM(data []byte) (crypto.Signer, error)
```
