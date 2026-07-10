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
        readDoc('docs/backend_ssh_session_design.md'),
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

  int dartTestCount() {
    return Directory('test')
        .listSync(recursive: true)
        .whereType<File>()
        .where((file) => file.path.endsWith('_test.dart'))
        .map(
          (file) => RegExp(
            r'^\s*test(?:Widgets)?\(',
            multiLine: true,
          ).allMatches(file.readAsStringSync()).length,
        )
        .fold<int>(0, (sum, count) => sum + count);
  }

  bool localPathExists(String path) {
    final file = localFile(path);
    return file.existsSync() || Directory(file.path).existsSync();
  }

  void expectHandoffCommandsWriteEvidence(String docText) {
    final commands = RegExp(
      r'^python3 tool/release_handoff\.py --version .*$',
      multiLine: true,
    ).allMatches(docText).map((match) => match.group(0)!).toList();
    expect(commands, isNotEmpty);
    for (final command in commands) {
      expect(command, contains('--output docs/qa-builds/handoff-'));
      expect(command, contains('.md'));
    }
  }

  void expectScopedPreflightCommandsWriteLogs(
    Map<String, String> docsByName,
  ) {
    final commandPattern = RegExp(
      r'python3 tool/qa_preflight\.py [^`\n]*--scope [^`\n]*',
    );
    final commands = <String>[];
    for (final entry in docsByName.entries) {
      for (final match in commandPattern.allMatches(entry.value)) {
        final command = match.group(0)!;
        commands.add('${entry.key}: $command');
        expect(command, contains('--log docs/qa-builds/preflight-'));
        expect(command, contains('.log'));
      }
    }
    expect(commands, isNotEmpty);
  }

  const assistantTab = 'AI助手';
  const webLookup = '发送给 AI 助手';
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
    expect(
      readme,
      contains(
        'python3 tool/release_status_report.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --log docs/qa-builds/release-status-<version+build>.log',
      ),
    );
    expect(readme, contains('`NOT READY` means'));
    expect(readme, contains('signing inputs, QA records, or final evidence'));
    expect(evidence, contains('docs/release_audit.md'));
    expect(evidence, contains('docs/qa_device_checklist.md'));
    expect(evidence, contains('docs/qa_build_record_template.md'));
    expect(evidence, contains('docs/qa-builds/README.md'));
    expect(audit, contains('qa_device_checklist.md'));
    expect(audit, contains('tool/validate_qa_build_record.py'));
    expect(audit, contains('without secret redaction failures'));
    expect(audit, contains('tool/qa_build_record_report.py'));
    expect(audit, contains('redacted evidence'));
    expect(qa, contains('release_evidence.md'));
    expect(qa, contains('qa_build_record_template.md'));
    expect(qa, contains('docs/qa-builds/README.md'));
    expect(qa, contains('tool/validate_qa_build_record.py'));
    expect(qa, contains('tool/qa_build_record_report.py'));
    expect(qa, contains('tool/qa_release_evidence_links.py'));
    expect(qa, contains('tool/qa_preflight.py'));
    expect(qa, contains('tool/release_status_report.py'));
    expect(qa, contains('tool/release_handoff.py'));
    expect(qa, contains('tool/verify_runtime_boundary.py'));
    expect(qa, contains('tool/run_release_gates.py'));
    expect(qa, contains('Release handoff result'));
    expect(qa, contains('Preflight result'));
    expect(qa, contains('Runtime boundary verification result'));
    expect(qa, contains('Automated release gates result'));
    expect(
      qa,
      contains(
        'python3 tool/release_handoff.py --version <version+build> --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-<version+build>.md',
      ),
    );
    expect(
      qa,
      contains(
        'python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
      ),
    );
    expect(
      qa,
      contains('python3 tool/release_status_report.py --scope android'),
    );
    expect(
      qa,
      contains(
        'python3 tool/release_handoff.py --version <version+build> --scope android --output docs/qa-builds/handoff-android-<version+build>.md',
      ),
    );
    expect(
      qa,
      contains('python3 tool/qa_preflight.py --scope android'),
    );
    expect(
      qa,
      isNot(
        contains(
          'python3 tool/release_handoff.py --version <version+build> --scope android --team-id',
        ),
      ),
    );
    expect(
      qa,
      contains(
        'python3 tool/release_handoff.py --version <version+build> --scope ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-ios-<version+build>.md',
      ),
    );
    expect(
      qa,
      contains(
        'Release handoff outputs saved directly under `docs/qa-builds/` must use a',
      ),
    );
    expect(qa, contains('`handoff-*.md` filename'));
    expect(
      qa,
      contains('validator cannot mistake the handoff plan'),
    );
    final qaBuildsReadme = readDoc('docs/qa-builds/README.md');
    expect(qaBuildsReadme, contains('Current Local Evidence Snapshot'));
    expect(qaBuildsReadme, contains('dry-run preview preserves that snapshot'));
    expect(qaBuildsReadme, contains('saved file unchanged'));
    expect(qa, contains('completed signed-build QA'));
    final checklist = readDoc('docs/release_checklist.md');
    for (final expected in [
      'SSH session management record',
      'mobile-created Hub control',
      'authorized GUI/agent worker',
      'GUI/agent-bound `backend_session_id`',
      'explicit worker claim/update evidence',
      '`ssh_session` realtime `output_chunk`/`output_seq`',
      'claimed_by desktop-agent-1',
      'not phone-local',
      'ad hoc terminal evidence',
      'GUI/agent evidence line containing',
      'actual values for Hub session ID',
      'numeric `output_seq`',
      'traceable attachment ID',
    ]) {
      expect(checklist, contains(expected));
    }
    expectHandoffCommandsWriteEvidence(qa);
    expectScopedPreflightCommandsWriteLogs({
      'docs/release_evidence.md': evidence,
      'docs/release_checklist.md': readDoc('docs/release_checklist.md'),
      'docs/qa_device_checklist.md': qa,
      'docs/qa-builds/README.md': readDoc('docs/qa-builds/README.md'),
      'docs/qa-builds/handoff-0.1.0+1.md':
          readDoc('docs/qa-builds/handoff-0.1.0+1.md'),
    });
    expect(releaseDocCorpus(), contains('tool/create_qa_build_record.py'));
    expect(qa, contains('tool/validate_qa_build_records_dir.py'));
    expect(qa, contains('tool/verify_final_release_evidence.py'));
    expect(
      qa,
      contains(
        'python3 tool/verify_final_release_evidence.py docs/qa-builds --scope android-ios --log docs/qa-builds/final-release-evidence-<version+build>.log',
      ),
    );
  });

  test('README preserves mobile AI assistant positioning', () {
    final readme = readDoc('README.md');
    final readmeText = readme.replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'MaClaw mobile AI assistant',
      'MaClaw GUI-like multi-tab AI assistant',
      '`AI助手`',
      '`主对话`',
      'secondary-tab controls',
      'typed and voice input',
      'no legacy `查信息` entry',
      'assistant online answers with citations',
      'GUI/agent-managed backend SSH session management',
      'phone creates or',
      'controls Hub records',
      'MaClaw desktop/agent claims the work',
      'real SSH session',
      'only the three preset official HubCenter endpoints',
      'assistant history',
    ]) {
      expect(readme, contains(expected));
    }
    for (final expected in [
      'does not expose custom Hub endpoint configuration',
      'Third-party LLM access is available only as an optional account/settings action',
      'first screen for an unregistered or signed-out user is phone registration/login',
      'no redemption-code login on mobile',
      'no arbitrary third-party LLM endpoint, base URL, provider URL, or API-key entry',
      'verified `phone:<digits>` account uses its MaClaw official credits',
      'post-SMS-verification usage evidence',
      'concrete `llm-request-id` and `llm-usage-record`',
      'MaClaw logo splash screen',
      'rule out Flutter placeholder/template branding',
      'signed-in `AI助手` first screen',
      'does not embed or directly call the Go `corelib` package',
      'official Hub or on authorized remote desktop/server digital employees',
      'AI assistant, emergency documents, backend SSH session management, digital employees',
      'SSH passwords, private keys, and passphrases stay on the authorized MaClaw GUI/agent side',
      'Hub control record was claimed by a concrete GUI/agent worker',
      'same GUI/agent-bound `backend_session_id`',
      'realtime `ssh_session` `output_chunk`/`output_seq`',
      'copied-output GUI/agent evidence line with actual Hub session ID',
      'concrete `claimed_by` identity such as `claimed_by desktop-agent-1`',
      'redacted text or a traceable attachment ID',
      'The mobile `远程` surface must follow the MaClaw GUI SSH backend-management',
      '`connect`, confirmed command input, `exec_background`',
      '`check`/`wait`/`list`/`kill`',
      'file `upload`/`download`/`list`',
      'desktop-side `SSHSessionManager`',
      '`SSHBackgroundTaskManager`',
      'and SFTP path',
      'share-to-app evidence',
      'text, URL, image, PDF, Word, Excel, and CSV payloads',
      '`permission-grant:<id>` tied to camera/photo, microphone/speech',
      'media/file access, notification delivery/open',
      'typed notification payload or tap/open evidence for document export',
      'digital employee, and backend SSH abnormal/disconnect flows',
      'traceable document draft, upload task, and export job IDs',
      'secure token storage',
      'GUI/agent-managed session contract',
      'remaining real-device QA needed for release evidence',
    ]) {
      expect(readmeText, contains(expected));
    }
    for (final forbidden in [
      'emergency AI work',
      'source-backed lookup',
      'search history',
      'information lookup',
      'SSH credentials stay in secure storage',
      'secure credential storage',
      'phone-side SSH credential storage',
      'phone-side SSH credential save',
      'phone-side SSH credential read',
      'mobile saves SSH passwords',
      'mobile stores SSH private keys',
      '手机保存 SSH 凭据',
      '手机保存服务器密钥',
      '手机录入 SSH 密码',
      'remaining Hub/agent work needed',
    ]) {
      expect(readmeText, isNot(contains(forbidden)));
    }
  });

  test('user guide preserves mobile product decisions', () {
    final guide = readDoc('docs/user_guide.md');
    final guideText = guide.replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'MaClaw logo splash screen',
      'first screen is phone registration/login',
      'discovered Hub',
      'SMS verification succeeds',
      'multi-tab assistant',
      'account/settings area',
      'Assistant questions use the Hub LLM execution path',
      'without sending its API key to the phone',
      'MaClaw desktop GUI',
      'A desktop QR never creates an account',
      'only mobile registration and login path',
      'provider base URLs, or API keys',
      '`$assistantTab` tab',
      'do not remove the `$assistantTab` entry',
      'disables `$webLookup`',
      'disables assistant online access',
      '`助手联网`, `文档草稿`, or `日志排障`',
      '`$documentsTab` tab',
      'Template selection fills a phone-friendly emergency skeleton',
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
    expect(
      guideText,
      contains('new phone number is registered and signed in automatically'),
    );
    expect(guideText, contains('verified `phone:<digits>` account'));
    expect(guideText, contains('Third-party LLM access is optional'));
    expect(guideText, contains('does not expose a redemption-code login path'));
    expect(guideText, isNot(contains('disables the search feature')));
    expect(
      guideText,
      contains('provider QR code generated from the LLM configuration screen'),
    );
    expect(
      guideText,
      contains('does not accept arbitrary third-party LLM endpoints'),
    );
  });

  test('release docs do not contain mojibake markers', () {
    final docs = releaseDocCorpus();

    for (final expected in [
      assistantTab,
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
      '鍔',
      '鏌',
      '涓',
      '痐',
      '漙',
      '\ufffd',
    ]) {
      expect(docs, isNot(contains(mojibake)));
    }
  });

  test('release tooling sources do not contain replacement markers', () {
    final toolSources = [
      'tool/validate_qa_build_record.py',
      'tool/qa_build_record_report.py',
      'tool/qa_preflight.py',
      'tool/release_status_report.py',
      'tool/release_handoff.py',
      'tool/run_release_gates.py',
      'tool/verify_manual_release_gates.py',
      'tool/verify_final_release_evidence.py',
    ];

    for (final sourcePath in toolSources) {
      expect(
        readDoc(sourcePath),
        isNot(contains('\ufffd')),
        reason: '$sourcePath must stay UTF-8 clean for QA evidence parsing.',
      );
    }
  });

  test('native launch screens use MaClaw logo assets', () {
    final androidLaunch =
        readDoc('android/app/src/main/res/drawable/launch_background.xml');
    final androidLaunchV21 =
        readDoc('android/app/src/main/res/drawable-v21/launch_background.xml');
    final androidLaunchV31 =
        readDoc('android/app/src/main/res/values-v31/styles.xml');
    final androidManifest = readDoc('android/app/src/main/AndroidManifest.xml');
    final iosStoryboard =
        readDoc('ios/Runner/Base.lproj/LaunchScreen.storyboard');
    final iosLaunchReadme =
        readDoc('ios/Runner/Assets.xcassets/LaunchImage.imageset/README.md');

    for (final xml in [androidLaunch, androidLaunchV21]) {
      expect(xml, contains('android:src="@mipmap/launch_image"'));
      expect(xml, isNot(contains('<!-- <item>')));
      expect(xml, contains('MaClaw launch splash'));
      expect(xml, isNot(contains('customize your launch splash screen')));
    }
    for (final xml in [
      readDoc('android/app/src/main/res/values/styles.xml'),
      readDoc('android/app/src/main/res/values-night/styles.xml'),
    ]) {
      expect(xml, contains('MaClaw logo splash'));
      expect(xml, isNot(contains('Flutter engine draws its first frame')));
      expect(xml, isNot(contains('V2 of Flutter')));
    }
    expect(
      androidLaunchV31,
      contains('android:windowSplashScreenAnimatedIcon'),
    );
    expect(androidLaunchV31, contains('@mipmap/launch_image'));
    expect(
      androidLaunchV31,
      contains('android:windowSplashScreenIconBackgroundColor'),
    );
    expect(androidManifest, contains('android:icon="@mipmap/ic_launcher"'));
    expect(
      androidManifest,
      contains('android:roundIcon="@mipmap/ic_launcher"'),
    );
    expect(
      File(
        'android/app/src/main/kotlin/top/mypapers/maclaw/maclaw_mobile/MainActivity.kt',
      ).existsSync(),
      isFalse,
      reason: 'old Flutter template package activity must not remain',
    );
    expect(iosStoryboard, contains('image="LaunchImage"'));
    expect(iosLaunchReadme, contains('MaClaw Launch Screen Assets'));
    expect(iosLaunchReadme, contains('do not replace them with template'));
    expect(iosLaunchReadme, isNot(contains('Flutter project')));

    for (final path in [
      'android/app/src/main/res/mipmap-mdpi/launch_image.png',
      'android/app/src/main/res/mipmap-hdpi/launch_image.png',
      'android/app/src/main/res/mipmap-xhdpi/launch_image.png',
      'android/app/src/main/res/mipmap-xxhdpi/launch_image.png',
      'android/app/src/main/res/mipmap-xxxhdpi/launch_image.png',
      'ios/Runner/Assets.xcassets/LaunchImage.imageset/LaunchImage.png',
      'ios/Runner/Assets.xcassets/LaunchImage.imageset/LaunchImage@2x.png',
      'ios/Runner/Assets.xcassets/LaunchImage.imageset/LaunchImage@3x.png',
      'ios/Runner/Assets.xcassets/AppIcon.appiconset/Icon-App-1024x1024@1x.png',
    ]) {
      final file = File(path);
      expect(file.existsSync(), isTrue, reason: '$path must exist');
      expect(
        file.lengthSync(),
        greaterThan(1024),
        reason: '$path must not be an empty Flutter placeholder',
      );
    }
  });

  test('Android wrapper comments do not use generated template wording', () {
    final androidWrapperSources = {
      'android/app/build.gradle.kts': readDoc('android/app/build.gradle.kts'),
      'android/app/src/debug/AndroidManifest.xml':
          readDoc('android/app/src/debug/AndroidManifest.xml'),
      'android/app/src/profile/AndroidManifest.xml':
          readDoc('android/app/src/profile/AndroidManifest.xml'),
    };

    for (final entry in androidWrapperSources.entries) {
      expect(
        entry.value,
        isNot(contains('The Flutter Gradle Plugin must be applied')),
        reason: '${entry.key} should not keep generated template comments.',
      );
      expect(
        entry.value,
        isNot(contains('The INTERNET permission is required for development')),
        reason: '${entry.key} should not keep generated template comments.',
      );
      expect(
        entry.value,
        isNot(contains('hot reload')),
        reason: '${entry.key} should not keep generated template comments.',
      );
    }
    expect(
      androidWrapperSources['android/app/build.gradle.kts'],
      contains('MaClaw Mobile wrapper'),
    );
    expect(
      androidWrapperSources['android/app/src/debug/AndroidManifest.xml'],
      contains('MaClaw service smoke checks'),
    );
    expect(
      androidWrapperSources['android/app/src/profile/AndroidManifest.xml'],
      contains('MaClaw service smoke checks'),
    );
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
    final checklistText = checklist.replaceAll(RegExp(r'\s+'), ' ');
    for (final expected in [
      'Hub APIs',
      'realtime updates',
      'explicit digital employee task handoff',
      'not through Dart FFI',
      'gomobile bindings',
      'dynamic libraries',
      'native corelib method-channel bridges',
    ]) {
      expect(checklistText, contains(expected));
    }
    final evidence = readDoc('docs/release_evidence.md');
    expect(evidence, contains('phone-side SSH credential save/read API'));
    expect(evidence, contains('custom Hub URL configuration'));
    expect(evidence, contains('redemption-code login'));
    expect(
      evidence,
      contains('arbitrary third-party LLM provider/base URL/API-key fields'),
    );
    expect(evidence, contains('terminal emulator dependency'));
    expect(evidence, contains('saveServerPassword'));
    expect(evidence, contains('readServerPrivateKey'));
  });

  test('release docs keep handoff commands scoped and evidence-writing', () {
    final docs = {
      'docs/release_checklist.md': readDoc('docs/release_checklist.md'),
      'docs/release_evidence.md': readDoc('docs/release_evidence.md'),
      'docs/qa_device_checklist.md': readDoc('docs/qa_device_checklist.md'),
      'docs/qa-builds/README.md': readDoc('docs/qa-builds/README.md'),
    };
    final commandPattern = RegExp(r'python3 tool/release_handoff\.py[^\n`]*');

    for (final entry in docs.entries) {
      final commands = commandPattern
          .allMatches(entry.value)
          .map((match) => match.group(0)!)
          .toList();
      for (final command in commands) {
        expect(command, contains('--scope '), reason: entry.key);
        expect(
          command,
          contains('--output docs/qa-builds/handoff-'),
          reason: entry.key,
        );
      }
    }
  });

  test('release docs keep automated evidence commands writing QA logs', () {
    final docs = {
      'docs/release_evidence.md': readDoc('docs/release_evidence.md'),
      'docs/qa_device_checklist.md': readDoc('docs/qa_device_checklist.md'),
      'docs/qa-builds/README.md': readDoc('docs/qa-builds/README.md'),
    };

    final runtimeLogPattern = RegExp(
      r'python3 tool/verify_runtime_boundary\.py --log docs/qa-builds/runtime-boundary-[^\s`]+\.log',
    );
    final releaseGatesLogPattern = RegExp(
      r'python3 tool/run_release_gates\.py --log docs/qa-builds/release-gates-[^\s`]+\.log',
    );

    for (final entry in docs.entries) {
      expect(
        runtimeLogPattern.hasMatch(entry.value),
        isTrue,
        reason: '${entry.key} should document saved runtime-boundary evidence',
      );
      expect(
        releaseGatesLogPattern.hasMatch(entry.value),
        isTrue,
        reason: '${entry.key} should document saved release-gates evidence',
      );
    }

    expect(
      docs['docs/release_evidence.md'],
      contains('The local transcript was saved under `docs/qa-builds/`'),
    );
    expect(
      docs['docs/release_evidence.md']!.replaceAll(RegExp(r'\s+'), ' '),
      contains(
        'attach the versioned `release-gates-<version+build>.log` from signed-build QA as external evidence',
      ),
    );
  });

  test('release docs keep final evidence verifier commands log-backed', () {
    final docs = {
      'docs/release_checklist.md': readDoc('docs/release_checklist.md'),
      'docs/release_evidence.md': readDoc('docs/release_evidence.md'),
      'docs/qa_device_checklist.md': readDoc('docs/qa_device_checklist.md'),
      'docs/qa-builds/README.md': readDoc('docs/qa-builds/README.md'),
    };
    final commandPattern =
        RegExp(r'python3 tool/verify_final_release_evidence\.py[^\n`]*');

    for (final entry in docs.entries) {
      final commands = commandPattern
          .allMatches(entry.value)
          .map((match) => match.group(0)!)
          .toList();
      expect(commands, isNotEmpty, reason: entry.key);
      for (final command in commands) {
        expect(command, contains('--scope '), reason: entry.key);
        expect(
          command,
          contains('--log docs/qa-builds/final-release-evidence-'),
          reason: entry.key,
        );
        expect(command, contains('.log'), reason: entry.key);
      }
      final normalized = entry.value.replaceAll(RegExp(r'\s+'), ' ');
      expect(
        normalized,
        contains(
          'successful final evidence logs must use that same version/build in the `final-release-evidence*.log` filename',
        ),
        reason: entry.key,
      );
      expect(
        normalized,
        contains('validated QA record filename'),
        reason: entry.key,
      );
      expect(
        normalized,
        contains('not generic labels such as `Completed QA record`'),
        reason: entry.key,
      );
    }
  });

  test('release docs keep final decision log references in QA builds', () {
    final docs = {
      'docs/qa_device_checklist.md': readDoc('docs/qa_device_checklist.md'),
      'docs/qa-builds/README.md': readDoc('docs/qa-builds/README.md'),
    };
    final runtimeResultPattern = RegExp(
      r'--runtime-boundary-result "MaClaw Mobile runtime boundary verified: no corelib, phone-local SSH, terminal emulator, phone-side SSH credential, custom Hub URL, redemption-code login, or third-party LLM provider/base URL/API-key regressions; log: docs/qa-builds/runtime-boundary-[^"]+\.log"',
    );
    final gateResultPattern = RegExp(
      r'--automated-gates-result "run_release_gates\.py: \d+ gates passed; log: docs/qa-builds/release-gates-[^"]+\.log"',
    );

    for (final entry in docs.entries) {
      expect(
        runtimeResultPattern.hasMatch(entry.value),
        isTrue,
        reason: '${entry.key} should prefill runtime-boundary QA log evidence',
      );
      expect(
        gateResultPattern.hasMatch(entry.value),
        isTrue,
        reason: '${entry.key} should prefill release-gates QA log evidence',
      );
    }
  });

  test(
      'signed QA evidence command examples keep setup and preflight before saved logs',
      () {
    final docs = {
      'docs/release_checklist.md': readDoc('docs/release_checklist.md'),
      'docs/qa_device_checklist.md': readDoc('docs/qa_device_checklist.md'),
      'docs/qa-builds/README.md': readDoc('docs/qa-builds/README.md'),
    };

    for (final entry in docs.entries
        .where((entry) => entry.key != 'docs/release_checklist.md')) {
      expectInOrder(entry.value, [
        'python3 tool/release_status_report.py',
        'python3 tool/release_handoff.py',
        'python3 tool/setup_android_signing.py',
        'python3 tool/setup_ios_export_options.py',
        'python3 tool/qa_preflight.py',
        'python3 tool/verify_runtime_boundary.py --log docs/qa-builds/runtime-boundary-',
        'python3 tool/run_release_gates.py --log docs/qa-builds/release-gates-',
        'python3 tool/create_qa_build_record.py',
      ]);
    }

    for (final entry in docs.entries) {
      final normalized = entry.value.replaceAll(RegExp(r'\s+'), ' ');
      expect(
        normalized,
        contains(
          '<APPLE_TEAM_ID>` is allowed only for planning/status commands (`release_status_report.py`, `release_handoff.py`, and `qa_preflight.py`)',
        ),
        reason: entry.key,
      );
      expect(
        normalized,
        contains(
          'Replace it with the real 10-character Apple Team ID before running `setup_ios_export_options.py`, `plan_ios_release.py`, or `signed_artifact_evidence.py`',
        ),
        reason: entry.key,
      );
      expect(
        normalized,
        contains(
          'PowerShell treats unquoted `<...>` placeholders as redirection syntax',
        ),
        reason: entry.key,
      );
      expect(
        normalized,
        contains(
          'for dry-run previews with placeholders, wrap placeholder arguments in quotes',
        ),
        reason: entry.key,
      );
    }

    final signingDocs = {
      ...docs,
      'docs/qa_build_record_template.md':
          readDoc('docs/qa_build_record_template.md'),
      'docs/release_evidence.md': readDoc('docs/release_evidence.md'),
    };
    for (final entry in signingDocs.entries) {
      for (final forbidden in [
        'setup_ios_export_options.py --team-id <APPLE_TEAM_ID>',
        'plan_ios_release.py --team-id <APPLE_TEAM_ID>',
        'signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive" --team-id <APPLE_TEAM_ID>',
      ]) {
        expect(entry.value, isNot(contains(forbidden)), reason: entry.key);
      }
    }

    for (final entry in signingDocs.entries) {
      if (!entry.value.contains('setup_ios_export_options.py') &&
          !entry.value.contains('plan_ios_release.py') &&
          !entry.value.contains('signed_artifact_evidence.py ios')) {
        continue;
      }
      expect(
        entry.value,
        contains('<REAL_APPLE_TEAM_ID>'),
        reason:
            '${entry.key} should mark signing commands with the real Team ID placeholder',
      );
    }

    final artifactSequences = {
      'docs/qa_device_checklist.md': [
        'python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
        'python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds',
        'python3 tool/build_android_release.py --artifact appbundle --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds',
        'python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development',
      ],
      'docs/qa-builds/README.md': [
        'python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
        'python3 tool/build_android_release.py --artifact apk --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds',
        'python3 tool/build_android_release.py --artifact appbundle --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds',
        'python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development',
      ],
    };
    for (final entry in artifactSequences.entries) {
      expectInOrder(docs[entry.key]!, [
        ...entry.value,
        'python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive"',
        'python3 tool/verify_runtime_boundary.py --log docs/qa-builds/runtime-boundary-',
      ]);
    }
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
      'python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> --record-dir docs/qa-builds --version <version+build>',
      '--signing-identity "<alias or certificate fingerprint>"',
      '--installer-channel "<internal test channel>"',
      'python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds',
      'python3 tool/build_android_release.py --artifact appbundle --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds',
      'python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development',
      'After the signed archive/TestFlight build exists',
      'python3 tool/signed_artifact_evidence.py ios',
      '--archive-or-build "build/ios/archive/MaClawMobile.xcarchive"',
      '--provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>"',
      '--record-dir docs/qa-builds',
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
      'python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> --record-dir docs/qa-builds --version <version+build>',
      '--signing-identity "<alias or certificate fingerprint>"',
      '--installer-channel "<internal test channel>"',
      'Install a signed development/TestFlight build',
      'python3 tool/signed_artifact_evidence.py ios',
      '--archive-or-build "build/ios/archive/MaClawMobile.xcarchive"',
      '--provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>"',
      '--record-dir docs/qa-builds',
      'text, URLs, images, PDFs, Word, Excel, and CSV files',
      'Ask one assistant question by voice',
      'photo/image/screenshot question',
      'document/export, digital employee, and SSH abnormal notifications',
      'typed payload or tap/open evidence',
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
    final releaseDocsText =
        releaseDocCorpus().toLowerCase().replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'Hub Discovery And Service Smoke Test',
      'Login with an official MaClaw account through HubCenter',
      'authenticated mobile session result',
      'https://hubs.maclaw.top',
      'Discovered Hub',
      'LLM access mode',
      'signed-out first screen is phone registration/login',
      'MaClaw desktop GUI QR authorization',
      'Ask one assistant question by voice',
      'recognized transcript',
      'photo/image/screenshot assistant question',
      'traceable screenshot/recording ID plus the resulting assistant citation answer',
      'or document upload task ID',
      'Upload a document',
      'Export a document to PDF, Word, and Markdown',
      'Submit a digital employee task',
      'including the same task ID',
      'Confirm realtime updates',
      'matching task or job',
      'notifications are delivered',
      'typed notification payloads or tap/open targets',
      'offline warning',
      'restore connectivity',
      'visible HTTPS source URL',
      'AI assistant query',
      'Mail,',
      'WeChat',
      'clipboard',
      'saved local path',
      'Before attaching any screenshot',
      'terminal output, or device log',
      'redact private customer content and raw secrets',
      'Authorization/Cookie',
      'JWTs',
      'API keys',
      'cloud access key IDs',
      'URLs with embedded credentials',
      'Backend SSH Session Smoke Test',
      'agent/backend-managed SSH session',
      'mobile foreground agent',
      'Hub backend-session',
      'not as evidence that the phone opened SSH',
      'session list/status surface',
      'output_chunk',
      'output_seq',
      'claimed_by',
      '/api/mobile/ssh/sessions/claim',
      'worker claim/update evidence',
      'If the copied backend-session output contains credentials or private customer',
      'keep that GUI/agent evidence line',
      'redacted text or a traceable attachment ID',
      'interrupt',
      'Disconnect and reconnect',
      'sensitive-data warning',
      'command drafts',
      'not-auto-executed',
      'pasted/copied backend session output',
      'Clear the phone-side server-profile cache',
      'approver different from the tester',
      'Account Privacy And Local Data',
      'Change theme and speech language',
      'Clear local work records',
      'server-profile metadata',
      'separate explicit account',
    ]) {
      expect(qa, contains(expected));
    }
    expect(qaText, contains('optional account/settings action'));
    expect(qaText, contains('arbitrary third-party endpoint'));
    expect(qaText, contains('server-profile cache'));
    expect(qaText, contains('phone-side server-profile cache'));
    expect(qa, isNot(contains('Search query')));
    for (final forbidden in [
      'retained server credentials after local reset',
      'retained server credential',
      'server credential clearing',
      'server credentials clear confirmation',
      'credential deletion confirmation',
    ]) {
      expect(qaText, isNot(contains(forbidden)));
      expect(releaseDocsText, isNot(contains(forbidden)));
    }
  });

  test('release docs lock backend SSH session evidence fields', () {
    final qa = readDoc('docs/qa_device_checklist.md');
    final template = readDoc('docs/qa_build_record_template.md');
    final design = readDoc('docs/backend_ssh_session_design.md');
    final guide = readDoc('docs/user_guide.md');
    final evidence = readDoc('docs/release_evidence.md');
    final audit = readDoc('docs/release_audit.md');
    final pubspec = readDoc('pubspec.yaml').toLowerCase();
    final pubspecLock = readDoc('pubspec.lock').toLowerCase();
    final runtimeSource = Directory('lib')
        .listSync(recursive: true)
        .whereType<File>()
        .where((file) => file.path.endsWith('.dart'))
        .map((file) => file.readAsStringSync())
        .join('\n')
        .toLowerCase();
    final qaText = qa.toLowerCase().replaceAll(RegExp(r'\s+'), ' ');
    final designText = design.replaceAll(RegExp(r'\s+'), ' ');
    final guideText = guide.toLowerCase().replaceAll(RegExp(r'\s+'), ' ');
    final evidenceText = evidence.toLowerCase().replaceAll(RegExp(r'\s+'), ' ');
    final auditText = audit.toLowerCase().replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'host type',
      'auth mode',
      'backend ssh session',
      'connect result',
      'read-only command',
      'command output excerpt',
      'ssh realtime incremental output evidence',
      'gui/agent claim',
      'worker handoff',
      'disconnect result',
      'reconnect result',
      'copied backend session output evidence',
    ]) {
      expect(qaText, contains(expected));
      expect(evidenceText, contains(expected));
      expect(auditText, contains(expected));
    }
    expect(qaText, contains('interrupt result'));
    expect(qaText, contains('ctrl+c'));
    expect(qaText, contains('copy backend session output'));
    expect(qaText, contains('copied backend session output evidence'));
    expect(
      template.toLowerCase(),
      contains('copied backend session output evidence'),
    );
    expect(template.toLowerCase(), contains('generic terminal screenshot'));
    expect(evidenceText, contains('interrupt/ctrl+c evidence'));
    expect(evidenceText, contains('explicit worker claim/update evidence'));
    expect(auditText, contains('interrupt/ctrl+c evidence'));
    expect(evidenceText, contains('backend session output copy'));
    expect(
      evidenceText,
      contains(
        'ui labels for copy/clear actions that say backend session output instead of terminal output and no terminal emulator dependency',
      ),
    );
    expect(
      evidenceText,
      contains(
        'server/desktop log-analysis shortcut using backend session output and key logs wording',
      ),
    );
    expect(
      evidenceText,
      contains(
        'backend session output/log ai-analysis confirmation wording in account privacy',
      ),
    );
    expect(
      evidenceText,
      contains('backend session/log output before ai analysis'),
    );
    expect(
      evidenceText,
      isNot(contains('terminal or log output before ai analysis')),
    );
    expect(
      evidenceText,
      contains(
        'legacy ssh credential residues without exposing phone-side ssh credential save/read apis',
      ),
    );
    expect(
      evidenceText,
      contains(
        'rejection of deprecated ssh credential qa fields in favor of server-profile metadata/cache evidence',
      ),
    );
    expect(
      evidenceText,
      contains(
        'credential-residue cleanup and without writing phone-side ssh secrets',
      ),
    );
    expect(evidenceText, contains('copied backend session output'));
    expect(
      evidenceText,
      contains(
        'ai/digital-employee handoff evidence tied to the same gui/agent-bound `backend_session_id`',
      ),
    );
    expect(auditText, contains('copied backend session output evidence'));
    expect(
      auditText,
      contains(
        'ai/digital-employee handoff tied to the same gui/agent-bound `backend_session_id`',
      ),
    );
    for (final expected in [
      'gui-like backend ssh session management',
      'agent/backend session manager',
      'not act as a standalone phone-local ssh terminal client',
      'mobile foreground assistant',
      'hub control record',
      'authorized maclaw gui/agent worker claims that record',
      'sync server profiles published by maclaw gui/agent',
      'does not collect ssh passwords, private keys, or passphrases',
      'backend session output',
      'gui/agent-managed background server tasks',
      'check, wait for, list, or kill those tasks',
      'backend file listing, upload, and download operations',
      'gui/agent sftp path',
      'before backend session output is sent to ai',
      'submitted as a digital employee task',
      'line/character counts',
      'local redaction',
    ]) {
      expect(guideText, contains(expected));
    }
    expect(guideText, isNot(contains('before terminal output is sent to ai')));
    expect(pubspec, isNot(contains('dartssh2')));
    expect(pubspecLock, isNot(contains('dartssh2')));
    for (final forbidden in [
      'package:dartssh2',
      'sshclient',
      'sshsocket',
      'sftpclient',
      'dartssh2',
    ]) {
      expect(runtimeSource, isNot(contains(forbidden)));
    }

    for (final expected in [
      '`POST /api/mobile/ssh/tasks/claim`',
      '`PATCH /api/mobile/ssh/tasks/{task_id}/worker`',
      '`POST /api/mobile/ssh/files/claim`',
      '`PATCH /api/mobile/ssh/files/{operation_id}/worker`',
      'authorized GUI/agent claims session, background-task, and file-operation work',
      'executes it through the existing desktop-side managers',
    ]) {
      expect(design, contains(expected));
    }
    for (final expected in [
      'test/backend_ssh_command_test.dart',
      'test/mobile_realtime_client_test.dart',
      'test/mobile_realtime_bridge_test.dart',
      'flutter test test/mobile_realtime_client_test.dart test/mobile_realtime_bridge_test.dart test/servers_controller_test.dart --concurrency=1 --reporter compact',
      '21 realtime and backend server controller tests',
      '`ssh_task`',
      '`ssh_file_operation`',
      'bridge dispatch of backend ssh task events',
      'per-session task cache',
      'queueing create, attach, reconnect, interrupt, input, and close control records',
      'preserving backend-session input carriage returns',
      'flutter test test/api_client_test.dart test/mobile_realtime_client_test.dart test/mobile_realtime_bridge_test.dart test/servers_controller_test.dart test/servers_screen_test.dart test/backend_ssh_command_test.dart --concurrency=1 --reporter compact',
      '59 mobile backend ssh control-plane tests',
      'request gui/agent-managed `exec_background`',
      '`check_task`, `wait_task`, `list_tasks`, `kill_task`, and backend file',
      'caches gui/agent background task status in the server controller',
      'mobile screen button that submits a command as a gui/agent background task',
      'maclaw-style backend ssh session management',
      'terminal-first ssh client',
      'phone foreground agent as a hub session-management requester',
      'rather than phone-local sftp',
      'phone-side foreground path using tenant hub apis',
      'create, attach, interrupt, reconnect, send input to, and close gui/agent-managed backend ssh sessions',
      'keeps backend command payloads queued for hub/agent execution rather than phone-local ssh execution',
      'go test ./hub/internal/httpapi -run "testmobile.*(ssh|backendssh|realtimebackendssh)" -count=1',
      'go test ./gui -run "testmobiledigitalemployeecandidateids|testremotehubclient.*mobile|testmobiledocumentsourcemarkdown|testresolvemobilebackendsshhost|testmobileserverprofilesfromsshhosts|testprocessmobilebackendsshsession"',
      '17 gui mobile worker/profile tests',
      'missing-profile failure reporting through the worker path',
      'desktop-managed close handling that clears pending input',
      'process-level handling that waits for the desktop',
      'mobile-to-core background task id mapping',
      'bounded default wait timeout behavior',
      'phone-local sftp',
      'official hub mobile bootstrap advertising `backend_ssh_sessions`',
      'not advertising the legacy `local_ssh` field',
      'preserving sanitized `source_machine_id` and `updated_at` provenance metadata',
    ]) {
      expect(evidenceText, contains(expected));
    }
    expect(auditText, contains('test/backend_ssh_command_test.dart'));
    expect(
      auditText,
      contains(
        'go test ./gui -run "testmobiledigitalemployeecandidateids|testremotehubclient.*mobile|testmobiledocumentsourcemarkdown|testresolvemobilebackendsshhost|testmobileserverprofilesfromsshhosts|testprocessmobilebackendsshsession" -count=1',
      ),
    );
    expect(
      auditText,
      contains(
        'copied backend session output evidence with a gui/agent evidence line containing actual values for hub session id, `backend_session_id`, concrete `claimed_by` worker identity such as `claimed_by desktop-agent-1`, and numeric `output_seq`',
      ),
    );

    for (final expected in [
      'Tenant Hub mobile-facing endpoints',
      '`GET /api/mobile/server-profiles`',
      'Machine-authenticated desktop/agent worker endpoints',
      '`POST /api/mobile/ssh/sessions/claim`',
      '`PUT /api/mobile/server-profiles` publishes sanitized desktop `SSHHosts` metadata',
      'not a phone-side credential or server-profile editor',
      'Phone-side removal of a server profile only clears local cached metadata',
      'Foreground Agent Flow',
      'foreground control-plane agent',
      'acts as a foreground agent',
      'Hub control record for the backend SSH session manager',
      'authorized MaClaw GUI/agent worker claims it with machine authentication',
      'reports `backend_session_id`, status, output preview, `output_chunk`, and `output_seq` back to Hub',
      'It must not present itself as owning the SSH transport or credentials',
      'the phone is the emergency operator interface',
      'Hub is the coordination queue',
      'not a request for the phone to open an SSH transport',
      'Hub session-management record',
      'queue confirmed operations',
      'executes background work',
      'performs file transfer on the real SSH session',
      'MaClaw GUI/agent plus `SSHSessionManager` remain the backend session owner',
      'GUI-Equivalent Management Contract',
      'Session ownership stays with the desktop GUI/agent worker',
      'foreground mobile agent is a session-management requester',
      'not a request for the phone to open an SSH transport',
      'Hub session-management record',
      'queue confirmed operations',
      'start background tasks',
      'request file operations',
      'authorized GUI/agent worker claims it',
      'backend managed SSH session',
      'MaClaw-style SSH backend management',
      'not as a terminal-first SSH client',
      'foreground view is a selectable backend-session output panel',
      '`SSHSessionManager`',
      '`SSHPool` connections',
      'PTY handles',
      '`pollMobileBackendSSHSessionsOnce` asks the tenant Hub for a claimable',
      '`claimMobileBackendSSHSession` uses the machine-authenticated worker path',
      '`processMobileBackendSSHSession` resolves the sanitized mobile',
      'creates or reuses the `SSHSessionManager` session',
      'reports the GUI/core `backend_session_id`',
      '`updateMobileBackendSSHSession` sends worker state back through',
      '`mobileBackendSSHOutputChunk` tracks the output delta',
      '`pollMobileBackendSSHTasksOnce` claims mobile-created',
      '`processMobileBackendSSHTask` maps the mobile task ID',
      '`SSHBackgroundTaskManager` task ID',
      '`wait_requested` uses the bounded default',
      'ordinary `running` polls refresh status once',
      '`pollMobileBackendSSHFileOperationsOnce`',
      '`processMobileBackendSSHFileOperation` run file control records',
      '`SFTPTransfer` path',
      'remote `stat` and `list` use read-only commands',
      '`exec_background`',
      '`check_task`',
      '`wait_task`',
      '`list_tasks`',
      '`kill_task`',
      '`upload`',
      '`download`',
      'remote server management',
      'managed background tasks with task IDs',
      'GUI/agent SFTP path',
      'Backend task management',
      'streams status/log tails back to Hub',
      'File operations',
      'not as an embedded terminal library',
      'incremental `output_chunk`',
      'monotonic `output_seq`',
      'backend session ID linkage',
      'GUI/agent evidence line with actual values for the Hub session ID',
      'worker `claimed_by`',
      'prove the output came from the backend session manager rather than a phone-local SSH client',
      'Digital employee handoff context must include the GUI/agent-bound `backend_session_id`',
      'not merely the Hub control-record `session_id`',
      'tie analysis, command drafts, and follow-up actions back to the same `backend_session_id`',
      '`WriteInputChecked`',
      '`InterruptByID`',
      '`ReconnectByID`',
      '`RemoveSession`',
      '`CheckShellResponsive`',
      '`probeShell`',
      'consecutive execution-failure tracking',
      'active backend session ID included for traceability',
      'the mobile foreground agent creates a Hub control record',
      'The claim, not the phone button tap, is what',
      'proves the backend managed session exists',
      'simple SSH client',
    ]) {
      expect(designText, contains(expected));
    }
    for (final forbidden in [
      '`POST /api/mobile/server-profiles`',
      '`DELETE /api/mobile/server-profiles/{profile_id}`',
    ]) {
      expect(designText, isNot(contains(forbidden)));
    }

    for (final expected in [
      'Host type',
      'Auth mode',
      'Connect result',
      'Read-only command',
      'Command output excerpt',
      'SSH realtime incremental output evidence',
      'claimed_by',
      'claim/worker handoff evidence',
      'Interrupt result',
      'Disconnect result',
      'Reconnect result',
      'Copied backend session output evidence',
      'GUI/agent evidence line with actual values for Hub session ID',
      'backend_session_id, concrete claimed_by worker identity such as claimed_by desktop-agent-1, and numeric output_seq',
      'backend session manager',
      'foreground agent may create/attach the Hub management record',
      'proven only after MaClaw GUI/agent claims and manages it',
    ]) {
      expect(template, contains(expected));
    }
    for (final expected in [
      'copied output or backend-session output panel note must include',
      'GUI/agent evidence line with actual values for the Hub session ID',
      '`backend_session_id`',
      '`claimed_by`',
      '`output_seq`',
      'not sufficient as session proof',
    ]) {
      expect(qa, contains(expected));
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
      'permission-grant:<id>',
      'iOS signed Runner and Share Extension target',
      'iOS real-device/TestFlight share-to-app',
      'iOS runtime permission prompts',
      'Real backend SSH session smoke test',
      'interrupt/Ctrl+C evidence',
      'Hub discovery smoke test',
      'MaClaw logo splash',
      'Flutter placeholder',
      'voice transcription',
      'photo/image assistant input',
      'shared result',
      'document draft',
      'notification delivery',
      'network offline/recovery',
      'realtime Hub URL confirmation',
      'Voice transcript and photo/image/screenshot assistant input produce cited answers or document tasks',
      'real-device voice/photo smoke remains manual',
      'Document/export, digital employee, and SSH abnormal notifications are delivered with typed payload/open evidence',
      'real-device notification delivery remains manual',
      'Offline or weak-network warnings appear and Hub services recover after connectivity returns',
      'real Hub/network recovery smoke remains manual',
    ]) {
      expect(audit, contains(expected));
    }
    expect(
      auditText,
      contains(
        'selected HubCenter, discovered Hub, tenant, LLM mode/QR authorization evidence with post-SMS-verification official credits usage record ID, bootstrap, cold-start MaClaw logo splash evidence with no Flutter placeholder/template branding, signed-in `AI助手` first-screen evidence with visible `主对话`/secondary-tab controls, microphone/voice input, and no legacy `查信息` entry, AI assistant query with citations',
      ),
    );
    expect(auditText, contains('with `permission-grant:<id>` evidence'));
  });

  test('manual release gate table covers full Hub smoke evidence', () {
    final evidence = readDoc('docs/release_evidence.md');
    final evidenceText = evidence.replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'Hub discovery smoke test',
      'cold-start MaClaw logo splash evidence',
      'no Flutter placeholder/template branding',
      'signed-in `AI助手` first-screen evidence',
      'no legacy `查信息` entry',
      'AI assistant query with citations',
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
        'selected HubCenter, discovered Hub, tenant, LLM mode/QR authorization evidence with post-SMS-verification official credits usage record ID, concrete `llm-request-id` and `llm-usage-record`, bootstrap result',
      ),
    );
    for (final expected in [
      'Hub discovery with post-SMS-verification official credits LLM proof, concrete',
      '`llm-request-id` and `llm-usage-record`, notification',
      'real-device share/permission, Hub discovery with post-SMS-verification',
      'official credits LLM proof, concrete `llm-request-id` and',
    ]) {
      expect(evidenceText, contains(expected));
    }
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
      'Launch splash logo evidence',
      'Date: YYYY-MM-DD',
      'Git commit: 7-40 character hexadecimal commit SHA',
      'Branch: git branch name',
      'Flutter version: Flutter x.y.z',
      'MaClaw account: phone:<digits> or masked phone:<last-digits>',
      'Artifact path',
      'SHA256',
      'Version/build number: app version + build number, such as 1.0.0+42',
      'Signing identity: release alias, SHA fingerprint, upload key, or certificate ID',
      'python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds',
      'python3 tool/build_android_release.py --artifact appbundle --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds',
      'python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> --record-dir docs/qa-builds --version <version+build>',
      '--signing-identity "<alias or certificate fingerprint>"',
      '--installer-channel "<internal test channel>"',
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
      'Do not write only `UUID`; include the actual profile UUID value',
      'python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds',
      'python3 tool/signed_artifact_evidence.py ios',
      '--archive-or-build "build/ios/archive/MaClawMobile.xcarchive"',
      '--provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>"',
      '--record-dir docs/qa-builds',
      'App group: group.top.mypapers.maclaw.mobile',
      'iOS signed install result',
      'Speech recognition permission',
      'Photo library permission',
      'Local network permission',
      'Bootstrap user/quota/feature flags/service status',
      'phone-number-only MaClaw official login through HubCenter',
      'SMS',
      'official credits bound to that phone account',
      'authenticated mobile session',
      'HubCenter probe result',
      'Discovered Hub/tenant result',
      'LLM access evidence',
      'LLM setup surface restriction',
      'Assistant first screen evidence',
      'AI assistant query',
      'Voice/photo assistant input evidence',
      'visible HTTPS source URL',
      'share target or output',
      'clipboard',
      'saved local path',
      'Document draft created from assistant result',
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
      'assistant history',
      'Server-profile metadata retained after local reset',
      'Server-profile cache clear confirmation',
      'Connect result',
      'Interrupt result',
      'Reconnect result',
      'AI analysis confirmation and sensitive-data warning',
      'AI explanation, command drafts',
      'manual/not-auto-executed',
      'pasted/copied backend session output',
      'Backend SSH server-profile cache clear confirmation',
      'Release handoff result',
      'handoff output path, attachment ID, or command transcript reference',
      'Preflight result',
      'python3 tool/qa_preflight.py --log',
      'Runtime boundary verification result',
      'python3 tool/verify_runtime_boundary.py',
      'phone-local SSH dependency',
      'terminal emulator dependency',
      'phone-side SSH',
      'credential save/read API',
      'custom Hub URL configuration',
      'redemption-code login',
      'arbitrary third-party LLM provider/base URL/API-key fields',
      'Automated release gates result',
      'python3 tool/run_release_gates.py',
      'gate count, and log attachment ID',
      'Hub discovery smoke passed: passed / waived with reason',
      'Approval date: YYYY-MM-DD',
      'Approver must be different from Tester',
    ]) {
      expect(template, contains(expected));
    }
    for (final forbidden in [
      'AI search query:',
      'Document draft created from search:',
      'search history',
    ]) {
      expect(template, isNot(contains(forbidden)));
    }
    expectInOrder(template, [
      'python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds',
      'python3 tool/build_android_release.py --artifact appbundle --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds',
      'already-built artifact',
      'python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> --record-dir docs/qa-builds --version <version+build>',
    ]);
    expectInOrder(template, [
      'python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds',
      'already-built archive or TestFlight build',
      'python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive"',
    ]);
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
      'python3 -m unittest tool/release_evidence_commands_test.py',
      'python3 -m unittest tool/setup_android_signing_test.py',
      'python3 -m unittest tool/release_status_report_test.py',
      'python3 -m unittest tool/release_handoff_test.py',
      'python3 tool/validate_qa_build_records_dir.py docs/qa-builds',
      'python3 -m unittest tool/verify_runtime_boundary_test.py',
      'python3 -m unittest tool/run_release_gates_test.py',
      'python3 -m unittest tool/verify_debug_apk_evidence_test.py',
      'python3 -m unittest tool/update_debug_apk_evidence_test.py',
      'python3 -m unittest tool/signed_artifact_evidence_test.py',
      'python3 -m unittest tool/verify_manual_release_gates_test.py',
      'python3 tool/verify_manual_release_gates.py',
      'python3 -m unittest tool/verify_final_release_evidence_test.py',
      'python3 -m unittest tool/verify_android_release_signing_test.py',
      'python3 -m unittest tool/build_android_release_test.py',
      'python3 -m unittest tool/verify_ios_wrapper_test.py',
      'python3 -m unittest tool/plan_ios_release_test.py',
      'python3 -m unittest tool/setup_ios_export_options_test.py',
      'python3 tool/verify_android_release_signing.py',
      'python3 tool/verify_ios_wrapper.py',
      'python3 tool/verify_runtime_boundary.py',
      'go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts" -count=1',
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
    expect(checklist, contains('tool/release_evidence_commands_test.py'));
    expect(checklist, contains('tool/setup_android_signing_test.py'));
    expect(checklist, contains('tool/setup_android_signing.py'));
    expect(checklist, contains('tool/release_status_report_test.py'));
    expect(checklist, contains('python3 tool/release_status_report.py'));
    expectInOrder(checklist, [
      'Before creating signed QA packages on a local machine, run:',
      'python3 tool/setup_android_signing.py',
      'python3 tool/setup_ios_export_options.py --team-id <REAL_APPLE_TEAM_ID> --export-method development',
      'python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
    ]);
    expect(
      checklist,
      contains(
        'python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
      ),
    );
    expect(
      checklist,
      contains('python3 tool/validate_qa_build_records_dir.py docs/qa-builds'),
    );
    expect(
      checklist,
      contains(
        'python3 tool/validate_qa_build_records_dir.py docs/qa-builds --scope android',
      ),
    );
    expect(checklist, contains('tool/verify_runtime_boundary_test.py'));
    expect(
      checklist,
      contains(
        'go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts" -count=1',
      ),
    );
    expect(checklist, contains('tool/run_release_gates.py'));
    expect(checklist, contains('python3 tool/run_release_gates.py --dry-run'));
    expect(checklist, contains('tool/run_release_gates_test.py'));
    expect(checklist, contains('tool/verify_runtime_boundary.py'));
    expect(checklist, contains('Release handoff result'));
    expect(checklist, contains('Preflight result'));
    expect(checklist, contains('Runtime boundary verification result'));
    expect(checklist, contains('phone-local SSH dependency'));
    expect(checklist, contains('terminal emulator dependency'));
    expect(checklist, contains('phone-side SSH credential'));
    expect(checklist, contains('custom Hub URL configuration'));
    expect(checklist, contains('redemption-code login'));
    expect(
      checklist,
      contains('arbitrary third-party LLM provider/base URL/API-key fields'),
    );
    expect(checklist, contains('Automated release gates result'));
    expect(checklist, contains('python3 tool/verify_debug_apk_evidence.py'));
    expect(checklist, contains('tool/verify_manual_release_gates_test.py'));
    expect(checklist, contains('tool/verify_final_release_evidence_test.py'));
    expect(checklist, contains('tool/verify_android_release_signing_test.py'));
    expect(checklist, contains('tool/build_android_release_test.py'));
    expect(
      checklist,
      contains(
        'python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number> --dry-run',
      ),
    );
    expect(checklist, contains('--artifact appbundle'));
    expect(checklist, contains('tool/verify_ios_wrapper_test.py'));
    expect(checklist, contains('tool/plan_ios_release_test.py'));
    expect(checklist, contains('tool/setup_ios_export_options_test.py'));
    expect(checklist, contains('tool/setup_ios_export_options.py'));
    expect(
      checklist,
      contains(
        'python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development',
      ),
    );
    expect(checklist, contains('--export-method app-store'));
    expect(
      checklist,
      contains('python3 tool/verify_android_release_signing.py'),
    );
    expect(checklist, contains('python3 tool/verify_ios_wrapper.py'));
    expect(
      checklist,
      contains('python3 tool/verify_final_release_evidence.py docs/qa-builds'),
    );
    expect(
      checklist,
      contains(
        'python3 tool/verify_final_release_evidence.py docs/qa-builds --scope android-ios --log docs/qa-builds/final-release-evidence-<version+build>.log',
      ),
    );
    expect(checklist, contains('guarded QA build record link block'));
    expect(checklist, contains('validated QA record filename'));
    expect(
      checklist,
      contains('not generic labels such as `Completed QA record`'),
    );
    expect(
      checklist,
      contains(
        'python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence',
      ),
    );
    for (final expected in [
      'Validated records must already pass `python3 tool/validate_qa_build_record.py`',
      'without secret redaction failures',
      'Authorization/Cookie headers',
      'JWTs',
      'API keys',
      'cloud access key IDs',
      'URLs with embedded credentials',
    ]) {
      expect(checklist, contains(expected));
    }
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
    expect(
      evidence,
      contains(
        'phone-initiated interrupt evidence through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` with GUI/agent Ctrl+C handling',
      ),
    );
    expect(evidence, contains('phone-initiated interrupt evidence'));
    expect(evidence, contains('GUI/agent Ctrl+C handling'));
    expect(evidence, contains('handoff evidence paths being rejected'));
    expect(
      evidence,
      contains('without mixing in missing evidence-field noise'),
    );
    expect(evidence, contains('tool/qa_release_evidence_links.py'));
    expect(evidence, contains('tool/qa_release_evidence_links_test.py'));
    expect(evidence, contains('deferred final verifier messaging'));
    expect(evidence, contains('cover Android/iOS'));
    expect(
      evidence,
      contains(
        'python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence',
      ),
    );
    expect(evidence, contains('<!-- QA_BUILD_RECORD_LINKS_START -->'));
    expect(evidence, contains('<!-- QA_BUILD_RECORD_LINKS_END -->'));
    expectInOrder(evidence, [
      '<!-- QA_BUILD_RECORD_LINKS_START -->',
      '<!-- QA_BUILD_RECORD_LINKS_END -->',
    ]);
    expect(evidence, contains('tool/qa_preflight.py'));
    expect(evidence, contains('tool/qa_preflight_test.py'));
    expect(evidence, contains('tool/release_evidence_commands.py'));
    expect(evidence, contains('tool/release_evidence_commands_test.py'));
    expect(
      evidence,
      contains('`release_handoff.py --dry-run --output ...` preview'),
    );
    expect(evidence, contains('tool/setup_android_signing.py'));
    expect(evidence, contains('`--log` evidence capture'));
    expect(evidence, contains('existing-log overwrite protection'));
    expect(evidence, contains('`--force` replacement'));
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
    expect(
      evidence,
      contains('final verification rechecks copied backend session output'),
    );
    expect(
      evidence,
      contains('rejects raw secrets in that copied output evidence'),
    );
    expect(
      evidence,
      contains('points backend-session final-layer failures back'),
    );
    expect(evidence, contains('qa_build_record_report.py'));
    expect(evidence, contains('auditable verification scope'));
    expect(evidence, contains('QA records directory'));
    expect(evidence, contains('tool/verify_android_release_signing.py'));
    expect(evidence, contains('tool/verify_android_release_signing_test.py'));
    expect(evidence, contains('android/key.properties.example'));
    expect(evidence, contains('tool/build_android_release.py'));
    expect(evidence, contains('tool/build_android_release_test.py'));
    expect(
      evidence,
      contains(
        'successful signed artifact evidence output printing the next',
      ),
    );
    expect(
      evidence,
      contains(
        'python3 tool/validate_qa_build_record.py <records-dir>/<record>.md',
      ),
    );
    expect(
      evidence,
      contains(
        'python3 tool/qa_build_record_report.py <records-dir>/<record>.md',
      ),
    );
    expect(evidence, contains('custom records-dir paths normalized'));
    expect(evidence, contains('tool/verify_ios_wrapper.py'));
    expect(evidence, contains('tool/verify_ios_wrapper_test.py'));
    expect(evidence, contains('tool/plan_ios_release.py'));
    expect(evidence, contains('tool/plan_ios_release_test.py'));
    expect(
      evidence,
      contains('successful iOS signed archive/TestFlight evidence output'),
    );
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
      'go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts" -count=1',
      'go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession" -count=1',
      'python3 -m unittest tool/configure_platforms_test.py',
      'python3 -m unittest tool/validate_qa_build_record_test.py',
      'python3 -m unittest tool/create_qa_build_record_test.py',
      'python3 -m unittest tool/validate_qa_build_records_dir_test.py',
      'python3 -m unittest tool/qa_build_record_report_test.py',
      'python3 -m unittest tool/qa_release_evidence_links_test.py',
      'python3 -m unittest tool/qa_preflight_test.py',
      'python3 -m unittest tool/release_evidence_commands_test.py',
      'python3 -m unittest tool/setup_android_signing_test.py',
      'python3 -m unittest tool/release_status_report_test.py',
      'python3 -m unittest tool/release_handoff_test.py',
      'python3 tool/validate_qa_build_records_dir.py docs/qa-builds',
      'python3 -m unittest tool/verify_runtime_boundary_test.py',
      'python3 -m unittest tool/run_release_gates_test.py',
      'python3 -m unittest tool/verify_debug_apk_evidence_test.py',
      'python3 -m unittest tool/update_debug_apk_evidence_test.py',
      'python3 -m unittest tool/signed_artifact_evidence_test.py',
      'python3 -m unittest tool/verify_manual_release_gates_test.py',
      'python3 tool/verify_manual_release_gates.py',
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
      'go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts" -count=1',
      'go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession" -count=1',
      'cd mobile/maclaw_mobile',
      'python3 -m unittest tool/configure_platforms_test.py',
      'python3 -m unittest tool/validate_qa_build_record_test.py',
      'python3 -m unittest tool/create_qa_build_record_test.py',
      'python3 -m unittest tool/validate_qa_build_records_dir_test.py',
      'python3 -m unittest tool/qa_build_record_report_test.py',
      'python3 -m unittest tool/qa_release_evidence_links_test.py',
      'python3 -m unittest tool/qa_preflight_test.py',
      'python3 -m unittest tool/release_evidence_commands_test.py',
      'python3 -m unittest tool/setup_android_signing_test.py',
      'python3 -m unittest tool/release_status_report_test.py',
      'python3 -m unittest tool/release_handoff_test.py',
      'python3 tool/validate_qa_build_records_dir.py docs/qa-builds',
      'python3 -m unittest tool/verify_runtime_boundary_test.py',
      'python3 -m unittest tool/run_release_gates_test.py',
      'python3 -m unittest tool/verify_debug_apk_evidence_test.py',
      'python3 -m unittest tool/update_debug_apk_evidence_test.py',
      'python3 -m unittest tool/signed_artifact_evidence_test.py',
      'python3 -m unittest tool/verify_manual_release_gates_test.py',
      'python3 tool/verify_manual_release_gates.py',
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
      readDoc('docs/user_guide.md'),
      readDoc('docs/backend_ssh_session_design.md'),
      readDoc('docs/qa-builds/README.md'),
    ].join('\n');

    final referencedPaths = <String>{};
    final fileRefPattern = RegExp(
      r'(?<![A-Za-z0-9_./-])((?:\.\.\/\.\.\/)?(?:\.github\/workflows\/maclaw-mobile\.yml|\.gitignore|README\.md|pubspec\.(?:yaml|lock)|android\/key\.properties\.example|android\/app\/src\/main\/AndroidManifest\.xml|ios\/ExportOptions\.plist\.example|ios\/ShareExtension\/?|docs\/[A-Za-z0-9_./-]+\.(?:md)|test\/[A-Za-z0-9_./-]+\.(?:dart)|tool\/[A-Za-z0-9_./-]+\.(?:py)))',
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
    expectHandoffCommandsWriteEvidence(qaBuildsReadme);

    for (final expected in [
      'one completed QA build record per signed Android/iOS release candidate',
      'ignored by git',
      'force-add a fully redacted record only when release policy requires it',
      'Keep this',
      'docs/qa_build_record_template.md',
      '`tool/release_status_report.py` is expected',
      '`NOT READY`',
      '`android/key.properties`',
      '`ios/ExportOptions.plist`',
      'pre-signing setup state',
      'Do not add placeholder signing files',
      'placeholder QA records',
      'placeholder final evidence links',
      'build date, platform scope, and build number',
      'python3 tool/create_qa_build_record.py --date 2026-07-02 --scope android-ios --version 1.0.0+42',
      'python3 tool/setup_android_signing.py',
      'python3 tool/setup_ios_export_options.py --team-id <REAL_APPLE_TEAM_ID> --export-method development',
      'python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
      'python3 tool/build_android_release.py --artifact apk --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds',
      'python3 tool/build_android_release.py --artifact appbundle --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds',
      'python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds',
      'python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive"',
      'python3 tool/release_status_report.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --log docs/qa-builds/release-status-<version+build>.log',
      'python3 tool/release_handoff.py --version 1.0.0+42 --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-1.0.0+42.md',
      'python3 tool/release_status_report.py --scope android',
      'python3 tool/release_handoff.py --version 1.0.0+42 --scope android --output docs/qa-builds/handoff-android-1.0.0+42.md',
      'python3 tool/qa_preflight.py --scope android',
      'python3 tool/release_handoff.py --version 1.0.0+42 --scope ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-ios-1.0.0+42.md',
      'python3 tool/verify_runtime_boundary.py',
      'python3 tool/run_release_gates.py',
      'Release handoff result',
      'Preflight result',
      '--preflight-result',
      'Runtime boundary verification result',
      'no embedded Go corelib',
      'Dart FFI',
      'gomobile binding',
      'native corelib MethodChannel bridge',
      'phone-local SSH dependency',
      'terminal emulator dependency',
      'phone-side SSH credential',
      'custom Hub URL configuration',
      'redemption-code login',
      'arbitrary',
      'third-party LLM provider/base URL/API-key fields',
      'discovered Hub APIs',
      'explicitly authorized digital employee handoff',
      'For backend SSH evidence, record MaClaw GUI-equivalent backend session',
      'management, not a phone-local SSH client check',
      'mobile-created Hub control record being claimed by an authorized GUI/agent',
      'ssh_session output with output_chunk/output_seq',
      'claimed_by desktop-agent-1',
      'Do not use generic',
      'worker labels, ad hoc terminal screenshots, or handoff plans/logs as completed',
      'preserving that evidence line while replacing credentials or private customer excerpts with redacted text or a traceable attachment ID',
      'replacing credentials or private customer excerpts with redacted text or a traceable attachment ID',
      'If copied backend-session output contains credentials or private customer',
      'keep the GUI/agent evidence line and replace credentials or private',
      'customer excerpts with redacted text or a traceable attachment ID',
      'tool/qa_build_record_report.py',
      'prints this targeted remediation',
      'Automated release gates result',
      'runtime-boundary log, and release-gates log commands refuse to',
      'overwrite existing saved evidence files unless `--force` is provided',
      'Release handoff outputs saved directly under `docs/qa-builds/` must use a',
      '`handoff-*.md` filename',
      'cannot be mistaken for completed signed-build QA records',
      'Use `--scope android` or `--scope ios`',
      'refuses to overwrite an existing',
      'python3 tool/validate_qa_build_record.py docs/qa-builds/<record>.md',
      'python3 tool/qa_build_record_report.py docs/qa-builds/<record>.md',
      'python3 tool/qa_release_evidence_links.py docs/qa-builds',
      'python3 tool/validate_qa_build_records_dir.py docs/qa-builds',
      'python3 tool/validate_qa_build_records_dir.py docs/qa-builds --scope android',
      'python3 tool/verify_final_release_evidence.py docs/qa-builds',
      'docs/qa-builds/final-release-evidence-<version+build>.log',
      'links every validated record by filename',
      'Markdown link labels containing the',
      'validated QA record filename',
      'not generic labels such as `Completed QA record`',
      'Do not store SSH passwords',
      'private keys',
      'access tokens',
      'private key blocks',
      '`password=`/`token=`/`api_key=` assignments',
      'Authorization Bearer/Basic headers',
      'Cookie/Set-Cookie/PRIVATE-TOKEN/X-API-Key',
      'literal API tokens',
      'JWTs',
      'cloud access key IDs',
      'Google API keys',
      'URLs with embedded credentials',
      'redacted screenshots',
      'attachment IDs',
      'artifact hashes',
    ]) {
      expect(qaBuildsReadme, contains(expected));
    }
    expect(
      qaBuildsReadme,
      isNot(
        contains(
          'python3 tool/release_handoff.py --version 1.0.0+42 --scope android --team-id',
        ),
      ),
    );
    expect(qaBuildsReadmeText, contains('private customer content'));
    expect(
      qaBuildsReadmeText,
      contains('completed signed-build QA records are still absent'),
    );
    for (final expected in [
      'to return `NOT READY`',
      '`android/key.properties`',
      '`ios/ExportOptions.plist`',
    ]) {
      expect(qaBuildsReadmeText, contains(expected));
    }
    for (final expected in [
      'Out-of-scope invalid records appear as an ignored warning',
      'do not block the current scoped Android or iOS package',
      'records whose filename scope cannot be parsed',
      'validated in-scope records span multiple version/build values',
      'Keep final release QA records to one version/build',
    ]) {
      expect(qaBuildsReadmeText, contains(expected));
    }
    expect(qaBuildRecordTemplate, contains('docs/qa-builds/'));
    expect(
      qaBuildRecordTemplate,
      contains('YYYY-MM-DD-<android|ios|android-ios>-<version+build>.md'),
    );
    expect(qaBuildRecordTemplate, contains('docs/qa-builds/README.md'));
    expect(
      qaBuildsReadmeText,
      contains(
        'skips this README, handoff-*.md release-handoff evidence files, and non-Markdown evidence attachments',
      ),
    );
    for (final expected in [
      'continuation release-gates, runtime-boundary, and final-release-evidence logs',
      'Continuation logs are useful local evidence snapshots',
      'do not satisfy the completed signed-build QA record requirement',
    ]) {
      expect(qaBuildsReadmeText, contains(expected));
    }
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
    final fullFlutterTestCount = dartTestCount();

    for (final expected in [
      'Resolved Automated Test Residuals',
      'Drift debug-only warning',
      'MobileLocalStore',
      'shared future',
      'passes without the Drift',
      'Passed: $fullFlutterTestCount tests',
    ]) {
      expect(evidence, contains(expected));
    }
  });

  test('release evidence records current local gate log and debug APK refresh',
      () {
    final evidence = readDoc('docs/release_evidence.md');
    final handoff = readDoc('docs/qa-builds/handoff-0.1.0+1.md');

    for (final expected in [
      'run on 2026-07-10 passed all',
      'final-release-evidence-20260706-backend-ssh-realtime.log',
      'runtime-boundary-20260706-backend-ssh-realtime.log',
      'The local transcript was saved under `docs/qa-builds/`',
      'attach the versioned `release-gates-<version+build>.log`',
      'from signed-build QA as external evidence',
      'Refreshed after the local 2026-07-10 debug APK build verification run.',
      'CC367FEDE66721219CA398A9AD3FDD93B57969577579267E78529CDB095960E6',
      'These cannot be proven by local unit tests or the unsigned debug APK',
      'Android signed internal build',
      'Signed APK/AAB path, SHA256, signing identity, build number, installer channel, and install result',
    ]) {
      expect(evidence, contains(expected));
    }
    final gateLogMatches = RegExp(r'release-gates-continuation-2\.log')
        .allMatches(evidence)
        .map((match) => match.group(0)!)
        .toSet();
    expect(gateLogMatches, hasLength(1));
    final gateLogName = gateLogMatches.single;
    expect(handoff, contains('Current Local Evidence Snapshot'));
    expect(
      handoff,
      contains(
        'python3 tool/release_handoff.py --version 0.1.0+1 --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --dry-run --output docs/qa-builds/handoff-0.1.0+1.md',
      ),
    );
    expect(handoff, contains(gateLogName));
    expect(handoff, contains('explicit worker claim/update evidence'));
    expect(
      handoff,
      contains('GUI/agent claim or worker handoff plus explicit worker'),
    );
    expect(
      handoff,
      contains('GUI-equivalent backend SSH session management smoke'),
    );
    expect(
      handoff,
      contains('traceable GUI/agent-managed backend SSH session smoke target'),
    );
    expect(
      handoff,
      contains('same GUI/agent-bound backend_session_id'),
    );
    expect(
      handoff,
      isNot(contains('safe GUI/agent-managed SSH session smoke target')),
    );
    final latestGateLog = File('docs/qa-builds/$gateLogName');
    expect(latestGateLog.existsSync(), isTrue);
    expect(
      latestGateLog.readAsStringSync(),
      contains(
        'All MaClaw Mobile automated release gates passed: 38 gates passed.',
      ),
    );
    expect(
      handoff,
      contains('runtime-boundary-20260706-backend-ssh-realtime.log'),
    );
    final runtimeBoundaryLog = File(
      'docs/qa-builds/runtime-boundary-20260706-backend-ssh-realtime.log',
    );
    expect(runtimeBoundaryLog.existsSync(), isTrue);
    expect(
      runtimeBoundaryLog.readAsStringSync(),
      contains('MaClaw Mobile runtime boundary verified.'),
    );
    expect(
      handoff,
      contains('final-release-evidence-20260706-backend-ssh-realtime.log'),
    );
    final finalEvidenceLog = File(
      'docs/qa-builds/final-release-evidence-20260706-backend-ssh-realtime.log',
    );
    expect(finalEvidenceLog.existsSync(), isTrue);
    expect(
      finalEvidenceLog.readAsStringSync(),
      contains(
        'Final release evidence requires at least one completed signed-build QA record.',
      ),
    );
    expect(
      handoff,
      isNot(contains('release-gates-continuation2-20260705.log')),
    );
    expect(
      handoff,
      isNot(contains('runtime-boundary-continuation-20260705.log')),
    );
    expect(
      handoff,
      isNot(contains('final-release-evidence-continuation-20260705.log')),
    );
  });

  test('mobile gitignore excludes generated Flutter and Android caches', () {
    final gitignore = readDoc('.gitignore');

    for (final expected in [
      '.dart_tool/',
      '.idea/',
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
      'flutter_*.log',
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
    final evidenceText = evidence.replaceAll(RegExp(r'\s+'), ' ');
    final gateRunner = readDoc('tool/run_release_gates.py');

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
      'tool/qa_preflight_test.py': 'Passed: {count} QA preflight helper tests.',
      'tool/release_evidence_commands_test.py':
          'Passed: {count} release evidence command helper tests.',
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

    final gateTestPaths = RegExp(r'tool/[A-Za-z0-9_]+_test\.py')
        .allMatches(gateRunner)
        .map((match) => match.group(0)!)
        .toSet()
        .toList()
      ..sort();
    final documentedTestPaths = expectedCounts.keys.toList()..sort();
    expect(
      documentedTestPaths,
      gateTestPaths,
      reason:
          'Every Python unittest release gate should have an evidence test-count entry.',
    );

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
    expect(
      evidenceText,
      contains(
        'Android-only handoff commands must not include Apple Team ID options while iOS-only commands keep Team ID and export method values',
      ),
    );
    expect(
      evidenceText,
      contains(
        'scope-specific handoff evidence paths for Android-only and iOS-only internal QA',
      ),
    );
    expect(
      evidenceText,
      contains(
        'non-colliding scoped handoff output paths for Android-only and iOS-only QA',
      ),
    );
    expect(
      evidenceText,
      contains(
        'scoped internal QA command parity so Android-only and iOS-only QA command examples stay part of preflight',
      ),
    );
    for (final expected in [
      'out-of-scope invalid records as ignored warnings while current-scope or unparseable invalid records still block',
      'reporting out-of-scope invalid records as ignored warnings instead of blocking Android-only or iOS-only link updates',
      'scoped final verification ignoring invalid records whose filenames clearly belong to the other platform',
      'copied backend session output secret remediation',
      'GUI/agent evidence line with real Hub session ID',
      'concrete `claimed_by` worker identity',
      'numeric `output_seq`',
      'redacted text or a traceable attachment ID',
      'private customer excerpts',
      'preserving that evidence line while replacing credentials or private customer excerpts with redacted text or a traceable attachment ID',
      'Documents backend SSH evidence as MaClaw GUI-equivalent backend session',
      'management, not a phone-local SSH client check; copied backend-session',
      'output must include actual Hub session ID, GUI/agent-bound',
      '`backend_session_id`, concrete `claimed_by` worker identity such as',
      '`claimed_by desktop-agent-1`, and numeric `output_seq`',
    ]) {
      expect(evidenceText, contains(expected));
    }
  });

  test('release evidence records aggregate Python release tool test count', () {
    final evidence = readDoc('docs/release_evidence.md');
    final testCount = Directory('tool')
        .listSync()
        .whereType<File>()
        .where((file) => file.path.endsWith('_test.py'))
        .map(
          (file) => RegExp(r'^\s*def test_', multiLine: true)
              .allMatches(file.readAsStringSync())
              .length,
        )
        .fold<int>(0, (sum, count) => sum + count);

    expect(
      evidence,
      contains('Passed: $testCount Python release tool tests.'),
    );
  });
}
