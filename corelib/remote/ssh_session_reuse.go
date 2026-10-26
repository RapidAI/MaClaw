package remote

import "strings"

// SSHHostKeyReuse reports whether an existing session satisfies the host-key
// policy of a connect. A requested pin matches only that pin. A capture, used
// when the caller does not yet have a pin, matches a session that already
// recorded one: that handshake is the pin. It does not match a session with
// no pin, because that connection cannot prove which key authenticated it.
// A connect that asks for neither pin nor capture keeps the open session.
func SSHHostKeyReuse(storedPin, requestedPin string, capture bool) bool {
	stored := strings.TrimSpace(storedPin)
	requested := strings.TrimSpace(requestedPin)
	if requested != "" {
		return stored == requested
	}
	if capture {
		return stored != ""
	}
	return true
}

// configuredSSHLabel is a label the user or an OpenSSH alias actually set.
// Create copies the host id into HostLabel when no label was set, so that
// copy must not attach a connect for a different host.
func configuredSSHLabel(summary SSHSessionSummary) string {
	label := strings.TrimSpace(summary.HostLabel)
	if label == "" || label == strings.TrimSpace(summary.HostID) {
		return ""
	}
	return label
}

// SelectReusableSSHSession is the connect admission. It returns the session
// this owner, endpoint (or configured label), and host-key policy already
// has. An open PTY wins, even over a newer terminal record. When every match
// has exited or failed, the newest of those is still this session: the caller
// reconnects that id. RemoveSession is what forgets it. A nil result is the
// only signal that the caller may open a session. It does not dial, probe, or
// interrupt. force_new does not call this.
func SelectReusableSSHSession(sessions []*SSHManagedSession, hostID, label, ownerID, requestedPin string, capture bool) *SSHManagedSession {
	hostID = strings.TrimSpace(hostID)
	label = strings.TrimSpace(label)
	var chosen *SSHManagedSession
	var chosenAt int64
	var chosenOpen bool
	var have bool
	for _, session := range sessions {
		if session == nil || !SSHSessionOwnerMatches(session.OwnerID, ownerID) {
			continue
		}
		summary := session.GetSummary()
		if strings.TrimSpace(summary.HostID) != hostID && (label == "" || configuredSSHLabel(summary) != label) {
			continue
		}
		if !SSHHostKeyReuse(session.HostKeyFingerprint(), requestedPin, capture) {
			continue
		}
		open := SessionStatus(summary.Status).IsOpen()
		// UpdatedAt is unix seconds, and output refreshes it. Two sessions in
		// the same second are equal. Map order must not decide which PTY a
		// connect continues, so the smaller session id wins a tie. An open PTY
		// is the debug machine; a newer exit must not take its place.
		if !have || (open && !chosenOpen) || (open == chosenOpen && (summary.UpdatedAt > chosenAt || (summary.UpdatedAt == chosenAt && session.ID < chosen.ID))) {
			chosen = session
			chosenAt = summary.UpdatedAt
			chosenOpen = open
			have = true
		}
	}
	return chosen
}
