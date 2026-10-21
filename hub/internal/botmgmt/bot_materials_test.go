package botmgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type materialSettings map[string]string

func (m materialSettings) Set(_ context.Context, k, v string) error        { m[k] = v; return nil }
func (m materialSettings) Get(_ context.Context, k string) (string, error) { return m[k], nil }

type materialDesktop struct {
	user, path string
	got        []byte
	fail       int
	before     func()
}

func (d *materialDesktop) Open(context.Context, string, string) (string, error) { return "", nil }
func (d *materialDesktop) Stop(context.Context, string, string) error           { return nil }
func (d *materialDesktop) UploadFile(_ context.Context, _, user, path string, body io.Reader, size int64) (string, error) {
	if d.before != nil {
		d.before()
	}
	if d.fail > 0 {
		d.fail--
		return "", errors.New("desktop down")
	}
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
	if err != nil || !up.Done || up.Path != "~/素材/小红书助手/封面-up-12345678.png" {
		t.Fatalf("last %+v %v", up, err)
	}
	if !bytes.Equal(d.got, data) || d.user != "alice" || d.path != "/home/desktop/素材/小红书助手/封面-up-12345678.png" {
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
	if err != nil || up.Path != "~/素材/bot_a/_a-up-abcdefgh.txt" || d.path != "/home/desktop/素材/bot_a/_a-up-abcdefgh.txt" {
		t.Fatalf("collision fallback %+v %v path=%s", up, err, d.path)
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

func TestUploadMaterialRetriesTheDesktopPutWithoutRestaging(t *testing.T) {
	svc, d := materialService(t, []Bot{{ID: "bot_a", Name: "助手", OwnerUserID: "alice"}})
	d.fail = 1
	ctx := context.Background()
	if _, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: "up-12345678", Name: "a.txt", Offset: 0, Total: 4, Data: []byte("ab")}); err != nil {
		t.Fatal(err)
	}
	last := MaterialChunk{UploadID: "up-12345678", Name: "a.txt", Offset: 2, Total: 4, Data: []byte("cd")}
	if _, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", last); err == nil {
		t.Fatal("desktop failure was ignored")
	}
	up, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", last)
	if err != nil || !up.Done || string(d.got) != "abcd" || up.Path != "~/素材/助手/a-up-12345678.txt" {
		t.Fatalf("retry %+v %v got %q", up, err, d.got)
	}
}

func TestUploadMaterialReplaysAFinishedUpload(t *testing.T) {
	svc, d := materialService(t, []Bot{{ID: "bot_a", Name: "助手", OwnerUserID: "alice"}})
	ctx := context.Background()
	last := MaterialChunk{UploadID: "up-12345678", Name: "a.txt", Offset: 2, Total: 4, Data: []byte("cd")}
	if _, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: "up-12345678", Name: "a.txt", Offset: 0, Total: 4, Data: []byte("ab")}); err != nil {
		t.Fatal(err)
	}
	up, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", last)
	if err != nil || !up.Done {
		t.Fatalf("finish %+v %v", up, err)
	}
	d.fail = 1
	again, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", last)
	if err != nil || !again.Done || again.Path != up.Path || again.Received != 4 {
		t.Fatalf("replay %+v %v", again, err)
	}
	fromZero, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: "up-12345678", Name: "a.txt", Offset: 0, Total: 4, Data: []byte("ab")})
	if err != nil || !fromZero.Done || fromZero.Path != up.Path {
		t.Fatalf("offset 0 of a finished upload rewrote it: %+v %v", fromZero, err)
	}
}

func TestUploadMaterialOffsetZeroReplacesThePart(t *testing.T) {
	svc, d := materialService(t, []Bot{{ID: "bot_a", Name: "助手", OwnerUserID: "alice"}})
	ctx := context.Background()
	id := "up-12345678"
	if _, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: id, Name: "a.txt", Offset: 0, Total: 6, Data: []byte("ab")}); err != nil {
		t.Fatal(err)
	}
	up, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: id, Name: "a.txt", Offset: 0, Total: 6, Data: []byte("q")})
	if err != nil || up.Done || up.Received != 1 {
		t.Fatalf("restart %+v %v", up, err)
	}
	up, err = svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: id, Name: "a.txt", Offset: 1, Total: 6, Data: []byte("wxyz!")})
	if err != nil || !up.Done || string(d.got) != "qwxyz!" {
		t.Fatalf("restart appended onto the old part: got %q %+v %v", d.got, up, err)
	}
}

func TestReplaceMaterialChunkDropsThePreviousBytes(t *testing.T) {
	part := filepath.Join(t.TempDir(), "up-12345678.part")
	if err := os.WriteFile(part, []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A desktop put holds the part open. Truncate still replaces those bytes.
	held, err := os.Open(part)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	n, err := replaceMaterialChunk(part, []byte("xy"))
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, err := os.ReadFile(part)
	if err != nil || string(got) != "xy" {
		t.Fatalf("part=%q err=%v", got, err)
	}
}

func TestUploadMaterialRejectsAChangedName(t *testing.T) {
	svc, _ := materialService(t, []Bot{{ID: "bot_a", Name: "助手", OwnerUserID: "alice"}})
	ctx := context.Background()
	if _, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: "up-12345678", Name: "a.txt", Offset: 0, Total: 4, Data: []byte("ab")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: "up-12345678", Name: "b.txt", Offset: 2, Total: 4, Data: []byte("cd")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("renamed upload accepted: %v", err)
	}
}

func TestMaterialUploadsDoNotShareOneLock(t *testing.T) {
	svc, d := materialService(t, []Bot{{ID: "bot_a", Name: "助手", OwnerUserID: "alice"}})
	id1, id2 := "up-lock-aaaa", "up-lock-bbbb"
	entered := make(chan struct{})
	release := make(chan struct{})
	var n atomic.Int32
	d.before = func() {
		if n.Add(1) == 1 {
			close(entered)
			<-release
		}
	}
	ctx := context.Background()
	go func() {
		_, _ = svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: id1, Name: "a.txt", Total: 1, Data: []byte("a")})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first upload did not reach the desktop")
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.UploadMaterialChunk(ctx, "t1", "alice", "bot_a", MaterialChunk{UploadID: id2, Name: "b.txt", Total: 1, Data: []byte("b")})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second upload blocked behind the first")
	}
	close(release)
}
