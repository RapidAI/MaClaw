package llm

import (
	"encoding/json"
	"testing"
)

func TestUsageUnmarshalJSONFoldsSeparateReasoningTokens(t *testing.T) {
	var usage Usage
	if err := json.Unmarshal([]byte(`{
		"prompt_tokens": 100,
		"completion_tokens": 50,
		"total_tokens": 550,
		"completion_tokens_details": {"reasoning_tokens": 400}
	}`), &usage); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if usage.PromptTokens != 100 || usage.CompletionTokens != 450 || usage.TotalTokens != 550 {
		t.Fatalf("usage = %+v, want prompt 100 completion 450 total 550", usage)
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 450 {
		t.Fatalf("normalized usage = %+v, want input 100 output 450", usage)
	}
}

func TestOpenAISDKChunkUsageFoldsSeparateReasoningTokens(t *testing.T) {
	usage := openAISDKChunkUsage(`{"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":550,"completion_tokens_details":{"reasoning_tokens":400}}}`)
	if !llmUsageHasCounts(usage) {
		t.Fatal("expected parsed usage")
	}
	if usage.PromptTokens != 100 || usage.CompletionTokens != 450 || usage.TotalTokens != 550 {
		t.Fatalf("sdk chunk usage = %+v, want prompt 100 completion 450 total 550", usage)
	}
}

func TestLLMUsageHasCountsKeepsExplicitZeroReport(t *testing.T) {
	usage := openAISDKChunkUsage(`{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`)
	if !llmUsageHasCounts(usage) {
		t.Fatalf("explicit zero usage = %+v, want kept as reported", usage)
	}
	if usage.PromptTokens != 0 || usage.CompletionTokens != 0 || !usage.InputReported || !usage.OutputReported {
		t.Fatalf("explicit zero usage = %+v, want reported zeros", usage)
	}
}

func TestUsageFromPositiveCountsTreatsTotalOnlyAsReportedZeroOutput(t *testing.T) {
	usage := usageFromPositiveCounts(0, 0, 18)
	if !llmUsageHasCounts(usage) {
		t.Fatal("expected total-only typed usage")
	}
	if usage.PromptTokens != 18 || usage.CompletionTokens != 0 || !usage.InputReported || !usage.OutputReported {
		t.Fatalf("typed total-only usage = %+v, want reported input 18 and reported zero output", usage)
	}
}

func TestUsageUnmarshalJSONMarksTotalOnlyAsReportedZeroOutput(t *testing.T) {
	var usage Usage
	if err := json.Unmarshal([]byte(`{"total_tokens":18}`), &usage); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if usage.PromptTokens != 18 || usage.CompletionTokens != 0 || !usage.InputReported || !usage.OutputReported {
		t.Fatalf("usage = %+v, want reported input 18 and reported zero output", usage)
	}
}

func TestUsageUnmarshalJSONAcceptsStringTokenCounts(t *testing.T) {
	var usage Usage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":"12.0","completion_tokens":"8","total_tokens":"20"}`), &usage); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if usage.PromptTokens != 12 || usage.CompletionTokens != 8 || usage.TotalTokens != 20 {
		t.Fatalf("usage = %+v, want 12/8/20", usage)
	}
}
