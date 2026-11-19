package browser

import "testing"

func TestListTitleHitRejectsADifferentContinuation(t *testing.T) {
	want := "每日论文综述：2026-11-15 Transformer"
	other := "每日论文综述：2026-11-14 旧模型"
	if clipRunes(want, 12) != clipRunes(other, 12) {
		t.Fatal("fixture does not share the 12-character list key")
	}
	if listTitleHit(want, other) {
		t.Fatal("the other day matched")
	}
	if !listTitleHit(want, want) {
		t.Fatal("exact title")
	}
	if !listTitleHit(want, want+" 审核中") {
		t.Fatal("title plus a status")
	}
	if !listTitleHit(want, "每日论文综述：2026-11-15…") {
		t.Fatal("truncated right title")
	}
	if listTitleHit(want, "每日论文综述：2026-11-14…") {
		t.Fatal("truncated other day")
	}
	if listTitleHit("综述", "论文综述精选") {
		t.Fatal("a short title matched inside a different one")
	}
	if !listTitleHit("综述", "综述") {
		t.Fatal("short exact title")
	}
}
