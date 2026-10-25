package guiapp

import (
	"context"
	"encoding/xml"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// The 搜索 button next to 确定. A keyword query hits the public arXiv API and
// comes back as titles and abstracts. Opening one result reuses
// FileCompanionOpenArxiv, so a paper is downloaded once and then read locally.
const (
	arxivSearchMaxResults       = 10
	arxivSearchTimeoutS         = 30
	arxivSearchMaxQueryRunes    = 200
	arxivSearchMaxTerms         = 12
	arxivSearchMaxTermRunes     = 80
	arxivSearchMaxAuthors       = 8
	arxivSearchMaxTitleRunes    = 500
	arxivSearchMaxAbstractRunes = 4000
	// arxivSearchMaxCategories caps the subject chips one row carries. arXiv
	// entries routinely list four or five cross-lists, and printing them all
	// turns the citation line into a paragraph.
	arxivSearchMaxCategories = 3
)

// arxivAPIBaseURL is the Atom query endpoint. Tests point it at a local server.
var arxivAPIBaseURL = "https://export.arxiv.org/api/query"

// FileCompanionArxivHit is one paper in the search list. ID is the canonical
// version-less identifier FileCompanionOpenArxiv accepts.
//
// Published and Categories are what make the list readable as a bibliography
// rather than a pile of paragraphs. Published is the calendar date on the
// citation line: the latest submitted version (Atom updated) when that
// timestamp parses, otherwise the original submission (Atom published). The
// JSON name stays published so the list has one date field. Categories carry
// the primary subject. Both are optional so a feed without them still lists.
type FileCompanionArxivHit struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Abstract   string   `json:"abstract"`
	Authors    string   `json:"authors,omitempty"`
	Published  string   `json:"published,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

type arxivAtomFeed struct {
	XMLName xml.Name         `xml:"http://www.w3.org/2005/Atom feed"`
	Entries []arxivAtomEntry `xml:"http://www.w3.org/2005/Atom entry"`
}

type arxivAtomEntry struct {
	ID         string           `xml:"http://www.w3.org/2005/Atom id"`
	Title      string           `xml:"http://www.w3.org/2005/Atom title"`
	Summary    string           `xml:"http://www.w3.org/2005/Atom summary"`
	Published  string           `xml:"http://www.w3.org/2005/Atom published"`
	Updated    string           `xml:"http://www.w3.org/2005/Atom updated"`
	Authors    []arxivAtomName  `xml:"http://www.w3.org/2005/Atom author"`
	Categories []arxivAtomTerm  `xml:"http://www.w3.org/2005/Atom category"`
	Primary    arxivAtomPrimary `xml:"http://arxiv.org/schemas/atom primary_category"`
}

type arxivAtomName struct {
	Name string `xml:"http://www.w3.org/2005/Atom name"`
}

type arxivAtomTerm struct {
	Term string `xml:"term,attr"`
}

type arxivAtomPrimary struct {
	Term string `xml:"term,attr"`
}

// FileCompanionSearchArxiv searches arXiv by keyword and returns up to ten
// papers. An empty box is a user error, not a network failure.
func (a *App) FileCompanionSearchArxiv(query string) ([]FileCompanionArxivHit, error) {
	empty := []FileCompanionArxivHit{}
	raw := strings.TrimSpace(query)
	if raw == "" {
		return empty, errors.New("请输入关键字")
	}
	if rs := []rune(raw); len(rs) > arxivSearchMaxQueryRunes {
		raw = string(rs[:arxivSearchMaxQueryRunes])
	}
	searchURL, err := arxivSearchURL(raw)
	if err != nil {
		return empty, err
	}
	parent := context.Background()
	if a != nil && a.ctx != nil {
		parent = a.ctx
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(arxivSearchTimeoutS)*time.Second+arxivDownloadSlack)
	defer cancel()
	var strategy corelib.WebSearchStrategy
	if a != nil && a.imHandler != nil {
		strategy = a.imHandler.getWebSearchStrategy()
	}
	result, err := websearch.FetchWithStrategyCtx(ctx, searchURL, &websearch.FetchOptions{
		TimeoutS: arxivSearchTimeoutS,
		MaxBytes: 2 * 1024 * 1024,
		Headers: map[string]string{
			"Accept": "application/atom+xml, application/xml;q=0.9, */*;q=0.1",
		},
	}, strategy)
	if err != nil {
		logFileCompanion("arxiv search failed")
		return empty, errors.New(arxivSearchErrorText(err))
	}
	body := ""
	if result != nil {
		body = result.Content
	}
	hits, err := arxivParseFeed(body)
	if err != nil {
		logFileCompanion("arxiv search failed")
		return empty, errors.New("搜索失败：arXiv 没有返回论文列表")
	}
	if hits == nil {
		hits = empty
	}
	logFileCompanion("arxiv search hits=%d", len(hits))
	return hits, nil
}

// arxivSearchURL builds the Atom query. A query whose every term is itself an
// arXiv id uses id_list: the all: field covers title, author, and abstract,
// not the identifier, so all:"2510.16079" returns papers that mention that
// number and misses the paper. The open box prefills this dialog with exactly
// those ids. Anything else is an all-field query, each term quoted so a pasted
// colon, quote, or the word AND cannot change the field or add a boolean.
//
// sortBy=relevance is only valid with search_query. Sending it beside id_list
// makes the API answer with an error entry instead of the paper.
func arxivSearchURL(query string) (string, error) {
	tokens := arxivSearchTokens(query)
	if len(tokens) == 0 {
		return "", errors.New("请输入关键字")
	}
	u, err := url.Parse(arxivAPIBaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("搜索失败")
	}
	params := u.Query()
	params.Set("start", "0")
	if ids, ok := arxivSearchIDsOnly(tokens); ok {
		params.Set("id_list", strings.Join(ids, ","))
		params.Set("max_results", strconv.Itoa(len(ids)))
	} else {
		parts := make([]string, len(tokens))
		for i, token := range tokens {
			parts[i] = `all:"` + token + `"`
		}
		params.Set("search_query", strings.Join(parts, " AND "))
		params.Set("max_results", strconv.Itoa(arxivSearchMaxResults))
		params.Set("sortBy", "relevance")
		params.Set("sortOrder", "descending")
	}
	u.RawQuery = params.Encode()
	return u.String(), nil
}

// arxivSearchTokens splits on whitespace. A comma splits the query only when
// every piece is an arXiv id, so "2510.16079,1706.03762" is two papers while
// "N,N-dimethyl" stays one keyword.
func arxivSearchTokens(query string) []string {
	fields := strings.Fields(query)
	if pieces, ok := arxivCommaSeparatedIDs(fields); ok {
		return pieces
	}
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		token := arxivSearchToken(field)
		if token == "" {
			continue
		}
		tokens = append(tokens, token)
		if len(tokens) >= arxivSearchMaxTerms {
			return tokens
		}
	}
	return tokens
}

// arxivCommaSeparatedIDs is the no-space id list. A later word that is not an
// id rejects the split, so the comma stays inside the keyword instead of
// dropping that word and fetching only the papers.
func arxivCommaSeparatedIDs(fields []string) ([]string, bool) {
	pieces := make([]string, 0, len(fields))
	sawComma := false
	for _, field := range fields {
		parts := strings.FieldsFunc(field, arxivSearchComma)
		if len(parts) == 0 {
			continue
		}
		if len(parts) > 1 {
			sawComma = true
		}
		for _, part := range parts {
			token := arxivSearchToken(part)
			if token == "" {
				continue
			}
			if arxivParseCandidate(arxivNormalizeInput(token)) == "" {
				return nil, false
			}
			pieces = append(pieces, token)
		}
	}
	if !sawComma || len(pieces) == 0 {
		return nil, false
	}
	if len(pieces) > arxivSearchMaxTerms {
		pieces = pieces[:arxivSearchMaxTerms]
	}
	return pieces, true
}

func arxivSearchComma(r rune) bool {
	return r == ',' || r == '，'
}

// arxivSearchIDsOnly reports the canonical ids when every term is an arXiv
// reference. One ordinary word makes the whole query a keyword search, so
// "attention 2510.16079" is not silently reduced to the id.
func arxivSearchIDsOnly(tokens []string) ([]string, bool) {
	if len(tokens) == 0 {
		return nil, false
	}
	ids := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		id := arxivParseCandidate(arxivNormalizeInput(token))
		if id == "" {
			return nil, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, false
	}
	if len(ids) > arxivSearchMaxResults {
		ids = ids[:arxivSearchMaxResults]
	}
	return ids, true
}

func arxivSearchToken(field string) string {
	var b strings.Builder
	for _, r := range field {
		switch r {
		case '"', '\\', '\n', '\r', '\t':
			continue
		default:
			if r < 32 {
				continue
			}
			b.WriteRune(r)
		}
	}
	// A trailing comma is the separator before the next word ("Smith, John"),
	// not part of the term. A comma inside the term ("N,N-dimethyl") stays.
	token := strings.Trim(strings.TrimSpace(b.String()), ",，")
	rs := []rune(token)
	if len(rs) > arxivSearchMaxTermRunes {
		token = string(rs[:arxivSearchMaxTermRunes])
	}
	return strings.TrimSpace(token)
}

func arxivParseFeed(body string) ([]FileCompanionArxivHit, error) {
	body = strings.TrimSpace(strings.TrimPrefix(body, "\uFEFF"))
	if body == "" {
		return nil, errors.New("empty feed")
	}
	var feed arxivAtomFeed
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		return nil, err
	}
	if feed.XMLName.Local != "feed" {
		return nil, errors.New("not a feed")
	}
	hits := make([]FileCompanionArxivHit, 0, len(feed.Entries))
	seen := make(map[string]struct{}, len(feed.Entries))
	for _, entry := range feed.Entries {
		id := arxivIDFromInput(entry.ID)
		title := arxivLimitRunes(arxivCollapseSpace(entry.Title), arxivSearchMaxTitleRunes)
		if id == "" || title == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		names := make([]string, 0, len(entry.Authors))
		for _, author := range entry.Authors {
			name := arxivCollapseSpace(author.Name)
			if name != "" {
				names = append(names, name)
			}
		}
		hits = append(hits, FileCompanionArxivHit{
			ID:         id,
			Title:      title,
			Abstract:   arxivLimitRunes(arxivCollapseSpace(entry.Summary), arxivSearchMaxAbstractRunes),
			Authors:    arxivAuthorLine(names),
			Published:  arxivListDate(entry.Updated, entry.Published),
			Categories: arxivCategoryList(entry.Primary.Term, entry.Categories),
		})
		if len(hits) >= arxivSearchMaxResults {
			break
		}
	}
	return hits, nil
}

func arxivCollapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func arxivLimitRunes(s string, max int) string {
	rs := []rune(s)
	if max > 0 && len(rs) > max {
		return string(rs[:max])
	}
	return s
}

func arxivAuthorLine(names []string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) > arxivSearchMaxAuthors {
		return strings.Join(names[:arxivSearchMaxAuthors], ", ") + " 等"
	}
	return strings.Join(names, ", ")
}

// arxivListDate is the date on the citation line. Atom updated is the version
// a reader would download; published is only the first submission, which can
// be years earlier. An unparseable updated timestamp falls back to published.
func arxivListDate(updated, published string) string {
	if date := arxivPublishedDate(updated); date != "" {
		return date
	}
	return arxivPublishedDate(published)
}

// arxivPublishedDate reduces an Atom timestamp (2005-04-01T00:00:00Z) to a
// calendar date. Anything unparseable is dropped rather than printed raw:
// a malformed date in a citation line reads as a broken build, while a missing
// one simply is not there.
func arxivPublishedDate(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return ""
}

// arxivCategoryList puts the primary subject first and keeps the cross-lists in
// feed order. The primary category is what a reader files the paper under, so
// it must not be pushed down by the order arXiv happens to emit categories in.
func arxivCategoryList(primary string, categories []arxivAtomTerm) []string {
	out := make([]string, 0, arxivSearchMaxCategories)
	seen := make(map[string]struct{}, arxivSearchMaxCategories)
	add := func(term string) {
		name := strings.TrimSpace(term)
		if name == "" || len(out) >= arxivSearchMaxCategories {
			return
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	add(primary)
	for _, category := range categories {
		add(category.Term)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func arxivSearchErrorText(err error) string {
	if err == nil {
		return "搜索失败"
	}
	msg := strings.TrimSpace(err.Error())
	what := strings.ToLower(arxivStripURL(msg))
	switch {
	case arxivLooksLikeHubLogin(what):
		return "搜索失败：需要先在主窗口登录"
	case arxivStatusLooksNotFound(what):
		return "搜索失败：arXiv 没有响应这次查询"
	case arxivStatusLooksForbidden(what):
		return "搜索失败：arXiv 拒绝了这次请求，请稍后再试"
	case strings.Contains(what, "timed out"), strings.Contains(what, "deadline exceeded"), strings.Contains(what, "context deadline"):
		return "搜索失败：连接 arXiv 超时，请稍后再试"
	}
	if msg == "" {
		return "搜索失败"
	}
	tail := strings.TrimSpace(arxivStripURL(msg))
	if tail == "" {
		return "搜索失败"
	}
	rs := []rune(tail)
	if len(rs) > 80 {
		tail = string(rs[:80]) + "…"
	}
	return "搜索失败：" + tail
}
