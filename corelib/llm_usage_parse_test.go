package corelib

import (
	"encoding/json"
	"testing"
)

func TestParseLLMUsageFieldsFoldsSeparateReasoningTokens(t *testing.T) {
	fields := ParseLLMUsageFields(map[string]interface{}{
		"prompt_tokens":     float64(100),
		"completion_tokens": float64(50),
		"total_tokens":      float64(550),
		"completion_tokens_details": map[string]interface{}{
			"reasoning_tokens": float64(400),
		},
	})
	if fields.Input != 100 || fields.Output != 450 || fields.Total != 550 {
		t.Fatalf("separate reasoning = %+v, want input 100 output 450 total 550", fields)
	}
}

func TestParseLLMUsageFieldsKeepsReasoningAlreadyInsideCompletion(t *testing.T) {
	fields := ParseLLMUsageFields(map[string]interface{}{
		"prompt_tokens":     float64(100),
		"completion_tokens": float64(500),
		"total_tokens":      float64(600),
		"completion_tokens_details": map[string]interface{}{
			"reasoning_tokens": float64(400),
		},
	})
	if fields.Input != 100 || fields.Output != 500 || fields.Total != 600 {
		t.Fatalf("included reasoning = %+v, want input 100 output 500 total 600", fields)
	}
}

func TestParseLLMUsageFieldsAcceptsStringNumbersAndInfersMissingLeg(t *testing.T) {
	fields := ParseLLMUsageFields(map[string]interface{}{
		"prompt_tokens":     "12.0",
		"completion_tokens": "8",
		"total_tokens":      "20",
	})
	if fields.Input != 12 || fields.Output != 8 || fields.Total != 20 {
		t.Fatalf("string usage = %+v, want 12/8/20", fields)
	}

	inferred := ParseLLMUsageFields(map[string]interface{}{
		"completion_tokens": float64(8),
		"total_tokens":      float64(20),
	})
	if inferred.Input != 12 || inferred.Output != 8 {
		t.Fatalf("inferred input = %+v, want 12/8", inferred)
	}
}

func TestResponsesToOpenAIUsageFoldsSeparateReasoningTokens(t *testing.T) {
	usage := responsesToOpenAIUsage(map[string]interface{}{
		"input_tokens":          float64(100),
		"output_tokens":         float64(50),
		"total_tokens":          float64(550),
		"output_tokens_details": map[string]interface{}{"reasoning_tokens": float64(400)},
	})
	if usage["prompt_tokens"] != float64(100) || usage["completion_tokens"] != float64(450) || usage["total_tokens"] != float64(550) {
		t.Fatalf("responses usage = %#v, want 100/450/550", usage)
	}
}

func TestParseLLMResponseUsageMatchesHubCenterReasoningTotal(t *testing.T) {
	stat := ParseLLMResponseUsage([]byte(`{"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":550,"completion_tokens_details":{"reasoning_tokens":400}}}`))
	if stat.InputTokens != 100 || stat.OutputTokens != 450 || stat.TotalTokens != 550 {
		t.Fatalf("response usage = %+v, want 100/450/550", stat)
	}
	data, _ := json.Marshal(stat)
	if len(data) == 0 {
		t.Fatal("expected marshaled usage")
	}
}
