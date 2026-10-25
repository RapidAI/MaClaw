package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"testing"
	"time"
)

func TestMermaidRasterSkipsWhenTheWindowCannotDraw(t *testing.T) {
	app := &App{}
	start := time.Now()
	_, err := app.rasterMermaidPNG("flowchart LR\n  A --> B")
	if err == nil {
		t.Fatal("expected the fence to stay source when the window is not ready")
	}
	if time.Since(start) > time.Second {
		t.Fatal("a window that cannot draw must not wait out the raster timeout")
	}
}

func TestMermaidRasterReadyUnblocksAWaitingDocument(t *testing.T) {
	app := &App{}
	go func() {
		time.Sleep(30 * time.Millisecond)
		app.MermaidRasterReady()
	}()
	if !app.waitMermaidRasterReady(time.Second) {
		t.Fatal("the document kept waiting after the window could draw")
	}
}

func TestMermaidRasterWaitsOnceForTheWindow(t *testing.T) {
	app := &App{}
	app.ctx = context.WithValue(context.Background(), "events", true)
	mermaidRasterReadyWait = 200 * time.Millisecond
	t.Cleanup(func() { mermaidRasterReadyWait = 3 * time.Second })

	start := time.Now()
	if _, err := app.rasterMermaidPNG("flowchart LR\n  A --> B"); err == nil {
		t.Fatal("expected the fence to stay source")
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("the first diagram did not wait for the window")
	}
	start = time.Now()
	if _, err := app.rasterMermaidPNG("flowchart LR\n  C --> D"); err == nil {
		t.Fatal("expected the fence to stay source")
	}
	if waited := time.Since(start); waited > 50*time.Millisecond {
		t.Fatalf("the next diagram waited %s after the window had already missed the first", waited)
	}
}

func TestMermaidRasterPauseSkipsTheNextDiagramAfterATimeout(t *testing.T) {
	app := &App{}
	app.ctx = context.WithValue(context.Background(), "events", true)
	app.mermaidRasterReady.Store(true)
	mermaidPDFRasterWait = 40 * time.Millisecond
	t.Cleanup(func() { mermaidPDFRasterWait = 30 * time.Second })

	start := time.Now()
	if _, err := app.rasterMermaidPNG("flowchart LR\n  A --> B"); err == nil {
		t.Fatal("expected a timeout when the window does not answer")
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("the timeout returned before the draw was given time")
	}
	start = time.Now()
	if _, err := app.rasterMermaidPNG("flowchart LR\n  C --> D"); err == nil {
		t.Fatal("expected the next diagram to stay source")
	}
	if waited := time.Since(start); waited > 20*time.Millisecond {
		t.Fatalf("the next diagram waited %s after a timeout", waited)
	}
	if _, ok := app.cachedMermaidPNG("flowchart LR\n  A --> B"); ok {
		t.Fatal("a timed-out diagram was reused")
	}
}

func TestMermaidRasterReusesAPictureAlreadyDrawn(t *testing.T) {
	app := &App{}
	app.mermaidRasterPauseUntil.Store(time.Now().Add(time.Minute).UnixNano())
	pngBytes := solidPNG(t)
	const source = "flowchart LR\n  A --> B"
	app.rememberMermaidPNG(source, pngBytes)

	start := time.Now()
	got, err := app.rasterMermaidPNG(source)
	if err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatalf("cached picture len=%d err=%v", len(got), err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("a picture already drawn waited on the window")
	}
}

func TestMermaidRasterDoesNotRememberAPictureThePDFCannotEmbed(t *testing.T) {
	app := &App{}
	const source = "flowchart LR\n  A --> B"
	app.rememberMermaidPNG(source, []byte("png"))
	if _, ok := app.cachedMermaidPNG(source); ok {
		t.Fatal("bytes that are not a picture were remembered")
	}
	torn := solidPNG(t)
	torn[len(torn)-13] ^= 0xff
	app.rememberMermaidPNG(source, torn)
	if _, ok := app.cachedMermaidPNG(source); ok {
		t.Fatal("a png with a broken chunk was remembered")
	}
	good := solidPNG(t)
	app.rememberMermaidPNG(source, good)
	got, ok := app.cachedMermaidPNG(source)
	if !ok || !bytes.Equal(got, good) {
		t.Fatal("a picture the PDF can embed was dropped")
	}
}

func TestMermaidRasterRejectsInvalidTextWithoutWaiting(t *testing.T) {
	app := &App{}
	app.ctx = context.WithValue(context.Background(), "events", true)
	app.mermaidRasterReady.Store(true)
	start := time.Now()
	if _, err := app.rasterMermaidPNG("flowchart LR\n  A[\xff] --> B"); err == nil {
		t.Fatal("expected invalid text to stay source")
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("invalid text waited on the window")
	}
}

func TestMermaidRasterCacheDropsTheOldestPicture(t *testing.T) {
	app := &App{}
	pngBytes := solidPNG(t)
	for i := 0; i < mermaidRasterCacheEntries; i++ {
		app.rememberMermaidPNG(strconv.Itoa(i), pngBytes)
	}
	app.rememberMermaidPNG("new", pngBytes)
	if _, ok := app.cachedMermaidPNG("0"); ok {
		t.Fatal("the oldest picture was kept past the cap")
	}
	got, ok := app.cachedMermaidPNG("new")
	if !ok || !bytes.Equal(got, pngBytes) {
		t.Fatal("the newest picture was dropped")
	}
}

func TestSubmitMermaidRasterDeliversThePicture(t *testing.T) {
	app := &App{}
	pngBytes := solidPNG(t)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	ch := make(chan mermaidRasterResult, 1)
	app.mermaidRasterWait.Store("9", ch)
	app.SubmitMermaidRaster("9", dataURL)
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatal(res.err)
		}
		img, _, err := image.Decode(bytes.NewReader(res.png))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 3 {
			t.Fatalf("picture size = %v", img.Bounds())
		}
	case <-time.After(time.Second):
		t.Fatal("the picture was not delivered")
	}
}

func TestSubmitMermaidRasterIgnoresALateOrEmptyPicture(t *testing.T) {
	app := &App{}
	app.SubmitMermaidRaster("missing", "data:image/png;base64,AAAA")
	ch := make(chan mermaidRasterResult, 1)
	app.mermaidRasterWait.Store("10", ch)
	app.SubmitMermaidRaster("10", "not-a-picture")
	select {
	case res := <-ch:
		if res.err == nil || len(res.png) != 0 {
			t.Fatalf("bad payload was accepted: %+v", res)
		}
	case <-time.After(time.Second):
		t.Fatal("the failure was not delivered")
	}
}

func TestPngBytesFromDataURLRejectsNonImages(t *testing.T) {
	if _, err := pngBytesFromDataURL("data:text/plain;base64,QQ=="); err == nil {
		t.Fatal("a text payload was accepted")
	}
	if _, err := pngBytesFromDataURL(""); err == nil {
		t.Fatal("an empty payload was accepted")
	}
	got, err := pngBytesFromDataURL("data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png")))
	if err != nil || string(got) != "png" {
		t.Fatalf("png bytes = %q err=%v", got, err)
	}
}

func solidPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
