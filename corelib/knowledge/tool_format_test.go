package knowledge

import "testing"

func TestBestContentText_CardPrefersClaim(t *testing.T) {
	r := SearchResult{
		ResultType: "card",
		Claim:      "api2 服务器: api2.maclaw.top, 用户名 root, 密码 sunion123",
		Summary:    "api2 服务器信息",
		Snippet:    "api2...服务器",
	}
	got := BestContentText(r)
	if got != r.Claim {
		t.Fatalf("card with all fields: expected Claim, got %q", got)
	}
}

func TestBestContentText_CardFallsToSummaryWhenClaimEmpty(t *testing.T) {
	r := SearchResult{
		ResultType: "card",
		Summary:    "服务器登录信息汇总",
		Snippet:    "api2...服务器",
	}
	got := BestContentText(r)
	if got != r.Summary {
		t.Fatalf("card without claim: expected Summary, got %q", got)
	}
}

func TestBestContentText_CardFallsToSnippetWhenClaimAndSummaryEmpty(t *testing.T) {
	r := SearchResult{
		ResultType: "card",
		Snippet:    "api2...服务器",
	}
	got := BestContentText(r)
	if got != r.Snippet {
		t.Fatalf("card with only snippet: expected Snippet, got %q", got)
	}
}

func TestBestContentText_FactPrefersClaim(t *testing.T) {
	r := SearchResult{
		ResultType: "fact",
		Claim:      "马勇博士共有 3 项发明专利。",
		Summary:    "专利信息",
		Subject:    "马勇",
		Predicate:  "拥有",
		Object:     "3项专利",
	}
	got := BestContentText(r)
	if got != r.Claim {
		t.Fatalf("fact with claim: expected Claim, got %q", got)
	}
}

func TestBestContentText_FactFallsToTriple(t *testing.T) {
	r := SearchResult{
		ResultType: "fact",
		Subject:    "api2",
		Predicate:  "密码是",
		Object:     "sunion123",
	}
	got := BestContentText(r)
	expected := "api2 密码是 sunion123"
	if got != expected {
		t.Fatalf("fact with only triple: expected %q, got %q", expected, got)
	}
}

func TestBestContentText_NodePrefersSnippet(t *testing.T) {
	r := SearchResult{
		ResultType: "node",
		Snippet:    "...部署在 api2.maclaw.top 上运行的 OmniRoute 容器...",
		Claim:      "完整的部署文档内容，很长很长",
		Summary:    "部署文档摘要",
	}
	got := BestContentText(r)
	if got != r.Snippet {
		t.Fatalf("node with all fields: expected Snippet (FTS highlight), got %q", got)
	}
}

func TestBestContentText_NodeFallsToSummaryWhenSnippetEmpty(t *testing.T) {
	r := SearchResult{
		ResultType: "node",
		Claim:      "完整内容",
		Summary:    "文档摘要",
	}
	got := BestContentText(r)
	if got != r.Summary {
		t.Fatalf("node without snippet: expected Summary, got %q", got)
	}
}

func TestBestContentText_EmptyResultTypeDefaultsToCardBehavior(t *testing.T) {
	// ResultType="" (not set) should behave like card — Claim first.
	r := SearchResult{
		Claim:   "完整内容",
		Snippet: "短片段",
	}
	got := BestContentText(r)
	if got != r.Claim {
		t.Fatalf("empty ResultType: expected Claim (card default), got %q", got)
	}
}

func TestBestContentText_AllEmpty(t *testing.T) {
	r := SearchResult{ResultType: "card"}
	got := BestContentText(r)
	if got != "" {
		t.Fatalf("all empty: expected empty string, got %q", got)
	}
}

func TestBestContentText_SnippetNotUsedWhenClaimAvailableForCard(t *testing.T) {
	// This is the exact bug scenario: FTS snippet is 14 chars, Claim has full content.
	r := SearchResult{
		ResultType: "card",
		Claim:      "api1 服务器: api1.maclaw.top, 用户名 root, 密码 sunion123\napi2 服务器: api2.maclaw.top, 用户名 root, 密码 sunion123",
		Snippet:    "api1/api2 服务器", // 14 chars — the bug would return this instead of full Claim
	}
	got := BestContentText(r)
	if got != r.Claim {
		t.Fatalf("bug scenario: expected full Claim with passwords, got %q (len=%d)", got, len([]rune(got)))
	}
	if got == r.Snippet {
		t.Fatal("bug scenario: returned short Snippet instead of full Claim — priority is wrong!")
	}
}
