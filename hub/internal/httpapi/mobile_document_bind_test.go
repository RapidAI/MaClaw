package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestMobileLookupOwnedDraft(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-doc-bind@example.com")
	ownerID := enroll.UserID
	principal := &auth.ViewerPrincipal{UserID: ownerID, Email: "mobile-doc-bind@example.com"}

	mobileDocuments.Lock()
	mobileDocuments.drafts["doc_bind_1"] = mobileDocumentDraftRecord{
		ID: "doc_bind_1", OwnerID: ownerID, Title: "周报",
		Markdown: "# 标题\n\n内容", UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.drafts["doc_other"] = mobileDocumentDraftRecord{
		ID: "doc_other", OwnerID: "other", Title: "他人",
		Markdown: "x", UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "doc_bind_1")
		delete(mobileDocuments.drafts, "doc_other")
		mobileDocuments.Unlock()
	})

	got, ok := mobileLookupOwnedDraft(principal, "doc_bind_1")
	if !ok || got.Title != "周报" {
		t.Fatalf("lookup own draft failed: ok=%v got=%#v", ok, got)
	}
	if _, ok := mobileLookupOwnedDraft(principal, "doc_other"); ok {
		t.Fatal("must not return other owner's draft")
	}
	if _, ok := mobileLookupOwnedDraft(principal, "missing"); ok {
		t.Fatal("missing draft should fail")
	}
}

func TestMobileInjectBoundDocument(t *testing.T) {
	base := mobileBuildLLMMessages("请润色第二段", nil, nil, nil)
	draft := mobileDocumentDraftRecord{
		ID: "d1", Title: "草稿", Markdown: "第一段\n\n第二段需要润色。",
		UpdatedAt: time.Now().UTC(),
	}
	msgs := mobileInjectBoundDocument(base, draft)
	if len(msgs) < 2 {
		t.Fatalf("expected injected messages, got %d", len(msgs))
	}
	// Second message should be bound document system context.
	if msgs[1]["role"] != "system" {
		t.Fatalf("role=%s", msgs[1]["role"])
	}
	content := msgs[1]["content"]
	for _, part := range []string{"document_id: d1", "第二段需要润色", "maclaw-document-edit", "has_original: false"} {
		if !strings.Contains(content, part) {
			t.Fatalf("bound context missing %q: %s", part, content)
		}
	}
}

func TestMobileInjectBoundDocumentWithOriginal(t *testing.T) {
	base := mobileBuildLLMMessages("根据原件写摘要", nil, nil, nil)
	draft := mobileDocumentDraftRecord{
		ID: "d2", Title: "推荐表", Markdown: "# 推荐表\n\n正文",
		SourceFilename: "推荐表.docx", SourceContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		SourceBytes: []byte("PK fake"), UpdatedAt: time.Now().UTC(),
	}
	msgs := mobileInjectBoundDocument(base, draft)
	content := msgs[1]["content"]
	for _, part := range []string{"has_original: true", "source_filename: 推荐表.docx", "ORIGINAL uploaded file", "正文"} {
		if !strings.Contains(content, part) {
			t.Fatalf("bound context missing %q: %s", part, content)
		}
	}
}

func TestMobileDraftMarkdownLooksLikeRawBinary(t *testing.T) {
	if !mobileDraftMarkdownLooksLikeRawBinary("PK\x03\x04[Content_Types].xml") {
		t.Fatal("expected docx garbage to look binary")
	}
	if mobileDraftMarkdownLooksLikeRawBinary("# 正常标题\n\n正文") {
		t.Fatal("normal markdown should not look binary")
	}
}

func TestMobileExtractDocumentEditFence(t *testing.T) {
	answer := "建议如下：\n\n```maclaw-document-edit\n# 新标题\n\n改写后的正文\n```\n\n记得审阅。"
	body, ok := mobileExtractDocumentEditFence(answer)
	if !ok {
		t.Fatal("expected fence")
	}
	if body != "# 新标题\n\n改写后的正文" {
		t.Fatalf("body=%q", body)
	}
	if _, ok := mobileExtractDocumentEditFence("no fence"); ok {
		t.Fatal("unexpected fence")
	}
}
