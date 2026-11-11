package guiapp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/diarization"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/modelmanager"
)

func testCAMPlusHeader() []byte {
	var data bytes.Buffer
	data.WriteString("CMPG\x01")
	_ = binary.Write(&data, binary.LittleEndian, uint32(2))
	for _, name := range []string{"head.conv1.weight", "xvector.dense.linear.weight"} {
		_ = binary.Write(&data, binary.LittleEndian, uint16(len(name)))
		data.WriteString(name)
		data.WriteByte(1)
		_ = binary.Write(&data, binary.LittleEndian, uint32(1))
		_ = binary.Write(&data, binary.LittleEndian, float32(0))
	}
	return data.Bytes()
}

func withDiarizationTestModelsDir(t *testing.T) string {
	t.Helper()
	previous := embedding.BaseDirFunc.Load()
	base := t.TempDir()
	embedding.BaseDirFunc.Store(func() string { return base })
	t.Cleanup(func() { embedding.BaseDirFunc.Store(previous) })
	return filepath.Join(base, "models")
}

func TestCheckDiarizationModel(t *testing.T) {
	dir := withDiarizationTestModelsDir(t)
	app := &App{}

	if got := app.CheckDiarizationModel(); got["exists"] != false {
		t.Fatalf("missing model status = %#v", got)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(dir, diarization.DefaultCAMPlusFilename)
	if err := os.WriteFile(modelPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := app.CheckDiarizationModel(); got["exists"] != false {
		t.Fatalf("empty model status = %#v", got)
	}
	if err := os.WriteFile(modelPath, []byte("CMPG"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := app.CheckDiarizationModel(); got["exists"] != false {
		t.Fatalf("truncated model status = %#v", got)
	}
	if err := os.WriteFile(modelPath, testCAMPlusHeader(), 0o644); err != nil {
		t.Fatal(err)
	}
	got := app.CheckDiarizationModel()
	if got["exists"] != true || got["size"] != int64(len(testCAMPlusHeader())) || got["path"] != modelPath {
		t.Fatalf("ready model status = %#v", got)
	}
}

func TestDiarizationEnabledDefaultsToTrue(t *testing.T) {
	if !corelib.AppConfigDefaults().DiarizationEnabled {
		t.Fatal("DiarizationEnabled = false, want true by default")
	}
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{DiarizationEnabled: true}}
	if !app.GetDiarizationEnabled() {
		t.Fatal("GetDiarizationEnabled = false")
	}
}

func TestResetCAMPlusModelOnlyDropsMatchingCache(t *testing.T) {
	camPlusMMMu.Lock()
	prevMM := camPlusMM
	camPlusMMMu.Unlock()
	prevPath, _ := camPlusLoadedPath.Load().(string)
	t.Cleanup(func() {
		camPlusMMMu.Lock()
		camPlusMM = prevMM
		camPlusMMMu.Unlock()
		camPlusLoadedPath.Store(prevPath)
	})

	loadedPath := "D:/models/current.cmpg"
	camPlusMMMu.Lock()
	camPlusMM = modelmanager.New(modelmanager.Config[*diarization.CAMPlus]{
		Name:        "cam++-test",
		Load:        func() (*diarization.CAMPlus, error) { return &diarization.CAMPlus{}, nil },
		UnloadDelay: time.Hour,
	})
	camPlusMMMu.Unlock()
	camPlusLoadedPath.Store(loadedPath)
	if _, done, err := camPlusMM.Acquire(); err != nil {
		t.Fatalf("plant CAM++ model: %v", err)
	} else {
		done()
	}

	resetCAMPlusModel("D:/models/other.cmpg")
	if !camPlusMM.Loaded() {
		t.Fatal("unrelated model replacement cleared cached CAM++ instance")
	}
	resetCAMPlusModel(loadedPath)
	if camPlusMM.Loaded() {
		t.Fatal("matching model replacement did not clear cached CAM++ instance")
	}
}

func TestDownloadDiarizationModelFallsBackToHubAndResumes(t *testing.T) {
	dir := withDiarizationTestModelsDir(t)
	content := string(testCAMPlusHeader()) + " test model content"
	var sawRange atomic.Bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models/"+diarization.DefaultCAMPlusFilename {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Range") != "bytes=5-" {
			t.Fatalf("Range = %q, want bytes=5-", r.Header.Get("Range"))
		}
		sawRange.Store(true)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 5-%d/%d", len(content)-1, len(content)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte(content[5:]))
	}))
	defer hub.Close()

	previousURL := diarizationModelDefaultURL
	diarizationModelDefaultURL = "http://127.0.0.1:1/not-available"
	t.Cleanup(func() { diarizationModelDefaultURL = previousURL })

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, diarization.DefaultCAMPlusFilename)
	if err := os.WriteFile(dest+".tmp", []byte(content[:5]), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{RemoteHubURL: hub.URL + "/"}}
	if err := app.DownloadDiarizationModel(); err != nil {
		t.Fatalf("DownloadDiarizationModel: %v", err)
	}
	if !sawRange.Load() {
		t.Fatal("Hub fallback did not resume the cached temporary file")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("downloaded content = %q, want %q", got, content)
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains after download: %v", err)
	}
}
