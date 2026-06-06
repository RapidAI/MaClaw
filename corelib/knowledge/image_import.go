package knowledge

import (
	"context"
	"log"
	"path/filepath"
	"strconv"
)

// ExtractAndProcessDocumentImages extracts embedded images from a parsed document,
// saves them as assets, generates descriptions, and returns image DocumentNodes
// ready to be inserted alongside text nodes.
//
// This function is called from the import pipeline after ParseDocumentNodes succeeds.
// It handles DOCX, PPTX, and PDF formats. For other formats it returns nil (no images).
//
// If imageAssets or imageDescriber are nil, image extraction is skipped gracefully.
func (s *SQLiteStore) ExtractAndProcessDocumentImages(
	ctx context.Context,
	source Source,
	filePath string,
	kind string,
	textNodes []DocumentNode,
) []DocumentNode {
	if s.imageAssets == nil {
		return nil
	}

	// Extract raw image data from the document
	var imageNodes []DocumentNode
	var imageBytes map[string][]byte
	var err error

	switch kind {
	case SourceKindDOCX:
		imageNodes, imageBytes, err = ExtractDOCXImages(source, filePath, textNodes)
	case SourceKindPPTX:
		imageNodes, imageBytes, err = ExtractPPTXImages(source, filePath, textNodes)
	case SourceKindPDF:
		imageNodes, imageBytes, err = ExtractPDFImages(source, filePath, textNodes)
	case SourceKindDOC:
		imageNodes, imageBytes, err = ExtractDOCImages(source, filePath, textNodes)
	default:
		return nil
	}

	if err != nil {
		log.Printf("[knowledge-image] extraction failed for %s (%s): %v", filepath.Base(filePath), kind, err)
		return nil
	}
	if len(imageNodes) == 0 {
		return nil
	}

	// Process each extracted image: save asset + generate description
	var processedNodes []DocumentNode
	for i := range imageNodes {
		// Check context cancellation between images.
		select {
		case <-ctx.Done():
			log.Printf("[knowledge-image] context cancelled, processed %d/%d images", len(processedNodes), len(imageNodes))
			return processedNodes
		default:
		}

		node := &imageNodes[i]
		bytesKey := node.Metadata["_image_bytes_key"]
		data, ok := imageBytes[bytesKey]
		if !ok || len(data) == 0 {
			continue
		}

		// Determine file extension from format metadata
		format := node.Metadata[MetaImageFormat]
		ext := "." + format
		if ext == "." {
			ext = ".png"
		}

		// Skip vector images for asset processing (keep node for metadata)
		if node.Metadata[MetaImageIsVector] == "true" {
			node.Text = "矢量图片 (" + format + ")"
			if node.Title == "" {
				node.Title = "矢量图"
			}
			// Still save the raw file but don't generate thumbnails/OCR
			asset, err := s.imageAssets.SaveImageFromBytes(source.ID+"_"+bytesKey, data, ext)
			if err == nil {
				node.Metadata[MetaImageAssetPath] = asset.OriginalPath
			}
			cleanImageNodeMetadata(node)
			processedNodes = append(processedNodes, *node)
			continue
		}

		// Save image asset (original + thumbnails)
		assetID := source.ID + "_" + bytesKey
		asset, err := s.imageAssets.SaveImageFromBytes(assetID, data, ext)
		if err != nil {
			log.Printf("[knowledge-image] save asset failed: %v", err)
			continue
		}

		// Update metadata with asset info
		node.Metadata[MetaImageAssetPath] = asset.OriginalPath
		if asset.Width > 0 {
			node.Metadata[MetaImageWidth] = itoa(asset.Width)
			node.Metadata[MetaImageHeight] = itoa(asset.Height)
		}

		// Generate description (Vision LLM or OCR + context)
		if s.imageDescriber != nil {
			// Acquire semaphore for concurrency control (ctx-aware).
			if s.imageDescSem != nil {
				select {
				case s.imageDescSem <- struct{}{}:
				case <-ctx.Done():
					return processedNodes
				}
			}
			hints := BuildImageHintsFromNode(*node, source)
			desc, err := s.imageDescriber.Describe(ctx, asset.OriginalPath, hints)
			if s.imageDescSem != nil {
				<-s.imageDescSem
			}
			if err != nil {
				log.Printf("[knowledge-image] describe failed for %s: %v", filepath.Base(asset.OriginalPath), err)
			} else {
				node.Text = FormatImageNodeText(desc)
				if desc.Title != "" && node.Title == "" {
					node.Title = desc.Title
				}
				if desc.OCRText != "" {
					node.Metadata[MetaImageOCRText] = desc.OCRText
				}
			}
		}

		// If no description was generated, use fallback
		if node.Text == "" {
			hints := BuildImageHintsFromNode(*node, source)
			node.Text = FormatImageNodeText(ImageDescription{
				Title:       inferImageTitle(hints),
				Description: inferImageDescription(hints, ""),
			})
		}

		cleanImageNodeMetadata(node)
		processedNodes = append(processedNodes, *node)
	}

	if len(processedNodes) > 0 {
		log.Printf("[knowledge-image] extracted %d images from %s (%s)", len(processedNodes), filepath.Base(filePath), kind)
	}
	return processedNodes
}

// ProcessStandaloneImage processes a standalone image file during directory import.
// Creates a Source + DocumentNode with description.
// Returns the nodes to be inserted (may be nil if processing fails).
func (s *SQLiteStore) ProcessStandaloneImage(
	ctx context.Context,
	source Source,
	filePath string,
	refs []ImageReference,
) []DocumentNode {
	if s.imageAssets == nil {
		return nil
	}

	// Save image asset
	asset, err := s.imageAssets.SaveImageFromPath(source.ID, filePath)
	if err != nil {
		log.Printf("[knowledge-image] save standalone image failed: %v", err)
		return nil
	}

	// Build hints from references and file info
	hints := ImageHints{
		FileName:    filepath.Base(filePath),
		SourceTitle: source.Title,
	}
	if len(refs) > 0 {
		best := refs[0]
		hints.ContextBefore = best.ContextBefore
		hints.ContextAfter = best.ContextAfter
		hints.AltText = best.AltText
		hints.ParentTitle = best.SectionTitle
	}

	// Generate description (respects semaphore for concurrency control)
	var desc ImageDescription
	if s.imageDescriber != nil {
		if s.imageDescSem != nil {
			select {
			case s.imageDescSem <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
		}
		desc, err = s.imageDescriber.Describe(ctx, asset.OriginalPath, hints)
		if s.imageDescSem != nil {
			<-s.imageDescSem
		}
		if err != nil {
			log.Printf("[knowledge-image] describe standalone image failed: %v", err)
		}
	}
	if desc.Title == "" {
		desc.Title = inferImageTitle(hints)
	}
	if desc.Description == "" {
		desc.Description = inferImageDescription(hints, desc.OCRText)
	}

	// Build metadata
	metadata := map[string]string{
		MetaImageAssetPath: asset.OriginalPath,
		MetaImageFormat:    asset.Format,
		"relative_path":   source.RelativePath,
	}
	if asset.Width > 0 {
		metadata[MetaImageWidth] = itoa(asset.Width)
		metadata[MetaImageHeight] = itoa(asset.Height)
	}
	if desc.OCRText != "" {
		metadata[MetaImageOCRText] = desc.OCRText
	}
	if hints.AltText != "" {
		metadata[MetaImageAltText] = hints.AltText
	}
	// Store reference source info
	if len(refs) > 0 {
		metadata[MetaImageRefSource] = refs[0].SourceID
		metadata[MetaImageRefNode] = refs[0].NodeID
	}

	node := DocumentNode{
		ID:         NewID("kdn"),
		SourceID:   source.ID,
		Type:       NodeTypeImage,
		Title:      desc.Title,
		Text:       FormatImageNodeText(desc),
		Metadata:   metadata,
		TokenCount: estimateTokens(FormatImageNodeText(desc)),
	}

	return []DocumentNode{node}
}

// --- helpers ---

// cleanImageNodeMetadata removes internal temp keys that shouldn't be persisted.
func cleanImageNodeMetadata(node *DocumentNode) {
	delete(node.Metadata, "_image_bytes_key")
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
