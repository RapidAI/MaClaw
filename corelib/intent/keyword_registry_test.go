package intent

import (
	"testing"
)

func TestNewKeywordRegistry_NotEmpty(t *testing.T) {
	r := NewKeywordRegistry()
	if len(r.entries) == 0 {
		t.Fatal("expected non-empty entries")
	}
	if len(r.byLabel) == 0 {
		t.Fatal("expected non-empty byLabel")
	}
	if len(r.strongIndex) == 0 {
		t.Fatal("expected non-empty strongIndex")
	}
	if len(r.weakByLabel) == 0 {
		t.Fatal("expected non-empty weakByLabel")
	}
}

func TestNewKeywordRegistry_ConflictResolution(t *testing.T) {
	r := NewKeywordRegistry()

	// "ssh" appears in both LabelSSH and potentially other lists.
	// SSH should win due to highest priority.
	if label, ok := r.strongIndex["ssh"]; !ok || label != LabelSSH {
		t.Errorf("expected strongIndex[ssh] = LabelSSH, got %v (ok=%v)", label, ok)
	}

	// "浏览器" should map to LabelBrowser (strong).
	if label, ok := r.strongIndex["浏览器"]; !ok || label != LabelBrowser {
		t.Errorf("expected strongIndex[浏览器] = LabelBrowser, got %v (ok=%v)", label, ok)
	}

	// "写代码" should map to LabelCoding (strong).
	if label, ok := r.strongIndex["写代码"]; !ok || label != LabelCoding {
		t.Errorf("expected strongIndex[写代码] = LabelCoding, got %v (ok=%v)", label, ok)
	}
}

func TestNewKeywordRegistry_Deduplication(t *testing.T) {
	r := NewKeywordRegistry()

	// Count how many times "写代码" appears in entries — should be deduplicated.
	count := 0
	for _, e := range r.entries {
		if e.Keyword == "写代码" && e.Label == LabelCoding && e.Strength == Strong {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 写代码/LabelCoding/Strong to appear once, got %d", count)
	}
}

func TestKeywordRegistry_Match_Basic(t *testing.T) {
	r := NewKeywordRegistry()

	matches := r.Match("帮我连接服务器查看日志")
	if len(matches) == 0 {
		t.Fatal("expected matches for SSH keywords")
	}

	// Should find "服务器" and "日志" at minimum.
	foundServer := false
	foundLog := false
	for _, m := range matches {
		if m.Entry.Keyword == "服务器" {
			foundServer = true
		}
		if m.Entry.Keyword == "日志" || m.Entry.Keyword == "查看日志" {
			foundLog = true
		}
	}
	if !foundServer {
		t.Error("expected to find 服务器 match")
	}
	if !foundLog {
		t.Error("expected to find 日志 match")
	}
}

func TestKeywordRegistry_Match_CaseInsensitive(t *testing.T) {
	r := NewKeywordRegistry()

	matches := r.Match("SSH into the server")
	found := false
	for _, m := range matches {
		if m.Entry.Keyword == "ssh" && m.Entry.Label == LabelSSH {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected case-insensitive match for SSH")
	}
}

func TestKeywordRegistry_Match_EmptyText(t *testing.T) {
	r := NewKeywordRegistry()
	matches := r.Match("")
	if len(matches) != 0 {
		t.Errorf("expected no matches for empty text, got %d", len(matches))
	}
}

func TestKeywordRegistry_Match_Position(t *testing.T) {
	r := NewKeywordRegistry()

	matches := r.Match("请帮我搜索论文")
	found := false
	for _, m := range matches {
		if m.Entry.Keyword == "搜索论文" {
			found = true
			if m.Position < 0 {
				t.Error("expected non-negative position")
			}
			break
		}
	}
	if !found {
		t.Error("expected to find 搜索论文 match")
	}
}

func TestKeywordRegistry_ByLabel(t *testing.T) {
	r := NewKeywordRegistry()

	// LabelSSH should have many entries.
	sshEntries := r.byLabel[LabelSSH]
	if len(sshEntries) < 10 {
		t.Errorf("expected at least 10 SSH entries, got %d", len(sshEntries))
	}

	// LabelBrowser should have both strong and weak entries.
	browserEntries := r.byLabel[LabelBrowser]
	hasStrong := false
	hasWeak := false
	for _, e := range browserEntries {
		if e.Strength == Strong {
			hasStrong = true
		}
		if e.Strength == Weak {
			hasWeak = true
		}
	}
	if !hasStrong || !hasWeak {
		t.Error("expected LabelBrowser to have both strong and weak entries")
	}
}

func TestKeywordRegistry_WeakByLabel(t *testing.T) {
	r := NewKeywordRegistry()

	// LabelBrowser should have weak keywords.
	weakBrowser := r.weakByLabel[LabelBrowser]
	if len(weakBrowser) == 0 {
		t.Error("expected weak browser keywords")
	}

	// LabelContinuation should have weak keywords.
	weakCont := r.weakByLabel[LabelContinuation]
	if len(weakCont) == 0 {
		t.Error("expected weak continuation keywords")
	}
}
