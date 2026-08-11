package knowledge

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// Keep supporting OOXML metadata reasonably bounded even when these exported
// extractors are called outside the normal knowledge-import preflight. Image
// enrichment must not turn a small compressed XML part into an unbounded
// allocation.
const maxKnowledgeOfficeImageXMLBytes int64 = 8 * 1024 * 1024

var errKnowledgeOfficeImagePartTooLarge = errors.New("embedded Office image part exceeds limit")

// ExtractDOCXImages extracts embedded images from a DOCX file and returns
// DocumentNodes of type "image" with position context from surrounding text.
//
// Each returned node has:
//   - Type: NodeTypeImage
//   - Metadata[MetaImageFormat]: detected format from file extension
//   - Metadata[MetaImageAltText]: alt/descr from OOXML
//   - Metadata["_image_bytes_key"]: key into the returned bytes map
//
// The caller is responsible for saving the image bytes via ImageAssetManager.
func ExtractDOCXImages(source Source, filePath string, textNodes []DocumentNode) ([]DocumentNode, map[string][]byte, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open docx zip: %w", err)
	}
	defer r.Close()

	// Step 1: Build rId → media path mapping from word/_rels/document.xml.rels
	relsMap, err := parseDocxRels(r)
	if err != nil {
		return nil, nil, err
	}

	// Step 2: Parse document.xml to find image positions (paragraph index + alt text)
	var documentXML []byte
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			documentXML, err = readZipFileAtMost(f, maxKnowledgeOfficeImageXMLBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("read document.xml: %w", err)
			}
			break
		}
	}
	if len(documentXML) == 0 {
		return nil, nil, nil
	}

	imageRefs := parseDocxImagePositions(documentXML, relsMap)
	if len(imageRefs) == 0 {
		return nil, nil, nil
	}

	// Step 3: Index media entries without inflating them. Only referenced images
	// are read below; a document with an unused media directory must not consume
	// the image-import budget.
	mediaFiles := make(map[string]*zip.File)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "word/media/") && !f.FileInfo().IsDir() {
			mediaFiles[f.Name] = f
		}
	}

	// Step 4: Create image DocumentNodes with context. Do not retain more than
	// the shared per-document payload budget before handing the result to the
	// common asset pipeline.
	var nodes []DocumentNode
	imageBytes := make(map[string][]byte)
	var retainedBytes int64
	mediaData := make(map[string][]byte)
	mediaReadFailed := make(map[string]bool)

	for _, ref := range imageRefs {
		data, ok := mediaData[ref.mediaPath]
		if !ok {
			media, exists := mediaFiles[ref.mediaPath]
			if !exists || mediaReadFailed[ref.mediaPath] || media.UncompressedSize64 < 500 || media.UncompressedSize64 > uint64(MaxKnowledgeImageAssetBytes) || media.UncompressedSize64 > uint64(maxKnowledgeDocumentImageBytes-retainedBytes) {
				continue
			}
			data, err = readZipFileAtMost(media, MaxKnowledgeImageAssetBytes)
			if err != nil || len(data) > int(maxKnowledgeDocumentImageBytes-retainedBytes) {
				mediaReadFailed[ref.mediaPath] = true
				continue
			}
			retainedBytes += int64(len(data))
			mediaData[ref.mediaPath] = data
		}
		if len(data) == 0 {
			continue
		}

		// Skip tiny images (likely decorative icons)
		if len(data) < 500 {
			continue
		}

		ext := filepath.Ext(ref.mediaPath)
		if IsVectorImageExt(ext) {
			// Vector format — still record but mark as vector
			nodeID := NewID("kdn")
			nodes = append(nodes, DocumentNode{
				ID:       nodeID,
				SourceID: source.ID,
				Type:     NodeTypeImage,
				Title:    ref.altText,
				Metadata: map[string]string{
					MetaImageFormat:    strings.TrimPrefix(ext, "."),
					MetaImageIsVector:  "true",
					MetaImageAltText:   ref.altText,
					"_image_bytes_key": nodeID,
				},
			})
			imageBytes[nodeID] = data
			continue
		}

		// Determine context from surrounding text nodes
		contextBefore, contextAfter := getDocxImageContext(ref.paragraphIndex, textNodes)

		nodeID := NewID("kdn")
		nodes = append(nodes, DocumentNode{
			ID:       nodeID,
			SourceID: source.ID,
			Type:     NodeTypeImage,
			Title:    ref.altText,
			Metadata: map[string]string{
				MetaImageFormat:    normalizeFormatName(ext),
				MetaImageAltText:   ref.altText,
				"_image_bytes_key": nodeID,
				"context_before":   contextBefore,
				"context_after":    contextAfter,
			},
		})
		imageBytes[nodeID] = data
	}

	return nodes, imageBytes, nil
}

// --- internal types ---

type docxImageRef struct {
	mediaPath      string // e.g. "word/media/image1.png"
	altText        string // from <wp:docPr descr="..."> or name="..."
	paragraphIndex int    // 0-based index in paragraph sequence
}

// --- relationship parsing ---

func parseDocxRels(r *zip.ReadCloser) (map[string]string, error) {
	// rId → target path (e.g. "rId5" → "media/image1.png")
	relsMap := make(map[string]string)

	for _, f := range r.File {
		if f.Name != "word/_rels/document.xml.rels" {
			continue
		}
		data, err := readZipFileAtMost(f, maxKnowledgeOfficeImageXMLBytes)
		if err != nil {
			return nil, fmt.Errorf("read rels: %w", err)
		}

		decoder := xml.NewDecoder(bytes.NewReader(data))
		for {
			tok, err := decoder.Token()
			if err != nil {
				break
			}
			se, ok := tok.(xml.StartElement)
			if !ok || se.Name.Local != "Relationship" {
				continue
			}
			var id, target, relType string
			for _, attr := range se.Attr {
				switch attr.Name.Local {
				case "Id":
					id = attr.Value
				case "Target":
					target = attr.Value
				case "Type":
					relType = attr.Value
				}
			}
			// Only image relationships
			if strings.Contains(relType, "/image") || strings.HasPrefix(target, "media/") {
				relsMap[id] = "word/" + target
			}
		}
		break
	}

	return relsMap, nil
}

// --- document.xml image position parsing ---

func parseDocxImagePositions(data []byte, relsMap map[string]string) []docxImageRef {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var refs []docxImageRef
	paragraphIndex := -1
	var currentAlt string

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p": // <w:p> paragraph start
				paragraphIndex++
				currentAlt = "" // reset alt at paragraph boundary
			case "docPr": // <wp:docPr name="..." descr="...">
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "descr":
						currentAlt = attr.Value
					case "name":
						if currentAlt == "" {
							currentAlt = attr.Value
						}
					}
				}
			case "blip": // <a:blip r:embed="rIdN">
				var embedID string
				for _, attr := range t.Attr {
					if attr.Name.Local == "embed" {
						embedID = attr.Value
					}
				}
				if embedID != "" {
					if mediaPath, ok := relsMap[embedID]; ok {
						refs = append(refs, docxImageRef{
							mediaPath:      mediaPath,
							altText:        currentAlt,
							paragraphIndex: paragraphIndex,
						})
					}
				}
				currentAlt = "" // reset for next image in same paragraph
			}
		}
	}

	return refs
}

// --- context extraction ---

func getDocxImageContext(paragraphIndex int, textNodes []DocumentNode) (before, after string) {
	// Delegate to shared helper.
	return getTextNodeContext(paragraphIndex, textNodes, 200)
}

// --- helpers ---

func readZipFile(f *zip.File) ([]byte, error) {
	return readZipFileAtMost(f, maxKnowledgeOfficeImageXMLBytes)
}

func readZipFileAtMost(f *zip.File, maxBytes int64) ([]byte, error) {
	if f == nil || maxBytes < 0 || f.UncompressedSize64 > uint64(maxBytes) {
		return nil, errKnowledgeOfficeImagePartTooLarge
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errKnowledgeOfficeImagePartTooLarge
	}
	return data, nil
}

func normalizeFormatName(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	switch ext {
	case "jpg":
		return "jpeg"
	default:
		return ext
	}
}

// KindFromImageExt returns the image format kind string for a file extension.
// Used by external callers that classify files by extension.
func KindFromImageExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	switch ext {
	case ".png":
		return "png"
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".gif":
		return "gif"
	case ".bmp":
		return "bmp"
	case ".webp":
		return "webp"
	default:
		return strings.TrimPrefix(ext, ".")
	}
}

// imageNodeID is unused — kept as documentation of the convention.
// All image node ID generation uses NewID("kdn") directly.

// getTextNodeContext extracts context text from text nodes around a given index.
// Used by PPTX and PDF image extractors as well.
func getTextNodeContext(index int, textNodes []DocumentNode, maxRunes int) (before, after string) {
	if len(textNodes) == 0 {
		return "", ""
	}
	if index > 0 && index-1 < len(textNodes) {
		before = truncateRunes(textNodes[index-1].Text, maxRunes)
	}
	if index < len(textNodes) {
		after = truncateRunes(textNodes[index].Text, maxRunes)
	} else if index-1 < len(textNodes) {
		after = truncateRunes(textNodes[len(textNodes)-1].Text, maxRunes)
	}
	return before, after
}

// BuildImageHintsFromNode constructs ImageHints from a DocumentNode's metadata.
func BuildImageHintsFromNode(node DocumentNode, source Source) ImageHints {
	return ImageHints{
		FileName:      fmt.Sprintf("%s_image_%s", source.Title, node.ID),
		ContextBefore: node.Metadata["context_before"],
		ContextAfter:  node.Metadata["context_after"],
		AltText:       node.Metadata[MetaImageAltText],
		ParentTitle:   node.Title,
		PageNumber:    node.Page,
		SourceTitle:   source.Title,
		OwnerID:       source.OwnerID,
		TenantID:      source.TenantID,
	}
}
