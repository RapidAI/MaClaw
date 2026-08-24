package httpthreat

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrUnauthorized = errors.New("httpthreat: unauthorized")
	ErrForbidden    = errors.New("httpthreat: forbidden")
	ErrConflict     = errors.New("httpthreat: conflict")
	ErrInvalid      = errors.New("httpthreat: invalid")
	ErrNotFound     = errors.New("httpthreat: not found")
)

type EmbedFunc func(preview string) ([]float32, error)

type Artifact struct {
	Pipeline      string
	Serving       *Head
	Previous      *Head
	Candidate     *Head
	TrainIDs      []string
	ACKs          map[string]string
	TargetNodes   []string
	Training      bool
	OverrideNote  string
	BlockUpgrades int
	WindowStart   time.Time
	ExportOK      bool
	CorpusCap     int
	RuleMap       map[string]string
	SafetyValve   bool
	TrainError    string
	IntelHosts    map[string]string
	Sites         []string
}

type Engine struct {
	mu           sync.Mutex
	trainAll     sync.Mutex
	rules        *RuleEngine
	corpus       *Corpus
	arts         map[string]*Artifact
	runs         map[string][]TrainRun
	embed        EmbedFunc
	arbitrate    ArbitrateFunc
	encoderID    string
	dir          string
	now          func() time.Time
	siteTenant   map[string]string
	blockLimit   int
	ingestN      map[string]int
	ingestWin    map[string]time.Time
	dropCount    map[string]int
	unlabelN     map[string]int
	recent       map[string][]AuditRow
	hits         map[string]map[string]int
	restKeyBytes []byte
	flushMu      sync.Mutex
	flushDirty   map[string]bool
	flushSched   map[string]bool
}

func NewEngine(encoderID string, embed EmbedFunc) *Engine {
	return NewEngineAt("", encoderID, embed)
}

func NewEngineAt(dir, encoderID string, embed EmbedFunc) *Engine {
	e := &Engine{
		rules:      NewRuleEngine(),
		corpus:     NewCorpus(encoderID, DefaultCorpusCap),
		arts:       map[string]*Artifact{},
		runs:       map[string][]TrainRun{},
		embed:      embed,
		encoderID:  strings.TrimSpace(encoderID),
		dir:        strings.TrimSpace(dir),
		now:        func() time.Time { return time.Now().UTC() },
		siteTenant: map[string]string{},
		blockLimit: 200,
		ingestN:    map[string]int{},
		ingestWin:  map[string]time.Time{},
		dropCount:  map[string]int{},
		unlabelN:   map[string]int{},
		recent:     map[string][]AuditRow{},
		hits:       map[string]map[string]int{},
		flushDirty: map[string]bool{},
		flushSched: map[string]bool{},
	}
	e.loadRestKey()
	e.loadAll()
	return e
}

func (e *Engine) BindSite(tenantID, siteID string) {
	tenant := strings.TrimSpace(tenantID)
	site := strings.TrimSpace(siteID)
	if tenant == "" || site == "" {
		return
	}
	e.mu.Lock()
	e.siteTenant[site] = tenant
	a := e.art(tenant)
	a.Sites = appendUnique(a.Sites, site)
	e.mu.Unlock()
	e.flushTenant(tenant)
}

func (e *Engine) art(tenant string) *Artifact {
	a := e.arts[tenant]
	if a == nil {
		a = &Artifact{Pipeline: PipelineOff, ACKs: map[string]string{}}
		e.arts[tenant] = a
	}
	return a
}

func (e *Engine) teacherSnap(tenant, nodeID string, tx Transaction) (hit ruleHit, ruleAction, mode string, head *Head, siteTenant, servingHash string) {
	hit = e.rules.Classify(tx)
	e.mu.Lock()
	art := e.art(tenant)
	if class := art.IntelHosts[normalizeHost(tx.Host)]; IsTrainableClass(class) && hit.Source != SourceSignature {
		hit.Class, hit.Source, hit.RuleID = class, SourceIntel, "p1.intel.host"
	}
	mapped := ""
	if art.RuleMap != nil {
		mapped = art.RuleMap[hit.RuleID]
	}
	mode = NormalizePipeline(art.Pipeline)
	head = e.headForNodeLocked(art, nodeID)
	if head == nil {
		mode = PipelineOff
	}
	if head != nil {
		servingHash = head.Hash()
	}
	siteTenant = e.siteTenant[strings.TrimSpace(tx.SiteID)]
	e.mu.Unlock()
	if mapped != "" {
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
	ruleAction = ClassAction(hit.Class)
	if ruleAction == "" {
		ruleAction = ActionAllow
	}
	return
}

func tenantFromIdentity(id NodeIdentity, claimed string) (string, error) {
	tenant := strings.TrimSpace(id.TenantID)
	if tenant == "" {
		return "", ErrUnauthorized
	}
	if strings.TrimSpace(claimed) != "" && strings.TrimSpace(claimed) != tenant {
		return "", ErrForbidden
	}
	return tenant, nil
}

// Detect runs rules then optional head. Corpus upsert failure never changes the decision.
func (e *Engine) Detect(id NodeIdentity, tx Transaction) (dec Decision, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = nil
			if dec.RuleClass == "" {
				dec.RuleClass, dec.RuleSource, dec.RuleAction = ClassBenign, SourceFallback, ActionAllow
			}
			if dec.RuleAction == "" {
				dec.RuleAction = ActionAllow
			}
			dec.Class, dec.Source, dec.Action = dec.RuleClass, dec.RuleSource, dec.RuleAction
			dec.HeadUsed = false
			dec.Pipeline = PipelineOff
		}
	}()
	tenant, err := tenantFromIdentity(id, tx.TenantID)
	if err != nil {
		return Decision{}, err
	}
	tx.TenantID = tenant
	applyHTTP2Pseudo(&tx)
	noteSession(e.rules, tenant, tx)
	preview := BuildPreview(tx)
	sampleID := SampleID(tenant, e.encoderID, preview)
	hit, ruleAction, mode, head, siteTenant, servingHash := e.teacherSnap(tenant, id.NodeID, tx)

	dec = Decision{
		RuleClass: hit.Class, RuleSource: hit.Source, RuleID: hit.RuleID, RuleAction: ruleAction,
		Class: hit.Class, Source: hit.Source, Action: ruleAction,
		Preview: preview, SampleID: sampleID, Pipeline: mode, ServingHash: servingHash,
	}

	var emb []float32
	if mode != PipelineOff && HeadMayScore(hit.Source) && e.embed != nil && head != nil && head.Ready() {
		if vec, eerr := e.embed(preview); eerr == nil && len(vec) >= HeadDim {
			emb = vec
			pred := head.Predict(vec)
			dec.HeadClass = pred.Class
			dec.HeadMaxP = pred.MaxP
			dec.HeadProbs = pred.Probs
			class, src, action, used := applyPipeline(mode, tenant, tx.SiteID, tx.SourceID, siteTenant, hit.Class, hit.Source, ruleAction, pred)
			dec.Class, dec.Source, dec.Action, dec.HeadUsed = class, src, action, used
		}
	}

	tripped := false
	if dec.HeadUsed && dec.Action == ActionBlock && dec.RuleAction != ActionBlock {
		e.mu.Lock()
		if e.tripSafetyLocked(tenant) {
			dec.Action = ruleAction
			dec.Class = hit.Class
			dec.Source = hit.Source
			dec.HeadUsed = false
			dec.Demoted = true
			e.art(tenant).Pipeline = PipelineShadow
			e.art(tenant).SafetyValve = true
			tripped = true
		}
		e.mu.Unlock()
	}

	if !SkipCorpus(tx) {
		e.record(tenant, tx, dec, emb)
		e.rememberDecision(tenant, dec)
	}
	if tripped {
		e.flushTenant(tenant)
	}
	return dec, nil
}

func (e *Engine) rememberDecision(tenant string, dec Decision) {
	row := AuditRow{
		ID: dec.SampleID, Action: dec.Action, RuleClass: dec.RuleClass, RuleID: dec.RuleID,
		HeadClass: dec.HeadClass, Preview: dec.Preview, At: e.now().UTC().Format(time.RFC3339),
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recent[tenant] = append([]AuditRow{row}, e.recent[tenant]...)
	if len(e.recent[tenant]) > DefaultAuditCap {
		e.recent[tenant] = e.recent[tenant][:DefaultAuditCap]
	}
	rid := strings.TrimSpace(dec.RuleID)
	if rid == "" {
		return
	}
	if e.hits[tenant] == nil {
		e.hits[tenant] = map[string]int{}
	}
	e.hits[tenant][rid]++
}

func (e *Engine) tripSafetyLocked(tenant string) bool {
	a := e.art(tenant)
	now := e.now()
	if a.WindowStart.IsZero() || now.Sub(a.WindowStart) > time.Minute {
		a.WindowStart = now
		a.BlockUpgrades = 0
	}
	a.BlockUpgrades++
	return a.BlockUpgrades > e.blockLimit
}

func (e *Engine) record(tenant string, tx Transaction, dec Decision, emb []float32) {
	now := e.now()
	s := Sample{
		ID: dec.SampleID, TenantID: tenant, EncoderID: e.encoderID, Preview: dec.Preview,
		Embedding: emb, RuleClass: dec.RuleClass, RuleSource: dec.RuleSource, RuleID: dec.RuleID,
		SiteID: tx.SiteID, HeadClass: dec.HeadClass, HeadMaxP: dec.HeadMaxP,
	}
	if AutoGold(dec.RuleSource) && IsTrainableClass(dec.RuleClass) {
		s.GoldClass = dec.RuleClass
		s.GoldSource = GoldAuto
	}
	e.corpus.Upsert(now, s)
	cap := DefaultCorpusCap
	e.mu.Lock()
	if n := e.art(tenant).CorpusCap; n > 0 {
		cap = n
	}
	e.mu.Unlock()
	e.corpus.TrimTenant(tenant, cap)
	e.scheduleFlush(tenant)
}

func (e *Engine) overIngest(tenant string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	if e.ingestWin[tenant].IsZero() || now.Sub(e.ingestWin[tenant]) > time.Minute {
		e.ingestWin[tenant] = now
		e.ingestN[tenant] = 0
	}
	e.ingestN[tenant]++
	if e.ingestN[tenant] > 2000 {
		e.dropCount[tenant]++
		return true
	}
	return false
}

// Ingest is detect-node delivery. Auth tenant wins. Over-rate drops the sample only.
func (e *Engine) Ingest(id NodeIdentity, tx Transaction) error {
	return e.IngestSample(id, IngestRequest{Transaction: tx})
}

func (e *Engine) IngestSample(id NodeIdentity, req IngestRequest) error {
	if e.overIngest(strings.TrimSpace(id.TenantID)) {
		return nil
	}
	if req.Decision == nil {
		_, err := e.Detect(id, req.Transaction)
		return err
	}
	tenant, err := tenantFromIdentity(id, req.Transaction.TenantID)
	if err != nil {
		return err
	}
	tx := req.Transaction
	tx.TenantID = tenant
	applyHTTP2Pseudo(&tx)
	preview := BuildPreview(tx)
	sampleID := SampleID(tenant, e.encoderID, preview)
	hit, ruleAction, mode, head, siteTenant, _ := e.teacherSnap(tenant, id.NodeID, tx)
	dec := *req.Decision
	dec.Preview = preview
	dec.SampleID = sampleID
	dec.RuleClass, dec.RuleSource, dec.RuleID, dec.RuleAction = hit.Class, hit.Source, hit.RuleID, ruleAction
	var emb []float32
	verifiedUpgrade := false
	if mode != PipelineOff && HeadMayScore(hit.Source) && e.embed != nil && head != nil && head.Ready() {
		if vec, eerr := e.embed(preview); eerr == nil && len(vec) >= HeadDim {
			emb = vec
			pred := head.Predict(vec)
			dec.HeadClass = pred.Class
			dec.HeadMaxP = pred.MaxP
			_, _, action, used := applyPipeline(mode, tenant, tx.SiteID, tx.SourceID, siteTenant, hit.Class, hit.Source, ruleAction, pred)
			verifiedUpgrade = used && action == ActionBlock && ruleAction != ActionBlock
		}
	}
	clientDemoted := req.Decision.Demoted
	clientBlockUpgrade := req.Decision.HeadUsed && req.Decision.Action == ActionBlock
	tripped := false
	if verifiedUpgrade && (clientDemoted || clientBlockUpgrade) {
		e.mu.Lock()
		trip := clientDemoted || e.tripSafetyLocked(tenant)
		if trip {
			dec.Action = ruleAction
			dec.Class = hit.Class
			dec.Source = hit.Source
			dec.HeadUsed = false
			dec.Demoted = true
			cur := NormalizePipeline(e.art(tenant).Pipeline)
			if cur == PipelineCanary || cur == PipelineOn {
				e.art(tenant).Pipeline = PipelineShadow
				e.art(tenant).SafetyValve = true
				tripped = true
			}
		}
		e.mu.Unlock()
	} else {
		dec.Demoted = false
		dec.HeadUsed = false
		dec.Class, dec.Source, dec.Action = hit.Class, hit.Source, ruleAction
	}
	if !SkipCorpus(tx) {
		e.record(tenant, tx, dec, emb)
		e.rememberDecision(tenant, dec)
	}
	if tripped {
		e.flushTenant(tenant)
	}
	return nil
}

func (e *Engine) Label(id NodeIdentity, req LabelRequest) error {
	tenant, err := tenantFromIdentity(id, req.TenantID)
	if err != nil {
		return err
	}
	if req.Role != RoleAnalyst && req.Role != RoleAdmin {
		return ErrForbidden
	}
	if !e.writable() {
		return fmt.Errorf("%w: corpus not writable", ErrConflict)
	}
	if req.Abstain || strings.EqualFold(strings.TrimSpace(req.GoldClass), "abstain") {
		cur, ok := e.corpus.Get(req.SampleID)
		if !ok || cur.TenantID != tenant {
			return ErrNotFound
		}
		if !e.corpus.Abstain(req.SampleID) {
			return ErrNotFound
		}
		e.flushTenant(tenant)
		return nil
	}
	gold := strings.TrimSpace(req.GoldClass)
	if gold == "" {
		cur, ok := e.corpus.Get(req.SampleID)
		if !ok || cur.TenantID != tenant {
			return ErrNotFound
		}
		if !e.corpus.Unlabel(req.SampleID) {
			return ErrNotFound
		}
		e.mu.Lock()
		e.unlabelN[tenant]++
		e.mu.Unlock()
		e.flushTenant(tenant)
		return nil
	}
	if !IsTrainableClass(gold) {
		return fmt.Errorf("%w: gold class", ErrInvalid)
	}
	src := strings.TrimSpace(req.GoldSource)
	if src == "" {
		src = GoldHuman
	}
	if src == GoldLLM && req.Role != RoleAdmin && req.Role != RoleAnalyst {
		return ErrForbidden
	}
	cur, ok := e.corpus.Get(req.SampleID)
	if !ok || cur.TenantID != tenant {
		return ErrNotFound
	}
	if _, ok := e.corpus.Label(req.SampleID, gold, src, e.now()); !ok {
		return ErrNotFound
	}
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) LabelBatch(id NodeIdentity, req BatchLabelRequest) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if req.Role != RoleAnalyst && req.Role != RoleAdmin {
		return ErrForbidden
	}
	if !e.writable() {
		return fmt.Errorf("%w: corpus not writable", ErrConflict)
	}
	gold := strings.TrimSpace(req.GoldClass)
	if !IsTrainableClass(gold) {
		return fmt.Errorf("%w: gold class", ErrInvalid)
	}
	if len(req.SampleIDs) == 0 || len(req.SampleIDs) > BatchLabelLimit {
		return fmt.Errorf("%w: batch size", ErrInvalid)
	}
	allowed := map[string]bool{}
	for _, s := range e.corpus.ReviewQueue(tenant) {
		allowed[s.ID] = true
	}
	for _, sid := range req.SampleIDs {
		if !allowed[sid] {
			return fmt.Errorf("%w: sample not in current queue", ErrInvalid)
		}
		cur, ok := e.corpus.Get(sid)
		if !ok || cur.TenantID != tenant {
			return ErrNotFound
		}
		if _, ok := e.corpus.Label(sid, gold, GoldHuman, e.now()); !ok {
			return ErrNotFound
		}
	}
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) Train(id NodeIdentity, role string) error {
	return e.train(id, role, false)
}

func (e *Engine) StartTrain(id NodeIdentity, role string) error {
	return e.train(id, role, true)
}

func (e *Engine) train(id NodeIdentity, role string, async bool) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	if !e.writable() {
		return fmt.Errorf("%w: corpus not writable", ErrConflict)
	}
	if !e.trainAll.TryLock() {
		return fmt.Errorf("%w: training", ErrConflict)
	}
	e.mu.Lock()
	if e.art(tenant).Training {
		e.mu.Unlock()
		e.trainAll.Unlock()
		return fmt.Errorf("%w: training", ErrConflict)
	}
	e.art(tenant).Training = true
	e.art(tenant).TrainError = ""
	e.mu.Unlock()
	run := func() error {
		defer e.trainAll.Unlock()
		defer func() {
			e.mu.Lock()
			e.art(tenant).Training = false
			e.mu.Unlock()
		}()
		err := e.fitCandidate(tenant)
		if err != nil {
			e.mu.Lock()
			e.art(tenant).TrainError = err.Error()
			e.mu.Unlock()
		}
		return err
	}
	if async {
		go func() { _ = run() }()
		return nil
	}
	return run()
}

func (e *Engine) fitCandidate(tenant string) error {
	rows := e.corpus.ListTenant(tenant)
	autoN := 0
	byRule := map[string]int{}
	for _, s := range rows {
		if s.GoldSource == GoldAuto && IsTrainableClass(s.GoldClass) {
			autoN++
			rid := strings.TrimSpace(s.RuleID)
			if rid == "" {
				rid = s.RuleSource
			}
			byRule[rid]++
		}
	}
	var labeled []labeledEmb
	var ids []string
	for _, s := range rows {
		if s.GoldClass == "" || !IsTrainableClass(s.GoldClass) {
			continue
		}
		emb := s.Embedding
		if len(emb) < HeadDim {
			if e.embed == nil || s.EncoderID != e.encoderID {
				continue
			}
			vec, eerr := e.embed(s.Preview)
			if eerr != nil || len(vec) < HeadDim {
				continue
			}
			emb = vec
		}
		w := 1.0
		if s.GoldSource == GoldAuto && autoN > 0 {
			rid := strings.TrimSpace(s.RuleID)
			if rid == "" {
				rid = s.RuleSource
			}
			share := float64(byRule[rid]) / float64(autoN)
			if share > DefaultAutoGoldShare {
				w = DefaultAutoGoldShare / share
			}
		}
		labeled = append(labeled, labeledEmb{Emb: emb, Label: s.GoldClass, Weight: w})
		ids = append(ids, s.ID)
	}
	trainedAt := e.now().UTC().Format(time.RFC3339)
	head, err := TrainHead(labeled, 1, DefaultTau, trainedAt)
	if err != nil {
		return err
	}
	SignHead(&head, e.restKey())
	golds := make([]TrainGold, 0, len(ids))
	for _, s := range rows {
		if s.GoldClass == "" || !IsTrainableClass(s.GoldClass) {
			continue
		}
		golds = append(golds, TrainGold{ID: s.ID, GoldClass: s.GoldClass, GoldSource: s.GoldSource})
	}
	e.mu.Lock()
	a := e.art(tenant)
	a.Candidate = &head
	a.TrainIDs = ids
	run := TrainRun{Version: head.Version, TrainedAt: trainedAt, SampleIDs: ids, Golds: golds}
	e.runs[tenant] = append(e.runs[tenant], run)
	if len(e.runs[tenant]) > 20 {
		e.runs[tenant] = e.runs[tenant][len(e.runs[tenant])-20:]
	}
	e.mu.Unlock()
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) Adopt(id NodeIdentity, role string) error {
	return e.AdoptOverride(id, role, "", "")
}

func (e *Engine) AdoptOverride(id NodeIdentity, role, override, reason string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	e.mu.Lock()
	a := e.art(tenant)
	if a.Training {
		e.mu.Unlock()
		return fmt.Errorf("%w: training", ErrConflict)
	}
	if a.Candidate == nil || !a.Candidate.Ready() {
		e.mu.Unlock()
		return fmt.Errorf("%w: no candidate", ErrInvalid)
	}
	mode := NormalizePipeline(a.Pipeline)
	hasServing := a.Serving != nil
	distributed := e.distributedLocked(a)
	cand := a.Candidate
	trainIDs := append([]string(nil), a.TrainIDs...)
	e.mu.Unlock()
	if hasServing && !distributed {
		return fmt.Errorf("%w: distribution incomplete", ErrConflict)
	}
	force := false
	if mode == PipelineCanary || mode == PipelineOn {
		if !e.evalHead(tenant, cand, true, trainIDs).CanPromote {
			reason = strings.TrimSpace(reason)
			n := len([]rune(reason))
			if override != PromoteOverride || n < 10 || n > 2000 {
				return fmt.Errorf("%w: candidate gate", ErrConflict)
			}
			force = true
		}
	}
	e.mu.Lock()
	a = e.art(tenant)
	if a.Training {
		e.mu.Unlock()
		return fmt.Errorf("%w: training", ErrConflict)
	}
	if a.Candidate == nil || !a.Candidate.Ready() {
		e.mu.Unlock()
		return fmt.Errorf("%w: no candidate", ErrInvalid)
	}
	if a.Serving != nil && !e.distributedLocked(a) {
		e.mu.Unlock()
		return fmt.Errorf("%w: distribution incomplete", ErrConflict)
	}
	if force {
		a.OverrideNote = strings.TrimSpace(reason)
	}
	a.Previous = a.Serving
	a.Serving = a.Candidate.Clone()
	SignHead(a.Serving, e.restKey())
	a.Candidate = nil
	a.ACKs = map[string]string{}
	e.mu.Unlock()
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) SetPipeline(id NodeIdentity, mode, override, reason, role string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	mode = NormalizePipeline(mode)
	e.mu.Lock()
	a := e.art(tenant)
	cur := NormalizePipeline(a.Pipeline)
	if rankPipeline(mode) > rankPipeline(cur) && a.Training {
		e.mu.Unlock()
		return fmt.Errorf("%w: training", ErrConflict)
	}
	if rankPipeline(mode) < rankPipeline(cur) || mode == PipelineOff {
		a.Pipeline = mode
		a.SafetyValve = false
		e.mu.Unlock()
		e.flushTenant(tenant)
		return nil
	}
	if a.Serving == nil || !a.Serving.Ready() {
		e.mu.Unlock()
		return fmt.Errorf("%w: no serving", ErrInvalid)
	}
	if cur == PipelineOff && (mode == PipelineCanary || mode == PipelineOn) {
		e.mu.Unlock()
		return fmt.Errorf("%w: cannot skip shadow", ErrInvalid)
	}
	if mode == PipelineShadow {
		a.Pipeline = mode
		a.SafetyValve = false
		e.mu.Unlock()
		e.flushTenant(tenant)
		return nil
	}
	distributed := e.distributedLocked(a)
	serving := a.Serving
	trainIDs := append([]string(nil), a.TrainIDs...)
	e.mu.Unlock()
	if !distributed {
		return fmt.Errorf("%w: distribution incomplete", ErrConflict)
	}
	force := false
	if !e.evalHead(tenant, serving, true, trainIDs).CanPromote {
		reason = strings.TrimSpace(reason)
		n := len([]rune(reason))
		if override != PromoteOverride || n < 10 || n > 2000 {
			return fmt.Errorf("%w: gate", ErrConflict)
		}
		force = true
	}
	e.mu.Lock()
	a = e.art(tenant)
	if rankPipeline(mode) > rankPipeline(NormalizePipeline(a.Pipeline)) && a.Training {
		e.mu.Unlock()
		return fmt.Errorf("%w: training", ErrConflict)
	}
	if a.Serving == nil || !a.Serving.Ready() {
		e.mu.Unlock()
		return fmt.Errorf("%w: no serving", ErrInvalid)
	}
	if !e.distributedLocked(a) {
		e.mu.Unlock()
		return fmt.Errorf("%w: distribution incomplete", ErrConflict)
	}
	if force {
		a.OverrideNote = strings.TrimSpace(reason)
	}
	a.Pipeline = mode
	a.SafetyValve = false
	e.mu.Unlock()
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) Rollback(id NodeIdentity, role string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	e.mu.Lock()
	dirty := false
	defer func() {
		e.mu.Unlock()
		if dirty {
			e.flushTenant(tenant)
		}
	}()
	a := e.art(tenant)
	if a.Previous == nil {
		return fmt.Errorf("%w: no previous", ErrInvalid)
	}
	a.Serving, a.Previous = a.Previous, a.Serving
	a.ACKs = map[string]string{}
	dirty = true
	return nil
}

func (e *Engine) Inspect(id NodeIdentity, r *http.Request) (Decision, error) {
	return e.Detect(id, TransactionFromHTTP(r))
}

func (e *Engine) ACK(id NodeIdentity, hash string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	e.mu.Lock()
	dirty := false
	defer func() {
		e.mu.Unlock()
		if dirty {
			e.flushTenant(tenant)
		}
	}()
	a := e.art(tenant)
	if a.Serving == nil || strings.TrimSpace(id.NodeID) == "" {
		return ErrInvalid
	}
	if hash != "" && hash != a.Serving.Hash() {
		return ErrInvalid
	}
	a.ACKs[id.NodeID] = a.Serving.Hash()
	dirty = true
	return nil
}

func (e *Engine) SetTargets(tenant string, nodes []string) {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		return
	}
	e.mu.Lock()
	e.art(tenant).TargetNodes = append([]string(nil), nodes...)
	e.mu.Unlock()
	e.flushTenant(tenant)
}

func (e *Engine) headForNodeLocked(a *Artifact, nodeID string) *Head {
	key := e.restKey()
	servingOK := VerifyHead(a.Serving, key)
	prevOK := VerifyHead(a.Previous, key)
	if servingOK && (a.Previous == nil || nodeID == "" || nodeID == "admin" || a.ACKs[nodeID] == a.Serving.Hash()) {
		return a.Serving
	}
	if prevOK {
		return a.Previous
	}
	if servingOK && a.Previous == nil {
		return a.Serving
	}
	return nil
}

func (e *Engine) Bundle(id NodeIdentity) (ServingBundle, error) {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return ServingBundle{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.art(tenant)
	out := ServingBundle{Pipeline: NormalizePipeline(a.Pipeline)}
	key := e.restKey()
	if VerifyHead(a.Serving, key) {
		out.Serving = a.Serving.Clone()
		out.Hash = a.Serving.Hash()
	}
	if VerifyHead(a.Previous, key) {
		out.Previous = a.Previous.Clone()
		out.PreviousHash = a.Previous.Hash()
	}
	if len(a.IntelHosts) > 0 {
		out.IntelHosts = map[string]string{}
		for k, v := range a.IntelHosts {
			out.IntelHosts[k] = v
		}
	}
	if len(a.RuleMap) > 0 {
		out.RuleMap = map[string]string{}
		for k, v := range a.RuleMap {
			out.RuleMap[k] = v
		}
	}
	out.Sites = append([]string(nil), a.Sites...)
	return out, nil
}

func (e *Engine) UpdateTargets(id NodeIdentity, role string, nodes, add, remove []string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	e.mu.Lock()
	a := e.art(tenant)
	if len(nodes) > 0 {
		a.TargetNodes = append([]string(nil), nodes...)
	}
	seen := map[string]bool{}
	var next []string
	for _, n := range a.TargetNodes {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		next = append(next, n)
	}
	for _, n := range add {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		next = append(next, n)
	}
	drop := map[string]bool{}
	for _, n := range remove {
		drop[strings.TrimSpace(n)] = true
	}
	kept := next[:0]
	for _, n := range next {
		if drop[n] {
			delete(a.ACKs, n)
			continue
		}
		kept = append(kept, n)
	}
	a.TargetNodes = kept
	e.mu.Unlock()
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) Gate(id NodeIdentity, which string) (GateReport, error) {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return GateReport{}, err
	}
	e.mu.Lock()
	a := e.art(tenant)
	distributed := true
	var head *Head
	if which == "candidate" {
		head = a.Candidate
	} else {
		head = a.Serving
		distributed = e.distributedLocked(a)
	}
	trainIDs := append([]string(nil), a.TrainIDs...)
	encoderReady := e.embed != nil
	e.mu.Unlock()
	if head == nil || !head.Ready() {
		return GateReport{Ready: false}, nil
	}
	return Evaluate(e.corpus.ListTenant(tenant), head.TrainedAt, func(s Sample) string {
		emb := e.embedOf(s)
		if len(emb) < HeadDim {
			return ClassUnknown
		}
		return head.Predict(emb).Class
	}, distributed, encoderReady, trainIDs), nil
}

func (e *Engine) Queue(id NodeIdentity) ([]QueueItem, error) {
	return e.QueueFilter(id, "", "")
}

func (e *Engine) QueueFilter(id NodeIdentity, ruleID, siteID string) ([]QueueItem, error) {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return nil, err
	}
	items := e.corpus.ReviewQueueRanked(tenant)
	if rid := strings.TrimSpace(ruleID); rid != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.RuleID == rid {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if sid := strings.TrimSpace(siteID); sid != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.SiteID == sid {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	e.mu.Lock()
	a := e.art(tenant)
	head := a.Serving
	if head == nil || !head.Ready() {
		head = a.Candidate
	}
	e.mu.Unlock()
	for i := range items {
		ruleAction := ClassAction(items[i].RuleClass)
		if ruleAction == "" {
			ruleAction = ActionAllow
		}
		predClass := items[i].HeadClass
		if head != nil && head.Ready() {
			if emb := e.embedOf(items[i].Sample); len(emb) >= HeadDim {
				pred := head.Predict(emb)
				predClass = pred.Class
				items[i].HeadClass = pred.Class
				items[i].HeadMaxP = pred.MaxP
				items[i].HeadProbs = topClassProbs(pred.Probs, 3)
			}
		}
		action, _ := MergeDisposition(ruleAction, predClass, true)
		items[i].WouldAction = action
	}
	return items, nil
}

func (e *Engine) Status(id NodeIdentity) StatusView {
	tenant := strings.TrimSpace(id.TenantID)
	e.mu.Lock()
	a := e.art(tenant)
	out := StatusView{
		Pipeline: a.Pipeline, Training: a.Training, EncoderID: e.encoderID,
		EncoderReady: e.embed != nil, Distributed: e.distributedLocked(a),
		OverrideNote: a.OverrideNote,
		TargetNodes:  append([]string(nil), a.TargetNodes...), DropCount: e.dropCount[tenant],
		MapVersion: MapVersion, ExportEnabled: a.ExportOK,
		CorpusCap: a.CorpusCap, SafetyValve: a.SafetyValve, TrainError: a.TrainError,
		LLMReady: e.arbitrate != nil,
	}
	if out.CorpusCap <= 0 {
		out.CorpusCap = DefaultCorpusCap
	}
	serving := a.Serving
	trainIDs := append([]string(nil), a.TrainIDs...)
	distributed := out.Distributed
	if serving != nil {
		out.ServingHash = serving.Hash()
		out.ServingReady = VerifyHead(serving, e.restKey())
	}
	if used := e.headForNodeLocked(a, id.NodeID); used != nil && a.Previous != nil && used == a.Previous {
		out.UsingPrevious = true
	}
	if a.Candidate != nil {
		out.CandidateHash = a.Candidate.Hash()
	}
	if NormalizePipeline(a.Pipeline) == PipelineCanary {
		out.NATHint = "NAT enlarges the canary bucket; prefer site_id or tenant hash"
	}
	want := ""
	if serving != nil {
		want = serving.Hash()
	}
	for _, n := range a.TargetNodes {
		if a.ACKs[n] != want {
			out.PendingNodes = append(out.PendingNodes, n)
		}
	}
	e.mu.Unlock()
	out.CorpusWritable = e.writable()
	rows := e.corpus.ListTenant(tenant)
	for _, s := range rows {
		switch s.GoldSource {
		case GoldAuto:
			out.GoldAuto++
		case GoldHuman:
			out.GoldHuman++
		case GoldLLM:
			out.GoldLLM++
		}
		if s.GoldSource != GoldHuman && !s.Abstained && HeadMayScore(s.RuleSource) {
			out.QueueCount++
		}
	}
	out.CanTrain = out.EncoderReady && out.CorpusWritable && (out.GoldAuto+out.GoldHuman+out.GoldLLM) > 0
	if !out.EncoderReady {
		out.CannotTrain = append(out.CannotTrain, "encoder_not_ready")
	}
	if !out.CorpusWritable {
		out.CannotTrain = append(out.CannotTrain, "corpus_unwritable")
	}
	if out.GoldAuto+out.GoldHuman+out.GoldLLM == 0 {
		out.CannotTrain = append(out.CannotTrain, "no_gold")
	}
	rep := GateReport{}
	if serving != nil && serving.Ready() {
		rep = Evaluate(rows, serving.TrainedAt, func(s Sample) string {
			emb := e.embedOf(s)
			if len(emb) < HeadDim {
				return ClassUnknown
			}
			return serving.Predict(emb).Class
		}, distributed, out.EncoderReady, trainIDs)
	}
	if !out.ServingReady {
		out.WhyNotPromote = append(out.WhyNotPromote, "no_serving")
	}
	if len(out.TargetNodes) == 0 {
		out.WhyNotPromote = append(out.WhyNotPromote, "empty_targets")
	}
	if out.ServingReady && !out.Distributed {
		out.WhyNotPromote = append(out.WhyNotPromote, "distribution_incomplete")
	}
	if !out.EncoderReady {
		out.WhyNotPromote = append(out.WhyNotPromote, "encoder_not_ready")
	}
	if !rep.ReviewCoverage {
		out.WhyNotPromote = append(out.WhyNotPromote, "coverage")
	}
	if !rep.Accuracy {
		out.WhyNotPromote = append(out.WhyNotPromote, "accuracy")
	}
	if !rep.Recall {
		out.WhyNotPromote = append(out.WhyNotPromote, "recall")
	}
	if !rep.TwoWindows {
		out.WhyNotPromote = append(out.WhyNotPromote, "windows")
	}
	return out
}

func (e *Engine) RecentDecisions(id NodeIdentity) ([]AuditRow, error) {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]AuditRow(nil), e.recent[tenant]...), nil
}

func (e *Engine) RuleHits(id NodeIdentity) (RuleMapView, error) {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return RuleMapView{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.art(tenant)
	seen := map[string]struct{}{}
	out := RuleMapView{Version: MapVersion}
	for rid, n := range e.hits[tenant] {
		seen[rid] = struct{}{}
		out.Entries = append(out.Entries, RuleHit{RuleID: rid, Class: a.RuleMap[rid], Hits: n})
	}
	for rid, class := range a.RuleMap {
		if _, ok := seen[rid]; ok {
			continue
		}
		out.Entries = append(out.Entries, RuleHit{RuleID: rid, Class: class})
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		if out.Entries[i].Hits == out.Entries[j].Hits {
			return out.Entries[i].RuleID < out.Entries[j].RuleID
		}
		return out.Entries[i].Hits > out.Entries[j].Hits
	})
	for host, class := range a.IntelHosts {
		out.Intel = append(out.Intel, IntelHost{Host: host, Class: class})
	}
	sort.Slice(out.Intel, func(i, j int) bool { return out.Intel[i].Host < out.Intel[j].Host })
	out.Sites = append([]string(nil), a.Sites...)
	return out, nil
}

func (e *Engine) SetIntel(id NodeIdentity, role, host, class string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	host = normalizeHost(host)
	if host == "" {
		return fmt.Errorf("%w: host", ErrInvalid)
	}
	class = strings.TrimSpace(class)
	e.mu.Lock()
	a := e.art(tenant)
	if a.IntelHosts == nil {
		a.IntelHosts = map[string]string{}
	}
	if class == "" {
		delete(a.IntelHosts, host)
	} else {
		if !IsTrainableClass(class) {
			e.mu.Unlock()
			return fmt.Errorf("%w: class", ErrInvalid)
		}
		a.IntelHosts[host] = class
	}
	e.mu.Unlock()
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) UpdateSites(id NodeIdentity, role string, add, remove []string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	e.mu.Lock()
	a := e.art(tenant)
	for _, site := range add {
		site = strings.TrimSpace(site)
		if site == "" {
			continue
		}
		e.siteTenant[site] = tenant
		a.Sites = appendUnique(a.Sites, site)
	}
	if len(remove) > 0 {
		drop := map[string]struct{}{}
		for _, site := range remove {
			site = strings.TrimSpace(site)
			drop[site] = struct{}{}
			if e.siteTenant[site] == tenant {
				delete(e.siteTenant, site)
			}
		}
		kept := a.Sites[:0]
		for _, site := range a.Sites {
			if _, ok := drop[site]; !ok {
				kept = append(kept, site)
			}
		}
		a.Sites = kept
	}
	e.mu.Unlock()
	e.flushTenant(tenant)
	return nil
}

func appendUnique(in []string, v string) []string {
	for _, cur := range in {
		if cur == v {
			return in
		}
	}
	return append(in, v)
}

func (e *Engine) Runs(id NodeIdentity) ([]TrainRun, error) {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]TrainRun(nil), e.runs[tenant]...), nil
}

func (e *Engine) embedOf(s Sample) []float32 {
	if len(s.Embedding) >= HeadDim {
		return s.Embedding
	}
	if e.embed == nil || s.EncoderID != e.encoderID {
		return nil
	}
	vec, err := e.embed(s.Preview)
	if err != nil || len(vec) < HeadDim {
		return nil
	}
	return vec
}

func (e *Engine) Observe(id NodeIdentity) (ObserveReport, error) {
	return e.ObserveKind(id, "replay")
}

func (e *Engine) ObserveKind(id NodeIdentity, kind string) (ObserveReport, error) {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return ObserveReport{}, err
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "online" {
		kind = "replay"
	}
	e.mu.Lock()
	a := e.art(tenant)
	head := a.Serving
	if head == nil || !head.Ready() {
		head = a.Candidate
	}
	trainedAt := ""
	if head != nil {
		trainedAt = head.TrainedAt
	}
	mode := NormalizePipeline(a.Pipeline)
	exclude := map[string]struct{}{}
	for _, sid := range a.TrainIDs {
		exclude[sid] = struct{}{}
	}
	e.mu.Unlock()
	out := ObserveReport{
		Cross: map[string]map[string]int{},
		Note:  "observation, not a gate",
		Kind:  kind,
	}
	trained, _ := time.Parse(time.RFC3339, trainedAt)
	if head == nil || !head.Ready() {
		return out, nil
	}
	for _, s := range e.corpus.ListTenant(tenant) {
		if _, skip := exclude[s.ID]; skip || !HeadMayScore(s.RuleSource) {
			continue
		}
		if kind == "online" {
			if mode == PipelineOff {
				continue
			}
			seen, _ := time.Parse(time.RFC3339, s.LastSeen)
			if trained.IsZero() || seen.Before(trained) || s.HeadClass == "" {
				continue
			}
		}
		var pred Prediction
		if kind == "online" {
			pred = Prediction{Class: s.HeadClass, MaxP: s.HeadMaxP}
		} else {
			emb := e.embedOf(s)
			if len(emb) < HeadDim {
				continue
			}
			pred = head.Predict(emb)
		}
		out.Compared++
		if pred.Class == ClassUnknown || pred.MaxP < DefaultTau {
			out.LowConf++
		}
		if pred.Class == s.RuleClass {
			out.Agree++
		}
		if pred.Class != ClassUnknown && pred.Class != s.RuleClass && pred.MaxP >= DefaultTau {
			if action, used := MergeDisposition(ClassAction(s.RuleClass), pred.Class, true); used {
				out.Rewrite++
				if action == ActionBlock {
					out.BlockRate++
				}
			}
		}
		if out.Cross[s.RuleClass] == nil {
			out.Cross[s.RuleClass] = map[string]int{}
		}
		out.Cross[s.RuleClass][pred.Class]++
	}
	if out.Compared > 0 {
		out.RewriteRate = float64(out.Rewrite) / float64(out.Compared)
		out.BlockRate = out.BlockRate / float64(out.Compared)
	}
	e.mu.Lock()
	dropped := e.unlabelN[tenant]
	e.mu.Unlock()
	humans := 0
	for _, s := range e.corpus.ListTenant(tenant) {
		if s.GoldSource == GoldHuman {
			humans++
		}
	}
	if humans+dropped > 0 {
		out.UnlabelRate = float64(dropped) / float64(humans+dropped)
	}
	return out, nil
}

func (e *Engine) evalHead(tenant string, head *Head, distributed bool, trainIDs []string) GateReport {
	if head == nil || !head.Ready() {
		return GateReport{Ready: false}
	}
	return Evaluate(e.corpus.ListTenant(tenant), head.TrainedAt, func(s Sample) string {
		emb := e.embedOf(s)
		if len(emb) < HeadDim {
			return ClassUnknown
		}
		return head.Predict(emb).Class
	}, distributed, e.embed != nil, trainIDs)
}

func (e *Engine) distributedLocked(a *Artifact) bool {
	if a.Serving == nil {
		return false
	}
	if len(a.TargetNodes) == 0 {
		return false
	}
	want := a.Serving.Hash()
	for _, n := range a.TargetNodes {
		if a.ACKs[n] != want {
			return false
		}
	}
	return true
}

func rankPipeline(mode string) int {
	switch NormalizePipeline(mode) {
	case PipelineOff:
		return 0
	case PipelineShadow:
		return 1
	case PipelineCanary:
		return 2
	case PipelineOn:
		return 3
	default:
		return 0
	}
}

func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (e *Engine) RemapRule(id NodeIdentity, role, ruleID, class string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return fmt.Errorf("%w: rule id", ErrInvalid)
	}
	class = strings.TrimSpace(class)
	e.mu.Lock()
	a := e.art(tenant)
	if a.RuleMap == nil {
		a.RuleMap = map[string]string{}
	}
	a.RuleMap[ruleID] = class
	e.mu.Unlock()
	e.corpus.RemapAuto(tenant, ruleID, class)
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) SetCorpusCap(id NodeIdentity, role string, cap int) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	if cap <= 0 {
		cap = DefaultCorpusCap
	}
	e.mu.Lock()
	e.art(tenant).CorpusCap = cap
	e.mu.Unlock()
	e.corpus.TrimTenant(tenant, cap)
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) SetExport(id NodeIdentity, role string, enabled bool, reason string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	if enabled {
		n := len([]rune(strings.TrimSpace(reason)))
		if n < 10 || n > 2000 {
			return fmt.Errorf("%w: export reason", ErrInvalid)
		}
	}
	e.mu.Lock()
	e.art(tenant).ExportOK = enabled
	e.mu.Unlock()
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) Export(id NodeIdentity, role string) ([]ExportRow, error) {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return nil, err
	}
	if role != RoleAdmin {
		return nil, ErrForbidden
	}
	e.mu.Lock()
	ok := e.art(tenant).ExportOK
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: export disabled", ErrForbidden)
	}
	var out []ExportRow
	for _, s := range e.corpus.ListTenant(tenant) {
		out = append(out, ExportRow{
			ID: s.ID, RuleClass: s.RuleClass, RuleID: s.RuleID,
			GoldClass: s.GoldClass, GoldSource: s.GoldSource, Preview: s.Preview,
		})
	}
	return out, nil
}

func (e *Engine) Lookup(id NodeIdentity, sampleID string) (SampleView, error) {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return SampleView{}, err
	}
	s, ok := e.corpus.Get(strings.TrimSpace(sampleID))
	if !ok || s.TenantID != tenant {
		return SampleView{}, ErrNotFound
	}
	return SampleView{
		ID: s.ID, Preview: s.Preview, RuleClass: s.RuleClass, RuleID: s.RuleID,
		GoldClass: s.GoldClass, GoldSource: s.GoldSource, HeadClass: s.HeadClass,
	}, nil
}

func (e *Engine) ImportCandidate(id NodeIdentity, role string, head Head) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAdmin {
		return ErrForbidden
	}
	if !head.Ready() {
		return fmt.Errorf("%w: head", ErrInvalid)
	}
	head.TrainedAt = e.now().UTC().Format(time.RFC3339)
	head.hash = atomic.Value{}
	SignHead(&head, e.restKey())
	e.mu.Lock()
	e.art(tenant).Candidate = head.Clone()
	e.mu.Unlock()
	e.flushTenant(tenant)
	return nil
}

func topClassProbs(probs map[string]float64, n int) []ClassProb {
	out := make([]ClassProb, 0, len(probs))
	for class, p := range probs {
		out = append(out, ClassProb{Class: class, P: p})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].P == out[j].P {
			return out[i].Class < out[j].Class
		}
		return out[i].P > out[j].P
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
