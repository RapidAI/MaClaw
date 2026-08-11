package knowledge

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// ExtractPPTXImages extracts embedded images from a PPTX file and returns
// DocumentNodes of type "image" with slide-level position context.
//
// Each returned node has:
//   - Type: NodeTypeImage
//   - Page: slide number (1-based)
//   - Metadata with format, alt text, and context
//   - Metadata["_image_bytes_key"]: key into the returned bytes map
func ExtractPPTXImages(source Source, filePath string, textNodes []DocumentNode) ([]DocumentNode, map[string][]byte, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open pptx zip: %w", err)
	}
	defer r.Close()

	// Step 1: Parse each slide's relationships and find image references.
	// Relationship and slide XML use the bounded shared ZIP reader, so their
	// declared expansion cannot become an image-import allocation.
	slideRels := parsePPTXSlideRels(r)                // slide path → (rId → media path)
	slideImages := parsePPTXSlideImages(r, slideRels) // slide number → []imageRef

	if len(slideImages) == 0 {
		return nil, nil, nil
	}

	// Step 2: Build an index of media entries without reading every embedded
	// payload. Only slide-referenced images may enter the bounded retention
	// budget below.
	mediaFiles := make(map[string]*zip.File)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "ppt/media/") && !f.FileInfo().IsDir() {
			mediaFiles[f.Name] = f
		}
	}

	// Step 3: Build slide text context map (from textNodes)
	slideTextMap := buildSlideTextMap(textNodes)

	// Step 4: Create image nodes. The per-document retention ceiling applies
	// before image bytes are read so unused or excess media never accumulates
	// in this extractor.
	var nodes []DocumentNode
	imageBytes := make(map[string][]byte)
	var retainedBytes int64
	mediaData := make(map[string][]byte)
	mediaReadFailed := make(map[string]bool)

	for slideNum, refs := range slideImages {
		slideContext := slideTextMap[slideNum]
		for _, ref := range refs {
			data, ok := mediaData[ref.mediaPath]
			if !ok {
				media, exists := mediaFiles[ref.mediaPath]
				if !exists || mediaReadFailed[ref.mediaPath] || media.UncompressedSize64 < 500 || media.UncompressedSize64 > uint64(MaxKnowledgeImageAssetBytes) || media.UncompressedSize64 > uint64(maxKnowledgeDocumentImageBytes-retainedBytes) {
					continue
				}
				var readErr error
				data, readErr = readZipFileAtMost(media, MaxKnowledgeImageAssetBytes)
				if readErr != nil || len(data) > int(maxKnowledgeDocumentImageBytes-retainedBytes) {
					mediaReadFailed[ref.mediaPath] = true
					continue
				}
				retainedBytes += int64(len(data))
				mediaData[ref.mediaPath] = data
			}
			if len(data) == 0 {
				continue
			}
			// Skip tiny images
			if len(data) < 500 {
				continue
			}

			ext := filepath.Ext(ref.mediaPath)
			nodeID := NewID("kdn")

			metadata := map[string]string{
				MetaImageFormat:    normalizeFormatName(ext),
				MetaImageAltText:   ref.altText,
				"_image_bytes_key": nodeID,
				"context_before":   slideContext,
			}
			if IsVectorImageExt(ext) {
				metadata[MetaImageIsVector] = "true"
			}

			nodes = append(nodes, DocumentNode{
				ID:       nodeID,
				SourceID: source.ID,
				Type:     NodeTypeImage,
				Title:    ref.altText,
				Page:     slideNum,
				Metadata: metadata,
			})
			imageBytes[nodeID] = data
		}
	}

	return nodes, imageBytes, nil
}

// --- internal types ---

type pptxImageRef struct {
	mediaPath string
	altText   string
}

// --- relationship parsing ---

func parsePPTXSlideRels(r *zip.ReadCloser) map[string]map[string]string {
	// slide rels path → (rId → media path)
	// e.g. "ppt/slides/_rels/slide1.xml.rels" → {"rId2": "ppt/media/image1.png"}
	result := make(map[string]map[string]string)

	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, "ppt/slides/_rels/") || !strings.HasSuffix(f.Name, ".xml.rels") {
			continue
		}
		data, err := readZipFile(f)
		if err != nil {
			continue
		}

		relsMap := make(map[string]string)
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
			if strings.Contains(relType, "/image") || strings.HasPrefix(target, "../media/") {
				// Convert relative path to absolute within zip
				mediaPath := "ppt/media/" + filepath.Base(target)
				relsMap[id] = mediaPath
			}
		}

		// Map slide rels file to the slide XML path
		// "ppt/slides/_rels/slide1.xml.rels" → "ppt/slides/slide1.xml"
		slideName := strings.TrimSuffix(filepath.Base(f.Name), ".rels")
		slidePath := "ppt/slides/" + slideName
		result[slidePath] = relsMap
	}

	return result
}

func parsePPTXSlideImages(r *zip.ReadCloser, slideRels map[string]map[string]string) map[int][]pptxImageRef {
	result := make(map[int][]pptxImageRef)

	// Find and parse each slide XML
	var slideFiles []*zip.File
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slideFiles = append(slideFiles, f)
		}
	}

	// Sort by name to get correct slide order
	sort.Slice(slideFiles, func(i, j int) bool {
		return slideFiles[i].Name < slideFiles[j].Name
	})

	for slideIdx, f := range slideFiles {
		slideNum := slideIdx + 1
		rels := slideRels[f.Name]
		if len(rels) == 0 {
			continue
		}

		data, err := readZipFile(f)
		if err != nil {
			continue
		}

		refs := parseSlideImageRefs(data, rels)
		if len(refs) > 0 {
			result[slideNum] = refs
		}
	}

	return result
}

func parseSlideImageRefs(slideXML []byte, rels map[string]string) []pptxImageRef {
	decoder := xml.NewDecoder(bytes.NewReader(slideXML))
	var refs []pptxImageRef
	var currentAlt string

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "cNvPr": // <p:cNvPr name="..." descr="...">
			for _, attr := range se.Attr {
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
			for _, attr := range se.Attr {
				if attr.Name.Local == "embed" {
					embedID = attr.Value
				}
			}
			if embedID != "" {
				if mediaPath, ok := rels[embedID]; ok {
					refs = append(refs, pptxImageRef{
						mediaPath: mediaPath,
						altText:   currentAlt,
					})
				}
			}
			currentAlt = ""
		}
	}

	return refs
}

// buildSlideTextMap extracts slide text context from text nodes.
// Text nodes from PPTX parsing have format "--- Slide N ---" headers.
func buildSlideTextMap(textNodes []DocumentNode) map[int]string {
	result := make(map[int]string)
	for _, node := range textNodes {
		if node.Page > 0 {
			existing := result[node.Page]
			if existing == "" {
				result[node.Page] = truncateRunes(node.Text, 300)
			} else if len(existing) < 300 {
				result[node.Page] = existing + " " + truncateRunes(node.Text, 200)
			}
		}
	}
	// Also try parsing from "--- Slide N ---" format in Text
	for _, node := range textNodes {
		if node.Page == 0 && strings.HasPrefix(node.Text, "--- Slide ") {
			var num int
			if _, err := fmt.Sscanf(node.Text, "--- Slide %d ---", &num); err == nil && num > 0 {
				if _, exists := result[num]; !exists {
					result[num] = truncateRunes(node.Text, 300)
				}
			}
		}
	}
	return result
}
