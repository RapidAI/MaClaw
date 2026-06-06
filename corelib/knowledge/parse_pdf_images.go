package knowledge

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ExtractPDFImages extracts embedded images from a PDF file.
//
// Strategy: Use pdfcpu CLI if available, otherwise fall back to a no-op.
// The pdfcpu approach is preferred because it handles all PDF image encodings
// (DCT/JPEG, Flate/PNG, JBIG2, CCITT, etc.) without requiring CGO.
//
// Each returned node has:
//   - Type: NodeTypeImage
//   - Page: page number (if determinable)
//   - Metadata with format and context
//   - Metadata["_image_bytes_key"]: key into the returned bytes map
//
// If pdfcpu is not available, returns nil (no images extracted).
// This is acceptable because PDF image extraction is a "best effort" enhancement.
func ExtractPDFImages(source Source, filePath string, textNodes []DocumentNode) ([]DocumentNode, map[string][]byte, error) {
	// Try pdfcpu CLI approach first (works if user has pdfcpu installed or
	// we bundle it). Falls back gracefully if not available.
	nodes, imageBytes, err := extractPDFImagesWithCLI(source, filePath, textNodes)
	if err == nil {
		return nodes, imageBytes, nil
	}

	// Fallback: try Go-native extraction (limited to simpler encodings).
	nodes, imageBytes, err = extractPDFImagesNative(source, filePath, textNodes)
	if err == nil {
		return nodes, imageBytes, nil
	}

	// No extraction method available — this is not a fatal error.
	log.Printf("[knowledge-pdf] image extraction not available for %s: %v", filepath.Base(filePath), err)
	return nil, nil, nil
}

// extractPDFImagesWithCLI uses pdfcpu (if installed) to extract images.
// pdfcpu writes extracted images to a temp directory.
func extractPDFImagesWithCLI(source Source, filePath string, textNodes []DocumentNode) ([]DocumentNode, map[string][]byte, error) {
	// Check if pdfcpu is available
	pdfcpuPath, err := exec.LookPath("pdfcpu")
	if err != nil {
		return nil, nil, fmt.Errorf("pdfcpu not found: %w", err)
	}

	// Create temp directory for extracted images
	tmpDir, err := os.MkdirTemp("", "maclaw-pdf-images-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Run pdfcpu extract images
	cmd := exec.Command(pdfcpuPath, "extract", "-mode", "image", filePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("pdfcpu extract: %w (output: %s)", err, truncateBytes(output, 200))
	}

	// Read extracted images from temp directory
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read temp dir: %w", err)
	}

	var nodes []DocumentNode
	imageBytes := make(map[string][]byte)

	// Build page text context map
	pageTextMap := buildPageTextMap(textNodes)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if !isImageFileExt(ext) {
			continue
		}

		imgPath := filepath.Join(tmpDir, name)
		data, err := os.ReadFile(imgPath)
		if err != nil {
			continue
		}
		// Skip tiny images (decorative icons)
		if len(data) < 500 {
			continue
		}

		// Try to determine page number from filename (pdfcpu format: "pageN_imageM.ext")
		pageNum := parsePageFromFilename(name)
		context := pageTextMap[pageNum]

		nodeID := NewID("kdn")
		metadata := map[string]string{
			MetaImageFormat:    normalizeFormatName(ext),
			"_image_bytes_key": nodeID,
		}
		if context != "" {
			metadata["context_before"] = context
		}
		if IsVectorImageExt(ext) {
			metadata[MetaImageIsVector] = "true"
		}

		nodes = append(nodes, DocumentNode{
			ID:       nodeID,
			SourceID: source.ID,
			Type:     NodeTypeImage,
			Page:     pageNum,
			Metadata: metadata,
		})
		imageBytes[nodeID] = data
	}

	if len(nodes) == 0 {
		return nil, nil, nil
	}
	return nodes, imageBytes, nil
}

// extractPDFImagesNative is a placeholder for Go-native PDF image extraction.
// TODO: Implement using pdfcpu Go library (github.com/pdfcpu/pdfcpu/pkg/api)
// when added as a dependency. This avoids the CLI requirement.
func extractPDFImagesNative(source Source, filePath string, textNodes []DocumentNode) ([]DocumentNode, map[string][]byte, error) {
	return nil, nil, fmt.Errorf("native PDF image extraction not yet implemented")
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

// parsePageFromFilename tries to extract page number from pdfcpu output filenames.
// Common formats: "page_1_image_1.png", "1_image_1.png", etc.
func parsePageFromFilename(name string) int {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if (p == "page" || p == "p") && i+1 < len(parts) {
			if n, err := strconv.Atoi(parts[i+1]); err == nil {
				return n
			}
		}
	}
	// Try first numeric part
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n < 10000 {
			return n
		}
	}
	return 0
}

func isImageFileExt(ext string) bool {
	ext = strings.ToLower(ext)
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".tiff", ".tif":
		return true
	}
	return false
}
