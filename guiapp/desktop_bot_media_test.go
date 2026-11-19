package guiapp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestLandDesktopBotMediaStagesWhatItCannotWrite(t *testing.T) {
	base := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(prev) })
	if err := os.MkdirAll(filepath.Join(base, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "data", "bot-files"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	shots, local, ids := landDesktopBotMedia("bot_1", []DesktopBotImage{{
		MIME: "image/png", Data: tinyPNG, AttachmentID: "shot-1",
	}}, nil)
	if len(shots) != 0 || len(local) != 0 || len(ids) != 1 {
		t.Fatalf("shots=%v local=%v ids=%v", shots, local, ids)
	}
	got, err := (&App{}).FetchDesktopBotAttachment("bot_1", ids[0])
	if err != nil || got == nil || got.Data != tinyPNG {
		t.Fatalf("staged picture %#v err=%v", got, err)
	}
}

func TestStageDesktopPicturesFeedsRefetch(t *testing.T) {
	ids := stageDesktopPictures("bot_2", []DesktopBotImage{{
		MIME: "image/png", Data: tinyPNG, AttachmentID: "again",
	}}, nil)
	if len(ids) != 1 || ids[0] == "" {
		t.Fatalf("ids %v", ids)
	}
	got, err := (&App{}).FetchDesktopBotAttachment("bot_2", ids[0])
	if err != nil || got == nil || got.AttachmentID != ids[0] || got.Data != tinyPNG {
		t.Fatalf("refetch %#v err=%v", got, err)
	}
}
