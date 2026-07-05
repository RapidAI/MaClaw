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
        'python3 tool/release_status_report.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
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
    expect(qa, contains('completed signed-build QA'));
    expectHandoffCommandsWriteEvidence(qa);
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
      'typed or voice input',
      'assistant online answers with citations',
      'only the three preset official HubCenter endpoints',
      'assistant history',
    ]) {
      expect(readme, contains(expected));
    }
    for (final expected in [
      'does not expose custom Hub endpoint configuration',
      'Third-party LLM access is available only as an optional account/settings action',
      'first screen for an unregistered or signed-out user is phone registration/login',
      'does not embed or directly call the Go `corelib` package',
      'official Hub or on authorized remote desktop/server digital employees',
      'AI assistant, emergency documents, manual SSH, digital employees',
    ]) {
      expect(readmeText, contains(expected));
    }
    for (final forbidden in [
      'emergency AI work',
      'source-backed lookup',
      'search history',
      'information lookup',
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
      'MaClaw desktop GUI',
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

    for (final xml in [androidLaunch, androidLaunchV21]) {
      expect(xml, contains('android:src="@mipmap/launch_image"'));
      expect(xml, isNot(contains('<!-- <item>')));
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
      r'--runtime-boundary-result "MaClaw Mobile runtime boundary verified\. log: docs/qa-builds/runtime-boundary-[^"]+\.log"',
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
      'docs/qa_device_checklist.md': readDoc('docs/qa_device_checklist.md'),
      'docs/qa-builds/README.md': readDoc('docs/qa-builds/README.md'),
    };

    for (final entry in docs.entries) {
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

    final artifactSequences = {
      'docs/qa_device_checklist.md': [
        'python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
        'python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds',
        'python3 tool/build_android_release.py --artifact appbundle --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds',
        'python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development',
      ],
      'docs/qa-builds/README.md': [
        'python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
        'python3 tool/build_android_release.py --artifact apk --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds',
        'python3 tool/build_android_release.py --artifact appbundle --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds',
        'python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development',
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
      'python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development',
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
      'resulting assistant citation answer or document upload task ID',
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
    expect(qaText, contains('optional account/settings action'));
    expect(qaText, contains('arbitrary third-party endpoint'));
    expect(qa, isNot(contains('Search query')));
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
      'permission-grant:<id>',
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
        'selected HubCenter, discovered Hub, tenant, LLM mode/QR authorization evidence, bootstrap, AI assistant query with citations',
      ),
    );
    expect(auditText, contains('with `permission-grant:<id>` evidence'));
  });

  test('manual release gate table covers full Hub smoke evidence', () {
    final evidence = readDoc('docs/release_evidence.md');
    final evidenceText = evidence.replaceAll(RegExp(r'\s+'), ' ');

    for (final expected in [
      'Hub discovery smoke test',
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
      'python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds',
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
      'Server credentials retained after local reset',
      'Server profiles/SSH credentials clear confirmation',
      'Connect result',
      'Reconnect result',
      'AI analysis confirmation and sensitive-data warning',
      'AI explanation, command drafts',
      'manual/not-auto-executed',
      'pasted/copied output',
      'Credential deletion confirmation',
      'Release handoff result',
      'handoff output path, attachment ID, or command transcript reference',
      'Runtime boundary verification result',
      'python3 tool/verify_runtime_boundary.py',
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
      'python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds',
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
    expect(checklist, contains('tool/release_evidence_commands_test.py'));
    expect(checklist, contains('tool/setup_android_signing_test.py'));
    expect(checklist, contains('tool/setup_android_signing.py'));
    expect(checklist, contains('tool/release_status_report_test.py'));
    expect(checklist, contains('python3 tool/release_status_report.py'));
    expectInOrder(checklist, [
      'Before creating signed QA packages on a local machine, run:',
      'python3 tool/setup_android_signing.py',
      'python3 tool/setup_ios_export_options.py --team-id <APPLE_TEAM_ID> --export-method development',
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
        'go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)" -count=1',
      ),
    );
    expect(checklist, contains('tool/run_release_gates.py'));
    expect(checklist, contains('python3 tool/run_release_gates.py --dry-run'));
    expect(checklist, contains('tool/run_release_gates_test.py'));
    expect(checklist, contains('tool/verify_runtime_boundary.py'));
    expect(checklist, contains('Release handoff result'));
    expect(checklist, contains('Runtime boundary verification result'));
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
        'python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development',
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
    expect(evidence, contains('auditable verification scope'));
    expect(evidence, contains('QA records directory'));
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
      'python3 tool/setup_ios_export_options.py --team-id <APPLE_TEAM_ID> --export-method development',
      'python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
      'python3 tool/build_android_release.py --artifact apk --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds',
      'python3 tool/build_android_release.py --artifact appbundle --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds',
      'python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds',
      'python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive"',
      'python3 tool/release_status_report.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development',
      'python3 tool/release_handoff.py --version 1.0.0+42 --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-1.0.0+42.md',
      'python3 tool/release_status_report.py --scope android',
      'python3 tool/release_handoff.py --version 1.0.0+42 --scope android --output docs/qa-builds/handoff-android-1.0.0+42.md',
      'python3 tool/qa_preflight.py --scope android',
      'python3 tool/release_handoff.py --version 1.0.0+42 --scope ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-ios-1.0.0+42.md',
      'python3 tool/verify_runtime_boundary.py',
      'python3 tool/run_release_gates.py',
      'Release handoff result',
      'Runtime boundary verification result',
      'no embedded Go corelib',
      'Dart FFI',
      'gomobile binding',
      'native corelib MethodChannel bridge',
      'discovered Hub APIs',
      'explicitly authorized digital employee handoff',
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

    for (final expected in [
      'run on 2026-07-05 passed all',
      'release-gates-local-20260705.log',
      'The local transcript was saved under `docs/qa-builds/`',
      'attach the versioned `release-gates-<version+build>.log`',
      'from signed-build QA as external evidence',
      'Refreshed after the 2026-07-05 full automated release gate run.',
      '03739ABFD43A3E1773564314AD7F58A8F75BD37F35B8F799B07D690936277F9B',
      'These cannot be proven by local unit tests or the unsigned debug APK',
      'Android signed internal build',
      'Signed APK/AAB path, SHA256, signing identity, build number, installer channel, and install result',
    ]) {
      expect(evidence, contains(expected));
    }
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
