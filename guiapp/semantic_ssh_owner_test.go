package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestSSHSessionOwnerMatchesIsExact(t *testing.T) {
	if !remote.SSHSessionOwnerMatches(" principal-a ", "principal-a") {
		t.Fatal("trimmed principal ids match")
	}
	if !remote.SSHSessionOwnerMatches("", "  ") {
		t.Fatal("an empty owner belongs only to an empty principal")
	}
	if remote.SSHSessionOwnerMatches("", "principal-a") || remote.SSHSessionOwnerMatches("principal-a", "") {
		t.Fatal("a named principal must not inherit an unowned session, and an empty principal must not inherit a named one")
	}
	if remote.SSHSessionOwnerMatches("principal-a", "principal-b") {
		t.Fatal("different principals must not match")
	}
}

func TestAnotherPrincipalsSSHSessionDoesNotBindThisTurn(t *testing.T) {
	const (
		api2    = "desktop-user:api2-task"
		znsoft  = "desktop-user:znsoft-task"
		api2SID = "ssh_root@api2.example:22_1"
	)
	sessions := []*remote.SSHManagedSession{
		{ID: api2SID, OwnerID: api2},
		{ID: "ssh_znsoft_dead", OwnerID: znsoft},
		{ID: "ssh_unowned", OwnerID: ""},
		nil,
	}
	alive := func(session *remote.SSHManagedSession) bool {
		return session != nil && session.ID != "ssh_znsoft_dead"
	}

	znsoftBound := sshSessionsBoundToPrincipal(sessions, znsoft, alive)
	if len(znsoftBound) != 0 {
		t.Fatalf("znsoft live sessions = %d, want 0", len(znsoftBound))
	}
	if mode := semanticSSHPublicationMode(len(znsoftBound), false); mode != "connect" {
		t.Fatalf("a principal with no live session of its own publishes %s, want connect", mode)
	}
	if ids := sshBoundSituationSessionIDs(sessions, znsoft, alive); len(ids) != 0 {
		t.Fatalf("znsoft situation = %#v, want none", ids)
	}

	api2Bound := sshSessionsBoundToPrincipal(sessions, api2, alive)
	if len(api2Bound) != 1 || api2Bound[0].ID != api2SID {
		t.Fatalf("api2 bound = %#v", sessionIDs(api2Bound))
	}
	if mode := semanticSSHPublicationMode(len(api2Bound), false); mode != "exec" {
		t.Fatalf("the principal that opened the only live session publishes %s, want exec", mode)
	}
	if ids := sshBoundSituationSessionIDs(sessions, api2, alive); len(ids) != 1 || ids[0] != api2SID {
		t.Fatalf("api2 situation = %#v", ids)
	}

	unowned := sshSessionsBoundToPrincipal(sessions, "", alive)
	if len(unowned) != 1 || unowned[0].ID != "ssh_unowned" {
		t.Fatalf("empty principal bound = %#v", sessionIDs(unowned))
	}
	if got := sshSessionsBoundToPrincipal(sessions, api2, nil); got != nil {
		t.Fatal("a missing liveness predicate binds nothing")
	}
}

func TestTwoLiveSessionsOfOnePrincipalPublishNeitherMode(t *testing.T) {
	sessions := []*remote.SSHManagedSession{
		{ID: "ssh_1", OwnerID: "principal-a"},
		{ID: "ssh_2", OwnerID: "principal-a"},
		{ID: "ssh_other", OwnerID: "principal-b"},
	}
	bound := sshSessionsBoundToPrincipal(sessions, "principal-a", func(*remote.SSHManagedSession) bool { return true })
	if len(bound) != 2 {
		t.Fatalf("principal-a live sessions = %d, want 2", len(bound))
	}
	if mode := semanticSSHPublicationMode(len(bound), false); mode != "none" {
		t.Fatalf("several live sessions of one principal publish %s, want none", mode)
	}
	if mode := semanticSSHPublicationMode(len(bound), true); mode != "exec" {
		t.Fatalf("a runtime hook publishes %s, want exec", mode)
	}
	ids := sshSituationSessionIDs(append(sessions, &remote.SSHManagedSession{ID: "ssh_3", OwnerID: "principal-a"}, &remote.SSHManagedSession{ID: "ssh_4", OwnerID: "principal-a"}), "principal-a")
	if len(ids) != 3 || ids[0] != "ssh_1" || ids[2] != "ssh_3" {
		t.Fatalf("situation cap = %#v", ids)
	}
}

func TestSSHSessionReuseStaysInsideTheOwner(t *testing.T) {
	other := &remote.SSHManagedSession{
		ID:      "ssh_other",
		OwnerID: "other",
		Summary: remote.SSHSessionSummary{HostID: "user@host:22", HostLabel: "gpu", Status: "running"},
	}
	mine := &remote.SSHManagedSession{
		ID:      "ssh_mine",
		OwnerID: "mine",
		Summary: remote.SSHSessionSummary{HostID: "user@host:22", HostLabel: "gpu", Status: "running"},
	}
	if sshSessionReusable(other, "user@host:22", "gpu", "mine") {
		t.Fatal("connect must not reuse another principal's session on the same host")
	}
	if !sshSessionReusable(mine, "user@host:22", "", "mine") {
		t.Fatal("connect reuses the same principal's running session on the same host")
	}
	exited := &remote.SSHManagedSession{
		OwnerID: "mine",
		Summary: remote.SSHSessionSummary{HostID: "user@host:22", Status: "exited"},
	}
	if sshSessionReusable(exited, "user@host:22", "", "mine") {
		t.Fatal("an exited session is not reusable")
	}
	unowned := &remote.SSHManagedSession{
		OwnerID: "",
		Summary: remote.SSHSessionSummary{HostID: "user@host:22", Status: "running"},
	}
	if sshSessionReusable(unowned, "user@host:22", "", "mine") {
		t.Fatal("a named principal must not reuse a session that recorded no owner")
	}
	if !sshSessionReusable(unowned, "user@host:22", "", "") {
		t.Fatal("an empty principal reuses an unowned running session")
	}
	byLabel := &remote.SSHManagedSession{
		OwnerID: "mine",
		Summary: remote.SSHSessionSummary{HostID: "other-host", HostLabel: "gpu", Status: "running"},
	}
	waiting := &remote.SSHManagedSession{
		OwnerID: "mine",
		Summary: remote.SSHSessionSummary{HostID: "user@host:22", Status: "waiting_input"},
	}
	if !sshSessionReusable(waiting, "user@host:22", "", "mine") {
		t.Fatal("waiting_input is the same PTY and must be reused")
	}
	busyOther := &remote.SSHManagedSession{
		OwnerID: "other",
		Summary: remote.SSHSessionSummary{HostID: "user@host:22", Status: "busy"},
	}
	if sshSessionReusable(busyOther, "user@host:22", "", "mine") {
		t.Fatal("a busy session of another principal is not reusable")
	}
	if !sshSessionReusable(byLabel, "user@host:22", "gpu", "mine") {
		t.Fatal("the same principal can reuse a running session by label")
	}
	if sshSessionReusable(byLabel, "user@host:22", "", "mine") {
		t.Fatal("a different host without a label is not the same session")
	}

	h := &IMMessageHandler{}
	list := sshSessionList{other, mine}
	got := h.findRunningSSHSession(list, "user@host:22", "", "mine")
	if got == nil || got.ID != "ssh_mine" {
		t.Fatalf("reusable session = %#v", got)
	}
	if got := h.findRunningSSHSession(sshSessionList{other}, "user@host:22", "", "mine"); got != nil {
		t.Fatalf("foreign session was reused: %s", got.ID)
	}
}

func TestSSHForeignSessionReasonLeavesAMissingSessionToTheCaller(t *testing.T) {
	h := &IMMessageHandler{}
	if got := sshForeignSessionReason(h, "ssh_1", "principal-a", true); got != "" {
		t.Fatalf("no manager: %q", got)
	}
	h.sshMgr = remote.NewSSHSessionManager(nil)
	if got := sshForeignSessionReason(h, "missing", "principal-a", true); got != "" {
		t.Fatalf("missing session: %q", got)
	}
	if got := sshForeignSessionReason(h, "ssh_1", "principal-a", false); got != "" {
		t.Fatalf("unscoped call: %q", got)
	}
}

func TestSemanticSSHPublicationForASessionlessPrincipal(t *testing.T) {
	if semanticSSHPublicationMode(0, false) != "connect" || semanticSSHPublicationMode(1, false) != "exec" || semanticSSHPublicationMode(2, false) != "none" {
		t.Fatal("publication mode must follow this principal's live-session count")
	}
	h := &IMMessageHandler{}
	const principal = "desktop-user:znsoft-task"
	if !semanticTrustedSSHConnectPublished(h, principal) || semanticTrustedSSHPublished(h, principal) {
		t.Fatal("a principal with no session of its own publishes connect")
	}
	if sshAvailabilityMode(nil, principal) != "none" {
		t.Fatal("a nil handler publishes no ssh mode")
	}
	if sshAvailabilityMode(h, principal) != "connect" {
		t.Fatal("availability mode for a session-less principal is connect")
	}
}

func TestSituationUsesLocalStatusUntilTheCatalogProbes(t *testing.T) {
	open := &remote.SSHManagedSession{
		ID:      "ssh_open",
		OwnerID: "principal-a",
		Handle:  &remote.SSHPTYSession{},
		Summary: remote.SSHSessionSummary{Status: "running", HostID: "user@host:22"},
	}
	terminal := &remote.SSHManagedSession{
		ID:      "ssh_exited",
		OwnerID: "principal-a",
		Handle:  &remote.SSHPTYSession{},
		Summary: remote.SSHSessionSummary{Status: "exited"},
	}
	foreign := &remote.SSHManagedSession{
		ID:      "ssh_foreign",
		OwnerID: "principal-b",
		Handle:  &remote.SSHPTYSession{},
		Summary: remote.SSHSessionSummary{Status: "busy"},
	}
	noHandle := &remote.SSHManagedSession{
		ID:      "ssh_no_handle",
		OwnerID: "principal-a",
		Summary: remote.SSHSessionSummary{Status: "running"},
	}
	ids := sshBoundSituationSessionIDs([]*remote.SSHManagedSession{open, terminal, foreign, noHandle}, "principal-a", sshSituationSessionAlive)
	if len(ids) != 1 || ids[0] != "ssh_open" {
		t.Fatalf("situation = %#v", ids)
	}

	if guiRuntimeSSHSessionAlive(open) {
		t.Fatal("an unstarted handle is not a live PTY")
	}
	ids = sshBoundSituationSessionIDs([]*remote.SSHManagedSession{open}, "principal-a", sshSituationSessionAlive)
	if len(ids) != 0 {
		t.Fatalf("situation kept a session the catalog probe rejected: %#v", ids)
	}

	held := &remote.SSHManagedSession{
		ID:      "ssh_held",
		OwnerID: "principal-a",
		Handle:  &remote.SSHPTYSession{},
		Summary: remote.SSHSessionSummary{Status: "running"},
	}
	rememberSSHSessionAlive(held.Handle, true)
	ids = sshBoundSituationSessionIDs([]*remote.SSHManagedSession{open, held}, "principal-a", sshSituationSessionAlive)
	if len(ids) != 1 || ids[0] != "ssh_held" {
		t.Fatalf("situation = %#v, want the session whose probe is still open", ids)
	}
}

func TestSSHSessionStatusOpenMatchesTheBoundLifecycle(t *testing.T) {
	for _, status := range []remote.SessionStatus{remote.SessionStarting, remote.SessionRunning, remote.SessionBusy, remote.SessionWaitingInput} {
		if !sshSessionStatusOpen(status) {
			t.Fatalf("%s is still this principal's session", status)
		}
	}
	if sshSessionStatusOpen(remote.SessionExited) || sshSessionStatusOpen(remote.SessionError) {
		t.Fatal("a terminal session is not open")
	}
}

type sshSessionList []*remote.SSHManagedSession

func (l sshSessionList) List() []*remote.SSHManagedSession { return l }

func sessionIDs(sessions []*remote.SSHManagedSession) []string {
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			ids = append(ids, "<nil>")
			continue
		}
		ids = append(ids, session.ID)
	}
	return ids
}
