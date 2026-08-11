package browser

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// errNoOCRBackend is returned when a VisionFirstOCRProvider has neither a
// vision channel nor a local fallback engine configured.
var errNoOCRBackend = errors.New("no OCR backend configured")

// Vision-first circuit breaker defaults: after visionCBMaxFailures consecutive
// vision errors the channel is skipped for visionCBCooldown so a hung or
// broken endpoint cannot add its full timeout to every recognition call.
const (
	visionCBMaxFailures = 3
	visionCBCooldown    = 5 * time.Minute
)

// VisionFirstOCRProvider prefers a multimodal LLM for screenshot text
// recognition and falls back to the local OCR engine (PP-OCRv6) when the
// vision channel is absent or fails. This implements the "use the model's own
// vision when it supports images; use OCR only otherwise" policy without
// changing any of the call sites' pipelines.
//
// A small circuit breaker remembers consecutive vision failures: once the
// threshold is hit, Recognize goes straight to the local engine until the
// cooldown elapses (half-open: a single vision attempt decides whether the
// breaker closes again).
type VisionFirstOCRProvider struct {
	vision   *LLMVisionProvider // nil means no vision channel configured
	fallback OCRProvider        // local PP-OCRv6 engine
	logf     func(format string, args ...interface{})

	cbMu         sync.Mutex
	cbFailures   int       // consecutive vision failures
	cbOpenUntil  time.Time // vision skipped while now < cbOpenUntil
	cbProbing    bool      // half-open probe in flight (single-flight token)
	cbMaxFailure int       // override in tests; zero → default
	cbCooldown   time.Duration
	now          func() time.Time // override in tests; nil → time.Now
}

// NewVisionFirstOCRProvider creates a provider that tries vision first and
// falls back to the local OCR engine. vision may be nil (fallback only);
// fallback may be nil (vision only). logf may be nil.
func NewVisionFirstOCRProvider(vision *LLMVisionProvider, fallback OCRProvider, logf func(format string, args ...interface{})) *VisionFirstOCRProvider {
	return &VisionFirstOCRProvider{vision: vision, fallback: fallback, logf: logf}
}

// Recognize implements OCRProvider: vision LLM first, local OCR on failure.
func (p *VisionFirstOCRProvider) Recognize(pngBase64 string) ([]OCRResult, error) {
	if p.vision != nil && p.visionAllowed() {
		results, err := p.vision.Recognize(pngBase64)
		if err == nil {
			p.recordVisionSuccess()
			return results, nil
		}
		// ErrVisionUnsupported means the active model simply has no image
		// capability — the expected steady state for text-only models, not an
		// endpoint failure. Skip the breaker and the failure log, or every
		// recognition on a text-only model would churn the circuit and spam
		// the log while always landing on the local engine anyway.
		if !errors.Is(err, ErrVisionUnsupported) {
			p.recordVisionFailure()
			if p.logf != nil {
				p.logf("vision OCR failed, falling back to local OCR: %v", err)
			}
		} else {
			// A half-open probe token may be checked out; release it without
			// touching the breaker so a later switch to a vision-capable model
			// is not starved by a stuck cbProbing flag.
			p.releaseVisionProbe()
		}
	}
	if p.fallback != nil {
		return p.fallback.Recognize(pngBase64)
	}
	return nil, errNoOCRBackend
}

// visionAllowed reports whether the breaker lets a vision attempt through.
// When half-open it hands out a single probe token: only one caller tries the
// vision channel; concurrent callers go straight to the fallback instead of
// each paying a full endpoint timeout.
func (p *VisionFirstOCRProvider) visionAllowed() bool {
	p.cbMu.Lock()
	defer p.cbMu.Unlock()
	if p.cbOpenUntil.IsZero() {
		return true
	}
	if p.currentTimeLocked().Before(p.cbOpenUntil) {
		return false
	}
	// Cooldown elapsed: half-open — single-flight probe; recordVisionSuccess/
	// recordVisionFailure release the token and decide the next state.
	if p.cbProbing {
		return false
	}
	p.cbProbing = true
	return true
}

func (p *VisionFirstOCRProvider) recordVisionSuccess() {
	p.cbMu.Lock()
	defer p.cbMu.Unlock()
	p.cbFailures = 0
	p.cbOpenUntil = time.Time{}
	p.cbProbing = false
}

// releaseVisionProbe returns a checked-out half-open probe token without
// changing the breaker state (used for ErrVisionUnsupported, which is a
// capability state rather than a vision-endpoint success or failure).
func (p *VisionFirstOCRProvider) releaseVisionProbe() {
	p.cbMu.Lock()
	p.cbProbing = false
	p.cbMu.Unlock()
}

func (p *VisionFirstOCRProvider) recordVisionFailure() {
	p.cbMu.Lock()
	probing := p.cbProbing
	p.cbProbing = false
	maxFailures := p.cbMaxFailure
	if maxFailures <= 0 {
		maxFailures = visionCBMaxFailures
	}
	cooldown := p.cbCooldown
	if cooldown <= 0 {
		cooldown = visionCBCooldown
	}
	var logMsg string
	// Half-open probe failed: re-open immediately instead of counting from
	// zero — otherwise a dead endpoint gets up to maxFailures full timeouts
	// every cooldown cycle instead of one.
	if probing || (!p.cbOpenUntil.IsZero() && !p.currentTimeLocked().Before(p.cbOpenUntil)) {
		p.cbOpenUntil = p.currentTimeLocked().Add(cooldown)
		p.cbFailures = 0
		logMsg = fmt.Sprintf("vision OCR half-open probe failed, circuit re-opened for %v", cooldown)
	} else {
		p.cbFailures++
		if p.cbFailures >= maxFailures {
			p.cbOpenUntil = p.currentTimeLocked().Add(cooldown)
			p.cbFailures = 0
			logMsg = fmt.Sprintf("vision OCR circuit open for %v after %d consecutive failures", cooldown, maxFailures)
		}
	}
	p.cbMu.Unlock()
	// Log after unlocking: cbMu is not reentrant and logf callbacks must never
	// be invoked while holding it.
	if logMsg != "" && p.logf != nil {
		p.logf("%s", logMsg)
	}
}

func (p *VisionFirstOCRProvider) currentTimeLocked() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// IsAvailable implements OCRProvider.
func (p *VisionFirstOCRProvider) IsAvailable() bool {
	if p.vision != nil && p.vision.IsAvailable() {
		return true
	}
	return p.fallback != nil && p.fallback.IsAvailable()
}

// Close implements OCRProvider as a no-op: the fallback is typically the
// process-wide shared OCR engine (sharedNativeOCRProvider) whose lifetime the
// app owns, so closing it from a wrapper would unload the engine for every
// other consumer. The vision channel holds no resources.
func (p *VisionFirstOCRProvider) Close() {}
