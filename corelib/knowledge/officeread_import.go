package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// parseOfficeReadNodes deliberately converts only StructuredMarkdown into
// document nodes. The plain-text chat extraction continues to use agent's
// text-only API, and image bytes are handled only by the controlled asset
// pipeline below.
func parseOfficeReadNodes(source Source, filePath, kind string) ([]DocumentNode, error) {
	return parseOfficeReadNodesFromExtraction(source, filePath, kind, nil)
}

// officeReadRichExtraction keeps one controlled OfficeRead result alive only
// for the duration of a knowledge import. It lets structured Markdown and
// managed-image ingestion share the same extraction, without adding image
// bytes to a public knowledge API or retaining them after the import call.
type officeReadRichExtraction struct {
	loaded  bool
	content agent.OfficeReadRichContent
	enabled bool
	err     error
	config  *agent.OfficeReadConfig
}

func (e *officeReadRichExtraction) load(filePath string) {
	if e == nil || e.loaded {
		return
	}
	e.loaded = true
	if e.config != nil {
		e.content, e.enabled, e.err = agent.ExtractOfficeReadRichContentWithOfficeReadConfig(filePath, *e.config)
		return
	}
	e.content, e.enabled, e.err = agent.ExtractOfficeReadRichContent(filePath)
}

func parseOfficeReadNodesFromExtraction(source Source, filePath, kind string, extraction *officeReadRichExtraction) ([]DocumentNode, error) {
	if extraction == nil {
		extraction = &officeReadRichExtraction{}
	}
	extraction.load(filePath)
	if !extraction.enabled {
		return nil, errUnsupportedParser
	}
	if extraction.err != nil {
		return nil, extraction.err
	}
	content := extraction.content
	// The public preview seam accepts a caller-provided kind, while the rich
	// adapter selects its format from the actual file path/signature. Refuse a
	// disagreement before writing kind-specific node metadata; otherwise a
	// caller could persist DOCX Markdown as DOC (or equivalent).
	if !officeReadRichContentMatchesKind(content, kind) {
		return nil, agent.ErrOfficeReadFormatMismatch
	}
	markdown := normalizeKnowledgeText(content.Markdown)
	if markdown == "" {
		return nil, fmt.Errorf("OfficeRead has no structured Markdown")
	}
	nodes := parseMarkdownNodes(source, markdown)
	if len(nodes) > maxKnowledgeOfficeReadMarkdownNodes {
		// Structured Markdown is an optional enrichment. A heading storm must
		// fail closed rather than fan out into unbounded FTS/card work or leave a
		// partly indexed source. The adapter's rune limit alone cannot bound this
		// because a document can consist of very many tiny headings.
		return nil, agent.ErrOfficeReadOutputTooLarge
	}
	for i := range nodes {
		if nodes[i].Metadata == nil {
			nodes[i].Metadata = make(map[string]string)
		}
		nodes[i].Metadata["format"] = kind
		nodes[i].Metadata["extractor"] = "officeread_structured_markdown"
	}
	return nodes, nil
}

// maxKnowledgeOfficeReadMarkdownNodes caps the amount of persistence and
// downstream enrichment work one explicitly enabled rich Office document can
// create. It is intentionally generous for normal heading-rich reports, while
// keeping a tiny-heading output from consuming unbounded database, FTS, card,
// and embedding resources.
const maxKnowledgeOfficeReadMarkdownNodes = 10_000

func officeReadRichContentMatchesKind(content agent.OfficeReadRichContent, kind string) bool {
	format := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(content.Format)), ".")
	kind = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(kind)), ".")
	return format != "" && format == kind
}

// sanitizeOfficeReadKnowledgeError is used before a rich OfficeRead failure
// is persisted in sources/import items or sent through import progress. The
// adapter's stable errors are safe to retain; parser and filesystem errors
// are not, because they can include host paths or document-derived details.
func sanitizeOfficeReadKnowledgeError(err error) error {
	if err == nil {
		return err
	}
	for _, safe := range []error{
		agent.ErrOfficeReadUnsafeContainer,
		agent.ErrOfficeReadEncryptedContainer,
		agent.ErrOfficeReadTimedOut,
		agent.ErrOfficeReadFormatMismatch,
		agent.ErrOfficeReadInputTooLarge,
		agent.ErrOfficeReadOutputTooLarge,
		agent.ErrOfficeReadSourceChanged,
		agent.ErrOfficeReadExtractionFailed,
	} {
		if errors.Is(err, safe) {
			return safe
		}
	}
	return agent.ErrOfficeReadExtractionFailed
}

// sanitizeKnowledgeParseError is the persistence boundary for parsers whose
// input is supplied from a local file. Office and CSV parsers can return
// filesystem, archive, XML, or cell-derived diagnostics; those details must
// not become durable source/import-item metadata or UI progress payloads.
// Keep the wider knowledge parser diagnostics unchanged so existing PDF and
// text troubleshooting behavior is not inadvertently narrowed.
func sanitizeKnowledgeParseError(kind string, err error) error {
	if err == nil {
		return nil
	}
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(kind)), ".") {
	case SourceKindDOC, SourceKindDOCX, SourceKindXLS, SourceKindXLSX, SourceKindPPT, SourceKindPPTX, SourceKindCSV:
		return sanitizeOfficeReadKnowledgeError(err)
	default:
		return err
	}
}

// officeReadImportParse is the import-only parse result. input is retained only
// until its caller has either consumed the matching rich payload or completed
// the legacy image phase; it is never persisted or exposed by public APIs.
type officeReadImportParse struct {
	nodes       []DocumentNode
	content     agent.OfficeReadRichContent
	richEnabled bool
	input       *knowledgeDocumentInput
	contentHash string
}

func (result *officeReadImportParse) close() {
	if result != nil {
		result.input.close()
	}
}

// parseDocumentNodesForOfficeReadImport is the import-only variant of
// ParseDocumentNodes. It keeps an Office private snapshot alive through the
// optional legacy image phase, ensuring text nodes and extracted images cannot
// be assembled from two versions of a concurrently replaced source file.
func parseDocumentNodesForOfficeReadImport(source Source, filePath, kind string) (*officeReadImportParse, error) {
	return parseDocumentNodesForOfficeReadImportWithOfficeReadConfig(source, filePath, kind, nil)
}

// parseDocumentNodesForOfficeReadImportWithOfficeReadConfig is the import
// boundary used by request-scoped hosts. A nil policy retains the desktop
// provider behavior; a non-nil policy must originate from trusted host config.
func parseDocumentNodesForOfficeReadImportWithOfficeReadConfig(source Source, filePath, kind string, config *agent.OfficeReadConfig) (*officeReadImportParse, error) {
	// Import can outlive the request goroutine. Preserve the trusted policy as
	// an independent snapshot before parsing, so the host may safely reuse or
	// mutate its config object for a later request without changing this one.
	config = agent.CloneOfficeReadConfigPtr(config)
	input, err := prepareKnowledgeDocumentInput(filePath, kind)
	if err != nil {
		return nil, err
	}
	contentHash := ""
	if isSnapshotBoundKnowledgeKind(kind) {
		// A Source's persisted content hash must identify the same bytes that
		// its text nodes and (for spreadsheets) structured row importer consume.
		// Office input and CSV input are private verified snapshots rather than
		// replaceable live paths. Other formats retain the scan/refresh hash
		// contract and do not pay a second full-file read here.
		contentHash, err = fileSHA256(input.path)
		if err != nil {
			input.close()
			return nil, sanitizeOfficeReadKnowledgeError(err)
		}
	}
	extraction := &officeReadRichExtraction{config: config}
	nodes, err := parseDocumentNodesFromInput(source, input.path, kind, extraction)
	if !extraction.loaded || !extraction.enabled {
		return &officeReadImportParse{nodes: nodes, input: input, contentHash: contentHash}, err
	}
	// Keep the parse-time policy decision even if structured Markdown was
	// unavailable and parseOfficeReadOrLegacyNodes used the legacy text path.
	// Otherwise a concurrent GUI toggle could make the later image stage take a
	// different policy (or reopen a document through a parser the original
	// decision had intentionally excluded).
	if extraction.err != nil || err != nil {
		return &officeReadImportParse{nodes: nodes, richEnabled: true, input: input, contentHash: contentHash}, err
	}
	return &officeReadImportParse{nodes: nodes, content: extraction.content, richEnabled: true, input: input, contentHash: contentHash}, nil
}

// parseDocumentNodesWithOfficeReadRichContent retains the historical narrow
// return shape for focused callers. Normal import/refresh paths use the owned
// result above so that a legacy image fallback shares the same snapshot too.
func parseDocumentNodesWithOfficeReadRichContent(source Source, filePath, kind string) ([]DocumentNode, agent.OfficeReadRichContent, bool, error) {
	result, err := parseDocumentNodesForOfficeReadImport(source, filePath, kind)
	if result == nil {
		return nil, agent.OfficeReadRichContent{}, false, err
	}
	defer result.close()
	return result.nodes, result.content, result.richEnabled, err
}

// ParseOfficeReadRichContentForKnowledge is a narrow public seam for the
// knowledge UI and tests. It never returns raw image bytes: those belong to
// the asset manager and are persisted only during an import transaction.
func ParseOfficeReadRichContentForKnowledge(source Source, filePath, kind string) ([]DocumentNode, bool, error) {
	nodes, err := parseOfficeReadNodes(source, filePath, kind)
	if IsUnsupportedParserError(err) {
		return nil, false, nil
	}
	return nodes, true, err
}

// ParseOfficeReadRichContentForKnowledgeFile offers the structured Markdown
// preview path without invoking the legacy parser. It is an explicit caller
// choice; the normal document/attachment read path remains text-only.
func ParseOfficeReadRichContentForKnowledgeFile(source Source, filePath string) ([]DocumentNode, bool, error) {
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
	if kind == "" {
		return nil, false, agent.ErrOfficeReadExtractionFailed
	}
	info, err := os.Stat(filePath)
	if err != nil {
		// Preview errors may be returned directly through the GUI. Do not expose
		// local paths or OS-specific access details before the adapter boundary.
		return nil, false, agent.ErrOfficeReadExtractionFailed
	}
	if info.IsDir() {
		return nil, false, agent.ErrOfficeReadExtractionFailed
	}
	return ParseOfficeReadRichContentForKnowledge(source, filePath, kind)
}

// officeReadImagesForImport is intentionally a second extraction rather than
// sharing opaque parser state across packages. Rich content is only enabled
// after an explicit configuration opt-in, and this function never exposes
// binary data outside knowledge asset ingestion.
func officeReadImagesForImport(
	ctx context.Context,
	s *SQLiteStore,
	source Source,
	filePath, kind string,
	textNodes []DocumentNode,
) []DocumentNode {
	if s == nil || s.imageAssets == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	content, enabled, err := agent.ExtractOfficeReadRichContent(filePath)
	if !enabled || err != nil || len(content.Images) == 0 || !officeReadRichContentMatchesKind(content, kind) {
		return nil
	}
	return officeReadImagesFromRichContentForImport(ctx, s, source, kind, textNodes, content)
}

// officeReadImagesFromRichContentForImport owns the conversion boundary from
// an already-extracted rich payload to managed knowledge assets. Keeping it
// separate allows the size and metadata rules to be exercised without
// extracting a second Office container in focused tests.
func officeReadImagesFromRichContentForImport(
	ctx context.Context,
	s *SQLiteStore,
	source Source,
	kind string,
	textNodes []DocumentNode,
	content agent.OfficeReadRichContent,
) []DocumentNode {
	if s == nil || s.imageAssets == nil || len(content.Images) == 0 {
		return nil
	}
	// This helper is also a narrow test/asset conversion seam. A missing
	// Format means the caller deliberately supplied a prevalidated payload;
	// reject only an explicit disagreement with the source kind.
	if strings.TrimSpace(content.Format) != "" && !officeReadRichContentMatchesKind(content, kind) {
		return nil
	}

	imageNodes := make([]DocumentNode, 0, len(content.Images))
	imageBytes := make(map[string][]byte, len(content.Images))
	contextText := officeReadImageContext(textNodes)
	for i, image := range content.Images {
		// OfficeRead already validates/normalizes image payloads. Do not apply
		// the legacy zip-extractor's arbitrary 500-byte decorative-image rule:
		// valid small icons can carry important document semantics, and the
		// asset manager enforces safe storage/decoding limits below.
		if !shouldImportOfficeReadImageData(image.Data) {
			continue
		}
		ext := normalizeImageExt(image.Ext)
		if ext == "." || ext == "" {
			ext = filepath.Ext(image.Name)
		}
		ext = normalizeImageExt(ext)
		nodeID := NewID("kdn")
		metadata := map[string]string{
			MetaImageFormat:    strings.TrimPrefix(ext, "."),
			"_image_bytes_key": nodeID,
			"extractor":        "officeread",
		}
		if alt := strings.TrimSpace(image.Alt); alt != "" {
			metadata[MetaImageAltText] = alt
		}
		if contextText != "" {
			metadata["context_before"] = contextText
		}
		if IsVectorImageExt(ext) {
			metadata[MetaImageIsVector] = "true"
		}
		title := strings.TrimSpace(image.Alt)
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(image.Name), filepath.Ext(image.Name))
		}
		if title == "" {
			title = fmt.Sprintf("Image %d", i+1)
		}
		imageNodes = append(imageNodes, DocumentNode{
			ID:       nodeID,
			SourceID: source.ID,
			Type:     NodeTypeImage,
			Title:    title,
			Metadata: metadata,
		})
		imageBytes[nodeID] = image.Data
	}
	return s.processExtractedImagesParallel(ctx, source, imageNodes, imageBytes)
}

func shouldImportOfficeReadImageData(data []byte) bool {
	// Keep the rich-content boundary aligned with the common managed asset
	// boundary. The agent adapter separately enforces the aggregate budget.
	return len(data) > 0 && int64(len(data)) <= MaxKnowledgeImageAssetBytes
}

func officeReadImageContext(nodes []DocumentNode) string {
	for _, node := range nodes {
		if node.Type == NodeTypeImage || strings.TrimSpace(node.Text) == "" {
			continue
		}
		return truncateRunes(node.Text, 400)
	}
	return ""
}
