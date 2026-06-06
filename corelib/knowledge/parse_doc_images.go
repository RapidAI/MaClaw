package knowledge

import (
	"fmt"
	"log"

	"github.com/shakinm/xlsReader/doc"
)

// ExtractDOCImages extracts embedded images from a .doc (Word 97-2003) file.
// Uses the LegacyOfficeReader's GetImages() method which parses the OLE2
// Pictures stream and returns decoded image data.
//
// .doc format has limited position metadata — images are associated at
// document level (not precise paragraph positioning like DOCX).
//
// Each returned node has:
//   - Type: NodeTypeImage
//   - Metadata with format info
//   - Metadata["_image_bytes_key"]: key into the returned bytes map
func ExtractDOCImages(source Source, filePath string, textNodes []DocumentNode) ([]DocumentNode, map[string][]byte, error) {
	document, err := doc.OpenFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open .doc for image extraction: %w", err)
	}

	images := document.GetImages()
	if len(images) == 0 {
		return nil, nil, nil
	}

	var nodes []DocumentNode
	imageBytes := make(map[string][]byte)

	for i, img := range images {
		if len(img.Data) < 500 {
			// Skip tiny images (likely decorative icons)
			continue
		}

		ext := img.Extension()
		if ext == "" {
			ext = ".png" // fallback
		}

		// Check if it's a vector format
		isVector := IsVectorImageExt(ext)

		// Build context from document text nodes (document-level association)
		// For .doc we can't determine exact paragraph position, so use
		// a document summary as context.
		contextBefore := ""
		if len(textNodes) > 0 {
			// Use first text node as general context
			contextBefore = truncateRunes(textNodes[0].Text, 200)
		}
		if i < len(textNodes) {
			// Try to correlate by rough index (best effort for .doc)
			contextBefore = truncateRunes(textNodes[i].Text, 200)
		}

		nodeID := NewID("kdn")
		metadata := map[string]string{
			MetaImageFormat:    normalizeFormatName(ext),
			"_image_bytes_key": nodeID,
			"context_before":   contextBefore,
		}
		if isVector {
			metadata[MetaImageIsVector] = "true"
		}

		nodes = append(nodes, DocumentNode{
			ID:       nodeID,
			SourceID: source.ID,
			Type:     NodeTypeImage,
			Title:    fmt.Sprintf("图片 %d", i+1),
			Metadata: metadata,
		})
		imageBytes[nodeID] = img.Data
	}

	if len(nodes) > 0 {
		log.Printf("[knowledge-image] extracted %d images from .doc file %s", len(nodes), source.Title)
	}
	return nodes, imageBytes, nil
}
