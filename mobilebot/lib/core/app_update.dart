import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flutter/services.dart';

/// 在线更新 — 镜像桌面 GUI `app.go` 的更新协议：
/// latest.json manifest 双源竞速（GitHub + Cloudflare R2），下载 URL 按
/// 「per-tag 不可变 R2 → latest R2 → GitHub → manifest 镜像」回退，
/// sha256 校验后交给系统安装器。iOS 无侧载，整个功能只对 Android 生效。
const githubLatestManifestUrl =
    'https://github.com/RapidAI/MaClaw/releases/latest/download/latest.json';
const r2LatestManifestUrl =
    'https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest.json';
const r2PublicBaseUrl = 'https://pub-c837069cbe31469590a5fea6235b436b.r2.dev';
const githubReleaseDownloadBase =
    'https://github.com/RapidAI/MaClaw/releases/download';
const releasePageUrl = 'https://github.com/RapidAI/MaClaw/releases/latest';

/// The published asset name — the workflow stages the release APK as
/// `MaClawGo.apk`; the manifest keys on exactly this string.
const updateAssetName = 'MaClawGo.apk';

class AppUpdate {
  final String tag;
  final String version; // display form, V-prefixed
  final List<String> downloadUrls;
  final String sha256;
  final String releaseUrl;

  const AppUpdate({
    required this.tag,
    required this.version,
    required this.downloadUrls,
    this.sha256 = '',
    this.releaseUrl = releasePageUrl,
  });
}

class UpdateCheckResult {
  final AppUpdate? update;
  final String latestVersion;
  final bool failed;

  const UpdateCheckResult({
    this.update,
    this.latestVersion = '',
    this.failed = false,
  });

  bool get hasUpdate => update != null;
}

/// Version comparison mirroring `compareVersions` in guiapp/app.go: strip
/// v/V, numeric parts first, then pre-release weight (stable > rc > beta >
/// alpha) and its numeric suffix.
int compareVersions(String a, String b) {
  List<String> clean(String s) {
    var v = s.trim().toLowerCase();
    // Strip ASCII and full-width V prefixes alike (release tags use both).
    if (v.startsWith('v') || v.startsWith('ｖ')) v = v.substring(1);
    return v.split(' ').first.split('-');
  }

  final pa = clean(a);
  final pb = clean(b);
  final numA = pa.first.split('.');
  final numB = pb.first.split('.');
  for (var i = 0; i < numA.length || i < numB.length; i++) {
    int at(List<String> p) => i < p.length
        ? int.tryParse(RegExp(r'^\d+').firstMatch(p[i])?.group(0) ?? '0') ?? 0
        : 0;
    final d = at(numA) - at(numB);
    if (d != 0) return d > 0 ? 1 : -1;
  }
  int preWeight(List<String> parts) {
    if (parts.length < 2) return 4;
    final p = parts.sublist(1).join('-');
    if (p.startsWith('alpha')) return 1;
    if (p.startsWith('beta')) return 2;
    if (p.startsWith('rc')) return 3;
    return 0;
  }

  final w = preWeight(pa) - preWeight(pb);
  if (w != 0) return w > 0 ? 1 : -1;
  int preNum(List<String> parts) => parts.length < 2
      ? 0
      : int.tryParse(
              RegExp(r'(\d+)$').firstMatch(parts.last)?.group(1) ?? '0',
            ) ??
            0;
  final n = preNum(pa) - preNum(pb);
  return n == 0 ? 0 : (n > 0 ? 1 : -1);
}

/// A manifest mirror URL is trusted only when it points at the compiled-in
/// R2 host under `latest/<asset>` — remote metadata must never redirect the
/// download to an arbitrary host (same rule as the desktop's
/// `isReleaseMirrorURL`).
bool isReleaseMirrorUrl(String raw) {
  final uri = Uri.tryParse(raw.trim());
  if (uri == null ||
      uri.scheme != 'https' ||
      uri.hasQuery ||
      uri.hasFragment ||
      uri.userInfo.isNotEmpty) {
    return false;
  }
  return uri.host == Uri.parse(r2PublicBaseUrl).host &&
      uri.path == '/latest/$updateAssetName';
}

Map<String, dynamic> _asMap(Object? v) =>
    v is Map ? Map<String, dynamic>.from(v) : const {};

class AppUpdater {
  final Dio _dio;

  AppUpdater({Dio? dio}) : _dio = dio ?? Dio();

  /// Whether the downloaded APK can install over the current app — Android
  /// refuses a same-package update signed by a different key
  /// (INSTALL_FAILED_UPDATE_INCOMPATIBLE), which surfaces as a bare
  /// “签名冲突”. `null` means the check could not run (non-Android host or
  /// channel missing) — the installer then reports its own error as before.
  Future<bool?> apkSignatureCompatible(File apk) async {
    if (!Platform.isAndroid) return null;
    try {
      const channel = MethodChannel('maclawgo/update');
      return await channel.invokeMethod<bool>(
        'apkSignatureMatches',
        {'path': apk.path},
      );
    } on Object {
      return null;
    }
  }

  /// Race the GitHub and R2 manifests; the first successful answer wins —
  /// same content from both, so latency decides (desktop
  /// `fetchFirstManifestRelease`). A fast failure does not end the check.
  Future<UpdateCheckResult> check(String currentVersion) async {
    const sources = [
      (name: 'github', url: githubLatestManifestUrl),
      (name: 'r2', url: r2LatestManifestUrl),
    ];
    final errors = <String>[];
    final completer = Completer<AppUpdate?>();
    var pending = sources.length;
    for (final s in sources) {
      unawaited(
        _fetchManifest(s.url)
            .then((update) {
              if (!completer.isCompleted) completer.complete(update);
            })
            .catchError((Object e) {
              errors.add('${s.name}: $e');
              if (--pending == 0 && !completer.isCompleted) {
                completer.complete(null);
              }
            }),
      );
    }
    final update = await completer.future.timeout(
      const Duration(seconds: 15),
      onTimeout: () => null,
    );
    if (update == null) {
      return UpdateCheckResult(
        failed: true,
        latestVersion: errors.isEmpty ? 'fetch_failed' : errors.join('; '),
      );
    }
    var cleanCurrent = currentVersion.trim().toLowerCase();
    if (cleanCurrent.startsWith('v') || cleanCurrent.startsWith('ｖ')) {
      cleanCurrent = cleanCurrent.substring(1);
    }
    if (compareVersions(update.tag, cleanCurrent) > 0) {
      return UpdateCheckResult(update: update, latestVersion: update.version);
    }
    return UpdateCheckResult(latestVersion: update.version);
  }

  Future<AppUpdate> _fetchManifest(String url) async {
    // ResponseType.plain on purpose: GitHub's /latest/download redirect
    // serves octet-stream, so the json typed getter would hand back a
    // String and the Map cast would throw before we could decode it.
    final resp = await _dio.get<String>(
      url,
      options: Options(
        sendTimeout: const Duration(seconds: 5),
        receiveTimeout: const Duration(seconds: 5),
        responseType: ResponseType.plain,
        headers: const {'User-Agent': 'MaClawGo'},
      ),
    );
    final Object? decoded;
    try {
      decoded = jsonDecode(resp.data ?? '');
    } on FormatException {
      throw FormatException('manifest is not JSON: $url');
    }
    final manifest = _asMap(decoded);
    var tag = (manifest['tag'] ?? manifest['version'] ?? '').toString().trim();
    if (tag.isEmpty) {
      throw FormatException('manifest has no tag/version: $url');
    }
    final asset = _asMap(_asMap(manifest['assets'])[updateAssetName]);
    final sha = (asset['sha256'] ?? '').toString().trim();
    // Download order mirrors the desktop: immutable per-tag R2 first (its
    // bytes always match the manifest checksum), latest/ mirror second,
    // GitHub release third, then manifest-provided mirror URLs.
    final urls = <String>[
      '$r2PublicBaseUrl/releases/${Uri.encodeComponent(tag)}/${Uri.encodeComponent(updateAssetName)}',
      '$r2PublicBaseUrl/latest/$updateAssetName',
      '$githubReleaseDownloadBase/${Uri.encodeComponent(tag)}/${Uri.encodeComponent(updateAssetName)}',
      for (final u in [
        ...(asset['urls'] is List ? asset['urls'] as List : const []),
        asset['url'],
      ])
        if (isReleaseMirrorUrl(u.toString())) u.toString().trim(),
    ];
    return AppUpdate(
      tag: tag,
      version: (tag.toUpperCase().startsWith('V') || tag.startsWith('Ｖ'))
          ? tag
          : 'V$tag',
      downloadUrls: urls,
      sha256: sha,
    );
  }

  /// Download the APK trying each URL in order; [onProgress] gets 0.0–1.0.
  /// Returns the verified file. Throws when every URL failed or the sha256
  /// (when the manifest carries one) does not match.
  Future<File> download(
    AppUpdate update, {
    void Function(double progress)? onProgress,
  }) async {
    Object? lastError;
    for (final url in update.downloadUrls) {
      final tmp = File(
        '${Directory.systemTemp.path}${Platform.pathSeparator}maclawgo-update.apk',
      );
      try {
        await _dio.download(
          url,
          tmp.path,
          options: Options(
            sendTimeout: const Duration(seconds: 15),
            receiveTimeout: const Duration(minutes: 5),
          ),
          onReceiveProgress: (received, total) {
            if (total > 0) onProgress?.call(received / total);
          },
        );
        if (update.sha256.isNotEmpty) {
          final digest = await sha256.bind(tmp.openRead()).first;
          if (digest.toString().toLowerCase() != update.sha256.toLowerCase()) {
            lastError = 'sha256 mismatch';
            await tmp.delete().onError((_, _) => tmp);
            continue;
          }
        }
        // A valid APK is a zip — reject truncated/HTML error bodies early.
        final head = await tmp.openRead(0, 4).expand((c) => c).toList();
        if (head.length < 4 || head[0] != 0x50 || head[1] != 0x4B) {
          lastError = 'downloaded file is not an APK';
          await tmp.delete().onError((_, _) => tmp);
          continue;
        }
        return tmp;
      } on Object catch (error) {
        lastError = error;
        await tmp.delete().onError((_, _) => tmp);
      }
    }
    throw Exception('下载更新包失败：$lastError');
  }
}
