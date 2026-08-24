package agentservice

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// Every reviewed host adapter publishes one closed invocation schema, and each
// adapter's own test states the fields it rejects. That leaves a gap: a new
// adapter arrives with no such test and nothing notices. This gate holds the
// whole set against the same parameter closure the GUI surface is held to.

func reviewedHostInvocationSchemas() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"attachmentdeliver": reviewedHostAttachmentDeliverInvocationSchema(),
		"audiorender":       reviewedHostAudioRenderInvocationSchema(),
		"audiosynthesize":   reviewedHostAudioSynthesizeInvocationSchema(),
		"audiotranscribe":   reviewedHostAudioTranscribeInvocationSchema(),
		"audit":             reviewedHostAuditInvocationSchema(),
		"browser":           reviewedHostBrowserInvocationSchema(),
		"buildverify":       reviewedHostBuildVerifyInvocationSchema(),
		"clock":             reviewedHostClockInvocationSchema(),
		"computeruse":       reviewedHostComputerUseInvocationSchema(),
		"config":            reviewedHostConfigInvocationSchema(),
		"delegate":          reviewedHostDelegateInvocationSchema(),
		"docgenerate":       reviewedHostDocumentGenerateInvocationSchema(),
		"doclaunch":         reviewedHostSystemLaunchInvocationSchema(),
		"docread":           reviewedHostDocumentReadInvocationSchema(),
		"filedeliver":       reviewedHostFileDeliverInvocationSchema(),
		"filedownload":      reviewedHostFileDownloadInvocationSchema(),
		"fileread":          reviewedHostFileReadInvocationSchema(),
		"filewrite":         reviewedHostFileWriteInvocationSchema(),
		"goal":              reviewedHostGoalInvocationSchema(),
		"knowledge":         reviewedHostKnowledgeInvocationSchema(),
		"knowledgeadmin":    reviewedHostKnowledgeAdminInvocationSchema(),
		"knowledgewrite":    reviewedHostKnowledgeWriteInvocationSchema(),
		"memory":            reviewedHostMemoryInvocationSchema(),
		"memoryrecall":      reviewedHostMemoryRecallInvocationSchema(),
		"messagesend":       reviewedHostMessageSendInvocationSchema(),
		"officewrite":       reviewedHostOfficeWriteInvocationSchema(),
		"repoinspect":       reviewedHostRepoInspectInvocationSchema(),
		"repomutate":        reviewedHostRepoMutateInvocationSchema(),
		"schedule":          reviewedHostScheduleInvocationSchema(),
		"scheduledispatch":  reviewedHostScheduleDispatchInvocationSchema(),
		"session":           reviewedHostSessionInvocationSchema(),
		"shellexecute":      reviewedHostShellInvocationSchema(),
		"ssh":               reviewedHostSSHInvocationSchema(),
		"task":              reviewedHostTaskInvocationSchema(),
		"template":          reviewedHostTemplateInvocationSchema(),
		"urllaunch":         reviewedHostURLLaunchInvocationSchema(),
		"visualcapture":     reviewedHostVisualCaptureInvocationSchema(),
		"webfetch":          reviewedHostWebFetchInvocationSchema(),
	}
}

// Reviewed reasons for the crossings that remain. A location or identifier
// field is only acceptable when the adapter resolves it against a server-owned
// root and the caller's own principal, never as written.
const (
	// resolveWorkspacePath plus the principal comparison confine the value to
	// the caller's workspace, so the model names a location inside a root it
	// already owns rather than a host path.
	reasonHostWorkspaceConfinedLocation = "workspace-confined location resolved against the caller's principal"
	// The URL is what the capability exists to address. The adapter owns the
	// scheme allowlist and canonicalization; the model cannot pick the target
	// of a different capability with it.
	reasonHostCapabilityTargetURL = "the URL is the capability's own target and is canonicalized by the host adapter"
	// The identifier selects a record inside the host's own store, which the
	// adapter reads with the caller's principal. It does not substitute a
	// provider, selection, or grant binding.
	reasonHostPrincipalScopedRecordID = "record identifier inside the host store, read with the caller's principal"
)

var reviewedHostSchemaGateBaseline = map[string]map[string]string{
	"browser":        {"url": reasonHostCapabilityTargetURL},
	"filedownload":   {"url": reasonHostCapabilityTargetURL},
	"urllaunch":      {"url": reasonHostCapabilityTargetURL},
	"webfetch":       {"url": reasonHostCapabilityTargetURL},
	"doclaunch":      {"path": reasonHostWorkspaceConfinedLocation},
	"filedeliver":    {"path": reasonHostWorkspaceConfinedLocation},
	"fileread":       {"path": reasonHostWorkspaceConfinedLocation},
	"filewrite":      {"path": reasonHostWorkspaceConfinedLocation},
	"officewrite":    {"path": reasonHostWorkspaceConfinedLocation},
	"knowledgeadmin": {"id": reasonHostPrincipalScopedRecordID},
	"memory":         {"id": reasonHostPrincipalScopedRecordID},
	"schedule":       {"id": reasonHostPrincipalScopedRecordID},
	"session":        {"id": reasonHostPrincipalScopedRecordID},
	"task":           {"id": reasonHostPrincipalScopedRecordID},
	"knowledgewrite": {
		"path": reasonHostWorkspaceConfinedLocation,
		"url":  reasonHostCapabilityTargetURL,
	},
}

func TestReviewedHostAdaptersHaveNoUnreviewedParameterCrossing(t *testing.T) {
	var unreviewed []string
	for adapter, schema := range reviewedHostInvocationSchemas() {
		reviewed := reviewedHostSchemaGateBaseline[adapter]
		for _, finding := range coretool.InspectManagedInvocationSchema(schema) {
			if _, ok := reviewed[finding.Pointer]; ok {
				continue
			}
			unreviewed = append(unreviewed, fmt.Sprintf("%s: %s", adapter, finding))
		}
	}
	if len(unreviewed) > 0 {
		sort.Strings(unreviewed)
		t.Fatalf("reviewed host adapters cross the parameter closure at %d unreviewed sites:\n  %s",
			len(unreviewed), strings.Join(unreviewed, "\n  "))
	}
}

func TestReviewedHostSchemaGateBaselineHasNoStaleEntries(t *testing.T) {
	schemas := reviewedHostInvocationSchemas()
	var stale []string
	for adapter, reviewed := range reviewedHostSchemaGateBaseline {
		schema, published := schemas[adapter]
		if !published {
			stale = append(stale, adapter+": adapter is no longer published")
			continue
		}
		live := make(map[string]bool)
		for _, finding := range coretool.InspectManagedInvocationSchema(schema) {
			live[finding.Pointer] = true
		}
		for pointer, reason := range reviewed {
			if reason == "" {
				stale = append(stale, adapter+" "+pointer+": baseline entry has no reason")
			}
			if !live[pointer] {
				stale = append(stale, adapter+" "+pointer+": crossing is gone, delete the entry")
			}
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("host schema gate baseline is stale:\n  %s", strings.Join(stale, "\n  "))
	}
}

// Server-bound fields and open objects have no reviewed exception on this
// surface, so this invariant is asserted without a baseline escape hatch.
func TestReviewedHostAdaptersNeverPublishReservedOrOpenFields(t *testing.T) {
	for adapter, schema := range reviewedHostInvocationSchemas() {
		for _, finding := range coretool.InspectManagedInvocationSchema(schema) {
			switch finding.Code {
			case coretool.SchemaGateReservedField, coretool.SchemaGateOpenObject, coretool.SchemaGateMissingSchema:
				t.Fatalf("host adapter %q publishes %s", adapter, finding)
			}
		}
	}
}

func TestReviewedHostAdaptersHaveCompleteParameterAuthorization(t *testing.T) {
	for adapter, schema := range reviewedHostInvocationSchemas() {
		authorization, err := coretool.NewParameterAuthorization(schema)
		if err != nil {
			t.Fatalf("authorize %q: %v", adapter, err)
		}
		if authorization.Digest == "" || authorization.CanonicalizerVer == "" {
			t.Fatalf("%q authorization is not bound: %#v", adapter, authorization)
		}
		properties, _ := schema["properties"].(map[string]interface{})
		if len(authorization.AllowedFields) != len(properties) {
			t.Fatalf("%q allowed fields %v do not match its schema properties", adapter, authorization.AllowedFields)
		}
	}
}

// A hand-written adapter list would silently stop covering a new adapter. The
// package source is the authority for which adapters exist.
func TestReviewedHostSchemaGateCoversEveryAdapter(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	declaration := regexp.MustCompile(`(?m)^func (reviewedHost\w+InvocationSchema)\(\)`)
	declared := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, readErr := os.ReadFile(filepath.Clean(name))
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		for _, match := range declaration.FindAllStringSubmatch(string(source), -1) {
			declared[match[1]] = name
		}
	}
	if len(declared) == 0 {
		t.Fatal("no reviewed host invocation schema was found, the coverage check is vacuous")
	}
	if len(declared) != len(reviewedHostInvocationSchemas()) {
		var missing []string
		for function, file := range declared {
			missing = append(missing, function+" ("+file+")")
		}
		sort.Strings(missing)
		t.Fatalf("the gate covers %d adapters but the package declares %d schemas:\n  %s",
			len(reviewedHostInvocationSchemas()), len(declared), strings.Join(missing, "\n  "))
	}
}
