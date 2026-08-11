package knowledge

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSafeExtractDocumentImagesFailsClosedOnPanic(t *testing.T) {
	nodes, imageBytes, err := safeExtractDocumentImages(SourceKindDOCX, func() ([]DocumentNode, map[string][]byte, error) {
		panic("malformed Office image relationship")
	})
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("panic error = %v", err)
	}
	if len(nodes) != 0 || len(imageBytes) != 0 {
		t.Fatalf("panic retained image output: nodes=%#v bytes=%#v", nodes, imageBytes)
	}
}

func TestProcessOneExtractedImageCleansProvisionalAssetAfterDescriberPanic(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{imageAssets: assets, imageDescriber: panicImageDescriber{}}
	key := "embedded-image"
	source := Source{ID: "source"}
	node := DocumentNode{Metadata: map[string]string{
		"_image_bytes_key": key,
		MetaImageFormat:    "png",
	}}
	if _, ok := store.processOneExtractedImage(context.Background(), source, node, map[string][]byte{key: recoveryTestPNG(t)}); ok {
		t.Fatal("panic-safe image processing unexpectedly returned a node")
	}
	if _, statErr := os.Stat(assets.AssetDir(source.ID + "_" + key)); !os.IsNotExist(statErr) {
		t.Fatalf("provisional asset was not removed: %v", statErr)
	}
}

func TestProcessOneExtractedImageCleansProvisionalAssetOnCancelledDescribe(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{imageAssets: assets, imageDescriber: blockingImageDescriber{}, imageDescSem: make(chan struct{}, 1)}
	// Fill the description semaphore so processing saves the original asset,
	// then blocks before Describe. Cancellation must reclaim that provisional
	// asset instead of leaving a file with no database node.
	store.imageDescSem <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	key := "cancelled-image"
	source := Source{ID: "source"}
	node := DocumentNode{Metadata: map[string]string{"_image_bytes_key": key, MetaImageFormat: "png"}}
	finished := make(chan bool, 1)
	go func() {
		_, ok := store.processOneExtractedImage(ctx, source, node, map[string][]byte{key: recoveryTestPNG(t)})
		finished <- ok
	}()
	assetDir := assets.AssetDir(source.ID + "_" + key)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(assetDir); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("image asset was not saved before description wait")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case ok := <-finished:
		if ok {
			t.Fatal("cancelled image processing unexpectedly returned a node")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled image processing did not finish")
	}
	if _, statErr := os.Stat(assetDir); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled provisional asset was not removed: %v", statErr)
	}
}

func TestProcessStandaloneImageCleansProvisionalAssetAfterDescriberPanic(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{imageAssets: assets, imageDescriber: panicImageDescriber{}}
	path := filepath.Join(t.TempDir(), "standalone.png")
	if err := os.WriteFile(path, recoveryTestPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "standalone-panic", Title: "Standalone"}
	if nodes := store.ProcessStandaloneImage(context.Background(), source, path, nil); len(nodes) != 0 {
		t.Fatalf("panic-safe standalone processing returned nodes: %#v", nodes)
	}
	if _, statErr := os.Stat(assets.AssetDir(source.ID)); !os.IsNotExist(statErr) {
		t.Fatalf("standalone provisional asset was not removed: %v", statErr)
	}
}

func TestProcessStandaloneImageCleansProvisionalAssetOnCancelledDescribe(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{imageAssets: assets, imageDescriber: blockingImageDescriber{}, imageDescSem: make(chan struct{}, 1)}
	store.imageDescSem <- struct{}{}
	path := filepath.Join(t.TempDir(), "standalone-cancelled.png")
	if err := os.WriteFile(path, recoveryTestPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "standalone-cancelled", Title: "Standalone"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan []DocumentNode, 1)
	go func() { finished <- store.ProcessStandaloneImage(ctx, source, path, nil) }()
	assetDir := assets.AssetDir(source.ID)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(assetDir); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("standalone asset was not saved before description wait")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case nodes := <-finished:
		if len(nodes) != 0 {
			t.Fatalf("cancelled standalone processing returned nodes: %#v", nodes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled standalone processing did not finish")
	}
	if _, statErr := os.Stat(assetDir); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled standalone provisional asset was not removed: %v", statErr)
	}
}

type panicImageDescriber struct{}

func (panicImageDescriber) Describe(context.Context, string, ImageHints) (ImageDescription, error) {
	panic("describer failure")
}

func (panicImageDescriber) Close() {}

type blockingImageDescriber struct{}

func (blockingImageDescriber) Describe(context.Context, string, ImageHints) (ImageDescription, error) {
	return ImageDescription{}, nil
}

func (blockingImageDescriber) Close() {}

func recoveryTestPNG(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "image.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 80, B: 180, A: 255})
		}
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
