package browser

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type stubOCRProvider struct {
	results []OCRResult
	err     error
	calls   int
}

func (s *stubOCRProvider) Recognize(string) ([]OCRResult, error) {
	s.calls++
	return s.results, s.err
}
func (s *stubOCRProvider) IsAvailable() bool { return true }
func (s *stubOCRProvider) Close()            {}

func TestVisionFirstOCRProvider_PrefersVision(t *testing.T) {
	vision := NewLLMVisionProvider(func(_, _ string) (string, error) {
		return `[{"text":"hello","confidence":0.9,"bbox":[1,2,3,4]}]`, nil
	})
	fallback := &stubOCRProvider{results: []OCRResult{{Text: "native"}}}
	p := NewVisionFirstOCRProvider(vision, fallback, nil)

	got, err := p.Recognize("png")
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(got) != 1 || got[0].Text != "hello" {
		t.Fatalf("got %+v, want vision result", got)
	}
	if fallback.calls != 0 {
		t.Fatalf("fallback called %d times, want 0", fallback.calls)
	}
}

func TestVisionFirstOCRProvider_FallsBackOnVisionError(t *testing.T) {
	vision := NewLLMVisionProvider(func(_, _ string) (string, error) {
		return "", errors.New("model does not support vision")
	})
	fallback := &stubOCRProvider{results: []OCRResult{{Text: "native"}}}
	p := NewVisionFirstOCRProvider(vision, fallback, nil)

	got, err := p.Recognize("png")
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(got) != 1 || got[0].Text != "native" {
		t.Fatalf("got %+v, want fallback result", got)
	}
	if fallback.calls != 1 {
		t.Fatalf("fallback called %d times, want 1", fallback.calls)
	}
}

func TestVisionFirstOCRProvider_NilVisionUsesFallback(t *testing.T) {
	fallback := &stubOCRProvider{results: []OCRResult{{Text: "native"}}}
	p := NewVisionFirstOCRProvider(nil, fallback, nil)

	got, err := p.Recognize("png")
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(got) != 1 || got[0].Text != "native" {
		t.Fatalf("got %+v, want fallback result", got)
	}
}

func TestVisionFirstOCRProvider_NoBackend(t *testing.T) {
	p := NewVisionFirstOCRProvider(nil, nil, nil)
	if _, err := p.Recognize("png"); err == nil {
		t.Fatal("want error when no backend configured")
	}
	if p.IsAvailable() {
		t.Fatal("IsAvailable = true with no backend")
	}
}

func TestVisionFirstOCRProvider_IsAvailable(t *testing.T) {
	vision := NewLLMVisionProvider(func(_, _ string) (string, error) { return "", nil })
	if !NewVisionFirstOCRProvider(vision, nil, nil).IsAvailable() {
		t.Fatal("IsAvailable = false with vision only")
	}
	if !NewVisionFirstOCRProvider(nil, &stubOCRProvider{}, nil).IsAvailable() {
		t.Fatal("IsAvailable = false with fallback only")
	}
	if NewVisionFirstOCRProvider(NewLLMVisionProvider(nil), nil, nil).IsAvailable() {
		t.Fatal("IsAvailable = true with nil sendImage and no fallback")
	}
}

func TestVisionFirstOCRProvider_CircuitBreaker(t *testing.T) {
	var visionCalls int
	vision := NewLLMVisionProvider(func(_, _ string) (string, error) {
		visionCalls++
		return "", errors.New("endpoint hung")
	})
	fallback := &stubOCRProvider{results: []OCRResult{{Text: "native"}}}
	p := NewVisionFirstOCRProvider(vision, fallback, nil)
	now := time.Now()
	p.now = func() time.Time { return now }
	p.cbMaxFailure = 2
	p.cbCooldown = time.Minute

	// Two failures open the breaker.
	for i := 0; i < 2; i++ {
		if _, err := p.Recognize("png"); err != nil {
			t.Fatalf("Recognize: %v", err)
		}
	}
	if visionCalls != 2 {
		t.Fatalf("visionCalls = %d, want 2", visionCalls)
	}
	// Breaker open: vision skipped entirely.
	if _, err := p.Recognize("png"); err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if visionCalls != 2 {
		t.Fatalf("vision called during cooldown: visionCalls = %d", visionCalls)
	}
	// Cooldown elapsed: half-open attempt goes through.
	now = now.Add(2 * time.Minute)
	if _, err := p.Recognize("png"); err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if visionCalls != 3 {
		t.Fatalf("half-open attempt did not reach vision: visionCalls = %d", visionCalls)
	}
}

func TestVisionFirstOCRProvider_VisionSuccessResetsBreaker(t *testing.T) {
	var fail bool
	vision := NewLLMVisionProvider(func(_, _ string) (string, error) {
		if fail {
			return "", errors.New("boom")
		}
		return `[{"text":"ok","confidence":0.9,"bbox":[0,0,1,1]}]`, nil
	})
	p := NewVisionFirstOCRProvider(vision, &stubOCRProvider{results: []OCRResult{{Text: "native"}}}, nil)
	p.cbMaxFailure = 2

	fail = true
	if _, err := p.Recognize("x"); err != nil {
		t.Fatal(err)
	}
	fail = false
	if _, err := p.Recognize("x"); err != nil {
		t.Fatal(err)
	}
	fail = true
	// One more failure: counter was reset by the success, breaker stays closed.
	if _, err := p.Recognize("x"); err != nil {
		t.Fatal(err)
	}
	p.cbMu.Lock()
	open := !p.cbOpenUntil.IsZero()
	p.cbMu.Unlock()
	if open {
		t.Fatal("breaker opened despite intervening success")
	}
}

func TestVisionFirstOCRProvider_HalfOpenFailureReopens(t *testing.T) {
	var visionCalls int
	vision := NewLLMVisionProvider(func(_, _ string) (string, error) {
		visionCalls++
		return "", errors.New("endpoint still dead")
	})
	p := NewVisionFirstOCRProvider(vision, &stubOCRProvider{results: []OCRResult{{Text: "native"}}}, nil)
	now := time.Now()
	p.now = func() time.Time { return now }
	p.cbMaxFailure = 2
	p.cbCooldown = time.Minute

	// Open the breaker with two failures.
	for i := 0; i < 2; i++ {
		_, _ = p.Recognize("x")
	}
	// Cooldown elapses; the half-open probe fails → breaker must re-open.
	now = now.Add(2 * time.Minute)
	_, _ = p.Recognize("x")
	if visionCalls != 3 {
		t.Fatalf("visionCalls = %d, want 3", visionCalls)
	}
	// Immediately afterwards the breaker must be closed again (no new probe).
	now = now.Add(time.Second)
	_, _ = p.Recognize("x")
	if visionCalls != 3 {
		t.Fatalf("half-open failure did not re-open the breaker: visionCalls = %d", visionCalls)
	}
}

func TestVisionFirstOCRProvider_HalfOpenSingleFlight(t *testing.T) {
	release := make(chan struct{})
	probeStarted := make(chan struct{})
	var startOnce sync.Once
	var visionCalls int32
	var mu sync.Mutex
	vision := NewLLMVisionProvider(func(_, _ string) (string, error) {
		mu.Lock()
		visionCalls++
		call := visionCalls
		mu.Unlock()
		if call == 1 {
			return "", errors.New("dead") // initial failure opens the breaker
		}
		startOnce.Do(func() { close(probeStarted) })
		<-release // hold the half-open probe in flight
		return "", errors.New("dead")
	})
	p := NewVisionFirstOCRProvider(vision, &stubOCRProvider{results: []OCRResult{{Text: "native"}}}, nil)
	now := time.Now()
	p.now = func() time.Time { return now }
	p.cbMaxFailure = 1
	p.cbCooldown = time.Minute

	// Open the breaker.
	_, _ = p.Recognize("x")
	now = now.Add(2 * time.Minute)

	// First half-open caller holds the probe; wait until it is inside vision.
	done := make(chan struct{})
	go func() { _, _ = p.Recognize("x"); close(done) }()
	<-probeStarted

	// A concurrent caller must NOT reach vision while the probe is in flight.
	if _, err := p.Recognize("x"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	calls := visionCalls
	mu.Unlock()
	if calls != 2 {
		t.Fatalf("concurrent half-open caller reached vision: visionCalls = %d, want 2", calls)
	}
	close(release)
	<-done
}

func TestLLMVisionProvider_ParseFailureIsError(t *testing.T) {
	vision := NewLLMVisionProvider(func(_, _ string) (string, error) {
		return "这张图上有一些文字……", nil
	})
	if _, err := vision.Recognize("png"); err == nil {
		t.Fatal("unparseable vision response must return an error so callers can fall back")
	}
}

func TestVisionFirstOCRProvider_UnsupportedSkipsBreakerAndLog(t *testing.T) {
	vision := NewLLMVisionProvider(func(_, _ string) (string, error) {
		return "", fmt.Errorf("send image: %w", ErrVisionUnsupported)
	})
	fallback := &stubOCRProvider{results: []OCRResult{{Text: "native"}}}
	var logs int
	p := NewVisionFirstOCRProvider(vision, fallback, func(string, ...interface{}) { logs++ })
	p.cbMaxFailure = 1 // a single counted failure would open the breaker

	for i := 0; i < 3; i++ {
		got, err := p.Recognize("png")
		if err != nil {
			t.Fatalf("Recognize #%d: %v", i, err)
		}
		if len(got) != 1 || got[0].Text != "native" {
			t.Fatalf("Recognize #%d = %+v, want fallback result", i, got)
		}
	}
	if fallback.calls != 3 {
		t.Fatalf("fallback.calls = %d, want 3", fallback.calls)
	}
	p.cbMu.Lock()
	open := !p.cbOpenUntil.IsZero()
	failures := p.cbFailures
	p.cbMu.Unlock()
	if open || failures != 0 {
		t.Fatalf("capability error must not touch the breaker: open=%v failures=%d", open, failures)
	}
	if logs != 0 {
		t.Fatalf("capability error logged %d times, want 0", logs)
	}
}

func TestVisionFirstOCRProvider_UnsupportedReleasesHalfOpenProbe(t *testing.T) {
	var visionCalls int
	unsupported := false
	vision := NewLLMVisionProvider(func(_, _ string) (string, error) {
		visionCalls++
		if unsupported {
			return "", ErrVisionUnsupported
		}
		return "", errors.New("endpoint dead")
	})
	p := NewVisionFirstOCRProvider(vision, &stubOCRProvider{results: []OCRResult{{Text: "native"}}}, nil)
	now := time.Now()
	p.now = func() time.Time { return now }
	p.cbMaxFailure = 1
	p.cbCooldown = time.Minute

	// Open the breaker with one real failure.
	_, _ = p.Recognize("x")
	// Cooldown elapses; the half-open probe hits "model has no vision".
	now = now.Add(2 * time.Minute)
	unsupported = true
	_, _ = p.Recognize("x")
	if visionCalls != 2 {
		t.Fatalf("half-open probe did not reach vision: visionCalls = %d, want 2", visionCalls)
	}
	p.cbMu.Lock()
	probing := p.cbProbing
	p.cbMu.Unlock()
	if probing {
		t.Fatal("capability error left the half-open probe token checked out")
	}
	// A later real failure must be able to probe again and re-open the breaker.
	unsupported = false
	_, _ = p.Recognize("x")
	if visionCalls != 3 {
		t.Fatalf("probe token stuck, vision not retried: visionCalls = %d, want 3", visionCalls)
	}
	p.cbMu.Lock()
	reopened := !p.cbOpenUntil.IsZero() && p.cbOpenUntil.After(now)
	p.cbMu.Unlock()
	if !reopened {
		t.Fatal("breaker did not re-open after post-probe real failure")
	}
}
