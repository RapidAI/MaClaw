package embedding

import (
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInflightGuardCloseWaitsAndRejects(t *testing.T) {
	var g inflightGuard
	var running, maxSeen atomic.Int32
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if !g.acquire() {
					return
				}
				// nested acquire must not block even while Close waits
				if g.acquire() {
					g.release()
				}
				n := running.Add(1)
				if n > maxSeen.Load() {
					maxSeen.Store(n)
				}
				time.Sleep(time.Millisecond)
				running.Add(-1)
				g.release()
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	if !g.close() {
		t.Fatal("first close returned false")
	}
	if n := running.Load(); n != 0 {
		t.Fatalf("close returned with %d calls in flight", n)
	}
	if g.acquire() {
		t.Fatal("acquire succeeded after close")
	}
	if g.close() {
		t.Fatal("second close returned true")
	}
	close(stop)
	wg.Wait()
	if maxSeen.Load() < 2 {
		t.Logf("low concurrency observed (%d)", maxSeen.Load())
	}
}

func closeRaceIters() int {
	if n, err := strconv.Atoi(os.Getenv("MACLAW_CLOSE_RACE_ITERS")); err == nil && n > 0 {
		return n
	}
	return 3
}

func gemmaTestModel(t testing.TB) string {
	t.Helper()
	p := DefaultModelPath()
	if p == "" {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, ".maclaw", "models", DefaultModelFilename)
		}
	}
	if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
		t.Skipf("EmbeddingGemma model not found (%s)", p)
	}
	return p
}

// hammerThenClose runs 8 goroutines over every inference entry point, closes
// the embedder mid-flight, and checks that nothing crashes, Close returns,
// and later calls fail with ErrEmbedderClosed.
func hammerThenClose(t *testing.T, emb Embedder, delay time.Duration) {
	t.Helper()
	texts := []string{"帮我把这个文件发给张三", "open the quarterly report and summarise it for me please", "查询杭州明天天气", "x"}
	var wg sync.WaitGroup
	var calls, closedErrs, otherErrs atomic.Int64
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; ; i++ {
				var err error
				switch (w + i) % 4 {
				case 0:
					_, err = emb.EmbedBatch(texts)
				case 1:
					_, err = EmbedAs(emb, texts[i%len(texts)], RoleClassification)
				case 2:
					_, err = EmbedBatchAs(emb, texts, RoleClassification)
				default:
					_, err = emb.Embed(texts[i%len(texts)])
				}
				calls.Add(1)
				if err != nil {
					if errors.Is(err, ErrEmbedderClosed) {
						closedErrs.Add(1)
						return
					}
					otherErrs.Add(1)
					return
				}
			}
		}(w)
	}
	time.Sleep(delay)
	done := make(chan struct{})
	go func() { emb.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("Close did not return within 60s")
	}
	wg.Wait()
	if otherErrs.Load() > 0 {
		t.Fatalf("%d calls failed with an error other than ErrEmbedderClosed", otherErrs.Load())
	}
	if _, err := emb.Embed("after close"); !errors.Is(err, ErrEmbedderClosed) {
		t.Fatalf("Embed after Close: err=%v, want ErrEmbedderClosed", err)
	}
	if _, err := emb.EmbedBatch(texts); err == nil {
		t.Fatal("EmbedBatch after Close succeeded")
	}
	t.Logf("delay=%s calls=%d closed-errors=%d", delay, calls.Load(), closedErrs.Load())
}

func TestGemmaCloseDuringConcurrentEmbeds(t *testing.T) {
	path := gemmaTestModel(t)
	for i := 0; i < closeRaceIters(); i++ {
		emb, err := NewGemmaEmbedder(path, DefaultEmbeddingDim)
		if err != nil {
			t.Skipf("gemma unavailable: %v", err)
		}
		hammerThenClose(t, emb, time.Duration(rand.Intn(300))*time.Millisecond)
	}
}

func TestQwen3CloseDuringConcurrentEmbeds(t *testing.T) {
	path := qwen3TestModel(t)
	for i := 0; i < closeRaceIters(); i++ {
		emb, err := NewQwen3Embedder(path, 0)
		if err != nil {
			t.Fatal(err)
		}
		hammerThenClose(t, emb, time.Duration(rand.Intn(300))*time.Millisecond)
	}
}
