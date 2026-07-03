import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  String readDoc(String path) => File(path).readAsStringSync();
  String releaseDocCorpus() => [
        readDoc('README.md'),
        readDoc('docs/user_guide.md'),
        readDoc('docs/release_checklist.md'),
        readDoc('docs/release_evidence.md'),
        readDoc('docs/release_audit.md'),
        readDoc('docs/qa_device_checklist.md'),
        readDoc('docs/qa_build_record_template.md'),
        readDoc('docs/qa-builds/README.md'),
      ].join('\n');

  void expectInOrder(String text, List<String> expectedParts) {
    var cursor = -1;
    for (final part in expectedParts) {
      final index = text.indexOf(part, cursor + 1);
      expect(
        index,
        isNonNegative,
        reason: '$part should appear after index $cursor',
      );
      cursor = index;
    }
  }

  File localFile(String path) {
    if (path.startsWith('.github/')) {
      return File('../../$path');
    }
    if (path == '.gitignore') {
      return File('../../.gitignore');
    }
    if (path == 'README.md') {
      return File(path);
    }
    return File(path);
  }

  bool localPathExists(String path) {
    final file = localFile(path);
    return file.existsSync() || Directory(file.path).existsSync();
  }

  const lookupTab = '\u67e5\u4fe1\u606f';
  const documentsTab = '\u6587\u6863';
  const remoteTab = '\u8fdc\u7a0b';
  const employeesTab = '\u5458\u5de5';
  const accountTab = '\u6211\u7684';

  test('release docs cross-link checklist evidence audit and QA steps', () {
    final readme = readDoc('README.md');
    final evidence = readDoc('docs/release_evidence.md');
    final audit = readDoc('docs/release_audit.md');
    final qa = readDoc('docs/qa_device_checklist.md');

    for (final docName in [
      'docs/release_checklist.md',
      'docs/release_evidence.md',
      'docs/release_audit.md',
      'docs/qa_device_checklist.md',
      'docs/qa_build_record_template.md',
      'docs/qa-builds/README.md',
    ]) {
      expect(readme, contains(docName));
    }
    expect(evidence, contains('docs/release_audit.md'));
    expect(evidence, contains('docs/qa_device_checklist.md'));
    expect(evidence, contains('docs/qa_build_record_template.md'));
    expect(evidence, contains('docs/qa-builds/README.md'));
    expect(audit, contains('qa_device_checklist.md'));
    expect(qa, contains('release_evidence.md'));
    expect(qa, contains('qa_build_record_template.md'));
    expect(qa, contains('docs/qa-builds/README.md'));
    expect(qa, contains('tool/validate_qa_build_record.py'));
    expect(qa, contains('tool/qa_build_record_report.py'));
    expect(qa, contains('tool/qa_release_evidence_links.py'));
    expect(qa, contains('tool/qa_preflight.py'));
    expect(qa, contains('tool/release_status_report.py'));
    expect(releaseDocCorpus(), contains('tool/create_qa_build_record.py'));
    expect(qa, contains('tool/validate_qa_build_records_dir.py'));
    expect(qa, contains('tool/verify_final_release_evidence.py'));
  });

  test('user guide preserves mobile product decisions', () {
    final guide = readDoc('docs/user_guide.md');

    for (final expected in [
      'MaClaw logo splash screen',
      'If a valid mobile session and LLM access are already configured',
      'opens the assistant directly',
      'MaClaw official service redemption code',
      'provider QR code generated',
      'from the LLM configuration screen in MaClaw desktop GUI',
      'multi-tab assistant',
      'does not accept arbitrary third-party LLM endpoints',
      '`$lookupTab` tab',
      '`$documentsTab` tab',
      '`$remoteTab` tab',
      '`$employeesTab` tab',
      '`$accountTab` tab',
      'programming tools, heavy workflows, admin consoles',
      'does not support custom Hub URLs',
      'does not embed or directly call',
      'Go `corelib`',
      'official Hub',
      'digital employees',
    ]) {
      expect(guide, contains(expected));
    }
  });

  test('release docs do not contain mojibake markers', () {
    final docs = releaseDocCorpus();

    for (final expected in [
      lookupTab,
      documentsTab,
      remoteTab,
      employeesTab,
      accountTab,
    ]) {
      expect(docs, contains(expected));
    }

    for (final mojibake in [
      '\u93cc',
      '\u93c2',
      '\u93c8',
      '\u938f',
      '\ufffd',
    ]) {
      expect(docs, isNot(contains(mojibake)));
    }
  });

  test('release docs keep mobile corelib boundary explicit', () {
    final readme = readDoc('README.md');
    final guide = readDoc('docs/user_guide.md');
    final checklist = readDoc('docs/release_checklist.md');
    final audit = readDoc('docs/release_audit.md');

    for (final doc in [readme, guide, checklist, audit]) {
      expect(doc, contains('does not embed or directly call'));
      expect(doc, contains('corelib'));
      expect(doc, contains('Hub'));
      expect(doc, contains('digital employee'));
    }
    expect(
      readme,
      contains('the phone reaches them through the discovered Hub APIs'),
    );
    expect(
      checklist,
      contains('capabilities stay behind the official Hub'),
    );
  });

  test('QA checklist covers signed build manual gates', () {
    final qa = readDoc('docs/qa_device_checklist.md');

    for (final expected in [
      'Android Signed Build',
      'iOS Signing And Share Extension',
      'top.mypapers.maclaw.mobile.ShareExtension',
      'group.top.mypapers.maclaw.mobile',
      'Team ID',
      'Provisioning profiles: Runner and Share Extension profile UUID/file/name',
      'ios/ExportOptions.plist.example',
      'android/key.properties.example',
      'MACLAW_ANDROID_STORE_FILE',
      'MACLAW_ANDROID_STORE_PASSWORD',
      'MACLAW_ANDROID_KEY_ALIAS',
      'MACLAW_ANDROID_KEY_PASSWORD',
      'Artifact path',
      'SHA256',
      'Firebase App Distribution',
      'release build without `android/key.properties` fails',
      'using the debug signing key',
    ]) {
      expect(qa, contains(expected));
    }
  });

  test('QA checklist covers required share payloads and permissions', () {
    final qa = readDoc('docs/qa_device_checklist.md');
    final normalizedQa = qa.replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'Plain text',
      'URL',
      'Image/photo',
      'PDF',
      'Word `.docx` or `.doc`',
      'Excel `.xlsx` or `.xls`',
      'CSV',
      'Notification permission',
      'Camera permission',
      'Microphone permission',
      'Speech recognition permission',
      'Photo library permission',
      'Local network permission',
    ]) {
      expect(qa, contains(expected));
    }
    expect(
      normalizedQa,
      contains('traceable evidence filename or attachment ID'),
    );
  });

  test('release checklist keeps signed share payload requirements complete',
      () {
    final checklist = readDoc('docs/release_checklist.md');
    final checklistText = checklist.replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'Build a signed internal test package',
      'android/key.properties.example',
      'android/key.properties',
      'storeFile',
      'keyAlias',
      'does not fall back to the debug',
      'Install a signed development/TestFlight build',
      'text, URLs, images, PDFs, Word, Excel, and CSV files',
      'Ask one assistant question by voice',
      'photo/image/screenshot question',
      'document/export, digital employee, and SSH abnormal notifications',
      'payload or tap/open evidence',
      'offline/weak-network warnings',
      'visible HTTPS',
      'named target or output',
    ]) {
      expect(checklist, contains(expected));
    }
    for (final expected in [
      'recognized transcript is sent to the assistant',
      'cited answer evidence or a document upload task ID',
      'recovers after connectivity returns',
    ]) {
      expect(checklistText, contains(expected));
    }
  });

  test('QA checklist covers service and SSH smoke evidence', () {
    final qa = readDoc('docs/qa_device_checklist.md');
    final qaText = qa.replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'Hub Discovery And Service Smoke Test',
      'Login with an official MaClaw account through HubCenter',
      'authenticated mobile session result',
      'https://hubs.maclaw.top',
      'Discovered Hub',
      'LLM access mode',
      'LLM setup screen only exposes',
      'Ask one assistant question by voice',
      'recognized transcript',
      'photo/image/screenshot assistant question',
      'resulting search citation answer or document upload task ID',
      'Upload a document',
      'Export a document to PDF, Word, and Markdown',
      'Submit a digital employee task',
      'including the same task ID',
      'Confirm realtime updates',
      'matching task or job',
      'notifications are delivered',
      'notification payload or tap/open target',
      'offline warning',
      'restore connectivity',
      'visible HTTPS source URL',
      'Mail,',
      'WeChat',
      'clipboard',
      'saved local path',
      'Manual SSH Smoke Test',
      'Disconnect and reconnect',
      'sensitive-data warning',
      'command drafts',
      'not-auto-executed',
      'pasted/copied output',
      'Delete the server profile',
      'approver different from the tester',
      'Account Privacy And Local Data',
      'Change theme and speech language',
      'Clear local work records',
      'server profiles and SSH',
      'separate explicit account',
    ]) {
      expect(qa, contains(expected));
    }
    expect(qaText, contains('arbitrary third-party endpoint'));
  });

  test('release docs lock SSH action-specific evidence fields', () {
    final qa = readDoc('docs/qa_device_checklist.md');
    final template = readDoc('docs/qa_build_record_template.md');
    final evidence = readDoc('docs/release_evidence.md');
    final audit = readDoc('docs/release_audit.md');
    final qaText = qa.toLowerCase().replaceAll(RegExp(r'\s+'), ' ');
    final evidenceText = evidence.toLowerCase().replaceAll(RegExp(r'\s+'), ' ');
    final auditText = audit.toLowerCase().replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'host type',
      'auth mode',
      'connect result',
      'read-only command',
      'command output excerpt',
      'disconnect result',
      'reconnect result',
      'copied output evidence',
    ]) {
      expect(qaText, contains(expected));
      expect(evidenceText, contains(expected));
      expect(auditText, contains(expected));
    }

    for (final expected in [
      'Host type',
      'Auth mode',
      'Connect result',
      'Read-only command',
      'Command output excerpt',
      'Disconnect result',
      'Reconnect result',
      'Copied output evidence',
    ]) {
      expect(template, contains(expected));
    }
  });

  test('release audit keeps manual blockers explicit', () {
    final audit = readDoc('docs/release_audit.md');
    final auditText = audit.replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'Remaining Release Blockers',
      'Signed Android internal APK/AAB',
      'version/build number',
      'installer channel',
      'Android real-device share-to-app',
      'Android runtime permission prompts',
      'iOS signed Runner and Share Extension target',
      'iOS real-device/TestFlight share-to-app',
      'iOS runtime permission prompts',
      'Real SSH maintenance smoke test',
      'Hub discovery smoke test',
      'voice transcription',
      'photo/image assistant input',
      'shared result',
      'document draft',
      'notification delivery',
      'network offline/recovery',
      'realtime Hub URL confirmation',
      'Voice transcript and photo/image/screenshot assistant input produce cited answers or document tasks',
      'real-device voice/photo smoke remains manual',
      'Document/export, digital employee, and SSH abnormal notifications are delivered with payload/open evidence',
      'real-device notification delivery remains manual',
      'Offline or weak-network warnings appear and Hub services recover after connectivity returns',
      'real Hub/network recovery smoke remains manual',
    ]) {
      expect(audit, contains(expected));
    }
    expect(
      auditText,
      contains(
        'selected HubCenter, discovered Hub, tenant, LLM mode/QR authorization evidence, bootstrap, AI search with citations',
      ),
    );
  });

  test('manual release gate table covers full Hub smoke evidence', () {
    final evidence = readDoc('docs/release_evidence.md');
    final evidenceText = evidence.replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'Hub discovery smoke test',
      'AI search with citations',
      'voice transcription',
      'photo/image assistant input',
      'shared result',
      'document draft',
      'document upload/export task IDs',
      'digital employee task ID',
      'realtime status',
      'notification delivery',
      'network offline/recovery',
      'API base URL',
      'realtime Hub URL confirmation',
    ]) {
      expect(evidence, contains(expected));
    }
    expect(
      evidenceText,
      contains(
        'selected HubCenter, discovered Hub, tenant, LLM mode/QR authorization evidence, bootstrap result',
      ),
    );
  });

  test('QA build record template captures every manual release gate', () {
    final template = readDoc('docs/qa_build_record_template.md');

    for (final expected in [
      'HubCenter candidates: https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top',
      'Selected HubCenter URL',
      'Discovered Hub URL',
      'Tenant ID: tenant identifier',
      'LLM access mode: maclaw_official / desktop_qr_third_party',
      'Desktop GUI QR authorization ID',
      'Date: YYYY-MM-DD',
      'Git commit: 7-40 character hexadecimal commit SHA',
      'Branch: git branch name',
      'Flutter version: Flutter x.y.z',
      'MaClaw account: email or account ID',
      'Artifact path',
      'SHA256',
      'Version/build number: app version + build number, such as 1.0.0+42',
      'Signing identity: release alias, SHA fingerprint, upload key, or certificate ID',
      'Runner bundle id: top.mypapers.maclaw.mobile',
      'Android signed install result',
      'Account screen shows selected Hub and tenant',
      'No custom Hub URL setting found',
      'Plain text',
      'URL',
      'Image/photo',
      'PDF',
      'Word .docx or .doc',
      'Excel .xlsx or .xls',
      'CSV',
      'Notification permission',
      'Camera permission',
      'Microphone permission',
      'Media/file access',
      'Local network / SSH scenario',
      'Archive/TestFlight build: .xcarchive path or TestFlight build number',
      'Share Extension bundle id: top.mypapers.maclaw.mobile.ShareExtension',
      'Team ID',
      'Provisioning profiles',
      'App group: group.top.mypapers.maclaw.mobile',
      'iOS signed install result',
      'Speech recognition permission',
      'Photo library permission',
      'Local network permission',
      'Bootstrap user/quota/feature flags/service status',
      'MaClaw official account login through HubCenter',
      'authenticated mobile session',
      'HubCenter probe result',
      'Discovered Hub/tenant result',
      'LLM access evidence',
      'LLM setup surface restriction',
      'Voice/photo assistant input evidence',
      'visible HTTPS source URL',
      'share target or output',
      'clipboard',
      'saved local path',
      'Document draft created from search',
      'notice',
      'report',
      'email',
      'proposal',
      'meeting minutes',
      'statement',
      'Document upload task ID',
      'PDF export job ID',
      'Word export job ID',
      'Markdown export job ID',
      'Exported document share evidence',
      'Digital employee task ID',
      'recorded document/export/digital-employee task or job IDs',
      'Realtime update evidence',
      'Notification delivery evidence',
      'API base URL confirmation',
      'Realtime Hub URL confirmation',
      'Network offline/recovery evidence',
      'Theme and speech language change result',
      'Local work records reset confirmation',
      'Server credentials retained after local reset',
      'Server profiles/SSH credentials clear confirmation',
      'Connect result',
      'Reconnect result',
      'AI analysis confirmation and sensitive-data warning',
      'AI explanation, command drafts',
      'manual/not-auto-executed',
      'pasted/copied output',
      'Credential deletion confirmation',
      'Hub discovery smoke passed: passed / waived with reason',
      'Approval date: YYYY-MM-DD',
      'Approver must be different from Tester',
    ]) {
      expect(template, contains(expected));
    }
  });

  test('mobile CI can be manually triggered and watches its own gate file', () {
    final workflow = File(
      '../../.github/workflows/maclaw-mobile.yml',
    ).readAsStringSync();
    final checklist = readDoc('docs/release_checklist.md');
    final evidence = readDoc('docs/release_evidence.md');

    for (final expected in [
      'workflow_dispatch:',
      '".github/workflows/maclaw-mobile.yml"',
      'python3 -m unittest tool/configure_platforms_test.py',
      'python3 -m unittest tool/validate_qa_build_record_test.py',
      'python3 -m unittest tool/create_qa_build_record_test.py',
      'python3 -m unittest tool/validate_qa_build_records_dir_test.py',
      'python3 -m unittest tool/qa_build_record_report_test.py',
      'python3 -m unittest tool/qa_release_evidence_links_test.py',
      'python3 -m unittest tool/qa_preflight_test.py',
      'python3 -m unittest tool/setup_android_signing_test.py',
      'python3 -m unittest tool/release_status_report_test.py',
      'python3 tool/validate_qa_build_records_dir.py docs/qa-builds',
      'python3 -m unittest tool/verify_runtime_boundary_test.py',
      'python3 -m unittest tool/run_release_gates_test.py',
      'python3 -m unittest tool/verify_debug_apk_evidence_test.py',
      'python3 -m unittest tool/verify_manual_release_gates_test.py',
      'python3 -m unittest tool/verify_final_release_evidence_test.py',
      'python3 -m unittest tool/verify_android_release_signing_test.py',
      'python3 -m unittest tool/build_android_release_test.py',
      'python3 -m unittest tool/verify_ios_wrapper_test.py',
      'python3 -m unittest tool/plan_ios_release_test.py',
      'python3 -m unittest tool/setup_ios_export_options_test.py',
      'python3 tool/verify_android_release_signing.py',
      'python3 tool/verify_ios_wrapper.py',
      'python3 tool/verify_runtime_boundary.py',
      'go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)" -count=1',
      'flutter test test/release_docs_test.dart --concurrency=1 --reporter compact',
      'flutter pub get',
      'flutter analyze',
      'flutter test --concurrency=1',
      'flutter build apk --debug',
      'actions/upload-artifact@v4',
      'maclaw-mobile-debug-apk',
      'mobile/maclaw_mobile/build/app/outputs/flutter-apk/app-debug.apk',
    ]) {
      expect(workflow, contains(expected));
    }
    expect(
      checklist,
      contains('python3 -m unittest tool/configure_platforms_test.py'),
    );
    expect(checklist, contains('tool/validate_qa_build_record_test.py'));
    expect(checklist, contains('tool/create_qa_build_record_test.py'));
    expect(checklist, contains('tool/qa_build_record_report_test.py'));
    expect(checklist, contains('tool/qa_release_evidence_links_test.py'));
    expect(checklist, contains('tool/qa_preflight_test.py'));
    expect(checklist, contains('tool/setup_android_signing_test.py'));
    expect(checklist, contains('tool/setup_android_signing.py'));
    expect(checklist, contains('tool/release_status_report_test.py'));
    expect(checklist, contains('python3 tool/release_status_report.py'));
    expect(checklist, contains('python3 tool/qa_preflight.py'));
    expect(
      checklist,
      contains('python3 tool/validate_qa_build_records_dir.py docs/qa-builds'),
    );
    expect(checklist, contains('tool/verify_runtime_boundary_test.py'));
    expect(
      checklist,
      contains(
        'go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)" -count=1',
      ),
    );
    expect(checklist, contains('tool/run_release_gates.py'));
    expect(checklist, contains('python3 tool/run_release_gates.py --dry-run'));
    expect(checklist, contains('tool/run_release_gates_test.py'));
    expect(checklist, contains('tool/verify_runtime_boundary.py'));
    expect(checklist, contains('python3 tool/verify_debug_apk_evidence.py'));
    expect(checklist, contains('tool/verify_manual_release_gates_test.py'));
    expect(checklist, contains('tool/verify_final_release_evidence_test.py'));
    expect(checklist, contains('tool/verify_android_release_signing_test.py'));
    expect(checklist, contains('tool/build_android_release_test.py'));
    expect(checklist, contains('python3 tool/build_android_release.py --artifact apk --dry-run'));
    expect(checklist, contains('--artifact appbundle'));
    expect(checklist, contains('tool/verify_ios_wrapper_test.py'));
    expect(checklist, contains('tool/plan_ios_release_test.py'));
    expect(checklist, contains('tool/setup_ios_export_options_test.py'));
    expect(checklist, contains('tool/setup_ios_export_options.py'));
    expect(checklist, contains('python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development'));
    expect(checklist, contains('--export-method app-store'));
    expect(checklist, contains('python3 tool/verify_android_release_signing.py'));
    expect(checklist, contains('python3 tool/verify_ios_wrapper.py'));
    expect(
      checklist,
      contains('python3 tool/verify_final_release_evidence.py docs/qa-builds'),
    );
    expect(
      checklist,
      contains(
        'flutter test test/release_docs_test.dart --concurrency=1 --reporter compact',
      ),
    );
    expect(evidence, contains('tool/validate_qa_build_record.py'));
    expect(evidence, contains('tool/create_qa_build_record.py'));
    expect(evidence, contains('tool/create_qa_build_record_test.py'));
    expect(evidence, contains('tool/qa_build_record_report.py'));
    expect(evidence, contains('tool/qa_build_record_report_test.py'));
    expect(evidence, contains('tool/qa_release_evidence_links.py'));
    expect(evidence, contains('tool/qa_release_evidence_links_test.py'));
    expect(evidence, contains('tool/qa_preflight.py'));
    expect(evidence, contains('tool/qa_preflight_test.py'));
    expect(evidence, contains('tool/setup_android_signing.py'));
    expect(evidence, contains('tool/setup_android_signing_test.py'));
    expect(evidence, contains('tool/release_status_report.py'));
    expect(evidence, contains('tool/release_status_report_test.py'));
    expect(evidence, contains('tool/validate_qa_build_records_dir.py'));
    expect(evidence, contains('tool/validate_qa_build_records_dir_test.py'));
    expect(evidence, contains('tool/verify_runtime_boundary.py'));
    expect(evidence, contains('tool/run_release_gates.py'));
    expect(evidence, contains('tool/run_release_gates_test.py'));
    expect(evidence, contains('tool/verify_debug_apk_evidence.py'));
    expect(evidence, contains('tool/verify_manual_release_gates.py'));
    expect(evidence, contains('tool/verify_manual_release_gates_test.py'));
    expect(evidence, contains('tool/verify_final_release_evidence.py'));
    expect(evidence, contains('tool/verify_final_release_evidence_test.py'));
    expect(evidence, contains('tool/verify_android_release_signing.py'));
    expect(evidence, contains('tool/verify_android_release_signing_test.py'));
    expect(evidence, contains('android/key.properties.example'));
    expect(evidence, contains('tool/build_android_release.py'));
    expect(evidence, contains('tool/build_android_release_test.py'));
    expect(evidence, contains('tool/verify_ios_wrapper.py'));
    expect(evidence, contains('tool/verify_ios_wrapper_test.py'));
    expect(evidence, contains('tool/plan_ios_release.py'));
    expect(evidence, contains('tool/plan_ios_release_test.py'));
    expect(evidence, contains('tool/setup_ios_export_options.py'));
    expect(evidence, contains('tool/setup_ios_export_options_test.py'));
    expect(evidence, contains('ios/ExportOptions.plist.example'));
    expect(evidence, contains('artifact path, byte size, and SHA256'));
    expect(
      evidence,
      contains(
        'flutter test test/release_docs_test.dart --concurrency=1 --reporter compact',
      ),
    );
    expect(evidence, contains('maclaw-mobile-debug-apk'));
  });

  test('release checklist CI command order matches release runner order', () {
    final checklist = readDoc('docs/release_checklist.md');

    expectInOrder(checklist, [
      'go test ./hub/internal/httpapi -run "TestMobile.*" -count=1',
      'go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)" -count=1',
      'go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown" -count=1',
      'python3 -m unittest tool/configure_platforms_test.py',
      'python3 -m unittest tool/validate_qa_build_record_test.py',
      'python3 -m unittest tool/create_qa_build_record_test.py',
      'python3 -m unittest tool/validate_qa_build_records_dir_test.py',
      'python3 -m unittest tool/qa_build_record_report_test.py',
      'python3 -m unittest tool/qa_release_evidence_links_test.py',
      'python3 -m unittest tool/qa_preflight_test.py',
      'python3 -m unittest tool/setup_android_signing_test.py',
      'python3 -m unittest tool/release_status_report_test.py',
      'python3 tool/validate_qa_build_records_dir.py docs/qa-builds',
      'python3 -m unittest tool/verify_runtime_boundary_test.py',
      'python3 -m unittest tool/run_release_gates_test.py',
      'python3 -m unittest tool/verify_debug_apk_evidence_test.py',
      'python3 -m unittest tool/verify_manual_release_gates_test.py',
      'python3 -m unittest tool/verify_final_release_evidence_test.py',
      'python3 -m unittest tool/verify_android_release_signing_test.py',
      'python3 -m unittest tool/build_android_release_test.py',
      'python3 -m unittest tool/verify_ios_wrapper_test.py',
      'python3 -m unittest tool/plan_ios_release_test.py',
      'python3 -m unittest tool/setup_ios_export_options_test.py',
      'flutter test test/release_docs_test.dart --concurrency=1 --reporter compact',
      'flutter create --platforms android,ios .',
      'python3 tool/configure_platforms.py',
      'python3 tool/verify_android_release_signing.py',
      'python3 tool/verify_ios_wrapper.py',
      'python3 tool/verify_runtime_boundary.py',
      'flutter pub get',
      'flutter analyze',
      'flutter test --concurrency=1',
      'flutter build apk --debug',
    ]);
  });

  test('release evidence automated gate order matches release runner order',
      () {
    final evidence = readDoc('docs/release_evidence.md');

    expectInOrder(evidence, [
      'go test ./hub/internal/httpapi -run "TestMobile.*" -count=1',
      'go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)" -count=1',
      'go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown" -count=1',
      'cd mobile/maclaw_mobile',
      'python3 -m unittest tool/configure_platforms_test.py',
      'python3 -m unittest tool/validate_qa_build_record_test.py',
      'python3 -m unittest tool/create_qa_build_record_test.py',
      'python3 -m unittest tool/validate_qa_build_records_dir_test.py',
      'python3 -m unittest tool/qa_build_record_report_test.py',
      'python3 -m unittest tool/qa_release_evidence_links_test.py',
      'python3 -m unittest tool/qa_preflight_test.py',
      'python3 -m unittest tool/setup_android_signing_test.py',
      'python3 -m unittest tool/release_status_report_test.py',
      'python3 tool/validate_qa_build_records_dir.py docs/qa-builds',
      'python3 -m unittest tool/verify_runtime_boundary_test.py',
      'python3 -m unittest tool/run_release_gates_test.py',
      'python3 -m unittest tool/verify_debug_apk_evidence_test.py',
      'python3 -m unittest tool/verify_manual_release_gates_test.py',
      'python3 -m unittest tool/verify_final_release_evidence_test.py',
      'python3 -m unittest tool/verify_android_release_signing_test.py',
      'python3 -m unittest tool/build_android_release_test.py',
      'python3 -m unittest tool/verify_ios_wrapper_test.py',
      'python3 -m unittest tool/plan_ios_release_test.py',
      'python3 -m unittest tool/setup_ios_export_options_test.py',
      'flutter test test/release_docs_test.dart --concurrency=1 --reporter compact',
      'flutter create --platforms android,ios .',
      'python3 tool/configure_platforms.py',
      'python3 tool/verify_android_release_signing.py',
      'python3 tool/verify_ios_wrapper.py',
      'python3 tool/verify_runtime_boundary.py',
      'flutter pub get',
      'flutter analyze',
      'flutter test --concurrency=1',
      'flutter build apk --debug',
    ]);
  });

  test('release docs reference existing local files', () {
    final docs = [
      readDoc('README.md'),
      readDoc('docs/release_checklist.md'),
      readDoc('docs/release_evidence.md'),
      readDoc('docs/release_audit.md'),
      readDoc('docs/qa_device_checklist.md'),
      readDoc('docs/qa_build_record_template.md'),
      readDoc('docs/qa-builds/README.md'),
    ].join('\n');

    final referencedPaths = <String>{};
    final fileRefPattern = RegExp(
      r'(?<![A-Za-z0-9_./-])((?:\.\.\/\.\.\/)?(?:\.github\/workflows\/maclaw-mobile\.yml|\.gitignore|README\.md|pubspec\.yaml|android\/key\.properties\.example|android\/app\/src\/main\/AndroidManifest\.xml|ios\/ExportOptions\.plist\.example|ios\/ShareExtension\/?|docs\/[A-Za-z0-9_./-]+\.(?:md)|test\/[A-Za-z0-9_./-]+\.(?:dart)|tool\/[A-Za-z0-9_./-]+\.(?:py)))',
    );

    for (final match in fileRefPattern.allMatches(docs)) {
      var path = match.group(1)!;
      if (path.startsWith('../../')) {
        path = path.substring(6);
      }
      referencedPaths.add(path);
    }

    expect(referencedPaths, isNotEmpty);
    for (final path in referencedPaths) {
      expect(
        localPathExists(path),
        isTrue,
        reason: '$path is referenced by release docs but is missing',
      );
    }
  });

  test('QA build record directory documents naming validation and redaction',
      () {
    final qaBuildsReadme = readDoc('docs/qa-builds/README.md');
    final qaBuildRecordTemplate = readDoc('docs/qa_build_record_template.md');
    final qaBuildsReadmeText = qaBuildsReadme.replaceAll(RegExp(r'\s+'), ' ');
    final rootGitignore = File('../../.gitignore').readAsStringSync();

    for (final expected in [
      'one completed QA build record per signed Android/iOS release candidate',
      'ignored by git',
      'force-add a fully redacted record only when release policy requires it',
      'Keep this',
      'docs/qa_build_record_template.md',
      'build date, platform scope, and build number',
      'python3 tool/create_qa_build_record.py --date 2026-07-02 --scope android-ios --version 1.0.0+42',
      'python3 tool/qa_preflight.py',
      'python3 tool/release_status_report.py',
      'Use `--scope android` or `--scope ios`',
      'refuses to overwrite an existing',
      'python3 tool/validate_qa_build_record.py docs/qa-builds/<record>.md',
      'python3 tool/qa_build_record_report.py docs/qa-builds/<record>.md',
      'python3 tool/qa_release_evidence_links.py docs/qa-builds',
      'python3 tool/validate_qa_build_records_dir.py docs/qa-builds',
      'python3 tool/verify_final_release_evidence.py docs/qa-builds',
      'links every validated record by filename',
      'Do not store SSH passwords',
      'private keys',
      'access tokens',
      'redacted screenshots',
      'attachment IDs',
      'artifact hashes',
    ]) {
      expect(qaBuildsReadme, contains(expected));
    }
    expect(qaBuildsReadmeText, contains('private customer content'));
    expect(qaBuildRecordTemplate, contains('docs/qa-builds/'));
    expect(
      qaBuildRecordTemplate,
      contains('YYYY-MM-DD-<android|ios|android-ios>-<version+build>.md'),
    );
    expect(qaBuildRecordTemplate, contains('docs/qa-builds/README.md'));
    expect(
      qaBuildsReadmeText,
      contains(
        'skips this README and ignores non-Markdown evidence attachments',
      ),
    );
    expect(rootGitignore, contains('mobile/maclaw_mobile/docs/qa-builds/*'));
    expect(
      rootGitignore,
      contains('!mobile/maclaw_mobile/docs/qa-builds/README.md'),
    );
    expectInOrder(rootGitignore, [
      'mobile/maclaw_mobile/docs/qa-builds/*',
      '!mobile/maclaw_mobile/docs/qa-builds/README.md',
    ]);
  });

  test('release evidence records resolved automated test residuals', () {
    final evidence = readDoc('docs/release_evidence.md');

    for (final expected in [
      'Resolved Automated Test Residuals',
      'Drift debug-only warning',
      'MobileLocalStore',
      'shared future',
      'passes without the Drift',
      'Passed: 190 tests',
    ]) {
      expect(evidence, contains(expected));
    }
  });

  test('mobile gitignore excludes generated Flutter and Android caches', () {
    final gitignore = readDoc('.gitignore');

    for (final expected in [
      '.dart_tool/',
      '.flutter-plugins',
      '.flutter-plugins-dependencies',
      'build/',
      'android/.gradle/',
      'android/.kotlin/',
      'android/local.properties',
      'ios/Flutter/ephemeral/',
      'ios/Pods/',
      'ios/.symlinks/',
      'ios/ExportOptions.plist',
      '*.iml',
    ]) {
      expect(gitignore, contains(expected));
    }
  });

  test('release evidence records current release docs test count', () {
    final evidence = readDoc('docs/release_evidence.md');
    final testSource = readDoc('test/release_docs_test.dart');
    final testCount =
        RegExp(r'^\s*test\(', multiLine: true).allMatches(testSource).length;

    expect(
      evidence,
      contains('Passed: $testCount release documentation integrity tests.'),
    );
  });

  test('release evidence records current full release gate count', () {
    final evidence = readDoc('docs/release_evidence.md');
    final evidenceText = evidence.replaceAll(RegExp(r'\s+'), ' ');
    final gateRunner = readDoc('tool/run_release_gates.py');
    final gateCount = RegExp(r'^\s*ReleaseGate\(', multiLine: true)
        .allMatches(gateRunner)
        .length;

    expect(
      evidenceText,
      contains('passed all $gateCount automated release gates'),
    );
  });

  test('release evidence records current Python release tool test counts', () {
    final evidence = readDoc('docs/release_evidence.md');

    final expectedCounts = {
      'tool/configure_platforms_test.py':
          'Passed: {count} platform configuration tests.',
      'tool/validate_qa_build_record_test.py':
          'Passed: {count} QA record validator tests.',
      'tool/create_qa_build_record_test.py':
          'Passed: {count} QA build record scaffold tests.',
      'tool/validate_qa_build_records_dir_test.py':
          'Passed: {count} QA build records directory validator tests.',
      'tool/qa_build_record_report_test.py':
          'Passed: {count} QA build record report tests.',
      'tool/qa_release_evidence_links_test.py':
          'Passed: {count} QA release evidence link helper tests.',
      'tool/qa_preflight_test.py':
          'Passed: {count} QA preflight helper tests.',
      'tool/setup_android_signing_test.py':
          'Passed: {count} Android signing setup helper tests.',
      'tool/release_status_report_test.py':
          'Passed: {count} release status report helper tests.',
      'tool/release_handoff_test.py':
          'Passed: {count} release handoff helper tests.',
      'tool/run_release_gates_test.py':
          'Passed: {count} release gate runner tests.',
      'tool/verify_debug_apk_evidence_test.py':
          'Passed: {count} debug APK evidence verifier tests.',
      'tool/update_debug_apk_evidence_test.py':
          'Passed: {count} debug APK evidence updater tests.',
      'tool/signed_artifact_evidence_test.py':
          'Passed: {count} signed artifact evidence helper tests.',
      'tool/verify_manual_release_gates_test.py':
          'Passed: {count} manual release gate parity tests.',
      'tool/verify_final_release_evidence_test.py':
          'Passed: {count} final release evidence verifier tests.',
      'tool/verify_android_release_signing_test.py':
          'Passed: {count} Android release signing verifier tests.',
      'tool/build_android_release_test.py':
          'Passed: {count} Android release build helper tests.',
      'tool/verify_ios_wrapper_test.py':
          'Passed: {count} iOS wrapper verifier tests.',
      'tool/plan_ios_release_test.py':
          'Passed: {count} iOS release plan helper tests.',
      'tool/setup_ios_export_options_test.py':
          'Passed: {count} iOS export options setup helper tests.',
      'tool/verify_runtime_boundary_test.py':
          'Passed: {count} runtime boundary verifier tests.',
    };

    for (final entry in expectedCounts.entries) {
      final source = readDoc(entry.key);
      final testCount =
          RegExp(r'^\s*def test_', multiLine: true).allMatches(source).length;
      expect(
        evidence,
        contains(entry.value.replaceFirst('{count}', '$testCount')),
        reason: '${entry.key} has $testCount tests',
      );
    }
  });
}
