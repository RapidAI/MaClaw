package guiapp

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

// A page with fewer non-whitespace runes than this still counts as little
// text, so the preview also shows the next page. Kept in step with
// ATTACHMENT_PREVIEW_SPARSE_RUNES in attachmentOpeningPreview.ts.
const attachmentPreviewSparseRunes = 80

// attachmentPreviewMaxPages stops a run of blank pages from attaching an
// unbounded preview. The walk still continues while each page is sparse.
const attachmentPreviewMaxPages = 8

// attachmentPreviewTextCap bounds the text copied into the bridge payload.
// Page selection uses the full extract before this cut.
const attachmentPreviewTextCap = 4000

// attachmentPreviewTokenTTL keeps a chat preview loading after the 10-minute
// file-viewer lease would have expired. The transcript stays on screen.
const attachmentPreviewTokenTTL = 6 * time.Hour

// attachmentPreviewPDFMaxBytes is how much of a PDF the chat shot may copy.
// The shot fetches the preview and holds the whole body in one buffer, with
// range and stream disabled. 128 MiB still covers a scan past the 32 MiB
// office text budget. The file viewer keeps its own range cap.
const attachmentPreviewPDFMaxBytes int64 = 128 << 20

// Slide rasters are shown at ATTACHMENT_DOC_PREVIEW_MAX_WIDTH (640 CSS px).
// Twice that stays sharp on hi-DPI without shipping a full-deck render.
const attachmentPreviewSlideWidth = 1280

// AIAssistantOpeningPreview is the first-page preview for one attached
// document. Pages already follow the sparse-page rule: page 1, then the next
// page while the previous one has little text.
type AIAssistantOpeningPreview struct {
	Kind       string                          `json:"kind"`
	PreviewURL string                          `json:"preview_url,omitempty"`
	Pages      []AIAssistantOpeningPreviewPage `json:"pages"`
}

// AIAssistantOpeningPreviewPage is one page or slide in that preview.
type AIAssistantOpeningPreviewPage struct {
	Index        int     `json:"index"`
	Text         string  `json:"text,omitempty"`
	Width        float64 `json:"width,omitempty"`
	Height       float64 `json:"height,omitempty"`
	ImageDataURL string  `json:"image_data_url,omitempty"`
}

func attachmentPreviewRuneCount(text string) int {
	n := 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		n++
	}
	return n
}

func attachmentPreviewPageIsSparse(text string) bool {
	return attachmentPreviewRuneCount(text) < attachmentPreviewSparseRunes
}

// attachmentOpeningPreviewCount is how many leading pages to show.
// The first page is always included. A sparse page pulls in the next one.
func attachmentOpeningPreviewCount(texts []string) int {
	if len(texts) == 0 {
		return 0
	}
	count := 1
	limit := len(texts)
	if limit > attachmentPreviewMaxPages {
		limit = attachmentPreviewMaxPages
	}
	for count < limit && attachmentPreviewPageIsSparse(texts[count-1]) {
		count++
	}
	return count
}

func capAttachmentPreviewText(text string) string {
	runes := []rune(text)
	if len(runes) <= attachmentPreviewTextCap {
		return text
	}
	return string(runes[:attachmentPreviewTextCap])
}

// AIAssistantAttachmentOpeningPreview returns the opening pages of a PDF,
// Word, or PowerPoint the user attached in the assistant. Other previewable
// documents are paged in the webview from the existing text preview.
func (a *App) AIAssistantAttachmentOpeningPreview(path string) (out *AIAssistantOpeningPreview, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			out = nil
			err = fmt.Errorf("无法预览该文档")
		}
	}()
	cleaned, info, err := openAttachmentPreviewFile(a, path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(cleaned)) {
	case ".pdf":
		return previewPDFOpening(cleaned, info.Size())
	case ".docx":
		return previewOpeningBinary(cleaned, "docx", info)
	case ".pptx":
		return previewPPTXOpening(cleaned)
	default:
		return nil, fmt.Errorf("无法预览该文档")
	}
}

func openAttachmentPreviewFile(a *App, path string) (string, os.FileInfo, error) {
	cleaned, err := a.normalizeOpenPathForApp(path)
	if err != nil {
		return "", nil, fmt.Errorf("文件路径无效")
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "", nil, fmt.Errorf("文件路径为空")
	}
	if abs, absErr := filepath.Abs(cleaned); absErr == nil {
		cleaned = abs
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("文件不存在")
		}
		return "", nil, fmt.Errorf("无法打开文件")
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("不是文件")
	}
	if info.Size() <= 0 {
		return "", nil, fmt.Errorf("文件是空的")
	}
	// The office reader cap is a whole-file text budget. A PDF page shot copies
	// that file into the webview, so it uses the shot buffer instead: above
	// the text cap, and far below the viewer's range cap. Word and slides
	// keep the text cap because their opening preview reads the file.
	if info.Size() > attachmentPreviewSizeLimit(cleaned) {
		return "", nil, fmt.Errorf("文件过大，无法预览")
	}
	return cleaned, info, nil
}

// attachmentPreviewSizeLimit is how large a file the opening preview may open.
// A PDF stops at the chat-shot buffer. Other documents stay on the office
// text-extraction ceiling.
func attachmentPreviewSizeLimit(path string) int64 {
	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		return attachmentPreviewPDFMaxBytes
	}
	return agent.MaxOfficeReadFileBytes
}

func previewOpeningBinary(path, kind string, info os.FileInfo) (*AIAssistantOpeningPreview, error) {
	if info.Size() <= 0 {
		return nil, fmt.Errorf("无法预览该文档")
	}
	token := issueTaskResultPreviewLease(path, attachmentPreviewTokenTTL)
	if token == "" {
		return nil, fmt.Errorf("无法预览该文档")
	}
	return &AIAssistantOpeningPreview{
		Kind:       kind,
		PreviewURL: taskResultPreviewHTTPPath + "?t=" + token,
		Pages:      []AIAssistantOpeningPreviewPage{},
	}, nil
}

func previewPDFOpening(path string, size int64) (*AIAssistantOpeningPreview, error) {
	if size <= 0 {
		return nil, fmt.Errorf("不是有效的 PDF 文件")
	}
	// Past the text budget, do not slurp the file to pick a page. The shot
	// draws page 1 from the lease. Sparse-page selection stays on the small
	// files the office reader is willing to parse.
	if size > agent.MaxOfficeReadFileBytes {
		return previewPDFOpeningLeaseOnly(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法打开文件")
	}
	peek := data
	if len(peek) > taskResultPreviewPDFPeekBytes {
		peek = peek[:taskResultPreviewPDFPeekBytes]
	}
	if !looksLikePDF(peek) {
		return nil, fmt.Errorf("不是有效的 PDF 文件")
	}
	pageCount, err := gopdf2.GetSourcePDFPageCountFromBytes(data)
	if err != nil || pageCount <= 0 {
		return nil, fmt.Errorf("无法预览该文档")
	}
	sizes, _ := gopdf2.GetSourcePDFPageSizesFromBytes(data)
	texts := make([]string, 0, min(pageCount, attachmentPreviewMaxPages))
	for i := 0; i < pageCount && len(texts) < attachmentPreviewMaxPages; i++ {
		texts = append(texts, safeExtractPDFPageText(data, i))
		if !attachmentPreviewPageIsSparse(texts[len(texts)-1]) {
			break
		}
	}
	count := attachmentOpeningPreviewCount(texts)
	if count > len(texts) {
		count = len(texts)
	}
	token := issueTaskResultPreviewLease(path, attachmentPreviewTokenTTL)
	if token == "" {
		return nil, fmt.Errorf("无法预览该文档")
	}
	pages := make([]AIAssistantOpeningPreviewPage, 0, count)
	for i := 0; i < count; i++ {
		page := AIAssistantOpeningPreviewPage{
			Index: i + 1,
			Text:  capAttachmentPreviewText(texts[i]),
		}
		if info, ok := sizes[i+1]; ok {
			page.Width = info.Width
			page.Height = info.Height
		}
		pages = append(pages, page)
	}
	return &AIAssistantOpeningPreview{
		Kind:       "pdf",
		PreviewURL: taskResultPreviewHTTPPath + "?t=" + token,
		Pages:      pages,
	}, nil
}

// previewPDFOpeningLeaseOnly issues the viewer lease after a header peek.
// Page 1 is enough: the chat shot rasters that page, and there is no text
// to skip a sparse cover.
func previewPDFOpeningLeaseOnly(path string) (*AIAssistantOpeningPreview, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("无法打开文件")
	}
	ok, peekErr := peekPDFHeader(file)
	closeErr := file.Close()
	if peekErr != nil || closeErr != nil {
		return nil, fmt.Errorf("无法读取文件")
	}
	if !ok {
		return nil, fmt.Errorf("不是有效的 PDF 文件")
	}
	token := issueTaskResultPreviewLease(path, attachmentPreviewTokenTTL)
	if token == "" {
		return nil, fmt.Errorf("无法预览该文档")
	}
	return &AIAssistantOpeningPreview{
		Kind:       "pdf",
		PreviewURL: taskResultPreviewHTTPPath + "?t=" + token,
		Pages:      []AIAssistantOpeningPreviewPage{{Index: 1}},
	}, nil
}

func safeExtractPDFPageText(data []byte, pageIndex int) (text string) {
	defer func() { _ = recover() }()
	text, err := agent.PDFPageBody(data, pageIndex)
	if err != nil {
		return ""
	}
	return text
}

func previewPPTXOpening(path string) (*AIAssistantOpeningPreview, error) {
	texts, err := pptxSlideTexts(path)
	count := 1
	if err == nil && len(texts) > 0 {
		count = attachmentOpeningPreviewCount(texts)
	}
	if count < 1 {
		count = 1
	}
	dir, dirErr := os.MkdirTemp("", "maclaw-attach-pptx-")
	if dirErr != nil {
		return nil, fmt.Errorf("无法预览该文档")
	}
	defer os.RemoveAll(dir)
	rendered, renderErr := pptx.RenderPreview(path, pptx.PreviewOptions{
		OutputDir:   dir,
		Width:       attachmentPreviewSlideWidth,
		MaxSlides:   count,
		Format:      "jpeg",
		JPEGQuality: 82,
	})
	pages := make([]AIAssistantOpeningPreviewPage, 0, count)
	if renderErr == nil && rendered != nil {
		limit := len(rendered.Images)
		if limit > count {
			limit = count
		}
		for i := 0; i < limit; i++ {
			page := AIAssistantOpeningPreviewPage{Index: i + 1}
			if i < len(texts) {
				page.Text = capAttachmentPreviewText(texts[i])
			}
			if url, w, h := slidePreviewDataURL(rendered.Images[i]); url != "" {
				page.ImageDataURL = url
				page.Width = float64(w)
				page.Height = float64(h)
			}
			pages = append(pages, page)
		}
	}
	if len(pages) == 0 && len(texts) > 0 {
		for i := 0; i < count && i < len(texts); i++ {
			pages = append(pages, AIAssistantOpeningPreviewPage{
				Index: i + 1,
				Text:  capAttachmentPreviewText(texts[i]),
			})
		}
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("无法预览该文档")
	}
	return &AIAssistantOpeningPreview{Kind: "pptx", Pages: pages}, nil
}

func pptxSlideTexts(path string) ([]string, error) {
	pres, err := pptx.Read(path)
	if err != nil {
		return nil, err
	}
	texts := make([]string, 0, len(pres.Slides))
	for _, slide := range pres.Slides {
		texts = append(texts, slidePlainText(slide))
	}
	return texts, nil
}

func slidePlainText(slide pptx.Slide) string {
	var b strings.Builder
	for _, shape := range slide.Shapes {
		if shape.Text != nil {
			for _, paragraph := range shape.Text.Paragraphs {
				for _, run := range paragraph.Runs {
					b.WriteString(run.Text)
				}
				b.WriteByte('\n')
			}
		}
		if shape.Table != nil {
			for _, row := range shape.Table.Rows {
				for _, cell := range row.Cells {
					b.WriteString(cell.Text)
					b.WriteByte(' ')
				}
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

func slidePreviewDataURL(path string) (string, int, int) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return "", 0, 0
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return "", 0, 0
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(raw), cfg.Width, cfg.Height
}
