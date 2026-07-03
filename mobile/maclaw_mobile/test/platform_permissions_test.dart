import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('android manifest declares mobile capability permissions', () {
    final manifest =
        File('android/app/src/main/AndroidManifest.xml').readAsStringSync();

    expect(manifest, contains('android.permission.INTERNET'));
    expect(manifest, contains('android.permission.CAMERA'));
    expect(manifest, contains('android.permission.RECORD_AUDIO'));
    expect(manifest, contains('android.permission.POST_NOTIFICATIONS'));
    expect(manifest, contains('android.permission.READ_MEDIA_IMAGES'));
    expect(manifest, contains('android.permission.READ_MEDIA_VIDEO'));
    expect(manifest, contains('android.permission.READ_EXTERNAL_STORAGE'));
    expect(manifest, contains('android:maxSdkVersion="32"'));
  });

  test('android wrapper keeps official package, deep link, and share entries',
      () {
    final manifest =
        File('android/app/src/main/AndroidManifest.xml').readAsStringSync();
    final gradle = File('android/app/build.gradle.kts').readAsStringSync();
    final activity = File(
      'android/app/src/main/kotlin/top/mypapers/maclaw/mobile/MainActivity.kt',
    ).readAsStringSync();

    expect(gradle, contains('namespace = "top.mypapers.maclaw.mobile"'));
    expect(gradle, contains('applicationId = "top.mypapers.maclaw.mobile"'));
    expect(gradle, contains('rootProject.file("key.properties")'));
    expect(gradle, contains('maclawReleaseSigningConfigured'));
    expect(gradle, contains('signingConfigs.getByName("release")'));
    expect(gradle, isNot(contains('signingConfigs.getByName("debug")')));
    expect(activity, contains('package top.mypapers.maclaw.mobile'));
    expect(manifest, contains('android:label="MaClaw Mobile"'));
    expect(manifest, contains('android:scheme="maclaw"'));
    expect(manifest, contains('android.intent.action.SEND"'));
    expect(manifest, contains('android.intent.action.SEND_MULTIPLE"'));
    for (final mimeType in [
      'text/plain',
      'image/*',
      'application/pdf',
      'application/msword',
      'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      'application/vnd.ms-excel',
      'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      'text/csv',
    ]) {
      expect(manifest, contains('android:mimeType="$mimeType"'));
    }
  });

  test('ios plist declares readable privacy usage descriptions', () {
    final plist = File('ios/Runner/Info.plist').readAsStringSync();

    expect(plist, contains('<key>NSCameraUsageDescription</key>'));
    expect(plist, contains('<string>用于拍照提问和导入图片文档。</string>'));
    expect(plist, contains('<key>NSPhotoLibraryUsageDescription</key>'));
    expect(plist, contains('<string>用于从相册导入图片或截图。</string>'));
    expect(plist, contains('<key>NSMicrophoneUsageDescription</key>'));
    expect(plist, contains('<string>用于语音提问。</string>'));
    expect(plist, contains('<key>NSSpeechRecognitionUsageDescription</key>'));
    expect(plist, contains('<string>用于将语音提问转成文字。</string>'));
    expect(plist, contains('<key>NSLocalNetworkUsageDescription</key>'));
    expect(
      plist,
      contains('<string>用于连接本地或内网服务器进行 SSH 应急维护。</string>'),
    );
    expect(plist, isNot(contains('?/string>')));
    expect(plist, isNot(contains('鐢')));
    expect(plist, isNot(contains('閻')));
    expect(plist, isNot(contains('�')));
  });

  test('ios wrapper keeps official URL schemes and app group wiring', () {
    final runnerPlist = File('ios/Runner/Info.plist').readAsStringSync();
    final runnerEntitlements =
        File('ios/Runner/Runner.entitlements').readAsStringSync();
    final sharePlist = File('ios/ShareExtension/Info.plist').readAsStringSync();
    final shareEntitlements =
        File('ios/ShareExtension/ShareExtension.entitlements')
            .readAsStringSync();
    final shareController =
        File('ios/ShareExtension/ShareViewController.swift').readAsStringSync();
    final project =
        File('ios/Runner.xcodeproj/project.pbxproj').readAsStringSync();

    expect(runnerPlist, contains('<string>MaClaw Mobile</string>'));
    expect(runnerPlist, contains('<string>maclaw</string>'));
    expect(
      runnerPlist,
      contains('<string>ShareMedia-\$(PRODUCT_BUNDLE_IDENTIFIER)</string>'),
    );
    expect(runnerPlist, contains('<string>\$(CUSTOM_GROUP_ID)</string>'));
    expect(
      runnerEntitlements,
      contains('<string>\$(CUSTOM_GROUP_ID)</string>'),
    );
    expect(
      sharePlist,
      contains('<string>com.apple.share-services</string>'),
    );
    expect(
      sharePlist,
      contains('<string>\$(PRODUCT_MODULE_NAME).ShareViewController</string>'),
    );
    expect(
      sharePlist,
      contains('<key>NSExtensionActivationSupportsText</key>'),
    );
    expect(
      sharePlist,
      contains('<key>NSExtensionActivationSupportsWebURLWithMaxCount</key>'),
    );
    expect(
      sharePlist,
      contains('<key>NSExtensionActivationSupportsFileWithMaxCount</key>'),
    );
    expect(sharePlist, contains('<integer>10</integer>'));
    expect(
      sharePlist,
      contains('<key>NSExtensionActivationSupportsImageWithMaxCount</key>'),
    );
    expect(sharePlist, contains('<integer>20</integer>'));
    expect(sharePlist, contains('<string>Image</string>'));
    expect(
      shareEntitlements,
      contains('<string>\$(CUSTOM_GROUP_ID)</string>'),
    );
    expect(shareController, contains('RSIShareViewController'));
    expect(
      project,
      contains('PRODUCT_BUNDLE_IDENTIFIER = top.mypapers.maclaw.mobile;'),
    );
    expect(
      project,
      contains('CUSTOM_GROUP_ID = group.top.mypapers.maclaw.mobile;'),
    );
  });
}
