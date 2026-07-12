package corelib

import "testing"

func TestNLSkillEntryMatchesNameHubSkillIDAndDisplayName(t *testing.T) {
	// Regression: PDF翻译工具 runs "paper_pdf_translator" while the installed
	// hub skill is registered as display name "Paper PDF Translator".
	skill := NLSkillEntry{
		Name:       "Paper PDF Translator",
		HubSkillID: "paper_pdf_translator",
		SkillDir:   `C:\Users\me\.maclaw\data\skills\Paper PDF Translator`,
		Status:     "active",
	}
	for _, query := range []string{
		"paper_pdf_translator",
		"Paper PDF Translator",
		"paper_pdf_translator@1.0.0",
		"PAPER_PDF_TRANSLATOR",
	} {
		if !skill.MatchesName(query) {
			t.Fatalf("MatchesName(%q) = false, want true for hub skill %#v", query, skill)
		}
	}
	if skill.MatchesName("other_skill") || skill.MatchesName("") {
		t.Fatal("MatchesName should reject unrelated/empty queries")
	}

	// Stable IDs only (no loose display-name match).
	if !skill.MatchesQualifiedID("paper_pdf_translator") || !skill.MatchesQualifiedID("paper_pdf_translator@1.0.0") {
		t.Fatal("MatchesQualifiedID should accept HubSkillID and @version refs")
	}
	if skill.MatchesQualifiedID("Paper PDF Translator") {
		t.Fatal("MatchesQualifiedID must not accept loose display names")
	}

	// Display-name / dir-name loose identity without HubSkillID.
	local := NLSkillEntry{Name: "Sheet Analysis", DirName: "sheet-analysis"}
	if !local.MatchesName("sheet_analysis") || !local.MatchesName("sheet-analysis") {
		t.Fatalf("loose Name/DirName identity failed: %#v", local)
	}
	if local.MatchesQualifiedID("sheet_analysis") {
		t.Fatal("MatchesQualifiedID should not match DirName-only identity")
	}

	// SkillID bare match (no publisher. prefix required).
	withID := NLSkillEntry{Name: "Demo", SkillID: "lovstudio.demo"}
	if !withID.MatchesName("lovstudio.demo") || !withID.MatchesQualifiedID("lovstudio.demo") {
		t.Fatal("SkillID should match without requiring extra query shape checks")
	}

	// Publisher-qualified stable ID.
	pub := NLSkillEntry{Name: "Demo", Publisher: "acme"}
	if !pub.MatchesQualifiedID("acme:Demo") || !pub.MatchesName("acme:Demo") {
		t.Fatal("publisher:name should match as stable ID")
	}
	if pub.MatchesQualifiedID("Demo") {
		t.Fatal("bare display name must not match MatchesQualifiedID even with publisher set")
	}
}

func TestNormalizeSkillMatchQuery(t *testing.T) {
	if got := NormalizeSkillMatchQuery("  paper_pdf_translator@1.0.0  "); got != "paper_pdf_translator" {
		t.Fatalf("NormalizeSkillMatchQuery = %q", got)
	}
	if got := NormalizeSkillMatchQuery("@only"); got != "@only" {
		// leading @ is not a version pin separator at index 0
		t.Fatalf("leading @ should not strip, got %q", got)
	}
}

func TestNormalizeSkillIdentityKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Paper PDF Translator", "paper_pdf_translator"},
		{"paper_pdf_translator", "paper_pdf_translator"},
		{"sheet-analysis", "sheet_analysis"},
		{"  Foo   Bar  ", "foo_bar"},
	}
	for _, tc := range cases {
		if got := NormalizeSkillIdentityKey(tc.in); got != tc.want {
			t.Fatalf("NormalizeSkillIdentityKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNLSkillEntrySkillIdentityKeysAndRuntimeRef(t *testing.T) {
	skill := NLSkillEntry{
		Name:       "Paper PDF Translator",
		HubSkillID: "paper_pdf_translator",
		SkillDir:   `C:\Users\me\.maclaw\data\skills\Paper PDF Translator`,
	}
	keys := skill.SkillIdentityKeys()
	if !containsSkillKey(keys, "paper_pdf_translator") {
		t.Fatalf("SkillIdentityKeys missing hub id, got %v", keys)
	}
	// Lowercased display name and underscore form both index this skill.
	if !containsSkillKey(keys, "paper pdf translator") {
		t.Fatalf("SkillIdentityKeys missing lowercased display name, got %v", keys)
	}
	if !containsSkillKey(keys, "paper_pdf_translator") {
		t.Fatalf("SkillIdentityKeys incomplete: %v", keys)
	}
	if ref := skill.PreferredRuntimeSkillRef(); ref != "paper_pdf_translator" {
		t.Fatalf("PreferredRuntimeSkillRef = %q, want hub id", ref)
	}
	// CollectSkillIdentityKeys is idempotent on its own output.
	again := CollectSkillIdentityKeys(keys...)
	if len(again) != len(keys) {
		t.Fatalf("CollectSkillIdentityKeys not stable: first=%v second=%v", keys, again)
	}
}

func containsSkillKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

func TestMaclawLLMProviderCodexSubscriptionOAuthToken(t *testing.T) {
	tests := []struct {
		name     string
		provider MaclawLLMProvider
		want     string
	}{
		{
			name: "chatgpt codex oauth",
			provider: MaclawLLMProvider{
				Name:             "OpenAI",
				URL:              "https://chatgpt.com/backend-api/codex",
				OAuthAccessToken: "eyJhbGciOiJub25lIn0.payload.sig",
			},
			want: "eyJhbGciOiJub25lIn0.payload.sig",
		},
		{
			name: "platform endpoint",
			provider: MaclawLLMProvider{
				Name:             "OpenAI",
				URL:              "https://api.openai.com/v1",
				OAuthAccessToken: "eyJhbGciOiJub25lIn0.payload.sig",
			},
		},
		{
			name: "other provider",
			provider: MaclawLLMProvider{
				Name:             "Custom",
				URL:              "https://chatgpt.com/backend-api/codex",
				OAuthAccessToken: "eyJhbGciOiJub25lIn0.payload.sig",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.provider.CodexSubscriptionOAuthToken(); got != tt.want {
				t.Fatalf("CodexSubscriptionOAuthToken() = %q, want %q", got, tt.want)
			}
		})
	}

	provider := MaclawLLMProvider{Name: "OpenAI", URL: "https://chatgpt.com/backend-api/codex"}
	if !provider.IsCodexSubscriptionOAuthProvider() {
		t.Fatal("ChatGPT Codex provider was not recognized")
	}
}
