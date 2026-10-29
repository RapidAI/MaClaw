package demoskill

import (
	"context"
	"fmt"
	"strings"
)

// ShotFetcher loads one screenshot PNG by timeline Ref.
type ShotFetcher func(ctx context.Context, id, ref string) ([]byte, error)

// VisionCaptioner describes one screenshot for the distill prompt. Prefer a
// multimodal chat model. Empty string means try OCR next.
type VisionCaptioner func(ctx context.Context, png []byte) (string, error)

// OCRCaptioner is the fallback when vision is unavailable or returns nothing.
type OCRCaptioner func(ctx context.Context, png []byte) (string, error)

// CaptionMaxShots bounds how many screenshots one distill run captions.
const CaptionMaxShots = 40

// Caption fills Event.Text on screenshot events that only have a Ref, so
// Compact keeps them and BuildPrompt can use the image information. Missing
// shots are skipped; the rest of the timeline still distills.
func Caption(ctx context.Context, tl *Timeline, fetch ShotFetcher, vision VisionCaptioner, ocr OCRCaptioner) error {
	if tl == nil || fetch == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	n := 0
	for i := range tl.Events {
		e := &tl.Events[i]
		if e.Kind != KindScreenshot || strings.TrimSpace(e.Text) != "" || strings.TrimSpace(e.Ref) == "" {
			continue
		}
		if n >= CaptionMaxShots {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		png, err := fetch(ctx, tl.ID, e.Ref)
		if err != nil || len(png) == 0 {
			continue
		}
		n++
		text := ""
		if vision != nil {
			text, _ = vision(ctx, png)
		}
		if strings.TrimSpace(text) == "" && ocr != nil {
			text, _ = ocr(ctx, png)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if len([]rune(text)) > 800 {
			text = string([]rune(text)[:800]) + "…"
		}
		prefix := ""
		if label := strings.Trim(strings.TrimSpace(e.App+" / "+e.Window), " /"); label != "" {
			prefix = label + "："
		}
		e.Text = prefix + text
	}
	return nil
}

// ApplyCaptions sets Text from a ref→caption map (for tests and precomputed captions).
func ApplyCaptions(tl *Timeline, captions map[string]string) {
	if tl == nil || len(captions) == 0 {
		return
	}
	for i := range tl.Events {
		e := &tl.Events[i]
		if e.Kind != KindScreenshot || e.Ref == "" {
			continue
		}
		if c := strings.TrimSpace(captions[e.Ref]); c != "" && strings.TrimSpace(e.Text) == "" {
			e.Text = c
		}
	}
}

// CaptionPrompt is the short instruction sent with each screenshot.
func CaptionPrompt() string {
	return "用一两句话描述这张桌面截图里对完成任务有用的信息（窗口、按钮、表单、状态），不要猜测看不见的内容。"
}

// FormatOCRLines joins OCR lines into one caption.
func FormatOCRLines(lines []string) string {
	var kept []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, line)
		if len(kept) >= 30 {
			break
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return fmt.Sprintf("画面文字：%s", strings.Join(kept, " / "))
}
