package corelib

import "testing"

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
