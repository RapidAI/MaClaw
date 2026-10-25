package guiapp

import (
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
	if q.Get("max_results") != "10" || q.Get("sortBy") != "relevance" || q.Get("start") != "0" || q.Get("id_list") != "" {
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
		if r.URL.Query().Get("max_results") != "10" {
			t.Errorf("max_results=%q", r.URL.Query().Get("max_results"))
		}
		w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()
	prev := arxivAPIBaseURL
	arxivAPIBaseURL = srv.URL + "/api/query"
	t.Cleanup(func() { arxivAPIBaseURL = prev })

	hits, err := (&App{}).FileCompanionSearchArxiv("listed paper")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != `all:"listed" AND all:"paper"` {
		t.Fatalf("query=%q", gotQuery)
	}
	if len(hits) != 1 || hits[0].ID != "2510.16079" || hits[0].Title != "Listed Paper" || hits[0].Abstract != "Listed abstract." {
		t.Fatalf("hits=%#v", hits)
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
		_, err := (&App{}).FileCompanionSearchArxiv(in)
		if err == nil || !strings.Contains(err.Error(), "请输入关键字") {
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

	_, err := (&App{}).FileCompanionSearchArxiv("not a feed")
	if err == nil || !strings.Contains(err.Error(), "没有返回论文列表") {
		t.Fatalf("bad feed err=%v", err)
	}
	_, err = (&App{}).FileCompanionSearchArxiv("missing")
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

	hits, err := (&App{}).FileCompanionSearchArxiv("https://arxiv.org/abs/2510.16079v2")
	if err != nil {
		t.Fatal(err)
	}
	if idList != "2510.16079" || searchQuery != "" || sortBy != "" {
		t.Fatalf("id_list=%q search_query=%q sortBy=%q", idList, searchQuery, sortBy)
	}
	if len(hits) != 1 || hits[0].ID != "2510.16079" || hits[0].Title != "The Paper Itself" {
		t.Fatalf("hits=%#v", hits)
	}
}
