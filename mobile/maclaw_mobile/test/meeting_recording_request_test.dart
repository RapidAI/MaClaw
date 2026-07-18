import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/meeting_recording/meeting_recording_request.dart';
import 'package:maclaw_mobile/features/meeting_recording/meeting_recording_upload_queue.dart';

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
}
