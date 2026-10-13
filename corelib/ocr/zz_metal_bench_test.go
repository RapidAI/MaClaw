package ocr

// OCR latency / output harness (skipped unless ZZ_OCR_MODELS is set):
//
//	ZZ_OCR_MODELS=<dir with ppocrv6_<tier>_{det,rec}.onnx> ZZ_OCR_IMAGES=<dir> [ZZ_OCR_TIER=small] [ZZ_OCR_OUT=file.json] go test -run TestZZOCRBench -v

import (
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
)

func TestZZOCRBench(t *testing.T) {
	dir := os.Getenv("ZZ_OCR_MODELS")
	if dir == "" {
		t.Skip("ZZ_OCR_MODELS not set")
	}
	tier := os.Getenv("ZZ_OCR_TIER")
	if tier == "" {
		tier = DefaultModelTier
	}
	t0 := time.Now()
	e, err := NewEngine(filepath.Join(dir, DetModelFilename(tier)), filepath.Join(dir, RecModelFilename(tier)))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	t.Logf("RESULT load_ms=%.0f rss_MB=%s", float64(time.Since(t0).Microseconds())/1000, zzRSS())
	var files []string
	for _, ext := range []string{"*.png", "*.jpg"} {
		m, _ := filepath.Glob(filepath.Join(os.Getenv("ZZ_OCR_IMAGES"), ext))
		files = append(files, m...)
	}
	sort.Strings(files)
	all := map[string][]Result{}
	var total time.Duration
	for i, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := image.Decode(fh)
		fh.Close()
		if err != nil {
			t.Fatal(err)
		}
		e.Recognize(img) // warm
		if i == 0 {
			onnxrt.ProfileReport()
		}
		var ds []time.Duration
		var res []Result
		for k := 0; k < 3; k++ {
			t1 := time.Now()
			res, err = e.Recognize(img)
			if err != nil {
				t.Fatal(err)
			}
			ds = append(ds, time.Since(t1))
		}
		sort.Slice(ds, func(a, b int) bool { return ds[a] < ds[b] })
		total += ds[1]
		all[filepath.Base(f)] = res
		b := img.Bounds()
		t.Logf("%-28s %4dx%-4d boxes=%3d %8.1f ms", filepath.Base(f), b.Dx(), b.Dy(), len(res), float64(ds[1].Microseconds())/1000)
	}
	t.Logf("RESULT images=%d total_ms=%.1f rss_MB=%s", len(files), float64(total.Microseconds())/1000, zzRSS())
	if os.Getenv("ONNXRT_PROFILE") == "1" {
		t.Logf("profile:\n%s", onnxrt.ProfileReport())
	}
	if out := os.Getenv("ZZ_OCR_OUT"); out != "" {
		b, _ := json.MarshalIndent(all, "", " ")
		os.WriteFile(out, b, 0o644)
	}
}

func zzRSS() string {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return "?"
	}
	kb, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return strconv.Itoa(kb / 1024)
}
