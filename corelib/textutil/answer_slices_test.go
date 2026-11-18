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
	long := strings.Repeat("甲", 40)
	if got := ProjectAnswerSlices(long, " world"); got != long+" world" {
		t.Fatalf("short tail = %q", got)
	}
}

func TestProjectAnswerSlicesKeepsAGapWhenTheOpeningIsShort(t *testing.T) {
	ending := strings.Repeat("乙", 40)
	got := ProjectAnswerSlices("好的", ending)
	if got != "好的\n\n…\n\n"+ending {
		t.Fatalf("short opening was glued onto a different answer: %q", got)
	}
}

func TestProjectAnswerSlicesStitchesEmojiByCodeUnit(t *testing.T) {
	// Eight emoji are 8 runes and 16 UTF-16 code units: one clause on the
	// viewer, and too short if the run is counted in runes.
	shared := strings.Repeat("😀", 8)
	opening := "甲" + shared
	ending := shared + "结尾"
	got := ProjectAnswerSlices(opening, ending)
	if got != opening+"结尾" {
		t.Fatalf("emoji run was not stitched once: %q", got)
	}
	if strings.Contains(got, shared+shared) || strings.Contains(got, "…") {
		t.Fatalf("emoji run was duplicated: %q", got)
	}
}

func TestProjectAnswerSlicesKeepsOneCopyWhenTheStreamKeepsATrailingNewline(t *testing.T) {
	body := strings.Repeat("甲", 40) + "答案"
	opening := body + "\n"
	got := ProjectAnswerSlices(opening, body)
	if got != body {
		t.Fatalf("trailing newline split the answer: %q", got)
	}
	// The settled answer continues past the streamed sentence. The newline the
	// loop trimmed must not sit between the shared run and that continuation,
	// and the earlier planning stays.
	planning := strings.Repeat("计", 20)
	extended := body + "结尾"
	if got := ProjectAnswerSlices(planning+opening, extended); got != planning+extended {
		t.Fatalf("extension after the trimmed newline = %q", got)
	}
}

func TestProjectAnswerSlicesKeepsASystemNoticeApart(t *testing.T) {
	got := ProjectAnswerSlices("已经写到这里", "[系统提示] MaClawSrv 执行失败：LLM provider returned 503")
	if !strings.Contains(got, "已经写到这里\n\n[系统提示]") {
		t.Fatalf("notice was stitched into the answer: %q", got)
	}
	// A short notice must stay a notice. The short-fragment rule would
	// otherwise glue it onto the answer.
	short := ProjectAnswerSlices("已经写到这里", "[系统提示] 失败")
	if short != "已经写到这里\n\n[系统提示] 失败" {
		t.Fatalf("short notice = %q", short)
	}
}

func TestClipKeepTailRetainsTheSettledEnding(t *testing.T) {
	short := "Hello world"
	if got := ClipKeepTail(short, 1200); got != short {
		t.Fatalf("short text = %q", got)
	}
	ending := strings.Repeat("乙", 20)
	got := ClipKeepTail(strings.Repeat("甲", 80)+ending, 30)
	if !strings.HasPrefix(got, "...") || !strings.HasSuffix(got, ending) {
		t.Fatalf("tail clip = %q", got)
	}
	if got := ClipKeepTail("  "+ending+"  ", 0); got != ending {
		t.Fatalf("disabled clip = %q", got)
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
	withNewline := opening + "\n"
	if got := StreamEndContent(withNewline, opening); got != opening {
		t.Fatalf("newline-only difference = %q", got)
	}
}
