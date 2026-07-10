# MaClaw Mobile Release Audit

Use this audit with `release_checklist.md` and `release_evidence.md`. It records
which checklist items are already proven by automated evidence and which remain
manual release gates. Do not mark a release complete until every `Manual gate`
item has real signed-build, real-device, or Hub discovery smoke-test
evidence.

Use `qa_device_checklist.md` to execute and record those manual gates. Completed
signed-build QA records must pass `python3 tool/validate_qa_build_record.py`
without secret redaction failures before the audit can count them as release
evidence. If validation fails, run
`python3 tool/qa_build_record_report.py docs/qa-builds/<record>.md` and replace
raw secrets with redacted evidence, attachment IDs, task IDs, artifact hashes,
or reviewer notes.

Status legend:

- `Automated evidence`: covered by tests, static checks, generated wrapper
  checks, or local build output recorded in `release_evidence.md`.
- `Manual gate`: cannot be proven by local tests or an unsigned debug APK.
- `Partial`: automated evidence exists, but a real-device or signed-build check
  is still required before release.

## Service

| Checklist item | Status | Evidence needed |
| --- | --- | --- |
| App uses exactly three preset official HubCenters and discovers the user's Hub/tenant | Automated evidence | `test/official_service_test.dart`, `test/auth_service_test.dart`, `test/session_state_test.dart`, `go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)|TestSameURLOriginHandlesDefaultPorts" -count=1` |
| No custom Hub URL setting in UI, storage, or API client | Automated evidence | `test/official_service_surface_test.dart`, `test/official_service_test.dart` |
| Mobile account login uses phone number plus SMS verification only, resolves Hub/tenant through HubCenter, and signs in with the discovered Hub token | Automated evidence | `test/auth_service_test.dart`, `test/login_screen_test.dart`, `test/app_smoke_test.dart` |
| Verified phone accounts use MaClaw official LLM credits bound to `phone:<digits>` | Automated evidence | `test/auth_service_test.dart`, `test/mobile_bootstrap_test.dart`, `test/login_screen_test.dart`, `test/app_smoke_test.dart`; real Hub credit balance remains part of Hub discovery smoke |
| Account page shows official LLM credits, model, service group, and discovered Hub status path | Automated evidence | `test/api_client_test.dart`, `test/account_screen_test.dart` |
| Login starts at HubCenter; bootstrap, assistant online, document, SSH analysis, and digital employee APIs use the discovered Hub | Automated evidence | `test/auth_service_test.dart`, `test/mobile_api_contract_test.dart`, `go test ./hub/internal/httpapi -run "TestMobile.*" -count=1` |
| Third-party LLM access requires desktop GUI QR authorization evidence | Automated evidence | `test/auth_service_test.dart`, `tool/validate_qa_build_record_test.py` |
| Flutter app does not embed or directly call Go `corelib`; core capabilities stay behind Hub/digital employees | Automated evidence | `tool/verify_runtime_boundary.py`, `tool/verify_runtime_boundary_test.py`, `test/release_docs_test.dart`, `README.md`, `docs/user_guide.md`, `docs/release_checklist.md` |
| Export downloads reject absolute URLs outside the discovered Hub | Automated evidence | `test/official_service_test.dart`, `test/api_client_test.dart`, `test/documents_state_test.dart` |
| Realtime WebSocket resolves only against the discovered Hub | Automated evidence | `test/mobile_realtime_client_test.dart` |

## Android

| Checklist item | Status | Evidence needed |
| --- | --- | --- |
| Native wrappers exist or are regenerated | Automated evidence | `flutter create --platforms android,ios .`, `python tool/configure_platforms.py` |
| Android manifest has camera, microphone, notification, internet, media/file read, deep link, and share intent entries | Automated evidence | `test/platform_permissions_test.dart` |
| Debug APK builds before QA handoff | Automated evidence | `flutter build apk --debug`, artifact path and SHA256 in `release_evidence.md` |
| Android 13+ notification permission can be requested from account screen | Partial | `test/account_screen_test.dart`, plus real Android 13+ permission prompt evidence |
| Signed internal package installs and share-to-app works for text, URLs, images, PDFs, Word, Excel, and CSV | Manual gate | Signed APK/AAB, SHA256, signing identity, version/build number, installer channel, install result, and real-device share notes |

## iOS

| Checklist item | Status | Evidence needed |
| --- | --- | --- |
| `tool/configure_platforms.py` runs | Automated evidence | `python tool/configure_platforms.py` |
| Share Extension files, file/image activation rules, and bundle/app-group wiring are generated | Partial | `test/platform_permissions_test.dart`, plus Xcode signed target evidence |
| URL schemes include `maclaw` and `ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER)` | Automated evidence | `test/platform_permissions_test.dart` |
| `Info.plist` contains readable camera, microphone, speech recognition, photo library, and local network usage descriptions | Automated evidence | `test/platform_permissions_test.dart`, `tool/configure_platforms_test.py` |
| Camera, microphone, speech recognition, photo library, local network, and notification prompts work on device | Manual gate | iOS real-device or TestFlight QA notes/screenshots |

## User Workflows

| Checklist item | Status | Evidence needed |
| --- | --- | --- |
| Login with phone number and SMS verification through HubCenter discovery | Automated evidence | `test/auth_service_test.dart`, `test/login_screen_test.dart`, `test/app_smoke_test.dart`; real SMS and Hub discovery smoke remain manual |
| Cold start shows the MaClaw logo splash screen and no Flutter placeholder/template branding | Partial | `test/app_smoke_test.dart`, `test/release_docs_test.dart`, native launch asset checks, plus signed-build cold-start screenshot or recording |
| Signed-in launch opens the GUI-like `AI助手` first tab with `主对话`/secondary-tab controls, visible voice input, and no legacy `查信息` entry | Partial | `test/app_smoke_test.dart`, `test/assistant_screen_test.dart`, `test/mobile_feature_flags_test.dart`, plus signed-build real-device first-screen evidence |
| AI assistant shows citations, shares results, separates starred frequent questions from recent history, and turns results into each document template type | Automated evidence | `test/assistant_screen_test.dart`, `test/assistant_retry_test.dart` |
| AI assistant result copy/share/draft text and citation copy/share text redact common secrets before externalizing content | Automated evidence | `test/assistant_screen_test.dart` |
| AI assistant history stores locally redacted answer previews | Automated evidence | `test/assistant_screen_test.dart` |
| Voice transcript and photo/image/screenshot assistant input produce cited answers or document tasks, including photo/image assistant input evidence | Partial | `test/assistant_screen_test.dart`, `test/platform_permissions_test.dart`, `tool/validate_qa_build_record_test.py`; real-device voice/photo smoke remains manual |
| Shared URL remains a citation fallback when assistant online access returns no extra source | Automated evidence | `test/assistant_screen_test.dart`, `test/mobile_shared_intent_test.dart` |
| Photo, gallery screenshot/image, or file import from assistant enters document parsing flow | Automated evidence | `test/assistant_screen_test.dart` |
| Document create, import long-running task guidance, edit, table/comment insertion, AI processing, export, and share PDF/Word/Markdown | Automated evidence | `test/documents_screen_test.dart`, `test/documents_state_test.dart` |
| Server profile with tag/note, backend SSH session creation/attach, session list/status, GUI/agent-bound `backend_session_id`, GUI/agent claim or worker handoff evidence, explicit worker claim/update evidence, SSH realtime incremental output evidence through `ssh_session` `output_chunk`/`output_seq`, interrupt/Ctrl+C evidence, read-only command output excerpt, disconnect result, reconnect result, copied backend session output evidence with a GUI/agent evidence line containing actual values for Hub session ID, `backend_session_id`, concrete `claimed_by` worker identity such as `claimed_by desktop-agent-1`, and numeric `output_seq`, AI analysis and AI/digital-employee handoff tied to the same GUI/agent-bound `backend_session_id`, and phone-side server-profile cache clearing | Partial | `test/servers_screen_test.dart`, `test/servers_controller_test.dart`, `test/backend_ssh_command_test.dart`, `test/mobile_realtime_client_test.dart`, `tool/validate_qa_build_record_test.py`, `go test ./hub/internal/httpapi -run "TestMobile.*(SSH|BackendSSH|RealtimeBackendSSH)" -count=1`, `go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession" -count=1`; automated coverage now uses GUI-style backend session management, while real-device desktop/agent handoff remains manual |
| Backend session output/logs can be handed to online digital employee with Hub/tenant sensitive-data warning and server-profile metadata when available | Automated evidence | `test/servers_screen_test.dart`, `test/digital_employees_screen_test.dart` |
| Typed digital employee task creates mobile emergency prompt, includes remote policy/status context, polls, copies/shares result, and keeps remote authorization rules | Automated evidence | `test/digital_employees_screen_test.dart`, `test/digital_employee_test.dart`, `test/digital_employees_controller_test.dart`, `go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession" -count=1` |
| Document/export, digital employee, and SSH abnormal notifications are delivered with typed payload/open evidence, opened payloads route back to the matching mobile tab with a recovery prompt, and document/digital employee notification messages redact common secrets | Partial | `test/mobile_notification_service_test.dart`, `test/app_smoke_test.dart`, `test/documents_state_test.dart`, `test/digital_employees_controller_test.dart`, `test/servers_screen_test.dart`, `tool/validate_qa_build_record_test.py`; real-device notification delivery remains manual |
| Offline or weak-network warnings appear and Hub services recover after connectivity returns | Partial | `test/mobile_network_status_test.dart`, `test/assistant_retry_test.dart`, `test/mobile_realtime_client_test.dart`, `tool/validate_qa_build_record_test.py`; real Hub/network recovery smoke remains manual |
| Hub smoke records network offline/recovery evidence for the same tenant session | Partial | `test/mobile_network_status_test.dart`, `test/assistant_retry_test.dart`, `test/mobile_realtime_client_test.dart`, plus real Hub/network recovery smoke |
| Theme and speech language can be changed from account screen | Automated evidence | `test/account_screen_test.dart`, `test/app_preferences_test.dart` |
| Clear local work records without deleting cached server-profile metadata | Automated evidence | `test/account_screen_test.dart`, `test/mobile_local_store_test.dart` |
| Clear server-profile caches separately from work records | Automated evidence | `test/account_screen_test.dart`, `test/mobile_local_store_test.dart`, `test/servers_controller_test.dart` |

## Safety

| Checklist item | Status | Evidence needed |
| --- | --- | --- |
| High-risk SSH commands are saved only after explicit confirmation | Automated evidence | `test/servers_screen_test.dart`, `test/ssh_risk_test.dart` |
| AI SSH analysis returns command drafts, not auto-executed commands | Automated evidence | `test/servers_screen_test.dart`, `test/mobile_api_contract_test.dart` |
| Server command history preserves executable commands but redacts saved list labels/previews | Automated evidence | `test/servers_controller_test.dart` |
| Local SQLite and legacy JSON cache migration redact preview/history fields that may contain old secrets | Automated evidence | `test/mobile_local_store_test.dart` |
| Backend session output is sent to AI only after confirmation, preview, warning, and local redaction of common secrets | Automated evidence | `test/servers_screen_test.dart` |
| Backend session output is submitted to digital employees only after Hub/tenant handoff confirmation, preview, warning, and local redaction of common secrets | Automated evidence | `test/servers_screen_test.dart`, `test/digital_employees_screen_test.dart` |
| Digital employee task results are locally redacted before copy, share, or document-draft export | Automated evidence | `test/digital_employees_screen_test.dart`, `test/servers_screen_test.dart` |
| Digital employee prompt history redacts common secrets before being stored for later reuse | Automated evidence | `test/digital_employees_controller_test.dart` |
| Digital employee task notification messages are locally redacted before appearing in the system tray | Automated evidence | `test/digital_employees_controller_test.dart` |
| Document task notification bodies are locally redacted before appearing in the system tray | Automated evidence | `test/documents_state_test.dart` |
| AI assistant result and citation externalization redacts common secrets before copy, share, or document-draft export | Automated evidence | `test/assistant_screen_test.dart` |
| AI assistant history previews redact common secrets before being stored for later reuse | Automated evidence | `test/assistant_screen_test.dart` |
| SSH passwords, private keys, and passphrases stay on the authorized MaClaw GUI/agent side; login tokens stay in secure storage | Automated evidence | `test/secure_vault_test.dart`, `test/server_profile_test.dart` |
| Deleting phone-side server profiles clears cached metadata and legacy credential residue | Automated evidence | `test/servers_controller_test.dart`, `test/secure_vault_test.dart` |
| Clearing local work records does not delete cached server-profile metadata | Automated evidence | `test/account_screen_test.dart`, `test/mobile_local_store_test.dart` |
| Clearing server-profile caches is separate and explicit | Automated evidence | `test/account_screen_test.dart`, `test/mobile_local_store_test.dart` |

## CI And Build Gates

| Checklist item | Status | Evidence needed |
| --- | --- | --- |
| Go mobile API tests pass | Automated evidence | `go test ./hub/internal/httpapi -run "TestMobile.*" -count=1` |
| Go mobile HubCenter discovery tests pass | Automated evidence | `go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)|TestSameURLOriginHandlesDefaultPorts" -count=1` |
| Go mobile GUI/digital employee tests pass | Automated evidence | `go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession" -count=1` |
| Platform configuration tests pass | Automated evidence | `python tool/configure_platforms_test.py` |
| Runtime boundary verifier passes | Automated evidence | `python tool/verify_runtime_boundary.py`, `python -m unittest tool/verify_runtime_boundary_test.py` |
| Flutter analysis and tests pass | Automated evidence | `flutter analyze`, `flutter test --concurrency=1` |
| Android debug APK builds | Automated evidence | `flutter build apk --debug` |
| Signed Android/iOS release candidates are available | Manual gate | Signed Android artifact and iOS archive/TestFlight build records |

## Remaining Release Blockers

These items are intentionally not closed by local automation:

- Signed Android internal APK/AAB with install result on at least one Android
  13+ device.
- Android real-device share-to-app for text, URL, image, PDF, Word, Excel, and
  CSV.
- Android runtime permission prompts for notification, camera, microphone,
  media/file access, and any platform local-network prompt if applicable, with
  `permission-grant:<id>` evidence. Local-network evidence must not be used as
  phone-local SSH proof; backend SSH remains GUI/agent-managed.
- iOS signed Runner and Share Extension target with official Team ID,
  provisioning profile, and app-group entitlement.
- iOS real-device/TestFlight share-to-app for text, URL, image, PDF, Word,
  Excel, and CSV.
- iOS runtime permission prompts for camera, microphone, speech recognition,
  photo library, local network, and notifications, with `permission-grant:<id>`
  evidence.
- Real backend SSH session smoke test against a server, covering
  GUI-equivalent backend SSH session management, including host type, auth
  mode, session creation/attach,
  backend-managed session proof that is
  not phone-local, actual `claimed_by` worker identity, GUI/agent claim or worker
  handoff evidence, explicit worker claim/update evidence, `ssh_session` realtime
  `output_chunk`/`output_seq` evidence, interrupt/Ctrl+C evidence
  through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt`
  with GUI/agent Ctrl+C handling, read-only command, command output excerpt,
  disconnect result, reconnect result, copied backend session output evidence,
  AI analysis confirmation tied to the same GUI/agent-bound
  `backend_session_id`, AI/digital-employee handoff evidence tied to the same
  `backend_session_id` if used, and phone-side server-profile cache clear
  confirmation.
- Hub discovery smoke test with account, selected HubCenter, discovered Hub,
  tenant, LLM mode/QR authorization evidence with post-SMS-verification official credits usage record ID, bootstrap, cold-start MaClaw logo
  splash evidence with no Flutter placeholder/template branding, signed-in
  `AI助手` first-screen evidence with visible `主对话`/secondary-tab controls,
  microphone/voice input, and no legacy `查信息` entry, AI assistant query with
  citations, voice transcription, photo/image assistant input, shared result,
  document draft, document upload/export,
  digital employee task, realtime status, notification delivery, network
  offline/recovery, API base URL, and realtime Hub URL confirmation.

Record these results with `docs/qa_device_checklist.md`.
