// ocrdbg runs the native Go PP-OCRv6 pipeline on one image and prints each
// detected box with its recognized text and confidence, for comparison
// against the PaddleOCR Python reference (.tmp/ocr-models/ref_ocr.py).
//
// Usage: go run ./corelib/ocr/cmd/ocrdbg [-models DIR] <image>
//
// The OCR_DET / OCR_REC env vars override the model file paths (e.g. to
// smoke-test the tiny or medium tiers instead of the default small tier).
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

func main() {
	models := flag.String("models", ".tmp/ocr-models", "directory containing ppocrv6_small_{det,rec}.onnx")
	cpuprofile := flag.String("cpuprofile", "", "write CPU profile of the Recognize call to this file")
	memprofile := flag.String("memprofile", "", "write cumulative allocs profile after the iterations to this file")
	iters := flag.Int("iters", 1, "repeat Recognize N times (report last result and total/avg time)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: ocrdbg [-models DIR] <image>")
		os.Exit(2)
	}

	detPath := os.Getenv("OCR_DET")
	if detPath == "" {
		detPath = filepath.Join(*models, ocr.DetModelFilename(ocr.DefaultModelTier))
	}
	recPath := os.Getenv("OCR_REC")
	if recPath == "" {
		recPath = filepath.Join(*models, ocr.RecModelFilename(ocr.DefaultModelTier))
	}
	eng, err := ocr.NewEngine(detPath, recPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load models:", err)
		os.Exit(1)
	}
	defer eng.Close()

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(1)
	}

	t0 := time.Now()
	if *cpuprofile != "" {
		pf, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cpuprofile:", err)
			os.Exit(1)
		}
		pprof.StartCPUProfile(pf)
		defer func() {
			pprof.StopCPUProfile()
			pf.Close()
		}()
	}
	var results []ocr.Result
	var mem0 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&mem0)
	for it := 0; it < *iters; it++ {
		results, err = eng.Recognize(img)
		if err != nil {
			fmt.Fprintln(os.Stderr, "recognize:", err)
			os.Exit(1)
		}
	}
	var mem1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&mem1)
	if *memprofile != "" {
		mf, err := os.Create(*memprofile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "memprofile:", err)
			os.Exit(1)
		}
		pprof.Lookup("allocs").WriteTo(mf, 0)
		mf.Close()
	}
	for i, r := range results {
		fmt.Printf("[%d] box=%v bbox=%v conf=%.4f text=%q\n", i, r.Box, r.BBox, r.Confidence, r.Text)
	}
	total := time.Since(t0)
	fmt.Printf("total: %d boxes in %v (avg %v over %d iters)\n", len(results), total, total/time.Duration(*iters), *iters)
	fmt.Printf("alloc: %v total, %d mallocs over %d iters (avg %v/iter)\n",
		fmtBytes(mem1.TotalAlloc-mem0.TotalAlloc), mem1.Mallocs-mem0.Mallocs, *iters,
		fmtBytes((mem1.TotalAlloc-mem0.TotalAlloc)/uint64(*iters)))
}

// fmtBytes formats a byte count in human units.
func fmtBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGiB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMiB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKiB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
