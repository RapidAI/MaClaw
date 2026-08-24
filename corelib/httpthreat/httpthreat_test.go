package httpthreat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func oneHot(class string) []float32 {
	emb := make([]float32, HeadDim)
	if i := classIndex(class); i >= 0 {
		emb[i] = 1
	}
	return emb
}

func TestBuildPreviewRedactsAndNormalizes(t *testing.T) {
	tx := Transaction{
		Method: "get", Host: "WWW.Example.COM:443.", Path: "/a/%2e%2e/secret",
		Query:     "z=1&token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.xxx&a=2",
		UserAgent: "Mozilla", ContentType: "Text/HTML", Referer: "https://WWW.Example.COM/path?x=1",
		Body:    "password=hunter2 and sk-abcdefghijklmnopqrstuvwxyz12",
		Headers: map[string]string{"Authorization": "Bearer abc", "Cookie": "sid=1"},
	}
	prev := BuildPreview(tx)
	if strings.Contains(prev, "hunter2") || strings.Contains(prev, "Bearer abc") || strings.Contains(strings.ToLower(prev), "sid=1") {
		t.Fatalf("leaked secret: %s", prev)
	}
	if strings.Contains(prev, "eyJhbGci") {
		t.Fatalf("jwt leaked: %s", prev)
	}
	if !strings.Contains(prev, "METHOD GET") || !strings.Contains(prev, "HOST www.example.com") {
		t.Fatalf("normalize failed: %s", prev)
	}
	if !strings.HasPrefix(prev, "METHOD ") || !strings.Contains(prev, "\nSTATUS \n") {
		t.Fatalf("template rows missing: %s", prev)
	}
	if len([]rune(prev)) > MaxPreviewRunes {
		t.Fatalf("too long %d", len([]rune(prev)))
	}
	id1 := SampleID("t1", "encA", prev)
	id2 := SampleID("t2", "encA", prev)
	if id1 == id2 || len(id1) != 40 {
		t.Fatalf("ids %s %s", id1, id2)
	}
}

func TestMergeDispositionNeverDowngrades(t *testing.T) {
	act, used := MergeDisposition(ActionBlock, ClassBenign, true)
	if used || act != ActionBlock {
		t.Fatalf("block->benign used=%v act=%s", used, act)
	}
	act, used = MergeDisposition(ActionAllow, ClassExploit, true)
	if !used || act != ActionBlock {
		t.Fatalf("allow->exploit used=%v act=%s", used, act)
	}
	act, used = MergeDisposition(ActionBlock, ClassExploit, false)
	if used || act != ActionBlock {
		t.Fatalf("shadow must keep rule")
	}
}

func TestRulesP0BeatsHeuristic(t *testing.T) {
	eng := NewRuleEngine()
	hit := eng.Classify(Transaction{Path: "/x", Query: "q=../etc/passwd", Method: "GET", Host: "a.test"})
	if hit.Source != SourceSignature || hit.Class != ClassExploit {
		t.Fatalf("%+v", hit)
	}
	hit = eng.Classify(Transaction{Path: "/", Query: "select=1", Method: "GET", Host: "a.test"})
	if hit.Source != SourceHeuristic {
		t.Fatalf("%+v", hit)
	}
}

func TestDetectIdentityTenantWins(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	_, err := e.Detect(NodeIdentity{}, Transaction{Path: "/"})
	if err != ErrUnauthorized {
		t.Fatalf("want unauthorized %v", err)
	}
	_, err = e.Detect(NodeIdentity{TenantID: "a"}, Transaction{TenantID: "b", Path: "/"})
	if err != ErrForbidden {
		t.Fatalf("want forbidden %v", err)
	}
	dec, err := e.Detect(NodeIdentity{TenantID: "a"}, Transaction{Path: "/", Method: "GET", Host: "x.test"})
	if err != nil || dec.Action == "" || dec.SampleID == "" {
		t.Fatalf("%+v %v", dec, err)
	}
	if dec.HeadUsed {
		t.Fatal("off must not rewrite")
	}
}

func TestOffShadowNeverRewriteP0NeverEmbedNeeded(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) {
		t.Fatal("should not embed")
		return nil, nil
	})
	id := NodeIdentity{TenantID: "t", NodeID: "n1"}
	dec, err := e.Detect(id, Transaction{Path: "/p", Query: "x=../etc/passwd", Method: "GET", Host: "h"})
	if err != nil || dec.RuleSource != SourceSignature || dec.HeadUsed {
		t.Fatalf("%+v %v", dec, err)
	}
	if err := e.SetPipeline(id, PipelineShadow, "", "", RoleAdmin); err == nil {
		t.Fatal("shadow without serving must fail")
	}
}

func TestTrainAdoptShadowAndUpgradeOnly(t *testing.T) {
	e := NewEngine("enc1", func(preview string) ([]float32, error) {
		p := strings.ToLower(preview)
		if strings.Contains(p, "passwd") || strings.Contains(p, "payload-x") || strings.Contains(p, "select=") {
			return oneHot(ClassExploit), nil
		}
		return oneHot(ClassBenign), nil
	})
	id := NodeIdentity{TenantID: "t", NodeID: "n1"}
	for i := 0; i < 3; i++ {
		_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
		_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/p", Query: "x=../etc/passwd"})
	}
	q := e.corpus.ReviewQueue("t")
	if len(q) == 0 {
		t.Fatal("expected review queue")
	}
	if err := e.Label(id, LabelRequest{SampleID: q[0].ID, GoldClass: ClassBenign, Role: RoleAnalyst}); err != nil {
		t.Fatal(err)
	}
	if err := e.Train(id, RoleAnalyst); err != ErrForbidden {
		t.Fatalf("analyst train %v", err)
	}
	if err := e.Train(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	st := e.Status(id)
	if st.ServingReady || st.CandidateHash == "" {
		t.Fatalf("train must write candidate only %+v", st)
	}
	if err := e.SetPipeline(id, PipelineCanary, "", "", RoleAdmin); err == nil {
		t.Fatal("off must not jump to canary")
	}
	if err := e.Adopt(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPipeline(id, PipelineOn, "", "", RoleAdmin); err == nil {
		t.Fatal("must not skip shadow")
	}
	if err := e.SetPipeline(id, PipelineShadow, "", "", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPipeline(id, PipelineOn, PromoteOverride, "need it for test", RoleAdmin); err == nil {
		t.Fatal("empty target set must not raise canary/on")
	}
	e.SetTargets("t", []string{"n1"})
	if err := e.ACK(id, ""); err != nil {
		t.Fatal(err)
	}
	dec, _ := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/", Query: "select=1"})
	if dec.HeadUsed {
		t.Fatal("shadow must not rewrite")
	}
	if err := e.SetPipeline(id, PipelineOn, PromoteOverride, "need it for test", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	dec, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/payload-x"})
	if dec.Action != ActionBlock || !dec.HeadUsed {
		t.Fatalf("on should upgrade fallback %+v", dec)
	}
	dec, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/p", Query: "x=../etc/passwd"})
	if dec.HeadUsed || dec.Action != ActionBlock {
		t.Fatalf("P0 must stay rule %+v", dec)
	}
}

func TestTrainHeadSeparatesClasses(t *testing.T) {
	var rows []labeledEmb
	for _, c := range TrainableClasses {
		rows = append(rows, labeledEmb{Emb: oneHot(c), Label: c})
	}
	head, err := TrainHead(rows, 1, 0.3, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range TrainableClasses {
		if got := head.Predict(oneHot(c)).Class; got != c {
			t.Fatalf("%s -> %s", c, got)
		}
	}
	if head.Hash() == "" || head.Hash() != head.Hash() {
		t.Fatal("hash must be stable")
	}
}

func TestACKRequiresNodeID(t *testing.T) {
	e := NewEngine("enc1", HashEmbed)
	h := EmptyHead(1, DefaultTau)
	e.mu.Lock()
	e.art("t").Serving = &h
	e.mu.Unlock()
	if err := e.ACK(NodeIdentity{TenantID: "t"}, h.Hash()); err != ErrInvalid {
		t.Fatalf("empty node id %v", err)
	}
}

func TestCanaryRequiresTenantSite(t *testing.T) {
	if CanarySelected("t1", "site", "", "t2") {
		t.Fatal("foreign site must not select")
	}
	if CanarySelected("", "", "", "") {
		t.Fatal("empty identity")
	}
}

func TestStatusReportsEmptyTargetsAndNoGold(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	st := e.Status(id)
	if st.CanTrain || len(st.CannotTrain) == 0 {
		t.Fatalf("empty pool must not train %+v", st)
	}
	found := false
	for _, w := range st.WhyNotPromote {
		if w == "empty_targets" {
			found = true
		}
	}
	if !found {
		t.Fatalf("empty targets must block promote %+v", st)
	}
}

func TestGateAgreementIsNotPromote(t *testing.T) {
	rep := Evaluate(nil, time.Now().UTC().Format(time.RFC3339), nil, true, true, nil)
	if rep.CanPromote {
		t.Fatal("empty must not promote")
	}
	if rep.Agreement != 0 {
		t.Fatal("agreement is observation only")
	}
	if Evaluate(nil, time.Now().UTC().Format(time.RFC3339), nil, true, false, nil).Ready {
		t.Fatal("encoder down must be not-ready")
	}
}

func TestTenantIsolationAndLLMNotInAccuracy(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	a := NodeIdentity{TenantID: "a", NodeID: "n"}
	b := NodeIdentity{TenantID: "b", NodeID: "n"}
	dec, err := e.Detect(a, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if err != nil {
		t.Fatal(err)
	}
	q, _ := e.Queue(b)
	if len(q) != 0 {
		t.Fatal("foreign tenant saw samples")
	}
	if err := e.Label(b, LabelRequest{SampleID: dec.SampleID, GoldClass: ClassBenign, Role: RoleAnalyst}); err != ErrNotFound {
		t.Fatalf("cross-tenant label %v", err)
	}
	now := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	llm := Sample{
		ID: "llm1", TenantID: "a", GoldClass: ClassExploit, GoldSource: GoldLLM,
		LabeledAt: now, RuleSource: SourceHeuristic, Embedding: oneHot(ClassExploit),
	}
	human := Sample{
		ID: "h1", TenantID: "a", GoldClass: ClassExploit, GoldSource: GoldHuman,
		LabeledAt: now, RuleSource: SourceHeuristic, Embedding: oneHot(ClassExploit),
	}
	rep := Evaluate([]Sample{llm, human}, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), func(s Sample) string {
		return ClassExploit
	}, true, true, nil)
	if rep.Recent.Reviews != 1 {
		t.Fatalf("llm gold must stay out of accuracy window: %+v", rep.Recent)
	}
}

func TestSafetyValveDemotesToShadow(t *testing.T) {
	e := NewEngine("enc1", func(preview string) ([]float32, error) {
		p := strings.ToLower(preview)
		if strings.Contains(p, "passwd") || strings.Contains(p, "payload-x") {
			return oneHot(ClassExploit), nil
		}
		return oneHot(ClassBenign), nil
	})
	e.blockLimit = 0
	id := NodeIdentity{TenantID: "t", NodeID: "n1"}
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/p", Query: "x=../etc/passwd"})
	q := e.corpus.ReviewQueue("t")
	_ = e.Label(id, LabelRequest{SampleID: q[0].ID, GoldClass: ClassBenign, Role: RoleAnalyst})
	if err := e.Train(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPipeline(id, PipelineShadow, "", "", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	e.SetTargets("t", []string{"n1"})
	if err := e.ACK(id, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPipeline(id, PipelineOn, PromoteOverride, "need it for test", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	dec, _ := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/payload-x"})
	if !dec.Demoted || dec.Action == ActionBlock && dec.HeadUsed {
		t.Fatalf("safety valve %+v", dec)
	}
	if e.Status(id).Pipeline != PipelineShadow {
		t.Fatalf("want shadow after valve %+v", e.Status(id))
	}
}

func TestReviewQueueRanksHotAndAbstain(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassExploit), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	now := time.Now().UTC()
	for _, class := range TrainableClasses {
		e.corpus.Upsert(now, Sample{
			ID: "gold-" + class, TenantID: "t", GoldClass: class, GoldSource: GoldHuman,
			RuleSource: SourceHeuristic, EncoderID: "enc1",
		})
	}
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok-1"})
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok-2"})
	for _, s := range e.corpus.ListTenant("t") {
		if strings.HasPrefix(s.ID, "gold-") {
			continue
		}
		s.RuleClass = ClassBenign
		s.RuleSource = SourceFallback
		s.HeadClass = ClassExploit
		s.HeadMaxP = 0.9
		e.corpus.Upsert(now, s)
	}
	ranked := e.corpus.ReviewQueueRanked("t")
	if len(ranked) < 2 || ranked[0].QueueReason != "hot" {
		t.Fatalf("hot cell should rank before recency %+v", ranked)
	}
	if err := e.Label(id, LabelRequest{SampleID: ranked[0].ID, Abstain: true, Role: RoleAnalyst}); err != nil {
		t.Fatal(err)
	}
	after, _ := e.Queue(id)
	for _, item := range after {
		if item.ID == ranked[0].ID {
			t.Fatal("abstain must leave the default queue")
		}
	}
}

func TestQueueCardWouldActionAndBatch(t *testing.T) {
	e := NewEngine("enc1", func(preview string) ([]float32, error) {
		if strings.Contains(strings.ToLower(preview), "payload") || strings.Contains(strings.ToLower(preview), "passwd") {
			return oneHot(ClassExploit), nil
		}
		return oneHot(ClassBenign), nil
	})
	id := NodeIdentity{TenantID: "t", NodeID: "n1"}
	now := time.Now().UTC()
	for _, class := range []string{ClassBenign, ClassExploit} {
		e.corpus.Upsert(now, Sample{
			ID: "gold-" + class, TenantID: "t", EncoderID: "enc1",
			GoldClass: class, GoldSource: GoldHuman, Embedding: oneHot(class),
			RuleSource: SourceHeuristic,
		})
	}
	if err := e.Train(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/payload-x"})
	cards, err := e.Queue(id)
	if err != nil || len(cards) == 0 {
		t.Fatalf("queue %v %+v", err, cards)
	}
	found := false
	for _, card := range cards {
		if strings.Contains(card.Preview, "payload") {
			found = true
			if card.WouldAction != ActionBlock {
				t.Fatalf("promote would-action %+v", card)
			}
			if len(card.HeadProbs) == 0 || card.HeadProbs[0].Class == "" {
				t.Fatalf("top-3 missing %+v", card)
			}
		}
	}
	if !found {
		t.Fatal("payload sample missing from queue")
	}
	ids := make([]string, 0, len(cards))
	for _, card := range cards {
		ids = append(ids, card.ID)
	}
	if err := e.LabelBatch(id, BatchLabelRequest{SampleIDs: ids, GoldClass: ClassScan, Role: RoleAnalyst}); err != nil {
		t.Fatal(err)
	}
	if err := e.LabelBatch(id, BatchLabelRequest{SampleIDs: ids, GoldClass: ClassScan, Role: RoleAnalyst}); err == nil {
		t.Fatal("already labeled rows must not batch")
	}
	tooMany := make([]string, BatchLabelLimit+1)
	if err := e.LabelBatch(id, BatchLabelRequest{SampleIDs: tooMany, GoldClass: ClassScan, Role: RoleAnalyst}); err == nil {
		t.Fatal("batch over 50 must fail")
	}
}

func TestSkipCorpusNonHTTPAndHealth(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	if _, err := e.Detect(id, Transaction{Method: "GET", Path: "/healthz"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ws", ContentType: "application/grpc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/chat", Upgrade: "websocket"}); err != nil {
		t.Fatal(err)
	}
	if n := len(e.corpus.ListTenant("t")); n != 0 {
		t.Fatalf("non-http/health must not enter corpus, got %d", n)
	}
	dec, err := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if err != nil || dec.RuleID != "p4.fallback" {
		t.Fatalf("%+v %v", dec, err)
	}
	if n := len(e.corpus.ListTenant("t")); n != 1 {
		t.Fatalf("normal http should record, got %d", n)
	}
}

func TestQueueFilterByRuleID(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok", SiteID: "s1"})
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/x", Query: "select=1", SiteID: "s2"})
	all, _ := e.Queue(id)
	if len(all) < 2 {
		t.Fatalf("want both P3/P4 %+v", all)
	}
	only, _ := e.QueueFilter(id, "p3.sqli.select", "")
	if len(only) != 1 || only[0].RuleID != "p3.sqli.select" {
		t.Fatalf("rule filter %+v", only)
	}
	site, _ := e.QueueFilter(id, "", "s1")
	if len(site) != 1 || site[0].SiteID != "s1" {
		t.Fatalf("site filter %+v", site)
	}
}

func TestAutoGoldRuleCapDownweights(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassExploit), nil })
	now := time.Now().UTC()
	for i := 0; i < 20; i++ {
		e.corpus.Upsert(now, Sample{
			ID: "auto-" + strings.Repeat("x", i+1), TenantID: "t", EncoderID: "enc1",
			GoldClass: ClassExploit, GoldSource: GoldAuto, RuleID: "p0.lfi.passwd",
			RuleSource: SourceSignature, Embedding: oneHot(ClassExploit),
		})
	}
	e.corpus.Upsert(now, Sample{
		ID: "auto-rare", TenantID: "t", EncoderID: "enc1",
		GoldClass: ClassMalware, GoldSource: GoldAuto, RuleID: "p0.malware.sqlmap",
		RuleSource: SourceSignature, Embedding: oneHot(ClassMalware),
	})
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	if err := e.Train(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if e.Status(id).CandidateHash == "" {
		t.Fatal("cap must still train")
	}
}

func TestDetectRecoversFromEmbedPanic(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { panic("embed") })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	head, err := TrainHead([]labeledEmb{{Emb: oneHot(ClassExploit), Label: ClassExploit}}, 1, DefaultTau, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	e.art("t").Serving = &head
	e.art("t").Pipeline = PipelineOn
	dec, err := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if err != nil || dec.HeadUsed || dec.Action != ActionAllow {
		t.Fatalf("panic must keep rules only %+v %v", dec, err)
	}
}

func TestInvalidSigDisablesServing(t *testing.T) {
	dir := t.TempDir()
	e := NewEngineAt(dir, "enc1", func(string) ([]float32, error) { return oneHot(ClassExploit), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n1"}
	now := time.Now().UTC()
	e.corpus.Upsert(now, Sample{
		ID: "g", TenantID: "t", EncoderID: "enc1", GoldClass: ClassExploit, GoldSource: GoldHuman,
		Embedding: oneHot(ClassExploit), RuleSource: SourceHeuristic,
	})
	if err := e.Train(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	e.SetTargets("t", []string{"n1"})
	_ = e.ACK(id, "")
	_ = e.SetPipeline(id, PipelineShadow, "", "", RoleAdmin)
	_ = e.SetPipeline(id, PipelineOn, PromoteOverride, "need it for test", RoleAdmin)
	e.art("t").Serving.Sig = "00"
	dec, _ := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/payload-x"})
	if dec.HeadUsed {
		t.Fatalf("bad sig must not rewrite %+v", dec)
	}
}

func TestRemapVoidsAutoGold(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassExploit), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	dec, err := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/p", Query: "x=../etc/passwd"})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := e.corpus.Get(dec.SampleID)
	if !ok || s.GoldSource != GoldAuto || s.RuleID != "p0.lfi.passwd" {
		t.Fatalf("auto gold %+v", s)
	}
	if err := e.RemapRule(id, RoleAdmin, "p0.lfi.passwd", ""); err != nil {
		t.Fatal(err)
	}
	s, _ = e.corpus.Get(dec.SampleID)
	if s.GoldSource != "" {
		t.Fatalf("voided auto gold %+v", s)
	}
	if e.Status(id).ServingReady {
		t.Fatal("remap must not swap serving")
	}
}

func TestHTTP2PseudoHeaders(t *testing.T) {
	tx := Transaction{Headers: map[string]string{":method": "POST", ":authority": "WWW.Example.COM", ":path": "/v2"}}
	applyHTTP2Pseudo(&tx)
	prev := BuildPreview(tx)
	if !strings.Contains(prev, "METHOD POST") || !strings.Contains(prev, "HOST www.example.com") || !strings.Contains(prev, "PATH /v2") {
		t.Fatalf("%s", prev)
	}
}

func TestExportRequiresAdminEnable(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if _, err := e.Export(id, RoleAdmin); err == nil {
		t.Fatal("export default off")
	}
	if err := e.SetExport(id, RoleAdmin, true, "need redacted dump"); err != nil {
		t.Fatal(err)
	}
	rows, err := e.Export(id, RoleAdmin)
	if err != nil || len(rows) == 0 || rows[0].Preview == "" {
		t.Fatalf("%+v %v", rows, err)
	}
}

func TestImportUsesLocalClock(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	head := EmptyHead(1, DefaultTau)
	head.TrainedAt = "2010-01-01T00:00:00Z"
	if err := e.ImportCandidate(id, RoleAdmin, head); err != nil {
		t.Fatal(err)
	}
	st := e.Status(id)
	if st.CandidateHash == "" || strings.HasPrefix(st.CandidateHash, "2010") {
		t.Fatalf("%+v", st)
	}
	if e.art("t").Candidate.TrainedAt == "2010-01-01T00:00:00Z" {
		t.Fatal("must rewrite trained_at to trainer clock")
	}
}

func TestUnackedNodeUsesPreviousAfterReadopt(t *testing.T) {
	e := NewEngine("enc1", func(preview string) ([]float32, error) {
		if strings.Contains(strings.ToLower(preview), "payload") {
			return oneHot(ClassExploit), nil
		}
		return oneHot(ClassBenign), nil
	})
	id1 := NodeIdentity{TenantID: "t", NodeID: "n1"}
	id2 := NodeIdentity{TenantID: "t", NodeID: "n2"}
	now := time.Now().UTC()
	e.corpus.Upsert(now, Sample{
		ID: "g1", TenantID: "t", EncoderID: "enc1", GoldClass: ClassBenign, GoldSource: GoldHuman,
		Embedding: oneHot(ClassBenign), RuleSource: SourceHeuristic,
	})
	e.corpus.Upsert(now, Sample{
		ID: "g2", TenantID: "t", EncoderID: "enc1", GoldClass: ClassExploit, GoldSource: GoldHuman,
		Embedding: oneHot(ClassExploit), RuleSource: SourceHeuristic,
	})
	if err := e.Train(id1, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(id1, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	first := e.Status(id1).ServingHash
	dec, _ := e.Detect(id2, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.ServingHash != first {
		t.Fatalf("first adopt without previous must use serving, got %s want %s", dec.ServingHash, first)
	}
	e.SetTargets("t", []string{"n1", "n2"})
	_ = e.ACK(id1, "")
	_ = e.ACK(id2, "")
	_ = e.SetPipeline(id1, PipelineShadow, "", "", RoleAdmin)
	e.corpus.Upsert(now.Add(time.Second), Sample{
		ID: "g3", TenantID: "t", EncoderID: "enc1", GoldClass: ClassScan, GoldSource: GoldHuman,
		Embedding: oneHot(ClassScan), RuleSource: SourceHeuristic,
	})
	if err := e.Train(id1, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(id1, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	prev := e.art("t").Previous.Hash()
	serv := e.art("t").Serving.Hash()
	if prev == "" || prev == serv {
		t.Fatal("expected distinct previous/serving")
	}
	dec, _ = e.Detect(id2, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.ServingHash != prev {
		t.Fatalf("unacked node must keep previous %s got %s", prev, dec.ServingHash)
	}
	if err := e.ACK(id2, ""); err != nil {
		t.Fatal(err)
	}
	dec, _ = e.Detect(id2, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.ServingHash != serv {
		t.Fatalf("acked node must use serving %s got %s", serv, dec.ServingHash)
	}
	b, err := e.Bundle(id2)
	if err != nil || b.Hash != serv || b.Serving == nil || b.Previous == nil {
		t.Fatalf("bundle %+v %v", b, err)
	}
}

func TestUpdateTargetsAddRemove(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "admin"}
	if err := e.UpdateTargets(id, RoleAnalyst, nil, []string{"n1"}, nil); err != ErrForbidden {
		t.Fatalf("analyst %v", err)
	}
	if err := e.UpdateTargets(id, RoleAdmin, nil, []string{"n1", "n2"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.UpdateTargets(id, RoleAdmin, nil, nil, []string{"n1"}); err != nil {
		t.Fatal(err)
	}
	st := e.Status(id)
	if len(st.TargetNodes) != 1 || st.TargetNodes[0] != "n2" {
		t.Fatalf("targets %+v", st.TargetNodes)
	}
}

func TestLookupFillsRunWithoutEmbedding(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	a := NodeIdentity{TenantID: "a", NodeID: "n"}
	b := NodeIdentity{TenantID: "b", NodeID: "n"}
	dec, err := e.Detect(a, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := e.Lookup(a, dec.SampleID)
	if err != nil || view.Preview == "" || view.ID != dec.SampleID {
		t.Fatalf("%+v %v", view, err)
	}
	if _, err := e.Lookup(b, dec.SampleID); err != ErrNotFound {
		t.Fatalf("cross-tenant lookup %v", err)
	}
	if err := e.SetCorpusCap(a, RoleAdmin, 10); err != nil {
		t.Fatal(err)
	}
	if e.Status(a).CorpusCap != 10 {
		t.Fatalf("cap %+v", e.Status(a))
	}
}

func TestStartTrainDoesNotBlockDetect(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	if _, err := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/p", Query: "x=../etc/passwd"}); err != nil {
		t.Fatal(err)
	}
	if err := e.StartTrain(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for e.Status(id).Training && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	st := e.Status(id)
	if st.Training || st.CandidateHash == "" {
		t.Fatalf("async train %+v", st)
	}
}

func TestRecentDecisionsAndRuleHitsTenantScoped(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	a := NodeIdentity{TenantID: "a", NodeID: "n"}
	b := NodeIdentity{TenantID: "b", NodeID: "n"}
	if _, err := e.Detect(a, Transaction{Method: "GET", Host: "h", Path: "/ok"}); err != nil {
		t.Fatal(err)
	}
	rows, err := e.RecentDecisions(a)
	if err != nil || len(rows) == 0 || rows[0].Preview == "" {
		t.Fatalf("audit %+v %v", rows, err)
	}
	cross, err := e.RecentDecisions(b)
	if err != nil || len(cross) != 0 {
		t.Fatalf("cross-tenant audit %+v %v", cross, err)
	}
	hits, err := e.RuleHits(a)
	if err != nil || hits.Version != MapVersion || len(hits.Entries) == 0 {
		t.Fatalf("hits %+v %v", hits, err)
	}
}

func TestIntelHostIsTenantScoped(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	a := NodeIdentity{TenantID: "a", NodeID: "n"}
	b := NodeIdentity{TenantID: "b", NodeID: "n"}
	if err := e.SetIntel(a, RoleAdmin, "evil.test", ClassMalware); err != nil {
		t.Fatal(err)
	}
	if err := e.SetIntel(a, RoleAnalyst, "evil.test", ClassMalware); err != ErrForbidden {
		t.Fatalf("analyst intel %v", err)
	}
	decA, err := e.Detect(a, Transaction{Method: "GET", Host: "evil.test", Path: "/ok"})
	if err != nil {
		t.Fatal(err)
	}
	if decA.RuleSource != SourceIntel || decA.RuleClass != ClassMalware {
		t.Fatalf("tenant intel %+v", decA)
	}
	decB, err := e.Detect(b, Transaction{Method: "GET", Host: "evil.test", Path: "/ok"})
	if err != nil {
		t.Fatal(err)
	}
	if decB.RuleSource == SourceIntel {
		t.Fatalf("foreign tenant must not see intel %+v", decB)
	}
}

func TestBoundSiteSelectsCanaryBucket(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	if err := e.UpdateSites(id, RoleAdmin, []string{"shop-1"}, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := e.RuleHits(id)
	if err != nil || len(hits.Sites) != 1 || hits.Sites[0] != "shop-1" {
		t.Fatalf("sites %+v %v", hits, err)
	}
	if e.siteTenant["shop-1"] != "t" {
		t.Fatalf("bind %+v", e.siteTenant)
	}
}

func TestP2SessionFromAuthFailures(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	for i := 0; i < 20; i++ {
		_, _ = e.Detect(id, Transaction{Method: "POST", Host: "h", Path: "/login", Status: "401", SourceID: "1.2.3.4"})
	}
	dec, err := e.Detect(id, Transaction{Method: "POST", Host: "h", Path: "/login", Status: "401", SourceID: "1.2.3.4"})
	if err != nil || dec.RuleSource != SourceSession || dec.RuleClass != ClassAuthAbuse {
		t.Fatalf("p2 %+v %v", dec, err)
	}
}

func TestIngestKeepsNodeEmbedding(t *testing.T) {
	n := 0
	e := NewEngine("enc1", func(string) ([]float32, error) {
		n++
		return oneHot(ClassBenign), nil
	})
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	tx := Transaction{Method: "GET", Host: "h", Path: "/ok"}
	dec := Decision{SampleID: SampleID("t", "enc1", BuildPreview(tx)), RuleClass: ClassBenign, RuleSource: SourceFallback, Action: ActionAllow, Preview: BuildPreview(tx)}
	if err := e.IngestSample(id, IngestRequest{Transaction: tx, Decision: &dec, Embedding: oneHot(ClassBenign), EncoderID: "enc1"}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("trainer must not re-embed, got %d", n)
	}
	rows, _ := e.RecentDecisions(id)
	if len(rows) == 0 {
		t.Fatal("ingest must record")
	}
}

func TestNodeUsesPreviousUntilAck(t *testing.T) {
	prev := EmptyHead(1, DefaultTau)
	serv := EmptyHead(2, DefaultTau)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n1"}, "", "", "enc1", func(string) ([]float32, error) { return oneHot(ClassExploit), nil })
	node.Install(ServingBundle{Pipeline: PipelineOn, Serving: &serv, Previous: &prev, Hash: serv.Hash()}, prev.Hash())
	dec, _ := node.Detect(Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.ServingHash != prev.Hash() {
		t.Fatalf("unacked must use previous %s vs %s", dec.ServingHash, prev.Hash())
	}
	node.Install(ServingBundle{Pipeline: PipelineOn, Serving: &serv, Previous: &prev, Hash: serv.Hash()}, serv.Hash())
	dec, _ = node.Detect(Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.ServingHash != serv.Hash() {
		t.Fatalf("acked must use serving %s", dec.ServingHash)
	}
}

func TestFromRequestDropsSecretsAndKeepsBody(t *testing.T) {
	body := "password=hunter2&ok=1"
	r := httptest.NewRequest(http.MethodPost, "https://WWW.Example.COM/a?z=1&token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.xxx", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret-token")
	r.Header.Set("Cookie", "sid=abc")
	r.Header.Set("User-Agent", "Mozilla")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Site-ID", "site-1")
	r.RemoteAddr = "203.0.113.9:1234"
	tx := FromRequest(r)
	if tx.Status != "" {
		t.Fatal("request-side status must be empty")
	}
	if tx.SiteID != "site-1" || tx.SourceID != "203.0.113.9" {
		t.Fatalf("ids %+v", tx)
	}
	if _, ok := tx.Headers["Authorization"]; ok {
		t.Fatal("authorization leaked")
	}
	if _, ok := tx.Headers["Cookie"]; ok {
		t.Fatal("cookie leaked")
	}
	got, _ := io.ReadAll(r.Body)
	if string(got) != body {
		t.Fatalf("body not restored: %q", got)
	}
	prev := BuildPreview(tx)
	if strings.Contains(prev, "secret-token") || strings.Contains(prev, "sid=abc") || !strings.Contains(prev, "\nSTATUS \n") {
		t.Fatalf("preview %s", prev)
	}
}

func TestWriteActionStopsStrictActions(t *testing.T) {
	rec := httptest.NewRecorder()
	if !WriteAction(rec, Decision{Action: ActionBlock}) || rec.Code != http.StatusForbidden {
		t.Fatalf("block %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	if WriteAction(rec, Decision{Action: ActionAllow}) || rec.Code != http.StatusOK {
		t.Fatal("allow must continue")
	}
	rec = httptest.NewRecorder()
	if WriteAction(rec, Decision{Action: ActionObserve}) {
		t.Fatal("observe must continue")
	}
	rec = httptest.NewRecorder()
	if !WriteAction(rec, Decision{Action: ActionChallenge}) || rec.Code != http.StatusUnauthorized {
		t.Fatalf("challenge %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	if !WriteAction(rec, Decision{Action: ActionRateLimit}) || rec.Code != http.StatusTooManyRequests {
		t.Fatalf("ratelimit %d", rec.Code)
	}
}

func TestNodeWrapDoesNotBlockDetectOnOffer(t *testing.T) {
	ingested := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/httpthreat/ingest" {
			ingested <- struct{}{}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n"}, srv.URL, "tok", "enc1", HashEmbed)
	h := node.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-ingested:
	case <-time.After(2 * time.Second):
		t.Fatal("offer must be async but happen")
	}
}

func TestNodeWrapSkipsHealthAndTrainer(t *testing.T) {
	ingested := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/httpthreat/ingest" {
			ingested <- struct{}{}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n"}, srv.URL, "", "enc1", HashEmbed)
	h := node.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health %d", rec.Code)
	}
	select {
	case <-ingested:
		t.Fatal("health must not ingest")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestNodePullKeepsAckWhenAckFails(t *testing.T) {
	prev := EmptyHead(1, DefaultTau)
	serv := EmptyHead(2, DefaultTau)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/httpthreat/serving" {
			_ = json.NewEncoder(w).Encode(ServingBundle{
				Pipeline: PipelineOn, Serving: &serv, Previous: &prev, Hash: serv.Hash(),
			})
			return
		}
		if r.URL.Path == "/api/httpthreat/ack" {
			http.Error(w, "no", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n1"}, srv.URL, "", "enc1", HashEmbed)
	node.Install(ServingBundle{Pipeline: PipelineOn, Serving: &serv, Previous: &prev, Hash: serv.Hash()}, prev.Hash())
	if err := node.Pull(); err == nil {
		t.Fatal("ack fail must surface")
	}
	dec, _ := node.Detect(Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.ServingHash != prev.Hash() {
		t.Fatalf("failed ack must keep previous, got %s", dec.ServingHash)
	}
}

func TestNodeAuthSendsMachineIdentity(t *testing.T) {
	got := make(chan *http.Request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r
		if r.URL.Path == "/api/httpthreat/serving" {
			_ = json.NewEncoder(w).Encode(ServingBundle{})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "ve-1"}, srv.URL, "tok", "enc1", HashEmbed)
	if err := node.Pull(); err != nil {
		t.Fatal(err)
	}
	req := <-got
	if req.Header.Get("Authorization") != "Bearer tok" || req.Header.Get("X-Machine-ID") != "ve-1" {
		t.Fatalf("auth headers %+v", req.Header)
	}
}

func TestNodeFromEnvRequiresTrainerAndTenant(t *testing.T) {
	t.Setenv("HTTPTHREAT_TRAINER_URL", "")
	t.Setenv("MACLAW_HUB_URL", "")
	t.Setenv("HTTPTHREAT_TENANT", "")
	t.Setenv("MACLAW_HUB_TENANT_ID", "")
	t.Setenv("MACLAW_TENANT_ID", "")
	if NodeFromEnv() != nil {
		t.Fatal("empty env must be nil")
	}
	t.Setenv("HTTPTHREAT_TRAINER_URL", "http://trainer.example")
	t.Setenv("HTTPTHREAT_TENANT", "tenant-a")
	t.Setenv("HTTPTHREAT_NODE", "n1")
	n := NodeFromEnv()
	if n == nil || n.ID.TenantID != "tenant-a" || n.ID.NodeID != "n1" {
		t.Fatalf("got %+v", n)
	}
}

func TestStartPullConfirmsServing(t *testing.T) {
	serv := EmptyHead(2, DefaultTau)
	pulled := make(chan struct{}, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/httpthreat/serving" {
			pulled <- struct{}{}
			_ = json.NewEncoder(w).Encode(ServingBundle{Pipeline: PipelineOff, Serving: &serv, Hash: serv.Hash()})
			return
		}
		if r.URL.Path == "/api/httpthreat/ack" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n1"}, srv.URL, "tok", "enc1", HashEmbed)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	node.StartPull(ctx, time.Hour)
	select {
	case <-pulled:
	case <-time.After(2 * time.Second):
		t.Fatal("pull")
	}
}

func TestSkipDetectPathKeepsAdminAndHealth(t *testing.T) {
	for _, p := range []string{"/healthz", "/admin", "/admin/x", "/api/v1/admin/auth/me", "/api/httpthreat/inspect", "/metrics"} {
		if !skipDetectPath(p) {
			t.Fatalf("must skip %s", p)
		}
	}
	if skipDetectPath("/app/page") {
		t.Fatal("user traffic must be scored when wrap is on")
	}
}

func TestStatusDoesNotDeadlockWithDetect(t *testing.T) {
	e := NewEngine("enc1", HashEmbed)
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 40; i++ {
			_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
		}
		close(done)
	}()
	for i := 0; i < 40; i++ {
		_ = e.Status(id)
		_, _ = e.Gate(id, "serving")
	}
	<-done
}

func TestRollbackDoesNotNeedACK(t *testing.T) {
	e := NewEngine("enc1", HashEmbed)
	id := NodeIdentity{TenantID: "t", NodeID: "n1"}
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/p", Query: "x=../etc/passwd"})
	if err := e.Train(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	first := e.Status(id).ServingHash
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	q := e.corpus.ReviewQueue("t")
	if len(q) > 0 {
		_ = e.Label(id, LabelRequest{SampleID: q[0].ID, GoldClass: ClassBenign, Role: RoleAnalyst})
	}
	if err := e.Train(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	e.SetTargets("t", []string{"n1"})
	if err := e.ACK(id, first); err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.Rollback(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if got := e.Status(id).ServingHash; got != first {
		t.Fatalf("rollback must swap without ACK, got %s want %s", got, first)
	}
}

func TestBundleOmitsUnverifiedServing(t *testing.T) {
	e := NewEngineAt(t.TempDir(), "enc1", HashEmbed)
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	h := EmptyHead(1, DefaultTau)
	e.mu.Lock()
	e.art("t").Serving = &h
	e.mu.Unlock()
	b, err := e.Bundle(id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Serving != nil {
		t.Fatal("unsigned serving must not be distributed")
	}
	SignHead(&h, e.restKey())
	e.mu.Lock()
	e.art("t").Serving = &h
	e.mu.Unlock()
	b, err = e.Bundle(id)
	if err != nil || b.Serving == nil || b.Hash != h.Hash() {
		t.Fatalf("signed serving must ship %+v %v", b, err)
	}
}

func TestNodePullDropsUnverifiedServing(t *testing.T) {
	h := EmptyHead(1, DefaultTau)
	key := []byte("0123456789abcdef0123456789abcdef")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ServingBundle{Pipeline: PipelineOn, Serving: &h, Hash: h.Hash()})
	}))
	t.Cleanup(srv.Close)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n1"}, srv.URL, "tok", "enc1", HashEmbed)
	node.VerifyKey = key
	if err := node.Pull(); err != nil {
		t.Fatal(err)
	}
	dec, _ := node.Detect(Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.Pipeline != PipelineOff || dec.ServingHash != "" {
		t.Fatalf("unverified serving must be off %+v", dec)
	}
}

func TestNodeInspectUsesJSONMirror(t *testing.T) {
	ingested := make(chan IngestRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/httpthreat/ingest" {
			var req IngestRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			ingested <- req
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n"}, srv.URL, "tok", "enc1", HashEmbed)
	body, _ := json.Marshal(Transaction{Method: "GET", Host: "evil.test", Path: "/p", Query: "x=../etc/passwd"})
	r := httptest.NewRequest(http.MethodPost, "/api/httpthreat/inspect", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	dec := node.Inspect(r)
	if dec.RuleSource != SourceSignature || dec.Action != ActionBlock {
		t.Fatalf("node inspect must score JSON mirror %+v", dec)
	}
	select {
	case got := <-ingested:
		if got.Path != "/p" || got.Host != "evil.test" {
			t.Fatalf("offer must be mirrored tx %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("offer")
	}
}

func TestInspectJSONTransaction(t *testing.T) {
	e := NewEngine("enc1", HashEmbed)
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	body, _ := json.Marshal(Transaction{Method: "GET", Host: "evil.test", Path: "/p", Query: "x=../etc/passwd"})
	r := httptest.NewRequest(http.MethodPost, "/api/httpthreat/inspect", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	dec, err := e.Inspect(id, r)
	if err != nil {
		t.Fatal(err)
	}
	if dec.RuleSource != SourceSignature || dec.Action != ActionBlock {
		t.Fatalf("inspect must score JSON mirror %+v", dec)
	}
	if dec.Preview == "" || strings.Contains(dec.Preview, "Cookie") {
		t.Fatalf("preview %+v", dec)
	}
}

func TestNodeSafetyValveDemotesLocally(t *testing.T) {
	head, err := TrainHead([]labeledEmb{{Emb: oneHot(ClassExploit), Label: ClassExploit}}, 1, DefaultTau, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n1"}, "", "", "enc1", func(string) ([]float32, error) {
		return oneHot(ClassExploit), nil
	})
	node.blockLimit = 0
	node.Install(ServingBundle{Pipeline: PipelineOn, Serving: &head, Hash: head.Hash()}, head.Hash())
	dec, _ := node.Detect(Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if !dec.Demoted || (dec.Action == ActionBlock && dec.HeadUsed) {
		t.Fatalf("node safety valve %+v", dec)
	}
	dec, _ = node.Detect(Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.Pipeline != PipelineShadow || dec.HeadUsed {
		t.Fatalf("node must stay shadow after valve %+v", dec)
	}
}

func TestNodeLocalShadowSurvivesPullOn(t *testing.T) {
	head, err := TrainHead([]labeledEmb{{Emb: oneHot(ClassExploit), Label: ClassExploit}}, 1, DefaultTau, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/httpthreat/serving" {
			_ = json.NewEncoder(w).Encode(ServingBundle{Pipeline: PipelineOn, Serving: &head, Hash: head.Hash()})
			return
		}
		if r.URL.Path == "/api/httpthreat/ack" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n1"}, srv.URL, "", "enc1", func(string) ([]float32, error) {
		return oneHot(ClassExploit), nil
	})
	node.blockLimit = 0
	node.Install(ServingBundle{Pipeline: PipelineOn, Serving: &head, Hash: head.Hash()}, head.Hash())
	if dec, _ := node.Detect(Transaction{Method: "GET", Host: "h", Path: "/ok"}); !dec.Demoted {
		t.Fatalf("valve %+v", dec)
	}
	if err := node.Pull(); err != nil {
		t.Fatal(err)
	}
	dec, _ := node.Detect(Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.Pipeline != PipelineShadow || dec.HeadUsed {
		t.Fatalf("pull of trainer on must keep local shadow %+v", dec)
	}
}

func TestParseVerifyKeyPrefersRawUnless32ByteHex(t *testing.T) {
	if got := parseVerifyKey(""); got != nil {
		t.Fatalf("empty %q", got)
	}
	if got := parseVerifyKey("deadbeef"); string(got) != "deadbeef" {
		t.Fatalf("short hex must stay raw %q", got)
	}
	hex32 := strings.Repeat("ab", 32)
	got := parseVerifyKey(hex32)
	if len(got) != 32 {
		t.Fatalf("64 hex chars must decode to 32 bytes, got %d", len(got))
	}
	if string(parseVerifyKey("not-hex-key")) != "not-hex-key" {
		t.Fatal("passphrase must stay raw")
	}
}

func TestNodePullDoesNotInstallBeforeAck(t *testing.T) {
	serv := EmptyHead(2, DefaultTau)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/httpthreat/serving" {
			_ = json.NewEncoder(w).Encode(ServingBundle{Pipeline: PipelineOn, Serving: &serv, Hash: serv.Hash()})
			return
		}
		if r.URL.Path == "/api/httpthreat/ack" {
			http.Error(w, "no", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n1"}, srv.URL, "", "enc1", HashEmbed)
	if err := node.Pull(); err == nil {
		t.Fatal("ack fail must surface")
	}
	dec, _ := node.Detect(Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.ServingHash != "" || dec.Pipeline != PipelineOff {
		t.Fatalf("unacked first serving must stay off %+v", dec)
	}
}

func TestIngestDemotedForcesTrainerShadow(t *testing.T) {
	head, err := TrainHead([]labeledEmb{{Emb: oneHot(ClassExploit), Label: ClassExploit}}, 1, DefaultTau, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassExploit), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n1"}
	e.mu.Lock()
	e.art("t").Serving = &head
	e.art("t").Pipeline = PipelineOn
	e.mu.Unlock()
	tx := Transaction{Method: "GET", Host: "h", Path: "/ok"}
	err = e.IngestSample(id, IngestRequest{
		Transaction: tx,
		Decision: &Decision{
			SampleID: "forged", Demoted: true, HeadUsed: true,
			Action: ActionBlock, RuleAction: ActionAllow,
			RuleSource: SourceSignature, RuleClass: ClassExploit,
		},
		Embedding: oneHot(ClassExploit), EncoderID: "enc1",
	})
	if err != nil {
		t.Fatal(err)
	}
	st := e.Status(id)
	if st.Pipeline != PipelineShadow || !st.SafetyValve {
		t.Fatalf("verified demoted ingest must force trainer shadow %+v", st)
	}
	s, ok := e.corpus.Get(SampleID("t", "enc1", BuildPreview(tx)))
	if !ok || s.GoldSource == GoldAuto || s.RuleSource == SourceSignature {
		t.Fatalf("ingest must not trust client gold/source %+v ok=%v", s, ok)
	}
}

func TestIngestIgnoresFakeDemoted(t *testing.T) {
	e := NewEngine("enc1", HashEmbed)
	id := NodeIdentity{TenantID: "t", NodeID: "n1"}
	h := EmptyHead(1, DefaultTau)
	e.mu.Lock()
	e.art("t").Serving = &h
	e.art("t").Pipeline = PipelineOn
	e.mu.Unlock()
	if err := e.IngestSample(id, IngestRequest{
		Transaction: Transaction{Method: "GET", Host: "h", Path: "/ok"},
		Decision:    &Decision{SampleID: "s1", Demoted: true, HeadUsed: true, Action: ActionBlock, RuleAction: ActionAllow, RuleSource: SourceSignature, RuleClass: ClassExploit},
	}); err != nil {
		t.Fatal(err)
	}
	st := e.Status(id)
	if st.Pipeline != PipelineOn || st.SafetyValve {
		t.Fatalf("unverified demoted must not shadow %+v", st)
	}
}

func TestIngestIgnoresCraftedEmbedding(t *testing.T) {
	head, err := TrainHead([]labeledEmb{
		{Emb: oneHot(ClassBenign), Label: ClassBenign},
		{Emb: oneHot(ClassExploit), Label: ClassExploit},
	}, 1, DefaultTau, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n1"}
	e.mu.Lock()
	e.art("t").Serving = &head
	e.art("t").Pipeline = PipelineOn
	e.mu.Unlock()
	if err := e.IngestSample(id, IngestRequest{
		Transaction: Transaction{Method: "GET", Host: "h", Path: "/ok"},
		Decision:    &Decision{SampleID: "s1", Demoted: true, HeadUsed: true, Action: ActionBlock, RuleAction: ActionAllow},
		Embedding:   oneHot(ClassExploit), EncoderID: "enc1",
	}); err != nil {
		t.Fatal(err)
	}
	st := e.Status(id)
	if st.Pipeline != PipelineOn || st.SafetyValve {
		t.Fatalf("crafted embedding must not force shadow %+v", st)
	}
}

func TestUpsertDoesNotEvictOtherTenant(t *testing.T) {
	c := NewCorpus("enc1", 2)
	now := time.Now().UTC()
	c.Upsert(now, Sample{ID: "a1", TenantID: "a", Preview: "1"})
	c.Upsert(now, Sample{ID: "a2", TenantID: "a", Preview: "2"})
	c.Upsert(now, Sample{ID: "b1", TenantID: "b", Preview: "3"})
	if _, ok := c.Get("a1"); !ok {
		t.Fatal("other tenant must not be evicted by global cap")
	}
	if _, ok := c.Get("b1"); !ok {
		t.Fatal("new tenant sample missing")
	}
	c.TrimTenant("a", 2)
	if _, ok := c.Get("b1"); !ok {
		t.Fatal("trim tenant a must keep b")
	}
	now = now.Add(time.Second)
	c.Upsert(now, Sample{ID: "a1", TenantID: "b", HeadClass: ClassExploit, Preview: "x"})
	got, _ := c.Get("a1")
	if got.TenantID != "a" || got.HeadClass == ClassExploit {
		t.Fatalf("foreign tenant must not mutate sample %+v", got)
	}
}

func TestNodePullEmptyKeepsLocalServing(t *testing.T) {
	serv := EmptyHead(2, DefaultTau)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ServingBundle{Pipeline: PipelineOff})
	}))
	t.Cleanup(srv.Close)
	node := NewNode(NodeIdentity{TenantID: "t", NodeID: "n1"}, srv.URL, "", "enc1", HashEmbed)
	node.Install(ServingBundle{Pipeline: PipelineOn, Serving: &serv, Hash: serv.Hash()}, serv.Hash())
	if err := node.Pull(); err != nil {
		t.Fatal(err)
	}
	dec, _ := node.Detect(Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if dec.ServingHash != serv.Hash() {
		t.Fatalf("empty pull must keep local serving, got %s", dec.ServingHash)
	}
}
