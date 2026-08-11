package knowledge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// maxKnowledgeDocumentImageBytes bounds the aggregate binary image payload
// retained from one Office document while assets are saved and described. It
// is deliberately smaller than the shared Office ZIP expansion ceiling: image
// extraction is optional enrichment and must not multiply a valid document's
// memory or disk use across hundreds of individually valid images.
const maxKnowledgeDocumentImageBytes int64 = 32 * 1024 * 1024

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
	if s == nil || s.imageAssets == nil {
		return nil
	}
	parsePath := filePath
	// This exported convenience path is also used outside the normal document
	// import transaction, where ParseDocumentNodes may not have run first. Do
	// not allow it to become a second way to open a malformed or encrypted
	// Office container merely because rich OfficeRead content is disabled. PDF
	// keeps its existing extraction path and is intentionally outside this
	// Office-specific preflight. PDF inputs may already be owned snapshots from
	// the import pipeline, so callers that need one consistent multi-stage PDF
	// parse pass that private pathname directly.
	if isOfficeReadImageKind(kind) {
		// The image parsers reopen a pathname after preflight. Use the same
		// private snapshot boundary as text extraction so a replacement cannot
		// turn an accepted container into different image bytes or a malicious
		// payload before ExtractDOCXImages/ExtractPPTXImages/ExtractDOCImages
		// gets to open it.
		snapshot, cleanup, err := agent.SnapshotOfficeReadInput(filePath, kind)
		if err != nil {
			logKnowledgeImageEvent("preflight_rejected", kind, 0)
			return nil
		}
		defer cleanup()
		parsePath = snapshot
	}

	// Extract raw image data from the document
	imageNodes, imageBytes, err := safeExtractDocumentImages(kind, func() ([]DocumentNode, map[string][]byte, error) {
		switch kind {
		case SourceKindDOCX:
			return ExtractDOCXImages(source, parsePath, textNodes)
		case SourceKindPPTX:
			return ExtractPPTXImages(source, parsePath, textNodes)
		case SourceKindPDF:
			return ExtractPDFImages(source, parsePath, textNodes)
		case SourceKindDOC:
			return ExtractDOCImages(source, parsePath, textNodes)
		default:
			return nil, nil, nil
		}
	})

	if err != nil {
		// Errors from a document parser can include source paths or embedded
		// metadata. Keep routine import telemetry structural and path-free.
		logKnowledgeImageEvent("extraction_failed", kind, 0)
		return nil
	}
	if len(imageNodes) == 0 {
		return nil
	}

	// Parallel save + describe when a document embeds multiple images.
	// Order of returned nodes matches input order for stable indexing.
	processedNodes := s.processExtractedImagesParallel(ctx, source, imageNodes, imageBytes)

	if len(processedNodes) > 0 {
		logKnowledgeImageEvent("extracted", kind, len(processedNodes))
	}
	return processedNodes
}

// safeExtractDocumentImages keeps optional embedded-image readers from
// unwinding a knowledge import worker. Some legacy Office and image libraries
// are third-party parsers, so a panic is treated exactly like extraction
// failure: no partial node or byte payload escapes to the asset pipeline.
func safeExtractDocumentImages(kind string, extract func() ([]DocumentNode, map[string][]byte, error)) (nodes []DocumentNode, imageBytes map[string][]byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			nodes = nil
			imageBytes = nil
			err = fmt.Errorf("extract %s images panicked: %v", kind, recovered)
		}
	}()
	return extract()
}

// ExtractAndProcessOfficeDocumentImages adds OfficeRead's opt-in image output
// for Office inputs while preserving existing DOCX/PPTX/PDF image extraction
// and all asset lifecycle rules. Image bytes never enter text nodes, chat
// injection, logs, or metadata JSON.
func (s *SQLiteStore) ExtractAndProcessOfficeDocumentImages(
	ctx context.Context,
	source Source,
	filePath string,
	kind string,
	textNodes []DocumentNode,
) []DocumentNode {
	if s == nil || s.imageAssets == nil {
		return nil
	}
	if !isOfficeReadImageKind(kind) {
		return s.ExtractAndProcessDocumentImages(ctx, source, filePath, kind, textNodes)
	}
	content, enabled, err := agent.ExtractOfficeReadRichContent(filePath)
	if enabled && err == nil {
		if !officeReadRichContentMatchesKind(content, kind) {
			// The caller's source kind controls asset lifecycle metadata. A rich
			// payload extracted for another format must not be persisted under it
			// or retried through a legacy parser.
			return nil
		}
		return s.extractAndProcessOfficeDocumentImagesFromRichContent(ctx, source, filePath, kind, textNodes, content, true)
	}
	if enabled && (agent.IsOfficeReadRichContentBlocked(err) || errors.Is(err, agent.ErrOfficeReadOutputTooLarge)) {
		// The rich-content boundary rejected this container as unsafe/encrypted
		// or reliably mislabelled, or it hit an adapter output limit. Do not let
		// this convenience entry point reopen it through a legacy image extractor.
		return nil
	}
	return s.ExtractAndProcessDocumentImages(ctx, source, filePath, kind, textNodes)
}

// extractAndProcessOfficeDocumentImagesFromRichContent consumes the result
// already obtained while parsing an explicitly enabled Office document. It
// avoids a second container extraction while preserving the legacy image path
// whenever rich content is disabled, fails, or yields no usable images.
func (s *SQLiteStore) extractAndProcessOfficeDocumentImagesFromRichContent(
	ctx context.Context,
	source Source,
	filePath string,
	kind string,
	textNodes []DocumentNode,
	content agent.OfficeReadRichContent,
	richContentAvailable bool,
) []DocumentNode {
	if isOfficeReadImageKind(kind) && richContentAvailable && !officeReadRichContentMatchesKind(content, kind) {
		// A caller may be holding a result from the same import transaction. Do
		// not let a stale or mismatched kind turn it into assets for a different
		// Office type, and do not reopen the container via the legacy path.
		return nil
	}
	if isOfficeReadImageKind(kind) && richContentAvailable {
		if nodes := officeReadImagesFromRichContentForImport(ctx, s, source, kind, textNodes, content); len(nodes) > 0 {
			return nodes
		}
		// Parsing captured an enabled OfficeRead rich-policy snapshot, but the
		// resulting payload contained no usable images (or image storage rejected
		// them). Never reread the live configuration here: a GUI rollout toggle
		// racing an import must not switch this one document to a legacy image
		// parser after its text nodes have already been selected.
		return nil
	}
	if isOfficeReadImageKind(kind) && agent.OfficeReadRichContentEnabledForFormat(kind) {
		// No parse-time rich snapshot reached this helper (for example a caller
		// outside the normal import parser). Capture the live policy once and
		// retain the existing fail-closed behavior: an explicitly enabled rich
		// attempt must not reopen the same Office container through legacy image
		// parsing merely because it returned no usable payload.
		return nil
	}
	return s.ExtractAndProcessDocumentImages(ctx, source, filePath, kind, textNodes)
}

// extractAndProcessDocumentImagesUsingRichOfficeContent is the import-pipeline
// entry point. Rich Office content applies only to Office containers; every
// other format, including PDF, keeps its native embedded-image extraction.
func (s *SQLiteStore) extractAndProcessDocumentImagesUsingRichOfficeContent(
	ctx context.Context,
	source Source,
	filePath string,
	kind string,
	textNodes []DocumentNode,
	content agent.OfficeReadRichContent,
	richContentAvailable bool,
) []DocumentNode {
	if isOfficeReadImageKind(kind) {
		return s.extractAndProcessOfficeDocumentImagesFromRichContent(ctx, source, filePath, kind, textNodes, content, richContentAvailable)
	}
	return s.ExtractAndProcessDocumentImages(ctx, source, filePath, kind, textNodes)
}

func isOfficeReadImageKind(kind string) bool {
	switch kind {
	case SourceKindDOC, SourceKindXLS, SourceKindPPT, SourceKindDOCX, SourceKindXLSX, SourceKindPPTX:
		return true
	default:
		return false
	}
}

// processExtractedImagesParallel saves assets and generates descriptions using
// the store imageDescSem (vision/OCR concurrency). Safe for multi-image DOCX/PPTX/PDF.
func (s *SQLiteStore) processExtractedImagesParallel(
	ctx context.Context,
	source Source,
	imageNodes []DocumentNode,
	imageBytes map[string][]byte,
) []DocumentNode {
	imageNodes = limitKnowledgeDocumentImagePayloads(imageNodes, imageBytes)
	if len(imageNodes) == 0 {
		return nil
	}
	// Single image: keep sequential path (no goroutine overhead).
	if len(imageNodes) == 1 {
		if node, ok := s.processOneExtractedImage(ctx, source, imageNodes[0], imageBytes); ok {
			return []DocumentNode{node}
		}
		return nil
	}

	out := make([]DocumentNode, len(imageNodes))
	okFlags := make([]bool, len(imageNodes))
	var wg sync.WaitGroup
	// Bound fan-out: description semaphore already limits vision/OCR; still cap
	// goroutines for asset save/thumbnail work.
	workers := importParallelWorkers(len(imageNodes))
	if workers > 4 {
		workers = 4
	}
	jobs := make(chan int, len(imageNodes))
	for i := range imageNodes {
		jobs <- i
	}
	close(jobs)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if node, ok := s.processOneExtractedImage(ctx, source, imageNodes[i], imageBytes); ok {
					out[i] = node
					okFlags[i] = true
				}
			}
		}()
	}
	wg.Wait()

	processed := make([]DocumentNode, 0, len(imageNodes))
	for i, ok := range okFlags {
		if ok {
			processed = append(processed, out[i])
		}
	}
	if ctx.Err() != nil && len(processed) > 0 {
		logKnowledgeImageEvent("context_cancelled", "", len(processed))
	}
	return processed
}

// limitKnowledgeDocumentImagePayloads is the final common retention boundary
// for native Office/PDF extractors and OfficeRead rich output. It protects
// callers that supply pre-extracted payloads and releases skipped byte slices
// before concurrent asset work begins. Individual assets retain their own
// stricter managed-store validation.
func limitKnowledgeDocumentImagePayloads(imageNodes []DocumentNode, imageBytes map[string][]byte) []DocumentNode {
	if len(imageNodes) == 0 || len(imageBytes) == 0 {
		return nil
	}
	kept := make([]DocumentNode, 0, len(imageNodes))
	keptKeys := make(map[string]struct{}, len(imageNodes))
	var total int64
	for _, node := range imageNodes {
		key := node.Metadata["_image_bytes_key"]
		data, ok := imageBytes[key]
		if !ok || len(data) == 0 {
			continue
		}
		if _, alreadyKept := keptKeys[key]; alreadyKept {
			// The bytes key identifies one managed asset. Do not schedule two
			// concurrent saves of that same asset, even if a malformed extractor
			// supplied duplicate nodes for it.
			continue
		}
		size := int64(len(data))
		if size > MaxKnowledgeImageAssetBytes || size > maxKnowledgeDocumentImageBytes-total {
			delete(imageBytes, key)
			continue
		}
		total += size
		keptKeys[key] = struct{}{}
		kept = append(kept, node)
	}
	for key := range imageBytes {
		if _, found := keptKeys[key]; !found {
			delete(imageBytes, key)
		}
	}
	return kept
}

func (s *SQLiteStore) processOneExtractedImage(
	ctx context.Context,
	source Source,
	node DocumentNode,
	imageBytes map[string][]byte,
) (result DocumentNode, ok bool) {
	// A saved asset is not yet committed to the knowledge base. If a decoder,
	// thumbnail generator, or describer fails after save, remove the exact
	// provisional asset so an optional enrichment failure cannot accumulate
	// orphaned files. The random bytes key makes this an exact asset target.
	assetID := ""
	assetSaved := false
	defer func() {
		if recovered := recover(); recovered != nil {
			logKnowledgeImageEvent("processing_panicked", "", 0)
			result = DocumentNode{}
			ok = false
		}
		if !ok && assetSaved {
			if err := s.imageAssets.DeleteAssets(assetID); err != nil {
				logKnowledgeImageEvent("asset_cleanup_failed", "", 0)
			}
		}
	}()
	select {
	case <-ctx.Done():
		return DocumentNode{}, false
	default:
	}

	bytesKey := node.Metadata["_image_bytes_key"]
	data, ok := imageBytes[bytesKey]
	if !ok || len(data) == 0 {
		return DocumentNode{}, false
	}

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
		assetID = source.ID + "_" + bytesKey
		asset, err := s.imageAssets.SaveImageFromBytes(assetID, data, ext)
		if err != nil {
			logKnowledgeImageEvent("asset_save_failed", "", 0)
			return DocumentNode{}, false
		}
		assetSaved = true
		node.Metadata[MetaImageAssetPath] = asset.OriginalPath
		node.Metadata[MetaImageAssetID] = assetID
		cleanImageNodeMetadata(&node)
		return node, true
	}

	assetID = source.ID + "_" + bytesKey
	asset, err := s.imageAssets.SaveImageFromBytes(assetID, data, ext)
	if err != nil {
		logKnowledgeImageEvent("asset_save_failed", "", 0)
		return DocumentNode{}, false
	}
	assetSaved = true

	node.Metadata[MetaImageAssetPath] = asset.OriginalPath
	node.Metadata[MetaImageAssetID] = assetID
	if asset.Width > 0 {
		node.Metadata[MetaImageWidth] = itoa(asset.Width)
		node.Metadata[MetaImageHeight] = itoa(asset.Height)
	}

	if s.imageDescriber != nil {
		if s.imageDescSem != nil {
			select {
			case s.imageDescSem <- struct{}{}:
			case <-ctx.Done():
				return DocumentNode{}, false
			}
		}
		hints := BuildImageHintsFromNode(node, source)
		desc, descErr := s.imageDescriber.Describe(ctx, asset.OriginalPath, hints)
		if s.imageDescSem != nil {
			<-s.imageDescSem
		}
		if descErr != nil {
			logKnowledgeImageEvent("describe_failed", "", 0)
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

	if node.Text == "" {
		hints := BuildImageHintsFromNode(node, source)
		node.Text = FormatImageNodeText(ImageDescription{
			Title:       inferImageTitle(hints),
			Description: inferImageDescription(hints, ""),
		})
	}

	cleanImageNodeMetadata(&node)
	return node, true
}

// ProcessStandaloneImage processes a standalone image file during directory import.
// Creates a Source + DocumentNode with description.
// Returns the nodes to be inserted (may be nil if processing fails).
func (s *SQLiteStore) ProcessStandaloneImage(
	ctx context.Context,
	source Source,
	filePath string,
	refs []ImageReference,
) (nodes []DocumentNode) {
	if s == nil || s.imageAssets == nil {
		return nil
	}
	// A standalone asset is provisionally written before the node that owns it
	// can be inserted. Keep the lifecycle fail-closed just like embedded Office
	// images: cancellation or a third-party image/description panic must not
	// leave an unreferenced asset directory behind.
	assetSaved := false
	defer func() {
		if recovered := recover(); recovered != nil {
			logKnowledgeImageEvent("standalone_processing_panicked", "", 0)
			nodes = nil
		}
		if len(nodes) == 0 && assetSaved {
			if err := s.imageAssets.DeleteAssets(source.ID); err != nil {
				logKnowledgeImageEvent("standalone_asset_cleanup_failed", "", 0)
			}
		}
	}()

	// Save image asset
	asset, err := s.imageAssets.SaveImageFromPath(source.ID, filePath)
	if err != nil {
		logKnowledgeImageEvent("standalone_asset_save_failed", "", 0)
		return nil
	}
	assetSaved = true

	// Build hints from references and file info
	hints := ImageHints{
		FileName:    filepath.Base(filePath),
		SourceTitle: source.Title,
		OwnerID:     source.OwnerID,
		TenantID:    source.TenantID,
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
			logKnowledgeImageEvent("standalone_describe_failed", "", 0)
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
		MetaImageAssetID: source.ID,
		MetaImageFormat:  asset.Format,
		"relative_path":  source.RelativePath,
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
	if node == nil {
		return
	}
	node.Metadata = sanitizeDocumentNodeMetadata(node.Metadata)
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
