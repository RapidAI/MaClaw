import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';

import '../../core/api/api_client.dart';
import '../../core/api/official_service.dart';
import '../../core/storage/mobile_local_store.dart';
import 'meeting_recording_upload.dart';

/// User-facing processing text must reflect the persisted mode: an offline
/// recording deliberately falls back to archive-only and must not later imply
/// that a transcript or minutes will be generated when it reconnects.
String meetingRecordingProcessingMessage(String processMode) {
  switch (processMode.trim().toLowerCase()) {
    case 'transcript':
      return '正在生成逐字稿';
    case 'keep':
      return '正在安全归档音频';
    case 'minutes':
    default:
      return '正在生成会议纪要';
  }
}

/// Restart-safe meeting-audio uploader. Every accepted chunk updates SQLite, so
/// a lost network only delays delivery instead of losing a completed meeting.
class MeetingRecordingUploadQueue {
  final MobileLocalStore _store;
  final ApiClient _api;

  MeetingRecordingUploadQueue({MobileLocalStore? store, ApiClient? api})
      : _store = store ?? MobileLocalStore(),
        _api = api ?? ApiClient(hubUrl: maclawDefaultHubCenterUrl);

  Future<MeetingRecordingUpload> enqueue({
    required String localPath,
    required String title,
    required String purpose,
    required String conversationId,
    required double durationSec,
    String processMode = 'minutes',
    String contentType = 'audio/wav',
  }) async {
    final task = MeetingRecordingUpload(
      localId: 'meeting-local-${DateTime.now().microsecondsSinceEpoch}',
      localPath: localPath,
      title: title,
      purpose: purpose,
      conversationId: conversationId,
      processMode: processMode,
      contentType: contentType,
      durationSec: durationSec,
      updatedAt: DateTime.now().toUtc(),
    );
    await _store.saveMeetingRecordingUpload(task);
    return task;
  }

  Future<MeetingRecordingUpload> upload(MeetingRecordingUpload task) =>
      _upload(task, mayRecreateExpiredSession: true);

  Future<MeetingRecordingUpload> _upload(
    MeetingRecordingUpload task, {
    required bool mayRecreateExpiredSession,
  }) async {
    final file = File(task.localPath);
    if (!await file.exists()) {
      final failed = task.copyWith(
          status: 'failed',
          message: '本地录音文件已不存在',
          updatedAt: DateTime.now().toUtc());
      await _store.saveMeetingRecordingUpload(failed);
      return failed;
    }
    var current = task;
    try {
      if (current.recordingId.isEmpty) {
        final session = await _api.createMeetingRecording(
            title: current.title,
            purpose: current.purpose,
            conversationId: current.conversationId,
            contentType: current.contentType);
        current = current.copyWith(
            recordingId: session.recordingId,
            status: 'uploading',
            message: '正在上传录音',
            updatedAt: DateTime.now().toUtc());
        await _saveAfterRemoteSessionBestEffort(current);
      }
      final session = await _api.getMeetingRecording(current.recordingId);
      final sessionStatus = session.status.toLowerCase();
      if (sessionStatus == 'processing' || sessionStatus == 'ready') {
        current = current.copyWith(
          status: sessionStatus,
          message: session.message.isEmpty ? '录音已提交处理' : session.message,
          updatedAt: DateTime.now().toUtc(),
        );
        await _saveAfterRemoteSessionBestEffort(current);
        await _releaseLocalAudioIfDelivered(current);
        return current;
      }
      if (sessionStatus == 'uploaded') {
        await _api.processMeetingRecording(current.recordingId,
            mode: current.processMode);
        current = current.copyWith(
          status: 'processing',
          message: meetingRecordingProcessingMessage(current.processMode),
          updatedAt: DateTime.now().toUtc(),
        );
        await _saveAfterRemoteSessionBestEffort(current);
        await _releaseLocalAudioIfDelivered(current);
        return current;
      }
      final chunkSize =
          session.chunkSize.clamp(64 * 1024, 8 * 1024 * 1024).toInt();
      final total = await file.length();
      var index = current.nextChunkIndex;
      final raf = await file.open();
      try {
        await raf.setPosition(index * chunkSize);
        while (true) {
          final bytes = await raf.read(chunkSize);
          if (bytes.isEmpty) break;
          await _api.uploadMeetingRecordingChunk(
              current.recordingId, index, Uint8List.fromList(bytes));
          index++;
          current = current.copyWith(
              nextChunkIndex: index,
              status: 'uploading',
              message:
                  '已上传 ${(raf.positionSync() * 100 / total).clamp(0, 100).toStringAsFixed(0)}%',
              updatedAt: DateTime.now().toUtc());
          await _saveAfterRemoteSessionBestEffort(current);
        }
      } finally {
        await raf.close();
      }
      if (index == 0) throw StateError('录音文件为空');
      await _api.completeMeetingRecording(
        current.recordingId,
        chunks: index,
        sha256: await _fileSHA256(file),
        durationSec: current.durationSec,
      );
      await _api.processMeetingRecording(current.recordingId,
          mode: current.processMode);
      current = current.copyWith(
          status: 'processing',
          message: meetingRecordingProcessingMessage(current.processMode),
          updatedAt: DateTime.now().toUtc());
      await _saveAfterRemoteSessionBestEffort(current);
      await _releaseLocalAudioIfDelivered(current);
      return current;
    } on Object catch (e) {
      if (mayRecreateExpiredSession &&
          current.recordingId.isNotEmpty &&
          _isRecordingSessionMissing(e)) {
        // A Hub that was restarted before persistence was enabled no longer has
        // this session. Preserve the local audio and safely begin a fresh
        // resumable upload instead of retrying an ID that can never recover.
        final reset = current.copyWith(
          clearSession: true,
          status: 'pending',
          message: '正在重新建立上传会话',
          updatedAt: DateTime.now().toUtc(),
        );
        await _saveAfterRemoteSessionBestEffort(reset);
        return _upload(reset, mayRecreateExpiredSession: false);
      }
      current = current.copyWith(
          status: 'pending',
          message: '等待网络重试：$e',
          updatedAt: DateTime.now().toUtc());
      await _saveAfterRemoteSessionBestEffort(current);
      return current;
    }
  }

  Future<void> _saveAfterRemoteSessionBestEffort(
    MeetingRecordingUpload task,
  ) async {
    try {
      await _store.saveMeetingRecordingUpload(task);
    } on Object {
      // Once Hub has created a session, SQLite is recovery metadata rather
      // than the authoritative operation. Keep uploading with the in-memory
      // recording ID so a cache outage cannot turn success into a duplicate.
    }
  }

  Future<String> _fileSHA256(File file) async {
    final digest = await sha256.bind(file.openRead()).first;
    return digest.toString();
  }

  bool _isRecordingSessionMissing(Object error) =>
      error is DioException && error.response?.statusCode == 404;

  /// After Hub has accepted processing, the resumable upload is no longer the
  /// source of recovery. Drop the phone's duplicate raw audio to avoid filling
  /// device storage; Hub retains its governed copy until retention expiry.
  Future<void> _releaseLocalAudioIfDelivered(
      MeetingRecordingUpload task) async {
    final file = File(task.localPath);
    try {
      if (await file.exists()) {
        await file.delete();
      }
    } on FileSystemException {
      // Keep the SQLite task when OS cleanup fails so a later cache cleanup or
      // user action can reclaim it without losing recovery metadata.
      return;
    }
    try {
      await _store.removeMeetingRecordingUpload(task.localId);
    } on Object {
      // Hub already owns the recording. Failure to remove stale local recovery
      // metadata must not downgrade the completed remote operation.
    }
  }

  Future<List<MeetingRecordingUpload>> resumePending() async {
    final items = await _store.loadMeetingRecordingUploads();
    final next = <MeetingRecordingUpload>[];
    for (final item in items) {
      if (item.status == 'pending' || item.status == 'uploading')
        next.add(await upload(item));
    }
    return next;
  }
}
