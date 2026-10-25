package guiapp

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

const (
	localSearchCorpusKnowledge      = "knowledge"
	localSearchCorpusProject        = "project"
	localSearchCorpusCloudWorkspace = "cloud_workspace"

	localSearchRouteHybrid = "hybrid"
	localSearchRouteFTS    = "fts"
	localSearchRouteVector = "vector"
	localSearchRouteScan   = "scan"

	localSearchFresh     = "fresh"
	localSearchUnindexed = "unindexed"

	localSearchProjectMaxLimit = 50

	// Ripgrep appends this when the walk stops early. Hits already collected
	// stay. An empty page is not a finished scan.
	projectScanStoppedMark = "search stopped before the tree was finished"

	localSearchEnterpriseMark = "[企业]"
)

// LocalSearchRequest is the shared evidence query for saved knowledge, the
// open project, and downloaded cloud-workspace text. One of Query or Pattern
// is set. Pattern is a project or cloud scan, not a knowledge query.
type LocalSearchRequest struct {
	Query     string   `json:"query,omitempty"`
	Retrieval string   `json:"retrieval,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	Fixed     bool     `json:"fixed,omitempty"`
	Corpus    string   `json:"corpus"`
	Root      string   `json:"root,omitempty"`
	Globs     []string `json:"globs,omitempty"`
	FileTypes []string `json:"file_types,omitempty"`
	Limit     int      `json:"limit,omitempty"`

	SearchScope     string   `json:"search_scope,omitempty"`
	ProjectPath     string   `json:"project_path,omitempty"`
	TopicHint       string   `json:"topic_hint,omitempty"`
	Domain          string   `json:"domain,omitempty"`
	ContextTerms    []string `json:"context_terms,omitempty"`
	ResultTypes     []string `json:"result_types,omitempty"`
	SourceKinds     []string `json:"source_kinds,omitempty"`
	SourceIDs       []string `json:"source_ids,omitempty"`
	Labels          []string `json:"labels,omitempty"`
	IncludeDisabled bool     `json:"include_disabled,omitempty"`
}

// LocalSearchHit is one evidence row. Knowledge hits keep the full citation
// payload. Project and cloud hits are line or file scans and stay unindexed.
type LocalSearchHit struct {
	Corpus        string                  `json:"corpus"`
	Route         string                  `json:"route"`
	Freshness     string                  `json:"freshness"`
	Locator       string                  `json:"locator,omitempty"`
	Title         string                  `json:"title,omitempty"`
	Snippet       string                  `json:"snippet,omitempty"`
	Score         float64                 `json:"score,omitempty"`
	Knowledge     *knowledge.SearchResult `json:"knowledge,omitempty"`
	Path          string                  `json:"path,omitempty"`
	Line          int                     `json:"line,omitempty"`
	WorkspaceID   string                  `json:"workspace_id,omitempty"`
	WorkspaceName string                  `json:"workspace_name,omitempty"`
	ProjectPath   string                  `json:"project_path,omitempty"`
	RelativePath  string                  `json:"relative_path,omitempty"`
	Match         string                  `json:"match,omitempty"`
	Tags          []string                `json:"tags,omitempty"`
}

// LocalSearchResult is the hits for one corpus and one route.
type LocalSearchResult struct {
	Corpus    string           `json:"corpus"`
	Route     string           `json:"route"`
	Freshness string           `json:"freshness"`
	Hits      []LocalSearchHit `json:"hits"`
}

// LocalSearch runs one evidence query. Knowledge uses the existing hybrid
// store, including enterprise merge. Project text uses ripgrep. Cloud
// workspace text uses the local cache scan. Neither of those corpora has an
// index, so their freshness is unindexed.
func (a *App) LocalSearch(req LocalSearchRequest) (LocalSearchResult, error) {
	if a == nil {
		return LocalSearchResult{}, fmt.Errorf("local search is unavailable")
	}
	query := strings.TrimSpace(req.Query)
	pattern := strings.TrimSpace(req.Pattern)
	if query != "" && pattern != "" {
		return LocalSearchResult{}, fmt.Errorf("local search accepts a query or a pattern, not both")
	}
	if query == "" && pattern == "" {
		return LocalSearchResult{}, fmt.Errorf("local search requires a query or a pattern")
	}
	req.Query = query
	req.Pattern = pattern
	switch strings.ToLower(strings.TrimSpace(req.Corpus)) {
	case localSearchCorpusKnowledge:
		return a.localSearchKnowledge(req)
	case localSearchCorpusProject:
		return a.localSearchProject(req)
	case localSearchCorpusCloudWorkspace:
		return a.localSearchCloudWorkspace(req)
	default:
		return LocalSearchResult{}, fmt.Errorf("unknown local search corpus %q", strings.TrimSpace(req.Corpus))
	}
}

func (a *App) localSearchKnowledge(req LocalSearchRequest) (LocalSearchResult, error) {
	if req.Pattern != "" {
		return LocalSearchResult{}, fmt.Errorf("pattern is only valid for project and cloud_workspace search")
	}
	route, err := localSearchKnowledgeRoute(req.Retrieval, req.Query)
	if err != nil {
		return LocalSearchResult{}, err
	}
	results, err := a.KnowledgeSearch(knowledge.SearchOptions{
		Query:           req.Query,
		Retrieval:       req.Retrieval,
		SearchScope:     req.SearchScope,
		ProjectPath:     req.ProjectPath,
		TopicHint:       req.TopicHint,
		Domain:          req.Domain,
		ContextTerms:    req.ContextTerms,
		ResultTypes:     req.ResultTypes,
		SourceKinds:     req.SourceKinds,
		SourceIDs:       req.SourceIDs,
		Labels:          req.Labels,
		Limit:           req.Limit,
		IncludeDisabled: req.IncludeDisabled,
	})
	if err != nil {
		return LocalSearchResult{}, err
	}
	hits := make([]LocalSearchHit, 0, len(results))
	for _, result := range results {
		copied := result
		title := localSearchKnowledgeTitle(copied)
		snippet := localSearchKnowledgeSnippet(copied)
		locator := knowledge.SearchResultLocator(copied)
		if title == "" {
			title = locator
		}
		hits = append(hits, LocalSearchHit{
			Corpus:    localSearchCorpusKnowledge,
			Route:     route,
			Freshness: localSearchFresh,
			Locator:   locator,
			Title:     title,
			Snippet:   snippet,
			Score:     copied.Score,
			Knowledge: &copied,
		})
	}
	return LocalSearchResult{
		Corpus:    localSearchCorpusKnowledge,
		Route:     route,
		Freshness: localSearchFresh,
		Hits:      hits,
	}, nil
}

func localSearchKnowledgeRoute(retrieval, query string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(retrieval)) {
	case "", "auto", localSearchRouteHybrid:
		retrieval = localSearchRouteHybrid
	case localSearchRouteFTS, localSearchRouteVector:
	default:
		return "", fmt.Errorf("unknown knowledge retrieval %q", strings.TrimSpace(retrieval))
	}
	if knowledge.QuerySuppressesEmbedding(query) {
		return localSearchRouteFTS, nil
	}
	return retrieval, nil
}

func localSearchEnterpriseLabel(text, sourceTitle, libraryID string) string {
	body := strings.TrimSpace(text)
	if body == "" || strings.HasPrefix(body, localSearchEnterpriseMark) {
		return body
	}
	if strings.TrimSpace(libraryID) != "" || strings.HasPrefix(strings.TrimSpace(sourceTitle), localSearchEnterpriseMark) {
		return localSearchEnterpriseMark + " " + body
	}
	return body
}

func localSearchFactText(result knowledge.SearchResult) string {
	if fact := strings.TrimSpace(result.Snippet); fact != "" {
		return fact
	}
	triple := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(result.Subject),
		strings.TrimSpace(result.Predicate),
		strings.TrimSpace(result.Object),
	}, " "))
	if triple != "" {
		return triple
	}
	return strings.TrimSpace(result.Claim)
}

func localSearchKnowledgeTitle(result knowledge.SearchResult) string {
	if result.ResultType == "fact" {
		for _, value := range []string{result.CardTitle, result.NodeTitle, result.Source.Title} {
			if value = strings.TrimSpace(value); value != "" {
				return localSearchEnterpriseLabel(value, result.Source.Title, result.LibraryID)
			}
		}
		if fact := localSearchFactText(result); fact != "" {
			return fact
		}
	}
	for _, value := range []string{
		result.CardTitle,
		result.NodeTitle,
		result.Source.Title,
		result.Claim,
		result.Summary,
		result.Citation,
		result.Source.URI,
	} {
		if value = strings.TrimSpace(value); value != "" {
			return localSearchEnterpriseLabel(value, result.Source.Title, result.LibraryID)
		}
	}
	return ""
}

func localSearchKnowledgeSnippet(result knowledge.SearchResult) string {
	if result.ResultType == "fact" {
		if fact := localSearchFactText(result); fact != "" {
			return fact
		}
	}
	for _, value := range []string{result.Snippet, result.Summary, result.Claim, result.Citation} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func (a *App) localSearchProject(req LocalSearchRequest) (LocalSearchResult, error) {
	if err := localSearchScanRetrieval(req.Retrieval); err != nil {
		return LocalSearchResult{}, err
	}
	root := strings.TrimSpace(req.Root)
	if root == "" {
		root = strings.TrimSpace(a.GetCurrentProjectPath())
	}
	if root == "" {
		return LocalSearchResult{}, fmt.Errorf("project search requires a root or a current project")
	}
	needle := req.Pattern
	fixed := req.Fixed
	if needle == "" {
		needle = req.Query
		fixed = true
	}
	args := map[string]interface{}{
		"pattern":      needle,
		"path":         root,
		"fixed_string": fixed,
		"max_results":  localSearchProjectLimit(req.Limit),
	}
	if glob := firstLocalSearchToken(req.Globs); glob != "" {
		args["glob"] = glob
	}
	if fileType := firstLocalSearchToken(req.FileTypes); fileType != "" {
		args["type"] = fileType
	}
	scanned := agent.ToolRipgrepDetailedCtx(a.knowledgeContext(), args)
	return projectScanResult(scanned.Text, scanned.Outcome)
}

func projectScanResult(text string, outcome agent.SearchToolOutcome) (LocalSearchResult, error) {
	if outcome == agent.SearchToolOutcomeError {
		msg := strings.TrimSpace(text)
		if msg == "" {
			msg = "project search failed"
		}
		return LocalSearchResult{}, fmt.Errorf("%s", msg)
	}
	hits := parseRipgrepHits(text)
	if len(hits) == 0 && strings.Contains(text, projectScanStoppedMark) {
		return LocalSearchResult{}, fmt.Errorf("project search stopped before the tree was finished")
	}
	if hits == nil {
		hits = []LocalSearchHit{}
	}
	return LocalSearchResult{
		Corpus:    localSearchCorpusProject,
		Route:     localSearchRouteScan,
		Freshness: localSearchUnindexed,
		Hits:      hits,
	}, nil
}

func (a *App) localSearchCloudWorkspace(req LocalSearchRequest) (LocalSearchResult, error) {
	if err := localSearchScanRetrieval(req.Retrieval); err != nil {
		return LocalSearchResult{}, err
	}
	needle := req.Query
	if req.Pattern != "" {
		needle = req.Pattern
	}
	found := a.SearchCloudWorkspaceContent(needle, req.Limit)
	hits := make([]LocalSearchHit, 0, len(found))
	for _, item := range found {
		locator := strings.TrimSpace(item.ID)
		if locator == "" {
			locator = strings.Trim(strings.TrimSpace(item.WorkspaceID)+"/"+strings.TrimSpace(item.RelativePath), "/")
		}
		hits = append(hits, LocalSearchHit{
			Corpus:        localSearchCorpusCloudWorkspace,
			Route:         localSearchRouteScan,
			Freshness:     localSearchUnindexed,
			Locator:       locator,
			Title:         item.Title,
			Snippet:       item.Preview,
			Path:          item.ProjectPath,
			WorkspaceID:   item.WorkspaceID,
			WorkspaceName: item.WorkspaceName,
			ProjectPath:   item.ProjectPath,
			RelativePath:  item.RelativePath,
			Match:         item.Match,
			Tags:          item.Tags,
		})
	}
	return LocalSearchResult{
		Corpus:    localSearchCorpusCloudWorkspace,
		Route:     localSearchRouteScan,
		Freshness: localSearchUnindexed,
		Hits:      hits,
	}, nil
}

func localSearchScanRetrieval(retrieval string) error {
	switch strings.ToLower(strings.TrimSpace(retrieval)) {
	case "", "auto":
		return nil
	default:
		return fmt.Errorf("retrieval %q is only valid for the knowledge corpus", strings.TrimSpace(retrieval))
	}
}

func localSearchProjectLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > localSearchProjectMaxLimit {
		return localSearchProjectMaxLimit
	}
	return limit
}

func firstLocalSearchToken(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// The first :line: is the separator the scanner writes. A later :443: inside
// the matched text must stay in the snippet. The drive colon is not a line
// number, so the leftmost digit field is the file line.
var ripgrepLinePattern = regexp.MustCompile(`:(\d+):(.*)`)

func parseRipgrepHits(text string) []LocalSearchHit {
	if strings.TrimSpace(text) == "" || strings.HasPrefix(strings.TrimSpace(text), "no matches") {
		return nil
	}
	var hits []LocalSearchHit
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "...") || strings.HasPrefix(line, "no matches") {
			continue
		}
		path, lineNo, snippet, ok := splitRipgrepContentLine(line)
		if !ok {
			hits = append(hits, LocalSearchHit{
				Corpus:    localSearchCorpusProject,
				Route:     localSearchRouteScan,
				Freshness: localSearchUnindexed,
				Snippet:   line,
			})
			continue
		}
		hits = append(hits, LocalSearchHit{
			Corpus:    localSearchCorpusProject,
			Route:     localSearchRouteScan,
			Freshness: localSearchUnindexed,
			Locator:   fmt.Sprintf("%s:%d", path, lineNo),
			Title:     filepath.Base(path),
			Snippet:   snippet,
			Path:      path,
			Line:      lineNo,
		})
	}
	return hits
}

func splitRipgrepContentLine(line string) (path string, lineNo int, snippet string, ok bool) {
	loc := ripgrepLinePattern.FindStringSubmatchIndex(line)
	if loc == nil {
		return "", 0, "", false
	}
	path = line[:loc[0]]
	if path == "" {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(line[loc[2]:loc[3]])
	if err != nil || n <= 0 {
		return "", 0, "", false
	}
	return path, n, line[loc[4]:loc[5]], true
}
