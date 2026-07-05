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
calls use the credits bound to that phone account.

MaClaw Mobile does not embed or directly call the desktop/server Go `corelib`.
Heavy MaClaw capabilities stay on the official Hub or on authorized remote
server/desktop digital employees. The phone accesses those capabilities through
the discovered Hub APIs, realtime updates, and explicit digital employee task
handoff.

## Startup And LLM Setup

1. Open the app and wait on the MaClaw logo splash screen.
2. If no mobile session exists, sign in with phone number and SMS verification.
3. If a valid mobile session and LLM access are already configured, the app
   opens the assistant directly.
4. If LLM access is missing, configure one of the supported access paths:
   MaClaw official service redemption code followed by phone SMS verification,
   or the provider QR code generated from the LLM configuration screen in
   MaClaw desktop GUI.
5. After setup succeeds, the app opens the multi-tab assistant.

MaClaw official LLM access is the default. The redemption code can help route
the official service setup, but it does not replace the mobile account: after
SMS verification succeeds, official LLM calls use the credits bound to the
verified `phone:<digits>` account. Third-party LLM access is allowed only when
it is authorized by scanning or pasting the MaClaw GUI QR payload. The mobile
app does not accept arbitrary third-party LLM endpoints.

## Look Up Information

Use the `查信息` tab to ask current-information questions.

- Type a question and tap `联网查询`.
- Tap the microphone to dictate a question.
- Tap the camera, gallery screenshot/image, or attachment buttons to send a
  photo, screenshot, or file into the document parsing flow.
- Share text or URLs from another app into MaClaw Mobile.
- Review source citations before sharing or turning the result into a document.
- Tap `整理为草稿` in the result card and choose a document template.
- Query result copy/share/draft actions and copied/shared citations redact
  common passwords, tokens, private key blocks, and credential URLs before
  content leaves the current result view.
  Recent search history stores only a locally redacted answer preview, while
  the current result view remains available for review before externalizing.

Star frequent questions to keep them in the `常用问题` section. Recent
non-starred searches remain in `最近查询` and can be cleared separately.

The assistant supports a primary tab and secondary tabs so urgent searches can
stay separated. Shared URLs are preserved as citations even when no extra search
result is available.

## Create Emergency Documents

Use the `文档` tab to create, import, edit, process, export, and share urgent
documents.

- Create a draft from templates: notice, report, email, proposal, meeting
  minutes, or statement.
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

Use the `远程` tab for manual SSH maintenance.

- Add server profiles with host, port, username, auth mode, tag, and note.
- Store SSH passwords and private-key passphrases in system secure storage.
- Connect manually, copy terminal output, or send recent output to AI analysis.
- Paste terminal output or logs into the analysis panel and hand them to an
  online digital employee when remote server/desktop capabilities are needed.
- When terminal output comes from a saved server profile, digital employee
  handoff includes non-secret server metadata such as name, host, port, user,
  tag, note, and auth mode.
- Save common commands and command history. The executable command is preserved
  for reuse, while the saved list label redacts common passwords, tokens,
  private key blocks, and credential URLs.
- High-risk command drafts require confirmation before being saved.
- Deleting a server profile clears its saved SSH credentials.

AI provides explanations and command drafts; it does not automatically execute
commands.

Before terminal output is sent to AI or submitted as a digital employee task,
MaClaw Mobile shows a confirmation with line/character counts, a preview, and a
local redaction for common passwords, tokens, private key blocks, and credential
URLs. Still review the preview and remove customer data or unusual secrets
before sending.

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
- From the server maintenance screen, hand pasted SSH output or logs to a
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
  connectivity returns, a restored banner confirms search, document, and task
  status checks can continue.
- Review privacy notes for tokens and SSH credentials.
- Clear local work records without deleting server profiles or SSH credentials.
- Clear server profiles and SSH credentials separately when device access should
  be revoked.

Clearing local work records resets search history, the latest document draft,
document tasks, command history, digital employee prompt/task history, and app
preferences. Server profiles and SSH credentials are managed by the separate
server-access cleanup action.

When older local records are migrated into the SQLite cache, MaClaw Mobile also
redacts common secrets from search answer previews, saved command labels, and
digital employee prompt history.
