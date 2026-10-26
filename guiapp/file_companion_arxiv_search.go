package guiapp

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// The 搜索 button next to 确定. A keyword query hits the public arXiv API and
// comes back as titles and abstracts. Opening one result reuses
// FileCompanionOpenArxiv, so a paper is downloaded once and then read locally.
const (
	arxivSearchPageSize         = 20
	arxivSearchMaxStart         = 2000
	arxivSearchTimeoutS         = 30
	arxivExpandTimeoutS         = 30
	arxivSearchMaxQueryRunes    = 800
	arxivSearchMaxTerms         = 12
	arxivSearchMaxTermRunes     = 80
	arxivSearchMaxGroups        = 3
	arxivSearchMaxTermsPerGroup = 5
	arxivSearchMinTermsPerGroup = 2
	// A box that is already a short keyword list is searched as written.
	// Longer text, Chinese, or a sentence is the goal the model expands.
	arxivSearchDirectMaxRunes   = 80
	arxivSearchDirectMaxTerms   = 6
	arxivSearchMaxInterpreted   = 80
	arxivSearchMaxAuthors       = 8
	arxivSearchMaxTitleRunes    = 500
	arxivSearchMaxAbstractRunes = 4000
	// arxivSearchMaxCategories caps the subject chips one row carries. arXiv
	// entries routinely list four or five cross-lists, and printing them all
	// turns the citation line into a paragraph.
	arxivSearchMaxCategories = 3
)

// arxivExpandSystemPrompt asks for terms only. The model must not emit an
// arXiv search_query: a colon or the word AND in that string would change
// the field or add a boolean. Go quotes every term itself.
const arxivExpandSystemPrompt = `你是 arXiv 检索扩展器。用户会描述研究目标，或给出关键字。只输出一个 JSON 对象，不要 Markdown。
把目标拆成 1 到 3 个互补词组。每个词组是 2 到 5 个必须同时出现的英文术语，可带一个 arXiv 分类（如 cs.CL、cs.AI、math.NT、quant-ph）。
术语用英文。第一组贴近用户原词，其余组只补充同一种问题的说法，不要换成另一个题目。不要编造论文编号，不要输出检索式。
{"interpreted":"一句话说明在找什么","groups":[{"terms":["retrieval augmented generation","long document"],"category":"cs.CL"}]}`

// arxivCategoryPattern matches an arXiv subject such as cs.CL, math.NT,
// cond-mat.str-el, or quant-ph. Anything else is dropped so a model cannot
// put a boolean or a second field into the category.
var arxivCategoryPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[A-Za-z][A-Za-z0-9-]*)?$`)

var errArxivExpandUnavailable = errors.New("arxiv expand unavailable")

// errArxivAPIEntry is a feed whose entries are arXiv API errors, not papers.
// An empty feed is a real miss. An errors entry with a 200 is a rejected query.
var errArxivAPIEntry = errors.New("arxiv api error entry")

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

// FileCompanionArxivGroup is one AND-group the expansion step produced.
// Terms are already sanitized. Category is empty when the model omitted it
// or named something that is not an arXiv subject.
type FileCompanionArxivGroup struct {
	Terms    []string `json:"terms"`
	Category string   `json:"category,omitempty"`
}

// FileCompanionArxivSearchPage is one page of a companion arXiv search.
// Page is 1-based. Total is the feed's totalResults when arXiv sent one,
// otherwise a lower bound that still lets a full page turn forward.
type FileCompanionArxivSearchPage struct {
	Hits        []FileCompanionArxivHit   `json:"hits"`
	Page        int                       `json:"page"`
	PageSize    int                       `json:"pageSize"`
	Total       int                       `json:"total"`
	Interpreted string                    `json:"interpreted,omitempty"`
	Groups      []FileCompanionArxivGroup `json:"groups,omitempty"`
	Fallback    bool                      `json:"fallback,omitempty"`
}

// arxivExpansion is the JSON object the model is asked to return.
type arxivExpansion struct {
	Interpreted string                    `json:"interpreted"`
	Groups      []FileCompanionArxivGroup `json:"groups"`
}

// arxivQueryPlan is the sanitized query for one goal. searchQuery is what
// export.arxiv.org receives. fallback is set when a goal was sent to the
// model and that call could not be used. A keyword list that never asked
// the model leaves it false.
type arxivQueryPlan struct {
	interpreted string
	searchQuery string
	groups      []FileCompanionArxivGroup
	fallback    bool
	total       int
	totalKnown  bool
}

// arxivSearchSlot is the one goal remembered for the open dialog. A later
// page of the same text reuses searchQuery and does not call the model.
type arxivSearchSlot struct {
	query       string
	interpreted string
	searchQuery string
	groups      []FileCompanionArxivGroup
	fallback    bool
	total       int
	totalKnown  bool
}

type arxivAtomFeed struct {
	XMLName xml.Name         `xml:"http://www.w3.org/2005/Atom feed"`
	Total   int              `xml:"http://a9.com/-/spec/opensearch/1.1/ totalResults"`
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

// FileCompanionSearchArxiv searches arXiv for a research goal or a keyword
// list and returns one page of 20 papers. Page is 1-based. An empty box is a
// user error, not a network failure.
//
// A bare arXiv id, abs link, or pdf link still uses id_list and does not call
// the model. A short keyword list is quoted and AND-ed as written, and that
// plan is cached so the next page does not suddenly ask the model. A goal
// (Chinese, a sentence, or a longer list) is expanded into a few English term
// groups. The expansion is cached on the goal, so the next page does not call
// the model again. When that call is missing or its JSON is unusable, the
// typed words are searched as they are and Fallback is set.
//
// refresh is a new search from the button. A page turn passes false, including
// 上一页 back to page 1, so a failed expansion stays on that result set. A new
// search retries a failed expansion and reuses one that already worked.
func (a *App) FileCompanionSearchArxiv(query string, page int, refresh bool) (FileCompanionArxivSearchPage, error) {
	blank := FileCompanionArxivSearchPage{Page: 1, PageSize: arxivSearchPageSize, Hits: []FileCompanionArxivHit{}}
	raw := strings.TrimSpace(query)
	if raw == "" {
		return blank, errors.New("请描述研究目标或输入关键字")
	}
	if rs := []rune(raw); len(rs) > arxivSearchMaxQueryRunes {
		raw = string(rs[:arxivSearchMaxQueryRunes])
	}
	page, start := arxivSearchPageStart(page)
	if ids, ok := arxivSearchIDsOnly(arxivSearchTokens(raw)); ok {
		return a.arxivSearchIDs(ids, page, start)
	}
	plan, cached := a.arxivCachedPlan(raw)
	// Page number 1 is both "new search" and "上一页". Only refresh retries a
	// failed expansion; a page turn keeps the query the other pages used.
	reuse := cached && !(refresh && plan.fallback)
	expand := !reuse && arxivGoalNeedsExpansion(raw)
	parent := context.Background()
	if a != nil && a.ctx != nil {
		parent = a.ctx
	}
	budget := time.Duration(arxivSearchTimeoutS) * time.Second
	if expand {
		budget += time.Duration(arxivExpandTimeoutS) * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, budget+arxivDownloadSlack)
	defer cancel()
	if !reuse {
		if expand {
			plan = a.arxivPlan(ctx, raw)
		} else {
			plan = arxivDirectPlan(raw)
		}
		if plan.searchQuery == "" {
			return blank, errors.New("请描述研究目标或输入关键字")
		}
		a.arxivStorePlan(raw, plan)
	}
	if plan.totalKnown && start >= plan.total {
		return arxivPageFromPlan(plan, page, nil, plan.total), nil
	}
	searchURL, err := arxivAPIURL(plan.searchQuery, "", start, arxivSearchPageSize, true)
	if err != nil {
		return blank, err
	}
	hits, reported, full, err := a.arxivFetchFeed(ctx, searchURL)
	if err != nil {
		return blank, err
	}
	total, known := arxivFinishPageTotal(reported, start, len(hits), full)
	// A full API window that omits totalResults only has a lower bound one
	// past that window. The window is full when arXiv sent a page of
	// entries, even if a duplicate or an error entry is not shown as a row.
	// Counting shown rows instead would end the search one duplicate early.
	// A cached totalResults is a ceiling: when it still covers the window,
	// the lower bound's +1 must not open a page past an exact multiple.
	// When the rows run past it, the ceiling is false and the next page has
	// to be fetched. A short window stays a lower bound of the rows in hand,
	// so a truncated feed cannot pin the end.
	if !known && plan.totalKnown && full {
		if plan.total >= start+len(hits) && plan.total >= start+arxivSearchPageSize {
			total = plan.total
			known = true
		} else {
			a.arxivForgetTotal(raw)
		}
	}
	if known {
		a.arxivStoreTotal(raw, total)
	}
	logFileCompanion("arxiv search hits=%d page=%d fallback=%t", len(hits), page, plan.fallback)
	return arxivPageFromPlan(plan, page, hits, total), nil
}

func arxivPageFromPlan(plan arxivQueryPlan, page int, hits []FileCompanionArxivHit, total int) FileCompanionArxivSearchPage {
	if hits == nil {
		hits = []FileCompanionArxivHit{}
	}
	groups := plan.groups
	if groups == nil {
		groups = []FileCompanionArxivGroup{}
	}
	return FileCompanionArxivSearchPage{
		Hits:        hits,
		Page:        page,
		PageSize:    arxivSearchPageSize,
		Total:       total,
		Interpreted: plan.interpreted,
		Groups:      groups,
		Fallback:    plan.fallback,
	}
}

func (a *App) arxivSearchIDs(ids []string, page, start int) (FileCompanionArxivSearchPage, error) {
	blank := FileCompanionArxivSearchPage{Page: 1, PageSize: arxivSearchPageSize, Hits: []FileCompanionArxivHit{}}
	total := len(ids)
	out := FileCompanionArxivSearchPage{
		Hits:     []FileCompanionArxivHit{},
		Page:     page,
		PageSize: arxivSearchPageSize,
		Total:    total,
	}
	// An id list is one page: the tokenizer already stops at twelve ids.
	if page > 1 || start >= total {
		return out, nil
	}
	searchURL, err := arxivAPIURL("", strings.Join(ids, ","), 0, total, false)
	if err != nil {
		return blank, err
	}
	parent := context.Background()
	if a != nil && a.ctx != nil {
		parent = a.ctx
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(arxivSearchTimeoutS)*time.Second+arxivDownloadSlack)
	defer cancel()
	hits, _, _, err := a.arxivFetchFeed(ctx, searchURL)
	if err != nil {
		return blank, err
	}
	if hits == nil {
		hits = []FileCompanionArxivHit{}
	}
	// An id that arXiv rejects comes back as an error entry and is not a row.
	// Counting the request instead of the rows tells the reader a paper is
	// missing from a one-page list they cannot turn.
	out.Hits = hits
	out.Total = len(hits)
	logFileCompanion("arxiv search hits=%d page=%d", len(hits), page)
	return out, nil
}

func (a *App) arxivFetchFeed(ctx context.Context, searchURL string) ([]FileCompanionArxivHit, int, bool, error) {
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
		return nil, 0, false, errors.New(arxivSearchErrorText(err))
	}
	body := ""
	if result != nil {
		body = result.Content
	}
	hits, total, entries, err := arxivParseFeedPage(body)
	if err != nil {
		logFileCompanion("arxiv search failed")
		if errors.Is(err, errArxivAPIEntry) {
			return nil, 0, false, errors.New("搜索失败：arXiv 拒绝了这次检索")
		}
		return nil, 0, false, errors.New("搜索失败：arXiv 没有返回论文列表")
	}
	return hits, total, entries >= arxivSearchPageSize, nil
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
	if ids, ok := arxivSearchIDsOnly(tokens); ok {
		return arxivAPIURL("", strings.Join(ids, ","), 0, len(ids), false)
	}
	return arxivAPIURL(arxivAndQuery(tokens), "", 0, arxivSearchPageSize, true)
}

// arxivAPIURL builds one Atom request. sort is only valid beside search_query.
// Sending it with id_list makes the API answer with an error entry.
func arxivAPIURL(searchQuery, idList string, start, maxResults int, sort bool) (string, error) {
	u, err := url.Parse(arxivAPIBaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("搜索失败")
	}
	if searchQuery == "" && idList == "" {
		return "", errors.New("请输入关键字")
	}
	if start < 0 {
		start = 0
	}
	if maxResults < 1 {
		maxResults = arxivSearchPageSize
	}
	params := u.Query()
	params.Set("start", strconv.Itoa(start))
	params.Set("max_results", strconv.Itoa(maxResults))
	if idList != "" {
		params.Set("id_list", idList)
	} else {
		params.Set("search_query", searchQuery)
		if sort {
			params.Set("sortBy", "relevance")
			params.Set("sortOrder", "descending")
		}
	}
	u.RawQuery = params.Encode()
	return u.String(), nil
}

func arxivSearchPageStart(page int) (int, int) {
	if page < 1 {
		page = 1
	}
	start := (page - 1) * arxivSearchPageSize
	if start > arxivSearchMaxStart {
		start = arxivSearchMaxStart
		page = start/arxivSearchPageSize + 1
	}
	return page, start
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

// arxivSearchIDsOnly reports the canonical ids when every topical term is an
// arXiv reference. Citation furniture copied off an abstract page — "Cite as:",
// a bracketed subject, a year — is not a topic, so it must not push the paper
// into all:"" (that field does not look up the identifier) or into a model
// expansion (the colon in "arXiv:" looks like sentence punctuation). One
// ordinary word still makes the whole query a keyword search, so
// "attention 2510.16079" is not silently reduced to the id.
func arxivSearchIDsOnly(tokens []string) ([]string, bool) {
	if len(tokens) == 0 {
		return nil, false
	}
	ids := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		id := arxivParseCandidate(arxivNormalizeInput(token))
		if id != "" {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
			continue
		}
		if arxivCitationNoise(token) {
			continue
		}
		return nil, false
	}
	if len(ids) == 0 {
		return nil, false
	}
	return ids, true
}

// arxivCitationArchives are the subjects arXiv prints in a citation with no
// subclass, such as [quant-ph]. A dotted subject (cs.CL, cond-mat.str-el) uses
// the same archives. A hyphenated word such as gpt-2 is not one of them.
var arxivCitationArchives = map[string]struct{}{
	"astro-ph": {}, "cond-mat": {}, "cs": {}, "econ": {}, "eess": {},
	"gr-qc": {}, "hep-ex": {}, "hep-lat": {}, "hep-ph": {}, "hep-th": {},
	"math": {}, "math-ph": {}, "nlin": {}, "nucl-ex": {}, "nucl-th": {},
	"physics": {}, "q-bio": {}, "q-fin": {}, "quant-ph": {}, "stat": {},
}

// arxivCitationNoise is text that rides along with an identifier and does not
// name a topic. It is ignored only while deciding that the box is an id list.
// A keyword query still searches these words when a topical word is present.
func arxivCitationNoise(token string) bool {
	if arxivCitationCategory(token) || arxivCitationYear(token) {
		return true
	}
	switch strings.ToLower(token) {
	case "cite", "as", "cited", "see", "above", "arxiv":
		return true
	}
	switch token {
	case "详见", "见", "参考":
		return true
	}
	return false
}

func arxivCitationCategory(token string) bool {
	archive, _, _ := strings.Cut(token, ".")
	if _, ok := arxivCitationArchives[archive]; !ok {
		return false
	}
	return arxivCategoryPattern.MatchString(token)
}

func arxivCitationYear(token string) bool {
	if len(token) != 4 {
		return false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return false
		}
	}
	return (token[0] == '1' && token[1] == '9') || (token[0] == '2' && token[1] == '0')
}

// arxivSearchEdgeQuotes wrap a term the way a citation or an IME does.
// Parentheses are not in this set: Na(OH) is a real query, and a trailing
// ")" is only removed when the whole term is one wrapping pair.
const arxivSearchEdgeQuotes = "“”‘’「」『』《》«»"

// arxivSearchTrailChars stick to the last word of a keyword list. A period
// is not an expansion trigger, so "state of the art." is searched as written
// — and the period has to come off, or the last term never matches. Brackets
// are absent on purpose, so a formula keeps its closing parenthesis.
const arxivSearchTrailChars = ".,;:!?．。、，；：！？…"

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
	token = arxivPolishSearchToken(token)
	rs := []rune(token)
	if len(rs) > arxivSearchMaxTermRunes {
		token = string(rs[:arxivSearchMaxTermRunes])
	}
	return strings.TrimSpace(token)
}

// arxivPolishSearchToken drops citation punctuation that is not part of the
// word. The loop peels one layer at a time so "(attention)." becomes
// attention, while Na(OH) is left alone: it is not wrapped as a whole.
func arxivPolishSearchToken(token string) string {
	for {
		next := strings.TrimSpace(token)
		next = strings.Trim(next, arxivSearchEdgeQuotes)
		next = strings.TrimRight(next, arxivSearchTrailChars)
		next = strings.TrimSpace(arxivStripWrappingPair(next))
		if next == token {
			return token
		}
		token = next
	}
}

// arxivStripWrappingPair removes one pair of brackets when they enclose the
// whole term. A closer that belongs to a formula (Na(OH)) is not a wrapper,
// because the term does not start with the matching opener.
func arxivStripWrappingPair(token string) string {
	rs := []rune(token)
	if len(rs) < 2 {
		return token
	}
	pairs := [][2]rune{{'(', ')'}, {'（', '）'}, {'[', ']'}, {'［', '］'}}
	for _, pair := range pairs {
		if rs[0] != pair[0] || rs[len(rs)-1] != pair[1] {
			continue
		}
		depth := 0
		wraps := true
		for i, r := range rs {
			switch r {
			case pair[0]:
				depth++
			case pair[1]:
				depth--
				if depth == 0 && i != len(rs)-1 {
					wraps = false
				}
			}
			if !wraps || depth < 0 {
				wraps = false
				break
			}
		}
		if wraps && depth == 0 {
			return strings.TrimSpace(string(rs[1 : len(rs)-1]))
		}
	}
	return token
}

func arxivParseFeed(body string) ([]FileCompanionArxivHit, error) {
	hits, _, _, err := arxivParseFeedPage(body)
	return hits, err
}

func arxivParseFeedPage(body string) ([]FileCompanionArxivHit, int, int, error) {
	body = strings.TrimSpace(strings.TrimPrefix(body, "\uFEFF"))
	if body == "" {
		return nil, 0, 0, errors.New("empty feed")
	}
	var feed arxivAtomFeed
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		return nil, 0, 0, err
	}
	if feed.XMLName.Local != "feed" {
		return nil, 0, 0, errors.New("not a feed")
	}
	hits := make([]FileCompanionArxivHit, 0, len(feed.Entries))
	seen := make(map[string]struct{}, len(feed.Entries))
	apiErrors := 0
	for _, entry := range feed.Entries {
		// http://arxiv.org/api/errors#… is how a 200 body reports a rejected
		// id or query. It is not a paper with an empty id.
		if arxivAtomIDIsAPIError(entry.ID) {
			apiErrors++
			continue
		}
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
		if len(hits) >= arxivSearchPageSize {
			break
		}
	}
	if len(hits) == 0 && apiErrors > 0 {
		return nil, 0, 0, errArxivAPIEntry
	}
	return hits, feed.Total, len(feed.Entries), nil
}

func arxivAtomIDIsAPIError(id string) bool {
	return strings.Contains(strings.ToLower(id), "/api/errors")
}

// arxivResolveTotal prefers a feed total that covers this window. full means
// arXiv sent a whole page of entries. A duplicate or an error entry can make
// the shown rows shorter than that page; the window is still not the end.
// A missing total on a full window is a lower bound one past the window, so
// the next page stays available. A short window ends at the rows in hand.
func arxivResolveTotal(reported, start, hitCount int, full bool) (int, bool) {
	if hitCount >= arxivSearchPageSize {
		full = true
	}
	if reported > 0 && reported >= start+hitCount && (!full || reported >= start+arxivSearchPageSize) {
		return reported, true
	}
	if full {
		return start + arxivSearchPageSize + 1, false
	}
	return start + hitCount, false
}

// arxivFinishPageTotal applies arxivResolveTotal, then stops the last slice
// from advertising another page. start cannot pass arxivSearchMaxStart, so a
// total past that slice would enable 下一页 and the next request would clamp
// back onto the same papers.
func arxivFinishPageTotal(reported, start, hitCount int, full bool) (int, bool) {
	total, known := arxivResolveTotal(reported, start, hitCount, full)
	if start >= arxivSearchMaxStart && total > start+hitCount {
		return start + hitCount, true
	}
	return total, known
}

func arxivAndQuery(tokens []string) string {
	parts := make([]string, len(tokens))
	for i, token := range tokens {
		parts[i] = `all:"` + token + `"`
	}
	return strings.Join(parts, " AND ")
}

// arxivSearchQueryFromGroups quotes every term. One plain group stays an AND
// list, which is what a keyword box used to send. A category or a second
// group is parenthesized so OR cannot outrank AND.
func arxivSearchQueryFromGroups(groups []FileCompanionArxivGroup) string {
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		clause := arxivAndQuery(group.Terms)
		if group.Category != "" {
			if clause != "" {
				clause += " AND "
			}
			clause += "cat:" + group.Category
		}
		if clause == "" {
			continue
		}
		if len(groups) > 1 || group.Category != "" {
			clause = "(" + clause + ")"
		}
		parts = append(parts, clause)
	}
	return strings.Join(parts, " OR ")
}

func (a *App) arxivCachedPlan(raw string) (arxivQueryPlan, bool) {
	if a == nil {
		return arxivQueryPlan{}, false
	}
	a.arxivSearchMu.Lock()
	defer a.arxivSearchMu.Unlock()
	slot := a.arxivSearchSlot
	if slot.query != raw || slot.searchQuery == "" {
		return arxivQueryPlan{}, false
	}
	return slot.plan(), true
}

func (s arxivSearchSlot) plan() arxivQueryPlan {
	return arxivQueryPlan{
		interpreted: s.interpreted,
		searchQuery: s.searchQuery,
		groups:      append([]FileCompanionArxivGroup(nil), s.groups...),
		fallback:    s.fallback,
		total:       s.total,
		totalKnown:  s.totalKnown,
	}
}

func (a *App) arxivStorePlan(raw string, plan arxivQueryPlan) {
	if a == nil {
		return
	}
	a.arxivSearchMu.Lock()
	defer a.arxivSearchMu.Unlock()
	a.arxivSearchSlot = arxivSearchSlot{
		query:       raw,
		interpreted: plan.interpreted,
		searchQuery: plan.searchQuery,
		groups:      append([]FileCompanionArxivGroup(nil), plan.groups...),
		fallback:    plan.fallback,
		total:       plan.total,
		totalKnown:  plan.totalKnown,
	}
}

func (a *App) arxivStoreTotal(raw string, total int) {
	if a == nil || total <= 0 {
		return
	}
	a.arxivSearchMu.Lock()
	defer a.arxivSearchMu.Unlock()
	if a.arxivSearchSlot.query != raw {
		return
	}
	a.arxivSearchSlot.total = total
	a.arxivSearchSlot.totalKnown = true
}

// arxivForgetTotal drops a ceiling the current page has already disproved.
// Leaving it in place makes the next start skip the request and come back empty.
func (a *App) arxivForgetTotal(raw string) {
	if a == nil {
		return
	}
	a.arxivSearchMu.Lock()
	defer a.arxivSearchMu.Unlock()
	if a.arxivSearchSlot.query != raw {
		return
	}
	a.arxivSearchSlot.totalKnown = false
}

func (a *App) arxivPlan(ctx context.Context, raw string) arxivQueryPlan {
	expanded, err := a.arxivExpandGoal(ctx, raw)
	if err == nil {
		if plan, ok := arxivPlanFromExpansion(expanded); ok {
			return plan
		}
	}
	tried := a != nil && (a.arxivExpand != nil || a.imHandler != nil)
	if tried {
		logFileCompanion("arxiv expand failed, using typed words")
	}
	return arxivFallbackPlan(raw)
}

func arxivFallbackPlan(raw string) arxivQueryPlan {
	tokens := arxivSearchTokens(raw)
	if len(tokens) == 0 {
		return arxivQueryPlan{}
	}
	groups := []FileCompanionArxivGroup{{Terms: append([]string(nil), tokens...)}}
	return arxivQueryPlan{
		searchQuery: arxivSearchQueryFromGroups(groups),
		groups:      groups,
		fallback:    true,
	}
}

// arxivGoalNeedsExpansion is true when the box is a research goal rather than
// a keyword list. Chinese (and the other CJK scripts a reader would type),
// sentence punctuation, a long list, or a long phrase all need English terms.
// A short ASCII keyword list does not: asking the model and then falling
// back to the same AND query only adds a wait and a false "未能扩展" line.
func arxivGoalNeedsExpansion(raw string) bool {
	if len([]rune(raw)) > arxivSearchDirectMaxRunes {
		return true
	}
	if len(arxivSearchTokens(raw)) > arxivSearchDirectMaxTerms {
		return true
	}
	for _, r := range raw {
		if strings.ContainsRune("。！？!?；;：:", r) || arxivIsCJK(r) {
			return true
		}
	}
	return false
}

func arxivIsCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// arxivDirectPlan searches the typed words. Groups stay empty so the dialog
// does not chip a copy of the box, and fallback stays false because the
// model was not asked.
func arxivDirectPlan(raw string) arxivQueryPlan {
	tokens := arxivSearchTokens(raw)
	if len(tokens) == 0 {
		return arxivQueryPlan{}
	}
	return arxivQueryPlan{searchQuery: arxivAndQuery(tokens)}
}

func (a *App) arxivExpandGoal(ctx context.Context, goal string) (arxivExpansion, error) {
	if a != nil && a.arxivExpand != nil {
		return a.arxivExpand(ctx, goal)
	}
	if a == nil || a.imHandler == nil {
		return arxivExpansion{}, errArxivExpandUnavailable
	}
	res, err := a.imHandler.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt:      arxivExpandSystemPrompt,
		UserMessage:       goal,
		TimeoutSec:        arxivExpandTimeoutS,
		Tag:               "arxiv-expand",
		PreferLightweight: true,
		NoThinking:        true,
		MaxOutputTokens:   700,
		Role:              maclawLLMRoleBackground,
	})
	if err != nil {
		return arxivExpansion{}, err
	}
	if res == nil {
		return arxivExpansion{}, errArxivExpandUnavailable
	}
	return parseArxivExpansion(res.Text)
}

func parseArxivExpansion(text string) (arxivExpansion, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return arxivExpansion{}, errors.New("no json")
	}
	var expanded arxivExpansion
	if err := json.Unmarshal([]byte(text[start:end+1]), &expanded); err != nil {
		return arxivExpansion{}, err
	}
	return expanded, nil
}

func arxivPlanFromExpansion(expanded arxivExpansion) (arxivQueryPlan, bool) {
	groups := make([]FileCompanionArxivGroup, 0, len(expanded.Groups))
	for _, group := range expanded.Groups {
		if len(groups) >= arxivSearchMaxGroups {
			break
		}
		terms := make([]string, 0, len(group.Terms))
		seen := make(map[string]struct{}, len(group.Terms))
		for _, term := range group.Terms {
			token := arxivSearchToken(term)
			if token == "" {
				continue
			}
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			terms = append(terms, token)
			if len(terms) >= arxivSearchMaxTermsPerGroup {
				break
			}
		}
		if len(terms) < arxivSearchMinTermsPerGroup {
			continue
		}
		category := ""
		if arxivCategoryPattern.MatchString(strings.TrimSpace(group.Category)) {
			category = strings.TrimSpace(group.Category)
		}
		groups = append(groups, FileCompanionArxivGroup{Terms: terms, Category: category})
	}
	if len(groups) == 0 {
		return arxivQueryPlan{}, false
	}
	return arxivQueryPlan{
		interpreted: arxivLimitRunes(arxivCollapseSpace(expanded.Interpreted), arxivSearchMaxInterpreted),
		searchQuery: arxivSearchQueryFromGroups(groups),
		groups:      groups,
		fallback:    false,
	}, true
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
