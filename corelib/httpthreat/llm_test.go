package httpthreat

import (
	"testing"
)

func TestParseLLMAdviceRejectsUnknownClass(t *testing.T) {
	if !ParseLLMAdvice(`{"class":"worm"}`).Abstain {
		t.Fatal("unknown class must abstain")
	}
	if got := ParseLLMAdvice(`{"class":"exploit"}`); got.Abstain || got.Class != ClassExploit {
		t.Fatalf("%+v", got)
	}
}

func TestArbitrateDoesNotOverwriteHumanAndKeepsTripleHuman(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	dec, _ := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/ok"})
	if err := e.Label(id, LabelRequest{SampleID: dec.SampleID, GoldClass: ClassBenign, Role: RoleAnalyst}); err != nil {
		t.Fatal(err)
	}
	e.SetArbitrator(func(string, string, string, []string) (LLMAdvice, error) {
		return LLMAdvice{Class: ClassExploit}, nil
	})
	if _, err := e.Arbitrate(id, dec.SampleID, RoleAnalyst); err == nil {
		t.Fatal("human gold must lock llm")
	}

	dec2, _ := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/", Query: "select=1"})
	e.SetArbitrator(func(string, string, string, []string) (LLMAdvice, error) {
		return LLMAdvice{Class: ClassMalware}, nil
	})
	adv, err := e.Arbitrate(id, dec2.SampleID, RoleAnalyst)
	if err != nil {
		t.Fatal(err)
	}
	if !adv.Abstain {
		t.Fatalf("triple disagreement must stay human %+v", adv)
	}
	got, _ := e.corpus.Get(dec2.SampleID)
	if got.GoldSource == GoldLLM || !got.NeedHuman {
		t.Fatalf("must not silent-write llm gold %+v", got)
	}
	if err := e.PromoteLLM(id, dec2.SampleID, RoleAnalyst); err != nil {
		t.Fatal(err)
	}
	got, _ = e.corpus.Get(dec2.SampleID)
	if got.GoldSource != GoldHuman || got.GoldClass != ClassMalware {
		t.Fatalf("promote %+v", got)
	}
}

func TestArbitrateAgreementWritesLLMGold(t *testing.T) {
	e := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	id := NodeIdentity{TenantID: "t", NodeID: "n"}
	dec, _ := e.Detect(id, Transaction{Method: "GET", Host: "h", Path: "/", Query: "select=1"})
	e.SetArbitrator(func(_, rule, _ string, _ []string) (LLMAdvice, error) {
		return LLMAdvice{Class: rule}, nil
	})
	adv, err := e.Arbitrate(id, dec.SampleID, RoleAdmin)
	if err != nil || adv.Abstain || adv.Class != ClassExploit {
		t.Fatalf("%+v %v", adv, err)
	}
	got, _ := e.corpus.Get(dec.SampleID)
	if got.GoldSource != GoldLLM {
		t.Fatalf("want llm gold %+v", got)
	}
	rep := Evaluate([]Sample{got}, "2000-01-01T00:00:00Z", func(Sample) string { return ClassExploit }, true, true, nil)
	if rep.Recent.Reviews != 0 {
		t.Fatalf("llm gold must not enter accuracy %+v", rep.Recent)
	}
}
