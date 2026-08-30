package pptx

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileRendersOutlineReadableByNativeReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.pptx")
	outline := Outline{
		Title:    "庆祝布偶宝宝5岁生日",
		Subtitle: "可爱的布偶猫咪生日快乐",
		Slides: []OutlineSlide{
			{Title: "关于我的布偶宝宝", Bullets: []string{"品种：布偶猫 (Ragdoll)", "特点：温顺粘人、蓝眼睛、长毛"}, Notes: "先介绍基本情况"},
			{Title: "成长相册", Bullets: []string{"第1年：小奶猫来到家里", "第5年：生日快乐"}},
		},
	}
	if err := WriteFile(path, outline); err != nil {
		t.Fatal(err)
	}
	pres, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if pres.SlideCount != 3 {
		t.Fatalf("slide_count = %d, want 3 (title + 2 content)", pres.SlideCount)
	}
	joined := ""
	for _, slide := range pres.Slides {
		for _, shape := range slide.Shapes {
			if shape.Text != nil {
				for _, para := range shape.Text.Paragraphs {
					for _, run := range para.Runs {
						joined += run.Text + "\n"
					}
				}
			}
		}
	}
	for _, want := range []string{"庆祝布偶宝宝5岁生日", "关于我的布偶宝宝", "温顺粘人", "成长相册", "生日快乐"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("rendered deck lost %q; text:\n%s", want, joined)
		}
	}
}

func TestWriteFileRejectsEmptyOutlineAndSanitizesXML(t *testing.T) {
	if err := WriteFile(filepath.Join(t.TempDir(), "x.pptx"), Outline{}); err == nil {
		t.Fatal("empty outline must be rejected")
	}
	path := filepath.Join(t.TempDir(), "ctrl.pptx")
	if err := WriteFile(path, Outline{Title: "a\x00b\x07c", Slides: []OutlineSlide{{Title: "t", Bullets: []string{"x\x1fy"}}}}); err != nil {
		t.Fatal(err)
	}
	pres, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, slide := range pres.Slides {
		for _, shape := range slide.Shapes {
			if shape.Text == nil {
				continue
			}
			for _, para := range shape.Text.Paragraphs {
				for _, run := range para.Runs {
					if strings.ContainsAny(run.Text, "\x00\x07\x1f") {
						t.Fatalf("control characters leaked into deck: %q", run.Text)
					}
				}
			}
		}
	}
}

func TestWriteFileSkipsEmptyLeadingBullets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bullets.pptx")
	if err := WriteFile(path, Outline{Slides: []OutlineSlide{{
		Title:   "要点",
		Bullets: []string{"", "  ", "实点"},
	}}}); err != nil {
		t.Fatal(err)
	}
	pres, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	var runs []string
	for _, slide := range pres.Slides {
		for _, shape := range slide.Shapes {
			if shape.Text == nil {
				continue
			}
			for _, para := range shape.Text.Paragraphs {
				for _, run := range para.Runs {
					if text := strings.TrimSpace(run.Text); text != "" {
						runs = append(runs, text)
					}
				}
			}
		}
	}
	if len(runs) != 2 || runs[0] != "要点" || runs[1] != "实点" {
		t.Fatalf("runs=%q, want [要点 实点]", runs)
	}
}

func TestWriteFileWithoutTitleReusesInitialBlankSlide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notitle.pptx")
	err := WriteFile(path, Outline{Slides: []OutlineSlide{
		{Title: "第一页", Bullets: []string{"a"}},
		{Title: "第二页", Bullets: []string{"b"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	pres, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if pres.SlideCount != 2 {
		t.Fatalf("slide_count = %d, want 2 (no blank first page)", pres.SlideCount)
	}
}
