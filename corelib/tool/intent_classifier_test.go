package tool

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

const intentClassifierWarmupTimeout = 90 * time.Second

func TestIntentClassifier_NoKeywordIntentWithNoopEmbedder(t *testing.T) {
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()

	cases := []string{
		"fix the authentication bug",
		"ssh to the server and inspect nginx logs",
		"what is docker",
		"translate this paper",
	}
	for _, input := range cases {
		result := ic.Classify(input)
		if result.Intent != IntentUnknown {
			t.Fatalf("Classify(%q) = %s, want unknown without semantic classifier or LLM", input, result.Intent)
		}
	}
}

func TestIntentClassifier_ShortCommandsAreStructuralOnly(t *testing.T) {
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()

	for _, input := range []string{"ok", "go"} {
		result := ic.Classify(input)
		if result.Intent != IntentShortCommand {
			t.Fatalf("Classify(%q) = %s, want short_command", input, result.Intent)
		}
	}
}

func TestIntentClassifier_Layer3_MockLLM(t *testing.T) {
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) {
		switch {
		case containsAll(prompt, "REST", "API"):
			return "coding", nil
		case containsAll(prompt, "remote", "server"):
			return "ssh", nil
		case containsAll(prompt, "translate", "paper"):
			return "content", nil
		default:
			return "unknown", nil
		}
	})

	cases := []struct {
		input string
		want  string
	}{
		{"create a REST API for user management", IntentCoding},
		{"connect to the remote server", IntentSSH},
		{"translate this paper", IntentContent},
	}
	for _, tc := range cases {
		result := ic.Classify(tc.input)
		if result.Intent != tc.want || result.Layer != 3 {
			t.Fatalf("Classify(%q) = %s layer=%d, want %s layer=3", tc.input, result.Intent, result.Layer, tc.want)
		}
	}
}

func TestIntentClassifier_Layer3_FallbackOnError(t *testing.T) {
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) {
		return "", fmt.Errorf("LLM unavailable")
	})

	result := ic.Classify("some ambiguous input")
	if result.Intent != IntentUnknown {
		t.Fatalf("expected unknown on LLM error, got %s", result.Intent)
	}
}

func TestIntentClassifier_Layer3_ParsesVariousFormats(t *testing.T) {
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()

	formats := []struct {
		response string
		want     string
	}{
		{"coding", IntentCoding},
		{"Coding", IntentCoding},
		{"CODING", IntentCoding},
		{"coding.", IntentCoding},
		{"ssh ", IntentSSH},
		{"content.", IntentContent},
		{"chat!", IntentChat},
		{"browser\nsome explanation", IntentBrowser},
		{"query,", IntentQuery},
		{"garbage text", IntentUnknown},
		{"", IntentUnknown},
	}
	for _, f := range formats {
		ic.SetLLMFunc(func(prompt string) (string, error) {
			return f.response, nil
		})
		result := ic.Classify("classify this request")
		if result.Intent != f.want {
			t.Fatalf("LLM response %q parsed as %s, want %s", f.response, result.Intent, f.want)
		}
	}
}

func TestIntentClassifier_WaitReadyZeroValue(t *testing.T) {
	var ic IntentClassifier
	if ic.WaitReady(0) {
		t.Fatal("zero-value classifier should not report ready")
	}
	ic.Close()
}

func containsAll(s string, parts ...string) bool {
	s = strings.ToLower(s)
	for _, part := range parts {
		if !strings.Contains(s, strings.ToLower(part)) {
			return false
		}
	}
	return true
}
