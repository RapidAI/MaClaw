package agentruntime

import "testing"

func TestResponseHasPDF(t *testing.T) {
	for _, tc := range []struct {
		name, path, mime string
	}{
		{"report.pdf", "", ""},
		{"", "/tmp/report.pdf", ""},
		{"", "", "application/pdf; charset=utf-8"},
	} {
		if !ResponseHasPDF(tc.name, tc.path, tc.mime) {
			t.Errorf("ResponseHasPDF(%q,%q,%q)=false", tc.name, tc.path, tc.mime)
		}
	}
	if ResponseHasPDF("report.txt", "/tmp/report.txt", "text/plain") {
		t.Fatal("non-PDF response was classified as PDF")
	}
}

func TestHostOwnedGeneratePDFSucceeded(t *testing.T) {
	if !HostOwnedGeneratePDFSucceeded("PDF artifact published; deliver it through the current-channel file adapter.") {
		t.Fatal("success marker was not recognized")
	}
	for _, result := range []string{
		"failed: PDF artifact published was not created",
		"[system rejected] parameter_schema_invalid",
		"[system unknown] provider timeout",
	} {
		if HostOwnedGeneratePDFSucceeded(result) {
			t.Errorf("failure marker was accepted: %q", result)
		}
	}
}

func TestArtifactAttachErrorProjection(t *testing.T) {
	if !ShouldClearStaleErrorAfterArtifactAttach("LLM call failed: timeout") {
		t.Fatal("ordinary stale error should be cleared")
	}
	for _, err := range []string{"cancelled", "semantic_capability_unmet", "[system rejected] x", "daily_llm_budget_exceeded", "recovery_checkpoint_failed", "panicked"} {
		if ShouldClearStaleErrorAfterArtifactAttach(err) || !KeepVisibleErrorAfterArtifactAttach(err) {
			t.Errorf("protected error projection incorrect for %q", err)
		}
	}
}
