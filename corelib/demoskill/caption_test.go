package demoskill

import (
	"context"
	"testing"
	"time"
)

func TestCaptionFillsScreenshotText(t *testing.T) {
	tl := &Timeline{ID: "demo-1", StartedAt: time.Now(), Events: []Event{
		{Kind: KindScreenshot, Ref: "001.png", App: "Mail"},
		{Kind: KindNarration, Text: "发周报"},
		{Kind: KindScreenshot, Ref: "002.png", Text: "already"},
	}}
	err := Caption(context.Background(), tl,
		func(_ context.Context, _, ref string) ([]byte, error) { return []byte(ref), nil },
		func(_ context.Context, png []byte) (string, error) { return "收件箱列表", nil },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if tl.Events[0].Text != "Mail：收件箱列表" {
		t.Fatalf("got %q", tl.Events[0].Text)
	}
	if tl.Events[2].Text != "already" {
		t.Fatal("must not overwrite existing captions")
	}
	if len(Compact(*tl)) < 2 {
		t.Fatal("captioned screenshots must survive Compact")
	}
}
