package knowledge

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Shared formatting for knowledge tool execution results.
// Used by GUI, TUI, and agentservice (maclawsrv) to present knowledge_search
// and knowledge_context_pack results to the LLM in a consistent manner.
// Modify here → all platforms pick up changes.
// ---------------------------------------------------------------------------

// SearchResultsHeader is the guidance text prepended to formatted text-search
// results. Image presentation is intentionally handled only by the dedicated
// knowledge_image_search tool, so normal evidence recall never encourages a
// model to construct or replay an image marker.
const SearchResultsHeader = "Use these results as evidence. Cite Source/Citation when answering. If the results do not fully cover the user's question, search again with refined terms or use knowledge_context_pack for comprehensive coverage. Use knowledge_image_search when the user explicitly asks to find, view, show, select, or compare saved images.\n\n"

// EmptySearchResultMessage is returned when knowledge_search finds no matches.
const EmptySearchResultMessage = "No results found for this query. Try different search terms, broader keywords, or use knowledge_context_pack for topic-based retrieval."

// EmptyContextPackMessage is returned when knowledge_context_pack finds no matches.
const EmptyContextPackMessage = "No relevant knowledge found for this context pack query. Try different search terms or broader topic hints."

// FormatSearchResultsForLLM formats search results as structured text for LLM consumption.
// This is the shared implementation used by agentservice and TUI.
// GUI uses JSON serialization instead (for frontend rendering), but the guidance
// header principle is the same.
func FormatSearchResultsForLLM(results []SearchResult) string {
	if len(results) == 0 {
		return EmptySearchResultMessage
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d results:\n\n", len(results)))
	b.WriteString(SearchResultsHeader)
	for i, r := range results {
		b.WriteString(fmt.Sprintf("### Result %d (score: %.2f, type: %s)\n", i+1, r.Score, r.ResultType))
		if r.CardTitle != "" {
			b.WriteString(fmt.Sprintf("**Title**: %s\n", r.CardTitle))
		}
		if r.Claim != "" {
			b.WriteString(fmt.Sprintf("**Claim**: %s\n", r.Claim))
		}
		if r.Summary != "" {
			b.WriteString(fmt.Sprintf("**Summary**: %s\n", r.Summary))
		}
		if r.Snippet != "" && r.Snippet != r.Claim && r.Snippet != r.Summary {
			b.WriteString(fmt.Sprintf("**Snippet**: %s\n", r.Snippet))
		}
		if r.Subject != "" {
			b.WriteString(fmt.Sprintf("**Fact**: %s %s %s\n", r.Subject, r.Predicate, r.Object))
		}
		if r.NodeType == NodeTypeImage || r.Source.Kind == SourceKindImage {
			// Standalone images commonly retain their import path in Source.URI.
			// Tool output enters model context, so image evidence must never fall
			// back to that URI when title/relative-path metadata is absent.
			b.WriteString(fmt.Sprintf("**Source**: %s\n", FormatImageSourceLabel(r)))
			b.WriteString(fmt.Sprintf("**Citation**: %s\n", FormatImageCitationLabel(r)))
		} else {
			b.WriteString(fmt.Sprintf("**Source**: %s\n", FormatSourceLabel(r)))
			b.WriteString(fmt.Sprintf("**Citation**: %s\n", FormatCitationLabel(r)))
		}
		if r.NodeType == NodeTypeImage || r.Source.Kind == SourceKindImage {
			b.WriteString("**Image**: This is an image result. If the user asks to show it, use the image display marker provided by the knowledge client.\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// FormatContextPackForLLM formats context pack results as structured text for LLM consumption.
func FormatContextPackForLLM(result ContextPackResult) string {
	if len(result.Items) == 0 {
		return EmptyContextPackMessage
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Knowledge context pack (%d items, %d chars):\n\n", result.Count, result.CharacterCount))
	for i, item := range result.Items {
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, item.ResultType, item.Text))
		if item.Citation != "" {
			b.WriteString(fmt.Sprintf("   Citation: %s\n", item.Citation))
		}
	}
	return b.String()
}

// KnowledgeSearchToolDescription is the shared tool description for knowledge_search.
// Used by agentservice and can be referenced by GUI/TUI tool registrations.
const KnowledgeSearchToolDescription = "Search the local knowledge base (documents, URLs, saved text, and imported image evidence). Returns ranked knowledge cards, facts, image nodes, and source citations without calling an LLM. Use knowledge_image_search, not this general search, when the user asks to find, see, show, select, or compare an image; that dedicated tool returns the safe display marker the client can render. Use when the user asks about saved knowledge, imported documents, or previously stored information. Also use proactively BEFORE asking the user for task parameters that may already be stored — such as server addresses, login credentials/usernames, environment config, or project paths. If results are incomplete for the user's question, search again with different keywords or use knowledge_context_pack for comprehensive multi-source coverage."

// KnowledgeContextPackToolDescription is the shared tool description for knowledge_context_pack.
const KnowledgeContextPackToolDescription = "Build a compact, citation-backed knowledge context pack from the local knowledge base. Use before answering from stored knowledge when you need a prompt-ready bundle of ranked cards and facts under a character budget. Especially useful for count/list questions (e.g., 'how many books/patents/projects') where multiple evidence items must be aggregated."

// FormatSourceLabel returns the best human-readable label for a search result's source.
func FormatSourceLabel(r SearchResult) string {
	// Image sources may retain their import URI as a local filesystem path.
	// Keep the generic helper safe as well: auto-recall and a few legacy
	// callers use it directly instead of going through the dedicated image
	// search formatter.
	if r.NodeType == NodeTypeImage || r.Source.Kind == SourceKindImage {
		return FormatImageSourceLabel(r)
	}
	for _, value := range []string{r.Source.Title, r.Source.RelativePath, r.Source.URI, r.Source.ID} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "unknown source"
}

// FormatImageSourceLabel is the equivalent of FormatSourceLabel for evidence
// that represents a locally managed image. In contrast to generic sources, it
// must not use Source.URI as a fallback because an image import URI can be an
// absolute local path.
func FormatImageSourceLabel(r SearchResult) string {
	for _, value := range []string{r.Source.Title, r.Source.RelativePath, r.Source.ID} {
		if value = SafeImageDisplayText(value); value != "" {
			return value
		}
	}
	return "knowledge image"
}

// FormatImageCitationLabel omits the persisted generic citation when it may
// have been built from Source.URI. It preserves display-safe positional
// evidence such as page, sheet, and image title.
func FormatImageCitationLabel(r SearchResult) string {
	parts := make([]string, 0, 5)
	if r.Page > 0 {
		parts = append(parts, fmt.Sprintf("page %d", r.Page))
	}
	for _, value := range []string{r.SheetName, r.RowRange, r.ColRange, r.NodeTitle} {
		if value = SafeImageDisplayText(value); value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "image evidence"
	}
	return strings.Join(parts, ", ")
}

// SafeImageDisplayText accepts a compact user-facing label but rejects values
// that look like a host path. Image search results travel through LLM tools,
// Coding Agents, and UI renderers; source metadata is imported input and may
// contain a local absolute path even when it is nominally a title or a
// relative-path field. Keep this guard in the shared formatter so every
// display path gets the same boundary.
func SafeImageDisplayText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "file://") || filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") || isWindowsAbsolutePath(value) || imageDisplayTextHasEmbeddedHostPath(value) {
		return ""
	}
	return value
}

var (
	// Imported metadata can prepend human text such as "source: " before the
	// actual path. filepath.IsAbs only identifies a complete path, so it is not
	// sufficient for the LLM/tool display boundary. These patterns deliberately
	// require absolute-path syntax (not a relative filename) and cover foreign
	// Windows paths even when formatting on a POSIX host.
	imageDisplayWindowsPathPattern = regexp.MustCompile(`(?i)[a-z]:[\\/]`)
	imageDisplayUNCPathPattern     = regexp.MustCompile(`(?i)(?:^|[\s\[\(\{"'=])(?:\\\\|//)[^\\/\s]+[\\/]`)
	imageDisplayPOSIXPathPattern   = regexp.MustCompile(`(?:^|[\s\[\(\{"'=])/(?:[^/\s]+/)+[^/\s]+`)
)

func imageDisplayTextHasEmbeddedHostPath(value string) bool {
	return imageDisplayWindowsPathPattern.MatchString(value) ||
		imageDisplayUNCPathPattern.MatchString(value) ||
		imageDisplayPOSIXPathPattern.MatchString(value)
}

// ProjectImageSearchResultForTool narrows image evidence to the fields that an
// agent needs to identify and cite it. Source records are persistence models:
// their URI, canonical URI, relative path, project path, and import errors can
// all contain host details. Never serialize that broader model through a tool
// result merely because an image was returned by a general search.
func ProjectImageSearchResultForTool(result SearchResult) SearchResult {
	if result.NodeType != NodeTypeImage && result.Source.Kind != SourceKindImage {
		return result
	}
	projected := result
	projected.Source = ProjectImageSourceForTool(result.Source)
	projected.NodeTitle = SafeImageDisplayText(result.NodeTitle)
	projected.CardTitle = SafeImageDisplayText(result.CardTitle)
	projected.Citation = FormatImageCitationLabel(result)
	// Media returned by a store is normally limited to an opaque AssetID, but
	// SearchResult is also used at API, snapshot, and test seams. Do not assume
	// a caller-provided thumbnail URL, caption, or alt text is safe merely
	// because the result itself is an image. The agent needs only the managed
	// asset identifier; its delivery layer derives a thumbnail or API URL after
	// authorization.
	if result.Media != nil && ImageAssetIDBelongsToSourceID(result.Media.AssetID, result.Source.ID) {
		projected.Media = &SearchResultMedia{AssetID: result.Media.AssetID}
	} else {
		projected.Media = nil
	}
	return projected
}

// ProjectImageSourceForTool removes persistence-only metadata from a
// standalone image source. It is used by source-oriented agent tools (source
// lists, topic relevance, digests, and timelines), which otherwise serialize
// Source directly instead of passing through SearchResult.
func ProjectImageSourceForTool(source Source) Source {
	if source.Kind != SourceKindImage {
		return source
	}
	return Source{
		ID:     source.ID,
		Kind:   source.Kind,
		Title:  SafeImageDisplayText(source.Title),
		Labels: append([]string(nil), source.Labels...),
		Status: source.Status,
	}
}

// ProjectImageSourcesForTool applies ProjectImageSourceForTool without
// mutating persistence values owned by the caller.
func ProjectImageSourcesForTool(sources []Source) []Source {
	if len(sources) == 0 {
		return sources
	}
	projected := make([]Source, len(sources))
	for i := range sources {
		projected[i] = ProjectImageSourceForTool(sources[i])
	}
	return projected
}

// ProjectImageTopicRelevanceForTool narrows image sources embedded in topic
// relevance payloads before they are serialized into an agent tool result.
func ProjectImageTopicRelevanceForTool(report TopicRelevanceReport) TopicRelevanceReport {
	for i := range report.Sources {
		report.Sources[i].Source = ProjectImageSourceForTool(report.Sources[i].Source)
	}
	return report
}

// ProjectImageSourceGraphForTool removes filesystem-bearing fields from image
// nodes in a source graph. SourceGraphNode intentionally duplicates selected
// Source fields, so SearchResult projection cannot protect this route.
func ProjectImageSourceGraphForTool(graph SourceGraphResult) SourceGraphResult {
	imageSourceIDs := make(map[string]struct{})
	for i := range graph.Nodes {
		if graph.Nodes[i].Kind == SourceKindImage {
			imageSourceIDs[graph.Nodes[i].ID] = struct{}{}
		}
		graph.Nodes[i] = projectImageSourceGraphNode(graph.Nodes[i])
	}
	for i := range graph.Isolates {
		graph.Isolates[i] = projectImageSourceGraphNode(graph.Isolates[i])
	}
	for i := range graph.Edges {
		if _, leftImage := imageSourceIDs[graph.Edges[i].SourceID]; !leftImage {
			if _, rightImage := imageSourceIDs[graph.Edges[i].RelatedSourceID]; !rightImage {
				continue
			}
		}
		graph.Edges[i].Terms = projectImageDisplayStrings(graph.Edges[i].Terms)
		graph.Edges[i].Evidence = projectImageDisplayStrings(graph.Edges[i].Evidence)
	}
	return graph
}

// ProjectImageSourcePathForTool applies the source-graph projection to the
// node list carried by a source-path result.
func ProjectImageSourcePathForTool(path SourcePathResult) SourcePathResult {
	imageSourceIDs := make(map[string]struct{})
	for i := range path.Nodes {
		if path.Nodes[i].Kind != SourceKindImage {
			continue
		}
		imageSourceIDs[path.Nodes[i].ID] = struct{}{}
	}
	for i := range path.Nodes {
		path.Nodes[i] = projectImageSourceGraphNode(path.Nodes[i])
	}
	for i := range path.Steps {
		if _, leftImage := imageSourceIDs[path.Steps[i].FromSourceID]; !leftImage {
			if _, rightImage := imageSourceIDs[path.Steps[i].ToSourceID]; !rightImage {
				continue
			}
		}
		path.Steps[i].Terms = projectImageDisplayStrings(path.Steps[i].Terms)
		path.Steps[i].Evidence = projectImageDisplayStrings(path.Steps[i].Evidence)
	}
	return path
}

// ProjectImageSourceDigestForTool narrows the embedded image source and its
// image-node metadata. The opaque managed asset ID remains useful to the
// delivery layer; parser-only metadata and display fields do not.
func ProjectImageSourceDigestForTool(digest SourceDigestResult) SourceDigestResult {
	if digest.Source.Kind == SourceKindImage {
		digest.Source = ProjectImageSourceForTool(digest.Source)
		digest.Title = SafeImageDisplayText(digest.Title)
		if digest.Title == "" {
			digest.Title = digest.Source.ID
		}
	}
	digest.Nodes = ProjectImageDocumentNodesForTool(digest.Nodes)
	digest.Links = ProjectImageSourceLinksForParent(digest.Links, digest.Source.Kind == SourceKindImage)
	if digest.Source.Kind == SourceKindImage {
		for i := range digest.Timeline {
			digest.Timeline[i] = projectImageSourceTimelineEvent(digest.Timeline[i])
		}
	}
	return digest
}

// ProjectImageDocumentNodesForTool limits every image node to title and the
// managed asset ID. This is deliberately independent of the parent Source:
// an Office document digest can contain embedded image nodes too.
func ProjectImageDocumentNodesForTool(nodes []DocumentNode) []DocumentNode {
	if len(nodes) == 0 {
		return nodes
	}
	projected := append([]DocumentNode(nil), nodes...)
	for i := range projected {
		if projected[i].Type != NodeTypeImage {
			continue
		}
		projected[i].Title = SafeImageDisplayText(projected[i].Title)
		assetID := ""
		if projected[i].Metadata != nil {
			if candidate := projected[i].Metadata[MetaImageAssetID]; IsSafeImageAssetID(candidate) {
				assetID = candidate
			}
		}
		if assetID == "" {
			projected[i].Metadata = nil
		} else {
			projected[i].Metadata = map[string]string{MetaImageAssetID: assetID}
		}
	}
	return projected
}

// ProjectImageSourceTimelineForTool applies the image-source boundary to a
// timeline. Event titles/details can be assembled from title, relative path,
// and import errors, so they must be stripped alongside the Source record.
func ProjectImageSourceTimelineForTool(timeline SourceTimelineResult) SourceTimelineResult {
	if timeline.Source.Kind != SourceKindImage {
		return timeline
	}
	timeline.Source = ProjectImageSourceForTool(timeline.Source)
	for i := range timeline.Events {
		timeline.Events[i] = projectImageSourceTimelineEvent(timeline.Events[i])
	}
	return timeline
}

// ProjectImageSourceLinksForTool limits a link's nested image source and
// drops any term/evidence value that embeds a host path. Links are surfaced by
// source detail, graph maintenance, and topic-link tools, so they form a
// separate boundary from SearchResult and Source.
func ProjectImageSourceLinksForTool(links []SourceLink) []SourceLink {
	return ProjectImageSourceLinksForParent(links, false)
}

// ProjectImageSourceLinksForParent applies the link projection when either the
// nested related source is an image or the containing source is an image.
// A document source can legitimately retain a local project path in its link
// evidence, so do not apply image-only filtering to every link globally.
func ProjectImageSourceLinksForParent(links []SourceLink, parentIsImage bool) []SourceLink {
	if len(links) == 0 {
		return links
	}
	projected := append([]SourceLink(nil), links...)
	for i := range projected {
		isImage := parentIsImage || projected[i].RelatedSource.Kind == SourceKindImage
		projected[i].RelatedSource = ProjectImageSourceForTool(projected[i].RelatedSource)
		if isImage {
			projected[i].Terms = projectImageDisplayStrings(projected[i].Terms)
			projected[i].Evidence = projectImageDisplayStrings(projected[i].Evidence)
		}
	}
	return projected
}

// ProjectImageSourceTopicLinksForTool projects link-building results returned
// by preview and refresh topic-link operations.
func ProjectImageSourceTopicLinksForTool(result SourceTopicLinkBuildResult) SourceTopicLinkBuildResult {
	result.Links = ProjectImageSourceLinksForTool(result.Links)
	return result
}

// ProjectImageSourceLinkEventsForTool removes path-bearing imported evidence
// from link audit events. Events have no source-kind field, so this narrowing
// is safe for all events and prevents an image-linked event from bypassing the
// result-specific projection.
func ProjectImageSourceLinkEventsForTool(events []SourceLinkEvent) []SourceLinkEvent {
	return ProjectImageSourceLinkEventsForParent(events, false)
}

// ProjectImageSourceLinkEventsForParent filters audit evidence only when the
// source that owns the event is an image source.
func ProjectImageSourceLinkEventsForParent(events []SourceLinkEvent, parentIsImage bool) []SourceLinkEvent {
	if len(events) == 0 || !parentIsImage {
		return events
	}
	projected := append([]SourceLinkEvent(nil), events...)
	for i := range projected {
		projected[i].Terms = projectImageDisplayStrings(projected[i].Terms)
		projected[i].Evidence = projectImageDisplayStrings(projected[i].Evidence)
		projected[i].Note = SafeImageDisplayText(projected[i].Note)
	}
	return projected
}

// ProjectImageSourceQualityForTool removes persistence-oriented metadata from
// image sources embedded in quality reports and their maintenance plans.
func ProjectImageSourceQualityForTool(report SourceQualityReport) SourceQualityReport {
	for i := range report.Items {
		report.Items[i].Source = ProjectImageSourceForTool(report.Items[i].Source)
	}
	return report
}

// ProjectImageSourceChangePreviewForTool protects refresh-preview tools. A
// preview contains both the current and candidate sources, plus sample text
// generated from document nodes, so it is another model-visible boundary.
func ProjectImageSourceChangePreviewForTool(preview SourceChangePreview) SourceChangePreview {
	preview.Source = ProjectImageSourceForTool(preview.Source)
	preview.NextSource = ProjectImageSourceForTool(preview.NextSource)
	if preview.Source.Kind == SourceKindImage || preview.NextSource.Kind == SourceKindImage {
		preview.Error = SafeImageDisplayText(preview.Error)
		for i := range preview.Samples {
			preview.Samples[i].Title = SafeImageDisplayText(preview.Samples[i].Title)
			preview.Samples[i].Snippet = SafeImageDisplayText(preview.Samples[i].Snippet)
		}
	}
	return preview
}

// ProjectImageSourceChangePreviewsForTool applies the same projection to a
// batch refresh-preview payload.
func ProjectImageSourceChangePreviewsForTool(result SourceChangePreviewResult) SourceChangePreviewResult {
	for i := range result.Previews {
		result.Previews[i] = ProjectImageSourceChangePreviewForTool(result.Previews[i])
	}
	return result
}

// ProjectImageSourceRefreshForTool projects the source list returned by bulk
// refresh operations. Those operations are available as Agent tools, so their
// result must observe the same image-source boundary as a source list.
func ProjectImageSourceRefreshForTool(result SourceRefreshResult) SourceRefreshResult {
	result.Sources = ProjectImageSourcesForTool(result.Sources)
	return result
}

// ProjectImageSourceRebuildForTool projects sources returned after derived
// cards/facts are rebuilt. The rebuild itself uses full persistence metadata;
// only the Agent-facing response is narrowed.
func ProjectImageSourceRebuildForTool(result SourceRebuildResult) SourceRebuildResult {
	result.Sources = ProjectImageSourcesForTool(result.Sources)
	return result
}

// ProjectImageSourceStatusUpdateForTool projects sources returned by bulk
// enable/disable operations.
func ProjectImageSourceStatusUpdateForTool(result SourceStatusUpdateResult) SourceStatusUpdateResult {
	result.Sources = ProjectImageSourcesForTool(result.Sources)
	return result
}

// ProjectImageSourceLabelUpdateForTool projects the sources returned by label
// update/backfill operations.
func ProjectImageSourceLabelUpdateForTool(result SourceLabelUpdateResult) SourceLabelUpdateResult {
	result.Sources = ProjectImageSourcesForTool(result.Sources)
	return result
}

// ProjectImageSourceVersionsForTool narrows source-version records for image
// imports. Version history is an agent-visible audit route and otherwise
// retains the original and canonical import URI.
func ProjectImageSourceVersionsForTool(versions []SourceVersion) []SourceVersion {
	if len(versions) == 0 {
		return versions
	}
	projected := append([]SourceVersion(nil), versions...)
	for i := range projected {
		if projected[i].Kind != SourceKindImage {
			continue
		}
		projected[i].URI = ""
		projected[i].CanonicalURI = ""
		projected[i].Title = SafeImageDisplayText(projected[i].Title)
		projected[i].Reason = SafeImageDisplayText(projected[i].Reason)
	}
	return projected
}

// ProjectImageURLBatchSaveForTool is used by bulk URL saves, whose source list
// can include imported image sources when a caller reuses the shared result
// type at the tool boundary.
func ProjectImageURLBatchSaveForTool(result URLBatchSaveResult) URLBatchSaveResult {
	result.Sources = ProjectImageSourcesForTool(result.Sources)
	return result
}

// ProjectImageQualityMaintenanceExecutionForTool projects both the planned
// quality evidence and concrete action payloads. Action results are typed as
// interface{} because maintenance actions differ; recurse only over the known
// source-bearing result structures without mutating unknown result data.
func ProjectImageQualityMaintenanceExecutionForTool(result SourceQualityMaintenanceExecuteResult) SourceQualityMaintenanceExecuteResult {
	result.Plan.Quality = ProjectImageSourceQualityForTool(result.Plan.Quality)
	for i := range result.Results {
		switch value := result.Results[i].Result.(type) {
		case SourceRefreshResult:
			result.Results[i].Result = ProjectImageSourceRefreshForTool(value)
		case *SourceRefreshResult:
			if value != nil {
				projected := ProjectImageSourceRefreshForTool(*value)
				result.Results[i].Result = &projected
			}
		case SourceRebuildResult:
			result.Results[i].Result = ProjectImageSourceRebuildForTool(value)
		case *SourceRebuildResult:
			if value != nil {
				projected := ProjectImageSourceRebuildForTool(*value)
				result.Results[i].Result = &projected
			}
		case SourceStatusUpdateResult:
			result.Results[i].Result = ProjectImageSourceStatusUpdateForTool(value)
		case *SourceStatusUpdateResult:
			if value != nil {
				projected := ProjectImageSourceStatusUpdateForTool(*value)
				result.Results[i].Result = &projected
			}
		}
	}
	return result
}

func projectImageSourceGraphNode(node SourceGraphNode) SourceGraphNode {
	if node.Kind != SourceKindImage {
		return node
	}
	node.Label = SafeImageDisplayText(node.Label)
	if node.Label == "" {
		node.Label = node.ID
	}
	node.ProjectPath = ""
	node.RelativePath = ""
	node.URI = ""
	return node
}

func projectImageSourceTimelineEvent(event SourceTimelineEvent) SourceTimelineEvent {
	event.Title = SafeImageDisplayText(event.Title)
	event.Detail = SafeImageDisplayText(event.Detail)
	event.Terms = projectImageDisplayStrings(event.Terms)
	event.Evidence = projectImageDisplayStrings(event.Evidence)
	return event
}

func projectImageDisplayStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	projected := make([]string, 0, len(values))
	for _, value := range values {
		if value = SafeImageDisplayText(value); value != "" {
			projected = append(projected, value)
		}
	}
	return projected
}

// ProjectImageSearchResultsForTool applies the image evidence boundary to a
// general search response while preserving the result order and non-image
// evidence unchanged.
func ProjectImageSearchResultsForTool(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}
	projected := make([]SearchResult, len(results))
	for i := range results {
		projected[i] = ProjectImageSearchResultForTool(results[i])
	}
	return projected
}

// ProjectImageCitationForTool removes persisted filesystem-oriented source
// fields from a citation that belongs to image evidence. It is the citation
// counterpart of ProjectImageSearchResultForTool for explain/context-pack
// payloads, which expose citations separately from their search hits.
func ProjectImageCitationForTool(citation Citation, result SearchResult) Citation {
	if result.NodeType != NodeTypeImage && result.Source.Kind != SourceKindImage {
		return citation
	}
	citation.Label = FormatImageCitationLabel(result)
	citation.SourceTitle = SafeImageDisplayText(citation.SourceTitle)
	citation.URI = ""
	citation.RelativePath = ""
	return citation
}

// ProjectImageExplainForTool applies the same projection to the separately
// serialized hits and citations of a knowledge explanation response.
func ProjectImageExplainForTool(explain ExplainResult) ExplainResult {
	explain.Results = ProjectImageSearchResultsForTool(explain.Results)
	if len(explain.Citations) == 0 || len(explain.Results) == 0 {
		return explain
	}
	bySourceNode := make(map[string]SearchResult, len(explain.Results))
	for _, result := range explain.Results {
		key := result.Source.ID + "\x00" + result.NodeID + "\x00" + result.CardID + "\x00" + result.FactID
		bySourceNode[key] = result
	}
	for i := range explain.Citations {
		citation := explain.Citations[i]
		key := citation.SourceID + "\x00" + citation.NodeID + "\x00" + citation.CardID + "\x00" + citation.FactID
		if result, ok := bySourceNode[key]; ok {
			explain.Citations[i] = ProjectImageCitationForTool(citation, result)
		}
	}
	return explain
}

// isWindowsAbsolutePath covers metadata created on Windows even when a
// different host is formatting a migrated knowledge snapshot. filepath.IsAbs
// only recognizes the current operating system's path syntax.
func isWindowsAbsolutePath(value string) bool {
	if strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, `//`) {
		return true
	}
	return len(value) >= 3 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

// FormatCitationLabel returns the best human-readable citation for a search result.
func FormatCitationLabel(r SearchResult) string {
	if r.NodeType == NodeTypeImage || r.Source.Kind == SourceKindImage {
		return FormatImageCitationLabel(r)
	}
	parts := make([]string, 0, 4)
	if strings.TrimSpace(r.Citation) != "" {
		parts = append(parts, strings.TrimSpace(r.Citation))
	}
	if r.Page > 0 {
		parts = append(parts, fmt.Sprintf("page %d", r.Page))
	}
	for _, value := range []string{r.SheetName, r.RowRange, r.ColRange, r.NodeTitle} {
		if strings.TrimSpace(value) != "" {
			parts = append(parts, strings.TrimSpace(value))
		}
	}
	if len(parts) == 0 {
		return "source item"
	}
	return strings.Join(parts, ", ")
}

// BestContentText returns the most complete text representation of a SearchResult
// for injection into LLM system prompts (auto-recall, SubAgent context, etc.).
//
// Priority design:
//   - For "node" results (raw document chunks): FTS Snippet is the best representation
//     because nodes contain full text that may be very long; the snippet highlights
//     the relevant window around the search term.
//   - For "card" and "fact" results: Claim contains the card's complete distilled knowledge.
//     FTS Snippet is just a ~32-token match window extracted by SQLite's snippet() function,
//     which often truncates critical details (credentials, full lists, multi-line content).
//     Summary is a shorter abstract. Snippet is the last resort.
//
// This is the single source of truth for snippet extraction priority.
// All consumers (GUI auto-recall, TUI auto-recall, agentservice, SubAgent, RemoteSubAgent)
// should use this function instead of implementing their own priority logic.
func BestContentText(r SearchResult) string {
	if r.ResultType == "node" {
		if r.Snippet != "" {
			return r.Snippet
		}
		if r.Summary != "" {
			return r.Summary
		}
		if r.Claim != "" {
			return r.Claim
		}
		return ""
	}
	// For fact results, prefer claim (full card context) over raw triple.
	if r.ResultType == "fact" {
		if r.Claim != "" {
			return r.Claim
		}
		if r.Summary != "" {
			return r.Summary
		}
		if r.Subject != "" && r.Predicate != "" {
			return r.Subject + " " + r.Predicate + " " + r.Object
		}
		return ""
	}
	// For card results (default): Claim > Summary > Snippet > Triple.
	if r.Claim != "" {
		return r.Claim
	}
	if r.Summary != "" {
		return r.Summary
	}
	if r.Snippet != "" {
		return r.Snippet
	}
	if r.Subject != "" && r.Predicate != "" {
		return r.Subject + " " + r.Predicate + " " + r.Object
	}
	return ""
}
