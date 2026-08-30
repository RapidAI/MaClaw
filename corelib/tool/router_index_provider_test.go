package tool

import (
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

type failingSkillIndexProvider struct {
	err   error
	calls int
}

func (p *failingSkillIndexProvider) Rebuild([]bm25.Doc) error {
	p.calls++
	return p.err
}

func (*failingSkillIndexProvider) Score(string) map[string]float64 { return nil }

func TestRouter_RefreshSkillIndexCheckedPropagatesProviderFailure(t *testing.T) {
	router := NewRouter(nil)
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{{Name: "demo", Description: "demo"}}})
	provider := &failingSkillIndexProvider{err: fmt.Errorf("injected index provider failure")}
	router.SetSkillIndexProvider(provider)
	if err := router.RefreshSkillIndexChecked(); err == nil || err.Error() != "injected index provider failure" {
		t.Fatalf("RefreshSkillIndexChecked() error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
}
