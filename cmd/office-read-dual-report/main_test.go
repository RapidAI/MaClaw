package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestOpaqueSampleIDDoesNotExposePathOrBasename(t *testing.T) {
	path := `C:\\private\\Acme acquisition plan 2026.doc`
	salt := []byte("fixed report-only salt for unit tests")
	id := opaqueSampleID(path, salt)
	if !strings.HasPrefix(id, "sample-") || len(id) != len("sample-")+16 {
		t.Fatalf("opaqueSampleID(%q) = %q", path, id)
	}
	for _, sensitive := range []string{"Acme", "acquisition", "private", ".doc"} {
		if strings.Contains(strings.ToLower(id), strings.ToLower(sensitive)) {
			t.Fatalf("sample id leaks %q: %q", sensitive, id)
		}
	}
	if again := opaqueSampleID(path, salt); id != again {
		t.Fatalf("sample ids must be stable within a report run: %q != %q", id, again)
	}
	if changed := opaqueSampleID(path, []byte("another report salt")); id == changed {
		t.Fatalf("sample IDs must not correlate across reports: %q", id)
	}
}

func TestAssessFormatSeparatesEvidenceFromReleaseReview(t *testing.T) {
	passing := formatSummary{Total: 10, OfficeReadOK: 10, LegacyOK: 10, LegacyTokens: 100, SharedTokens: 97, OfficeTokenHit: .97}
	if got := assessFormat(passing, provenanceInternalAuthorized, 10, .95); got.QuantitativeGate != "pass" || len(got.Reasons) != 0 {
		t.Fatalf("authorized quantitative pass = %#v", got)
	}

	fixture := assessFormat(passing, provenanceFixture, 10, .95)
	if fixture.QuantitativeGate != "insufficient_evidence" || !containsReason(fixture.Reasons, "sample_provenance_is_not_internal_authorized") {
		t.Fatalf("fixture must not qualify as release evidence: %#v", fixture)
	}

	failing := assessFormat(formatSummary{Total: 10, OfficeReadOK: 10, LegacyOK: 10, LegacyTokens: 100, SharedTokens: 90, OfficeTokenHit: .9}, provenanceInternalAuthorized, 10, .95)
	if failing.QuantitativeGate != "fail" || !containsReason(failing.Reasons, "officeread_token_coverage_below_threshold") {
		t.Fatalf("token regression must fail: %#v", failing)
	}

	invalid := assessFormat(passing, provenanceInternalAuthorized, 10, math.NaN())
	if invalid.QuantitativeGate == "pass" || !containsReason(invalid.Reasons, "invalid_minimum_token_hit") {
		t.Fatalf("non-finite threshold must not pass: %#v", invalid)
	}
}

func TestValidProvenance(t *testing.T) {
	for _, value := range []string{provenanceInternalAuthorized, provenanceFixture, provenanceUnknown, "FIXTURE"} {
		if !validProvenance(value) {
			t.Fatalf("validProvenance(%q) = false", value)
		}
	}
	if validProvenance("customer_export") {
		t.Fatal("unexpected provenance accepted")
	}
}

func TestAutomatedValidationRequirementsCoverAllReleaseGates(t *testing.T) {
	want := []string{
		"automated_text_order_and_semantic_regression",
		"automated_resource_failure_and_pagination_regression",
		"automated_gui_attachment_tool_image_contract",
		"automated_format_level_rollback_regression",
	}
	if !reflect.DeepEqual(manualReviewRequirements, want) {
		t.Fatalf("manual review requirements = %#v, want %#v", manualReviewRequirements, want)
	}
}

func TestReleaseEvidenceTemplateIsPendingAndContentFree(t *testing.T) {
	digest := strings.Repeat("a", sha256.Size*2)
	template := newReleaseEvidenceTemplate(digest, []string{"doc", "xls"})
	if template.SchemaVersion != releaseEvidenceSchemaVersion || template.CreatedAt != "" || template.DualReportSHA256 != digest || template.ReviewerID != "set-release-owner-id" {
		t.Fatalf("unexpected template: %#v", template)
	}
	if !reflect.DeepEqual(template.Formats, []string{"doc", "xls"}) || len(template.ManualReviews) != len(manualReviewRequirements) {
		t.Fatalf("template rollout scope = %#v", template)
	}
	for _, review := range template.ManualReviews {
		if review.Status != "pending" || review.ReviewedAt != "" || !reflect.DeepEqual(review.Formats, template.Formats) {
			t.Fatalf("template must require an explicit completion: %#v", review)
		}
	}
	serialized, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"source_path", "document_text", "password", "notes"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("template must remain content-free: %s", serialized)
		}
	}
}

func TestWriteReleaseEvidenceTemplateDoesNotOverwriteExistingAttestation(t *testing.T) {
	template := newReleaseEvidenceTemplate(strings.Repeat("f", sha256.Size*2), []string{"doc"})
	path := filepath.Join(t.TempDir(), "release-evidence.json")
	if err := writeReleaseEvidenceTemplate(path, template); err != nil {
		t.Fatalf("write template: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"created_at": ""`) || !strings.Contains(string(data), `"status": "pending"`) {
		t.Fatalf("pending template content = %s", data)
	}
	if err := writeReleaseEvidenceTemplate(path, template); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing attestation overwrite error = %v", err)
	}
	dataAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(dataAfter) != string(data) {
		t.Fatal("existing release evidence was modified")
	}
}

func TestValidateReleaseEvidenceRequiresEveryCompletedReviewBoundToReport(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	digest := strings.Repeat("b", sha256.Size*2)
	evidence := newReleaseEvidenceTemplate(digest, []string{"doc", "xls"})
	evidence.ReviewerID = "release.owner@example.com"
	evidence.CreatedAt = now.Format(time.RFC3339)
	for i := range evidence.ManualReviews {
		evidence.ManualReviews[i].Status = "completed"
		evidence.ManualReviews[i].ReviewedAt = now.Format(time.RFC3339)
	}
	if reasons := validateReleaseEvidence(evidence, digest, []string{"doc", "xls"}, now.Format(time.RFC3339), 0, now); len(reasons) != 0 {
		t.Fatalf("complete evidence reasons = %#v", reasons)
	}

	evidence.DualReportSHA256 = strings.Repeat("c", sha256.Size*2)
	evidence.ManualReviews = evidence.ManualReviews[:len(evidence.ManualReviews)-1]
	evidence.Formats = []string{"doc"}
	reasons := validateReleaseEvidence(evidence, digest, []string{"doc", "xls"}, now.Format(time.RFC3339), 0, now)
	for _, want := range []string{
		"release_evidence_report_digest_mismatch",
		"release_evidence_formats_mismatch",
		"missing_manual_review_requirement:" + manualReviewRequirements[len(manualReviewRequirements)-1],
	} {
		if !containsReason(reasons, want) {
			t.Fatalf("evidence reasons = %#v, want %q", reasons, want)
		}
	}
}

func TestAuditManualReviewEvidenceDistinguishesMissingAndInvalidEvidence(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	digest := strings.Repeat("d", sha256.Size*2)
	missing := auditManualReviewEvidence("", digest, []string{"doc"}, now.Format(time.RFC3339), 0, now)
	if missing.Provided || missing.StructurallyComplete || !containsReason(missing.Reasons, "missing_release_evidence") {
		t.Fatalf("missing evidence = %#v", missing)
	}
	path := filepath.Join(t.TempDir(), "invalid-evidence.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"1.0","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := auditManualReviewEvidence(path, digest, []string{"doc"}, now.Format(time.RFC3339), 0, now)
	if invalid.Provided || invalid.StructurallyComplete || !containsReason(invalid.Reasons, "invalid_release_evidence") {
		t.Fatalf("invalid evidence = %#v", invalid)
	}
}

func TestAuditManualReviewEvidenceOnlyMarksParsedAttestationProvided(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	digest := strings.Repeat("e", sha256.Size*2)
	base := t.TempDir()
	missing := auditManualReviewEvidence(filepath.Join(base, "missing.json"), digest, []string{"doc"}, now.Format(time.RFC3339), 0, now)
	if missing.Provided || missing.StructurallyComplete || !containsReason(missing.Reasons, "invalid_release_evidence") {
		t.Fatalf("unreadable evidence = %#v", missing)
	}

	malformedPath := filepath.Join(base, "malformed.json")
	if err := os.WriteFile(malformedPath, []byte(`{"schema_version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	malformed := auditManualReviewEvidence(malformedPath, digest, []string{"doc"}, now.Format(time.RFC3339), 0, now)
	if malformed.Provided || malformed.StructurallyComplete || !containsReason(malformed.Reasons, "invalid_release_evidence") {
		t.Fatalf("malformed evidence = %#v", malformed)
	}

	validJSONPath := filepath.Join(base, "incomplete.json")
	evidence := newReleaseEvidenceTemplate(digest, []string{"doc"})
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validJSONPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	incomplete := auditManualReviewEvidence(validJSONPath, digest, []string{"doc"}, now.Format(time.RFC3339), 0, now)
	if !incomplete.Provided || incomplete.StructurallyComplete || !containsReason(incomplete.Reasons, "invalid_release_evidence_created_at") {
		t.Fatalf("schema-valid incomplete evidence = %#v", incomplete)
	}
}

func TestReleaseEvidenceTimestampsMustFollowReportAndRespectFreshness(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	reportAt := now.Add(-2 * time.Hour)
	digest := strings.Repeat("e", sha256.Size*2)
	evidence := newReleaseEvidenceTemplate(digest, []string{"doc"})
	evidence.ReviewerID = "release.owner@example.com"
	evidence.CreatedAt = reportAt.Format(time.RFC3339)
	for i := range evidence.ManualReviews {
		evidence.ManualReviews[i].Status = "completed"
		evidence.ManualReviews[i].ReviewedAt = reportAt.Add(-time.Minute).Format(time.RFC3339)
	}
	if reasons := validateReleaseEvidence(evidence, digest, []string{"doc"}, reportAt.Format(time.RFC3339), 0, now); !containsReason(reasons, "invalid_manual_reviewed_at:"+manualReviewRequirements[0]) {
		t.Fatalf("pre-report review must be rejected: %#v", reasons)
	}

	for i := range evidence.ManualReviews {
		evidence.ManualReviews[i].ReviewedAt = reportAt.Add(time.Minute).Format(time.RFC3339)
	}
	if reasons := validateReleaseEvidence(evidence, digest, []string{"doc"}, reportAt.Format(time.RFC3339), time.Hour, now); len(reasons) == 0 {
		t.Fatal("review evidence older than the configured maximum age must be rejected")
	}
	for i := range evidence.ManualReviews {
		evidence.ManualReviews[i].ReviewedAt = now.Format(time.RFC3339)
	}
	evidence.CreatedAt = now.Format(time.RFC3339)
	if reasons := validateReleaseEvidence(evidence, digest, []string{"doc"}, reportAt.Format(time.RFC3339), 3*time.Hour, now); len(reasons) != 0 {
		t.Fatalf("fresh post-report evidence reasons = %#v", reasons)
	}
}

func TestReleaseEvidenceCreatedAtMustNotPrecedeCompletedReviews(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	reportAt := now.Add(-2 * time.Hour)
	digest := strings.Repeat("a", sha256.Size*2)
	evidence := newReleaseEvidenceTemplate(digest, []string{"doc"})
	evidence.ReviewerID = "release.owner@example.com"
	evidence.CreatedAt = reportAt.Add(time.Minute).Format(time.RFC3339)
	for i := range evidence.ManualReviews {
		evidence.ManualReviews[i].Status = "completed"
		evidence.ManualReviews[i].ReviewedAt = reportAt.Add(2 * time.Minute).Format(time.RFC3339)
	}
	reasons := validateReleaseEvidence(evidence, digest, []string{"doc"}, reportAt.Format(time.RFC3339), 0, now)
	if !containsReason(reasons, "manual_review_after_release_evidence_created_at:"+manualReviewRequirements[0]) {
		t.Fatalf("post-evidence manual review must be rejected: %#v", reasons)
	}
	evidence.CreatedAt = reportAt.Add(3 * time.Minute).Format(time.RFC3339)
	if reasons := validateReleaseEvidence(evidence, digest, []string{"doc"}, reportAt.Format(time.RFC3339), 0, now); len(reasons) != 0 {
		t.Fatalf("evidence created after all reviews must pass: %#v", reasons)
	}
}

func TestBuildReportRequiresDualMode(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	_, err := buildReport([]string{"ignored.docx"}, reportOptions{
		Provenance: provenanceFixture, MinimumSamples: 1, MinimumTokenHit: .95, Salt: []byte("test salt"),
	})
	if err == nil || !strings.Contains(err.Error(), "engine must be dual") {
		t.Fatalf("err = %v, want dual-mode guard", err)
	}
}

func TestBuildReportRejectsFormatsOutsideEffectiveDualPolicy(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	_, err := buildReport(nil, reportOptions{
		Provenance: provenanceInternalAuthorized, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{"doc", "docx"}, Salt: []byte("test salt"),
	})
	if err == nil || !strings.Contains(err.Error(), "do not match effective dual policy") {
		t.Fatalf("mismatched report scope must fail: %v", err)
	}
}

func TestBuildReportRejectsInputsOutsideEffectiveDualPolicy(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc")
	path := filepath.Join(t.TempDir(), "outside-scope.xls")
	if err := os.WriteFile(path, []byte("not opened by report collection"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := buildReport([]string{path}, reportOptions{
		Provenance: provenanceFixture, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{"doc"}, Salt: []byte("test salt"),
	})
	if err == nil || !strings.Contains(err.Error(), `input format "xls" is outside effective dual policy`) {
		t.Fatalf("out-of-scope input must fail before collection: %v", err)
	}
}

func TestBuildReportRejectsMalformedEffectiveDualPolicyScope(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc,pdf")
	_, err := buildReport(nil, reportOptions{
		Provenance: provenanceInternalAuthorized, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{"doc"}, Salt: []byte("test salt"),
	})
	if err == nil || !strings.Contains(err.Error(), "do not match effective dual policy") {
		t.Fatalf("malformed effective scope must reject report collection: %v", err)
	}
}

func TestBuildReportUsesResolvedAgentPolicyRatherThanDuplicatedEnvironmentParsing(t *testing.T) {
	// The report command normally gets this policy from its environment, but
	// the extractor also supports a host-supplied persisted policy. Keeping the
	// evidence tool on the exported resolved snapshot prevents the two parsers
	// drifting when the GUI policy evolves.
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "")
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "dual", Formats: []string{".XLS", "doc"}}
	})
	defer restore()

	report, err := buildReport(nil, reportOptions{
		Provenance: provenanceFixture, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{"doc", "xls"}, Salt: []byte("test salt"),
	})
	if err != nil {
		t.Fatalf("build report with resolved agent policy: %v", err)
	}
	if !reflect.DeepEqual(report.Formats, []string{"doc", "xls"}) {
		t.Fatalf("report formats = %#v, want host-resolved policy", report.Formats)
	}
}

func TestBuildReportRejectsNonFiniteTokenThreshold(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	_, err := buildReport(nil, reportOptions{
		Provenance: provenanceFixture, MinimumSamples: 1, MinimumTokenHit: math.NaN(), Salt: []byte("test salt"),
	})
	if err == nil || !strings.Contains(err.Error(), "between 0 and 1") {
		t.Fatalf("non-finite threshold err = %v", err)
	}
}

func TestBuildReportCanonicalizesAndValidatesFormats(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc,xls")
	report, err := buildReport(nil, reportOptions{
		Provenance: provenanceFixture, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{".XLS", "doc", "xls"}, Salt: []byte("test salt"),
	})
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if !reflect.DeepEqual(report.Formats, []string{"doc", "xls"}) {
		t.Fatalf("canonical formats = %#v", report.Formats)
	}
	if _, err := buildReport(nil, reportOptions{
		Provenance: provenanceFixture, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{"pdf"}, Salt: []byte("test salt"),
	}); err == nil || !strings.Contains(err.Error(), "unsupported report format") {
		t.Fatalf("unsupported format err = %v", err)
	}
}

func TestReportObservationForSampleFormatDoesNotCreditSniffedFormat(t *testing.T) {
	input := agent.OfficeReadObservation{
		Format: "docx", SourceBytes: 42, Elapsed: time.Second,
		OfficeReadOK: true, OfficeReadTokens: 12, LegacyOK: true, LegacyTokens: 12, SharedTokens: 12,
	}
	got := reportObservationForSampleFormat("ppt", input)
	if got.Format != "ppt" || got.SourceBytes != 42 || got.Elapsed != time.Second || got.ErrorClass != "format_mismatch" {
		t.Fatalf("mismatch observation = %#v", got)
	}
	if got.OfficeReadOK || got.LegacyOK || got.OfficeReadTokens != 0 || got.LegacyTokens != 0 || got.SharedTokens != 0 {
		t.Fatalf("sniffed metrics must not count for another extension: %#v", got)
	}
	if same := reportObservationForSampleFormat(".docx", input); same != input {
		t.Fatalf("matching observation changed: %#v", same)
	}
}

func TestBuildReportKeepsFixturePathAndTextOutOfJSON(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "Acme confidential roadmap.docx")
	writeReportDOCX(t, path, "Confidential Apollo roadmap milestone")

	report, err := buildReport([]string{path}, reportOptions{
		Provenance: provenanceFixture, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{"docx"}, Salt: []byte("fixed report salt"),
	})
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if report.Assessment["docx"].QuantitativeGate != "insufficient_evidence" || !containsReason(report.Assessment["docx"].Reasons, "sample_provenance_is_not_internal_authorized") {
		t.Fatalf("fixture report must remain non-release evidence: %#v", report.Assessment)
	}
	if len(report.Files) != 1 || report.Files[0].SampleID == "" || report.Files[0].Format != "docx" {
		t.Fatalf("unexpected files: %#v", report.Files)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(data)
	for _, sensitive := range []string{path, filepath.Base(path), "Confidential", "Apollo", "roadmap"} {
		if strings.Contains(strings.ToLower(serialized), strings.ToLower(sensitive)) {
			t.Fatalf("report leaked %q: %s", sensitive, serialized)
		}
	}
}

func TestBuildReportCountsHardLinkedFileOnlyOnce(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	dir := t.TempDir()
	original := filepath.Join(dir, "original.docx")
	hardLink := filepath.Join(dir, "same-content-under-another-name.docx")
	writeReportDOCX(t, original, "one physical sample only")
	if err := os.Link(original, hardLink); err != nil {
		t.Skipf("hard links are unavailable in this test environment: %v", err)
	}

	paths := uniqueOfficePaths([]string{original, hardLink})
	if len(paths) != 1 {
		t.Fatalf("uniqueOfficePaths() = %#v, want exactly one physical file", paths)
	}

	report, err := buildReport([]string{original, hardLink}, reportOptions{
		Provenance: provenanceFixture, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{"docx"}, Salt: []byte("fixed report salt"),
	})
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if len(report.Files) != 1 {
		t.Fatalf("report files = %#v, want one physical sample", report.Files)
	}
	if got := report.Summary["docx"].Total; got != 1 {
		t.Fatalf("report docx total = %d, want 1", got)
	}
}

func TestUniqueOfficePathsKeepsDistinctCaseSensitiveFiles(t *testing.T) {
	dir := t.TempDir()
	upper := filepath.Join(dir, "Report.docx")
	lower := filepath.Join(dir, "report.docx")
	writeReportDOCX(t, upper, "upper case file")
	if err := os.WriteFile(lower, []byte("separate lower case file"), 0o600); err != nil {
		// Windows and default macOS volumes commonly reject this as the same
		// filename. The behavior under test only applies where both names can
		// coexist, so do not make filesystem case rules a portability failure.
		t.Skipf("filesystem does not permit case-distinct filenames: %v", err)
	}
	upperInfo, err := os.Stat(upper)
	if err != nil {
		t.Fatal(err)
	}
	lowerInfo, err := os.Stat(lower)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(upperInfo, lowerInfo) {
		t.Skip("filesystem resolves case-distinct names to one physical file")
	}
	if got := uniqueOfficePaths([]string{upper, lower}); len(got) != 2 {
		t.Fatalf("uniqueOfficePaths() = %#v, want two independent files", got)
	}
}

func TestBuildReportMarksMisnamedOOXMLAsFormatMismatch(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	// Enable both formats to prove the report does not simply call the
	// filename-extension route and count the DOCX result as a DOC sample.
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc,docx")
	path := filepath.Join(t.TempDir(), "misnamed.doc")
	writeReportDOCX(t, path, "actual DOCX content")

	report, err := buildReport([]string{path}, reportOptions{
		Provenance: provenanceFixture, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{"doc", "docx"}, Salt: []byte("fixed report salt"),
	})
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if len(report.Files) != 1 || report.Files[0].Format != "doc" || report.Files[0].ErrorClass != "format_mismatch" {
		t.Fatalf("misnamed report file = %#v", report.Files)
	}
	if item := report.Files[0]; item.OfficeReadOK || item.LegacyOK || item.OfficeTokens != 0 || item.LegacyTokens != 0 || item.SharedTokens != 0 {
		t.Fatalf("format mismatch must have no parser metrics: %#v", item)
	}
	if summary := report.Summary["doc"]; summary.Total != 0 || summary.OfficeReadOK != 0 || summary.LegacyOK != 0 {
		t.Fatalf("misnamed DOC must not enter any release-gate counter: %#v", summary)
	}
}

func TestBuildReportExcludesOversizedDualInputFromGate(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "oversized confidential.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(32*1024*1024 + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	report, err := buildReport([]string{path}, reportOptions{
		Provenance: provenanceFixture, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{"docx"}, Salt: []byte("fixed report salt"),
	})
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if len(report.Files) != 1 {
		t.Fatalf("files = %#v", report.Files)
	}
	item := report.Files[0]
	if item.Format != "docx" || item.ErrorClass != "input_too_large" || item.OfficeReadOK || item.LegacyOK || item.OfficeRunes != 0 || item.LegacyRunes != 0 || item.OfficeTokens != 0 || item.LegacyTokens != 0 || item.SharedTokens != 0 {
		t.Fatalf("oversized dual record = %#v", item)
	}
	if summary := report.Summary["docx"]; summary.Total != 0 || summary.OfficeReadOK != 0 || summary.LegacyOK != 0 || summary.OfficeTokens != 0 || summary.LegacyTokens != 0 || summary.SharedTokens != 0 {
		t.Fatalf("oversized dual input leaked into gate summary: %#v", summary)
	}
}

func TestFormatMismatchDoesNotSatisfySampleGate(t *testing.T) {
	inputs := make([]reportInput, 0, 11)
	for i := 0; i < 10; i++ {
		inputs = append(inputs, reportInput{
			SampleID: "mismatch-" + string(rune('a'+i)), Format: "doc",
			Observation: agent.OfficeReadObservation{Format: "doc", ErrorClass: "format_mismatch"},
		})
	}
	inputs = append(inputs, reportInput{
		SampleID: "valid", Format: "doc",
		Observation: agent.OfficeReadObservation{Format: "doc", OfficeReadOK: true, LegacyOK: true, OfficeReadTokens: 10, LegacyTokens: 10, SharedTokens: 10},
	})
	report := buildReportFromInputs(report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 10, MinimumTokenHit: .95, Formats: []string{"doc"},
		Summary: make(map[string]formatSummary), Assessment: make(map[string]formatAssessment),
	}, inputs)
	if summary := report.Summary["doc"]; summary.Total != 1 || summary.OfficeReadOK != 1 || summary.LegacyOK != 1 {
		t.Fatalf("format mismatch leaked into gate counters: %#v", summary)
	}
	assessment := report.Assessment["doc"]
	if assessment.QuantitativeGate != "insufficient_evidence" || !containsReason(assessment.Reasons, "insufficient_sample_count") {
		t.Fatalf("mismatches must not satisfy sample gate: %#v", assessment)
	}
}

func TestNotDualEnabledDoesNotSatisfySampleGate(t *testing.T) {
	inputs := make([]reportInput, 0, 11)
	for i := 0; i < 10; i++ {
		inputs = append(inputs, reportInput{
			SampleID: "disabled-" + string(rune('a'+i)), Format: "doc",
			Observation: agent.OfficeReadObservation{Format: "doc", ErrorClass: "not_dual_enabled"},
		})
	}
	inputs = append(inputs, reportInput{
		SampleID: "valid", Format: "doc",
		Observation: agent.OfficeReadObservation{Format: "doc", OfficeReadOK: true, LegacyOK: true, OfficeReadTokens: 10, LegacyTokens: 10, SharedTokens: 10},
	})
	report := buildReportFromInputs(report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 10, MinimumTokenHit: .95, Formats: []string{"doc"},
		Summary: make(map[string]formatSummary), Assessment: make(map[string]formatAssessment),
	}, inputs)
	if summary := report.Summary["doc"]; summary.Total != 1 || summary.OfficeReadOK != 1 || summary.LegacyOK != 1 {
		t.Fatalf("disabled routes leaked into gate counters: %#v", summary)
	}
	if assessment := report.Assessment["doc"]; assessment.QuantitativeGate != "insufficient_evidence" || !containsReason(assessment.Reasons, "insufficient_sample_count") {
		t.Fatalf("disabled routes must not satisfy sample gate: %#v", assessment)
	}
}

func TestRejectedContainerSamplesDoNotSatisfySampleGate(t *testing.T) {
	inputs := make([]reportInput, 0, 11)
	for i := 0; i < 10; i++ {
		inputs = append(inputs, reportInput{
			SampleID: "rejected-" + string(rune('a'+i)), Format: "doc",
			Observation: agent.OfficeReadObservation{Format: "doc", ErrorClass: "encrypted"},
		})
	}
	inputs = append(inputs, reportInput{
		SampleID: "valid", Format: "doc",
		Observation: agent.OfficeReadObservation{Format: "doc", OfficeReadOK: true, OfficeReadSize: 10, LegacyOK: true, LegacySize: 10, OfficeReadTokens: 10, LegacyTokens: 10, SharedTokens: 10},
	})
	report := buildReportFromInputs(report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 10, MinimumTokenHit: .95, Formats: []string{"doc"},
		Summary: make(map[string]formatSummary), Assessment: make(map[string]formatAssessment),
	}, inputs)
	if summary := report.Summary["doc"]; summary.Total != 1 || summary.OfficeReadOK != 1 || summary.LegacyOK != 1 {
		t.Fatalf("rejected containers leaked into gate counters: %#v", summary)
	}
	assessment := report.Assessment["doc"]
	if assessment.QuantitativeGate != "insufficient_evidence" || !containsReason(assessment.Reasons, "insufficient_sample_count") {
		t.Fatalf("rejected containers must not satisfy sample gate: %#v", assessment)
	}
}

func TestBuildReportMarksEnabledButUnsampledFormatInsufficient(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc,xls")
	report, err := buildReport(nil, reportOptions{
		Provenance: provenanceInternalAuthorized, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: []string{"doc", "xls"}, Salt: []byte("fixed report salt"),
	})
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	for _, format := range []string{"doc", "xls"} {
		summary, ok := report.Summary[format]
		if !ok || summary.Total != 0 {
			t.Fatalf("missing unsampled format %q: %#v", format, report.Summary)
		}
		assessment := report.Assessment[format]
		if assessment.QuantitativeGate != "insufficient_evidence" || !containsReason(assessment.Reasons, "insufficient_sample_count") {
			t.Fatalf("unsampled format %q must block approval: %#v", format, assessment)
		}
	}
}

func TestAuditReportRequiresAuthorizedPassingEvidenceForEveryFormat(t *testing.T) {
	passingFiles := make([]reportFile, 0, 20)
	for i := 0; i < 10; i++ {
		docID := fmt.Sprintf("sample-%016x", i*2+1)
		xlsID := fmt.Sprintf("sample-%016x", i*2+2)
		passingFiles = append(passingFiles,
			reportFile{SampleID: docID, Format: "doc", OfficeReadOK: true, OfficeRunes: 1, LegacyOK: true, LegacyRunes: 1, OfficeTokens: 10, LegacyTokens: 10, SharedTokens: 10},
			reportFile{SampleID: xlsID, Format: "xls", OfficeReadOK: true, OfficeRunes: 1, LegacyOK: true, LegacyRunes: 1, OfficeTokens: 10, LegacyTokens: 10, SharedTokens: 10},
		)
	}
	passing := report{
		SchemaVersion: reportSchemaVersion, GeneratedAt: "2020-01-02T03:04:05Z", Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 10, MinimumTokenHit: .95,
		Files:   passingFiles,
		Formats: []string{"doc", "xls"}, Assessment: map[string]formatAssessment{
			"doc": {QuantitativeGate: "pass"}, "xls": {QuantitativeGate: "pass"},
		}, Summary: map[string]formatSummary{
			"doc": summarizeReportFiles(passingFiles, "doc"),
			"xls": summarizeReportFiles(passingFiles, "xls"),
		}, ManualReviewRequired: []string{"review_text_order_and_business_answer_quality"},
	}
	if audit := auditReport(passing, []string{"doc", "xls"}); !audit.QuantitativeReady || len(audit.Reasons) != 0 || !reflect.DeepEqual(audit.ManualReviewRequired, manualReviewRequirements) {
		t.Fatalf("passing audit = %#v", audit)
	}

	fixture := passing
	fixture.SampleProvenance = provenanceFixture
	if audit := auditReport(fixture, []string{"doc", "xls"}); audit.QuantitativeReady || !containsReason(audit.Reasons, "sample_provenance_is_not_internal_authorized") {
		t.Fatalf("fixture audit = %#v", audit)
	}

	failed := passing
	failed.Assessment = map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}, "xls": {QuantitativeGate: "fail"}}
	failed.Files[1].SharedTokens = 0
	failed.Summary["xls"] = summarizeReportFiles(failed.Files, "xls")
	if audit := auditReport(failed, []string{"doc", "xls"}); audit.QuantitativeReady || !containsReason(audit.Reasons, "quantitative_gate_not_pass:xls") {
		t.Fatalf("failed audit = %#v", audit)
	}

	if audit := auditReport(passing, []string{"pdf"}); audit.QuantitativeReady || !containsReason(audit.Reasons, "unsupported_required_format:pdf") {
		t.Fatalf("unsupported required format must fail audit = %#v", audit)
	}
}

func TestAuditReportAllowsFixtureOnlyWithExplicitAutomatedProfile(t *testing.T) {
	formats := []string{"doc", "ppt"}
	files := []reportFile{
		{SampleID: "sample-0000000000000001", Format: "doc", OfficeReadOK: true, OfficeRunes: 1, OfficeTokens: 10, LegacyOK: true, LegacyRunes: 1, LegacyTokens: 10, SharedTokens: 10},
		// PPT has no legacy reader. The automated profile must require the
		// OfficeRead result to cover every fixture rather than failing merely
		// because no legacy baseline exists.
		{SampleID: "sample-0000000000000002", Format: "ppt", OfficeReadOK: true, OfficeRunes: 1, OfficeTokens: 10},
	}
	input := report{
		SchemaVersion: reportSchemaVersion, GeneratedAt: "2026-08-10T12:00:00Z", Engine: "dual", SampleProvenance: provenanceFixture,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: formats, Files: files,
		Summary: map[string]formatSummary{
			"doc": summarizeReportFiles(files, "doc"), "ppt": summarizeReportFiles(files, "ppt"),
		},
		Assessment: map[string]formatAssessment{
			"doc": assessFormat(summarizeReportFiles(files, "doc"), provenanceFixture, 1, .95),
			"ppt": assessFormat(summarizeReportFiles(files, "ppt"), provenanceFixture, 1, .95),
		},
	}
	if audit := auditReport(input, formats); audit.QuantitativeReady || !containsReason(audit.Reasons, "sample_provenance_is_not_internal_authorized") {
		t.Fatalf("default audit must reject fixtures: %#v", audit)
	}
	audit := auditReportWithOptions(input, auditOptions{RequiredFormats: formats, MinimumSamples: 1, MinimumTokenHit: .95, Now: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC), AllowFixtureAutomation: true})
	if audit.QuantitativeReady || !audit.FixtureAutomationAccepted || !audit.FixtureAutomationReady || !auditMeetsSelectedProfile(audit, true) || len(audit.Reasons) != 0 {
		t.Fatalf("automated fixture audit = %#v", audit)
	}
	if auditMeetsSelectedProfile(audit, false) {
		t.Fatalf("fixture evidence must not satisfy the production profile: %#v", audit)
	}

	input.Files[1].OfficeReadOK = false
	input.Files[1].OfficeRunes = 0
	input.Summary["ppt"] = summarizeReportFiles(input.Files, "ppt")
	audit = auditReportWithOptions(input, auditOptions{RequiredFormats: formats, MinimumSamples: 1, MinimumTokenHit: .95, Now: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC), AllowFixtureAutomation: true})
	if audit.QuantitativeReady || audit.FixtureAutomationReady || auditMeetsSelectedProfile(audit, true) || !containsReason(audit.Reasons, "quantitative_gate_not_pass:ppt") {
		t.Fatalf("failed automated fixture must block audit: %#v", audit)
	}
}

func TestFixtureAutomationProfileStaysScopedAndFailClosed(t *testing.T) {
	fixture := report{
		SchemaVersion: reportSchemaVersion, GeneratedAt: "2026-08-10T12:00:00Z", Engine: "dual", SampleProvenance: provenanceFixture,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"},
		Files:      []reportFile{{SampleID: "sample-0000000000000001", Format: "doc", OfficeReadOK: true, OfficeRunes: 1, OfficeTokens: 10, LegacyOK: true, LegacyRunes: 1, LegacyTokens: 10, SharedTokens: 10}},
		Summary:    map[string]formatSummary{"doc": {Total: 1, OfficeReadOK: 1, LegacyOK: 1, OfficeTokens: 10, LegacyTokens: 10, SharedTokens: 10, OfficeTokenHit: 1, LegacyTokenHit: 1}},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "insufficient_evidence", Reasons: []string{"sample_provenance_is_not_internal_authorized"}}},
	}
	options := auditOptions{RequiredFormats: []string{"doc"}, MinimumSamples: 1, MinimumTokenHit: .95, Now: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC), AllowFixtureAutomation: true}

	t.Run("unknown provenance is never accepted", func(t *testing.T) {
		input := fixture
		input.SampleProvenance = provenanceUnknown
		audit := auditReportWithOptions(input, options)
		if audit.FixtureAutomationAccepted || audit.FixtureAutomationReady || auditMeetsSelectedProfile(audit, true) || !containsReason(audit.Reasons, "sample_provenance_is_not_internal_authorized") {
			t.Fatalf("unknown provenance audit = %#v", audit)
		}
	})

	t.Run("missing serialized assessment remains a rejection", func(t *testing.T) {
		input := fixture
		input.Assessment = map[string]formatAssessment{}
		audit := auditReportWithOptions(input, options)
		if audit.FixtureAutomationReady || !containsReason(audit.Reasons, "missing_assessment:doc") {
			t.Fatalf("missing fixture assessment audit = %#v", audit)
		}
	})

	t.Run("coverage regression remains a rejection", func(t *testing.T) {
		input := fixture
		input.Files[0].SharedTokens = 8
		input.Summary = map[string]formatSummary{"doc": summarizeReportFiles(input.Files, "doc")}
		audit := auditReportWithOptions(input, options)
		if audit.FixtureAutomationReady || !containsReason(audit.Reasons, "assessment_does_not_match_files:doc") || !containsReason(audit.Reasons, "quantitative_gate_not_pass:doc") || !containsReason(audit.Reasons, "required_quantitative_gate_not_pass:doc") {
			t.Fatalf("coverage-regressed fixture audit = %#v", audit)
		}
	})

	t.Run("tampered serialized assessment remains a rejection", func(t *testing.T) {
		input := fixture
		input.Assessment = map[string]formatAssessment{"doc": {QuantitativeGate: "pass", Reasons: []string{}}}
		audit := auditReportWithOptions(input, options)
		if audit.FixtureAutomationReady || !containsReason(audit.Reasons, "assessment_does_not_match_files:doc") || !containsReason(audit.Reasons, "assessment_reasons_do_not_match_files:doc") {
			t.Fatalf("tampered fixture assessment audit = %#v", audit)
		}
	})

	t.Run("authorized reports retain production semantics", func(t *testing.T) {
		input := fixture
		input.SampleProvenance = provenanceInternalAuthorized
		input.Summary = map[string]formatSummary{"doc": summarizeReportFiles(input.Files, "doc")}
		input.MinimumTokenHit = .8
		input.Assessment = make(map[string]formatAssessment)
		input.Assessment["doc"] = assessFormat(input.Summary["doc"], input.SampleProvenance, input.MinimumSamples, input.MinimumTokenHit)
		audit := auditReportWithOptions(input, auditOptions{RequiredFormats: []string{"doc"}, MinimumSamples: 1, MinimumTokenHit: .8, Now: options.Now, AllowFixtureAutomation: true})
		if !audit.QuantitativeReady || audit.FixtureAutomationAccepted || audit.FixtureAutomationReady || !auditMeetsSelectedProfile(audit, true) {
			t.Fatalf("authorized audit = %#v", audit)
		}
	})
}

func TestAuditReportRequiresAllSixOfficeFormatsWhenRequested(t *testing.T) {
	formats := []string{"doc", "docx", "ppt", "pptx", "xls", "xlsx"}
	files := make([]reportFile, 0, len(formats))
	for index, format := range formats {
		files = append(files, reportFile{
			SampleID: fmt.Sprintf("sample-%016x", index+1), Format: format,
			OfficeReadOK: true, OfficeRunes: 1, OfficeTokens: 10,
			LegacyOK: true, LegacyRunes: 1, LegacyTokens: 10, SharedTokens: 10,
		})
	}
	input := report{
		SchemaVersion: reportSchemaVersion, GeneratedAt: "2026-08-10T12:00:00Z", Engine: "dual",
		SampleProvenance: provenanceInternalAuthorized, MinimumSamples: 1, MinimumTokenHit: .95,
		Formats: formats, Files: files,
		Summary: make(map[string]formatSummary, len(formats)), Assessment: make(map[string]formatAssessment, len(formats)),
	}
	for _, format := range formats {
		input.Summary[format] = summarizeReportFiles(files, format)
		input.Assessment[format] = assessFormat(input.Summary[format], input.SampleProvenance, input.MinimumSamples, input.MinimumTokenHit)
	}

	if audit := auditReport(input, formats); !audit.QuantitativeReady {
		t.Fatalf("six-format authorized report must pass: %#v", audit)
	}

	missing := input
	missing.Formats = append([]string(nil), formats[:3]...)
	delete(missing.Summary, "pptx")
	delete(missing.Assessment, "pptx")
	if audit := auditReport(missing, formats); audit.QuantitativeReady ||
		!containsReason(audit.Reasons, "required_format_not_declared:pptx") ||
		!containsReason(audit.Reasons, "missing_summary:pptx") ||
		!containsReason(audit.Reasons, "missing_assessment:pptx") {
		t.Fatalf("missing required six-format evidence must fail audit: %#v", audit)
	}
}
func TestAuditReportOptionalAuthorizedEvidenceFreshness(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	files := make([]reportFile, 0, 10)
	for i := 0; i < 10; i++ {
		files = append(files, reportFile{
			SampleID: fmt.Sprintf("sample-%016x", i+1), Format: "doc",
			OfficeReadOK: true, OfficeRunes: 1, OfficeTokens: 10,
			LegacyOK: true, LegacyRunes: 1, LegacyTokens: 10, SharedTokens: 10,
		})
	}
	input := report{
		SchemaVersion: reportSchemaVersion, GeneratedAt: now.Add(-23 * time.Hour).Format(time.RFC3339), Engine: "dual",
		SampleProvenance: provenanceInternalAuthorized, MinimumSamples: 10, MinimumTokenHit: .95,
		Formats: []string{"doc"}, Files: files,
		Summary:    map[string]formatSummary{"doc": summarizeReportFiles(files, "doc")},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}
	fresh := auditReportWithOptions(input, auditOptions{
		RequiredFormats: []string{"doc"}, Now: now, MaximumAuthorizedReportAge: 24 * time.Hour,
	})
	if !fresh.QuantitativeReady || fresh.MaximumAuthorizedReportAge != "24h0m0s" {
		t.Fatalf("fresh authorized audit = %#v", fresh)
	}

	stale := input
	stale.GeneratedAt = now.Add(-25 * time.Hour).Format(time.RFC3339)
	expired := auditReportWithOptions(stale, auditOptions{
		RequiredFormats: []string{"doc"}, Now: now, MaximumAuthorizedReportAge: 24 * time.Hour,
	})
	if expired.QuantitativeReady || !containsReason(expired.Reasons, "authorized_report_exceeds_max_age") {
		t.Fatalf("expired authorized audit = %#v", expired)
	}

	fixture := stale
	fixture.SampleProvenance = provenanceFixture
	fixtureAudit := auditReportWithOptions(fixture, auditOptions{
		RequiredFormats: []string{"doc"}, Now: now, MaximumAuthorizedReportAge: time.Hour,
	})
	if containsReason(fixtureAudit.Reasons, "authorized_report_exceeds_max_age") || !containsReason(fixtureAudit.Reasons, "sample_provenance_is_not_internal_authorized") {
		t.Fatalf("fixture freshness audit = %#v", fixtureAudit)
	}
}

func TestAuditReportDoesNotMutateCallerRequiredFormats(t *testing.T) {
	required := []string{".DOC", "doc", "pdf", "xls"}
	original := append([]string(nil), required...)
	audit := auditReportWithOptions(report{}, auditOptions{
		RequiredFormats: required,
		Now:             time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC),
	})
	if !reflect.DeepEqual(required, original) {
		t.Fatalf("audit mutated required formats: got %#v, want %#v", required, original)
	}
	if !reflect.DeepEqual(audit.Formats, []string{"doc", "xls"}) {
		t.Fatalf("canonical audit formats = %#v", audit.Formats)
	}
	for _, reason := range []string{"duplicate_required_format:doc", "unsupported_required_format:pdf"} {
		if !containsReason(audit.Reasons, reason) {
			t.Fatalf("audit reasons = %#v, want %q", audit.Reasons, reason)
		}
	}
}

func TestAuditReportAppliesCurrentMinimumThresholds(t *testing.T) {
	files := make([]reportFile, 0, 5)
	for i := 0; i < 5; i++ {
		files = append(files, reportFile{
			SampleID: fmt.Sprintf("sample-%016x", i+1), Format: "doc",
			OfficeReadOK: true, OfficeRunes: 1, OfficeTokens: 100,
			LegacyOK: true, LegacyRunes: 1, LegacyTokens: 100, SharedTokens: 96,
		})
	}
	input := report{
		SchemaVersion: reportSchemaVersion, GeneratedAt: "2026-08-10T12:00:00Z", Engine: "dual",
		SampleProvenance: provenanceInternalAuthorized, MinimumSamples: 5, MinimumTokenHit: .95,
		Formats: []string{"doc"}, Files: files,
		Summary:    map[string]formatSummary{"doc": summarizeReportFiles(files, "doc")},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}
	passing := auditReportWithOptions(input, auditOptions{
		RequiredFormats: []string{"doc"}, MinimumSamples: 5, MinimumTokenHit: .95,
		Now: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC),
	})
	if !passing.QuantitativeReady || passing.RequiredMinimumSamples != 5 || passing.RequiredMinimumTokenHit != .95 {
		t.Fatalf("current matching audit = %#v", passing)
	}

	stricterSamples := auditReportWithOptions(input, auditOptions{
		RequiredFormats: []string{"doc"}, MinimumSamples: 10, MinimumTokenHit: .95,
		Now: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC),
	})
	if stricterSamples.QuantitativeReady || !containsReason(stricterSamples.Reasons, "report_minimum_samples_below_required") || !containsReason(stricterSamples.Reasons, "required_quantitative_gate_not_pass:doc") {
		t.Fatalf("stricter sample policy must reject stale report criteria: %#v", stricterSamples)
	}

	stricterCoverage := auditReportWithOptions(input, auditOptions{
		RequiredFormats: []string{"doc"}, MinimumSamples: 5, MinimumTokenHit: .97,
		Now: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC),
	})
	if stricterCoverage.QuantitativeReady || !containsReason(stricterCoverage.Reasons, "report_minimum_token_hit_below_required") || !containsReason(stricterCoverage.Reasons, "required_quantitative_gate_not_pass:doc") {
		t.Fatalf("stricter coverage policy must reject stale report criteria: %#v", stricterCoverage)
	}
}

func TestAuditReportRejectsNegativeMaximumAuthorizedReportAge(t *testing.T) {
	audit := auditReportWithOptions(report{}, auditOptions{
		RequiredFormats: []string{"doc"}, Now: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC),
		MaximumAuthorizedReportAge: -time.Hour,
	})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_maximum_authorized_report_age") {
		t.Fatalf("negative freshness policy audit = %#v", audit)
	}
}

func TestReportGeneratedAtValidRejectsMissingMalformedNonUTCAndFutureTimes(t *testing.T) {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	for _, raw := range []string{
		"",
		"not-a-timestamp",
		"2026-08-09T12:00:00+08:00",
		"2026-08-09T12:05:01Z",
	} {
		if reportGeneratedAtValid(raw, now) {
			t.Fatalf("invalid generated_at accepted: %q", raw)
		}
	}
	for _, raw := range []string{
		"2020-01-02T03:04:05Z",
		"2026-08-09T12:05:00Z",
		"2026-08-09T12:00:00+00:00",
	} {
		if !reportGeneratedAtValid(raw, now) {
			t.Fatalf("valid generated_at rejected: %q", raw)
		}
	}
}

func TestAuditReportRejectsInvalidGeneratedAt(t *testing.T) {
	valid := reportFile{SampleID: "sample-0000000000000001", Format: "doc", OfficeReadOK: true, OfficeRunes: 1, OfficeTokens: 1, LegacyOK: true, LegacyRunes: 1, LegacyTokens: 1, SharedTokens: 1}
	input := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"}, Files: []reportFile{valid},
		Summary:    map[string]formatSummary{"doc": summarizeReportFiles([]reportFile{valid}, "doc")},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}
	if audit := auditReport(input, []string{"doc"}); audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_generated_at") {
		t.Fatalf("missing generated_at must block audit: %#v", audit)
	}
}

func TestAuditReportRestoresCanonicalManualReviewRequirements(t *testing.T) {
	input := report{
		SchemaVersion: reportSchemaVersion, GeneratedAt: "2020-01-02T03:04:05Z", Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"},
		Files:                []reportFile{{SampleID: "sample-0000000000000001", Format: "doc", OfficeReadOK: true, OfficeRunes: 1, LegacyOK: true, LegacyRunes: 1, OfficeTokens: 1, LegacyTokens: 1, SharedTokens: 1}},
		Summary:              map[string]formatSummary{"doc": {Total: 1, OfficeReadOK: 1, LegacyOK: 1, OfficeTokens: 1, LegacyTokens: 1, SharedTokens: 1, OfficeTokenHit: 1, LegacyTokenHit: 1}},
		Assessment:           map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
		ManualReviewRequired: []string{},
	}
	audit := auditReport(input, []string{"doc"})
	if !audit.QuantitativeReady || !reflect.DeepEqual(audit.ManualReviewRequired, manualReviewRequirements) {
		t.Fatalf("audit must restore canonical manual requirements: %#v", audit)
	}
}

func TestAuditReportRecomputesAggregateEvidenceInsteadOfTrustingPass(t *testing.T) {
	report := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 10, MinimumTokenHit: .95, Formats: []string{"doc"},
		Files: []reportFile{{SampleID: "sample-doc", Format: "doc", OfficeReadOK: true, LegacyOK: true, OfficeTokens: 1, LegacyTokens: 1, SharedTokens: 1}},
		Summary: map[string]formatSummary{
			// A hand-edited report can claim pass even though its actual coverage
			// is below threshold. The audit must reject that contradiction.
			"doc": {Total: 10, OfficeReadOK: 10, LegacyOK: 10, LegacyTokens: 100, SharedTokens: 80, OfficeTokenHit: .8},
		},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}
	audit := auditReport(report, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "summary_does_not_match_files:doc") || !containsReason(audit.Reasons, "assessment_does_not_match_files:doc") || !containsReason(audit.Reasons, "quantitative_gate_not_pass:doc") {
		t.Fatalf("tampered report audit = %#v", audit)
	}
}

func TestAuditReportRejectsAssessmentReasonTampering(t *testing.T) {
	file := reportFile{SampleID: "sample-doc", Format: "doc", OfficeReadOK: true, LegacyOK: true, OfficeTokens: 10, LegacyTokens: 10, SharedTokens: 10}
	input := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceFixture,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"}, Files: []reportFile{file},
		Summary:    map[string]formatSummary{"doc": summarizeReportFiles([]reportFile{file}, "doc")},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "insufficient_evidence", Reasons: []string{}}},
	}
	audit := auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "assessment_reasons_do_not_match_files:doc") {
		t.Fatalf("reason tampering must block audit: %#v", audit)
	}
}

func TestAuditReportRejectsDuplicateSampleIDs(t *testing.T) {
	files := []reportFile{{SampleID: "reused", Format: "doc", OfficeReadOK: true, LegacyOK: true, OfficeTokens: 1, LegacyTokens: 1, SharedTokens: 1}}
	input := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"}, Files: append(files, files[0]),
		Summary:    map[string]formatSummary{"doc": summarizeReportFiles(append(files, files[0]), "doc")},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}
	if audit := auditReport(input, []string{"doc"}); audit.QuantitativeReady || !containsReason(audit.Reasons, "duplicate_sample_id") {
		t.Fatalf("duplicate sample IDs must block audit: %#v", audit)
	}
}

func TestAuditReportRejectsNonOpaqueSampleIDAndImpossibleSharedTokens(t *testing.T) {
	file := reportFile{SampleID: "customer-roadmap.docx", Format: "doc", OfficeReadOK: true, LegacyOK: true, OfficeTokens: 1, LegacyTokens: 1, SharedTokens: 1}
	input := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"}, Files: []reportFile{file},
		Summary:    map[string]formatSummary{"doc": summarizeReportFiles([]reportFile{file}, "doc")},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}
	audit := auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_sample_id") {
		t.Fatalf("non-opaque sample ID must block audit: %#v", audit)
	}

	file.SampleID = "sample-0000000000000001"
	file.LegacyOK = false
	input.Files = []reportFile{file}
	input.Summary["doc"] = summarizeReportFiles(input.Files, "doc")
	audit = auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_file_metrics") {
		t.Fatalf("shared tokens without both successes must block audit: %#v", audit)
	}
}

func TestAuditReportRejectsSuccessfulEmptyEvidence(t *testing.T) {
	valid := reportFile{SampleID: "sample-0000000000000001", Format: "doc", OfficeReadOK: true, OfficeRunes: 1, OfficeTokens: 1, LegacyOK: true, LegacyRunes: 1, LegacyTokens: 1, SharedTokens: 1}
	input := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"}, Files: []reportFile{valid},
		Summary:    map[string]formatSummary{"doc": summarizeReportFiles([]reportFile{valid}, "doc")},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}

	input.Files[0].OfficeRunes = 0
	input.Summary["doc"] = summarizeReportFiles(input.Files, "doc")
	audit := auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_file_metrics") {
		t.Fatalf("OfficeRead success without text evidence must block audit: %#v", audit)
	}

	input.Files[0] = valid
	input.Files[0].LegacyRunes = 0
	input.Summary["doc"] = summarizeReportFiles(input.Files, "doc")
	audit = auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_file_metrics") {
		t.Fatalf("legacy success without text evidence must block audit: %#v", audit)
	}

	// The comparison tokenizer intentionally does not create tokens for all
	// visible Unicode. A non-empty emoji/punctuation document is a real parse
	// result and should be assessed from its zero token baseline, not rejected
	// as malformed evidence.
	input.Files[0] = valid
	input.Files[0].OfficeTokens = 0
	input.Files[0].LegacyTokens = 0
	input.Files[0].SharedTokens = 0
	input.Summary["doc"] = summarizeReportFiles(input.Files, "doc")
	input.Assessment["doc"] = assessFormat(input.Summary["doc"], input.SampleProvenance, input.MinimumSamples, input.MinimumTokenHit)
	audit = auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || containsReason(audit.Reasons, "invalid_file_metrics") {
		t.Fatalf("non-empty zero-token evidence must be assessed, not rejected: %#v", audit)
	}
}

func TestAuditReportRejectsImpossibleMetricsAndUndeclaredRequiredFormat(t *testing.T) {
	files := make([]reportFile, 0, 10)
	for i := 0; i < 10; i++ {
		files = append(files, reportFile{
			SampleID: "sample-" + string(rune('a'+i)), Format: "doc", OfficeReadOK: true, LegacyOK: true,
			OfficeTokens: 1, LegacyTokens: 1, SharedTokens: 2,
		})
	}
	input := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 10, MinimumTokenHit: .95, Formats: []string{"xls"}, Files: files,
		Summary:    map[string]formatSummary{"doc": summarizeReportFiles(files, "doc")},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}
	audit := auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_file_metrics") || !containsReason(audit.Reasons, "required_format_not_declared:doc") {
		t.Fatalf("impossible/undeclared report must fail audit: %#v", audit)
	}
}

func TestAuditReportRejectsDuplicateRequiredFormatAliases(t *testing.T) {
	files := []reportFile{{SampleID: "sample-doc", Format: "doc", OfficeReadOK: true, LegacyOK: true, OfficeTokens: 1, LegacyTokens: 1, SharedTokens: 1}}
	input := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"}, Files: files,
		Summary:    map[string]formatSummary{"doc": summarizeReportFiles(files, "doc")},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}
	audit := auditReport(input, []string{".DOC", "doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "duplicate_required_format:doc") {
		t.Fatalf("duplicate required format alias must block audit: %#v", audit)
	}
	if len(audit.Formats) != 1 || audit.Formats[0] != "doc" {
		t.Fatalf("audit formats must be normalized and deduplicated: %#v", audit.Formats)
	}
}

func TestAuditDefaultsPreserveMalformedReportFormatScopeForRejection(t *testing.T) {
	input := report{
		SchemaVersion: reportSchemaVersion, GeneratedAt: "2026-08-10T12:00:00Z", Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc", "doc"},
		Summary: map[string]formatSummary{}, Assessment: map[string]formatAssessment{},
	}
	audit := auditReport(input, append([]string(nil), input.Formats...))
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "duplicate_required_format:doc") || !containsReason(audit.Reasons, "duplicate_declared_format:doc") {
		t.Fatalf("malformed default format scope must fail audit: %#v", audit)
	}
}

func TestAuditReportRejectsUndeclaredOrNonCanonicalFileFormatAndNonFiniteThreshold(t *testing.T) {
	files := []reportFile{{SampleID: "sample-doc", Format: "docx", OfficeReadOK: true, LegacyOK: true, OfficeTokens: 1, LegacyTokens: 1, SharedTokens: 1}}
	input := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"}, Files: files,
		Summary:    map[string]formatSummary{"doc": {}},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}
	audit := auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "file_format_not_declared:docx") {
		t.Fatalf("undeclared file format must block audit: %#v", audit)
	}

	input.Files[0].Format = ".DOC"
	audit = auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_file_format") {
		t.Fatalf("non-canonical file format must block audit: %#v", audit)
	}

	input.MinimumTokenHit = math.NaN()
	audit = auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_minimum_token_hit") {
		t.Fatalf("non-finite token threshold must block audit: %#v", audit)
	}
}

func TestAuditReportRejectsUndeclaredAggregateKeysAndInvalidErrorMetrics(t *testing.T) {
	passingFile := reportFile{SampleID: "sample-doc", Format: "doc", OfficeReadOK: true, OfficeRunes: 1, OfficeTokens: 1, LegacyOK: true, LegacyRunes: 1, LegacyTokens: 1, SharedTokens: 1}
	input := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"}, Files: []reportFile{passingFile},
		Summary: map[string]formatSummary{
			"doc": summarizeReportFiles([]reportFile{passingFile}, "doc"),
			"xls": {},
		},
		Assessment: map[string]formatAssessment{
			"doc": {QuantitativeGate: "pass"},
			"xls": {QuantitativeGate: "pass"},
		},
	}
	audit := auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "summary_format_not_declared:xls") || !containsReason(audit.Reasons, "assessment_format_not_declared:xls") {
		t.Fatalf("undeclared aggregate keys must block audit: %#v", audit)
	}

	input.Summary = map[string]formatSummary{"doc": summarizeReportFiles([]reportFile{passingFile}, "doc")}
	input.Assessment = map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}}
	input.Files[0].ErrorClass = "extract_error"
	audit = auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_file_metrics") {
		t.Fatalf("failed OfficeRead record cannot claim success: %#v", audit)
	}

	input.Files[0].ErrorClass = "unexpected_error_class"
	input.Summary["doc"] = summarizeReportFiles(input.Files, "doc")
	audit = auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_file_metrics") {
		t.Fatalf("unknown error class must fail audit: %#v", audit)
	}

	input.Files[0] = passingFile
	input.Files[0].OfficeReadOK = false
	input.Files[0].ErrorClass = "extract_error"
	input.Summary["doc"] = summarizeReportFiles(input.Files, "doc")
	audit = auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_file_metrics") {
		t.Fatalf("failed OfficeRead record cannot retain text/token metrics: %#v", audit)
	}

	input.Files[0] = passingFile
	input.Files[0].LegacyOK = false
	input.Files[0].ErrorClass = ""
	input.Summary["doc"] = summarizeReportFiles(input.Files, "doc")
	audit = auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_file_metrics") {
		t.Fatalf("failed legacy record cannot retain text/token metrics: %#v", audit)
	}
}

func TestAuditReportRejectsRejectedContainerMetrics(t *testing.T) {
	file := reportFile{
		SampleID: "sample-0000000000000001", Format: "doc", ErrorClass: "encrypted",
		OfficeReadOK: true, OfficeRunes: 1, OfficeTokens: 1, LegacyOK: true, LegacyRunes: 1, LegacyTokens: 1, SharedTokens: 1,
	}
	input := report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceInternalAuthorized,
		MinimumSamples: 1, MinimumTokenHit: .95, Formats: []string{"doc"}, Files: []reportFile{file},
		Summary:    map[string]formatSummary{"doc": summarizeReportFiles([]reportFile{file}, "doc")},
		Assessment: map[string]formatAssessment{"doc": {QuantitativeGate: "pass"}},
	}
	audit := auditReport(input, []string{"doc"})
	if audit.QuantitativeReady || !containsReason(audit.Reasons, "invalid_file_metrics") {
		t.Fatalf("rejected encrypted container cannot retain parser metrics: %#v", audit)
	}
}

func TestFormatSummaryMetricsRejectsImpossibleCountsAndRates(t *testing.T) {
	for _, summary := range []formatSummary{
		{Total: 1, OfficeReadOK: 2},
		{OfficeTokens: 1, LegacyTokens: 1, SharedTokens: 2},
		{OfficeTokenHit: .1},
		{LegacyTokenHit: .1},
	} {
		if formatSummaryMetricsValid(summary) {
			t.Fatalf("impossible summary accepted: %#v", summary)
		}
	}
}

func TestDecodeReportAcceptsLeadingUTF8BOM(t *testing.T) {
	encoded, err := json.Marshal(report{
		SchemaVersion: reportSchemaVersion,
		Engine:        "dual",
		Formats:       []string{"doc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeReport(append([]byte{0xef, 0xbb, 0xbf}, encoded...))
	if err != nil {
		t.Fatalf("decode report with UTF-8 BOM: %v", err)
	}
	if decoded.SchemaVersion != reportSchemaVersion || decoded.Engine != "dual" || len(decoded.Formats) != 1 || decoded.Formats[0] != "doc" {
		t.Fatalf("decoded report = %#v", decoded)
	}
}

func TestDualReportDigestIgnoresOnlyAcceptedLeadingUTF8BOM(t *testing.T) {
	reportBytes := []byte(`{"schema_version":"1.0","engine":"dual","formats":["doc"]}`)
	plain := dualReportDigest(reportBytes)
	withBOM := dualReportDigest(append([]byte{0xef, 0xbb, 0xbf}, reportBytes...))
	if plain != withBOM {
		t.Fatalf("leading UTF-8 BOM changed report digest: %x != %x", plain, withBOM)
	}
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	evidence := newReleaseEvidenceTemplate(fmt.Sprintf("%x", plain[:]), []string{"doc"})
	evidence.ReviewerID = "release.owner@example.com"
	evidence.CreatedAt = now.Format(time.RFC3339)
	for i := range evidence.ManualReviews {
		evidence.ManualReviews[i].Status = "completed"
		evidence.ManualReviews[i].ReviewedAt = now.Format(time.RFC3339)
	}
	if reasons := validateReleaseEvidence(evidence, fmt.Sprintf("%x", withBOM[:]), []string{"doc"}, now.Format(time.RFC3339), 0, now); len(reasons) != 0 {
		t.Fatalf("BOM-equivalent report must retain evidence binding: %#v", reasons)
	}
	withTrailingNewline := dualReportDigest(append(append([]byte(nil), reportBytes...), '\n'))
	if plain == withTrailingNewline {
		t.Fatalf("non-BOM report-byte change must require a new evidence binding: %x", plain)
	}
}

func TestDecodeReportRejectsUnknownDuplicateAndTrailingJSON(t *testing.T) {
	for _, encoded := range [][]byte{
		[]byte(`{"schema_version":"1.0","engine":"dual","formats":["doc"],"source_path":"C:\\private\\customer.doc"}`),
		[]byte(`{"schema_version":"1.0","engine":"dual","engine":"officeread","formats":["doc"]}`),
		[]byte(`{"schema_version":"1.0","engine":"dual","formats":["doc"],"summary":{"doc":{"total":1,"total":2}}}`),
		[]byte(`{"schema_version":"1.0","engine":"dual","formats":["doc"]} {}`),
	} {
		if _, err := decodeReport(encoded); err == nil {
			t.Fatalf("invalid audit JSON accepted: %s", encoded)
		}
	}
}

func TestDecodeReportRejectsExcessiveJSONNesting(t *testing.T) {
	encoded := strings.Repeat("[", maxAuditJSONDepth+1) + "0" + strings.Repeat("]", maxAuditJSONDepth+1)
	if _, err := decodeReport([]byte(encoded)); err == nil || !strings.Contains(err.Error(), "maximum JSON nesting depth") {
		t.Fatalf("deep audit JSON error = %v", err)
	}
}

func TestReadAuditReportRejectsOversizedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized-report.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(maxAuditReportBytes, io.SeekStart); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{'x'}); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readAuditReport(path); err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("oversized audit report error = %v", err)
	}
}

func TestReadAuditReportAcceptsInputAtSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "at-limit-report.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(maxAuditReportBytes-1, io.SeekStart); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{'x'}); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := readAuditReport(path)
	if err != nil {
		t.Fatalf("read audit report at limit: %v", err)
	}
	if int64(len(data)) != maxAuditReportBytes {
		t.Fatalf("read audit report length = %d, want %d", len(data), maxAuditReportBytes)
	}
}

func TestParseRequiredFormatsNormalizesButRejectsScopeTyposAndDuplicates(t *testing.T) {
	got, err := parseRequiredFormats(".XLS, doc")
	if err != nil || !reflect.DeepEqual(got, []string{"doc", "xls"}) {
		t.Fatalf("formats = %#v, err = %v", got, err)
	}
	for _, raw := range []string{"pdf", "doc,pdf", "doc,doc", ",doc", "doc,"} {
		if _, err := parseRequiredFormats(raw); err == nil {
			t.Fatalf("parseRequiredFormats(%q) unexpectedly accepted", raw)
		}
	}
}

func TestBuildReportFromInputsClassifiesHistoricalFixtureMetrics(t *testing.T) {
	result := buildReportFromInputs(report{
		SchemaVersion: reportSchemaVersion, Engine: "dual", SampleProvenance: provenanceFixture,
		MinimumSamples: 10, MinimumTokenHit: .95, Formats: []string{"doc", "ppt", "xls"},
		Summary: make(map[string]formatSummary), Assessment: make(map[string]formatAssessment),
	}, []reportInput{
		{SampleID: "sample-doc-a", Format: "doc", Observation: agent.OfficeReadObservation{OfficeReadOK: true, OfficeReadTokens: 1119}},
		{SampleID: "sample-doc-b", Format: "doc", Observation: agent.OfficeReadObservation{OfficeReadOK: true, OfficeReadTokens: 18940}},
		{SampleID: "sample-ppt-a", Format: "ppt", Observation: agent.OfficeReadObservation{OfficeReadOK: true, OfficeReadTokens: 78}},
		{SampleID: "sample-ppt-b", Format: "ppt", Observation: agent.OfficeReadObservation{OfficeReadOK: true, OfficeReadTokens: 1098}},
		{SampleID: "sample-xls-a", Format: "xls", Observation: agent.OfficeReadObservation{OfficeReadOK: true, OfficeReadTokens: 388}},
		{SampleID: "sample-xls-b", Format: "xls", Observation: agent.OfficeReadObservation{OfficeReadOK: true, OfficeReadTokens: 2340, LegacyOK: true, LegacyTokens: 3902, SharedTokens: 2336}},
	})
	if got := result.Assessment["doc"].QuantitativeGate; got != "insufficient_evidence" {
		t.Fatalf("doc fixture gate = %q", got)
	}
	if got := result.Summary["xls"].OfficeTokenHit; got < .598 || got > .599 {
		t.Fatalf("xls OfficeRead coverage = %f, want about .599", got)
	}
	if got := result.Assessment["xls"].QuantitativeGate; got != "fail" {
		t.Fatalf("xls fixture gate = %q", got)
	}
}

func writeReportDOCX(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`)); err != nil {
		_ = zw.Close()
		_ = f.Close()
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredFormatsNormalizesAndDeduplicatesValidScope(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", ".doc, XLS,doc,pptx")
	got := configuredFormats()
	want := []string{"doc", "pptx", "xls"}
	if len(got) != len(want) {
		t.Fatalf("configuredFormats() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("configuredFormats() = %#v, want %#v", got, want)
		}
	}
}

func TestConfiguredFormatsRejectsMalformedNonEmptyScope(t *testing.T) {
	for _, raw := range []string{"doc,pdf", "doc,", ",doc"} {
		t.Setenv("MACLAW_OFFICE_READ_FORMATS", raw)
		if got := configuredFormats(); got != nil {
			t.Fatalf("configuredFormats(%q) = %#v, want nil", raw, got)
		}
	}
}

func TestConfiguredFormatsDefaultsToAllSupportedFormats(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "")
	got := configuredFormats()
	want := []string{"doc", "docx", "ppt", "pptx", "xls", "xlsx"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configuredFormats() = %#v, want %#v", got, want)
	}
}

func containsReason(reasons []string, wanted string) bool {
	for _, reason := range reasons {
		if reason == wanted {
			return true
		}
	}
	return false
}
