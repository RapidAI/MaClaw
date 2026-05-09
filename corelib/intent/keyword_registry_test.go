package intent

import "testing"

func TestNewKeywordRegistry_DiagnosticIndexes(t *testing.T) {
	r := NewKeywordRegistry()
	if len(r.entries) == 0 || len(r.byLabel) == 0 || len(r.strongIndex) == 0 || len(r.weakByLabel) == 0 {
		t.Fatalf("registry indexes were not populated: entries=%d labels=%d strong=%d weak=%d",
			len(r.entries), len(r.byLabel), len(r.strongIndex), len(r.weakByLabel))
	}
}

func TestNewKeywordRegistry_StrongAndWeakEvidence(t *testing.T) {
	r := NewKeywordRegistry()

	if label, ok := r.strongIndex["ssh into"]; !ok || label != LabelSSH {
		t.Fatalf("strongIndex[ssh into]=%s ok=%v, want ssh true", label, ok)
	}
	foundBareSSHWeak := false
	for _, kw := range r.weakByLabel[LabelSSH] {
		if kw == "ssh" {
			foundBareSSHWeak = true
			break
		}
	}
	if !foundBareSSHWeak {
		t.Fatal("bare ssh should remain weak diagnostic evidence")
	}
	if len(r.weakByLabel[LabelBrowser]) == 0 {
		t.Fatal("browser should have weak diagnostic evidence")
	}
}

func TestKeywordRegistry_Deduplication(t *testing.T) {
	r := newKeywordRegistryFromEntries([]KeywordEntry{
		{Keyword: "write code", Label: LabelCoding, Strength: Strong},
		{Keyword: "write code", Label: LabelCoding, Strength: Strong},
	})
	if len(r.entries) != 1 {
		t.Fatalf("deduplicated entries=%d, want 1", len(r.entries))
	}
}

func TestKeywordRegistry_Match(t *testing.T) {
	r := NewKeywordRegistry()
	matches := r.Match("Please SSH into the server and check server logs")
	if len(matches) == 0 {
		t.Fatal("expected diagnostic matches")
	}

	foundSSHInto := false
	for _, m := range matches {
		if m.Entry.Keyword == "ssh into" && m.Entry.Label == LabelSSH && m.Position >= 0 {
			foundSSHInto = true
			break
		}
	}
	if !foundSSHInto {
		t.Fatalf("expected ssh into match, got %+v", matches)
	}
}

func TestKeywordRegistry_MatchEmptyText(t *testing.T) {
	r := NewKeywordRegistry()
	if matches := r.Match(""); len(matches) != 0 {
		t.Fatalf("empty text matches=%d, want 0", len(matches))
	}
}
