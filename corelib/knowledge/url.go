package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

const (
	defaultURLFetchBytes = 5 * 1024 * 1024
	maxURLFetchBytes     = 10 * 1024 * 1024
	defaultURLTimeoutSec = 30
)

var (
	absoluteURLPattern = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"'(){}\[\]\x{FF0C}\x{FF1B}\x{3001}]+`)
	attrURLPattern     = regexp.MustCompile(`(?i)\b(?:href|src|data-url)\s*=\s*["']([^"']+)["']`)
	locURLPattern      = regexp.MustCompile(`(?is)<loc>\s*([^<]+?)\s*</loc>`)
)

func (s *SQLiteStore) SaveURL(ctx context.Context, req URLSaveRequest) (Source, error) {
	if err := enforceURLDomainPolicy(ctx, s, req.URL); err != nil {
		return Source{}, err
	}
	source, nodes, err := buildURLSourceAndNodes(ctx, req, Source{})
	if err != nil {
		return Source{}, err
	}
	if err := enforceURLDomainPolicy(ctx, s, fallbackText(source.CanonicalURI, source.URI)); err != nil {
		return Source{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Source{}, err
	}
	defer tx.Rollback()
	isDuplicate := false
	if existing, ok, err := findExistingURLSourceForSave(ctx, tx, source); err != nil {
		return Source{}, err
	} else if ok {
		isDuplicate = true
		source.ID = existing.ID
		source.CreatedAt = existing.CreatedAt
		if err := deleteSourceDerivedRows(ctx, tx, existing.ID); err != nil {
			return Source{}, err
		}
		for i := range nodes {
			nodes[i].SourceID = source.ID
		}
	}
	if err := insertSource(ctx, tx, source); err != nil {
		return Source{}, err
	}
	if err := addSourceLabelsTx(ctx, tx, source.ID, ingestLabelsForSource(source, req.Labels, req.AutoLabels)); err != nil {
		return Source{}, err
	}
	if err := insertDocumentNodes(ctx, tx, nodes); err != nil {
		return Source{}, err
	}
	source, err = s.DistillAndSaveCardsWithMode(ctx, tx, source, nodes, req.DistillMode)
	if err != nil {
		return Source{}, err
	}
	if err := insertSourceVersionTx(ctx, tx, source, "save_url"); err != nil {
		return Source{}, err
	}
	if err := tx.Commit(); err != nil {
		return Source{}, err
	}
	_ = s.BackfillNodeEmbeddingsForSources(ctx, []string{source.ID})
	_, _ = s.refreshSourceTopicLinksFast(ctx, source.ID, importTopicLinkLimit, nil)
	// Commit is the success boundary. Post-commit hydration is best-effort so
	// context expiry cannot report a persisted source as failed/retryable.
	return s.finalizeCommittedSource(ctx, source, isDuplicate), nil
}

func (s *SQLiteStore) SaveURLs(ctx context.Context, req URLBatchSaveRequest) URLBatchSaveResult {
	result := URLBatchSaveResult{}
	seen := make(map[string]struct{}, len(req.URLs))
	for _, rawURL := range splitURLBatchInputs(req.URLs) {
		if _, ok := seen[rawURL]; ok {
			result.Skipped++
			result.Items = append(result.Items, URLBatchSaveItem{URL: rawURL, Status: URLBatchSaveStatusSkippedDuplicate})
			continue
		}
		seen[rawURL] = struct{}{}
		result.Requested++
		source, err := s.SaveURL(ctx, URLSaveRequest{
			URL:         rawURL,
			OwnerID:     req.OwnerID,
			TenantID:    req.TenantID,
			ProjectPath: req.ProjectPath,
			TopicHint:   req.TopicHint,
			DistillMode: req.DistillMode,
			SaveScope:   req.SaveScope,
			Labels:      req.Labels,
			AutoLabels:  req.AutoLabels,
			MaxBytes:    req.MaxBytes,
			TimeoutSec:  req.TimeoutSec,
		})
		if err != nil {
			result.Failed++
			result.Items = append(result.Items, URLBatchSaveItem{URL: rawURL, Status: URLBatchSaveStatusFailed, Error: err.Error()})
			continue
		}
		result.Saved++
		if source.SaveStatus == SaveStatusDuplicate {
			result.Duplicates++
		}
		result.Sources = append(result.Sources, source)
		result.Items = append(result.Items, URLBatchSaveItem{URL: rawURL, SourceID: source.ID, Title: source.Title, Status: URLBatchSaveStatusFromSource(source.Status)})
	}
	return result
}

func splitURLBatchInputs(values []string) []string {
	urls := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, isKnowledgeListSeparator) {
			part = strings.TrimSpace(part)
			if part != "" {
				urls = append(urls, part)
			}
		}
	}
	return urls
}

func (s *SQLiteStore) DiscoverURLs(ctx context.Context, req URLDiscoveryRequest) (URLDiscoveryResult, error) {
	result := DiscoverURLsFromText(req)
	for i := range result.Items {
		if !result.Items[i].Status.IsCandidate() {
			continue
		}
		if err := enforceURLDomainPolicy(ctx, s, result.Items[i].URL); err != nil {
			result.Items[i].Status = URLDiscoveryStatusRejected
			result.Items[i].Reason = err.Error()
			result.Candidates--
			result.Rejected++
			continue
		}
		result.URLs = append(result.URLs, result.Items[i].URL)
	}
	return result, nil
}

func DiscoverURLsFromText(req URLDiscoveryRequest) URLDiscoveryResult {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	text := strings.TrimSpace(req.Text)
	base, _ := ValidatePublicHTTPURL(req.BaseURL)
	rawCandidates := extractURLCandidates(text)
	result := URLDiscoveryResult{}
	seen := make(map[string]struct{}, len(rawCandidates))
	for _, raw := range rawCandidates {
		if result.Candidates >= limit {
			break
		}
		result.Requested++
		normalized, host, err := normalizeDiscoveredURL(raw, base)
		if err != nil {
			result.Rejected++
			result.Items = append(result.Items, URLDiscoveryItem{URL: strings.TrimSpace(raw), Status: URLDiscoveryStatusRejected, Reason: err.Error()})
			continue
		}
		if req.SameDomainOnly && base != nil && !sameOrSubdomain(host, strings.ToLower(base.Hostname())) {
			result.Rejected++
			result.Items = append(result.Items, URLDiscoveryItem{URL: normalized, Host: host, Status: URLDiscoveryStatusRejected, Reason: "outside base domain"})
			continue
		}
		if _, ok := seen[normalized]; ok {
			result.Skipped++
			result.Items = append(result.Items, URLDiscoveryItem{URL: normalized, Host: host, Status: URLDiscoveryStatusSkippedDuplicate})
			continue
		}
		seen[normalized] = struct{}{}
		result.Candidates++
		result.Items = append(result.Items, URLDiscoveryItem{URL: normalized, Host: host, Status: URLDiscoveryStatusCandidate})
	}
	return result
}

func extractURLCandidates(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	candidates := make([]string, 0)
	add := func(value string) {
		value = strings.TrimSpace(html.UnescapeString(value))
		if value != "" {
			candidates = append(candidates, value)
		}
	}
	for _, match := range absoluteURLPattern.FindAllString(text, -1) {
		add(match)
	}
	for _, match := range attrURLPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}
	for _, match := range locURLPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}
	for _, token := range strings.FieldsFunc(text, isKnowledgeListSeparator) {
		token = strings.TrimSpace(token)
		if looksLikeBarePublicHost(token) {
			add(token)
		}
	}
	return candidates
}

func normalizeDiscoveredURL(raw string, base *url.URL) (string, string, error) {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	raw = strings.TrimFunc(raw, isURLCandidateBoundary)
	if raw == "" {
		return "", "", fmt.Errorf("empty URL")
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if !strings.Contains(raw, "://") && base != nil && (strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../")) {
		rel, err := url.Parse(raw)
		if err != nil {
			return "", "", err
		}
		raw = base.ResolveReference(rel).String()
	}
	u, err := ValidatePublicHTTPURL(raw)
	if err != nil {
		return "", "", err
	}
	return u.String(), strings.ToLower(u.Hostname()), nil
}

func looksLikeBarePublicHost(value string) bool {
	value = strings.TrimFunc(value, isURLCandidateBoundary)
	if value == "" || strings.Contains(value, "://") || strings.ContainsAny(value, " \\") {
		return false
	}
	host := value
	if slash := strings.Index(host, "/"); slash >= 0 {
		host = host[:slash]
	}
	if colon := strings.LastIndex(host, ":"); colon > -1 {
		host = host[:colon]
	}
	return strings.Contains(host, ".") && !IsBlockedHost(host)
}

func isURLCandidateBoundary(r rune) bool {
	switch r {
	case ' ', '\t', '\r', '\n', '"', '\'', '<', '>', ')', ',', '.', ';', ']', '}', '\uFF0C', '\uFF1B', '\u3001':
		return true
	default:
		return false
	}
}

func sameOrSubdomain(host, baseHost string) bool {
	host = strings.Trim(strings.ToLower(host), ".")
	baseHost = strings.Trim(strings.ToLower(baseHost), ".")
	return host == baseHost || strings.HasSuffix(host, "."+baseHost)
}

func findExistingURLSourceForSave(ctx context.Context, tx *sql.Tx, source Source) (Source, bool, error) {
	uri := strings.TrimSpace(source.URI)
	canonicalURI := strings.TrimSpace(source.CanonicalURI)
	if uri == "" && canonicalURI == "" {
		return Source{}, false, nil
	}
	q := `SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM knowledge_sources
		WHERE kind IN (?, ?) AND COALESCE(owner_id, '') = ? AND COALESCE(tenant_id, '') = ? AND COALESCE(project_path, '') = ?
		AND (uri = ? OR canonical_uri = ? OR uri = ? OR canonical_uri = ?)
		ORDER BY updated_at DESC LIMIT 1`
	existing, err := scanSource(tx.QueryRowContext(ctx, q,
		SourceKindURL, SourceKindHTML, source.OwnerID, source.TenantID, source.ProjectPath,
		uri, uri, canonicalURI, canonicalURI,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			return Source{}, false, nil
		}
		return Source{}, false, err
	}
	return existing, true, nil
}

func (s *SQLiteStore) RefreshSource(ctx context.Context, id string) (Source, error) {
	return s.RefreshSourceWithOfficeReadConfig(ctx, id, nil)
}

// RefreshSourceWithOfficeReadConfig refreshes a source under an optional
// trusted OfficeRead policy. It preserves the desktop provider behavior when
// config is nil and lets multi-tenant hosts avoid process-wide policy bleed.
func (s *SQLiteStore) RefreshSourceWithOfficeReadConfig(ctx context.Context, id string, config *agent.OfficeReadConfig) (Source, error) {
	existing, err := s.GetSource(ctx, id)
	if err != nil {
		return Source{}, err
	}
	if existing.Kind == SourceKindURL || existing.Kind == SourceKindHTML {
		return s.refreshURLSource(ctx, existing)
	}
	if isRefreshableFileSource(existing.Kind) {
		return s.refreshFileSourceWithOfficeReadConfig(ctx, existing, config)
	}
	return Source{}, fmt.Errorf("source %s kind %q is not refreshable", id, existing.Kind)
}

func (s *SQLiteStore) PreviewSourceRefresh(ctx context.Context, id string) (SourceChangePreview, error) {
	return s.PreviewSourceRefreshWithOfficeReadConfig(ctx, id, nil)
}

// PreviewSourceRefreshWithOfficeReadConfig computes a refresh preview under
// the same optional trusted policy that will be used for the actual refresh.
func (s *SQLiteStore) PreviewSourceRefreshWithOfficeReadConfig(ctx context.Context, id string, config *agent.OfficeReadConfig) (SourceChangePreview, error) {
	existing, err := s.GetSource(ctx, id)
	if err != nil {
		return SourceChangePreview{}, err
	}
	preview := SourceChangePreview{
		SourceID:    existing.ID,
		Source:      existing,
		Refreshable: existing.Kind == SourceKindURL || existing.Kind == SourceKindHTML || isRefreshableFileSource(existing.Kind),
		OldHash:     existing.ContentHash,
		OldStatus:   existing.Status,
		GeneratedAt: time.Now().UTC(),
	}
	if !preview.Refreshable {
		preview.Error = fmt.Sprintf("source %s kind %q is not refreshable", existing.ID, existing.Kind)
		return preview, nil
	}
	oldNodes, err := s.ListNodesBySource(ctx, existing.ID, 5000)
	if err != nil {
		return SourceChangePreview{}, err
	}
	preview.OldNodeCount = len(oldNodes)

	var next Source
	var nextNodes []DocumentNode
	if existing.Kind == SourceKindURL || existing.Kind == SourceKindHTML {
		if err := enforceURLDomainPolicy(ctx, s, fallbackText(existing.CanonicalURI, existing.URI)); err != nil {
			preview.Error = err.Error()
			return preview, nil
		}
		next, nextNodes, err = buildURLSourceAndNodes(ctx, URLSaveRequest{
			URL:         fallbackText(existing.CanonicalURI, existing.URI),
			OwnerID:     existing.OwnerID,
			TenantID:    existing.TenantID,
			ProjectPath: existing.ProjectPath,
			TopicHint:   existing.TopicHint,
		}, existing)
	} else {
		var distill bool
		var input *knowledgeDocumentInput
		next, nextNodes, _, _, distill, input, err = buildFileRefreshSourceAndNodesWithOfficeReadConfigForImport(existing, config)
		if input != nil {
			defer input.close()
		}
		err = sanitizeKnowledgeParseError(existing.Kind, err)
		_ = distill
		if err == nil && next.Kind == SourceKindPDF {
			var nativeErr error
			if next.Status == StatusFailed && strings.TrimSpace(next.ErrorMessage) != "" {
				nativeErr = errors.New(next.ErrorMessage)
			}
			ocrPath := next.URI
			if input != nil {
				ocrPath = input.path
			}
			ocr, ocrErr := s.extractPDFOCRNodesWithNativeFallback(ctx, next, ocrPath, nextNodes, nativeErr, true)
			if ocrErr != nil {
				err = ocrErr
			} else {
				nextNodes, nativeErr = mergePDFNodes(nextNodes, nativeErr, ocr)
				if nativeErr != nil {
					err = nativeErr
				} else {
					next.Status = StatusParsed
					next.ErrorMessage = ""
				}
			}
		}
	}
	if err != nil {
		preview.Error = err.Error()
		return preview, nil
	}
	if next.Status == StatusParsed && len(nextNodes) > 0 {
		next.Status = StatusDistilled
	}
	preview.NextSource = next
	preview.NewHash = next.ContentHash
	preview.NewStatus = next.Status
	preview.NewNodeCount = len(nextNodes)
	preview.HashChanged = strings.TrimSpace(preview.OldHash) != strings.TrimSpace(preview.NewHash)
	preview.AddedNodes, preview.RemovedNodes, preview.UnchangedNodes, preview.Samples = compareSourceNodes(oldNodes, nextNodes, 6)
	preview.Changed = preview.HashChanged || preview.AddedNodes > 0 || preview.RemovedNodes > 0 || preview.OldStatus != preview.NewStatus || strings.TrimSpace(existing.ErrorMessage) != strings.TrimSpace(next.ErrorMessage)
	preview.RequiresRefresh = preview.Changed
	if next.ErrorMessage != "" {
		preview.Error = next.ErrorMessage
	}
	return preview, nil
}

func (s *SQLiteStore) PreviewSourcesRefresh(ctx context.Context, ids []string) SourceChangePreviewResult {
	return s.PreviewSourcesRefreshWithOfficeReadConfig(ctx, ids, nil)
}

// PreviewSourcesRefreshWithOfficeReadConfig previews a group of refreshes
// under one immutable, trusted OfficeRead policy snapshot.
func (s *SQLiteStore) PreviewSourcesRefreshWithOfficeReadConfig(ctx context.Context, ids []string, config *agent.OfficeReadConfig) SourceChangePreviewResult {
	result := SourceChangePreviewResult{}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result.Requested++
		preview, err := s.PreviewSourceRefreshWithOfficeReadConfig(ctx, id, config)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, SourceChangePreviewFailure{SourceID: id, Error: err.Error()})
			continue
		}
		if preview.Error != "" && (!preview.Refreshable || preview.NewStatus == "") {
			result.Failed++
			result.Failures = append(result.Failures, SourceChangePreviewFailure{SourceID: id, Error: preview.Error})
		} else if preview.Changed {
			result.Changed++
		} else {
			result.Unchanged++
		}
		result.Previews = append(result.Previews, preview)
	}
	return result
}

func (s *SQLiteStore) PreviewSourcesRefreshByFilter(ctx context.Context, opts ListSourcesOptions) (SourceChangePreviewResult, error) {
	return s.PreviewSourcesRefreshByFilterWithOfficeReadConfig(ctx, opts, nil)
}

// PreviewSourcesRefreshByFilterWithOfficeReadConfig applies a request-scoped
// OfficeRead policy to every preview selected by the filter.
func (s *SQLiteStore) PreviewSourcesRefreshByFilterWithOfficeReadConfig(ctx context.Context, opts ListSourcesOptions, config *agent.OfficeReadConfig) (SourceChangePreviewResult, error) {
	opts.Limit = sourceFilterLimit(opts, 100, 500, 5000)
	if opts.Status == "" {
		opts.IncludeDisabled = true
	}
	sources, err := s.ListSources(ctx, opts)
	if err != nil {
		return SourceChangePreviewResult{}, err
	}
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.ID)
	}
	return s.PreviewSourcesRefreshWithOfficeReadConfig(ctx, ids, config), nil
}

func (s *SQLiteStore) RefreshChangedSources(ctx context.Context, ids []string) ChangedSourceRefreshResult {
	return s.RefreshChangedSourcesWithOfficeReadConfig(ctx, ids, nil)
}

// RefreshChangedSourcesWithOfficeReadConfig previews and refreshes under the
// same policy, so a setting change cannot make the two phases disagree.
func (s *SQLiteStore) RefreshChangedSourcesWithOfficeReadConfig(ctx context.Context, ids []string, config *agent.OfficeReadConfig) ChangedSourceRefreshResult {
	preview := s.PreviewSourcesRefreshWithOfficeReadConfig(ctx, ids, config)
	changedIDs := sourceIDsFromChangedPreviews(preview.Previews)
	return ChangedSourceRefreshResult{
		Preview:   preview,
		Refresh:   s.RefreshSourcesWithOfficeReadConfig(ctx, changedIDs, config),
		SourceIDs: changedIDs,
	}
}

func (s *SQLiteStore) RefreshChangedSourcesByFilter(ctx context.Context, opts ListSourcesOptions) (ChangedSourceRefreshResult, error) {
	return s.RefreshChangedSourcesByFilterWithOfficeReadConfig(ctx, opts, nil)
}

// RefreshChangedSourcesByFilterWithOfficeReadConfig keeps preview and apply
// parsing bound to the caller's request-scoped OfficeRead configuration.
func (s *SQLiteStore) RefreshChangedSourcesByFilterWithOfficeReadConfig(ctx context.Context, opts ListSourcesOptions, config *agent.OfficeReadConfig) (ChangedSourceRefreshResult, error) {
	preview, err := s.PreviewSourcesRefreshByFilterWithOfficeReadConfig(ctx, opts, config)
	if err != nil {
		return ChangedSourceRefreshResult{}, err
	}
	changedIDs := sourceIDsFromChangedPreviews(preview.Previews)
	return ChangedSourceRefreshResult{
		Preview:   preview,
		Refresh:   s.RefreshSourcesWithOfficeReadConfig(ctx, changedIDs, config),
		SourceIDs: changedIDs,
	}, nil
}

func sourceIDsFromChangedPreviews(previews []SourceChangePreview) []string {
	ids := make([]string, 0, len(previews))
	seen := make(map[string]struct{}, len(previews))
	for _, preview := range previews {
		id := strings.TrimSpace(preview.SourceID)
		if id == "" || !preview.Changed || preview.Error != "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (s *SQLiteStore) RefreshSources(ctx context.Context, ids []string) SourceRefreshResult {
	return s.RefreshSourcesWithOfficeReadConfig(ctx, ids, nil)
}

// RefreshSourcesWithOfficeReadConfig refreshes a group of sources under one
// immutable, trusted OfficeRead policy snapshot.
func (s *SQLiteStore) RefreshSourcesWithOfficeReadConfig(ctx context.Context, ids []string, config *agent.OfficeReadConfig) SourceRefreshResult {
	result := SourceRefreshResult{}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result.Requested++
		source, err := s.RefreshSourceWithOfficeReadConfig(ctx, id, config)
		if err != nil {
			result.Failed++
			appendSourceRefreshFailure(&result, id, err)
			continue
		}
		result.Refreshed++
		result.Sources = append(result.Sources, source)
	}
	return result
}

func (s *SQLiteStore) RefreshSourcesByFilter(ctx context.Context, opts ListSourcesOptions) (SourceRefreshResult, error) {
	return s.RefreshSourcesByFilterWithOfficeReadConfig(ctx, opts, nil)
}

// RefreshSourcesByFilterWithOfficeReadConfig applies a request-scoped
// OfficeRead policy to each selected source.
func (s *SQLiteStore) RefreshSourcesByFilterWithOfficeReadConfig(ctx context.Context, opts ListSourcesOptions, config *agent.OfficeReadConfig) (SourceRefreshResult, error) {
	opts.Limit = sourceFilterLimit(opts, 100, 500, 5000)
	if opts.Status == "" {
		opts.IncludeDisabled = true
	}
	sources, err := s.ListSources(ctx, opts)
	if err != nil {
		return SourceRefreshResult{}, err
	}
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.ID)
	}
	return s.RefreshSourcesWithOfficeReadConfig(ctx, ids, config), nil
}

func appendSourceRefreshFailure(result *SourceRefreshResult, sourceID string, err error) {
	if result == nil || err == nil {
		return
	}
	result.Failures = append(result.Failures, SourceRefreshFailure{SourceID: sourceID, Error: err.Error()})
	if IsSQLiteLockedError(err) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%s: transient sqlite lock during refresh; retry later", sourceID))
	}
}

func (s *SQLiteStore) refreshURLSource(ctx context.Context, existing Source) (Source, error) {
	if err := enforceURLDomainPolicy(ctx, s, fallbackText(existing.CanonicalURI, existing.URI)); err != nil {
		return Source{}, err
	}
	source, nodes, err := buildURLSourceAndNodes(ctx, URLSaveRequest{
		URL:         fallbackText(existing.CanonicalURI, existing.URI),
		OwnerID:     existing.OwnerID,
		TenantID:    existing.TenantID,
		ProjectPath: existing.ProjectPath,
		TopicHint:   existing.TopicHint,
	}, existing)
	if err != nil {
		return Source{}, err
	}
	return s.replaceSourceDerivedRows(ctx, source, nodes, true, "refresh")
}

func (s *SQLiteStore) refreshFileSource(ctx context.Context, existing Source) (Source, error) {
	return s.refreshFileSourceWithOfficeReadConfig(ctx, existing, nil)
}

func (s *SQLiteStore) refreshFileSourceWithOfficeReadConfig(ctx context.Context, existing Source, config *agent.OfficeReadConfig) (Source, error) {
	source, nodes, richContent, richContentAvailable, distill, input, err := buildFileRefreshSourceAndNodesWithOfficeReadConfigForImport(existing, config)
	if input != nil {
		defer input.close()
	}
	if err != nil {
		return Source{}, sanitizeKnowledgeParseError(existing.Kind, err)
	}
	if source.Kind == SourceKindPDF {
		var nativeErr error
		if source.Status == StatusFailed && strings.TrimSpace(source.ErrorMessage) != "" {
			nativeErr = errors.New(source.ErrorMessage)
		}
		ocrPath := source.URI
		if input != nil {
			ocrPath = input.path
		}
		ocr, ocrErr := s.extractPDFOCRNodesWithNativeFallback(ctx, source, ocrPath, nodes, nativeErr, true)
		if ocrErr != nil {
			// Refresh must be non-destructive: a transient OCR or render failure
			// must not replace a previously searchable source with an empty,
			// failed record.
			return Source{}, ocrErr
		}
		nodes, nativeErr = mergePDFNodes(nodes, nativeErr, ocr)
		if nativeErr != nil {
			return Source{}, nativeErr
		}
		source.Status = StatusParsed
		source.ErrorMessage = ""
	}
	imagePath := source.URI
	if input != nil {
		imagePath = input.path
	}
	if isSpreadsheetKind(source.Kind) {
		// Rich OfficeRead content has already been extracted from this import-owned
		// snapshot. Preserve spreadsheet images as managed knowledge assets before
		// the rows are atomically replaced, without reopening the workbook through
		// a legacy image parser. When rich content is disabled the established
		// spreadsheet path remains image-free.
		imageNodes := []DocumentNode(nil)
		if richContentAvailable {
			imageNodes = s.extractAndProcessDocumentImagesUsingRichOfficeContent(ctx, source, imagePath, source.Kind, nodes, richContent, true)
			nodes = append(nodes, imageNodes...)
		}
		// The structured row importer is the spreadsheet counterpart to the
		// document-node refresh. It must consume the same private parser snapshot
		// as the text nodes, and its writes must join the replacement transaction
		// so a refresh can never leave fresh nodes with stale (or absent) tables.
		refreshed, replaceErr := s.replaceSourceDerivedRowsWithSpreadsheet(ctx, source, nodes, distill, "refresh", imagePath)
		if replaceErr != nil {
			s.deleteProvisionalEmbeddedImageAssets(officeReadImageAssetIDs(imageNodes))
			return Source{}, replaceErr
		}
		return refreshed, nil
	}
	imageNodes := s.extractAndProcessDocumentImagesUsingRichOfficeContent(ctx, source, imagePath, source.Kind, nodes, richContent, richContentAvailable)
	// Image assets are published before their nodes so OCR/vision work does not
	// hold the SQLite write transaction.  A refresh is different from the batch
	// importer: it has no outer provisional-asset rollback.  Retain the newly
	// created opaque IDs here and reclaim them if the derived-row replacement
	// cannot commit; otherwise a failed refresh leaves files no database node can
	// reference (and a later asset doctor has to clean them up).
	provisionalImageAssetIDs := officeReadImageAssetIDs(imageNodes)
	if len(imageNodes) > 0 {
		nodes = append(nodes, imageNodes...)
	}
	refreshed, replaceErr := s.replaceSourceDerivedRows(ctx, source, nodes, distill, "refresh")
	if replaceErr != nil {
		s.deleteProvisionalEmbeddedImageAssets(provisionalImageAssetIDs)
		return Source{}, replaceErr
	}
	return refreshed, nil
}

func buildFileRefreshSourceAndNodes(existing Source) (Source, []DocumentNode, bool, error) {
	source, nodes, _, _, distill, err := buildFileRefreshSourceAndNodesWithOfficeReadRichContent(existing)
	return source, nodes, distill, err
}

// buildFileRefreshSourceAndNodesWithOfficeReadRichContent keeps its historical
// non-owning result contract for preview and other text-only callers.
func buildFileRefreshSourceAndNodesWithOfficeReadRichContent(existing Source) (Source, []DocumentNode, agent.OfficeReadRichContent, bool, bool, error) {
	source, nodes, content, available, distill, input, err := buildFileRefreshSourceAndNodesWithOfficeReadRichContentForImport(existing)
	if input != nil {
		input.close()
	}
	return source, nodes, content, available, distill, err
}

// buildFileRefreshSourceAndNodesWithOfficeReadRichContent mirrors the normal
// import parser selection and keeps the bounded rich payload local to a
// refresh operation. This makes refreshing an OfficeRead-enabled document
// rebuild both structured text and its managed image assets consistently.
func buildFileRefreshSourceAndNodesWithOfficeReadRichContentForImport(existing Source) (Source, []DocumentNode, agent.OfficeReadRichContent, bool, bool, *knowledgeDocumentInput, error) {
	return buildFileRefreshSourceAndNodesWithOfficeReadConfigForImport(existing, nil)
}

func buildFileRefreshSourceAndNodesWithOfficeReadConfigForImport(existing Source, config *agent.OfficeReadConfig) (Source, []DocumentNode, agent.OfficeReadRichContent, bool, bool, *knowledgeDocumentInput, error) {
	path := strings.TrimSpace(existing.URI)
	if path == "" {
		return Source{}, nil, agent.OfficeReadRichContent{}, false, false, nil, fmt.Errorf("source %s has no file path", existing.ID)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Source{}, nil, agent.OfficeReadRichContent{}, false, false, nil, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return Source{}, nil, agent.OfficeReadRichContent{}, false, false, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Source{}, nil, agent.OfficeReadRichContent{}, false, false, nil, fmt.Errorf("source %s points to a symbolic link", existing.ID)
	}
	if !info.Mode().IsRegular() {
		return Source{}, nil, agent.OfficeReadRichContent{}, false, false, nil, fmt.Errorf("source %s is not a regular file", existing.ID)
	}
	kind := existing.Kind
	if extKind := kindForExt(filepath.Ext(absPath)); extKind != "" {
		kind = extKind
	}
	if !isRefreshableFileSource(kind) {
		return Source{}, nil, agent.OfficeReadRichContent{}, false, false, nil, fmt.Errorf("source %s kind %q is not refreshable", existing.ID, kind)
	}
	// Record the version selected for this refresh before any parser gets to
	// create its private snapshot. A refresh that sees a different snapshot
	// later must fail rather than silently replacing indexed version A with a
	// version B that arrived mid-operation.
	refreshHash, err := boundedDocumentSHA256(absPath)
	if err != nil {
		return Source{}, nil, agent.OfficeReadRichContent{}, false, false, nil, err
	}
	now := time.Now().UTC()
	source := existing
	source.Kind = kind
	source.URI = absPath
	source.FetchedAt = now
	// The content hash is assigned only after parsing establishes its owned
	// snapshot. Hashing the live path here would let a Markdown/text refresh
	// report bytes from a later replacement while its nodes came from an earlier
	// private copy (or vice versa).
	source.ContentHash = ""
	source.Status = StatusParsed
	source.ErrorMessage = ""
	source.UpdatedAt = now
	if source.Title == "" {
		source.Title = strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
	}
	if source.RelativePath == "" {
		source.RelativePath = filepath.Base(absPath)
	}
	if source.SourceTrust == 0 {
		source.SourceTrust = 0.9
	}

	fileRefreshBeforeParse(existing)
	parsed, parseErr := parseDocumentNodesForOfficeReadImportWithOfficeReadConfig(source, absPath, kind, config)
	if parsed == nil {
		if errors.Is(parseErr, agent.ErrOfficeReadSourceChanged) {
			// A refresh must not replace already indexed content with a failure
			// record when the user-controlled path changed while its snapshot was
			// being established. Callers surface the stable retryable error and
			// leave the prior Source/nodes intact.
			return Source{}, nil, agent.OfficeReadRichContent{}, false, false, nil, agent.ErrOfficeReadSourceChanged
		}
		persistedParseErr := sanitizeKnowledgeParseError(kind, parseErr)
		source.Status = StatusFailed
		source.ErrorMessage = persistedParseErr.Error()
		return source, nil, agent.OfficeReadRichContent{}, false, false, nil, nil
	}
	nodes, richContent, richContentAvailable := parsed.nodes, parsed.content, parsed.richEnabled
	if parsed.contentHash != "" {
		source.ContentHash = parsed.contentHash
	}
	if source.ContentHash != refreshHash {
		parsed.close()
		return Source{}, nil, agent.OfficeReadRichContent{}, false, false, nil, agent.ErrOfficeReadSourceChanged
	}
	if strings.TrimSpace(source.ContentHash) == "" {
		parsed.close()
		return Source{}, nil, agent.OfficeReadRichContent{}, false, false, nil, agent.ErrOfficeReadExtractionFailed
	}
	if parseErr != nil && IsUnsupportedParserError(parseErr) {
		parsed.close()
		source.Status = StatusPending
		source.ErrorMessage = sanitizeKnowledgeParseError(kind, parseErr).Error()
		return source, nil, agent.OfficeReadRichContent{}, false, false, nil, nil
	}
	if parseErr != nil {
		if kind == SourceKindPDF {
			// A scanned PDF commonly has no native text. Retain the verified
			// snapshot and let the caller run its OCR merge against these exact
			// bytes; returning a hard parse error here would discard the snapshot
			// and make OCR reopen the mutable source path.
			source.Status = StatusFailed
			source.ErrorMessage = parseErr.Error()
			return source, nodes, richContent, richContentAvailable, false, parsed.input, nil
		}
		parsed.close()
		persistedParseErr := sanitizeKnowledgeParseError(kind, parseErr)
		source.Status = StatusFailed
		source.ErrorMessage = persistedParseErr.Error()
		return source, nil, agent.OfficeReadRichContent{}, false, false, nil, nil
	}
	return source, nodes, richContent, richContentAvailable, true, parsed.input, nil
}

func compareSourceNodes(oldNodes, newNodes []DocumentNode, sampleLimit int) (int, int, int, []SourceChangeSample) {
	if sampleLimit <= 0 {
		sampleLimit = 5
	}
	oldCounts := make(map[string]int, len(oldNodes))
	newCounts := make(map[string]int, len(newNodes))
	oldBySig := make(map[string]DocumentNode, len(oldNodes))
	newBySig := make(map[string]DocumentNode, len(newNodes))
	for _, node := range oldNodes {
		sig := documentNodeSignature(node)
		oldCounts[sig]++
		if _, ok := oldBySig[sig]; !ok {
			oldBySig[sig] = node
		}
	}
	for _, node := range newNodes {
		sig := documentNodeSignature(node)
		newCounts[sig]++
		if _, ok := newBySig[sig]; !ok {
			newBySig[sig] = node
		}
	}
	added := 0
	removed := 0
	unchanged := 0
	samples := make([]SourceChangeSample, 0, sampleLimit)
	for sig, newCount := range newCounts {
		oldCount := oldCounts[sig]
		if newCount <= oldCount {
			unchanged += newCount
			continue
		}
		unchanged += oldCount
		added += newCount - oldCount
		if len(samples) < sampleLimit {
			samples = append(samples, sourceChangeSample("added", newBySig[sig]))
		}
	}
	for sig, oldCount := range oldCounts {
		newCount := newCounts[sig]
		if oldCount <= newCount {
			continue
		}
		removed += oldCount - newCount
		if len(samples) < sampleLimit {
			samples = append(samples, sourceChangeSample("removed", oldBySig[sig]))
		}
	}
	return added, removed, unchanged, samples
}

func documentNodeSignature(node DocumentNode) string {
	parts := []string{
		strings.TrimSpace(node.Type),
		strings.TrimSpace(node.Title),
		strings.TrimSpace(node.Text),
		fmt.Sprintf("%d", node.Page),
		strings.TrimSpace(node.SheetName),
		strings.TrimSpace(node.RowRange),
		strings.TrimSpace(node.ColRange),
	}
	return sha256String(strings.Join(parts, "\x00"))
}

func sourceChangeSample(kind string, node DocumentNode) SourceChangeSample {
	return SourceChangeSample{
		Kind:    kind,
		Title:   fallbackText(node.Title, node.Type),
		Snippet: truncateChangeSnippet(node.Text),
	}
}

func truncateChangeSnippet(text string) string {
	text = normalizeWhitespace(text)
	runes := []rune(text)
	if len(runes) <= 220 {
		return text
	}
	return string(runes[:220])
}

func (s *SQLiteStore) replaceSourceDerivedRows(ctx context.Context, source Source, nodes []DocumentNode, distill bool, reason string) (Source, error) {
	return s.replaceSourceDerivedRowsWithSpreadsheet(ctx, source, nodes, distill, reason, "")
}

// replaceSourceDerivedRowsWithSpreadsheet replaces one source's derived rows
// atomically. spreadsheetPath is an already-owned parser pathname (normally a
// private Office or CSV snapshot); callers must keep it valid until this function returns.
// An empty pathname preserves the normal document/URL replacement behavior.
func (s *SQLiteStore) replaceSourceDerivedRowsWithSpreadsheet(ctx context.Context, source Source, nodes []DocumentNode, distill bool, reason, spreadsheetPath string) (Source, error) {
	oldAssetIDs, err := s.imageAssetIDsForSources(ctx, []string{source.ID})
	if err != nil {
		return Source{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Source{}, err
	}
	defer tx.Rollback()
	if err := deleteSourceDerivedRows(ctx, tx, source.ID); err != nil {
		return Source{}, err
	}
	if err := insertSource(ctx, tx, source); err != nil {
		return Source{}, err
	}
	if len(nodes) > 0 {
		if err := insertDocumentNodes(ctx, tx, nodes); err != nil {
			return Source{}, err
		}
	}
	if spreadsheetPath != "" {
		if !isSpreadsheetKind(source.Kind) {
			return Source{}, fmt.Errorf("private spreadsheet path provided for incompatible source %s", source.ID)
		}
		if _, err := importSpreadsheetSourceV2(ctx, tx, source, spreadsheetPath, source.Kind); err != nil {
			return Source{}, sanitizeKnowledgeParseError(source.Kind, err)
		}
	}
	if distill && len(nodes) > 0 {
		nextSource, err := s.DistillAndSaveCardsWithMode(ctx, tx, source, nodes, DistillModeAuto)
		if err != nil {
			return Source{}, err
		}
		source = nextSource
	}
	if err := insertSourceVersionTx(ctx, tx, source, reason); err != nil {
		return Source{}, err
	}
	if err := tx.Commit(); err != nil {
		return Source{}, err
	}
	// Asset writes precede the DB transaction so that image descriptions and
	// thumbnails can run without holding SQLite locks. After a successful
	// replacement, reclaim only the old IDs that are no longer referenced.
	// This preserves assets whose deterministic IDs remained unchanged.
	s.deleteSupersededImageAssets(ctx, source.ID, oldAssetIDs[source.ID])
	_ = s.BackfillNodeEmbeddingsForSources(ctx, []string{source.ID})
	sources := []Source{source}
	if err := s.hydrateSourceCounts(ctx, sources); err != nil {
		return Source{}, err
	}
	return sources[0], nil
}

func isRefreshableFileSource(kind string) bool {
	switch kind {
	case SourceKindMarkdown, SourceKindText, SourceKindDOCX, SourceKindPDF, SourceKindPPT, SourceKindPPTX, SourceKindXLSX, SourceKindCSV, SourceKindDOC, SourceKindXLS:
		return true
	default:
		return false
	}
}

func buildURLSourceAndNodes(ctx context.Context, req URLSaveRequest, existing Source) (Source, []DocumentNode, error) {
	u, err := ValidatePublicHTTPURL(req.URL)
	if err != nil {
		return Source{}, nil, err
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultURLFetchBytes
	}
	if maxBytes > maxURLFetchBytes {
		maxBytes = maxURLFetchBytes
	}
	timeoutSec := req.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = defaultURLTimeoutSec
	}
	if timeoutSec > 600 {
		timeoutSec = 600
	}

	var result *websearch.FetchResult
	if req.PrefetchedHTML != "" {
		// Use pre-fetched content (e.g. from deep crawl) — skip HTTP fetch entirely.
		// Extract title from HTML if possible, use hostname as fallback.
		title := extractTitleFromHTML(req.PrefetchedHTML)
		if title == "" {
			title = u.Hostname()
		}
		result = &websearch.FetchResult{
			URL:     u.String(),
			Title:   title,
			Content: req.PrefetchedHTML,
		}
	} else {
		client := newPublicHTTPClient(time.Duration(timeoutSec) * time.Second)
		result, err = websearch.FetchWithClientCtx(ctx, u.String(), &websearch.FetchOptions{MaxBytes: maxBytes, TimeoutS: timeoutSec}, client)
		if err != nil {
			// For HTTP 403 (anti-bot), try Chrome which has a real browser fingerprint.
			if !strings.Contains(err.Error(), "HTTP 403") {
				return Source{}, nil, err
			}
		}
		// Fallback: if static HTTP fetch returned no readable text (JS-rendered SPA pages),
		// was blocked by anti-bot (403), or returned a JS challenge page, retry with headless Chrome.
		if err != nil || needsJSRendering(result) {
			// Give Chrome extra time for cold-start + JS rendering.
			chromeTimeout := timeoutSec
			if chromeTimeout < 45 {
				chromeTimeout = 45
			}
			rendered, renderErr := websearch.FetchCtx(ctx, u.String(), &websearch.FetchOptions{
				MaxBytes: maxBytes,
				TimeoutS: chromeTimeout,
				RenderJS: true,
			})
			if renderErr == nil && strings.TrimSpace(rendered.Content) != "" {
				result = rendered
				err = nil // Clear any prior HTTP error (e.g. 403) since Chrome succeeded.
			} else if renderErr != nil {
				// Chrome fallback also failed — return specific error based on cause.
				if strings.Contains(renderErr.Error(), "Chrome not found") {
					return Source{}, nil, fmt.Errorf("no readable text extracted from URL (page likely requires JavaScript; install Chrome or Edge to enable rendering)")
				}
				return Source{}, nil, fmt.Errorf("no readable text extracted from URL (JavaScript rendering attempted but failed: %v)", renderErr)
			} else {
				// renderErr == nil but rendered.Content is still empty.
				// If we entered fallback due to HTTP error (e.g. 403), result may be nil — return original error.
				if result == nil {
					return Source{}, nil, err
				}
			}
		}
	}
	finalURL, err := ValidatePublicHTTPURL(result.URL)
	if err != nil {
		return Source{}, nil, fmt.Errorf("final URL rejected: %w", err)
	}
	text := normalizeKnowledgeText(result.Content)
	if text == "" {
		return Source{}, nil, fmt.Errorf("no readable text extracted from URL")
	}
	now := time.Now().UTC()
	title := strings.TrimSpace(result.Title)
	if title == "" {
		title = finalURL.Hostname()
	}
	source := Source{
		ID:           existing.ID,
		Kind:         SourceKindURL,
		URI:          u.String(),
		CanonicalURI: finalURL.String(),
		Title:        title,
		SiteName:     finalURL.Hostname(),
		FetchedAt:    now,
		ContentHash:  sha256String(finalURL.String() + "\x00" + text),
		OwnerID:      strings.TrimSpace(req.OwnerID),
		TenantID:     strings.TrimSpace(req.TenantID),
		ProjectPath:  strings.TrimSpace(req.ProjectPath),
		TopicHint:    strings.TrimSpace(req.TopicHint),
		SourceTrust:  0.6,
		BatchID:      strings.TrimSpace(req.BatchID),
		Status:       StatusParsed,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if source.ID == "" {
		source.ID = NewID("ksrc")
	}
	if !existing.CreatedAt.IsZero() {
		source.CreatedAt = existing.CreatedAt
	}
	node := DocumentNode{
		ID:       NewID("kdn"),
		SourceID: source.ID,
		Type:     "webpage",
		Title:    title,
		Text:     text,
		Metadata: map[string]string{
			"url":          u.String(),
			"final_url":    finalURL.String(),
			"content_type": result.ContentType,
		},
		TokenCount: estimateTokens(text),
	}
	return source, annotateMultilingualNodes([]DocumentNode{node}), nil
}

// needsJSRendering checks if a fetch result indicates the page requires JavaScript
// rendering to produce meaningful content. This catches:
// - Empty content (SPA skeleton with no text)
// - Cloudflare/Akamai JS challenge pages ("enable JavaScript", "checking your browser")
// - Short boilerplate with no actual page content
func needsJSRendering(result *websearch.FetchResult) bool {
	if result == nil {
		return true
	}
	text := strings.TrimSpace(result.Content)
	if text == "" {
		return true
	}
	// Very short content is suspicious — real pages have more than 200 chars of readable text.
	if len([]rune(text)) < 200 {
		lower := strings.ToLower(text)
		for _, sig := range jsChallengeSignals {
			if strings.Contains(lower, sig) {
				return true
			}
		}
	}
	return false
}

var jsChallengeSignals = []string{
	"enable javascript",
	"please turn javascript on",
	"checking your browser",
	"just a moment",
	"verify you are human",
	"please wait",
	"ray id",
	"cf-browser-verification",
	"ddos protection",
	"access denied",
	"attention required",
}

func newPublicHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           publicDialContext(dialer),
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableCompression:    true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if err := validateResolvedPublicURL(req.Context(), req.URL); err != nil {
				return err
			}
			if len(via) > 0 {
				req.Header.Set("User-Agent", via[0].Header.Get("User-Agent"))
			}
			return nil
		},
	}
}

func publicDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := resolvePublicIPs(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no public address found for %s", host)
	}
}

func validateResolvedPublicURL(ctx context.Context, u *url.URL) error {
	if u == nil {
		return fmt.Errorf("URL is required")
	}
	if _, err := ValidatePublicHTTPURL(u.String()); err != nil {
		return err
	}
	_, err := resolvePublicIPs(ctx, u.Hostname())
	return err
}

func resolvePublicIPs(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.Trim(host, "[]")
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("host %s resolves to non-public IP", host)
		}
		return []net.IP{ip}, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	public := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if isBlockedIP(addr.IP) {
			return nil, fmt.Errorf("host %s resolves to non-public IP %s", host, addr.IP.String())
		}
		public = append(public, addr.IP)
	}
	return public, nil
}

func fallbackText(primary, secondary string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(secondary)
}
