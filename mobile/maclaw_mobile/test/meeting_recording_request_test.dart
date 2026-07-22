import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/meeting_recording/meeting_recording_request.dart';
import 'package:maclaw_mobile/features/meeting_recording/meeting_recording_upload.dart';
import 'package:maclaw_mobile/features/meeting_recording/meeting_recording_upload_queue.dart';

class _RecordingMeetingApiClient extends ApiClient {
  int createCalls = 0;
  int getCalls = 0;

  _RecordingMeetingApiClient() : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<MobileMeetingRecording> createMeetingRecording({
    required String title,
    String purpose = '',
    String conversationId = '',
    String contentType = 'audio/mp4',
  }) async {
    createCalls++;
    return const MobileMeetingRecording(
      recordingId: 'recording-remote-1',
      status: 'uploading',
    );
  }

  @override
  Future<MobileMeetingRecording> getMeetingRecording(String recordingId) async {
    getCalls++;
    return MobileMeetingRecording(
      recordingId: recordingId,
      status: 'processing',
    );
  }
}

class _FailingRemoteRecordingStore extends MobileLocalStore {
  int failedRemoteSaves = 0;

  @override
  Future<void> saveMeetingRecordingUpload(MeetingRecordingUpload upload) async {
    if (upload.recordingId.isNotEmpty) {
      failedRemoteSaves++;
      throw StateError('recording cache unavailable');
    }
  }

  @override
  Future<void> removeMeetingRecordingUpload(String localId) async {}
}

void main() {
  test('parses the Hub record-audio marker into an assistant request', () {
    final request = parseMeetingRecordingRequest(
      '__RECORD_AUDIO__{"title":"产品评审","purpose":"确认发布范围","hint":"录音仅在确认后开始"}',
      sourceQuery: '帮我记录这场产品评审',
    );

    expect(request, isNotNull);
    expect(request!.title, '产品评审');
    expect(request.purpose, '确认发布范围');
    expect(request.hint, '录音仅在确认后开始');
    expect(request.sourceQuery, '帮我记录这场产品评审');
  });

  test('rejects non-marker and malformed marker text', () {
    expect(parseMeetingRecordingRequest('正常回复'), isNull);
    expect(parseMeetingRecordingRequest('__RECORD_AUDIO__not-json'), isNull);
  });

  test('meeting consent copy preserves retention and storage disclosure', () {
    const copy = '请在开始前取得所有参会者同意。原始音频默认保留 30 天，可在完成后删除；逐字稿和纪要会继续保留。'
        '以 16kHz 单声道 PCM WAV 估算，每小时约占用 115 MB；受 512 MiB 上传上限限制，单次最多约 4 小时 39 分钟。';
    expect(copy, contains('参会者同意'));
    expect(copy, contains('30 天'));
    expect(copy, contains('115 MB'));
    expect(copy, contains('4 小时 39 分钟'));
  });

  test('meeting consent requires explicit attendee agreement', () {
    const consent = '我已取得所有参会者同意，并确认上传与留存安排';
    expect(consent, contains('所有参会者同意'));
    expect(consent, contains('上传与留存'));
  });

  test('offline recording falls back to archive-only mode', () {
    const offlineNotice = '暂时无法连接 Hub：本次将安全归档音频，网络恢复后自动上传。';
    expect(offlineNotice, contains('安全归档音频'));
    expect(offlineNotice, contains('网络恢复后自动上传'));
  });
  test('processing copy follows the persisted recording mode', () {
    expect(meetingRecordingProcessingMessage('minutes'), '正在生成会议纪要');
    expect(meetingRecordingProcessingMessage('transcript'), '正在生成逐字稿');
    expect(meetingRecordingProcessingMessage('keep'), '正在安全归档音频');
  });

  test('remote recording continues despite local persistence failure',
      () async {
    final directory = await Directory.systemTemp.createTemp('meeting-upload-');
    addTearDown(() async {
      if (await directory.exists()) await directory.delete(recursive: true);
    });
    final audio = File('${directory.path}${Platform.pathSeparator}audio.wav');
    await audio.writeAsBytes([1, 2, 3]);
    final api = _RecordingMeetingApiClient();
    final store = _FailingRemoteRecordingStore();
    final queue = MeetingRecordingUploadQueue(
      store: store,
      api: api,
    );
    final task = MeetingRecordingUpload(
      localId: 'local-1',
      localPath: audio.path,
      title: 'meeting',
      updatedAt: DateTime.utc(2026, 7, 22),
    );

    final uploaded = await queue.upload(task);
    expect(api.createCalls, 1);
    expect(api.getCalls, 1);
    expect(store.failedRemoteSaves, greaterThanOrEqualTo(2));
    expect(uploaded.recordingId, 'recording-remote-1');
    expect(uploaded.status, 'processing');
    expect(await audio.exists(), isFalse);
  });
}
