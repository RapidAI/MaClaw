package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	corelib "github.com/RapidAI/CodeClaw/corelib"
)

type fakeModelEmbedder struct{ calls int }

func (f *fakeModelEmbedder) Embed(string) ([]float32, error) { return []float32{1}, nil }
func (f *fakeModelEmbedder) EmbedBatch(t []string) ([][]float32, error) {
	f.calls++
	out := make([][]float32, len(t))
	for i := range t {
		out[i] = []float32{float32(len(t[i]))}
	}
	return out, nil
}
func (f *fakeModelEmbedder) Dim() int { return 1 }
func (f *fakeModelEmbedder) Close()   {}

func startTestModelService(t *testing.T, m *srvAIModelManager) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "m.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: newModelServiceHandler(m)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock
}

func TestModelServiceEmbedThroughSocket(t *testing.T) {
	root := t.TempDir()
	svc := newSrvAIModelManager(root)
	svc.remote = nil
	path := svc.modelPath(srvEmbeddingModelFilename())
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte("x"), 0o644)
	fake := &fakeModelEmbedder{}
	svc.embeddingMgr, svc.embeddingPath = fake, path
	sock := startTestModelService(t, svc)

	client := newSrvAIModelManager(t.TempDir())
	client.remote = newModelRemote(sock, false)
	v, err := client.embedBatch(context.Background(), corelib.AppConfig{}, []string{"ab", "abcd"})
	if err != nil || len(v) != 2 || v[1][0] != 4 || fake.calls != 1 {
		t.Fatalf("v=%v err=%v calls=%d", v, err, fake.calls)
	}
	if client.embeddingMgr != nil {
		t.Fatal("client loaded its own embedder")
	}
}

func TestModelServiceNotReadyMapsAndNoFallback(t *testing.T) {
	svc := newSrvAIModelManager(t.TempDir())
	svc.remote = nil
	sock := startTestModelService(t, svc)
	client := newSrvAIModelManager(t.TempDir())
	client.remote = newModelRemote(sock, true)
	if _, err := client.transcribeWAV(context.Background(), corelib.AppConfig{}, []byte("RIFF")); !errors.Is(err, errSrvAIModelNotReady) {
		t.Fatalf("asr err=%v", err)
	}
	if _, err := client.detectUI(context.Background(), "x"); !errors.Is(err, errSrvAIModelNotReady) {
		t.Fatalf("detect err=%v", err)
	}
}

func TestModelServiceUnavailableNoFallbackByDefault(t *testing.T) {
	client := newSrvAIModelManager(t.TempDir())
	client.remote = newModelRemote(filepath.Join(t.TempDir(), "missing.sock"), false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.recognizeOCR(ctx, "x")
	if err == nil || client.ocrMgr != nil {
		t.Fatalf("err=%v ocrMgr=%v", err, client.ocrMgr)
	}
}

func TestModelRemoteFromEnv(t *testing.T) {
	t.Setenv("MACLAW_MODEL_SERVICE_SOCKET", "")
	if newModelRemoteFromEnv() != nil {
		t.Fatal("unset should be in-process")
	}
	t.Setenv("MACLAW_MODEL_SERVICE_SOCKET", "/tmp/x.sock")
	r := newModelRemoteFromEnv()
	if r == nil || r.fallback {
		t.Fatal("fallback must default off")
	}
}
