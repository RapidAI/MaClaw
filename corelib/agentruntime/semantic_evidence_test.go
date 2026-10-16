package agentruntime

import "testing"

func TestTrustedLookupEvidenceRejectsControlMarkers(t *testing.T) {
	for _, value := range []string{"", "[system rejected] no", "[file_base64|x|text/plain]AAAA", "<turn: tool_call>", "｜DSML｜invoke"} {
		if !LookupEvidenceUntrusted(value) || TrustedLookupEvidence(value) != "" {
			t.Fatalf("tainted evidence accepted: %q", value)
		}
	}
	if got := TrustedLookupEvidence("杭州晴，28℃。"); got != "杭州晴，28℃。" {
		t.Fatalf("trusted evidence=%q", got)
	}
}
