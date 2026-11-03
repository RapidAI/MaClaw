# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- **Text extraction hangs** on malformed content streams: `tokenize` no longer
  stalls on stray `)`/`>` delimiters (TAMReview.pdf, basicapi.pdf) and
  `parseCMap` no longer overflows on 0xFFFF bfranges
- **Pages invisible to the parser**: object streams (/ObjStm, PDF 1.5+) are
  now expanded; `/Type/Page` vs `/Pages` token detection fixed
- **Garbled text** for fonts without usable ToUnicode: /Differences glyph
  names, WinAnsi/MacRoman/PDFDoc tables, predefined CMaps (UniGB/UniJIS/
  UniKS-UCS2) and legacy CJK decoders (GBK/Big5/Shift-JIS/EUC via x/text)
- **Text rise leak**: `Ts` is now always emitted; superscript rise no longer
  bleeds into all following text of a page
- Filter chains (FlateDecode+PNG predictors, ASCIIHex, ASCII85) applied
  uniformly to content, CMap and object streams
- **Text position after `q`/`Q` image groups** (`parseTextOperatorsCtx`): the
  text extractor now keeps a graphics-state stack — `cm` inside a `q`…`Q`
  group composes with the current matrix and `Q` restores it. Previously the
  last image-paint `cm` leaked onto every later fragment, reporting
  e.g. `X=14472 Y=-69847` for a fragment drawn at `(72, 700.47)`
- **Inline `<img>` participates in the line height** (`renderTextLines`): a
  600pt figure previously advanced the cursor by a single text line, so the
  flow drew later text over the bitmap and box clipping silently dropped the
  image when it no longer fit
- **First line of a tight HTML box still draws**: the `!paginate` clip now
  fires only when a line starts past the box bottom; the restored default
  `<p>` margins no longer swallow the first line of an 18pt strip box

### Added
- **Position-aware text extraction**: glyph advances from /Widths, /W (incl.
  compact CID form) drive exact run positions; `MergeTextRuns`,
  `GroupIntoTextLines` and `ExtractPageLines` reconstruct words/lines with
  correct spacing from per-glyph Tj streams; TJ negative-kern spaces honored
- **html2pdf overhaul**:
  - CSS engine: `<style>` blocks, tag/`.class`/`#id` selectors, specificity
    cascade, inline-style override
  - fragment-based line layout: CJK per-character wrapping with kinsoku
    (no line-start punctuation / line-end openers), `text-align: justify`,
    `line-height`, `letter-spacing`, `text-indent`, `text-transform`,
    `white-space: pre`
  - real strikethrough (`<s>`/`<del>`), underline and background highlight
    per fragment
  - rich tables: colspan/rowspan, `<colgroup>`/`<col>`/cell widths, per-cell
    alignment, `<caption>`, background/border attributes and CSS
  - `InsertHTMLPaginated`: multi-page flow with page-top/bottom control,
    table row pagination, `OnPageBreak` callback
  - nested lists with `start=`/`type=` (decimal, alpha, roman), `<pre>`/
    `<code>`, extended named + numeric HTML entities, base64 data-URI images
  - spaces are drawn as real glyphs so generated PDFs round-trip losslessly
- **Table layout**: `SetAutoRowHeight` (content-measured wrapped rows),
  `SetPaginate` (multi-page tables with repeated headers and per-page
  segment borders), colspan cells via `NewRowCellSpan`
- **Formula rendering** (`DrawFormula`/`MeasureFormula`): LaTeX-lite subset —
  sub/superscripts, `\frac`, `\sqrt`, `\binom`, Greek letters, common
  operators and function names
- Corpus-based extraction test suite (test/corpus, auto-skipped when absent)
  covering ISO 32000-2 samples, an arXiv paper, a 55-page formula booklet,
  GBK Chinese, Arabic CID fonts and ObjStm-based forms
- `cmd/pdfprobe` and `cmd/pdfrender` diagnostic tools, `cmd/htmldemo` showcase

### Changed
- Raw parser is now the primary extraction backend (ledongthuc/pdf remains as
  fallback); ligature-aware ToUnicode maps (multi-rune bfchar destinations)
- Text inside **Form XObjects** is extracted — page content embedded in
  `/Subtype /Form` objects (stamps, letterheads, running headers) recursed
  with Do translation offsets and per-form fonts/resources
  (TAMReview.pdf: 2 pages/3.2k chars -> 23 pages/13.4k chars)
- PDF 2.0 UTF-8 text strings (EF BB BF BOM) decode like UTF-16BE
- `<script>`/`<style>` parse as raw text — markup inside scripts no longer
  corrupts the element tree

### Fixed (review hardening)
- **Text rise leak**: `Ts` is always emitted — superscript rise previously
  bled into all following text of a page (phantom-strikethrough artifacts)
- **Inline `<a>` links** inside paragraphs were unstyled and produced no
  clickable annotation; now styled and linked
- **text-align: center/right** never applied its computed line start —
  centered headings rendered left-aligned
- **HTML table `RepeatTableHeaders`** was stored but never drawn; `<thead>`
  rows now repeat on continuation pages
- **Rowspan rows no longer orphaned across page breaks**: pages break between
  row groups (rows joined by a rowspan cell stay together); groups taller
  than a whole page fall back to row-level breaking
- Default `<p>` spacing restored (paragraphs without CSS collapsed to a bare
  line-height gap)
- **Data race** in the cached CJK decoders (lazy map write) — immutable
  Encodings are now shared and Decoders created per call; concurrent
  extraction is race-detector clean
- `hexBytes` mutated its input, which alias stream data — now copies
- table segment borders use each segment's actual header height
- a { text-decoration: none } is honored; `rgba()` colors parse;
  `line-height` percentages parse; `*.class` selectors work
- tokenizer no longer hangs on stray `)`/`>`; CMap bfrange 0xFFFF overflow

### Performance (review rounds)
- Object-header lookup replaced the `{digit} 0 obj` regexp that consumed 76%
  of extraction CPU (byte-level scan with backward parsing) — arXiv paper
  extraction 125ms -> 49ms
- Zero-copy tokenizer and allocation-free number parsing — 55-page formula
  booklet 205ms -> 82ms (2.5-2.6x overall)
- `ExtractAllPagesLines` parses once for all-page line extraction;
  `RenderAllPagesToImages` parses once for all-page rendering
- CSS rules pre-bucketed by specificity; CJK decoder Encodings cached;
  repeated `<img>` srcs memoized per render

## [1.0.0] - 2025-02-11

### Features

- PDF generation with Unicode subfont embedding and automatic subsetting
- Open and modify existing PDFs via `OpenPDF` / `OpenPDFFromBytes` / `OpenPDFFromStream`
- HTML-to-PDF rendering via `InsertHTMLBox`
- Text and image watermarks with opacity, rotation, and tiling
- 20+ annotation types (sticky notes, highlights, shapes, redaction, etc.)
- Page manipulation: extract, merge, delete, copy, move, reorder
- Text extraction with positions (`ExtractTextFromPage` / `ExtractPageText`)
- Multi-format text extraction: plain text, blocks, words, HTML, JSON (`ExtractTextFormatted`)
- Text search and replace in content streams (`SearchText` / `ReplaceText`)
- Image extraction with metadata (`ExtractImagesFromPage`)
- Image recompression (`RecompressImages`)
- SVG insertion (`ImageSVG`)
- Basic page rendering (`RenderPageToImage`)
- Form fields / AcroForm: text, checkbox, dropdown, radio, button, signature
- Digital signatures: PKCS#7 signing and verification (`SignPDF` / `VerifySignature`)
- AES-128 and AES-256 encryption (`SetEncryption`)
- RC4 password protection
- Content element CRUD: list, query, delete, modify, reposition individual elements
- TOC / bookmarks: read and write hierarchical outline trees
- XMP metadata with Dublin Core and PDF/A support
- Embedded file attachments (full CRUD)
- Optional Content Groups (OCG / layers)
- Page labels, page layout, page mode
- Incremental save for fast updates on large documents
- Document cloning, scrubbing, garbage collection
- Linearization (web optimization)
- Content stream cleaning
- Colorspace conversion
- PDF/A validation (1a, 1b, 2a, 2b)
- PDF document comparison (`ComparePDF`)
- Page transformations: scale, rotate, skew (`ScalePage` / `TransformPage` / `ScalePageInPDF`)
- Enhanced redaction with custom colors and overlay text (`ApplyRedactionsEnhanced` / `RedactText`)
- Link CRUD: get, delete, extract links from existing PDFs
- Journalling (undo/redo)
- Geometry utilities: `RectFrom`, `Matrix`, `Distance`
- Drawing: lines, rectangles, rounded rectangles, ovals, polygons, polylines, curves, sectors
- Table layout, header/footer callbacks, placeholder text
- PDF version control (1.4–2.0)
- Pure Go, no CGO dependencies
