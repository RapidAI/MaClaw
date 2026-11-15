import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/app_update.dart';

/// Answers GET with JSON and dio.download with bytes or failures.
class _FakeAdapter extends Fake implements HttpClientAdapter {
  _FakeAdapter({this.manifest, this.bytes, this.failOnGet = const {}, this.failOnDownload = const {}});

  final Map<String, dynamic>? manifest;
  final Uint8List? bytes;
  final Set<String> failOnGet;
  final Set<String> failOnDownload;
  final requested = <String>[];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requested.add(options.uri.toString());
    if ((options.receiveTimeout?.inSeconds ?? 0) >= 60 ||
        options.uri.path.endsWith('.apk')) {
      // dio.download path
      if (failOnDownload.contains(options.uri.toString()) ||
          bytes == null) {
        throw DioException(
            requestOptions: options, error: 'download failed');
      }
      return ResponseBody.fromBytes(bytes!, 200);
    }
    if (failOnGet.contains(options.uri.toString()) || manifest == null) {
      throw DioException(requestOptions: options, error: 'fetch failed');
    }
    return ResponseBody.fromString(
      jsonEncode(manifest),
      200,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  group('compareVersions', () {
    test('numeric parts decide', () {
      expect(compareVersions('5.8.1', '5.8.0'), 1);
      expect(compareVersions('1.9', '1.10'), -1);
      expect(compareVersions('v5.8.1', '5.8.1'), 0);
      expect(compareVersions('Ｖ6.2.0', '6.2.0'), 0);
    });

    test('pre-release weighs below stable', () {
      expect(compareVersions('6.0.0-beta', '6.0.0'), -1);
      expect(compareVersions('6.0.0-rc.1', '6.0.0-beta.2'), 1);
      expect(compareVersions('6.0.0-alpha.1', '6.0.0-beta.1'), -1);
    });
  });

  group('isReleaseMirrorUrl', () {
    const r2 = 'pub-c837069cbe31469590a5fea6235b436b.r2.dev';
    test('accepts only the compiled-in host and exact asset path', () {
      expect(isReleaseMirrorUrl('https://$r2/latest/MaClawGo.apk'), isTrue);
      expect(isReleaseMirrorUrl('https://evil.example/latest/MaClawGo.apk'),
          isFalse);
      expect(isReleaseMirrorUrl('https://$r2/releases/x/MaClawGo.apk'),
          isFalse);
      expect(isReleaseMirrorUrl('https://$r2/latest/other.apk'), isFalse);
      expect(isReleaseMirrorUrl('http://$r2/latest/MaClawGo.apk'), isFalse);
      expect(isReleaseMirrorUrl('https://$r2/latest/MaClawGo.apk?x=1'),
          isFalse);
    });
  });

  group('check', () {
    test('a newer tag offers the update with the desktop URL order', () async {
      final adapter = _FakeAdapter(manifest: {
        'tag': 'V6.0.0',
        'assets': {
          'MaClawGo.apk': {
            'name': 'MaClawGo.apk',
            'sha256': 'abc123',
            'urls': ['https://evil.example/x.apk'],
          },
        },
      });
      final result =
          await AppUpdater(dio: Dio()..httpClientAdapter = adapter)
              .check('5.0.0');
      expect(result.hasUpdate, isTrue);
      final urls = result.update!.downloadUrls;
      expect(urls[0], contains('/releases/V6.0.0/MaClawGo.apk'));
      expect(urls[1], contains('/latest/MaClawGo.apk'));
      expect(urls[2],
          'https://github.com/RapidAI/MaClaw/releases/download/V6.0.0/MaClawGo.apk');
      // Evil mirror URL rejected — never appears in the candidate list.
      expect(urls.any((u) => u.contains('evil.example')), isFalse);
      expect(result.update!.sha256, 'abc123');
    });

    test('same version reports up to date', () async {
      final adapter = _FakeAdapter(manifest: {'tag': 'V1.0.0'});
      final result =
          await AppUpdater(dio: Dio()..httpClientAdapter = adapter)
              .check('1.0.0');
      expect(result.hasUpdate, isFalse);
      expect(result.failed, isFalse);
    });

    test('a dead github source still wins via r2', () async {
      final adapter = _FakeAdapter(
        manifest: {'tag': 'V9.9.9'},
        failOnGet: {githubLatestManifestUrl},
      );
      final result =
          await AppUpdater(dio: Dio()..httpClientAdapter = adapter)
              .check('1.0.0');
      expect(result.hasUpdate, isTrue);
      expect(result.update!.tag, 'V9.9.9');
    });

    test('all sources failing reports a failed check', () async {
      final adapter = _FakeAdapter(manifest: null);
      final result =
          await AppUpdater(dio: Dio()..httpClientAdapter = adapter)
              .check('1.0.0');
      expect(result.failed, isTrue);
      expect(result.hasUpdate, isFalse);
    });
  });

  group('download', () {
    const update = AppUpdate(
      tag: 'V6.0.0',
      version: 'V6.0.0',
      downloadUrls: [
        'https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/releases/V6.0.0/MaClawGo.apk',
        'https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClawGo.apk',
      ],
    );

    test('falls back to the next URL when the first fails', () async {
      final apk = Uint8List.fromList(
          [0x50, 0x4B, 0x03, 0x04, ...List.filled(64, 1)]);
      final adapter = _FakeAdapter(
        bytes: apk,
        failOnDownload: {update.downloadUrls.first},
      );
      final file = await AppUpdater(
              dio: Dio()..httpClientAdapter = adapter)
          .download(update);
      expect(file.existsSync(), isTrue);
      expect(adapter.requested, contains(update.downloadUrls[1]));
      await file.delete();
    });

    test('a non-zip body is rejected and falls through', () async {
      final adapter = _FakeAdapter(
          bytes: Uint8List.fromList(utf8.encode('<html>404</html>')));
      expect(
        () => AppUpdater(dio: Dio()..httpClientAdapter = adapter)
            .download(update),
        throwsException,
      );
    });

    test('sha256 mismatch rejects the file', () async {
      final apk = Uint8List.fromList(
          [0x50, 0x4B, 0x03, 0x04, ...List.filled(64, 1)]);
      final adapter = _FakeAdapter(bytes: apk);
      const bad = AppUpdate(
        tag: 'V6.0.0',
        version: 'V6.0.0',
        downloadUrls: [
          'https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClawGo.apk',
        ],
        sha256: '0000',
      );
      expect(
        () => AppUpdater(dio: Dio()..httpClientAdapter = adapter)
            .download(bad),
        throwsException,
      );
    });
  });
}
