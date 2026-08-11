package knowledge

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"sort"
	"strings"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

// ExtractPDFImages extracts embedded images from a PDF file.
//
// The extraction is entirely in-process through GoPDF2, matching the pure-Go
// PDF inspection and OCR pipeline. Unsupported image encodings are skipped.
//
// Each returned node has:
//   - Type: NodeTypeImage
//   - Page: page number (if determinable)
//   - Metadata with format and context
//   - Metadata["_image_bytes_key"]: key into the returned bytes map
//
// Unsupported/unencodable image streams are skipped rather than turning a
// readable PDF import into an error; image extraction is best effort.
func ExtractPDFImages(source Source, filePath string, textNodes []DocumentNode) ([]DocumentNode, map[string][]byte, error) {
	nodes, imageBytes, err := extractPDFImagesNative(source, filePath, textNodes)
	if err == nil {
		return nodes, imageBytes, nil
	}

	// No extraction method available — this is not a fatal error.
	// Extraction is best effort. Do not put a user-controlled filename or a
	// parser/CLI error in logs: either may contain a local path or document
	// content. The import result already records the document status for the
	// user-facing workflow.
	logKnowledgeImageEvent("pdf_image_extraction_unavailable", SourceKindPDF, 0)
	return nil, nil, nil
}

// extractPDFImagesNative extracts through the supported in-process GoPDF2
// reader without spawning an external command.
//
// Some PDF filters represent raw pixel planes rather than an encoded image
// file. Those are deliberately skipped here: persisting bytes under a made-up
// .png extension would produce an unopenable knowledge asset. An image is
// accepted only when the standard decoder recognizes its serialized bytes.
func extractPDFImagesNative(source Source, filePath string, textNodes []DocumentNode) ([]DocumentNode, map[string][]byte, error) {
	pdfData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read PDF: %w", err)
	}
	pages, err := safeExtractPDFImages(pdfData)
	if err != nil {
		return nil, nil, fmt.Errorf("extract PDF images: %w", err)
	}
	pageTextMap := buildPageTextMap(textNodes)
	imageBytes := make(map[string][]byte)
	nodes := make([]DocumentNode, 0)
	for _, pageIndex := range sortedPDFImagePages(pages) {
		images := pages[pageIndex]
		pageNumber := pageIndex + 1
		for imageIndex, extracted := range images {
			data := extracted.Data
			// Avoid image.DecodeConfig on trivially small decorative objects.
			if len(data) < 500 {
				continue
			}
			_, detectedFormat, decodeErr := image.DecodeConfig(bytes.NewReader(data))
			if decodeErr != nil || !isImageFileExt("."+detectedFormat) {
				continue
			}
			nodeID := NewID("kdn")
			metadata := map[string]string{
				MetaImageFormat:    normalizeFormatName("." + detectedFormat),
				"_image_bytes_key": nodeID,
			}
			if context := pageTextMap[pageNumber]; context != "" {
				metadata["context_before"] = context
			}
			nodes = append(nodes, DocumentNode{
				ID:       nodeID,
				SourceID: source.ID,
				Type:     NodeTypeImage,
				Title:    fmt.Sprintf("PDF image %d.%d", pageNumber, imageIndex+1),
				Page:     pageNumber,
				Metadata: metadata,
			})
			imageBytes[nodeID] = data
		}
	}
	if len(nodes) == 0 {
		return nil, nil, nil
	}
	return nodes, imageBytes, nil
}

// sortedPDFImagePages preserves document order. GoPDF2 returns a map keyed by
// zero-based page index, whose randomized iteration order would otherwise make
// image-node ordering and downstream indexing nondeterministic.
func sortedPDFImagePages(pages map[int][]gopdf2.ExtractedImage) []int {
	pageIndexes := make([]int, 0, len(pages))
	for pageIndex := range pages {
		pageIndexes = append(pageIndexes, pageIndex)
	}
	sort.Ints(pageIndexes)
	return pageIndexes
}

// safeExtractPDFImages keeps malformed PDF image resources from unwinding the
// import worker. Image assets enrich a successfully parsed document, so an
// extraction panic is reported as a normal import error and can follow the
// caller's existing fallback/error policy.
func safeExtractPDFImages(pdfData []byte) (pages map[int][]gopdf2.ExtractedImage, err error) {
	return safeExtractPDFImagesWith(pdfData, gopdf2.ExtractImagesFromAllPages)
}

func safeExtractPDFImagesWith(pdfData []byte, extract func([]byte) (map[int][]gopdf2.ExtractedImage, error)) (pages map[int][]gopdf2.ExtractedImage, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			pages = nil
			err = fmt.Errorf("extract PDF images panicked: %v", recovered)
		}
	}()
	return extract(pdfData)
}

// --- helpers ---

func buildPageTextMap(textNodes []DocumentNode) map[int]string {
	result := make(map[int]string)
	for _, node := range textNodes {
		if node.Page > 0 {
			if existing := result[node.Page]; len(existing) < 300 {
				if existing != "" {
					result[node.Page] = existing + " " + truncateRunes(node.Text, 200)
				} else {
					result[node.Page] = truncateRunes(node.Text, 300)
				}
			}
		}
	}
	return result
}

func isImageFileExt(ext string) bool {
	ext = strings.ToLower(ext)
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".tiff", ".tif":
		return true
	}
	return false
}
