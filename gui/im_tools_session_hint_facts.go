package main

type sessionOutputHintFacts struct {
	Status                SessionStatus
	HasAPIRetry           bool
	HasRecentTransientAPI bool
	StallState            StallState
	CompletionLevel       CompletionLevel
	AutoContinueCount     int
	StructuredSession     bool
	ExitCode              *int
	Tool                  string
	ResumeContext         *SessionResumeContext
	FatalSessionError     bool
}

func collectSessionOutputHintFacts(session *RemoteSession, status string, rawLines []string) sessionOutputHintFacts {
	facts := sessionOutputHintFacts{
		Status:                normalizeSessionStatus(status),
		HasAPIRetry:           recentSessionOutputHasMarker(rawLines, 10, sessionOutputMarkerKind.IsAPIRetry),
		HasRecentTransientAPI: recentSessionOutputHasMarker(rawLines, 5, sessionOutputMarkerKind.IsTransientAPIIssue),
	}
	if session == nil {
		return facts
	}
	session.mu.RLock()
	facts.StallState = session.StallState
	facts.CompletionLevel = session.CompletionLevel
	facts.AutoContinueCount = session.AutoContinueCount
	if session.ExitCode != nil {
		cp := *session.ExitCode
		facts.ExitCode = &cp
	}
	facts.Tool = session.Tool
	facts.ResumeContext = session.ResumeContext
	session.mu.RUnlock()
	facts.StructuredSession = session.isStructuredSession()
	if facts.HasNonZeroTerminalExit() {
		facts.FatalSessionError = isFatalSessionError(rawLines)
	}
	return facts
}

func (f sessionOutputHintFacts) HasNonZeroTerminalExit() bool {
	return f.Status.IsTerminal() && f.ExitCode != nil && *f.ExitCode != 0
}

func (f sessionOutputHintFacts) HasStructuredNormalExitWithPossibleUnfinishedWork() bool {
	return f.Status == SessionExited && f.ExitCode != nil && *f.ExitCode == 0 && f.StructuredSession
}

func (f sessionOutputHintFacts) ResumeCount() int {
	if f.ResumeContext == nil {
		return 0
	}
	return f.ResumeContext.ResumeCount
}
