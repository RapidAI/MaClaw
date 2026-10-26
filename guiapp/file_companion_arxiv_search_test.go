package guiapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestArxivSearchURLQuotesEachTerm(t *testing.T) {
	got, err := arxivSearchURL(`transformer attention ti:secret "OR"`)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "export.arxiv.org" || u.Path != "/api/query" {
		t.Fatalf("url=%s", got)
	}
	q := u.Query()
	// The colon and the word OR stay inside quotes, so they are search text
	// rather than an arXiv field or a boolean operator.
	if q.Get("search_query") != `all:"transformer" AND all:"attention" AND all:"ti:secret" AND all:"OR"` {
		t.Fatalf("search_query=%q", q.Get("search_query"))
	}
	if q.Get("max_results") != "20" || q.Get("sortBy") != "relevance" || q.Get("start") != "0" || q.Get("id_list") != "" {
		t.Fatalf("params=%v", q)
	}
}

func TestArxivSearchURLUsesIDListForIdentifiers(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"2510.16079", "2510.16079"},
		{"https://arxiv.org/abs/2510.16079v2", "2510.16079"},
		{"https://arxiv.org/pdf/2510.16079.pdf", "2510.16079"},
		{"arXiv:cs/0701001", "cs/0701001"},
		{"2510.16079,1706.03762", "2510.16079,1706.03762"},
		{"2510.16079，1706.03762", "2510.16079,1706.03762"},
		{"2510.16079 2510.16079v2", "2510.16079"},
		{"cs/0701001 math/0701001", "cs/0701001,math/0701001"},
		// The line arXiv prints on every abstract page. The colon must not send
		// it to the model, and the subject in brackets is not a keyword.
		{"arXiv:2510.16079 [cs.CL]", "2510.16079"},
		{"Cite as: arXiv:2510.16079 [cs.CL]", "2510.16079"},
		{"arXiv:2510.16079v2 [quant-ph]", "2510.16079"},
		{"2510.16079 (2025)", "2510.16079"},
		{"详见 arXiv:2510.16079。", "2510.16079"},
		{"cited 2510.16079, see above", "2510.16079"},
	}
	for _, c := range cases {
		got, err := arxivSearchURL(c.in)
		if err != nil {
			t.Fatalf("arxivSearchURL(%q) err=%v", c.in, err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if q.Get("id_list") != c.want || q.Get("search_query") != "" || q.Get("sortBy") != "" || q.Get("sortOrder") != "" {
			t.Fatalf("arxivSearchURL(%q) params=%v", c.in, q)
		}
		if q.Get("max_results") != strconv.Itoa(strings.Count(c.want, ",")+1) {
			t.Fatalf("arxivSearchURL(%q) max_results=%q", c.in, q.Get("max_results"))
		}
	}
	// A keyword next to an id stays a keyword query. Treating the whole box
	// as an id list would drop the word the user typed.
	got, err := arxivSearchURL("attention 2510.16079")
	if err != nil {
		t.Fatal(err)
	}
	q := mustQuery(t, got)
	if q.Get("search_query") != `all:"attention" AND all:"2510.16079"` || q.Get("id_list") != "" || q.Get("sortBy") != "relevance" {
		t.Fatalf("mixed params=%v", q)
	}
	// A hyphenated word is not an arXiv archive. quant-ph is; gpt-2 is not.
	got, err = arxivSearchURL("gpt-2 2510.16079")
	if err != nil {
		t.Fatal(err)
	}
	q = mustQuery(t, got)
	if q.Get("search_query") != `all:"gpt-2" AND all:"2510.16079"` || q.Get("id_list") != "" {
		t.Fatalf("gpt-2 params=%v", q)
	}
	got, err = arxivSearchURL("cs.CL")
	if err != nil {
		t.Fatal(err)
	}
	q = mustQuery(t, got)
	if q.Get("search_query") != `all:"cs.CL"` || q.Get("id_list") != "" {
		t.Fatalf("bare category params=%v", q)
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestArxivSearchErrorText(t *testing.T) {
	if got := arxivSearchErrorText(nil); got != "搜索失败" {
		t.Fatalf("nil=%q", got)
	}
	cases := map[string]string{
		"MaClaw Hub download requires a signed-in hub account": "需要先在主窗口登录",
		"MaClaw Hub search requires a signed-in hub account":   "需要先在主窗口登录",
		"context deadline exceeded":                            "超时",
		"unexpected status code 404":                           "没有响应",
		`Get "https://export.arxiv.org/api/query?search_query=all%3A%222510.16079%22": dial tcp: lookup export.arxiv.org: no such host`: "dial tcp",
	}
	for in, want := range cases {
		got := arxivSearchErrorText(errString(in))
		if !strings.Contains(got, want) {
			t.Fatalf("arxivSearchErrorText(%q)=%q want %q", in, got, want)
		}
		if strings.Contains(got, "signed-in") || strings.Contains(got, "http://") || strings.Contains(got, "https://") {
			t.Fatalf("arxivSearchErrorText(%q)=%q still exposes the transport", in, got)
		}
	}
}

func TestArxivSearchURLKeepsCommaInsideAKeyword(t *testing.T) {
	got, err := arxivSearchURL("N,N-dimethyl transformer")
	if err != nil {
		t.Fatal(err)
	}
	q := mustQuery(t, got)
	if q.Get("search_query") != `all:"N,N-dimethyl" AND all:"transformer"` || q.Get("id_list") != "" {
		t.Fatalf("chemical params=%v", q)
	}
	got, err = arxivSearchURL("Smith, John")
	if err != nil {
		t.Fatal(err)
	}
	q = mustQuery(t, got)
	if q.Get("search_query") != `all:"Smith" AND all:"John"` || q.Get("id_list") != "" {
		t.Fatalf("author params=%v", q)
	}
}

func TestArxivSearchURLDropsCitationPunctuationButKeepsAFormula(t *testing.T) {
	// A period does not ask the model to expand, so it must not stay glued
	// to the last word either. Otherwise all:"art." matches nothing.
	if arxivGoalNeedsExpansion("state of the art.") {
		t.Fatal("a trailing period is not a research-goal sentence")
	}
	got, err := arxivSearchURL("state of the art.")
	if err != nil {
		t.Fatal(err)
	}
	q := mustQuery(t, got)
	if q.Get("search_query") != `all:"state" AND all:"of" AND all:"the" AND all:"art"` || q.Get("id_list") != "" {
		t.Fatalf("period params=%v", q)
	}
	got, err = arxivSearchURL("“transformer”")
	if err != nil {
		t.Fatal(err)
	}
	q = mustQuery(t, got)
	if q.Get("search_query") != `all:"transformer"` {
		t.Fatalf("quotes params=%v", q)
	}
	got, err = arxivSearchURL("(transformer).")
	if err != nil {
		t.Fatal(err)
	}
	q = mustQuery(t, got)
	if q.Get("search_query") != `all:"transformer"` {
		t.Fatalf("wrapped params=%v", q)
	}
	got, err = arxivSearchURL("Na(OH).")
	if err != nil {
		t.Fatal(err)
	}
	q = mustQuery(t, got)
	if q.Get("search_query") != `all:"Na(OH)"` {
		t.Fatalf("formula params=%v", q)
	}
	// The period is not part of the identifier. It still selects that paper.
	got, err = arxivSearchURL("2510.16079.")
	if err != nil {
		t.Fatal(err)
	}
	q = mustQuery(t, got)
	if q.Get("id_list") != "2510.16079" || q.Get("search_query") != "" {
		t.Fatalf("id params=%v", q)
	}
}

func TestArxivSearchURLRejectsBlankTerms(t *testing.T) {
	for _, in := range []string{"", "   ", `""`, `" \ "`} {
		if _, err := arxivSearchURL(in); err == nil || !strings.Contains(err.Error(), "请输入关键字") {
			t.Fatalf("arxivSearchURL(%q) err=%v", in, err)
		}
	}
	got, err := arxivSearchURL("注意力")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("search_query") != `all:"注意力"` {
		t.Fatalf("search_query=%q", u.Query().Get("search_query"))
	}
}

func TestArxivParseFeedKeepsTitleAbstractAndCanonicalID(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/" xmlns:arxiv="http://arxiv.org/schemas/atom">
  <title>ArXiv Query</title>
  <id>http://arxiv.org/api/query</id>
  <entry>
    <id>http://arxiv.org/abs/2510.16079v2</id>
    <title>  A Paper About
      Transformers  </title>
    <summary>  This abstract
      explains the method.  </summary>
    <published>2025-10-20T17:31:04Z</published>
    <updated>2026-03-04T12:00:00Z</updated>
    <author><name>Ada Lovelace</name></author>
    <author><name> Alan Turing </name></author>
    <link href="http://arxiv.org/pdf/2510.16079v2" rel="related" type="application/pdf"/>
    <arxiv:primary_category xmlns:arxiv="http://arxiv.org/schemas/atom" term="cs.CL"/>
    <category term="cs.CL" scheme="http://arxiv.org/schemas/atom"/>
    <category term="cs.LG" scheme="http://arxiv.org/schemas/atom"/>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/cs/0701001v1</id>
    <title>Old Style</title>
    <summary>Old abstract.</summary>
    <published>2025-10-20T17:31:04Z</published>
    <author><name>Grace Hopper</name></author>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2510.16079v1</id>
    <title>Same paper again</title>
    <summary>duplicate</summary>
  </entry>
  <entry>
    <id>http://example.com/not-a-paper</id>
    <title>Skip me</title>
    <summary>no</summary>
  </entry>
</feed>`
	hits, err := arxivParseFeed(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits=%d %#v", len(hits), hits)
	}
	if hits[0].ID != "2510.16079" || hits[0].Title != "A Paper About Transformers" || hits[0].Abstract != "This abstract explains the method." || hits[0].Authors != "Ada Lovelace, Alan Turing" {
		t.Fatalf("first=%#v", hits[0])
	}
	// updated is the version a reader would download. published is only v1.
	if hits[0].Published != "2026-03-04" {
		t.Fatalf("published=%q", hits[0].Published)
	}
	// The primary category leads, and the cross-list follows in feed order.
	if got := strings.Join(hits[0].Categories, ","); got != "cs.CL,cs.LG" {
		t.Fatalf("categories=%q", got)
	}
	if hits[1].ID != "cs/0701001" || hits[1].Title != "Old Style" || hits[1].Abstract != "Old abstract." || hits[1].Authors != "Grace Hopper" {
		t.Fatalf("second=%#v", hits[1])
	}
	// An entry that only has the original submission date keeps that date.
	// Categories stay empty when the feed omits them.
	if hits[1].Published != "2025-10-20" || len(hits[1].Categories) != 0 {
		t.Fatalf("second metadata=%#v", hits[1])
	}
}

func TestArxivCategoryListKeepsPrimaryFirstAndCapsTheLine(t *testing.T) {
	terms := func(names ...string) []arxivAtomTerm {
		out := make([]arxivAtomTerm, len(names))
		for i, name := range names {
			out[i] = arxivAtomTerm{Term: name}
		}
		return out
	}
	// The primary subject is repeated among the categories; it must appear once,
	// at the front.
	got := arxivCategoryList("cs.CL", terms("cs.LG", "cs.CL", "cs.AI", "stat.ML"))
	if strings.Join(got, ",") != "cs.CL,cs.LG,cs.AI" {
		t.Fatalf("got=%v", got)
	}
	// A feed that only carries the cross-lists still lists the paper.
	if got := arxivCategoryList("", terms("math.GT")); len(got) != 1 || got[0] != "math.GT" {
		t.Fatalf("cross-list only=%v", got)
	}
	if got := arxivCategoryList("  ", nil); got != nil {
		t.Fatalf("no categories=%v", got)
	}
}

func TestArxivListDatePrefersTheLatestVersion(t *testing.T) {
	if got := arxivListDate("2026-03-04T12:00:00Z", "2025-10-20T17:31:04Z"); got != "2026-03-04" {
		t.Fatalf("updated wins: %q", got)
	}
	if got := arxivListDate("not-a-date", "2025-10-20T17:31:04Z"); got != "2025-10-20" {
		t.Fatalf("published fallback: %q", got)
	}
	if got := arxivListDate("  ", ""); got != "" {
		t.Fatalf("blank updated: %q", got)
	}
}

func TestArxivPublishedDateDropsWhatItCannotParse(t *testing.T) {
	cases := map[string]string{
		"2025-10-20T17:31:04Z":   "2025-10-20",
		"2025-10-20":             "2025-10-20",
		" 2025-10-20T17:31:04Z ": "2025-10-20",
		"":                       "",
		"yesterday":              "",
	}
	for in, want := range cases {
		if got := arxivPublishedDate(in); got != want {
			t.Fatalf("arxivPublishedDate(%q)=%q want %q", in, got, want)
		}
	}
}

func TestArxivParseFeedRejectsANonFeed(t *testing.T) {
	if _, err := arxivParseFeed("<html><body>no</body></html>"); err == nil {
		t.Fatal("expected a non-feed to fail")
	}
	hits, err := arxivParseFeed(`<feed xmlns="http://www.w3.org/2005/Atom"></feed>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits=%#v", hits)
	}
}

func TestFileCompanionSearchArxivListsPapers(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/2510.16079v1</id>
    <title>Listed Paper</title>
    <summary>Listed abstract.</summary>
    <author><name>A. Author</name></author>
  </entry>
</feed>`
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("search_query")
		if r.URL.Query().Get("max_results") != "20" {
			t.Errorf("max_results=%q", r.URL.Query().Get("max_results"))
		}
		w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	page, err := (&App{}).FileCompanionSearchArxiv("listed paper", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != `all:"listed" AND all:"paper"` {
		t.Fatalf("query=%q", gotQuery)
	}
	hits := page.Hits
	if len(hits) != 1 || hits[0].ID != "2510.16079" || hits[0].Title != "Listed Paper" || hits[0].Abstract != "Listed abstract." {
		t.Fatalf("hits=%#v", hits)
	}
	if page.Fallback || page.Page != 1 || page.PageSize != 20 || page.Total != 1 || len(page.Groups) != 0 {
		t.Fatalf("page=%#v", page)
	}
}

func TestFileCompanionSearchArxivRejectsBlankWithoutDialing(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	for _, in := range []string{"", "   ", `"`} {
		_, err := (&App{}).FileCompanionSearchArxiv(in, 1, false)
		if err == nil || !strings.Contains(err.Error(), "请描述研究目标或输入关键字") {
			t.Fatalf("input %q err=%v", in, err)
		}
	}
	if hits != 0 {
		t.Fatalf("blank queries dialed the server %d times", hits)
	}
}

func TestFileCompanionSearchArxivReportsABadFeedAndHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("search_query"), "missing") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>not a feed</body></html>"))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	_, err := (&App{}).FileCompanionSearchArxiv("not a feed", 1, false)
	if err == nil || !strings.Contains(err.Error(), "没有返回论文列表") {
		t.Fatalf("bad feed err=%v", err)
	}
	_, err = (&App{}).FileCompanionSearchArxiv("missing", 1, false)
	if err == nil || !strings.Contains(err.Error(), "搜索失败") {
		t.Fatalf("status err=%v", err)
	}
}

func TestFileCompanionSearchArxivFetchesAnIDList(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/2510.16079v2</id>
    <title>The Paper Itself</title>
    <summary>Not a citation of the number.</summary>
  </entry>
</feed>`
	var idList, searchQuery, sortBy string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idList = r.URL.Query().Get("id_list")
		searchQuery = r.URL.Query().Get("search_query")
		sortBy = r.URL.Query().Get("sortBy")
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	page, err := (&App{}).FileCompanionSearchArxiv("https://arxiv.org/abs/2510.16079v2", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if idList != "2510.16079" || searchQuery != "" || sortBy != "" {
		t.Fatalf("id_list=%q search_query=%q sortBy=%q", idList, searchQuery, sortBy)
	}
	hits := page.Hits
	if len(hits) != 1 || hits[0].ID != "2510.16079" || hits[0].Title != "The Paper Itself" {
		t.Fatalf("hits=%#v", hits)
	}
	if page.Fallback || len(page.Groups) != 0 || page.Total != 1 {
		t.Fatalf("page=%#v", page)
	}
}

func TestFileCompanionSearchArxivIDListTotalCountsPapersNotRejectedIDs(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/api/errors#incorrect_id_format_for_0000.0000</id>
    <title>Error</title>
    <summary>incorrect id format for 0000.0000</summary>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2510.16079v1</id>
    <title>Listed Paper</title>
    <summary>Listed abstract.</summary>
  </entry>
</feed>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id_list") == "" || r.URL.Query().Get("search_query") != "" {
			t.Errorf("params=%v", r.URL.Query())
		}
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	page, err := (&App{}).FileCompanionSearchArxiv("2510.16079,0000.0000", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Hits) != 1 || page.Hits[0].ID != "2510.16079" || page.Total != 1 {
		t.Fatalf("page=%#v", page)
	}
}

func TestFileCompanionSearchArxivReportsAnAPIErrorEntry(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/api/errors#incorrect_id_format_for_0000.0000</id>
    <title>Error</title>
    <summary>incorrect id format for 0000.0000</summary>
  </entry>
</feed>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	_, err := (&App{}).FileCompanionSearchArxiv("listed paper", 1, false)
	if err == nil || !strings.Contains(err.Error(), "拒绝了这次检索") {
		t.Fatalf("err=%v", err)
	}
}

func TestArxivParseFeedPageKeepsAPaperBesideAnAPIError(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/api/errors#bad</id>
    <title>Error</title>
    <summary>bad</summary>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2510.16079v1</id>
    <title>Listed Paper</title>
    <summary>Listed abstract.</summary>
  </entry>
</feed>`
	hits, _, _, err := arxivParseFeedPage(feed)
	if err != nil || len(hits) != 1 || hits[0].ID != "2510.16079" {
		t.Fatalf("hits=%#v err=%v", hits, err)
	}
}

func TestArxivSearchPageStartClamps(t *testing.T) {
	page, start := arxivSearchPageStart(0)
	if page != 1 || start != 0 {
		t.Fatalf("page=%d start=%d", page, start)
	}
	page, start = arxivSearchPageStart(500)
	if start != arxivSearchMaxStart || page != arxivSearchMaxStart/arxivSearchPageSize+1 {
		t.Fatalf("page=%d start=%d", page, start)
	}
}

func TestParseArxivExpansionReadsFencedJSON(t *testing.T) {
	got, err := parseArxivExpansion("sure\n```json\n{\"interpreted\":\"长文档问答\",\"groups\":[{\"terms\":[\"retrieval\",\"document\"],\"category\":\"cs.CL\"}]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	plan, ok := arxivPlanFromExpansion(got)
	if !ok || plan.fallback || plan.interpreted != "长文档问答" {
		t.Fatalf("plan=%#v ok=%v", plan, ok)
	}
	if plan.searchQuery != `(all:"retrieval" AND all:"document" AND cat:cs.CL)` {
		t.Fatalf("query=%q", plan.searchQuery)
	}
	if _, err := parseArxivExpansion("not json"); err == nil {
		t.Fatal("expected prose to fail")
	}
}

func TestArxivPlanFromExpansionDropsShortGroupsAndBadCategories(t *testing.T) {
	plan, ok := arxivPlanFromExpansion(arxivExpansion{
		Interpreted: "attention",
		Groups: []FileCompanionArxivGroup{
			{Terms: []string{"only-one"}, Category: "cs.CL"},
			{Terms: []string{"self-attention", "sequence", `ti:secret`, `"OR"`}, Category: "not a category"},
			{Terms: []string{"a", "b"}, Category: "cs.LG"},
			{Terms: []string{"c", "d"}, Category: "math.NT"},
		},
	})
	if !ok || plan.fallback || len(plan.groups) != 3 {
		t.Fatalf("plan=%#v ok=%v", plan, ok)
	}
	if plan.groups[0].Category != "" || strings.Join(plan.groups[0].Terms, ",") != `self-attention,sequence,ti:secret,OR` {
		t.Fatalf("first=%#v", plan.groups[0])
	}
	if plan.groups[1].Category != "cs.LG" || plan.groups[2].Category != "math.NT" {
		t.Fatalf("groups=%#v", plan.groups)
	}
	// The fourth group is past the cap of three.
	want := `(all:"self-attention" AND all:"sequence" AND all:"ti:secret" AND all:"OR") OR (all:"a" AND all:"b" AND cat:cs.LG) OR (all:"c" AND all:"d" AND cat:math.NT)`
	if plan.searchQuery != want {
		t.Fatalf("query=%q", plan.searchQuery)
	}
	if _, ok := arxivPlanFromExpansion(arxivExpansion{Groups: []FileCompanionArxivGroup{{Terms: []string{"solo"}}}}); ok {
		t.Fatal("a one-term group is not a query")
	}
}

func TestFileCompanionSearchArxivUsesExpansionAndReusesIt(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
  <opensearch:totalResults>41</opensearch:totalResults>
  <entry>
    <id>http://arxiv.org/abs/2510.16079v1</id>
    <title>Listed Paper</title>
    <summary>Listed abstract.</summary>
  </entry>
</feed>`
	var calls int
	var starts []string
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts = append(starts, r.URL.Query().Get("start"))
		queries = append(queries, r.URL.Query().Get("search_query"))
		if r.URL.Query().Get("max_results") != "20" {
			t.Errorf("max_results=%q", r.URL.Query().Get("max_results"))
		}
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	app := &App{}
	app.arxivExpand = func(ctx context.Context, goal string) (arxivExpansion, error) {
		calls++
		if goal != "长文档问答" {
			t.Errorf("goal=%q", goal)
		}
		return arxivExpansion{
			Interpreted: "用检索增强做长文档问答",
			Groups: []FileCompanionArxivGroup{
				{Terms: []string{"retrieval augmented generation", "long document"}, Category: "cs.CL"},
				{Terms: []string{"RAG", "question answering"}, Category: "CS.CL"},
			},
		}, nil
	}
	first, err := app.FileCompanionSearchArxiv("长文档问答", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.FileCompanionSearchArxiv("  长文档问答  ", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expand calls=%d", calls)
	}
	wantQuery := `(all:"retrieval augmented generation" AND all:"long document" AND cat:cs.CL) OR (all:"RAG" AND all:"question answering")`
	if len(queries) != 2 || queries[0] != wantQuery || queries[1] != wantQuery {
		t.Fatalf("queries=%q", queries)
	}
	if len(starts) != 2 || starts[0] != "0" || starts[1] != "20" {
		t.Fatalf("starts=%v", starts)
	}
	if first.Fallback || first.Total != 41 || first.Page != 1 || len(first.Hits) != 1 || len(first.Groups) != 2 || first.Groups[1].Category != "" {
		t.Fatalf("first=%#v", first)
	}
	if second.Page != 2 || second.Total != 41 || second.Interpreted != first.Interpreted {
		t.Fatalf("second=%#v", second)
	}
	// A new search of a plan that worked does not call the model again.
	if _, err := app.FileCompanionSearchArxiv("长文档问答", 1, true); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expand calls=%d", calls)
	}
	// The remembered total stops a page that cannot contain a paper.
	before := len(starts)
	past, err := app.FileCompanionSearchArxiv("长文档问答", 4, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(starts) != before || len(past.Hits) != 0 || past.Total != 41 || past.Page != 4 {
		t.Fatalf("dials=%d past=%#v", len(starts)-before, past)
	}
}

func TestFileCompanionSearchArxivFallsBackWhenExpansionFails(t *testing.T) {
	var calls int
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("search_query")
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	app := &App{}
	app.arxivExpand = func(context.Context, string) (arxivExpansion, error) {
		calls++
		return arxivExpansion{}, errors.New("bad json")
	}
	// The goal carries an English term. A failed expansion must search that
	// term, never the Chinese sentence: arXiv indexes English, so all:"<the
	// whole sentence>" is a query that cannot hit and would be reported as
	// "no results" for a model failure.
	const goal = "agent 自进化 的论文"
	page, err := app.FileCompanionSearchArxiv(goal, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	again, err := app.FileCompanionSearchArxiv(goal, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !page.Fallback || !again.Fallback {
		t.Fatalf("calls=%d page=%#v again=%#v", calls, page, again)
	}
	if gotQuery != `all:"agent"` {
		t.Fatalf("query=%q", gotQuery)
	}
	if len(page.Groups) != 1 || strings.Join(page.Groups[0].Terms, ",") != "agent" {
		t.Fatalf("groups=%#v", page.Groups)
	}
	// 上一页 back to page 1 is a page turn. It must keep the failed plan.
	if _, err := app.FileCompanionSearchArxiv(goal, 1, false); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("page turn retried expansion: calls=%d", calls)
	}
	// The search button retries a failed expansion.
	if _, err := app.FileCompanionSearchArxiv(goal, 1, true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expand calls=%d", calls)
	}
}

// A goal with no Latin word left after a failed expansion has no query arXiv
// can answer. Reporting the empty list as "no results" would blame the query
// for a model failure, so the search returns an error instead of dialing.
func TestFileCompanionSearchArxivReportsAnUntranslatableGoal(t *testing.T) {
	dialed := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dialed++
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	app := &App{}
	app.arxivExpand = func(context.Context, string) (arxivExpansion, error) {
		return arxivExpansion{}, errors.New("timeout")
	}
	const goal = "需要一些自进化相关的论文"
	_, err := app.FileCompanionSearchArxiv(goal, 1, false)
	if !errors.Is(err, errArxivExpandNoTerms) {
		t.Fatalf("err=%v", err)
	}
	if dialed != 0 {
		t.Fatalf("dialed arXiv with an unanswerable query: dials=%d", dialed)
	}
	// The failure is not cached: the reader retrying the button asks the
	// model again rather than reusing a plan that has no query.
	if _, ok := app.arxivCachedPlan(goal); ok {
		t.Fatal("an untranslatable goal must not be remembered as a plan")
	}
}

// With no model configured the goal can never be translated, so telling the
// reader to retry would send them in a circle. The two failures say different
// things: one is a transient call failure, the other a missing capability.
func TestArxivPlanTellsAnUnavailableModelApartFromAFailedCall(t *testing.T) {
	const goal = "需要一些自进化相关的论文"

	_, err := (&App{}).arxivPlan(context.Background(), goal)
	if !errors.Is(err, errArxivExpandUnavailable) {
		t.Fatalf("no model: err=%v", err)
	}

	app := &App{}
	app.arxivExpand = func(context.Context, string) (arxivExpansion, error) {
		return arxivExpansion{}, errors.New("timeout")
	}
	_, err = app.arxivPlan(context.Background(), goal)
	if !errors.Is(err, errArxivExpandNoTerms) {
		t.Fatalf("failed call: err=%v", err)
	}

	// A Latin term in the goal still yields a query when the call fails.
	plan, err := app.arxivPlan(context.Background(), "需要一些agent自进化相关的论文")
	if err != nil || plan.searchQuery != `all:"agent"` {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

// A provider that is present but has no URL or model answers "LLM not
// configured", which is as permanent as having no handler at all. A timeout
// or an endpoint ban heals, so those keep the retry wording.
func TestArxivExpandPermanentSeparatesMissingConfigFromATransientFailure(t *testing.T) {
	permanent := []error{
		errArxivExpandUnavailable,
		errors.New("arxiv-expand LLM call failed: LLM not configured"),
		errors.New("arxiv-expand hub model unavailable: no key"),
	}
	for _, err := range permanent {
		if !arxivExpandPermanent(err) {
			t.Fatalf("%v should be permanent", err)
		}
	}
	transient := []error{
		nil,
		errors.New("arxiv-expand LLM call failed: context deadline exceeded"),
		errors.New("arxiv-expand LLM endpoint temporarily unavailable after recent network failure: x"),
		errors.New("arxiv-expand LLM call failed: no json"),
	}
	for _, err := range transient {
		if arxivExpandPermanent(err) {
			t.Fatalf("%v should be retryable", err)
		}
	}

	// The whole path: an unconfigured provider reports the non-retry message.
	app := &App{}
	app.arxivExpand = func(context.Context, string) (arxivExpansion, error) {
		return arxivExpansion{}, errors.New("arxiv-expand LLM call failed: LLM not configured")
	}
	_, err := app.arxivPlan(context.Background(), "需要一些自进化相关的论文")
	if !errors.Is(err, errArxivExpandUnavailable) {
		t.Fatalf("unconfigured provider: err=%v", err)
	}
}

// A Chinese goal usually has no spaces, so the whole sentence is one token.
// The Latin term inside it is still a query, and lifting it out is what keeps
// the fallback useful instead of guaranteed empty.
func TestArxivFallbackPlanLiftsTheLatinTermOutOfAChineseGoal(t *testing.T) {
	plan := arxivFallbackPlan("需要一些agent自进化相关的论文")
	if !plan.fallback {
		t.Fatalf("plan=%#v", plan)
	}
	if got := strings.Join(plan.groups[0].Terms, ","); got != "agent" {
		t.Fatalf("terms=%q", got)
	}
	if plan.searchQuery != `all:"agent"` {
		t.Fatalf("query=%q", plan.searchQuery)
	}
	// A hyphenated compound survives the split as one term.
	plan = arxivFallbackPlan("介绍self-evolving agent的进展")
	if got := strings.Join(plan.groups[0].Terms, ","); got != "self-evolving,agent" {
		t.Fatalf("terms=%q", got)
	}
	// Punctuation that happens to sit beside a real term is not a term.
	plan = arxivFallbackPlan("-- agent")
	if got := strings.Join(plan.groups[0].Terms, ","); got != "agent" {
		t.Fatalf("terms=%q", got)
	}
	// A single letter is a list marker in Chinese prose, not a query term.
	if plan := arxivFallbackPlan("R语言"); plan.searchQuery != "" {
		t.Fatalf("a one-letter run has no query: %#v", plan)
	}
	// C++ and C# are terms. Trimming the trailing plus or hash would leave a
	// one-letter word, so the edge trim deliberately spares them.
	if got := arxivFallbackPlan("C++编译器").searchQuery; got != `all:"C++"` {
		t.Fatalf("C++ query=%q", got)
	}
	if got := arxivFallbackPlan("C# 和 Java").searchQuery; got != `all:"C#" AND all:"Java"` {
		t.Fatalf("C# query=%q", got)
	}
}

// The token cap must not be spent on CJK words that are about to be dropped.
// A spaced-out Chinese sentence would otherwise lose the English term that
// sits past the cap, which is the only part of it arXiv can match.
func TestArxivSearchableTokensCountsOnlySurvivors(t *testing.T) {
	const spaced = "这 是 一 个 很 长 的 中 文 句 子 关 于 agent 自 进 化"
	if got := arxivFallbackPlan(spaced).searchQuery; got != `all:"agent"` {
		t.Fatalf("spaced goal query=%q", got)
	}
	// The unspaced form reaches the same query, so spacing alone must not
	// decide whether a goal is searchable.
	const glued = "这是一个很长的中文句子关于agent自进化"
	if got := arxivFallbackPlan(glued).searchQuery; got != `all:"agent"` {
		t.Fatalf("glued goal query=%q", got)
	}
}

func TestArxivFallbackPlanDropsTermsTheIndexCannotMatch(t *testing.T) {
	plan := arxivFallbackPlan("agent RAG 自进化 论文")
	if !plan.fallback {
		t.Fatalf("plan=%#v", plan)
	}
	if got := strings.Join(plan.groups[0].Terms, ","); got != "agent,RAG" {
		t.Fatalf("terms=%q", got)
	}
	if plan.searchQuery != `all:"agent" AND all:"RAG"` {
		t.Fatalf("query=%q", plan.searchQuery)
	}
	// Punctuation-only leftovers are not terms either.
	if plan := arxivFallbackPlan("，。、 agent"); strings.Join(plan.groups[0].Terms, ",") != "agent" {
		t.Fatalf("plan=%#v", plan)
	}
	if plan := arxivFallbackPlan("注意力 机制"); plan.searchQuery != "" {
		t.Fatalf("a CJK-only goal has no query: %#v", plan)
	}
}

func TestArxivPlanFromExpansionDropsEchoedChineseTerms(t *testing.T) {
	plan, ok := arxivPlanFromExpansion(arxivExpansion{
		Interpreted: "agent 自进化",
		Groups: []FileCompanionArxivGroup{
			{Terms: []string{"自进化", "智能体"}, Category: "cs.AI"},
			{Terms: []string{"self-evolving agent", "autonomous agent", "自进化"}, Category: "cs.AI"},
		},
	})
	if !ok {
		t.Fatal("a group with English terms is a query")
	}
	// The first group lost every term to the filter; the second keeps its
	// English terms and drops the echoed Chinese one.
	if len(plan.groups) != 1 || strings.Join(plan.groups[0].Terms, ",") != "self-evolving agent,autonomous agent" {
		t.Fatalf("groups=%#v", plan.groups)
	}
	if plan.searchQuery != `(all:"self-evolving agent" AND all:"autonomous agent" AND cat:cs.AI)` {
		t.Fatalf("query=%q", plan.searchQuery)
	}
	// An echo that leaves a group below the two-term floor drops the group
	// rather than searching a single word, which is the same rule a short
	// group already followed.
	if _, ok := arxivPlanFromExpansion(arxivExpansion{Groups: []FileCompanionArxivGroup{
		{Terms: []string{"self-evolving agent", "自进化"}, Category: "cs.AI"},
	}}); ok {
		t.Fatal("one usable term is still a short group")
	}
}

func TestParseArxivExpansionSalvagesATruncatedAnswer(t *testing.T) {
	// A model that narrates before answering hits the output cap mid-string.
	const cut = `{"interpreted":"agent 自进化","groups":[{"terms":["self-evolving agent","autonomous agent"],"category":"cs.AI"},{"terms":["continual learning","model evolution"],"category":"cs.LG"},{"terms":["open-ended"`
	got, err := parseArxivExpansion(cut)
	if err != nil {
		t.Fatal(err)
	}
	if got.Interpreted != "agent 自进化" {
		t.Fatalf("interpreted=%q", got.Interpreted)
	}
	if len(got.Groups) != 2 {
		t.Fatalf("groups=%#v", got.Groups)
	}
	plan, ok := arxivPlanFromExpansion(got)
	if !ok || plan.fallback || len(plan.groups) != 2 {
		t.Fatalf("plan=%#v ok=%v", plan, ok)
	}
	// A cut before any complete group still yields nothing usable.
	if _, err := parseArxivExpansion(`{"interpreted":"agent 自进化","groups":[{"terms":["self-evolving`); err == nil {
		t.Fatal("a half-written term must not become a query")
	}
	// A complete object still parses the ordinary way.
	if _, err := parseArxivExpansion(`{"interpreted":"x","groups":[{"terms":["a","b"]}]}`); err != nil {
		t.Fatal(err)
	}
}

func TestArxivFinishPageTotalStopsTheLastSliceFromAdvertisingAnother(t *testing.T) {
	total, known := arxivFinishPageTotal(9000, arxivSearchMaxStart, arxivSearchPageSize, true)
	if total != arxivSearchMaxStart+arxivSearchPageSize || !known {
		t.Fatalf("total=%d known=%v", total, known)
	}
	if (arxivSearchMaxStart/arxivSearchPageSize+1)*arxivSearchPageSize < total {
		t.Fatal("next page would repeat the last slice")
	}
	// A short page before the cap still ends where the rows end, and a full
	// page before the cap keeps the lower bound that opens the next page.
	total, known = arxivFinishPageTotal(0, 0, 3, false)
	if total != 3 || known {
		t.Fatalf("short total=%d known=%v", total, known)
	}
	total, known = arxivFinishPageTotal(0, 0, arxivSearchPageSize, true)
	if total != arxivSearchPageSize+1 || known {
		t.Fatalf("full total=%d known=%v", total, known)
	}
	// One dropped row does not make a full window look finished.
	total, known = arxivFinishPageTotal(0, 20, arxivSearchPageSize-1, true)
	if total != 20+arxivSearchPageSize+1 || known {
		t.Fatalf("dropped-row total=%d known=%v", total, known)
	}
	total, known = arxivFinishPageTotal(0, 20, arxivSearchPageSize-1, false)
	if total != 39 || known {
		t.Fatalf("short dropped total=%d known=%v", total, known)
	}
}

func arxivTestFeed(total int, ids ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">`)
	if total > 0 {
		fmt.Fprintf(&b, `<opensearch:totalResults>%d</opensearch:totalResults>`, total)
	}
	for _, id := range ids {
		fmt.Fprintf(&b, `<entry><id>http://arxiv.org/abs/%sv1</id><title>Paper %s</title><summary>S</summary></entry>`, id, id)
	}
	b.WriteString(`</feed>`)
	return b.String()
}

func TestFileCompanionSearchArxivKeepsAKnownTotalWhenALaterFullPageOmitsIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start := q.Get("start")
		query := q.Get("search_query")
		w.Header().Set("Content-Type", "application/atom+xml")
		var body string
		switch {
		case strings.Contains(query, "attention") && start == "0":
			body = arxivTestFeed(100, "2510.16079")
		case strings.Contains(query, "attention") && start == "20":
			ids := make([]string, arxivSearchPageSize)
			for i := range ids {
				ids[i] = fmt.Sprintf("2510.%05d", 10000+i)
			}
			body = arxivTestFeed(0, ids...)
		case strings.Contains(query, "listed") && start == "0":
			body = arxivTestFeed(100, "2510.16080")
		case strings.Contains(query, "listed") && start == "20":
			body = arxivTestFeed(0, "2510.16081")
		case strings.Contains(query, "state") && start == "0":
			body = arxivTestFeed(30, "2510.16082")
		case strings.Contains(query, "state") && start == "20":
			ids := make([]string, arxivSearchPageSize)
			for i := range ids {
				ids[i] = fmt.Sprintf("2510.%05d", 11000+i)
			}
			body = arxivTestFeed(0, ids...)
		case strings.Contains(query, "state") && start == "40":
			body = arxivTestFeed(0, "2510.16083")
		case strings.Contains(query, "exact") && start == "0":
			body = arxivTestFeed(40, "2510.16084")
		case strings.Contains(query, "exact") && start == "20":
			ids := make([]string, arxivSearchPageSize)
			for i := range ids {
				ids[i] = fmt.Sprintf("2510.%05d", 12000+i)
			}
			body = arxivTestFeed(0, ids...)
		case strings.Contains(query, "exact") && start == "40":
			// A dial past the ceiling would show this paper. The request must not happen.
			body = arxivTestFeed(0, "2510.16085")
		default:
			body = `<feed xmlns="http://www.w3.org/2005/Atom"></feed>`
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	app := &App{}
	app.arxivExpand = func(context.Context, string) (arxivExpansion, error) {
		t.Fatal("keyword pages must not call the model")
		return arxivExpansion{}, nil
	}

	page, err := app.FileCompanionSearchArxiv("attention mechanism", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 100 || len(page.Hits) != 1 {
		t.Fatalf("page1 total=%d hits=%d", page.Total, len(page.Hits))
	}
	page, err = app.FileCompanionSearchArxiv("attention mechanism", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 100 || len(page.Hits) != arxivSearchPageSize {
		t.Fatalf("full page without totalResults total=%d hits=%d", page.Total, len(page.Hits))
	}
	kept, ok := app.arxivCachedPlan("attention mechanism")
	if !ok || !kept.totalKnown || kept.total != 100 {
		t.Fatalf("cached total=%d known=%v ok=%v", kept.total, kept.totalKnown, ok)
	}

	// A short page is the end. Restoring 100 would leave 下一页 enabled.
	page, err = app.FileCompanionSearchArxiv("listed paper", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	page, err = app.FileCompanionSearchArxiv("listed paper", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 21 || len(page.Hits) != 1 {
		t.Fatalf("short page total=%d hits=%d", page.Total, len(page.Hits))
	}
	kept, ok = app.arxivCachedPlan("listed paper")
	if !ok || !kept.totalKnown || kept.total != 100 {
		t.Fatalf("short page pinned the end: total=%d known=%v ok=%v", kept.total, kept.totalKnown, ok)
	}

	// Rows past the cached ceiling disprove it. The page keeps the lower
	// bound, and the next page is fetched instead of returning the stale 30.
	page, err = app.FileCompanionSearchArxiv("state of the art", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	page, err = app.FileCompanionSearchArxiv("state of the art", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 20+arxivSearchPageSize+1 || len(page.Hits) != arxivSearchPageSize {
		t.Fatalf("lower bound total=%d hits=%d", page.Total, len(page.Hits))
	}
	kept, ok = app.arxivCachedPlan("state of the art")
	if !ok || kept.totalKnown {
		t.Fatalf("disproved ceiling still known: total=%d known=%v ok=%v", kept.total, kept.totalKnown, ok)
	}
	page, err = app.FileCompanionSearchArxiv("state of the art", 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 41 || len(page.Hits) != 1 {
		t.Fatalf("page past a disproved ceiling total=%d hits=%d", page.Total, len(page.Hits))
	}

	// 40 papers fill two pages. The missing total's +1 must not advertise a third.
	page, err = app.FileCompanionSearchArxiv("exact boundary", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 40 {
		t.Fatalf("exact page1 total=%d", page.Total)
	}
	page, err = app.FileCompanionSearchArxiv("exact boundary", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 40 || len(page.Hits) != arxivSearchPageSize {
		t.Fatalf("exact page2 total=%d hits=%d", page.Total, len(page.Hits))
	}
	kept, ok = app.arxivCachedPlan("exact boundary")
	if !ok || !kept.totalKnown || kept.total != 40 {
		t.Fatalf("exact ceiling total=%d known=%v ok=%v", kept.total, kept.totalKnown, ok)
	}
	page, err = app.FileCompanionSearchArxiv("exact boundary", 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Hits) != 0 || page.Total != 40 {
		t.Fatalf("page past an exact ceiling total=%d hits=%d", page.Total, len(page.Hits))
	}
}

func TestFileCompanionSearchArxivKeepsAKnownTotalWhenAFullWindowDropsARow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start := q.Get("start")
		query := q.Get("search_query")
		w.Header().Set("Content-Type", "application/atom+xml")
		var body string
		switch {
		case strings.Contains(query, "dropped") && start == "0":
			body = arxivTestFeed(100, "2510.16079")
		case strings.Contains(query, "dropped") && start == "20":
			ids := make([]string, arxivSearchPageSize)
			for i := range ids {
				ids[i] = fmt.Sprintf("2510.%05d", 13000+i)
			}
			// The last entry repeats the first paper, so the window is full
			// and one row is not shown.
			ids[len(ids)-1] = ids[0]
			body = arxivTestFeed(0, ids...)
		case strings.Contains(query, "short") && start == "0":
			ids := make([]string, arxivSearchPageSize-1)
			for i := range ids {
				ids[i] = fmt.Sprintf("2510.%05d", 14000+i)
			}
			body = arxivTestFeed(0, ids...)
		default:
			body = `<feed xmlns="http://www.w3.org/2005/Atom"></feed>`
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	app := &App{}
	app.arxivExpand = func(context.Context, string) (arxivExpansion, error) {
		t.Fatal("keyword pages must not call the model")
		return arxivExpansion{}, nil
	}
	if _, err := app.FileCompanionSearchArxiv("dropped row", 1, false); err != nil {
		t.Fatal(err)
	}
	page, err := app.FileCompanionSearchArxiv("dropped row", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 100 || len(page.Hits) != arxivSearchPageSize-1 {
		t.Fatalf("full window total=%d hits=%d", page.Total, len(page.Hits))
	}
	kept, ok := app.arxivCachedPlan("dropped row")
	if !ok || !kept.totalKnown || kept.total != 100 {
		t.Fatalf("cached total=%d known=%v ok=%v", kept.total, kept.totalKnown, ok)
	}

	// Nineteen entries really are the end. The missing total must not invent a next page.
	page, err = app.FileCompanionSearchArxiv("short window", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != arxivSearchPageSize-1 || len(page.Hits) != arxivSearchPageSize-1 {
		t.Fatalf("short window total=%d hits=%d", page.Total, len(page.Hits))
	}
}

func TestArxivGoalNeedsExpansion(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"attention", false},
		{"attention mechanism", false},
		{"one two three four five six", false},
		{"one two three four five six seven", true},
		{"注意力", true},
		{"attention?", true},
		{"find papers: attention", true},
		{strings.Repeat("a", 80), false},
		{strings.Repeat("a", 81), true},
	}
	for _, tc := range cases {
		if got := arxivGoalNeedsExpansion(tc.in); got != tc.want {
			t.Errorf("%q needs=%v want=%v", tc.in, got, tc.want)
		}
	}
}

func TestFileCompanionSearchArxivSkipsExpansionForShortKeywords(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("search_query")
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	app := &App{}
	app.arxivExpand = func(context.Context, string) (arxivExpansion, error) {
		t.Fatal("a short keyword list must not call the model")
		return arxivExpansion{}, nil
	}
	page, err := app.FileCompanionSearchArxiv("attention mechanism", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionSearchArxiv("attention mechanism", 2, false); err != nil {
		t.Fatal(err)
	}
	if page.Fallback || len(page.Groups) != 0 || gotQuery != `all:"attention" AND all:"mechanism"` {
		t.Fatalf("page=%#v query=%q", page, gotQuery)
	}
}

// A short ASCII box never reaches the fallback path, so its punctuation has
// to be dropped here as well. all:"--" matches nothing, and beside a real
// term in an AND it makes the whole query unanswerable.
func TestArxivDirectPlanDropsPunctuationOnlyWords(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"-- agent", `all:"agent"`},
		{"agent --", `all:"agent"`},
		{"***", ""},
		// Real terms keep their internal punctuation.
		{"N,N-dimethyl", `all:"N,N-dimethyl"`},
		{"GPT-4", `all:"GPT-4"`},
		{"transformer attention", `all:"transformer" AND all:"attention"`},
	}
	for _, c := range cases {
		if got := arxivDirectPlan(c.in).searchQuery; got != c.want {
			t.Fatalf("arxivDirectPlan(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestFileCompanionSearchArxivIDListSkipsExpansion(t *testing.T) {
	dials := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dials++
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><id>http://arxiv.org/abs/2510.16079v1</id><title>Paper</title><summary>S</summary></entry></feed>`))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	app := &App{}
	app.arxivExpand = func(context.Context, string) (arxivExpansion, error) {
		t.Fatal("an id list must not call the model")
		return arxivExpansion{}, nil
	}
	page, err := app.FileCompanionSearchArxiv("2510.16079", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if dials != 0 || len(page.Hits) != 0 || page.Total != 1 || page.Page != 2 {
		t.Fatalf("dials=%d page=%#v", dials, page)
	}
}

func TestFileCompanionSearchArxivCitationLineSkipsExpansion(t *testing.T) {
	var idList, searchQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idList = r.URL.Query().Get("id_list")
		searchQuery = r.URL.Query().Get("search_query")
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><id>http://arxiv.org/abs/2510.16079v1</id><title>Paper</title><summary>S</summary></entry></feed>`))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	app := &App{}
	app.arxivExpand = func(context.Context, string) (arxivExpansion, error) {
		t.Fatal("a pasted citation must not call the model")
		return arxivExpansion{}, nil
	}
	page, err := app.FileCompanionSearchArxiv("Cite as: arXiv:2510.16079 [cs.CL]", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if idList != "2510.16079" || searchQuery != "" || page.Fallback || len(page.Groups) != 0 || len(page.Hits) != 1 || page.Total != 1 || page.Hits[0].ID != "2510.16079" {
		t.Fatalf("id_list=%q search_query=%q page=%#v", idList, searchQuery, page)
	}
}
