package guiapp

import (
	"context"
	"testing"
	"time"
)

func TestUploadDesktopBotMaterialChunkPostsDecodedBytes(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	prev := desktopBotExchangeOverride
	desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		gotPath, gotBody = method+" "+path, body.(map[string]any)
		if d, ok := dest.(*DesktopBotMaterialUpload); ok {
			d.Done, d.Path = true, "~/素材/b/a.png"
		}
		return 200, nil
	}
	defer func() { desktopBotExchangeOverride = prev }()
	out, err := (&App{}).UploadDesktopBotMaterialChunk("bot 1", "up-12345678", "a.png", 0, 3, "YWJj")
	if err != nil || !out.Done || out.Path != "~/素材/b/a.png" || gotPath != "POST /api/v1/bots/bot%201/desktop/files" {
		t.Fatalf("%v %+v %s", err, out, gotPath)
	}
	if string(gotBody["data"].([]byte)) != "abc" || gotBody["total"].(int64) != 3 {
		t.Fatalf("body %v", gotBody)
	}
	if _, err := (&App{}).UploadDesktopBotMaterialChunk("b", "u", "n", 0, 1, "!!"); err == nil {
		t.Fatal("bad base64 accepted")
	}
}
