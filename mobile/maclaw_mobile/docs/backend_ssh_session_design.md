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
- `gui/im_tool_definitions.go` and `gui/tool_registry_builtin.go`
  - The GUI exposes SSH as remote server management, not only an interactive
    shell: `connect`, `exec`, `exec_background`, `check_task`, `wait_task`,
    `list_tasks`, `kill_task`, `upload`, `download`, `list`, and `close`.
  - Long commands are expected to become managed background tasks with task IDs,
    status checks, log tails, waiting, and kill controls.
  - File movement is handled through the existing GUI/agent SFTP path, so
    upload/download evidence belongs to the backend session owner, not the
    phone.
- `corelib/remote/ssh_session.go`
  - `SSHPTYSession` wraps the SSH channel as a PTY execution handle.
  - Output, input, interrupt, close, keepalive, and exit status are streamed
    through the same execution-handle shape used by local PTY sessions.
- `gui/remote_session_manager.go`
  - The GUI side treats remote activity as managed sessions that can be resumed,
    monitored, and controlled, instead of one-off terminal sockets.
- `gui/remote_mobile_backend_ssh_sessions.go`
  - `pollMobileBackendSSHSessionsOnce` asks the tenant Hub for a claimable
    mobile backend SSH control record.
  - `claimMobileBackendSSHSession` uses the machine-authenticated worker path
    `/api/mobile/ssh/sessions/claim`; the phone never claims its own SSH work.
  - `processMobileBackendSSHSession` resolves the sanitized mobile
    `server_profile_id` against desktop `SSHHosts`, creates or reuses the
    `SSHSessionManager` session, applies pending input, and reports the
    GUI/core `backend_session_id`.
  - `updateMobileBackendSSHSession` sends worker state back through
    `/api/mobile/ssh/sessions/{session_id}/worker`, including status, state,
    recent output, incremental output chunks, applied input counts, interrupt
    results, reconnect failures, and close results.
  - `mobileBackendSSHOutputChunk` tracks the output delta that mobile renders
    over realtime instead of requiring the phone to own the PTY.

## Mobile Target Model

Mobile connects only through the official Hub/tenant path:

1. The app discovers an available HubCenter from the three preset official
   HubCenter URLs.
2. HubCenter resolves the user's tenant Hub after phone login.
3. The mobile foreground assistant or remote-server page acts as a foreground
   agent: it asks the tenant Hub to create or attach an SSH backend session
   management record for a sanitized server profile published by MaClaw
   GUI/agent. The record is a durable management intent for the backend worker,
   not a request for the phone to open an SSH transport.
4. The tenant Hub records the session intent, pending input, status, and output
   cursor; an authorized MaClaw desktop/agent claims that control record.
5. MaClaw GUI/core owns the real SSH connection through `SSHSessionManager`,
   including credentials, PTY, keepalive, reconnect, responsiveness probes, and
   session lifecycle.
6. The phone shows session status, output, command drafts, risk warnings, and
   manual confirmation controls; it is the control surface for the backend
   session manager, not the process that owns the SSH transport.

This is the same management pattern as MaClaw GUI: a foreground agent can ask
for work, but the durable SSH session is created and maintained by the
authorized GUI/agent worker. Mobile must therefore model SSH as an agent-managed
background session with a Hub control record, not as an embedded terminal
library. The mobile foreground agent creates and controls management records;
it does not create SSH sockets, store server secrets, or replace the GUI/core
background session managers.

The phone should not call Go `corelib` directly. Flutter calls Hub APIs and
realtime streams; the Hub/agent side may reuse the existing Go SSH manager.

## Foreground Agent Flow

The mobile AI assistant is allowed to initiate emergency server maintenance,
but only as a foreground control-plane agent:

1. The user asks the mobile assistant to inspect or maintain a known server.
2. The assistant helps choose a Hub-synced server profile and creates or
   attaches a Hub control record for the backend SSH session manager in the tenant Hub.
3. The record remains queued or connecting until an authorized MaClaw GUI/agent
   worker claims it with machine authentication.
4. The GUI/agent worker resolves the profile against its local `SSHHosts`,
   creates or reuses the real managed session through `SSHSessionManager`, and
   reports `backend_session_id`, status, output preview, `output_chunk`, and
   `output_seq` back to Hub.
5. The mobile assistant can summarize output, prepare command drafts, request
   interrupt/reconnect/close, and submit redacted context to digital employees.
   It must not present itself as owning the SSH transport or credentials.
6. Command execution remains an explicit user action: the assistant can fill the
   command draft, risk-check it, and queue confirmed input, while the GUI/agent
   applies that input to the managed backend session.

This mirrors the MaClaw GUI session-management ability on mobile: the phone is
the emergency operator interface, Hub is the coordination queue, and
MaClaw GUI/agent plus `SSHSessionManager` remain the backend session owner.

## GUI-Equivalent Management Contract

The mobile feature must inherit the same management semantics that MaClaw GUI
already has for backend SSH sessions. This is the contract that separates it
from a simple SSH client:

- Session ownership stays with the desktop GUI/agent worker and the
  `SSHSessionManager`; mobile only creates, attaches, observes, and controls a
  Hub session-management record. The mobile foreground agent may initiate the
  record and queue confirmed operations, but the GUI/agent is the component that
  creates, keeps alive, reconnects, interrupts, executes background work, and
  performs file transfer on the real SSH session.
- The foreground mobile agent is a session-management requester: it can create
  the Hub control record from the AI assistant or remote-server surface, attach
  to an existing record, queue input, request interrupt/reconnect/close, start
  background tasks, request file operations, and hand output to AI/digital
  employees. The record is not active until an authorized GUI/agent worker
  claims it and binds it to a backend managed SSH session.
- Mobile must present this as MaClaw-style SSH backend management, not as a
  terminal-first SSH client. The foreground view is a selectable backend-session
  output panel for the managed Hub record, not a terminal emulator surface.
- Real SSH credentials, `SSHPool` connections, PTY handles, keepalive behavior,
  and lifecycle cleanup stay on the GUI/agent side.
- Output is reported as managed session state with recent preview text,
  incremental `output_chunk`, monotonic `output_seq`, last-activity status,
  backend session ID linkage, and the actual worker `claimed_by` identity
  reported by the authorized MaClaw GUI/agent. Mobile evidence must not replace
  that worker identity with a generic placeholder.
- User input is queued on the Hub record and applied by the GUI/agent through
  `WriteInputChecked`, so disconnected sessions can be detected and reconnected
  before input is retried.
- Mobile interrupt, reconnect, and close actions map to GUI/agent-managed
  `InterruptByID`, `ReconnectByID`, and `RemoveSession` operations.
- Responsiveness checks remain a backend concern: the GUI/agent can use
  `CheckShellResponsive`, `probeShell`, Ctrl+C recovery, and consecutive
  execution-failure tracking before deciding whether to keep, reconnect, or
  rebuild a session.
- AI and digital employees may analyze copied/redacted backend session output
  and produce command drafts, but command execution still requires explicit
  mobile user confirmation and the GUI/agent backend session path. Digital
  employee handoff context must include the GUI/agent-bound
  `backend_session_id` when a managed session is active, not merely the Hub
  control-record `session_id`, so remote workers and QA evidence can tie
  analysis, command drafts, and follow-up actions back to the same
  `backend_session_id`.

## GUI Capability Mapping

The mobile surface should expose the GUI/backend management capability, not a
raw terminal socket. The mapping is:

- Create/attach: the mobile foreground agent creates a Hub control record;
  GUI/agent claims it with `/api/mobile/ssh/sessions/claim` and binds it to
  `SSHSessionManager.Create`. The claim, not the phone button tap, is what
  proves the backend managed session exists.
- Session identity: mobile displays the Hub session ID, `backend_session_id`,
  server profile ID, claim/worker owner, and last update state so QA can prove
  the same managed session is used across actions.
- Command input: mobile queues only user-confirmed input; GUI/agent applies it
  through `WriteInputChecked` so broken sessions can reconnect before retry.
- Output: GUI/agent reports preview plus incremental `output_chunk` and
  monotonic `output_seq`; mobile renders that managed output and may copy or
  redact it for AI/digital-employee analysis. The mobile backend-session
  output panel and copied output must include a GUI/agent evidence line with actual values for the Hub session ID,
  GUI/agent-bound `backend_session_id`, worker `claimed_by`, and realtime
  numeric `output_seq`, so AI, digital-employee handoff, and QA records
  can prove the output came from the backend session manager rather than a
  phone-local SSH client.
- Interrupt/reconnect/close: mobile requests Hub control actions; GUI/agent
  handles them through `InterruptByID`, `ReconnectByID`, and `RemoveSession`.
- Health management: shell responsiveness probes, Ctrl+C recovery, connection
  pooling, keepalive, failure counting, and session rebuilding stay in
  GUI/core. Mobile only surfaces the state and requests manual control actions.
- Backend task management: mobile can request GUI-equivalent
  `exec_background`, `check_task`, `wait_task`, `list_tasks`, and `kill_task`
  operations through Hub control records. The GUI/agent creates the background
  task, owns the task ID, streams status/log tails back to Hub, and records the
  same `backend_session_id`.
- File operations: mobile can request GUI-equivalent `upload`, `download`, and
  remote `stat`/`list` operations only through the backend session path. The
  GUI/agent performs SFTP with its local SSH credentials and publishes sanitized
  progress/result metadata back to Hub. `local_path` always names a path on the
  claimed GUI/agent machine, never a phone file. Hub and mobile reject
  `upload`/`download` requests unless both `local_path` and `remote_path` are
  supplied; `stat`/`list` require `remote_path`.

## Required Mobile Capabilities

- Server profile list synced from MaClaw GUI/agent with tag, note, auth mode,
  and tenant-scoped sanitized metadata.
- Create or attach backend SSH session for a server profile.
- Session list/status surface showing backend session ID, host/profile linkage,
  alive/responsive state, last activity, and reconnect state.
- Stream backend session output through Hub realtime/WebSocket.
- Send user-confirmed input to the Hub control record for the claimed backend
  session; the desktop/agent applies it to the managed SSH session.
- Request backend background tasks for long-running commands, then view task
  IDs, status, log tails, completion, and kill results reported by the
  GUI/agent.
- Request backend file list/upload/download operations for emergency evidence
  collection or deployment support, with paths and result metadata shown on
  mobile but SSH credentials and SFTP execution kept on the GUI/agent side.
- Use risk classification before sending dangerous commands.
- Copy/redact session output before AI analysis.
- Ask AI for explanation and command drafts only; high-risk commands remain
  manual-confirm and never auto-executed by default.
- Hand backend session output or pasted/copied backend session output to a
  digital employee only after Hub/tenant warning confirmation, with sanitized
  server profile metadata and active backend session ID included for traceability.

## API Shape

Tenant Hub mobile-facing endpoints:

- `GET /api/mobile/server-profiles`
- `GET /api/mobile/ssh/sessions`
- `POST /api/mobile/ssh/sessions`
- `POST /api/mobile/ssh/sessions/{session_id}/attach`
- `POST /api/mobile/ssh/sessions/{session_id}/input`
- `GET /api/mobile/ssh/sessions/{session_id}/tasks`
- `POST /api/mobile/ssh/sessions/{session_id}/tasks`
- `GET /api/mobile/ssh/sessions/{session_id}/tasks/{task_id}`
- `POST /api/mobile/ssh/sessions/{session_id}/tasks/{task_id}/wait`
- `POST /api/mobile/ssh/sessions/{session_id}/tasks/{task_id}/kill`
- `POST /api/mobile/ssh/sessions/{session_id}/files`
- `POST /api/mobile/ssh/sessions/{session_id}/interrupt`
- `POST /api/mobile/ssh/sessions/{session_id}/reconnect`
- `DELETE /api/mobile/ssh/sessions/{session_id}`
- realtime event types for session output, status, disconnect, reconnect, and
  abnormal-state notifications.

Machine-authenticated desktop/agent worker endpoints:

- `PUT /api/mobile/server-profiles` publishes sanitized desktop `SSHHosts`
  metadata for the signed-in tenant. This is not a phone-side credential or
  server-profile editor.
- `POST /api/mobile/ssh/sessions/claim`
- `PATCH /api/mobile/ssh/sessions/{session_id}/worker`
- `POST /api/mobile/ssh/tasks/claim`
- `PATCH /api/mobile/ssh/tasks/{task_id}/worker`
- `POST /api/mobile/ssh/files/claim`
- `PATCH /api/mobile/ssh/files/{operation_id}/worker`

Those worker endpoints are the mobile bridge for the GUI's backend SSH
management ability: the phone creates or updates control records, while the
authorized GUI/agent claims session, background-task, and file-operation work
and executes it through the existing desktop-side managers.

Phone-side removal of a server profile only clears local cached metadata and
legacy local credential residue. It does not delete the MaClaw GUI/agent source
profile or any SSH credential held by the desktop/agent.

## Current State And Remaining Gap

The Flutter server screen no longer creates a phone-local SSH socket or depends
on `dartssh2`. It now calls Hub-style backend session APIs to create/attach,
interrupt, reconnect, send input, and close a managed session.

The tenant Hub now exposes the mobile session API and worker-side claim/update
contract. Mobile-created sessions, input, attach, interrupt, reconnect, and
close requests are represented as backend control records; authorized
desktop/agent workers claim those records with machine authentication and report
status, recent output, and the bound GUI/core backend session ID.

The desktop GUI remote client now polls for backend SSH session claims and
binds them to the existing `SSHSessionManager` using configured desktop
`SSHHosts`. It can create/reuse a managed SSH session, apply queued
mobile-confirmed input, send Ctrl+C interrupts, reconnect, close, and report
results back through Hub.

The desktop GUI remote client also polls for backend SSH task and file-operation
control records. `pollMobileBackendSSHTasksOnce` claims mobile-created
`exec_background`, `wait_task`, and `kill_task` requests through the
machine-authenticated worker path, then `processMobileBackendSSHTask` maps the
mobile task ID to the GUI/core `SSHBackgroundTaskManager` task ID, executes
through the existing desktop-managed SSH session, and reports log tails, status,
and exit code metadata back to Hub. `wait_requested` uses the bounded default
wait window when mobile does not provide a timeout, while ordinary `running`
polls refresh status once so the desktop worker loop remains responsive.
`pollMobileBackendSSHFileOperationsOnce` and
`processMobileBackendSSHFileOperation` run file control records through the
same GUI/agent-owned backend session: upload/download use the existing
`SFTPTransfer` path, while remote `stat` and `list` use read-only commands on
the managed session and return sanitized output through the Hub worker record.
This keeps backend task execution, SFTP credentials, and remote file access on
the authorized MaClaw GUI/agent side rather than the phone.

Mobile realtime parsing now recognizes `ssh_session` events and caches backend
SSH session status/output updates for the server surface. Hub worker updates
now include `output_chunk` and `output_seq` fields, and the desktop worker can
continue claiming its active backend SSH sessions so it can report incremental
backend session output rather than only the first connection snapshot.

Server-profile sync is now available in the first direction needed for mobile
emergency use: the desktop worker publishes sanitized `SSHHosts` metadata to
Hub with machine authentication, and mobile pulls `GET /api/mobile/server-profiles`
to merge those profiles with local emergency entries. The sync intentionally
excludes passwords, private keys, passphrases, and other secret material.

Remaining release-critical gaps:

- Finish physical device QA for weak networks, reconnect, high-risk command
  confirmation, and real desktop/agent SSH session handoff.
