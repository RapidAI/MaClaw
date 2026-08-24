package main

import (
	"errors"
	"strings"
	"testing"
)

// An instance with no audit store used to answer "No matching audit events or
// conversations for the current principal." Nothing had been searched, so that
// sentence was not a weak answer, it was a false one — and a model handed a
// negative finding passes it to the user as a negative finding.
func TestAnInstanceWithoutAuditStoresRefusesInsteadOfReportingNothingFound(t *testing.T) {
	h := &IMMessageHandler{}
	if log := h.getAuditLog(); log != nil {
		t.Skip("this handler unexpectedly has an audit store; the absence case cannot be built here")
	}
	got, err := h.readTrustedAudit("user-1", "")
	if err == nil {
		t.Fatalf("a storeless instance answered the question: %q", got)
	}
	if strings.Contains(got, "No matching") {
		t.Fatalf("a storeless instance reported a finding: %q", got)
	}
	if !strings.Contains(err.Error(), "trusted_audit_unavailable") {
		t.Fatalf("err = %q, want the bounded unavailable reason", err)
	}
}

// Absence and emptiness must stay distinguishable at the source, or the two
// collapse back into one answer the next time this function is edited.
func TestAnAbsentAuditStoreIsNotAnEmptyResult(t *testing.T) {
	h := &IMMessageHandler{}
	events, err := h.listTrustedAuditEvents("user-1", "")
	if !errors.Is(err, errTrustedAuditStoreAbsent) {
		t.Fatalf("absent event store returned err=%v, want the absence sentinel", err)
	}
	if len(events) != 0 {
		t.Fatalf("absent event store produced %d events", len(events))
	}
	conversations, err := h.listTrustedAuditConversations("user-1", "")
	if !errors.Is(err, errTrustedAuditStoreAbsent) {
		t.Fatalf("absent conversation store returned err=%v, want the absence sentinel", err)
	}
	if len(conversations) != 0 {
		t.Fatalf("absent conversation store produced %d hits", len(conversations))
	}
}

// A store that exists and matches nothing is still a real finding, and must
// keep saying so. The refusal above is about instances that cannot look, not
// about turns that looked and found nothing.
func TestAnEmptyAuditStoreStillReportsNothingFound(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := &IMMessageHandler{app: app}
	audit := h.getAuditLog()
	store := h.getSessionStore()
	if audit == nil || store == nil {
		t.Fatal("fixture failed to create the stores this case is about")
	}
	t.Cleanup(func() {
		_ = audit.Close()
		_ = store.Close()
	})
	got, err := h.readTrustedAudit("user-1", "")
	if err != nil {
		t.Fatalf("an empty but present store must answer, not refuse: %v", err)
	}
	if !strings.Contains(got, "No matching") {
		t.Fatalf("empty store answer = %q", got)
	}
}
