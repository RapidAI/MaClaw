package textutil

import (
	"strings"
	"testing"
)

func TestProjectAnswerSlicesStitchesASharedRun(t *testing.T) {
	shared := strings.Repeat("重叠", 40)
	opening := strings.Repeat("甲", 40) + shared
	rest := strings.Repeat("乙", 30) + "结尾"
	ending := shared + rest
	got := ProjectAnswerSlices(opening, ending)
	if got != opening+rest {
		t.Fatalf("stitched %d runes, want opening+rest without a second copy", len([]rune(got)))
	}
	if strings.Contains(got, "…") || strings.Contains(got, shared+shared) {
		t.Fatalf("shared run was duplicated: %q", got[len(got)-80:])
	}
}

func TestProjectAnswerSlicesKeepsAGapWhenOnlyAMarkIsShared(t *testing.T) {
	opening := strings.Repeat("甲", 39) + "。"
	ending := "。" + strings.Repeat("乙", 40) + "结尾"
	got := ProjectAnswerSlices(opening, ending)
	if got != opening+"\n\n…\n\n"+ending {
		t.Fatalf("got a hard join, want a gap")
	}
}

func TestProjectAnswerSlicesKeepsTheLongerContainingSlice(t *testing.T) {
	opening := strings.Repeat("甲", 20)
	ending := opening + "结尾"
	if got := ProjectAnswerSlices(opening, ending); got != ending {
		t.Fatalf("final that contains the opening = %q", got)
	}
	if got := ProjectAnswerSlices(ending, opening); got != ending {
		t.Fatalf("shorter ending must leave the longer opening, got %q", got)
	}
}

func TestProjectAnswerSlicesConcatenatesAShortFragment(t *testing.T) {
	if got := ProjectAnswerSlices("Hello", " world"); got != "Hello world" {
		t.Fatalf("fragment = %q", got)
	}
	if got := ProjectAnswerSlices("alias ", "merged"); got != "alias merged" {
		t.Fatalf("alias fragment = %q", got)
	}
}

func TestProjectAnswerSlicesKeepsASystemNoticeApart(t *testing.T) {
	got := ProjectAnswerSlices("已经写到这里", "[系统提示] MaClawSrv 执行失败：LLM provider returned 503")
	if !strings.Contains(got, "已经写到这里\n\n[系统提示]") {
		t.Fatalf("notice was stitched into the answer: %q", got)
	}
}

func TestStreamEndContentOmitsAnUnchangedOpening(t *testing.T) {
	opening := strings.Repeat("甲", 30)
	if got := StreamEndContent(opening, opening); got != "" {
		t.Fatalf("unchanged opening still sent %q", got)
	}
	if got := StreamEndContent("", "只有终稿"); got != "" {
		t.Fatalf("a turn that never streamed must not repeat the chunk, got %q", got)
	}
	ending := opening + "结尾"
	if got := StreamEndContent(opening, ending); got != ending {
		t.Fatalf("stream end = %q", got)
	}
}
