package browser

import "testing"

func TestRunAgentFastBatchRequiresSession(t *testing.T) {
	if _, err := RunAgentFastBatch(nil, []StepSpec{{Action: "navigate", Params: map[string]string{"url": "https://example.com"}}}, ""); err == nil {
		t.Fatal("nil session unexpectedly ran")
	}
}
