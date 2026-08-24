package httpthreat

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPersistSplitsCorpusAndArtifacts(t *testing.T) {
	dir := t.TempDir()
	e := NewEngineAt(dir, "enc1", func(preview string) ([]float32, error) {
		if strings.Contains(strings.ToLower(preview), "passwd") {
			return oneHot(ClassExploit), nil
		}
		return oneHot(ClassBenign), nil
	})
	id := NodeIdentity{TenantID: "tenant-a", NodeID: "n1"}
	_, err := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/p", Query: "x=../etc/passwd"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	q := e.corpus.ReviewQueue("tenant-a")
	if len(q) == 0 {
		t.Fatal("queue")
	}
	if err := e.Label(id, LabelRequest{SampleID: q[0].ID, GoldClass: ClassBenign, Role: RoleAnalyst}); err != nil {
		t.Fatal(err)
	}
	if err := e.Train(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	runs, err := e.Runs(id)
	if err != nil || len(runs) != 1 || len(runs[0].SampleIDs) == 0 || len(runs[0].Golds) == 0 {
		t.Fatalf("train run %+v %v", runs, err)
	}
	for _, g := range runs[0].Golds {
		if strings.Contains(g.GoldClass, "METHOD") {
			t.Fatal("run must not store preview")
		}
	}
	if err := e.Adopt(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPipeline(id, PipelineShadow, "", "", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.SetIntel(id, RoleAdmin, "evil.test", ClassMalware); err != nil {
		t.Fatal(err)
	}
	if err := e.UpdateSites(id, RoleAdmin, []string{"site-a"}, nil); err != nil {
		t.Fatal(err)
	}

	artRaw, err := os.ReadFile(filepath.Join(dir, "artifacts", "tenant-a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(artRaw), "METHOD GET") {
		t.Fatal("artifacts must not copy previews")
	}
	corpRaw, err := os.ReadFile(filepath.Join(dir, "corpus", "tenant-a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(corpRaw), "METHOD GET") {
		t.Fatal("corpus preview must be encrypted at rest")
	}
	if !strings.Contains(string(corpRaw), RestPrefix) {
		t.Fatalf("corpus should use %s: %s", RestPrefix, corpRaw)
	}

	reloaded := NewEngineAt(dir, "enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	st := reloaded.Status(id)
	if st.Pipeline != PipelineShadow || !st.ServingReady {
		t.Fatalf("reload status %+v", st)
	}
	foundPreview := false
	for _, s := range reloaded.corpus.ListTenant("tenant-a") {
		if strings.Contains(s.Preview, "METHOD GET") {
			foundPreview = true
		}
	}
	if !foundPreview {
		t.Fatal("reload must decrypt preview")
	}
	runs2, _ := reloaded.Runs(id)
	if len(runs2) != 1 {
		t.Fatalf("reload runs %d", len(runs2))
	}
	view, err := reloaded.RuleHits(id)
	if err != nil || len(view.Intel) != 1 || view.Intel[0].Host != "evil.test" || len(view.Sites) != 1 {
		t.Fatalf("reload intel/sites %+v %v", view, err)
	}
	dec, err := reloaded.Detect(id, Transaction{Method: "GET", Host: "evil.test", Path: "/ok"})
	if err != nil || dec.RuleSource != SourceIntel {
		t.Fatalf("reloaded intel detect %+v %v", dec, err)
	}
}

func TestIngestDropsOverRate(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	e.ingestN["t"] = 2000
	e.ingestWin["t"] = e.now()
	if err := e.Ingest(id, Transaction{Method: "GET", Host: "h", Path: "/ok"}); err != nil {
		t.Fatal(err)
	}
	if n := len(e.corpus.ListTenant("t")); n != 0 {
		t.Fatalf("over-rate must drop sample, got %d", n)
	}
}

func TestFlushDoesNotClobberNewerCorpus(t *testing.T) {
	dir := t.TempDir()
	e := NewEngineAt(dir, "enc1", HashEmbed)
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	dec, err := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		e.flushTenant("t")
	}()
	go func() {
		defer wg.Done()
		if err := e.Label(id, LabelRequest{SampleID: dec.SampleID, GoldClass: ClassBenign, Role: RoleAnalyst}); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	reloaded := NewEngineAt(dir, "enc1", HashEmbed)
	s, ok := reloaded.corpus.Get(dec.SampleID)
	if !ok || s.GoldClass != ClassBenign || s.GoldSource != GoldHuman {
		t.Fatalf("label must survive concurrent flush %+v ok=%v", s, ok)
	}
}

func TestScheduleFlushThenLabelKeepsGold(t *testing.T) {
	dir := t.TempDir()
	e := NewEngineAt(dir, "enc1", HashEmbed)
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	dec, err := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Label(id, LabelRequest{SampleID: dec.SampleID, GoldClass: ClassScan, Role: RoleAnalyst}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	reloaded := NewEngineAt(dir, "enc1", HashEmbed)
	s, ok := reloaded.corpus.Get(dec.SampleID)
	if !ok || s.GoldClass != ClassScan {
		t.Fatalf("debounced flush must not drop label %+v ok=%v", s, ok)
	}
}

func TestSetTargetsPersists(t *testing.T) {
	dir := t.TempDir()
	e := NewEngineAt(dir, "enc1", HashEmbed)
	e.SetTargets("tenant-a", []string{"n1", "n2"})
	reloaded := NewEngineAt(dir, "enc1", HashEmbed)
	st := reloaded.Status(NodeIdentity{TenantID: "tenant-a", NodeID: "n1"})
	if len(st.TargetNodes) != 2 || st.TargetNodes[0] != "n1" || st.TargetNodes[1] != "n2" {
		t.Fatalf("reload targets %+v", st.TargetNodes)
	}
}

func TestSafetyValvePersistsPipeline(t *testing.T) {
	dir := t.TempDir()
	head, err := TrainHead([]labeledEmb{{Emb: oneHot(ClassExploit), Label: ClassExploit}}, 1, DefaultTau, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineAt(dir, "enc1", func(string) ([]float32, error) { return oneHot(ClassExploit), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n1"}
	SignHead(&head, e.restKey())
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
	reloaded := NewEngineAt(dir, "enc1", HashEmbed)
	st := reloaded.Status(id)
	if st.Pipeline != PipelineShadow || !st.SafetyValve {
		t.Fatalf("valve must be durable %+v", st)
	}
}
