package browser

import "testing"

func TestPublishEditorTargetReusesTheRecordedDraft(t *testing.T) {
	draft := "https://zhuanlan.zhihu.com/write?draft=abc"
	spec := PublishSpec{EditorURL: "https://zhuanlan.zhihu.com/write", DraftURL: draft}
	if got := publishEditorTarget(spec); got != draft {
		t.Fatalf("target %s", got)
	}
	if publishOnPage("https://zhuanlan.zhihu.com/write", draft) {
		t.Fatal("a bare editor is not the recorded draft")
	}
	if !publishOnPage(draft, draft) {
		t.Fatal("the recorded draft is the page to stay on")
	}
	spec.NewDraft = true
	if got := publishEditorTarget(spec); got != spec.EditorURL {
		t.Fatalf("new draft target %s", got)
	}
}

func TestPublishEditorTargetLeavesAForeignDraftWhenAskedForANewOne(t *testing.T) {
	spec := PublishSpec{EditorURL: "https://editor.csdn.net/md/", NewDraft: true}
	cur := "https://editor.csdn.net/md?articleId=1"
	if publishOnPage(cur, publishEditorTarget(spec)) {
		t.Fatal("a new draft must not stay on someone else's article")
	}
	if !foreignDraft(cur, spec) {
		t.Fatal("the restored draft is still foreign")
	}
}
