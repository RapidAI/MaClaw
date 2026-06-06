package knowledge

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ImageHints provides context for generating image descriptions.
type ImageHints struct {
	FileName      string // original filename (e.g. "architecture.png")
	ContextBefore string // text before the image (truncated to 200 rune)
	ContextAfter  string // text after the image (truncated to 200 rune)
	AltText       string // alt text from markdown ![alt]() or OOXML descr attribute
	ParentTitle   string // section/slide title the image belongs to
	PageNumber    int    // page number (PDF/PPTX)
	SourceTitle   string // parent document title
}

// ImageDescription is the result of describing an image.
type ImageDescription struct {
	Title       string   // short title (e.g. "系统架构图")
	Description string   // detailed description (2-4 sentences)
	OCRText     string   // text extracted via OCR
	Entities    []string // recognized entities
}

// ImageDescriber generates descriptions for images using available methods.
type ImageDescriber interface {
	Describe(ctx context.Context, imagePath string, hints ImageHints) (ImageDescription, error)
	Close()
}

// OCRProvider is the interface for OCR recognition (matches browser.OCRProvider).
type OCRProvider interface {
	Recognize(pngBase64 string) ([]OCRResult, error)
	IsAvailable() bool
	Close()
}

// OCRResult matches browser.OCRResult (text + bounding box).
type OCRResult struct {
	Text  string    `json:"text"`
	Box   [][]int   `json:"box,omitempty"`
	Score float64   `json:"score,omitempty"`
}

// CompositeImageDescriber implements ImageDescriber with two-layer fallback:
// 1. Vision LLM (if configured and verified)
// 2. RapidOCR + context inference (always available fallback)
type CompositeImageDescriber struct {
	vision *VisionDescriber // nil if not configured
	ocr    OCRProvider      // nil if not available
}

// NewCompositeImageDescriber creates a describer with optional Vision and OCR providers.
func NewCompositeImageDescriber(vision *VisionDescriber, ocr OCRProvider) *CompositeImageDescriber {
	return &CompositeImageDescriber{
		vision: vision,
		ocr:    ocr,
	}
}

// Describe generates a description for the image at imagePath.
// Decision logic:
//   - Vision LLM configured + verified → use Vision LLM (+ supplement with OCR)
//   - Otherwise → OCR + context inference
func (c *CompositeImageDescriber) Describe(ctx context.Context, imagePath string, hints ImageHints) (ImageDescription, error) {
	// Layer 1: Vision LLM (best quality)
	if c.vision != nil && c.vision.IsVerified() {
		desc, err := c.vision.Describe(ctx, imagePath, hints)
		if err == nil {
			// Supplement with OCR text (Vision might miss some text)
			if c.ocr != nil && c.ocr.IsAvailable() {
				ocrText := c.ocrImage(imagePath)
				if ocrText != "" && desc.OCRText == "" {
					desc.OCRText = ocrText
				}
			}
			return desc, nil
		}
		// Vision runtime failure → degrade, clear verified
		c.vision.ClearVerified()
		log.Printf("[knowledge-image] vision LLM failed, falling back to OCR: %v", err)
	}

	// Layer 2: OCR + context inference
	return c.describeWithOCR(ctx, imagePath, hints)
}

// Close releases resources.
func (c *CompositeImageDescriber) Close() {
	if c.vision != nil {
		c.vision.Close()
	}
	// OCR lifecycle is managed by the caller (shared RapidOCRSidecar).
}

// describeWithOCR uses OCR text + context hints to generate a description.
func (c *CompositeImageDescriber) describeWithOCR(ctx context.Context, imagePath string, hints ImageHints) (ImageDescription, error) {
	var desc ImageDescription

	// Try OCR
	if c.ocr != nil && c.ocr.IsAvailable() {
		ocrText := c.ocrImage(imagePath)
		if ocrText != "" {
			desc.OCRText = ocrText
		}
	}

	// Infer title from hints
	desc.Title = inferImageTitle(hints)

	// Build description from available context
	desc.Description = inferImageDescription(hints, desc.OCRText)

	return desc, nil
}

// ocrImage reads an image file and runs OCR, returning concatenated text.
func (c *CompositeImageDescriber) ocrImage(imagePath string) string {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return ""
	}

	// RapidOCR expects PNG base64. For non-PNG formats, we still send base64
	// of the raw bytes — RapidOCR handles JPEG/PNG/BMP internally.
	b64 := base64.StdEncoding.EncodeToString(data)
	results, err := c.ocr.Recognize(b64)
	if err != nil {
		log.Printf("[knowledge-image] OCR failed for %s: %v", filepath.Base(imagePath), err)
		return ""
	}

	var texts []string
	for _, r := range results {
		text := strings.TrimSpace(r.Text)
		if text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, " ")
}

// inferImageTitle generates a title from available hints.
func inferImageTitle(hints ImageHints) string {
	// Priority: AltText > ParentTitle + filename > filename alone
	if hints.AltText != "" {
		return truncateRunes(hints.AltText, 60)
	}
	if hints.ParentTitle != "" {
		name := fileNameWithoutExt(hints.FileName)
		if name != "" {
			return fmt.Sprintf("%s - %s", hints.ParentTitle, name)
		}
		return hints.ParentTitle + " (图片)"
	}
	if hints.FileName != "" {
		return fileNameWithoutExt(hints.FileName)
	}
	if hints.PageNumber > 0 {
		return fmt.Sprintf("第%d页图片", hints.PageNumber)
	}
	return "图片"
}

// inferImageDescription builds a description from context hints and OCR text.
func inferImageDescription(hints ImageHints, ocrText string) string {
	var parts []string

	// Source context
	if hints.SourceTitle != "" && hints.PageNumber > 0 {
		parts = append(parts, fmt.Sprintf("来自《%s》第%d页", hints.SourceTitle, hints.PageNumber))
	} else if hints.SourceTitle != "" {
		parts = append(parts, fmt.Sprintf("来自《%s》", hints.SourceTitle))
	}

	// Section context
	if hints.ParentTitle != "" {
		parts = append(parts, fmt.Sprintf("章节: %s", hints.ParentTitle))
	}

	// Surrounding text context
	if hints.ContextBefore != "" {
		parts = append(parts, fmt.Sprintf("前文: %s", truncateRunes(hints.ContextBefore, 100)))
	}

	// OCR text
	if ocrText != "" {
		parts = append(parts, fmt.Sprintf("图中文字: %s", truncateRunes(ocrText, 300)))
	}

	if len(parts) == 0 {
		if hints.FileName != "" {
			return fmt.Sprintf("图片文件: %s", hints.FileName)
		}
		return "图片（无可用描述信息）"
	}

	return strings.Join(parts, "。")
}

// FormatImageNodeText formats the full text for a DocumentNode of type "image".
// This text is stored in Node.Text and indexed by FTS5.
func FormatImageNodeText(desc ImageDescription) string {
	var sb strings.Builder
	if desc.Title != "" {
		sb.WriteString(desc.Title)
		sb.WriteString("\n")
	}
	if desc.Description != "" {
		sb.WriteString(desc.Description)
		sb.WriteString("\n")
	}
	if desc.OCRText != "" {
		sb.WriteString("OCR: ")
		sb.WriteString(desc.OCRText)
	}
	return strings.TrimSpace(sb.String())
}

// --- helpers ---

func fileNameWithoutExt(name string) string {
	if name == "" {
		return ""
	}
	base := filepath.Base(name)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
