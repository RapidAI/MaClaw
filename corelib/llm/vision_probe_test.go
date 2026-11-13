package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestProbeVisionReadsTheImageAnswer(t *testing.T) {
	var sawImage bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sawImage = strings.Contains(string(body), "image_url")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"red"}}]}`))
	}))
	defer server.Close()
	supported, known := ProbeVision(context.Background(), corelib.MaclawLLMConfig{
		URL: server.URL + "/v1", Key: "k", Model: "see", Protocol: "openai",
	})
	if !sawImage || !supported || !known {
		t.Fatalf("saw=%v supported=%v known=%v", sawImage, supported, known)
	}
}

func TestProbeVisionRecordsAnExplicitImageRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"This model does not support image input"}}`, http.StatusBadRequest)
	}))
	defer server.Close()
	supported, known := ProbeVision(context.Background(), corelib.MaclawLLMConfig{
		URL: server.URL + "/v1", Key: "k", Model: "text", Protocol: "openai",
	})
	if supported || !known {
		t.Fatalf("supported=%v known=%v", supported, known)
	}
}

func TestImageInputRejectedIgnoresOtherFailures(t *testing.T) {
	if ImageInputRejected(&HTTPStatusError{StatusCode: 400, Body: []byte(`{"error":"bad model"}`)}) {
		t.Fatal("a request error that does not refuse images is not a capability answer")
	}
	if !ImageInputRejected(&HTTPStatusError{StatusCode: 422, Body: []byte(`image input is unsupported`)}) {
		t.Fatal("expected an image refusal")
	}
	if ImageInputRejected(&HTTPStatusError{StatusCode: 401, Body: []byte(`image unsupported`)}) {
		t.Fatal("auth failures are not a capability answer")
	}
}

func TestProviderVisionDecisionOverrideBeatsDetection(t *testing.T) {
	probed := corelib.MaclawLLMProvider{
		Model: "see", VisionModels: []string{"see"}, VisionTestedModels: []string{"see"},
		VisionByModel: []corelib.ModelVision{{Model: "see", Override: "auto"}},
	}
	sees, known, probe := corelib.ProviderVisionDecision(probed, "see")
	if !sees || !known || probe {
		t.Fatalf("detected vision = %v %v %v", sees, known, probe)
	}
	off := probed
	off.VisionByModel = []corelib.ModelVision{{Model: "see", Override: "off"}}
	sees, known, probe = corelib.ProviderVisionDecision(off, "see")
	if sees || !known || probe {
		t.Fatalf("override off = %v %v %v", sees, known, probe)
	}
	// A second model on the same provider is its own decision.
	other, known, probe := corelib.ProviderVisionDecision(probed, "qwen4-max-preview")
	if other || known || !probe {
		t.Fatalf("another unprobed model = %v %v %v", other, known, probe)
	}
	unknown := corelib.MaclawLLMProvider{Model: "see", VisionByModel: []corelib.ModelVision{{Model: "see", Override: "auto"}}}
	sees, known, probe = corelib.ProviderVisionDecision(unknown, "see")
	if sees || known || !probe {
		t.Fatalf("unprobed auto = %v %v %v", sees, known, probe)
	}
	legacy := corelib.MaclawLLMProvider{Model: "see"}
	sees, known, probe = corelib.ProviderVisionDecision(legacy, "see")
	if sees || !known || probe {
		t.Fatalf("legacy untested = %v %v %v", sees, known, probe)
	}
}
