package agentruntime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJobStatusVocabularyIsStable(t *testing.T) {
	cases := []struct {
		status JobStatus
		want   string
	}{
		{JobStatusPending, "pending"},
		{JobStatusRunning, "running"},
		{JobStatusSucceeded, "succeeded"},
		{JobStatusFailed, "failed"},
		{JobStatusCanceled, "canceled"},
		{JobStatusUnknown, "unknown"},
	}
	for _, tc := range cases {
		if string(tc.status) != tc.want {
			t.Fatalf("JobStatus=%q, want %q", tc.status, tc.want)
		}
	}
}

func TestNormalizeJobStatusFailsClosed(t *testing.T) {
	if got := NormalizeJobStatus(JobStatus("legacy-progress")); got != JobStatusUnknown {
		t.Fatalf("NormalizeJobStatus(legacy-progress)=%q, want unknown", got)
	}
	if got := NormalizeJobStatus(JobStatusFailed); got != JobStatusFailed {
		t.Fatalf("NormalizeJobStatus(failed)=%q, want failed", got)
	}
}

func TestProjectLegacyJobStatusVocabulary(t *testing.T) {
	cases := map[string]JobStatus{
		"queued":          JobStatusPending,
		"planning":        JobStatusPending,
		"indexing":        JobStatusRunning,
		"in_progress":     JobStatusRunning,
		"success":         JobStatusSucceeded,
		"completed":       JobStatusSucceeded,
		"timeout":         JobStatusFailed,
		"cancelled":       JobStatusCanceled,
		"partial_success": JobStatusUnknown,
	}
	for input, want := range cases {
		if got := ProjectLegacyJobStatus(input); got != want {
			t.Errorf("ProjectLegacyJobStatus(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestJobErrorCodeContract(t *testing.T) {
	if JobErrorCodeCanceled != "job_canceled" ||
		JobErrorCodeServiceRestarted != "service_restarted" ||
		JobErrorCodeReconcileRequired != "reconcile_required" ||
		JobErrorCodeResultNotSerializable != "result_not_serializable" ||
		JobErrorCodePersistenceFailed != "job_persistence_failed" {
		t.Fatalf("job error code contract drifted: %q %q %q %q %q", JobErrorCodeCanceled, JobErrorCodeServiceRestarted, JobErrorCodeReconcileRequired, JobErrorCodeResultNotSerializable, JobErrorCodePersistenceFailed)
	}
}

func TestNormalizeJobRecoveryPolicyFailsClosed(t *testing.T) {
	for _, policy := range []JobRecoveryPolicy{"", "future-policy", JobRecoveryPolicyReconcile} {
		if got := NormalizeJobRecoveryPolicy(policy); got != JobRecoveryPolicyReconcile {
			t.Fatalf("NormalizeJobRecoveryPolicy(%q)=%q, want reconcile", policy, got)
		}
	}
	if got := NormalizeJobRecoveryPolicy(JobRecoveryPolicyFail); got != JobRecoveryPolicyFail {
		t.Fatalf("NormalizeJobRecoveryPolicy(fail)=%q, want fail", got)
	}
}

func TestJobLeaseMetadataIsNotPartOfTransportJSON(t *testing.T) {
	expiresAt := time.Now().UTC().Add(time.Minute)
	payload, err := json.Marshal(Job{ID: "job_lease", LeaseOwnerID: "private-executor", LeaseExpiresAt: &expiresAt})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "private-executor") || strings.Contains(string(payload), "lease_expires") || strings.Contains(string(payload), "lease_owner") {
		t.Fatalf("repository lease metadata leaked into transport JSON: %s", payload)
	}
}
