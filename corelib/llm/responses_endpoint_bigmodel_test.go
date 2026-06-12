package llm

import "testing"

func TestResponsesEndpointNormalizesVersionedV4BaseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"glm v4", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/responses"},
		{"glm coding v4", "https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn/api/coding/paas/v4/responses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildResponsesEndpoint(tt.url); got != tt.want {
				t.Fatalf("endpoint = %q, want %q", got, tt.want)
			}
		})
	}
}
