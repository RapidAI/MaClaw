import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';

void main() {
  test('parses Hub meeting recording failure and document fields', () {
    final recording = MobileMeetingRecording.fromJson({
      'recording_id': 'meeting-1',
      'status': 'failed',
      'failure_code': 'AUDIO_MISSING_FOR_RETRY',
      'audio_available': false,
      'transcript_draft_id': 'transcript-1',
      'minutes_draft_id': 'minutes-1',
    });

    expect(recording.recordingId, 'meeting-1');
    expect(recording.failureCode, 'AUDIO_MISSING_FOR_RETRY');
    expect(recording.audioAvailable, isFalse);
    expect(recording.transcriptDraftId, 'transcript-1');
    expect(recording.minutesDraftId, 'minutes-1');
  });
}
