/// On-device voice model assets — the same files MaClaw GUI downloads into
/// its model dir (`guiapp/app_tts.go`, `guiapp/app_asr.go`):
///
///   kokoro-v1_0.koro                          Kokoro-82M 权重
///   kokoro_82m_selected_voices_koro_v2.zip    6 个音色 .koro（解压平铺）
///   sensevoice-small-q8.gguf                  SenseVoice ASR 量化模型
///
/// Primary source is the GitHub `Model_Release` asset; the account's Hub
/// `/api/v1/models/<file>` is the fallback, same as the desktop.
///
/// Models live in the app-specific external media dir
/// (`/sdcard/Android/media/<pkg>/voice`) — it needs no storage permission
/// and survives app upgrades AND reinstalls, so models never re-download.
library;

import 'dart:io';

import 'package:archive/archive.dart';
import 'package:dio/dio.dart';
import 'package:path_provider/path_provider.dart';

import 'locale.dart';

class VoiceAssets {
  static const kokoroFile = 'kokoro-v1_0.koro';
  static const voicesZipFile = 'kokoro_82m_selected_voices_koro_v2.zip';
  static const asrFile = 'sensevoice-small-q8.gguf';

  /// Mirrors `tts.RequiredTTSVoiceIDs` — readiness requires every voice.
  static const voiceIds = [
    'zm_yunxi',
    'zm_yunyang',
    'zf_xiaoxiao',
    'zf_xiaoyi',
    'am_adam',
    'af_heart',
  ];

  static const _releaseBase =
      'https://github.com/RapidAI/MaClaw/releases/download/Model_Release';

  final Dio dio;

  VoiceAssets({Dio? dio}) : dio = dio ?? Dio();

  static Directory? _dirCache;

  static Future<Directory> dir() async {
    if (_dirCache != null) return _dirCache!;
    // `Android/media/<pkg>` is app-private (no storage permission) yet kept
    // on uninstall — unlike `Android/data`, which the system wipes. Derive
    // it from getExternalStorageDirectory's `.../Android/data/<pkg>/files`.
    var base = await getExternalStorageDirectory();
    if (base != null) {
      final parts = base.path.split('/Android/data/');
      if (parts.length == 2 && parts[1].contains('/')) {
        final pkg = parts[1].split('/').first;
        base = Directory('${parts[0]}/Android/media/$pkg');
      }
    }
    base ??= await getApplicationDocumentsDirectory();
    final d = Directory('${base.path}/voice');
    if (!d.existsSync()) d.createSync(recursive: true);
    return _dirCache = d;
  }

  static Future<String> kokoroPath() async =>
      '${(await dir()).path}/$kokoroFile';
  static Future<String> voicesZipPath() async =>
      '${(await dir()).path}/$voicesZipFile';
  static Future<String> voiceDir() async =>
      '${(await dir()).path}/kokoro_voices';
  static Future<String> asrPath() async => '${(await dir()).path}/$asrFile';

  static bool _nonEmpty(String path) {
    final f = File(path);
    return f.existsSync() && f.lengthSync() > 0;
  }

  /// Kokoro weight + all six voice .koro files extracted.
  static Future<bool> ttsReady() async {
    if (!_nonEmpty(await kokoroPath())) return false;
    final vd = Directory(await voiceDir());
    for (final id in voiceIds) {
      if (!_nonEmpty('${vd.path}/$id.koro')) return false;
    }
    return true;
  }

  /// Same gate as desktop `asr.ModelFileStatus`: a non-empty file whose
  /// first four bytes are the GGUF magic. A truncated or checksum-failed
  /// download used to pass `_nonEmpty` and then fail every load.
  static Future<bool> asrReady() async => _validGguf(await asrPath());

  static const ggufMagic = 0x46554747; // "GGUF" little-endian

  /// Kokoro weight + every voice, or the SenseVoice GGUF. The sheet shows one
  /// progress line for this, the way the launch task downloads it.
  static Future<bool> modelsReady() async => await ttsReady() && await asrReady();

  static bool _validGguf(String path) {
    final f = File(path);
    RandomAccessFile? raf;
    try {
      if (!f.existsSync() || f.lengthSync() < 4) return false;
      raf = f.openSync();
      final header = raf.readSync(4);
      if (header.length < 4) return false;
      final magic = header[0] |
          (header[1] << 8) |
          (header[2] << 16) |
          (header[3] << 24);
      return magic == ggufMagic;
    } on Object {
      return false;
    } finally {
      raf?.closeSync();
    }
  }

  /// Drop a model that failed to load so the next ensure re-downloads it.
  static Future<void> rejectAsrFile() async {
    final f = File(await asrPath());
    if (!f.existsSync()) return;
    try {
      f.deleteSync();
    } on Object {
      // locked — asrReady stays false and the next tap retries
    }
  }

  /// Download `file` from the Model_Release asset, falling back to the
  /// account Hub's `/api/v1/models/` mirror — identical order to the
  /// desktop's `downloadTTSAssetWithFallback`.
  Future<void> _download(
    String file,
    String dest, {
    String? hubBase,
    void Function(int received, int total)? onProgress,
  }) async {
    final urls = [
      '$_releaseBase/$file',
      if (hubBase != null && hubBase.isNotEmpty)
        '${hubBase.replaceAll(RegExp(r'/+$'), '')}/api/v1/models/$file',
    ];
    final part = '$dest.part';
    Object? lastError;
    for (final url in urls) {
      try {
        await dio.download(
          url,
          part,
          options: Options(
            receiveTimeout: const Duration(minutes: 5),
            headers: const {'User-Agent': 'MaClawGo'},
          ),
          onReceiveProgress: onProgress,
        );
        if (File(part).lengthSync() > 0) {
          final current = File(dest);
          if (current.existsSync()) current.deleteSync();
          File(part).renameSync(dest);
          return;
        }
        lastError = StateError('empty body');
      } on Object catch (e) {
        lastError = e;
      }
    }
    // A truncated body must never sit at `dest` — a later ready check
    // would keep failing on the same bytes.
    for (final p in [part, dest]) {
      final f = File(p);
      if (f.existsSync()) {
        try {
          f.deleteSync();
        } on Object {
          // best effort — a locked file just retries next launch
        }
      }
    }
    throw lastError ?? StateError('download failed');
  }

  /// Kokoro weights + voice pack → ready for `VoiceLoadTTS`.
  Future<void> ensureTts({
    String? hubBase,
    void Function(String stage, double progress)? onProgress,
  }) async {
    final kp = await kokoroPath();
    if (!_nonEmpty(kp)) {
      await _download(
        kokoroFile,
        kp,
        hubBase: hubBase,
        onProgress: (r, t) => onProgress?.call(
          sharedL10n.voiceStageDownloadKokoro,
          t > 0 ? r / t * 0.9 : 0,
        ),
      );
    }
    final vd = Directory(await voiceDir());
    var missing = false;
    for (final id in voiceIds) {
      if (!_nonEmpty('${vd.path}/$id.koro')) {
        missing = true;
        break;
      }
    }
    if (missing) {
      onProgress?.call(sharedL10n.voiceStageDownloadVoices, 0.9);
      final zp = await voicesZipPath();
      if (!_nonEmpty(zp)) {
        await _download(voicesZipFile, zp, hubBase: hubBase);
      }
      onProgress?.call(sharedL10n.voiceStageUnpackVoices, 0.95);
      if (!vd.existsSync()) vd.createSync(recursive: true);
      final Archive zip;
      try {
        zip = ZipDecoder().decodeBytes(File(zp).readAsBytesSync());
      } on Object {
        File(zp).deleteSync(); // corrupt/truncated → next run re-downloads
        rethrow;
      }
      for (final entry in zip) {
        if (!entry.isFile) continue;
        // Mirror unzipKokoroVoices: basename only, .koro suffix only.
        final name = entry.name.split('/').last;
        if (!name.toLowerCase().endsWith('.koro')) continue;
        if (entry.size <= 0) continue; // a 0-byte .koro is not a voice
        File('${vd.path}/$name').writeAsBytesSync(entry.content as List<int>);
      }
    }
    onProgress?.call(sharedL10n.voiceStageReady, 1);
  }

  /// One deduplicated ensure for everything — concurrent callers (the
  /// startup task and the settings sheet) share the same download.
  static Future<void>? _ensureAllFuture;

  Future<void> ensureAll({
    String? hubBase,
    void Function(String stage, double progress)? onProgress,
  }) {
    return _ensureAllFuture ??= () async {
      try {
        await ensureTts(
          hubBase: hubBase,
          onProgress: (s, p) => onProgress?.call(s, p * 0.7),
        );
        await ensureAsr(
          hubBase: hubBase,
          onProgress: (s, p) => onProgress?.call(s, 0.7 + p * 0.3),
        );
      } finally {
        _ensureAllFuture = null; // a retry after failure starts fresh
      }
    }();
  }

  /// SenseVoice small q8 → ready for `VoiceLoadASR`.
  Future<void> ensureAsr({
    String? hubBase,
    void Function(String stage, double progress)? onProgress,
  }) async {
    final ap = await asrPath();
    if (await asrReady()) {
      onProgress?.call(sharedL10n.voiceStageReady, 1);
      return;
    }
    // A non-GGUF file stays until `_download` has a replacement. Deleting
    // first would lose the only copy if every URL fails.
    await _download(
      asrFile,
      ap,
      hubBase: hubBase,
      onProgress: (r, t) => onProgress?.call(
        sharedL10n.voiceStageDownloadSenseVoice,
        t > 0 ? r / t : 0,
      ),
    );
    onProgress?.call(sharedL10n.voiceStageReady, 1);
  }
}
