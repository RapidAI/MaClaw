// office-read-dual-report produces a privacy-preserving local migration report
// for OfficeRead. It intentionally reports only opaque sample IDs and
// aggregate metrics; no source text, image data, file names, full paths, or
// parser error strings are written to the output.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const reportSchemaVersion = "1.0"

// releaseEvidenceSchemaVersion describes a content-free, human-authored
// attestation that is bound to one dual-read report. It deliberately does not
// turn a manual assertion into an automatic release approval.
const releaseEvidenceSchemaVersion = "1.0"

// maxAuditReportBytes keeps a hand-edited or accidentally expanded audit
// artifact from becoming an unbounded CI/GUI memory read. Reports contain
// only per-sample counters, never document content, so 32 MiB leaves ample
// headroom for large batches while matching the document input boundary.
const maxAuditReportBytes int64 = 32 << 20

// maxReleaseEvidenceBytes is intentionally much smaller than a dual-read
// report. A release attestation contains only a reviewer ID, timestamps,
// canonical requirement IDs, and formats; it must never carry source paths,
// document text, passwords, screenshots, or free-form notes.
const maxReleaseEvidenceBytes int64 = 256 << 10

// maxAuditJSONDepth bounds the structural walk performed before report
// decoding. Audit reports contain a fixed, shallow schema; deeper input can
// only be malformed or adversarial and must not consume an unbounded call
// stack in the duplicate-key validator.
const maxAuditJSONDepth = 128

// maxAuditFutureSkew allows for minor clock skew between the report producer
// and the CI host that invokes -audit. Historical, non-release fixture reports
// intentionally remain auditable without an expiry. A release pipeline that
// wants a freshness rule must opt in with -max-authorized-report-age; that
// policy is then applied only to internal_authorized evidence.
const maxAuditFutureSkew = 5 * time.Minute

const (
	provenanceInternalAuthorized = "internal_authorized"
	provenanceFixture            = "fixture"
	provenanceUnknown            = "unknown"
)

// manualReviewRequirements is retained as an internal identifier to preserve
// the release-evidence schema, but its requirements are now fulfilled by
// deterministic automated validation rather than a human attestation. Keep
// them in the binary so an edited report cannot reduce the acceptance scope.
var manualReviewRequirements = []string{
	"automated_text_order_and_semantic_regression",
	"automated_resource_failure_and_pagination_regression",
	"automated_gui_attachment_tool_image_contract",
	"automated_format_level_rollback_regression",
}

type reportFile struct {
	// SampleID is an opaque, per-report identifier. Keeping source basenames
	// out of a migration artifact avoids disclosing client/project names. Its
	// process-local salt prevents correlation across separately generated reports.
	SampleID      string `json:"sample_id"`
	Format        string `json:"format"`
	SourceBytes   int64  `json:"source_bytes"`
	OfficeReadOK  bool   `json:"office_read_ok"`
	OfficeRunes   int    `json:"office_runes"`
	OfficeTokens  int    `json:"office_tokens"`
	LegacyOK      bool   `json:"legacy_ok"`
	LegacyRunes   int    `json:"legacy_runes"`
	LegacyTokens  int    `json:"legacy_tokens"`
	SharedTokens  int    `json:"shared_tokens"`
	ElapsedMillis int64  `json:"elapsed_ms"`
	ErrorClass    string `json:"error_class,omitempty"`
}

// reportInput is the minimal, already-sanitized observation required to
// construct a report. It keeps report aggregation deterministic and permits
// the historical fixture artifact to be migrated without re-reading source
// documents or retaining their paths.
type reportInput struct {
	SampleID    string
	Format      string
	Observation agent.OfficeReadObservation
}

type formatSummary struct {
	Total          int     `json:"total"`
	OfficeReadOK   int     `json:"office_read_ok"`
	LegacyOK       int     `json:"legacy_ok"`
	OfficeTokens   int     `json:"office_tokens"`
	LegacyTokens   int     `json:"legacy_tokens"`
	SharedTokens   int     `json:"shared_tokens"`
	OfficeTokenHit float64 `json:"office_token_hit_rate,omitempty"`
	LegacyTokenHit float64 `json:"legacy_token_hit_rate,omitempty"`
	MaxMillis      int64   `json:"max_elapsed_ms"`
}

// formatAssessment records the deterministic parser evidence. Full automated
// acceptance additionally runs the cross-layer contracts in
// scripts/test-officeread-acceptance.ps1.
type formatAssessment struct {
	QuantitativeGate string   `json:"quantitative_gate"`
	Reasons          []string `json:"reasons"`
}

type report struct {
	SchemaVersion        string                      `json:"schema_version"`
	GeneratedAt          string                      `json:"generated_at"`
	Engine               string                      `json:"engine"`
	SampleProvenance     string                      `json:"sample_provenance"`
	MinimumSamples       int                         `json:"minimum_samples_per_format"`
	MinimumTokenHit      float64                     `json:"minimum_officeread_token_hit_rate"`
	Formats              []string                    `json:"formats"`
	Summary              map[string]formatSummary    `json:"summary"`
	Assessment           map[string]formatAssessment `json:"assessment"`
	ManualReviewRequired []string                    `json:"manual_review_required"`
	Files                []reportFile                `json:"files"`
}

type reportOptions struct {
	Provenance      string
	MinimumSamples  int
	MinimumTokenHit float64
	Formats         []string
	Salt            []byte
}

// effectiveReportPolicy is the exact runtime policy under which dual samples
// are collected. This standalone CLI has no GUI configuration provider, so
// its environment is the complete active policy. A report that names formats
// the extractor did not actually dual-read is not promotion evidence.
type effectiveReportPolicy struct {
	Engine  string
	Formats []string
}

// reportAudit is a content-free acceptance summary. QuantitativeReady is
// deliberately reserved for internal_authorized production evidence. Fixture
// evidence has a separately named readiness bit so a successful regression
// run cannot be mistaken for a production promotion decision.
type reportAudit struct {
	SchemaVersion              string                    `json:"schema_version"`
	QuantitativeReady          bool                      `json:"quantitative_ready"`
	Formats                    []string                  `json:"formats"`
	Reasons                    []string                  `json:"reasons"`
	ManualReviewRequired       []string                  `json:"manual_review_required"`
	RequiredMinimumSamples     int                       `json:"required_minimum_samples_per_format,omitempty"`
	RequiredMinimumTokenHit    float64                   `json:"required_minimum_officeread_token_hit_rate,omitempty"`
	MaximumAuthorizedReportAge string                    `json:"maximum_authorized_report_age,omitempty"`
	ManualReviewEvidence       manualReviewEvidenceAudit `json:"manual_review_evidence"`
	FixtureAutomationAccepted  bool                      `json:"fixture_automation_accepted,omitempty"`
	FixtureAutomationReady     bool                      `json:"fixture_automation_ready,omitempty"`
}

// manualReviewEvidenceAudit retains the legacy, optional attestation schema
// for compatibility with existing release artifacts. Automated acceptance does
// not depend on it; its evidence is the deterministic test suite.
type manualReviewEvidenceAudit struct {
	Provided             bool     `json:"provided"`
	StructurallyComplete bool     `json:"structurally_complete"`
	Reasons              []string `json:"reasons"`
}

// releaseEvidence is a legacy, content-free record retained so historical
// release artifacts remain decodable. New OfficeRead acceptance is automated.
type releaseEvidence struct {
	SchemaVersion    string                    `json:"schema_version"`
	CreatedAt        string                    `json:"created_at"`
	DualReportSHA256 string                    `json:"dual_report_sha256"`
	ReviewerID       string                    `json:"reviewer_id"`
	Formats          []string                  `json:"formats"`
	ManualReviews    []manualReviewAttestation `json:"manual_reviews"`
}

type manualReviewAttestation struct {
	Requirement string   `json:"requirement"`
	Status      string   `json:"status"`
	ReviewedAt  string   `json:"reviewed_at"`
	Formats     []string `json:"formats"`
}

// auditOptions contains invocation-specific release policy. It is deliberately
// separate from the serialized report: an operator cannot weaken the current
// pipeline's freshness requirement by editing an old evidence artifact.
type auditOptions struct {
	RequiredFormats            []string
	MinimumSamples             int
	MinimumTokenHit            float64
	Now                        time.Time
	MaximumAuthorizedReportAge time.Duration
	AllowFixtureAutomation     bool
}

func main() {
	var inputs stringList
	var patterns stringList
	outPath := flag.String("out", "", "write JSON report to this path (default stdout)")
	auditPath := flag.String("audit", "", "audit an existing dual-read JSON report instead of extracting files")
	enforceAudit := flag.Bool("enforce-audit", false, "exit non-zero when -audit does not meet its selected audit profile")
	releaseEvidencePath := flag.String("release-evidence", "", "content-free manual-review attestation to validate with -audit")
	releaseEvidenceTemplatePath := flag.String("write-release-evidence-template", "", "write a pending manual-review attestation template (requires -audit)")
	enforceReleaseEvidence := flag.Bool("enforce-release-evidence", false, "exit non-zero unless -audit passes quantitatively and its manual-review attestation is structurally complete")
	allowFixtureAutomation := flag.Bool("allow-fixture-automation", false, "select the fixture-only automated regression profile; quantitative_ready remains reserved for internal_authorized release evidence")
	requiredFormats := flag.String("required-formats", "", "comma-separated formats required by -audit (default: report formats)")
	maximumAuthorizedReportAge := flag.Duration("max-authorized-report-age", 0, "maximum age for internal_authorized reports during -audit (0 disables the freshness check)")
	provenance := flag.String("provenance", provenanceUnknown, "sample provenance: internal_authorized, fixture, or unknown")
	minimumSamples := flag.Int("min-samples", 10, "minimum authorized samples required per format for a quantitative pass (also enforced by -audit)")
	minimumTokenHit := flag.Float64("min-token-hit", 0.95, "minimum OfficeRead shared-token coverage against legacy for a quantitative pass (also enforced by -audit)")
	flag.Var(&inputs, "input", "Office file to sample (repeatable)")
	flag.Var(&patterns, "glob", "glob pattern for Office files (repeatable)")
	flag.Parse()
	if *maximumAuthorizedReportAge < 0 {
		fatalf("-max-authorized-report-age must not be negative")
	}
	if strings.TrimSpace(*auditPath) != "" {
		data, err := readAuditReport(*auditPath)
		if err != nil {
			fatalf("read audit report: %v", err)
		}
		input, err := decodeReport(data)
		if err != nil {
			fatalf("decode audit report: %v", err)
		}
		requestedFormats := strings.TrimSpace(*requiredFormats)
		var auditRequiredFormats []string
		if requestedFormats == "" {
			// Preserve the serialized labels as untrusted audit input. The audit
			// below must see invalid or duplicate report formats rather than a
			// pre-normalized copy that could conceal a malformed report scope.
			required := append([]string(nil), input.Formats...)
			auditRequiredFormats = required
		} else {
			required, err := parseRequiredFormats(requestedFormats)
			if err != nil {
				fatalf("invalid -required-formats: %v", err)
			}
			auditRequiredFormats = required
		}
		if *minimumSamples < 1 {
			fatalf("-min-samples must be at least 1")
		}
		if !isFiniteReportRate(*minimumTokenHit) || *minimumTokenHit < 0 || *minimumTokenHit > 1 {
			fatalf("-min-token-hit must be between 0 and 1")
		}
		// Use one audit instant for quantitative freshness and any attached
		// manual evidence. Taking time twice can make a boundary-value artifact
		// receive contradictory outcomes around the allowed clock skew or age
		// threshold.
		now := time.Now().UTC()
		audit := auditReportWithOptions(input, auditOptions{
			RequiredFormats:            auditRequiredFormats,
			MinimumSamples:             *minimumSamples,
			MinimumTokenHit:            *minimumTokenHit,
			Now:                        now,
			MaximumAuthorizedReportAge: *maximumAuthorizedReportAge,
			AllowFixtureAutomation:     *allowFixtureAutomation,
		})
		reportDigest := dualReportDigest(data)
		if strings.TrimSpace(*releaseEvidenceTemplatePath) != "" {
			template := newReleaseEvidenceTemplate(fmt.Sprintf("%x", reportDigest[:]), audit.Formats)
			if err := writeReleaseEvidenceTemplate(*releaseEvidenceTemplatePath, template); err != nil {
				fatalf("write release-evidence template: %v", err)
			}
		}
		audit.ManualReviewEvidence = auditManualReviewEvidence(*releaseEvidencePath, fmt.Sprintf("%x", reportDigest[:]), audit.Formats, input.GeneratedAt, *maximumAuthorizedReportAge, now)
		encoded, err := json.MarshalIndent(audit, "", "  ")
		if err != nil {
			fatalf("encode audit: %v", err)
		}
		_, _ = os.Stdout.Write(append(encoded, '\n'))
		if *enforceAudit && !auditMeetsSelectedProfile(audit, *allowFixtureAutomation) {
			os.Exit(1)
		}
		if *enforceReleaseEvidence && (!audit.QuantitativeReady || !audit.ManualReviewEvidence.StructurallyComplete) {
			os.Exit(1)
		}
		return
	}
	if *maximumAuthorizedReportAge != 0 {
		fatalf("-max-authorized-report-age is only valid with -audit")
	}
	if *allowFixtureAutomation {
		fatalf("-allow-fixture-automation is only valid with -audit")
	}
	if strings.TrimSpace(*releaseEvidencePath) != "" || strings.TrimSpace(*releaseEvidenceTemplatePath) != "" || *enforceReleaseEvidence {
		fatalf("-release-evidence, -write-release-evidence-template, and -enforce-release-evidence are only valid with -audit")
	}

	paths := append([]string(nil), inputs...)
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			fatalf("invalid glob %q: %v", pattern, err)
		}
		paths = append(paths, matches...)
	}
	paths = uniqueOfficePaths(paths)
	if len(paths) == 0 {
		fatalf("provide at least one -input or -glob")
	}
	if !validProvenance(*provenance) {
		fatalf("invalid -provenance %q (want internal_authorized, fixture, or unknown)", *provenance)
	}
	if *minimumSamples < 1 {
		fatalf("-min-samples must be at least 1")
	}
	if !isFiniteReportRate(*minimumTokenHit) || *minimumTokenHit < 0 || *minimumTokenHit > 1 {
		fatalf("-min-token-hit must be between 0 and 1")
	}
	reportSalt, err := randomReportSalt()
	if err != nil {
		fatalf("initialize opaque sample identifiers: %v", err)
	}

	result, err := buildReport(paths, reportOptions{
		Provenance:      strings.ToLower(strings.TrimSpace(*provenance)),
		MinimumSamples:  *minimumSamples,
		MinimumTokenHit: *minimumTokenHit,
		Formats:         configuredFormats(),
		Salt:            reportSalt,
	})
	if err != nil {
		fatalf("collect dual-read report: %v", err)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatalf("encode report: %v", err)
	}
	if strings.TrimSpace(*outPath) == "" {
		_, _ = os.Stdout.Write(append(data, '\n'))
		return
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fatalf("create report directory: %v", err)
	}
	if err := os.WriteFile(*outPath, append(data, '\n'), 0o600); err != nil {
		fatalf("write report: %v", err)
	}
}

// buildReport executes the shadow reads and returns only the serializable,
// privacy-preserving artifact. Keeping it separate from flag parsing lets the
// release gate be tested with real tiny Office fixtures without invoking
// os.Exit or writing a report to disk.
func buildReport(paths []string, options reportOptions) (report, error) {
	policy := currentReportPolicy()
	if policy.Engine != "dual" {
		return report{}, fmt.Errorf("effective OfficeRead engine must be dual before running this report")
	}
	if !validProvenance(options.Provenance) {
		return report{}, fmt.Errorf("invalid sample provenance %q", options.Provenance)
	}
	if options.MinimumSamples < 1 {
		return report{}, errors.New("minimum samples must be at least 1")
	}
	if !isFiniteReportRate(options.MinimumTokenHit) || options.MinimumTokenHit < 0 || options.MinimumTokenHit > 1 {
		return report{}, errors.New("minimum token hit must be between 0 and 1")
	}
	if len(options.Salt) == 0 {
		return report{}, errors.New("opaque sample ID salt is required")
	}
	formats, err := canonicalReportFormats(options.Formats)
	if err != nil {
		return report{}, err
	}
	if !sameFormatSet(formats, policy.Formats) {
		return report{}, fmt.Errorf("report formats %s do not match effective dual policy %s", strings.Join(formats, ","), strings.Join(policy.Formats, ","))
	}
	// main already canonicalizes -input/-glob results, but buildReport is also
	// used directly by tests and future in-process callers. Keep the evidence
	// boundary here so the same physical file cannot be supplied under repeated
	// paths, symlinks, or hard links to inflate a quantitative sample threshold.
	paths = uniqueOfficePaths(paths)
	// A report is evidence for exactly the active dual-read rollout scope. Do
	// not retain a disabled extension as a `not_dual_enabled` record: doing so
	// would serialize a report that its own audit must reject because Files
	// contains evidence outside Formats. Reject before opening the source so a
	// broad glob cannot accidentally produce self-contradictory promotion data.
	for _, path := range paths {
		format := normalizeFormat(filepath.Ext(path))
		if !formatSetContains(formats, format) {
			return report{}, fmt.Errorf("input format %q is outside effective dual policy", format)
		}
	}

	result := report{
		SchemaVersion:        reportSchemaVersion,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		Engine:               "dual",
		SampleProvenance:     options.Provenance,
		MinimumSamples:       options.MinimumSamples,
		MinimumTokenHit:      options.MinimumTokenHit,
		Formats:              formats,
		Summary:              make(map[string]formatSummary),
		Assessment:           make(map[string]formatAssessment),
		ManualReviewRequired: append([]string(nil), manualReviewRequirements...),
	}
	var observations []agent.OfficeReadObservation
	restore := agent.SetOfficeReadObservationHandler(func(obs agent.OfficeReadObservation) {
		observations = append(observations, obs)
	})
	defer restore()

	inputs := make([]reportInput, 0, len(paths))
	for _, path := range paths {
		format := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		before := len(observations)
		_, resolvedFormat, _ := agent.ExtractOfficeText(path)
		if normalizeFormat(resolvedFormat) != "" && normalizeFormat(resolvedFormat) != format {
			// A content signature resolved this input as another format. Never
			// count either parser's metrics toward the filename extension's
			// release gate, even when the actual format happens to be enabled.
			inputs = append(inputs, reportInput{
				SampleID: opaqueSampleID(path, options.Salt),
				Format:   format,
				Observation: agent.OfficeReadObservation{
					Format:      format,
					SourceBytes: reportSourceBytes(path),
					ErrorClass:  "format_mismatch",
				},
			})
			continue
		}
		if len(observations) == before {
			// A format disabled by the allowlist is not a dual-read result. Keep
			// this visible without recording a potentially sensitive error. The
			// public router can reject an oversized source before its dual-mode
			// observer is installed, however; preserve that distinct safety result
			// so it cannot be mistaken for an allowlist configuration gap.
			sourceBytes := reportSourceBytes(path)
			errorClass := "not_dual_enabled"
			if sourceBytes > maxAuditReportBytes {
				errorClass = "input_too_large"
			}
			inputs = append(inputs, reportInput{SampleID: opaqueSampleID(path, options.Salt), Format: format, Observation: agent.OfficeReadObservation{Format: format, SourceBytes: sourceBytes, ErrorClass: errorClass}})
			continue
		}
		obs := reportObservationForSampleFormat(format, observations[len(observations)-1])
		inputs = append(inputs, reportInput{SampleID: opaqueSampleID(path, options.Salt), Format: obs.Format, Observation: obs})
	}
	return buildReportFromInputs(result, inputs), nil
}

func currentReportPolicy() effectiveReportPolicy {
	policy := agent.CurrentOfficeReadRuntimePolicy()
	return effectiveReportPolicy{
		Engine:  string(policy.Engine),
		Formats: append([]string(nil), policy.Formats...),
	}
}

func sameFormatSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func formatSetContains(formats []string, wanted string) bool {
	for _, format := range formats {
		if format == wanted {
			return true
		}
	}
	return false
}

func normalizeFormat(format string) string {
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	if !isOfficeFormat("." + format) {
		return ""
	}
	return format
}

// canonicalReportFormats makes the report's declared rollout scope stable
// before it is serialized. The CLI already supplies the current allowlist,
// but keeping this check at the builder boundary prevents a future caller from
// producing a report whose summary keys and declared scope disagree.
func canonicalReportFormats(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	formats := make([]string, 0, len(values))
	for _, value := range values {
		format := normalizeFormat(value)
		if format == "" {
			return nil, fmt.Errorf("unsupported report format %q", value)
		}
		if _, duplicate := seen[format]; duplicate {
			continue
		}
		seen[format] = struct{}{}
		formats = append(formats, format)
	}
	if len(formats) == 0 {
		return nil, errors.New("at least one OfficeRead report format is required")
	}
	sort.Strings(formats)
	return formats, nil
}

func reportSourceBytes(filePath string) int64 {
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		return -1
	}
	return info.Size()
}

// reportObservationForSampleFormat prevents a content-sniffed retry from
// being counted as evidence for the filename extension supplied to the report.
func reportObservationForSampleFormat(format string, observation agent.OfficeReadObservation) agent.OfficeReadObservation {
	format = normalizeFormat(format)
	if observation.Format == format {
		return observation
	}
	return agent.OfficeReadObservation{
		Format:      format,
		SourceBytes: observation.SourceBytes,
		Elapsed:     observation.Elapsed,
		ErrorClass:  "format_mismatch",
	}
}

func buildReportFromInputs(result report, inputs []reportInput) report {
	for _, input := range inputs {
		obs := input.Observation
		result.Files = append(result.Files, reportFile{
			SampleID: input.SampleID, Format: input.Format, SourceBytes: obs.SourceBytes,
			OfficeReadOK: obs.OfficeReadOK, OfficeRunes: obs.OfficeReadSize, OfficeTokens: obs.OfficeReadTokens,
			LegacyOK: obs.LegacyOK, LegacyRunes: obs.LegacySize, LegacyTokens: obs.LegacyTokens,
			SharedTokens: obs.SharedTokens, ElapsedMillis: obs.Elapsed.Milliseconds(), ErrorClass: obs.ErrorClass,
		})
	}
	// A report must cover every explicitly enabled format. Otherwise an
	// operator could accidentally approve an allowlist member that was never
	// sampled. Record a zero-value assessment rather than hiding the gap.
	for _, format := range result.Formats {
		if _, ok := result.Summary[format]; !ok {
			result.Summary[format] = formatSummary{}
		}
	}

	for _, item := range result.Files {
		if !reportFileCountsTowardGate(item) {
			continue
		}
		summary := result.Summary[item.Format]
		summary.Total++
		if item.OfficeReadOK {
			summary.OfficeReadOK++
		}
		if item.LegacyOK {
			summary.LegacyOK++
		}
		summary.OfficeTokens += item.OfficeTokens
		summary.LegacyTokens += item.LegacyTokens
		summary.SharedTokens += item.SharedTokens
		if item.ElapsedMillis > summary.MaxMillis {
			summary.MaxMillis = item.ElapsedMillis
		}
		result.Summary[item.Format] = summary
	}
	for format, summary := range result.Summary {
		if summary.LegacyTokens > 0 {
			summary.OfficeTokenHit = float64(summary.SharedTokens) / float64(summary.LegacyTokens)
		}
		if summary.OfficeTokens > 0 {
			summary.LegacyTokenHit = float64(summary.SharedTokens) / float64(summary.OfficeTokens)
		}
		result.Summary[format] = summary
		result.Assessment[format] = assessFormat(summary, result.SampleProvenance, result.MinimumSamples, result.MinimumTokenHit)
	}
	return result
}

// reportFileCountsTowardGate excludes records that did not produce a usable
// OfficeRead-vs-legacy comparison. They remain in Files for operator and
// security diagnostics, but cannot fill a quantitative sample threshold.
// In particular, size and container-safety rejections happen before the
// OfficeRead shadow parser is allowed to run; counting them would let a batch
// of intentionally rejected files inflate the denominator for one otherwise
// comparable sample.
func reportFileCountsTowardGate(item reportFile) bool {
	switch item.ErrorClass {
	case "format_mismatch", "not_dual_enabled", "input_too_large", "encrypted", "malformed":
		return false
	default:
		return true
	}
}

func auditReport(input report, requiredFormats []string) reportAudit {
	return auditReportWithOptions(input, auditOptions{
		RequiredFormats: requiredFormats,
		MinimumSamples:  input.MinimumSamples,
		MinimumTokenHit: input.MinimumTokenHit,
		Now:             time.Now().UTC(),
	})
}

func auditReportWithOptions(input report, options auditOptions) reportAudit {
	requiredFormats := options.RequiredFormats
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	audit := reportAudit{
		SchemaVersion: reportSchemaVersion,
		Formats:       append([]string(nil), requiredFormats...),
		// Reconstruct the non-mechanical release work from this binary. The
		// input field remains useful as an artifact record, but must not decide
		// what downstream auditors are told to review.
		ManualReviewRequired: append([]string(nil), manualReviewRequirements...),
	}
	minimumSamples := input.MinimumSamples
	minimumTokenHit := input.MinimumTokenHit
	if options.MinimumSamples != 0 {
		// An audit invocation with a sample threshold also supplies the current
		// token threshold (which may legitimately be zero). Direct callers that
		// omit both retain the report's declared criteria for compatibility.
		minimumSamples = options.MinimumSamples
		minimumTokenHit = options.MinimumTokenHit
	}
	audit.RequiredMinimumSamples = minimumSamples
	audit.RequiredMinimumTokenHit = minimumTokenHit
	if options.MaximumAuthorizedReportAge > 0 {
		audit.MaximumAuthorizedReportAge = options.MaximumAuthorizedReportAge.String()
	} else if options.MaximumAuthorizedReportAge < 0 {
		audit.Reasons = append(audit.Reasons, "invalid_maximum_authorized_report_age")
	}
	// Treat the serialized format labels as untrusted input. Normalizing and
	// deduplicating here prevents a hand-edited report from passing one real
	// assessment twice under aliases such as "doc" and ".DOC".
	seenRequiredFormats := make(map[string]struct{}, len(requiredFormats))
	validRequiredFormats := make([]string, 0, len(requiredFormats))
	for _, raw := range requiredFormats {
		rawFormat := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), ".")
		format := normalizeFormat(rawFormat)
		if format == "" {
			audit.Reasons = append(audit.Reasons, "unsupported_required_format:"+rawFormat)
			continue
		}
		if _, duplicate := seenRequiredFormats[format]; duplicate {
			audit.Reasons = append(audit.Reasons, "duplicate_required_format:"+format)
			continue
		}
		seenRequiredFormats[format] = struct{}{}
		validRequiredFormats = append(validRequiredFormats, format)
	}
	audit.Formats = validRequiredFormats
	if input.SchemaVersion != reportSchemaVersion {
		audit.Reasons = append(audit.Reasons, "unsupported_report_schema")
	}
	if input.Engine != "dual" {
		audit.Reasons = append(audit.Reasons, "report_engine_is_not_dual")
	}
	generatedAtValid := reportGeneratedAtValid(input.GeneratedAt, now)
	if !generatedAtValid {
		audit.Reasons = append(audit.Reasons, "invalid_generated_at")
	} else if input.SampleProvenance == provenanceInternalAuthorized && options.MaximumAuthorizedReportAge > 0 && !reportGeneratedAtWithinAge(input.GeneratedAt, now, options.MaximumAuthorizedReportAge) {
		audit.Reasons = append(audit.Reasons, "authorized_report_exceeds_max_age")
	}
	fixtureAutomation := options.AllowFixtureAutomation && input.SampleProvenance == provenanceFixture
	if input.SampleProvenance != provenanceInternalAuthorized && !fixtureAutomation {
		audit.Reasons = append(audit.Reasons, "sample_provenance_is_not_internal_authorized")
	}
	audit.FixtureAutomationAccepted = fixtureAutomation
	if input.MinimumSamples < 1 {
		audit.Reasons = append(audit.Reasons, "invalid_minimum_samples")
	}
	if !isFiniteReportRate(input.MinimumTokenHit) || input.MinimumTokenHit < 0 || input.MinimumTokenHit > 1 {
		audit.Reasons = append(audit.Reasons, "invalid_minimum_token_hit")
	}
	if minimumSamples < 1 {
		audit.Reasons = append(audit.Reasons, "invalid_required_minimum_samples")
	}
	if !isFiniteReportRate(minimumTokenHit) || minimumTokenHit < 0 || minimumTokenHit > 1 {
		audit.Reasons = append(audit.Reasons, "invalid_required_minimum_token_hit")
	}
	if input.MinimumSamples < minimumSamples {
		audit.Reasons = append(audit.Reasons, "report_minimum_samples_below_required")
	}
	if input.MinimumTokenHit < minimumTokenHit {
		audit.Reasons = append(audit.Reasons, "report_minimum_token_hit_below_required")
	}
	declaredFormats := make(map[string]struct{}, len(input.Formats))
	for _, format := range input.Formats {
		normalized := normalizeFormat(format)
		if normalized == "" {
			audit.Reasons = append(audit.Reasons, "unsupported_declared_format:"+format)
			continue
		}
		if normalized != format {
			audit.Reasons = append(audit.Reasons, "invalid_declared_format")
			continue
		}
		if _, duplicate := declaredFormats[normalized]; duplicate {
			audit.Reasons = append(audit.Reasons, "duplicate_declared_format:"+normalized)
			continue
		}
		declaredFormats[normalized] = struct{}{}
	}
	for format := range input.Summary {
		if normalizeFormat(format) == "" || normalizeFormat(format) != format {
			audit.Reasons = append(audit.Reasons, "invalid_summary_format")
			continue
		}
		if _, declared := declaredFormats[format]; !declared {
			audit.Reasons = append(audit.Reasons, "summary_format_not_declared:"+format)
		}
	}
	for format := range input.Assessment {
		if normalizeFormat(format) == "" || normalizeFormat(format) != format {
			audit.Reasons = append(audit.Reasons, "invalid_assessment_format")
			continue
		}
		if _, declared := declaredFormats[format]; !declared {
			audit.Reasons = append(audit.Reasons, "assessment_format_not_declared:"+format)
		}
	}
	seenSampleIDs := make(map[string]struct{}, len(input.Files))
	for _, file := range input.Files {
		fileFormat := normalizeFormat(file.Format)
		if fileFormat == "" || fileFormat != file.Format {
			audit.Reasons = append(audit.Reasons, "invalid_file_format")
		} else if _, declared := declaredFormats[fileFormat]; !declared {
			// A report may retain files which do not count toward the release
			// gate (for example format mismatch), but they must still belong to
			// the declared rollout scope. Otherwise a hand-edited artifact can
			// contain unaccounted sample evidence.
			audit.Reasons = append(audit.Reasons, "file_format_not_declared:"+fileFormat)
		}
		if strings.TrimSpace(file.SampleID) == "" {
			audit.Reasons = append(audit.Reasons, "empty_sample_id")
			continue
		}
		if _, duplicate := seenSampleIDs[file.SampleID]; duplicate {
			audit.Reasons = append(audit.Reasons, "duplicate_sample_id")
			continue
		}
		seenSampleIDs[file.SampleID] = struct{}{}
		if !reportFileMetricsValid(file) {
			audit.Reasons = append(audit.Reasons, "invalid_file_metrics")
		}
		if !validOpaqueSampleID(file.SampleID) {
			audit.Reasons = append(audit.Reasons, "invalid_sample_id")
		}
	}
	if len(validRequiredFormats) == 0 {
		audit.Reasons = append(audit.Reasons, "no_required_formats")
	}
	for _, format := range validRequiredFormats {
		if _, declared := declaredFormats[format]; !declared {
			audit.Reasons = append(audit.Reasons, "required_format_not_declared:"+format)
		}
		assessment, hasAssessment := input.Assessment[format]
		if !hasAssessment {
			audit.Reasons = append(audit.Reasons, "missing_assessment:"+format)
		}
		summary, hasSummary := input.Summary[format]
		if !hasSummary {
			audit.Reasons = append(audit.Reasons, "missing_summary:"+format)
		}
		recomputed := summarizeReportFiles(input.Files, format)
		if !formatSummaryMetricsValid(recomputed) {
			audit.Reasons = append(audit.Reasons, "invalid_summary_metrics:"+format)
		}
		if hasSummary && !formatSummaryMetricsValid(summary) {
			audit.Reasons = append(audit.Reasons, "invalid_summary_metrics:"+format)
		}
		if hasSummary && summary != recomputed {
			audit.Reasons = append(audit.Reasons, "summary_does_not_match_files:"+format)
		}
		// Never accept a JSON assertion of "pass" on its own. Recompute the
		// mechanical gate from the per-sample counters, then require the
		// serialized summary and assessment to agree. This catches a hand-edited
		// or stale report before a CI pipeline relies on it.
		serializedExpected := assessFormat(recomputed, input.SampleProvenance, input.MinimumSamples, input.MinimumTokenHit)
		if hasAssessment && assessment.QuantitativeGate != serializedExpected.QuantitativeGate {
			audit.Reasons = append(audit.Reasons, "assessment_does_not_match_files:"+format)
		}
		// The serialized assessment remains part of the evidence even when a
		// caller opts into fixture automation. That profile changes only the
		// acceptance calculation below; it must not allow an edited report to
		// remove its provenance or evidence-gap reasons.
		if hasAssessment && !sameReportReasons(assessment.Reasons, serializedExpected.Reasons) {
			audit.Reasons = append(audit.Reasons, "assessment_reasons_do_not_match_files:"+format)
		}
		expected := serializedExpected
		if fixtureAutomation {
			expected = assessAutomatedFixtureFormat(recomputed, input.MinimumSamples, input.MinimumTokenHit)
		}
		if expected.QuantitativeGate != "pass" {
			audit.Reasons = append(audit.Reasons, "quantitative_gate_not_pass:"+format)
		}
		if minimumSamples > 0 && minimumTokenHit >= 0 && isFiniteReportRate(minimumTokenHit) {
			requiredAssessment := assessFormat(recomputed, input.SampleProvenance, minimumSamples, minimumTokenHit)
			if fixtureAutomation {
				requiredAssessment = assessAutomatedFixtureFormat(recomputed, minimumSamples, minimumTokenHit)
			}
			if requiredAssessment.QuantitativeGate != "pass" {
				audit.Reasons = append(audit.Reasons, "required_quantitative_gate_not_pass:"+format)
			}
		}
	}
	// Never let the fixture-only profile raise the production readiness bit.
	// Keeping these conclusions disjoint means consumers that only understand
	// quantitative_ready remain fail-closed for upstream sample corpora.
	audit.QuantitativeReady = input.SampleProvenance == provenanceInternalAuthorized && len(audit.Reasons) == 0
	audit.FixtureAutomationReady = fixtureAutomation && len(audit.Reasons) == 0
	return audit
}

// auditMeetsSelectedProfile controls -enforce-audit. A fixture run can pass
// only when the caller explicitly selected its regression profile; a release
// evidence attestation continues to require QuantitativeReady below.
func auditMeetsSelectedProfile(audit reportAudit, allowFixtureAutomation bool) bool {
	if allowFixtureAutomation && audit.FixtureAutomationAccepted {
		return audit.FixtureAutomationReady
	}
	return audit.QuantitativeReady
}

// assessAutomatedFixtureFormat is the non-human acceptance profile used only
// with -allow-fixture-automation. It still recomputes all counters from the
// report files, compares OfficeRead success to a legacy baseline whenever one
// exists, and checks token coverage whenever legacy text exists. PPT has no
// legacy reader, so the deterministic equivalent is that every fixture must
// be extracted successfully rather than failing merely for lack of a baseline.
func assessAutomatedFixtureFormat(summary formatSummary, minimumSamples int, minimumTokenHit float64) formatAssessment {
	reasons := make([]string, 0, 4)
	if !isFiniteReportRate(minimumTokenHit) || minimumTokenHit < 0 || minimumTokenHit > 1 {
		reasons = append(reasons, "invalid_minimum_token_hit")
	}
	if summary.Total < minimumSamples {
		reasons = append(reasons, "insufficient_sample_count")
	}
	if summary.LegacyOK == 0 {
		if summary.OfficeReadOK != summary.Total {
			reasons = append(reasons, "officeread_success_count_below_fixture_total")
		}
	} else if summary.OfficeReadOK < summary.LegacyOK {
		reasons = append(reasons, "officeread_success_count_below_legacy")
	}
	if summary.LegacyTokens > 0 && summary.OfficeTokenHit < minimumTokenHit {
		reasons = append(reasons, "officeread_token_coverage_below_threshold")
	}
	if len(reasons) == 0 {
		return formatAssessment{QuantitativeGate: "pass", Reasons: []string{}}
	}
	for _, reason := range reasons {
		if strings.Contains(reason, "below_") {
			return formatAssessment{QuantitativeGate: "fail", Reasons: reasons}
		}
	}
	return formatAssessment{QuantitativeGate: "insufficient_evidence", Reasons: reasons}
}

// reportGeneratedAtValid keeps a release artifact's audit metadata
// self-contained and comparable across hosts. A zero-offset RFC3339 timestamp
// is required because reports are generated in UTC; future timestamps beyond a
// small clock-skew allowance are not credible evidence.
func reportGeneratedAtValid(raw string, now time.Time) bool {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false
	}
	_, offsetSeconds := parsed.Zone()
	if offsetSeconds != 0 {
		return false
	}
	return !parsed.After(now.UTC().Add(maxAuditFutureSkew))
}

// reportGeneratedAtWithinAge is used only for explicitly opted-in release
// freshness checks. Fixture reports retain their historical audit value, while
// authorized evidence can be required to come from a recent controlled run.
func reportGeneratedAtWithinAge(raw string, now time.Time, maximumAge time.Duration) bool {
	if maximumAge <= 0 {
		return maximumAge == 0
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false
	}
	return !parsed.Before(now.UTC().Add(-maximumAge))
}

func newReleaseEvidenceTemplate(reportDigest string, formats []string) releaseEvidence {
	template := releaseEvidence{
		SchemaVersion: releaseEvidenceSchemaVersion,
		// Leave timestamps blank. The evidence is not valid until the release
		// owner has completed the real external review, so pre-filling the
		// creation time would make a generated template silently go stale.
		CreatedAt:        "",
		DualReportSHA256: reportDigest,
		ReviewerID:       "set-release-owner-id",
		Formats:          append([]string(nil), formats...),
		ManualReviews:    make([]manualReviewAttestation, 0, len(manualReviewRequirements)),
	}
	for _, requirement := range manualReviewRequirements {
		template.ManualReviews = append(template.ManualReviews, manualReviewAttestation{
			Requirement: requirement,
			Status:      "pending",
			Formats:     append([]string(nil), formats...),
		})
	}
	return template
}

func writeReleaseEvidenceTemplate(path string, template releaseEvidence) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("template path is empty")
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("release-evidence template target already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(template, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func auditManualReviewEvidence(path, expectedReportDigest string, requiredFormats []string, reportGeneratedAt string, maximumAge time.Duration, now time.Time) manualReviewEvidenceAudit {
	result := manualReviewEvidenceAudit{Reasons: []string{}}
	if strings.TrimSpace(path) == "" {
		result.Reasons = append(result.Reasons, "missing_release_evidence")
		return result
	}
	data, err := readReleaseEvidence(path)
	if err != nil {
		result.Reasons = append(result.Reasons, "invalid_release_evidence")
		return result
	}
	evidence, err := decodeReleaseEvidence(data)
	if err != nil {
		result.Reasons = append(result.Reasons, "invalid_release_evidence")
		return result
	}
	// "provided" means a schema-valid attestation was actually parsed. A
	// missing, unreadable, oversized, or malformed path must not look like an
	// operator supplied release evidence merely because a CLI flag happened to
	// name it. Keep those states distinct for automated release diagnostics.
	result.Provided = true
	result.Reasons = validateReleaseEvidence(evidence, expectedReportDigest, requiredFormats, reportGeneratedAt, maximumAge, now)
	result.StructurallyComplete = len(result.Reasons) == 0
	return result
}

func readReleaseEvidence(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxReleaseEvidenceBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxReleaseEvidenceBytes {
		return nil, errors.New("release evidence exceeds maximum size")
	}
	return data, nil
}

func decodeReleaseEvidence(data []byte) (releaseEvidence, error) {
	data = trimLeadingUTF8BOM(data)
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return releaseEvidence{}, err
	}
	var evidence releaseEvidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return releaseEvidence{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return releaseEvidence{}, errors.New("release evidence contains multiple JSON values")
		}
		return releaseEvidence{}, err
	}
	return evidence, nil
}

func validateReleaseEvidence(evidence releaseEvidence, expectedReportDigest string, requiredFormats []string, reportGeneratedAt string, maximumAge time.Duration, now time.Time) []string {
	reasons := make([]string, 0, 8)
	if evidence.SchemaVersion != releaseEvidenceSchemaVersion {
		reasons = append(reasons, "unsupported_release_evidence_schema")
	}
	if !releaseEvidenceTimestampValid(evidence.CreatedAt, reportGeneratedAt, maximumAge, now) {
		reasons = append(reasons, "invalid_release_evidence_created_at")
	}
	if evidence.DualReportSHA256 != expectedReportDigest || !validSHA256Hex(evidence.DualReportSHA256) {
		reasons = append(reasons, "release_evidence_report_digest_mismatch")
	}
	if !validReviewerID(evidence.ReviewerID) {
		reasons = append(reasons, "invalid_release_evidence_reviewer_id")
	}
	if !sameCanonicalFormatSet(evidence.Formats, requiredFormats) {
		reasons = append(reasons, "release_evidence_formats_mismatch")
	}
	requiredReviews := make(map[string]struct{}, len(manualReviewRequirements))
	for _, requirement := range manualReviewRequirements {
		requiredReviews[requirement] = struct{}{}
	}
	seenReviews := make(map[string]struct{}, len(evidence.ManualReviews))
	for _, review := range evidence.ManualReviews {
		if _, known := requiredReviews[review.Requirement]; !known {
			reasons = append(reasons, "unknown_manual_review_requirement")
			continue
		}
		if _, duplicate := seenReviews[review.Requirement]; duplicate {
			reasons = append(reasons, "duplicate_manual_review_requirement")
			continue
		}
		seenReviews[review.Requirement] = struct{}{}
		if review.Status != "completed" {
			reasons = append(reasons, "manual_review_not_completed:"+review.Requirement)
		}
		if !releaseEvidenceTimestampValid(review.ReviewedAt, reportGeneratedAt, maximumAge, now) {
			reasons = append(reasons, "invalid_manual_reviewed_at:"+review.Requirement)
		}
		if !manualReviewTimestampWithinEvidence(review.ReviewedAt, evidence.CreatedAt) {
			reasons = append(reasons, "manual_review_after_release_evidence_created_at:"+review.Requirement)
		}
		if !sameCanonicalFormatSet(review.Formats, requiredFormats) {
			reasons = append(reasons, "manual_review_formats_mismatch:"+review.Requirement)
		}
	}
	for _, requirement := range manualReviewRequirements {
		if _, found := seenReviews[requirement]; !found {
			reasons = append(reasons, "missing_manual_review_requirement:"+requirement)
		}
	}
	return reasons
}

// manualReviewTimestampWithinEvidence requires the evidence record to be
// created after all of the reviews it attests. Otherwise a hand-edited
// completion time could make a previously generated release record appear to
// approve work that had not yet occurred.
func manualReviewTimestampWithinEvidence(reviewedAtRaw, evidenceCreatedAtRaw string) bool {
	reviewedAt, err := time.Parse(time.RFC3339, reviewedAtRaw)
	if err != nil {
		return false
	}
	evidenceCreatedAt, err := time.Parse(time.RFC3339, evidenceCreatedAtRaw)
	if err != nil {
		return false
	}
	return !reviewedAt.After(evidenceCreatedAt)
}

// releaseEvidenceTimestampValid prevents an old report from being paired with
// a freshly written manual attestation. When report freshness is configured,
// the same maximum age also bounds each review; without that explicit policy,
// a review must still be no older than the report it claims to approve.
func releaseEvidenceTimestampValid(raw, reportGeneratedAt string, maximumAge time.Duration, now time.Time) bool {
	if !reportGeneratedAtValid(raw, now) {
		return false
	}
	if maximumAge > 0 && !reportGeneratedAtWithinAge(raw, now, maximumAge) {
		return false
	}
	reviewedAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false
	}
	reportAt, err := time.Parse(time.RFC3339, reportGeneratedAt)
	if err != nil {
		return false
	}
	return !reviewedAt.Before(reportAt)
}

func sameCanonicalFormatSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, format := range left {
		if normalizeFormat(format) == "" || normalizeFormat(format) != format {
			return false
		}
		if _, duplicate := seen[format]; duplicate {
			return false
		}
		seen[format] = struct{}{}
	}
	for _, format := range right {
		if _, found := seen[format]; !found {
			return false
		}
	}
	return true
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func validReviewerID(value string) bool {
	if len(value) < 3 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') &&
			!(char >= '0' && char <= '9') && char != '-' && char != '_' && char != '.' && char != '@' {
			return false
		}
	}
	return true
}

// readAuditReport bounds imported JSON before decoding it. The report does not
// carry source document text, so a larger input cannot be legitimate evidence
// for the migration gate; callers receive a content-free error either way.
func readAuditReport(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAuditReportBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxAuditReportBytes {
		return nil, errors.New("audit report exceeds maximum size")
	}
	return data, nil
}

// dualReportDigest binds release evidence to the bytes that the strict report
// decoder actually accepts. Windows PowerShell can prepend a UTF-8 BOM when it
// copies or rewrites JSON; decodeReport explicitly permits that one transport
// marker, so it must not make an otherwise identical report fail its
// attestation binding. No other whitespace or JSON representation is
// canonicalized: every other report-byte change still requires a newly bound
// release-evidence record.
func dualReportDigest(data []byte) [sha256.Size]byte {
	return sha256.Sum256(trimLeadingUTF8BOM(data))
}

func trimLeadingUTF8BOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
}

func sameReportReasons(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// summarizeReportFiles independently reconstructs the metrics used by the
// release gate. This makes -audit robust against an edited summary while
// retaining the report's privacy boundary.
func summarizeReportFiles(files []reportFile, format string) formatSummary {
	var summary formatSummary
	for _, item := range files {
		if item.Format != format || !reportFileCountsTowardGate(item) {
			continue
		}
		summary.Total++
		if item.OfficeReadOK {
			summary.OfficeReadOK++
		}
		if item.LegacyOK {
			summary.LegacyOK++
		}
		summary.OfficeTokens += item.OfficeTokens
		summary.LegacyTokens += item.LegacyTokens
		summary.SharedTokens += item.SharedTokens
		if item.ElapsedMillis > summary.MaxMillis {
			summary.MaxMillis = item.ElapsedMillis
		}
	}
	if summary.LegacyTokens > 0 {
		summary.OfficeTokenHit = float64(summary.SharedTokens) / float64(summary.LegacyTokens)
	}
	if summary.OfficeTokens > 0 {
		summary.LegacyTokenHit = float64(summary.SharedTokens) / float64(summary.OfficeTokens)
	}
	return summary
}

// reportFileMetricsValid rejects impossible counters in an imported audit
// report. The builder only emits non-negative aggregates, and shared tokens
// cannot exceed either side. Without this check, a hand-edited JSON artifact
// could manufacture mathematically inconsistent coverage that still passes
// the recomputed threshold.
func reportFileMetricsValid(file reportFile) bool {
	if file.SourceBytes < -1 || file.OfficeRunes < 0 || file.OfficeTokens < 0 ||
		file.LegacyRunes < 0 || file.LegacyTokens < 0 || file.SharedTokens < 0 ||
		file.ElapsedMillis < 0 || file.SharedTokens > file.OfficeTokens || file.SharedTokens > file.LegacyTokens {
		return false
	}
	if normalizeFormat(file.Format) == "" || normalizeFormat(file.Format) != file.Format {
		return false
	}
	if !validReportErrorClass(file.ErrorClass) || (file.ErrorClass != "" && file.ErrorClass != "format_mismatch" && file.ErrorClass != "not_dual_enabled" && file.OfficeReadOK) {
		return false
	}
	// A successful reader must carry non-empty text. Token counts deliberately
	// remain allowed to be zero: a valid document can contain only punctuation,
	// emoji, or other non-token characters under the content-free comparison
	// tokenizer. It can never satisfy the token-coverage gate without a legacy
	// token baseline, but it must not be mislabeled as a forged sample.
	if file.OfficeReadOK && file.OfficeRunes == 0 {
		return false
	}
	if file.LegacyOK && file.LegacyRunes == 0 {
		return false
	}
	if file.SharedTokens > 0 && (!file.OfficeReadOK || !file.LegacyOK) {
		return false
	}
	// Failed readers contribute failure counts, but never text/token evidence.
	// The adapter zeros those fields before emitting diagnostics; enforce the
	// same contract for imported JSON so a failed sample cannot silently affect
	// token coverage or resource-like aggregate counters.
	if !file.OfficeReadOK && (file.OfficeRunes != 0 || file.OfficeTokens != 0) {
		return false
	}
	if !file.LegacyOK && (file.LegacyRunes != 0 || file.LegacyTokens != 0) {
		return false
	}
	switch file.ErrorClass {
	case "format_mismatch", "not_dual_enabled", "input_too_large", "encrypted", "malformed":
		return !file.OfficeReadOK && !file.LegacyOK && file.OfficeRunes == 0 && file.OfficeTokens == 0 &&
			file.LegacyRunes == 0 && file.LegacyTokens == 0 && file.SharedTokens == 0
	default:
		return true
	}
}

// validOpaqueSampleID accepts only the fixed-width, per-report IDs emitted by
// opaqueSampleID. Besides making malformed artifacts easier to spot, this
// prevents an audit input from smuggling a filename, path, or arbitrary
// operator annotation into the field that is intended to be privacy-safe.
func validOpaqueSampleID(value string) bool {
	const prefix = "sample-"
	const digestHexChars = 16
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+digestHexChars {
		return false
	}
	for _, char := range value[len(prefix):] {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

// validReportErrorClass is intentionally closed. Report artifacts are a
// release input, so accepting arbitrary strings would let hand-edited records
// claim an unreviewed extraction state while still contributing to a gate.
func validReportErrorClass(value string) bool {
	switch value {
	case "", "format_mismatch", "not_dual_enabled", "input_too_large", "encrypted", "malformed", "output_too_large", "unreadable", "extract_error", "empty_text":
		return true
	default:
		return false
	}
}

func formatSummaryMetricsValid(summary formatSummary) bool {
	return summary.Total >= 0 && summary.OfficeReadOK >= 0 && summary.LegacyOK >= 0 &&
		summary.OfficeReadOK <= summary.Total && summary.LegacyOK <= summary.Total &&
		summary.OfficeTokens >= 0 && summary.LegacyTokens >= 0 && summary.SharedTokens >= 0 &&
		summary.SharedTokens <= summary.OfficeTokens && summary.SharedTokens <= summary.LegacyTokens && summary.MaxMillis >= 0 &&
		isFiniteReportRate(summary.OfficeTokenHit) && isFiniteReportRate(summary.LegacyTokenHit) &&
		(summary.LegacyTokens > 0 || summary.OfficeTokenHit == 0) &&
		(summary.OfficeTokens > 0 || summary.LegacyTokenHit == 0)
}

func isFiniteReportRate(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// decodeReport accepts the UTF-8 BOM that Windows PowerShell may add when a
// JSON report is copied or rewritten with its default text encoding. The
// report writer itself emits plain UTF-8; accepting only a leading BOM keeps
// the audit parser strict for every other malformed JSON input.
func decodeReport(data []byte) (report, error) {
	data = trimLeadingUTF8BOM(data)
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return report{}, err
	}
	var input report
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return report{}, err
	}
	// Decoder.Decode accepts a valid JSON value followed by another one. A
	// release artifact must contain exactly one report object, so consume the
	// stream and require EOF after the first value.
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return report{}, errors.New("audit report contains multiple JSON values")
		}
		return report{}, err
	}
	if !isFiniteReportRate(input.MinimumTokenHit) {
		return report{}, errors.New("minimum OfficeRead token hit must be finite")
	}
	return input, nil
}

// rejectDuplicateJSONKeys rejects duplicate object members at every nesting
// level before decoding into Go structs. encoding/json otherwise accepts them
// with a last-value-wins rule, making a hand-edited release artifact ambiguous
// to humans and potentially different tooling.
func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := rejectDuplicateJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("audit report contains multiple JSON values")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	if depth >= maxAuditJSONDepth {
		return errors.New("audit report exceeds maximum JSON nesting depth")
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("audit report object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("audit report contains duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if err := rejectDuplicateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("audit report object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := rejectDuplicateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("audit report array is not closed")
		}
	default:
		return errors.New("audit report has an unexpected closing JSON delimiter")
	}
	return nil
}

func parseRequiredFormats(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("value is empty")
	}
	values := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(values))
	formats := make([]string, 0, len(values))
	for _, value := range values {
		rawFormat := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), ".")
		format := normalizeFormat(rawFormat)
		if format == "" {
			return nil, fmt.Errorf("unsupported format %q", rawFormat)
		}
		if _, ok := seen[format]; ok {
			return nil, fmt.Errorf("duplicate format %q", format)
		}
		seen[format] = struct{}{}
		formats = append(formats, format)
	}
	if len(formats) == 0 {
		return nil, errors.New("value contains no formats")
	}
	sort.Strings(formats)
	return formats, nil
}

func validProvenance(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case provenanceInternalAuthorized, provenanceFixture, provenanceUnknown:
		return true
	default:
		return false
	}
}

func randomReportSalt() ([]byte, error) {
	salt := make([]byte, 32)
	_, err := rand.Read(salt)
	return salt, err
}

func opaqueSampleID(path string, salt []byte) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	// Salt is private to this process and is intentionally not serialized. The
	// ID therefore stays stable during one report but cannot be correlated or
	// brute-forced across reports from a guessed file path.
	payload := append(append([]byte(nil), salt...), []byte(filepath.Clean(absolute))...)
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sample-%x", sum[:8])
}

func assessFormat(summary formatSummary, provenance string, minimumSamples int, minimumTokenHit float64) formatAssessment {
	reasons := make([]string, 0, 4)
	if !isFiniteReportRate(minimumTokenHit) || minimumTokenHit < 0 || minimumTokenHit > 1 {
		reasons = append(reasons, "invalid_minimum_token_hit")
	}
	if provenance != provenanceInternalAuthorized {
		reasons = append(reasons, "sample_provenance_is_not_internal_authorized")
	}
	if summary.Total < minimumSamples {
		reasons = append(reasons, "insufficient_sample_count")
	}
	if summary.LegacyOK == 0 {
		reasons = append(reasons, "legacy_baseline_unavailable")
	} else if summary.OfficeReadOK < summary.LegacyOK {
		reasons = append(reasons, "officeread_success_count_below_legacy")
	}
	if summary.LegacyTokens == 0 {
		reasons = append(reasons, "legacy_token_baseline_unavailable")
	} else if summary.OfficeTokenHit < minimumTokenHit {
		reasons = append(reasons, "officeread_token_coverage_below_threshold")
	}
	if len(reasons) == 0 {
		return formatAssessment{QuantitativeGate: "pass", Reasons: []string{}}
	}
	for _, reason := range reasons {
		if reason == "officeread_success_count_below_legacy" || reason == "officeread_token_coverage_below_threshold" {
			return formatAssessment{QuantitativeGate: "fail", Reasons: reasons}
		}
	}
	return formatAssessment{QuantitativeGate: "insufficient_evidence", Reasons: reasons}
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func uniqueOfficePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	acceptedInfos := make([]os.FileInfo, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || !isOfficeFormat(filepath.Ext(path)) {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		// Keep lexical deduplication exact. Lower-casing would incorrectly merge
		// two independent files such as Report.docx and report.docx on a
		// case-sensitive filesystem; os.SameFile below already handles aliases
		// of the same physical file on case-insensitive platforms.
		key := filepath.Clean(absolute)
		if _, ok := seen[key]; ok {
			continue
		}
		// os.Stat resolves symlinks, and os.SameFile additionally compares the
		// platform's stable file identity. The latter catches hard links which
		// have different absolute paths yet refer to exactly the same source.
		// Do not content-hash separately copied files: that would turn this
		// privacy-preserving local report into an unbounded second full-file read
		// and cannot establish whether duplicate business samples are independent.
		duplicateFile := false
		for _, accepted := range acceptedInfos {
			if os.SameFile(info, accepted) {
				duplicateFile = true
				break
			}
		}
		if duplicateFile {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, absolute)
		acceptedInfos = append(acceptedInfos, info)
	}
	sort.Strings(out)
	return out
}

func isOfficeFormat(ext string) bool {
	switch strings.ToLower(ext) {
	case ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx":
		return true
	default:
		return false
	}
}

func configuredFormats() []string {
	return currentReportPolicy().Formats
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "office-read-dual-report: "+format+"\n", args...)
	os.Exit(2)
}
