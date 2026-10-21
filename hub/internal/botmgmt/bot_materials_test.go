package botmgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

type materialSettings map[string]string

func (m materialSettings) Set(_ context.Context, k, v string) error        { m[k] = v; return nil }
func (m materialSettings) Get(_ context.Context, k string) (string, error) { return m[k], nil }

type materialDesktop struct {
	user, path string
	got        []byte
}

func (d *materialDesktop) Open(context.Context, string, string) (string, error) { return "", nil }
func (d *materialDesktop) Stop(context.Context, string, string) error           { return nil }
func (d *materialDesktop) UploadFile(_ context.Context, _, user, path string, body io.Reader, size int64) (string, error) {
	d.user, d.path = user, path
	d.got, _ = io.ReadAll(body)
	if int64(len(d.got)) != size {
		return "", errors.New("size")
	}
	return "ok", nil
}

func materialService(t *testing.T, bots []Bot) (*Service, *materialDesktop) {
	t.Helper()
	raw, _ := json.Marshal(record{Bots: bots})
	settings := materialSettings{storageKey("t1"): string(raw)}
	d := &materialDesktop{}
	return &Service{System: settings, Desktop: d, MaterialStageDir: t.TempDir()}, d
}

func TestUploadMaterialChunksLandInTheBotFolder(t *testing.T) {
	svc, d := materialService(t, []Bot{
		{ID: "bot_a", Name: "小红书助手", OwnerUserID: "alice"},
		{ID: "bot_b", Name: "写作", OwnerUserID: "alice"},
	})
	ctx := context.Background()
	data := []byte("0123456789")
	up, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: "up-12345678", Name: "封面.png", Offset: 0, Total: 10, Data: data[:6]})
	if err != nil || up.Done || up.Received != 6 {
		t.Fatalf("first %+v %v", up, err)
	}
	if _, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: "up-12345678", Name: "封面.png", Offset: 3, Total: 10, Data: data[3:]}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("wrong offset accepted: %v", err)
	}
	up, err = svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: "up-12345678", Name: "封面.png", Offset: 6, Total: 10, Data: data[6:]})
	if err != nil || !up.Done || up.Path != "~/素材/小红书助手/封面.png" {
		t.Fatalf("last %+v %v", up, err)
	}
	if !bytes.Equal(d.got, data) || d.user != "alice" || d.path != "/home/desktop/素材/小红书助手/封面.png" {
		t.Fatalf("desktop got %q user=%s path=%s", d.got, d.user, d.path)
	}
}

func TestUploadMaterialOwnerOnlyAndFolderFallback(t *testing.T) {
	svc, d := materialService(t, []Bot{
		{ID: "bot_a", Name: "助手", OwnerUserID: "alice"},
		{ID: "bot_b", Name: "助手", OwnerUserID: "alice"},
		{ID: "bot_c", Name: "x", OwnerUserID: "bob"},
	})
	ctx := context.Background()
	if _, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_c", MaterialChunk{UploadID: "up-12345678", Name: "a.txt", Total: 1, Data: []byte("a")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner's bot: %v", err)
	}
	up, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: "up-abcdefgh", Name: "../a.txt", Total: 1, Data: []byte("a")})
	if err != nil || up.Path != "~/素材/bot_a/_a.txt" || d.path != "/home/desktop/素材/bot_a/_a.txt" {
		t.Fatalf("collision fallback %+v %v", up, err)
	}
	for _, bad := range []MaterialChunk{
		{UploadID: "short", Name: "a", Total: 1, Data: []byte("a")},
		{UploadID: "up-12345678", Name: "..", Total: 1, Data: []byte("a")},
		{UploadID: "up-12345678", Name: "a", Total: 201 << 20, Data: []byte("a")},
		{UploadID: "up-12345678", Name: "a", Total: 1, Data: []byte("ab")},
	} {
		if _, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("bad chunk %+v accepted: %v", bad.UploadID, err)
		}
	}
}
