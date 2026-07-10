# MaClaw Mobile User Guide

MaClaw Mobile is for emergency work when a desktop is not available. It is not a
mobile copy of MaClaw GUI: programming tools, heavy workflows, admin consoles,
and full desktop-grade Office layout editing are intentionally out of scope.

The app connects only through the three preset official HubCenter endpoints:
`https://hubs.mypapers.top`, `https://hubs.maclaw.top`, and
`https://hubs2.maclaw.top`. It probes those endpoints, discovers your Hub and
tenant through the selected HubCenter, then connects through your Hub. The phone
does not support custom Hub URLs.

Mobile account access uses phone-number verification only. Enter a phone number,
receive the SMS verification code from the discovered Hub, and complete login.
If the phone number is new, verification creates the phone account and signs in;
if it already exists, verification signs in to that account. MaClaw official LLM
calls use the credits bound to that phone account. The mobile app does not
expose a redemption-code login path.

MaClaw Mobile does not embed or directly call the desktop/server Go `corelib`.
Heavy MaClaw capabilities stay on the official Hub or on authorized remote
server/desktop digital employees. The phone accesses those capabilities through
the discovered Hub APIs, realtime updates, and explicit digital employee task
handoff.

## Startup And Account Login

1. Open the app and wait on the MaClaw logo splash screen.
2. If no mobile session exists, the first screen is phone registration/login.
   Enter the phone number and request the SMS verification code through the
   discovered Hub.
3. After SMS verification succeeds, a new phone number is registered and signed
   in automatically; an existing phone number signs in to that account.
4. The app then opens the multi-tab assistant and uses the verified
   `phone:<digits>` account's MaClaw official credits for LLM calls.

MaClaw official LLM access is the default after phone verification. Third-party
LLM access is optional and lives under the account/settings area. It is allowed
only when authorized by scanning or pasting the provider QR code generated from
the LLM configuration screen in MaClaw desktop GUI. The mobile app does not
accept arbitrary third-party LLM endpoints, provider base URLs, or API keys.

## AI Assistant

Use the `AI助手` tab as the mobile MaClaw assistant, similar to the MaClaw GUI
assistant but adapted for short mobile sessions. It is not a search-only page:
current-information checking is one assistant capability alongside normal
conversation, voice input, screenshot/photo questions, document handoff,
server-log analysis, and digital employee task handoff.

The assistant is the primary signed-in workspace. Optional bootstrap feature
flags can hide documents, backend SSH session management, digital employees, or push notification
capabilities, but they do not remove the `AI助手` entry, even if Hub sends `assistant:false`, or make digital
employees the default landing page.
If the current Hub disables assistant online access, the assistant keeps the
workspace open, disables `发送给 AI 助手`, and explains that voice input, image/file
handoff, and document drafting remain available.

- Type a question and tap `发送给 AI 助手`.
- Tap the microphone to dictate a question; recognized speech fills the same
  assistant input box and can be edited before sending.
- Tap a quick prompt such as `助手联网`, `文档草稿`, or `日志排障` to fill a
  phone-friendly emergency question, then review or edit it before querying.
- Tap the camera, gallery screenshot/image, or attachment buttons to send a
  photo, screenshot, or file into the document parsing flow.
- Share text or URLs from another app into MaClaw Mobile.
- Review source citations before sharing or turning the result into a document.
- Tap `整理为草稿` in the result card and choose a document template.
- Query result copy/share/draft actions and copied/shared citations redact
  common passwords, tokens, private key blocks, and credential URLs before
  content leaves the current result view.
  Recent assistant history stores only a locally redacted answer preview, while
  the current result view remains available for review before externalizing.

Star frequent questions to keep them in the `常用问题` section. Recent
non-starred assistant conversations remain in `最近对话` and can be cleared separately.

The assistant supports a primary tab and secondary tabs so urgent conversations
can stay separated. Shared URLs are preserved as citations even when no extra
assistant result is available.

## Create Emergency Documents

Use the `文档` tab to create, import, edit, process, export, and share urgent
documents.

- Create a draft from templates: notice, report, email, proposal, meeting
  minutes, or statement.
- Template selection fills a phone-friendly emergency skeleton, such as facts,
  risks, actions, owners, deadlines, or meeting decisions. Editing your own
  content prevents later template changes from overwriting it.
- Import Word, PDF, Excel, text, Markdown, log, CSV, JSON, or image files.
- Images enter the OCR/vision parsing task flow.
- Long-running import and parsing tasks can continue after leaving the page;
  completion is surfaced by notification or by returning to the document page.
- Document processing, import, and export notifications redact common secrets
  from titles, filenames, and Hub task messages before they appear in the
  system notification tray.
- Use AI actions for summarize, translate, rewrite, expand, polish, and format.
- Use the lightweight editor to change title/body, insert a table, or add a
  comment.
- Export to PDF, Word, or Markdown, then share through the system share sheet.

Complex desktop-grade Office layout editing is intentionally out of scope.

## Maintain Servers

Use the `远程` tab for GUI-like backend SSH session management. Mobile should
control sessions created and managed by an agent/backend session manager, not
act as a standalone phone-local SSH terminal client.

The mobile foreground assistant acts as a foreground agent for emergency
maintenance: it can help choose a server profile, explain a problem, and create
or attach the Hub control record for the backend SSH session manager. The real
session becomes active only after an authorized MaClaw GUI/agent worker claims
that record and manages the session through the desktop SSHSessionManager.

Implementation details and the GUI reference model are tracked in
`docs/backend_ssh_session_design.md`.

- Sync server profiles published by MaClaw GUI/agent through the official Hub.
  The phone displays sanitized host, port, username, auth mode, tag, and note
  metadata, but does not collect SSH passwords, private keys, or passphrases.
- Create or attach a backend SSH session owned by MaClaw GUI/agent and backed by
  the existing `SSHSessionManager`; the session can be listed, reconnected,
  checked for shell responsiveness, and reused across agent actions.
- Start GUI/agent-managed background server tasks for long-running commands,
  then check, wait for, list, or kill those tasks from mobile through Hub
  control records.
- Request backend file listing, upload, and download operations through the
  GUI/agent SFTP path. The phone shows sanitized paths, progress, and results;
  SSH credentials and file-transfer execution remain on the authorized desktop
  or server agent.
- Copy backend session output, or send recent output to AI analysis.
- Paste backend session output or logs into the analysis panel and hand them to
  an online digital employee when remote server/desktop capabilities are needed.
- When backend session output comes from a saved server profile, digital
  employee handoff includes non-secret server metadata such as name, host, port,
  user, tag, note, and auth mode.
- Save common commands and command history. The executable command is preserved
  for reuse, while the saved list label redacts common passwords, tokens,
  private key blocks, and credential URLs.
- High-risk command drafts require confirmation before being saved.
- Removing a server profile from the phone clears the local cached profile and
  any legacy local credential residue; active SSH credentials remain managed by
  the authorized desktop/agent side.

AI provides explanations and command drafts; it does not automatically execute
commands. High-risk operations remain draft/manual-confirm even when an agent
created the backend SSH session.

Before backend session output is sent to AI or submitted as a digital employee
task, MaClaw Mobile shows a confirmation with line/character counts, a preview,
and a local redaction for common passwords, tokens, private key blocks, and
credential URLs. Still review the preview and remove customer data or unusual
secrets before sending.

## Use Digital Employees

Use the `员工` tab to access remote server or desktop capabilities.

- Refresh the employee list.
- Open an online digital employee and submit a task.
- Choose a mobile task type so the remote employee receives a structured
  emergency task brief with phone-friendly output requirements.
- The submitted task context includes the mobile emergency source, remote
  machine ID, online status, access policy, residency, runtime state, and
  manual-confirmation requirement so the remote side can enforce its own rules.
- Use built-in templates for system status, logs, resource checks, or file
  summaries.
- From the server maintenance screen, hand backend session output or logs to a
  digital employee for remote-side investigation.
- Poll task status, then copy, share, or turn the result into a document draft.
  Result copy/share/draft actions locally redact common passwords, tokens,
  private key blocks, and credential URLs before content leaves the app surface.
- Digital employee prompt history is also stored with common passwords, tokens,
  private key blocks, and credential URLs redacted locally.
- Digital employee completion/failure notifications also redact common secrets
  from remote task messages before they appear in the system notification tray.

Remote-side authorization still applies. The phone cannot bypass approval,
private access policies, or high-risk execution rules. By default, mobile
digital employee tasks ask remote workers to return high-risk commands as drafts
for manual confirmation instead of executing them automatically.

## Account, Privacy, And Settings

Use the `我的` tab for service and local settings.

- Confirm the selected HubCenter, discovered Hub, tenant, LLM access mode, and
  account binding. Phone accounts are shown with a masked `phone:<number>`
  identity, a MaClaw official credits ownership hint, and the current official
  LLM credits status when the Hub status endpoint is reachable.
- Use MaClaw official LLM access by default, or scan the QR code generated by
  MaClaw desktop GUI to authorize third-party LLM access.
- Check feature flags and upload/export limits.
- Choose system, light, or dark theme.
- Choose the speech input language.
- Request notification permission for long-running task and SSH alerts. When a
  task notification is opened, the app consumes its typed payload, routes known
  document, digital employee, or server alerts back to the matching mobile tab,
  and shows a recovery prompt for the relevant task or status screen.
- Offline warnings appear across mobile work screens; when official service
  connectivity returns, a restored banner confirms assistant online, document,
  and task status checks can continue.
- Review privacy notes for tokens, server-profile metadata, and backend session
  output.
- Clear local work records without deleting cached server-profile metadata.
- Clear server-profile caches separately when device access should be revoked.

Clearing local work records resets assistant history, the latest document draft,
document tasks, command history, digital employee prompt/task history, and app
preferences. Server-profile caches are managed by the separate server-access
cleanup action; real SSH credentials stay on the authorized desktop/agent side.

When older local records are migrated into the SQLite cache, MaClaw Mobile also
redacts common secrets from assistant answer previews, saved command labels, and
digital employee prompt history.
