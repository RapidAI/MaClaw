package httpthreat

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Node is the detect-node hot path. It pulls serving, scores locally, and
// offers samples to the trainer. Ingest failure never changes the decision.
type Node struct {
	ID        NodeIdentity
	BaseURL   string
	Token     string
	EncoderID string
	Embed     EmbedFunc
	Client    *http.Client

	VerifyKey []byte

	mu          sync.Mutex
	pulling     sync.Mutex
	rules       *RuleEngine
	bundle      ServingBundle
	acked       string
	offerCh     chan struct{}
	blockLimit  int
	blockN      int
	blockWin    time.Time
	localShadow bool
}

func NewNode(id NodeIdentity, baseURL, token, encoderID string, embed EmbedFunc) *Node {
	if strings.TrimSpace(encoderID) == "" {
		encoderID = DefaultEncoderID
	}
	if embed == nil {
		embed = HashEmbed
	}
	return &Node{
		ID: id, BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Token: token, EncoderID: encoderID, Embed: embed,
		Client:     &http.Client{Timeout: 10 * time.Second},
		rules:      NewRuleEngine(),
		offerCh:    make(chan struct{}, 64),
		blockLimit: 200,
	}
}

func (n *Node) Install(b ServingBundle, acked string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	n.bundle = b
	n.acked = acked
	n.localShadow = false
	n.mu.Unlock()
}

func (n *Node) Detect(tx Transaction) (dec Decision, emb []float32) {
	defer func() {
		if rec := recover(); rec != nil {
			if dec.RuleClass == "" {
				dec.RuleClass, dec.RuleSource, dec.RuleAction = ClassBenign, SourceFallback, ActionAllow
			}
			if dec.RuleAction == "" {
				dec.RuleAction = ActionAllow
			}
			dec.Class, dec.Source, dec.Action = dec.RuleClass, dec.RuleSource, dec.RuleAction
			dec.HeadUsed = false
			dec.Pipeline = PipelineOff
			emb = nil
		}
	}()
	if n == nil {
		return Decision{Class: ClassBenign, Source: SourceFallback, Action: ActionAllow, Pipeline: PipelineOff}, nil
	}
	tenant := strings.TrimSpace(n.ID.TenantID)
	tx.TenantID = tenant
	applyHTTP2Pseudo(&tx)
	noteSession(n.rules, tenant, tx)
	preview := BuildPreview(tx)
	sampleID := SampleID(tenant, n.EncoderID, preview)
	hit := n.rules.Classify(tx)
	n.mu.Lock()
	b := n.bundle
	if n.localShadow {
		b.Pipeline = PipelineShadow
	}
	acked := n.acked
	n.mu.Unlock()
	if class := b.IntelHosts[normalizeHost(tx.Host)]; IsTrainableClass(class) && hit.Source != SourceSignature {
		hit.Class, hit.Source, hit.RuleID = class, SourceIntel, "p1.intel.host"
	}
	if mapped := strings.TrimSpace(b.RuleMap[hit.RuleID]); mapped != "" {
		if IsTrainableClass(mapped) {
			hit.Class = mapped
		} else {
			hit.Class = ""
		}
	}
	if !IsTrainableClass(hit.Class) && hit.Class != ClassUnknown {
		hit.Class = ClassBenign
		if hit.Source == SourceSignature || hit.Source == SourceIntel {
			hit.Source = SourceFallback
			hit.RuleID = "p4.fallback"
		}
	}
	ruleAction := ClassAction(hit.Class)
	if ruleAction == "" {
		ruleAction = ActionAllow
	}
	mode := NormalizePipeline(b.Pipeline)
	head := nodeHead(b, acked, n.ID.NodeID, n.VerifyKey)
	if head == nil {
		mode = PipelineOff
	}
	hash := ""
	if head != nil {
		hash = head.Hash()
	}
	dec = Decision{
		RuleClass: hit.Class, RuleSource: hit.Source, RuleID: hit.RuleID, RuleAction: ruleAction,
		Class: hit.Class, Source: hit.Source, Action: ruleAction,
		Preview: preview, SampleID: sampleID, Pipeline: mode, ServingHash: hash,
	}
	siteTenant := ""
	site := strings.TrimSpace(tx.SiteID)
	if site != "" {
		for _, s := range b.Sites {
			if s == site {
				siteTenant = tenant
				break
			}
		}
	}
	if mode != PipelineOff && HeadMayScore(hit.Source) && n.Embed != nil && head != nil && head.Ready() {
		if vec, err := n.Embed(preview); err == nil && len(vec) >= HeadDim {
			emb = vec
			pred := head.Predict(vec)
			dec.HeadClass = pred.Class
			dec.HeadMaxP = pred.MaxP
			class, src, action, used := applyPipeline(mode, tenant, tx.SiteID, tx.SourceID, siteTenant, hit.Class, hit.Source, ruleAction, pred)
			dec.Class, dec.Source, dec.Action, dec.HeadUsed = class, src, action, used
			if used && action == ActionBlock && ruleAction != ActionBlock {
				n.mu.Lock()
				if n.tripSafetyLocked() {
					dec.Action = ruleAction
					dec.Class = hit.Class
					dec.Source = hit.Source
					dec.HeadUsed = false
					dec.Demoted = true
					n.localShadow = true
					n.bundle.Pipeline = PipelineShadow
				}
				n.mu.Unlock()
			}
			return dec, emb
		}
	}
	return dec, nil
}

func (n *Node) tripSafetyLocked() bool {
	if n == nil {
		return false
	}
	now := time.Now().UTC()
	if n.blockWin.IsZero() || now.Sub(n.blockWin) > time.Minute {
		n.blockWin = now
		n.blockN = 0
	}
	n.blockN++
	return n.blockN > n.blockLimit
}

func nodeHead(b ServingBundle, acked, nodeID string, key []byte) *Head {
	servingOK := VerifyHead(b.Serving, key)
	prevOK := VerifyHead(b.Previous, key)
	if servingOK && (b.Previous == nil || nodeID == "" || acked == b.Hash) {
		return b.Serving
	}
	if prevOK {
		return b.Previous
	}
	if servingOK && b.Previous == nil {
		return b.Serving
	}
	return nil
}

func (n *Node) Pull() error {
	if n == nil || n.BaseURL == "" {
		return fmt.Errorf("%w: trainer url", ErrInvalid)
	}
	n.pulling.Lock()
	defer n.pulling.Unlock()
	req, err := http.NewRequest(http.MethodGet, n.BaseURL+"/api/httpthreat/serving", nil)
	if err != nil {
		return err
	}
	n.auth(req)
	resp, err := n.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%w: pull %d", ErrConflict, resp.StatusCode)
	}
	var b ServingBundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return err
	}
	b = filterBundle(b, n.VerifyKey)
	if b.Hash == "" {
		n.mu.Lock()
		if n.bundle.Hash == "" {
			n.bundle = b
		}
		n.mu.Unlock()
		return nil
	}
	body, _ := json.Marshal(map[string]string{"hash": b.Hash})
	ack, err := http.NewRequest(http.MethodPost, n.BaseURL+"/api/httpthreat/ack", bytes.NewReader(body))
	if err != nil {
		return err
	}
	ack.Header.Set("Content-Type", "application/json")
	n.auth(ack)
	ackResp, err := n.client().Do(ack)
	if err != nil {
		return err
	}
	defer ackResp.Body.Close()
	if ackResp.StatusCode >= 300 {
		return fmt.Errorf("%w: ack %d", ErrConflict, ackResp.StatusCode)
	}
	n.mu.Lock()
	keepShadow := n.localShadow && (NormalizePipeline(b.Pipeline) == PipelineCanary || NormalizePipeline(b.Pipeline) == PipelineOn)
	n.bundle = b
	if keepShadow {
		n.bundle.Pipeline = PipelineShadow
	} else {
		n.localShadow = false
	}
	n.acked = b.Hash
	n.mu.Unlock()
	return nil
}

// WriteAction applies the frozen disposition. It returns true when the handler
// must stop (block / challenge / ratelimit). observe and allow continue.
func WriteAction(w http.ResponseWriter, dec Decision) bool {
	if w == nil {
		return false
	}
	switch dec.Action {
	case ActionBlock:
		http.Error(w, "forbidden", http.StatusForbidden)
		return true
	case ActionChallenge:
		w.Header().Set("WWW-Authenticate", `Bearer realm="httpthreat"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return true
	case ActionRateLimit:
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return true
	default:
		return false
	}
}

// Inspect scores a live request and offers the sample without blocking detect.
func (n *Node) Inspect(r *http.Request) Decision {
	tx := TransactionFromHTTP(r)
	dec, emb := n.Detect(tx)
	n.OfferAsync(tx, dec, emb)
	return dec
}

// Wrap is the §11 detect-node adapter. Ingest never blocks the hot path.
func (n *Node) Wrap(next http.Handler) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r != nil && skipDetectPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		dec := n.Inspect(r)
		if WriteAction(w, dec) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (n *Node) OfferAsync(tx Transaction, dec Decision, emb []float32) {
	if n == nil || SkipCorpus(tx) {
		return
	}
	tx = cloneTransaction(tx)
	emb = append([]float32(nil), emb...)
	if n.offerCh == nil || dec.Demoted {
		go func() { _ = n.Offer(tx, dec, emb) }()
		return
	}
	select {
	case n.offerCh <- struct{}{}:
		go func() {
			defer func() { <-n.offerCh }()
			_ = n.Offer(tx, dec, emb)
		}()
	default:
	}
}

func cloneTransaction(tx Transaction) Transaction {
	if len(tx.Headers) > 0 {
		h := make(map[string]string, len(tx.Headers))
		for k, v := range tx.Headers {
			h[k] = v
		}
		tx.Headers = h
	}
	return tx
}

// Offer sends the already-scored sample. Failure is ignored by the caller.
func (n *Node) Offer(tx Transaction, dec Decision, emb []float32) error {
	if n == nil || n.BaseURL == "" {
		return nil
	}
	reqBody, _ := json.Marshal(IngestRequest{Transaction: tx, Decision: &dec, Embedding: emb, EncoderID: n.EncoderID})
	req, err := http.NewRequest(http.MethodPost, n.BaseURL+"/api/httpthreat/ingest", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	n.auth(req)
	resp, err := n.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%w: ingest %d", ErrConflict, resp.StatusCode)
	}
	return nil
}

func (n *Node) auth(req *http.Request) {
	if n == nil || req == nil {
		return
	}
	if n.Token != "" {
		req.Header.Set("Authorization", "Bearer "+n.Token)
	}
	if id := strings.TrimSpace(n.ID.NodeID); id != "" {
		req.Header.Set("X-Machine-ID", id)
	}
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

// NodeFromEnv builds a detect node when a trainer URL and tenant are set.
// Missing config returns nil so the host stays a plain rule-less process.
func NodeFromEnv() *Node {
	base := firstEnv("HTTPTHREAT_TRAINER_URL", "MACLAW_HUB_URL")
	tenant := firstEnv("HTTPTHREAT_TENANT", "MACLAW_HUB_TENANT_ID", "MACLAW_TENANT_ID")
	if base == "" || tenant == "" {
		return nil
	}
	nodeID := firstEnv("HTTPTHREAT_NODE", "MACLAW_MACHINE_ID")
	if nodeID == "" {
		nodeID, _ = os.Hostname()
	}
	if strings.TrimSpace(nodeID) == "" {
		nodeID = "detect"
	}
	token := firstEnv("HTTPTHREAT_TOKEN", "MACLAW_HUB_TOKEN", "MACLAW_MACHINE_TOKEN")
	n := NewNode(NodeIdentity{TenantID: tenant, NodeID: nodeID}, base, token, firstEnv("HTTPTHREAT_ENCODER"), HashEmbed)
	n.VerifyKey = parseVerifyKey(firstEnv("HTTPTHREAT_VERIFY_KEY"))
	return n
}

func parseVerifyKey(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if len(raw) == 64 {
		if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
			return b
		}
	}
	return []byte(raw)
}

func filterBundle(b ServingBundle, key []byte) ServingBundle {
	if !VerifyHead(b.Serving, key) {
		b.Serving = nil
		b.Hash = ""
	} else if b.Serving != nil {
		b.Hash = b.Serving.Hash()
	}
	if !VerifyHead(b.Previous, key) {
		b.Previous = nil
		b.PreviousHash = ""
	} else if b.Previous != nil {
		b.PreviousHash = b.Previous.Hash()
	}
	return b
}

// WrapEnabled is the opt-in inbound adapter. Default is inspect/pull only.
func WrapEnabled() bool {
	switch strings.ToLower(firstEnv("HTTPTHREAT_WRAP")) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// StartPull confirms serving hashes so distribution can complete. Failures stay
// on previous; they never change a live decision.
func (n *Node) StartPull(ctx context.Context, every time.Duration) {
	if n == nil {
		return
	}
	if every <= 0 {
		every = 30 * time.Second
	}
	_ = n.Pull()
	if ctx == nil {
		return
	}
	go func() {
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				_ = n.Pull()
			}
		}
	}()
}

func (n *Node) client() *http.Client {
	if n.Client != nil {
		return n.Client
	}
	return http.DefaultClient
}
