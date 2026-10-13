package locall3

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// First run: the model is still downloading, so L3 is remote (mode on) or
// ErrNotReady (strict). When the verified model hot-loads, Attach switches
// every in-flight / later call to the local head atomically; a Detach (model
// closed) switches back. Run with -race when cgo is available.
func TestHotSwapUnderConcurrentRequests(t *testing.T) {
	t.Setenv(intent.LocalHeadEnv, "")
	t.Setenv(intent.LocalTauSEnv, "")
	dir := t.TempDir()
	writeHead(t, dir, intent.ShippedLocalHeadName, "", 1024, nil)
	for _, mode := range []string{intent.LocalL3On, intent.LocalL3Strict} {
		t.Run(mode, func(t *testing.T) {
			rt := NewWithMode(dir, mode)
			var remote, local, notReady, bad atomic.Int64
			remoteFn := func(context.Context, context.Context, string, string) (string, error) {
				remote.Add(1)
				return `{"top":[{"skill":"web_search","score":0.9}]}`, nil
			}
			fn := rt.ContextFunc(remoteFn)
			stop := make(chan struct{})
			var wg sync.WaitGroup
			for g := 0; g < 16; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						select {
						case <-stop:
							return
						default:
						}
						out, err := fn(context.Background(), context.Background(), "sys", "open the file")
						switch {
						case errors.Is(err, ErrNotReady):
							notReady.Add(1)
						case err != nil:
							bad.Add(1)
						case strings.Contains(out, "file_ops"):
							local.Add(1)
						case strings.Contains(out, "web_search"):
						default:
							bad.Add(1)
						}
					}
				}()
			}
			time.Sleep(20 * time.Millisecond) // "downloading" phase
			if h, _ := rt.Active(); h != nil {
				t.Fatal("head attached before the model was loaded")
			}
			if _, err := rt.Attach(qwen); err != nil { // hot-load
				t.Fatal(err)
			}
			before := local.Load()
			time.Sleep(20 * time.Millisecond)
			rt.Detach() // embedder closed / swapped
			time.Sleep(10 * time.Millisecond)
			if _, err := rt.Attach(qwen); err != nil {
				t.Fatal(err)
			}
			time.Sleep(10 * time.Millisecond)
			close(stop)
			wg.Wait()
			if bad.Load() != 0 {
				t.Fatalf("bad results: %d", bad.Load())
			}
			if local.Load() == before || local.Load() == 0 {
				t.Fatal("local head never served after hot-load")
			}
			if mode == intent.LocalL3Strict && remote.Load() != 0 {
				t.Fatalf("strict mode called remote %d times", remote.Load())
			}
			if mode == intent.LocalL3On && remote.Load() == 0 {
				t.Fatal("mode on: remote should serve while the model is missing")
			}
			if mode == intent.LocalL3Strict && notReady.Load() == 0 {
				t.Fatal("strict: ErrNotReady expected while the model is missing")
			}
		})
	}
}
