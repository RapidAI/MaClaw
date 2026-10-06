package intent

// shadow.go — intent "shadow mode": run the fully local L3 head next to the
// remote L3 tree classifier and log both verdicts, without changing routing.
//
// Off by default. When enabled, every ClassifyContext turn that reaches the
// Layer 3 decision point (remote tree called, skipped by the endpoint-failure
// window, or no L3 configured) — plus an optional random sample of turns
// Layer 2 settled on its own — produces one JSONL record. The hot path only
// captures references it already holds (L2 result, the L2 query vector, the
// remote candidates, the error and the remote latency) and does a
// non-blocking channel send after the turn's result is final. A single
// background worker per log file computes the L2 top-k, runs the local head
// on the reused L2 vector (no re-embedding), and appends the record. A full
// queue drops and counts; a panic in the head or writer is recovered and
// counted. Shadow mode never calls the tree callback, never touches the
// classifier cache or the endpoint-failure skip window, and never alters the
// returned ClassificationResult.
//
// Environment (read once in New; Config.Shadow takes precedence):
//
//	MACLAW_INTENT_SHADOW=1                 enable
//	MACLAW_INTENT_SHADOW_HEAD=<head.json>  local head (optional: without it only remote/L2 are logged)
//	MACLAW_INTENT_SHADOW_LOG=<file|dir>    default <maclaw data>/logs/intent-shadow/intent_shadow.jsonl
//	MACLAW_INTENT_SHADOW_TEXT=hash|none|full   default hash (salted HMAC-SHA256; salt stays next to the log)
//	MACLAW_INTENT_SHADOW_EMBED=1           also store the L2 query vector (float16, base64)
//	MACLAW_INTENT_SHADOW_L2_SAMPLE=0.05    fraction of L2-decided turns to log (default 0)
//	MACLAW_INTENT_SHADOW_MAX_MB=64         rotate at this size (default 64)
//	MACLAW_INTENT_SHADOW_BACKUPS=5         rotated files kept (default 5)
//	MACLAW_INTENT_SHADOW_QUEUE=256         bounded queue length (default 256)
//
// Logs are local files (0600, directory 0700) and are never sent anywhere.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Text logging modes.
const (
	ShadowTextHash = "hash"
	ShadowTextNone = "none"
	ShadowTextFull = "full"
)

// ShadowSchemaVersion is the "v" field of every record.
const ShadowSchemaVersion = 1

// ShadowConfig configures shadow mode. The zero value is disabled.
type ShadowConfig struct {
	Enabled        bool
	HeadPath       string     // loaded when Head is nil
	Head           *LocalHead // optional preloaded head
	LogPath        string     // file, or directory (→ intent_shadow.jsonl inside)
	TextMode       string     // hash (default) | none | full
	StoreEmbedding bool
	L2SampleRate   float64 // 0..1
	QueueSize      int     // default 256
	MaxBytes       int64   // default 64 MiB
	MaxBackups     int     // default 5
	TopK           int     // default 5
	// Salt overrides the on-disk salt for hash mode (tests).
	Salt []byte
}

// ShadowConfigFromEnv reads the MACLAW_INTENT_SHADOW* variables. ok is false
// when shadow mode is not enabled.
func ShadowConfigFromEnv() (cfg ShadowConfig, ok bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_INTENT_SHADOW"))) {
	case "1", "true", "on", "yes":
	default:
		return ShadowConfig{}, false
	}
	cfg.Enabled = true
	cfg.HeadPath = strings.TrimSpace(os.Getenv("MACLAW_INTENT_SHADOW_HEAD"))
	cfg.LogPath = strings.TrimSpace(os.Getenv("MACLAW_INTENT_SHADOW_LOG"))
	cfg.TextMode = strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_INTENT_SHADOW_TEXT")))
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_INTENT_SHADOW_EMBED"))) {
	case "1", "true", "on", "yes":
		cfg.StoreEmbedding = true
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv("MACLAW_INTENT_SHADOW_L2_SAMPLE")), 64); err == nil {
		cfg.L2SampleRate = v
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv("MACLAW_INTENT_SHADOW_MAX_MB")), 64); err == nil && v > 0 {
		cfg.MaxBytes = int64(v * (1 << 20))
	}
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MACLAW_INTENT_SHADOW_BACKUPS"))); err == nil && v >= 0 {
		cfg.MaxBackups = v
	}
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MACLAW_INTENT_SHADOW_QUEUE"))); err == nil && v > 0 {
		cfg.QueueSize = v
	}
	return cfg, true
}

// DefaultShadowLogDir is where records go when no log path is configured.
func DefaultShadowLogDir() string {
	if models := embedding.DefaultModelsDir(); models != "" {
		return filepath.Join(filepath.Dir(models), "logs", "intent-shadow")
	}
	if dir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, "maclaw", "intent-shadow")
	}
	return filepath.Join(os.TempDir(), "maclaw-intent-shadow")
}

func (c ShadowConfig) normalized() (ShadowConfig, error) {
	switch c.TextMode {
	case "":
		c.TextMode = ShadowTextHash
	case ShadowTextHash, ShadowTextNone, ShadowTextFull:
	default:
		return c, fmt.Errorf("intent shadow: bad text mode %q (hash|none|full)", c.TextMode)
	}
	if c.L2SampleRate < 0 || math.IsNaN(c.L2SampleRate) {
		c.L2SampleRate = 0
	}
	if c.L2SampleRate > 1 {
		c.L2SampleRate = 1
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 256
	}
	if c.MaxBytes <= 0 {
		c.MaxBytes = 64 << 20
	}
	if c.MaxBackups < 0 {
		c.MaxBackups = 0
	} else if c.MaxBackups == 0 {
		c.MaxBackups = 5
	}
	if c.TopK <= 0 {
		c.TopK = 5
	}
	p := c.LogPath
	if p == "" {
		p = DefaultShadowLogDir()
	}
	if st, err := os.Stat(p); (err == nil && st.IsDir()) || strings.HasSuffix(p, string(os.PathSeparator)) || strings.HasSuffix(p, "/") || filepath.Ext(p) == "" {
		p = filepath.Join(p, "intent_shadow.jsonl")
	}
	abs, err := filepath.Abs(p)
	if err == nil {
		p = abs
	}
	c.LogPath = p
	return c, nil
}

// ShadowStats counts shadow records since the log was opened.
type ShadowStats struct {
	Enqueued  uint64 // handed to the worker
	Written   uint64 // appended to the log
	Dropped   uint64 // queue full (never blocks the turn)
	Errors    uint64 // marshal/write failures and recovered panics
	SampledL2 uint64 // L2-decided turns selected by the sample rate
}

// ---------------------------------------------------------------------------
// Log record schema (v1). Field names are part of the analysis contract
// (scripts/intent_shadow_analyze.py).
// ---------------------------------------------------------------------------

type shadowRecord struct {
	V         int              `json:"v"`
	TS        string           `json:"ts"`
	TurnID    string           `json:"turn_id"`
	Kind      string           `json:"kind"` // "l3" (turn reached the L3 decision point) | "l2_sample"
	ModelID   string           `json:"model_id"`
	EmbedDim  int              `json:"embed_dim,omitempty"`
	HeadID    string           `json:"head_id,omitempty"`
	DecidedBy string           `json:"decided_by"` // l2 | l3 | l2_fallback | l2_short_lookup | none
	Final     shadowFinal      `json:"final"`
	L2        *shadowL2        `json:"l2,omitempty"`
	Remote    shadowRemote     `json:"remote"`
	Local     *shadowLocal     `json:"local,omitempty"`
	Agree     *bool            `json:"agree,omitempty"`          // local top-1 == remote top-1 (remote ok only)
	AgreeDec  *bool            `json:"agree_decision,omitempty"` // local accepted AND equals remote
	Sensitive shadowSensitive  `json:"sensitive"`
	Text      shadowText       `json:"text"`
	Emb       *shadowEmbedding `json:"emb,omitempty"`
}

type shadowFinal struct {
	Label    string   `json:"label"`
	Conf     float64  `json:"conf"`
	Layer    int      `json:"layer"`
	Degraded bool     `json:"degraded,omitempty"`
	Tools    []string `json:"tools,omitempty"` // tool names the intent resolved to (affinity)
}

type shadowL2Score struct {
	Label  string  `json:"label"`
	Raw    float64 `json:"raw"`    // max cosine in the embedder's own (prompted) space
	Mapped float64 `json:"mapped"` // quantile-mapped onto the raw-text scale L2 thresholds use
}

type shadowL2 struct {
	Primary   string          `json:"primary"`
	Conf      float64         `json:"conf"`
	Confident bool            `json:"confident"`
	TopK      []shadowL2Score `json:"topk,omitempty"`
}

type shadowCand struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

type shadowRemote struct {
	// Outcome: ok | empty | timeout | cancelled | http_5xx | http_4xx | protocol |
	// error | skipped_endpoint_window | not_configured | skipped_short_lookup | not_reached
	Outcome         string       `json:"outcome"`
	Called          bool         `json:"called"`
	Label           string       `json:"label,omitempty"`
	Score           float64      `json:"score,omitempty"`
	Candidates      []shadowCand `json:"candidates,omitempty"`
	LatencyMS       float64      `json:"latency_ms,omitempty"`
	EndpointFailure bool         `json:"endpoint_failure,omitempty"` // isTreeEndpointFailure(err): fed the skip window
	Error           string       `json:"error,omitempty"`            // only in text=full (may echo model output)
}

type shadowLocal struct {
	TopK      []LocalHeadScore `json:"topk,omitempty"`
	Label     string           `json:"label,omitempty"`
	P         float64          `json:"p,omitempty"`
	Decision  string           `json:"decision,omitempty"` // accept | ambiguous | unknown
	Threshold float64          `json:"threshold,omitempty"`
	Tau       float64          `json:"tau,omitempty"`
	TauS      float64          `json:"tau_s,omitempty"`
	LatencyUS float64          `json:"latency_us,omitempty"`
	Error     string           `json:"error,omitempty"` // no_head | no_l2_vector | dim_mismatch | panic
}

type shadowSensitive struct {
	Local  bool `json:"local"`
	Remote bool `json:"remote"`
}

type shadowText struct {
	Mode     string `json:"mode"`
	Hash     string `json:"hash,omitempty"`
	LenRunes int    `json:"len_runes"`
	Full     string `json:"full,omitempty"`
}

type shadowEmbedding struct {
	Dtype string `json:"dtype"` // "f16le"
	Dim   int    `json:"dim"`
	B64   string `json:"b64"`
}

// ---------------------------------------------------------------------------
// Per-turn capture (hot path: references and two clock reads only)
// ---------------------------------------------------------------------------

type shadowTurn struct {
	// L2
	l2Ran       bool
	l2Result    ClassificationResult
	l2Confident bool
	queryVec    []float32
	anchors     []intentAnchor
	cal         intentScoreCalibration
	modelID     string
	// L3
	reachedL3  bool // turn reached the L3 decision point
	remoteDone bool
	outcome    string
	candidates []TreeCandidate
	err        error
	latency    time.Duration
}

func (st *shadowTurn) noteL2(emb embedding.Embedder, anchors []intentAnchor, res ClassificationResult, confident bool, vec []float32) {
	st.l2Ran = true
	st.l2Result = res
	st.l2Confident = confident
	st.queryVec = vec
	st.anchors = anchors
	if len(vec) > 0 {
		st.cal = intentScoreCalibrationFor(emb)
	}
}

func (st *shadowTurn) noteRemote(ctx context.Context, cands []TreeCandidate, err error, d time.Duration) {
	st.reachedL3 = true
	st.remoteDone = true
	st.candidates = append([]TreeCandidate(nil), cands...)
	st.err = err
	st.latency = d
	// Classify now: the turn context may be cancelled after the turn ends.
	st.outcome = shadowRemoteOutcome(ctx, cands, err)
}

func (st *shadowTurn) noteRemoteSkipped(outcome string) {
	st.reachedL3 = true
	st.outcome = outcome
}

// ---------------------------------------------------------------------------
// Logger (one per log file, shared by every classifier in the process)
// ---------------------------------------------------------------------------

type shadowJob struct {
	turn   *shadowTurn
	text   string
	result ClassificationResult
	kind   string
	at     time.Time
	seq    uint64
}

type shadowLogger struct {
	cfg   ShadowConfig
	head  *LocalHead
	salt  []byte
	nonce string

	mu     sync.RWMutex // guards closed + queue close
	closed bool
	queue  chan shadowJob
	done   chan struct{}
	refs   int // guarded by shadowRegistryMu

	w *shadowRotatingWriter

	seq       atomic.Uint64
	enqueued  atomic.Uint64
	written   atomic.Uint64
	dropped   atomic.Uint64
	errs      atomic.Uint64
	sampledL2 atomic.Uint64

	// testHook runs in the worker before each record (tests: block / panic).
	testHook func()
}

var (
	shadowRegistryMu sync.Mutex
	shadowRegistry   = map[string]*shadowLogger{}
)

// acquireShadowLogger returns the process-wide logger for cfg.LogPath,
// creating it on first use (later configs for the same path share it).
func acquireShadowLogger(cfg ShadowConfig) (*shadowLogger, error) {
	cfg, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	shadowRegistryMu.Lock()
	defer shadowRegistryMu.Unlock()
	if l, ok := shadowRegistry[cfg.LogPath]; ok {
		l.refs++
		return l, nil
	}
	head := cfg.Head
	if head == nil && cfg.HeadPath != "" {
		head, err = LoadLocalHead(cfg.HeadPath)
		if err != nil {
			// Still log remote/L2 verdicts; local fields carry the error.
			log.Printf("[IntentShadow] local head unavailable (%v); logging remote/L2 only", err)
			head = nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o700); err != nil {
		return nil, err
	}
	salt := cfg.Salt
	if cfg.TextMode == ShadowTextHash && len(salt) == 0 {
		salt, err = loadOrCreateShadowSalt(filepath.Join(filepath.Dir(cfg.LogPath), ".intent_shadow_salt"))
		if err != nil {
			return nil, err
		}
	}
	w, err := newShadowRotatingWriter(cfg.LogPath, cfg.MaxBytes, cfg.MaxBackups)
	if err != nil {
		return nil, err
	}
	var nb [4]byte
	_, _ = rand.Read(nb[:])
	l := &shadowLogger{
		cfg:   cfg,
		head:  head,
		salt:  salt,
		nonce: hex.EncodeToString(nb[:]),
		queue: make(chan shadowJob, cfg.QueueSize),
		done:  make(chan struct{}),
		refs:  1,
		w:     w,
	}
	go l.run()
	shadowRegistry[cfg.LogPath] = l
	headID := "none"
	if head != nil {
		headID = head.ID
	}
	log.Printf("[IntentShadow] enabled: log=%s head=%s text=%s embed=%v l2_sample=%.3f", cfg.LogPath, headID, cfg.TextMode, cfg.StoreEmbedding, cfg.L2SampleRate)
	return l, nil
}

// release drops one reference; the last one drains the queue and closes the file.
func (l *shadowLogger) release() {
	shadowRegistryMu.Lock()
	l.refs--
	last := l.refs <= 0
	if last {
		delete(shadowRegistry, l.cfg.LogPath)
	}
	shadowRegistryMu.Unlock()
	if !last {
		return
	}
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.queue)
	}
	l.mu.Unlock()
	<-l.done
	_ = l.w.Close()
}

func (l *shadowLogger) stats() ShadowStats {
	return ShadowStats{
		Enqueued:  l.enqueued.Load(),
		Written:   l.written.Load(),
		Dropped:   l.dropped.Load(),
		Errors:    l.errs.Load(),
		SampledL2: l.sampledL2.Load(),
	}
}

// finish decides whether the turn is logged and enqueues it without blocking.
func (l *shadowLogger) finish(st *shadowTurn, text string, res ClassificationResult) {
	kind := "l3"
	if !st.reachedL3 {
		if !st.l2Ran || l.cfg.L2SampleRate <= 0 || mrand.Float64() >= l.cfg.L2SampleRate {
			return
		}
		kind = "l2_sample"
		l.sampledL2.Add(1)
	}
	res.ToolNames = append([]string(nil), res.ToolNames...)
	job := shadowJob{turn: st, text: text, result: res, kind: kind, at: time.Now(), seq: l.seq.Add(1)}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		l.dropped.Add(1)
		return
	}
	select {
	case l.queue <- job:
		l.enqueued.Add(1)
	default:
		l.dropped.Add(1)
	}
}

func (l *shadowLogger) run() {
	defer close(l.done)
	for job := range l.queue {
		l.process(job)
	}
}

func (l *shadowLogger) process(job shadowJob) {
	defer func() {
		if r := recover(); r != nil {
			l.errs.Add(1)
			log.Printf("[IntentShadow] recovered panic while logging: %v", r)
		}
	}()
	if l.testHook != nil {
		l.testHook()
	}
	rec := l.buildRecord(job)
	b, err := json.Marshal(rec)
	if err != nil {
		l.errs.Add(1)
		return
	}
	b = append(b, '\n')
	if err := l.w.Write(b); err != nil {
		l.errs.Add(1)
		return
	}
	l.written.Add(1)
}

func (l *shadowLogger) buildRecord(job shadowJob) shadowRecord {
	st := job.turn
	rec := shadowRecord{
		V:       ShadowSchemaVersion,
		TS:      job.at.Format(time.RFC3339Nano),
		TurnID:  l.nonce + "-" + strconv.FormatUint(job.seq, 10),
		Kind:    job.kind,
		ModelID: st.modelID,
	}
	if l.head != nil {
		rec.HeadID = l.head.ID
	}
	res := job.result
	rec.Final = shadowFinal{Label: string(res.Primary), Conf: res.Confidence, Layer: res.Layer, Degraded: res.Degraded, Tools: res.ToolNames}
	rec.DecidedBy = shadowDecidedBy(st, res)

	// L2
	if st.l2Ran {
		l2 := &shadowL2{Primary: string(st.l2Result.Primary), Conf: st.l2Result.Confidence, Confident: st.l2Confident}
		if len(st.queryVec) > 0 {
			rec.EmbedDim = len(st.queryVec)
			l2.TopK = shadowL2TopK(st.queryVec, st.anchors, st.cal, l.cfg.TopK)
		}
		rec.L2 = l2
	}

	// Remote
	rec.Remote = l.remoteSection(st)

	// Local head
	rec.Local = l.localSection(st)

	// Agreement / sensitivity
	if rec.Remote.Outcome == "ok" && rec.Local != nil && rec.Local.Label != "" {
		a := rec.Local.Label == rec.Remote.Label
		ad := a && rec.Local.Decision == LocalHeadAccept
		rec.Agree, rec.AgreeDec = &a, &ad
	}
	if l.head != nil {
		if rec.Local != nil && rec.Local.Label != "" {
			rec.Sensitive.Local = l.head.IsSensitive(rec.Local.Label)
		}
		if rec.Remote.Label != "" {
			rec.Sensitive.Remote = l.head.IsSensitive(rec.Remote.Label)
		}
	}

	// Text
	rec.Text = shadowText{Mode: l.cfg.TextMode, LenRunes: utf8.RuneCountInString(job.text)}
	switch l.cfg.TextMode {
	case ShadowTextFull:
		rec.Text.Full = job.text
	case ShadowTextHash:
		m := hmac.New(sha256.New, l.salt)
		m.Write([]byte(job.text))
		rec.Text.Hash = hex.EncodeToString(m.Sum(nil))[:32]
	}
	if l.cfg.StoreEmbedding && len(st.queryVec) > 0 {
		rec.Emb = &shadowEmbedding{Dtype: "f16le", Dim: len(st.queryVec), B64: encodeF16Base64(st.queryVec)}
	}
	return rec
}

func shadowDecidedBy(st *shadowTurn, res ClassificationResult) string {
	switch {
	case st.outcome == "skipped_short_lookup":
		return "l2_short_lookup"
	case !st.reachedL3 && st.l2Ran:
		return "l2"
	case res.Layer == 3:
		return "l3"
	case res.Layer == 2 && st.remoteDone && st.err == nil:
		return "l2_retained" // tree answered but L2 kept (weak verdict / contradiction)
	case res.Layer == 2:
		return "l2_fallback"
	default:
		return "none"
	}
}

func (l *shadowLogger) remoteSection(st *shadowTurn) shadowRemote {
	r := shadowRemote{Outcome: st.outcome}
	if !st.reachedL3 {
		if r.Outcome == "" {
			r.Outcome = "not_reached"
		}
		return r
	}
	if !st.remoteDone {
		return r
	}
	r.Called = true
	r.LatencyMS = float64(st.latency.Microseconds()) / 1000
	if st.err != nil {
		r.EndpointFailure = isTreeEndpointFailure(st.err)
		if l.cfg.TextMode == ShadowTextFull {
			msg := st.err.Error()
			if len(msg) > 300 {
				msg = msg[:300]
			}
			r.Error = msg
		}
		return r
	}
	for i, c := range st.candidates {
		if i >= l.cfg.TopK {
			break
		}
		r.Candidates = append(r.Candidates, shadowCand{Label: string(c.Label), Score: c.Score})
	}
	if len(st.candidates) > 0 {
		r.Label = string(st.candidates[0].Label)
		r.Score = st.candidates[0].Score
	}
	return r
}

// shadowRemoteOutcome classifies a tree call result without changing it.
func shadowRemoteOutcome(turnCtx context.Context, cands []TreeCandidate, err error) string {
	if err == nil {
		if len(cands) == 0 {
			return "empty"
		}
		return "ok"
	}
	var protocolErr *TreeResponseProtocolError
	if errors.As(err, &protocolErr) {
		return "protocol"
	}
	if turnCtx != nil && turnCtx.Err() != nil {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var coder httpStatusCoder
	if errors.As(err, &coder) {
		code := coder.HTTPStatusCode()
		switch {
		case code >= 500 && code < 600:
			return "http_5xx"
		case code >= 400 && code < 500:
			return "http_4xx"
		}
		return "http_" + strconv.Itoa(code)
	}
	return "error"
}

func (l *shadowLogger) localSection(st *shadowTurn) (out *shadowLocal) {
	out = &shadowLocal{}
	if l.head == nil {
		out.Error = "no_head"
		return out
	}
	out.Tau, out.TauS = l.head.Tau, l.head.TauS
	if len(st.queryVec) == 0 {
		out.Error = "no_l2_vector"
		return out
	}
	if len(st.queryVec) != l.head.Dim() {
		out.Error = "dim_mismatch"
		return out
	}
	defer func() {
		if r := recover(); r != nil {
			l.errs.Add(1)
			out = &shadowLocal{Tau: l.head.Tau, TauS: l.head.TauS, Error: "panic"}
		}
	}()
	t0 := time.Now()
	probs, err := l.head.Predict(st.queryVec)
	if err != nil {
		out.Error = "predict_error"
		return out
	}
	label, p, dec, thr := l.head.Decide(probs)
	out.LatencyUS = float64(time.Since(t0).Nanoseconds()) / 1000
	out.TopK = l.head.TopK(probs, l.cfg.TopK)
	out.Label, out.P, out.Decision, out.Threshold = label, p, dec, thr
	return out
}

func shadowL2TopK(vec []float32, anchors []intentAnchor, cal intentScoreCalibration, k int) []shadowL2Score {
	scores := make([]shadowL2Score, 0, len(anchors))
	for _, a := range anchors {
		if len(a.Vecs) == 0 {
			continue
		}
		mx := 0.0
		for _, av := range a.Vecs {
			if s := cosineSimilarity(vec, av); s > mx {
				mx = s
			}
		}
		scores = append(scores, shadowL2Score{Label: string(a.Label), Raw: mx, Mapped: cal.apply(mx)})
	}
	sort.SliceStable(scores, func(i, j int) bool { return scores[i].Raw > scores[j].Raw })
	if len(scores) > k {
		scores = scores[:k]
	}
	return scores
}

// ---------------------------------------------------------------------------
// Helpers: salt, float16, rotating writer
// ---------------------------------------------------------------------------

func loadOrCreateShadowSalt(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		if s, err := hex.DecodeString(strings.TrimSpace(string(b))); err == nil && len(s) >= 16 {
			return s, nil
		}
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) { // another process won the race
			if b, rerr := os.ReadFile(path); rerr == nil {
				if s, derr := hex.DecodeString(strings.TrimSpace(string(b))); derr == nil && len(s) >= 16 {
					return s, nil
				}
			}
		}
		return nil, fmt.Errorf("intent shadow salt: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(hex.EncodeToString(salt)); err != nil {
		return nil, err
	}
	return salt, nil
}

func float32ToF16(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16(b>>16) & 0x8000
	exp := int((b>>23)&0xff) - 127 + 15
	mant := b & 0x7fffff
	switch {
	case (b>>23)&0xff == 0xff: // Inf/NaN
		if mant != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case exp >= 0x1f:
		return sign | 0x7c00
	case exp <= 0:
		if exp < -10 {
			return sign
		}
		mant |= 0x800000
		shift := uint32(14 - exp)
		half := mant >> shift
		rem := mant & ((1 << shift) - 1)
		mid := uint32(1) << (shift - 1)
		if rem > mid || (rem == mid && half&1 == 1) {
			half++
		}
		return sign | uint16(half)
	}
	half := uint16(exp)<<10 | uint16(mant>>13)
	rem := mant & 0x1fff
	if rem > 0x1000 || (rem == 0x1000 && half&1 == 1) {
		half++ // may carry into the exponent, which is correct rounding
	}
	return sign | half
}

func encodeF16Base64(v []float32) string {
	buf := make([]byte, 2*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint16(buf[2*i:], float32ToF16(f))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// shadowRotatingWriter appends whole records with one write each and rotates
// by size: path → path.1 → … → path.N (oldest dropped). Mode 0600.
type shadowRotatingWriter struct {
	mu      sync.Mutex
	path    string
	max     int64
	backups int
	f       *os.File
	size    int64
}

func newShadowRotatingWriter(path string, max int64, backups int) (*shadowRotatingWriter, error) {
	w := &shadowRotatingWriter{path: path, max: max, backups: backups}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *shadowRotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_ = f.Chmod(0o600) // tighten a pre-existing file
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

func (w *shadowRotatingWriter) Write(b []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return os.ErrClosed
	}
	if w.size > 0 && w.size+int64(len(b)) > w.max {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	n, err := w.f.Write(b)
	w.size += int64(n)
	return err
}

func (w *shadowRotatingWriter) rotate() error {
	_ = w.f.Close()
	w.f = nil
	if w.backups > 0 {
		_ = os.Remove(fmt.Sprintf("%s.%d", w.path, w.backups))
		for i := w.backups - 1; i >= 1; i-- {
			_ = os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1))
		}
		if err := os.Rename(w.path, w.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		_ = os.Remove(w.path)
	}
	return w.open()
}

func (w *shadowRotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// ---------------------------------------------------------------------------
// Classifier API
// ---------------------------------------------------------------------------

// EnableShadow turns shadow mode on for this classifier (replacing any
// previous shadow logger). Records for the same log path share one writer.
func (u *UnifiedIntentClassifier) EnableShadow(cfg ShadowConfig) error {
	if !cfg.Enabled {
		u.CloseShadow()
		return nil
	}
	l, err := acquireShadowLogger(cfg)
	if err != nil {
		return err
	}
	if old := u.shadow.Swap(l); old != nil {
		old.release()
	}
	return nil
}

// CloseShadow turns shadow mode off and, for the last user of the log file,
// drains the queue and closes the file. Safe to call when shadow is off.
func (u *UnifiedIntentClassifier) CloseShadow() {
	if old := u.shadow.Swap(nil); old != nil {
		old.release()
	}
}

// ShadowStats reports the shadow log counters (zero when shadow is off).
func (u *UnifiedIntentClassifier) ShadowStats() (ShadowStats, bool) {
	l := u.shadow.Load()
	if l == nil {
		return ShadowStats{}, false
	}
	return l.stats(), true
}

// ShadowLogPath is the resolved log file ("" when shadow is off).
func (u *UnifiedIntentClassifier) ShadowLogPath() string {
	if l := u.shadow.Load(); l != nil {
		return l.cfg.LogPath
	}
	return ""
}
