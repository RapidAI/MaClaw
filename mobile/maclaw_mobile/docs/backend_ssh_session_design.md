# Backend SSH Session Design

MaClaw Mobile should mirror the MaClaw GUI backend SSH session management model,
not ship as a standalone phone-local SSH terminal.

## GUI Reference

The desktop GUI/core runtime already models SSH as managed background sessions:

- `corelib/remote/ssh_manager.go`
  - `SSHSessionManager` owns a session map and an `SSHPool`.
  - `Create(spec SSHSessionSpec)` creates a PTY-backed managed session.
  - `List`, `Get`, `WriteInput`, and `WriteInputChecked` operate on existing
    sessions by ID.
  - `ReconnectByID`, `reconnectSession`, `CheckShellResponsive`, and
    `probeShell` keep long-lived sessions usable across disconnects.
- `corelib/remote/ssh_session.go`
  - `SSHPTYSession` wraps the SSH channel as a PTY execution handle.
  - Output, input, interrupt, close, keepalive, and exit status are streamed
    through the same execution-handle shape used by local PTY sessions.
- `gui/remote_session_manager.go`
  - The GUI side treats remote activity as managed sessions that can be resumed,
    monitored, and controlled, instead of one-off terminal sockets.

## Mobile Target Model

Mobile connects only through the official Hub/tenant path:

1. The app discovers an available HubCenter from the three preset official
   HubCenter URLs.
2. HubCenter resolves the user's tenant Hub after phone login.
3. The mobile app asks the tenant Hub or authorized digital employee/agent to
   create or attach an SSH backend session.
4. The backend/agent owns the real SSH connection, credentials, PTY, keepalive,
   reconnect, responsiveness probes, and session lifecycle.
5. The phone shows session status, output, command drafts, risk warnings, and
   manual confirmation controls.

The phone should not call Go `corelib` directly. Flutter calls Hub APIs and
realtime streams; the Hub/agent side may reuse the existing Go SSH manager.

## Required Mobile Capabilities

- Server profile list with tag, note, auth mode, and tenant-scoped metadata.
- Create or attach backend SSH session for a server profile.
- Session list/status surface showing backend session ID, host/profile linkage,
  alive/responsive state, last activity, and reconnect state.
- Stream backend session output through Hub realtime/WebSocket.
- Send user-confirmed input to the backend session.
- Use risk classification before sending dangerous commands.
- Copy/redact session output before AI analysis.
- Ask AI for explanation and command drafts only; high-risk commands remain
  manual-confirm and never auto-executed by default.
- Hand backend session output or pasted/copied output to a digital employee only
  after Hub/tenant warning confirmation.

## API Shape To Add

Suggested tenant Hub endpoints:

- `GET /api/mobile/server-profiles`
- `POST /api/mobile/server-profiles`
- `DELETE /api/mobile/server-profiles/{profile_id}`
- `GET /api/mobile/ssh/sessions`
- `POST /api/mobile/ssh/sessions`
- `POST /api/mobile/ssh/sessions/{session_id}/attach`
- `POST /api/mobile/ssh/sessions/{session_id}/input`
- `POST /api/mobile/ssh/sessions/{session_id}/interrupt`
- `POST /api/mobile/ssh/sessions/{session_id}/reconnect`
- `DELETE /api/mobile/ssh/sessions/{session_id}`
- realtime event types for session output, status, disconnect, reconnect, and
  abnormal-state notifications.

## Current State And Remaining Gap

The Flutter server screen no longer creates a phone-local SSH socket or depends
on `dartssh2`. It now calls Hub-style backend session APIs to create/attach,
reconnect, send input, and close a managed session.

The remaining release-critical gap is on the service side: the tenant Hub and
authorized agent/digital employee must implement these APIs by binding them to
the GUI/core `SSHSessionManager` model, and realtime session output/status
events must be streamed back to mobile.
