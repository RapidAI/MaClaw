/// Persisted local upload task for a meeting recording. The raw audio stays on
/// the device until Hub has verified it, so a network failure never means a
/// user needs to record the meeting again.
class MeetingRecordingUpload {
  final String localId;
  final String localPath;
  final String title;
  final String purpose;
  final String conversationId;
  final String processMode;
  final String contentType;
  final String recordingId;
  final String status;
  final double durationSec;
  final int nextChunkIndex;
  final DateTime updatedAt;
  final String message;

  const MeetingRecordingUpload({
    required this.localId,
    required this.localPath,
    required this.title,
    this.purpose = '',
    this.conversationId = '',
    this.processMode = 'minutes',
    this.contentType = 'audio/wav',
    this.recordingId = '',
    this.status = 'pending',
    this.durationSec = 0,
    this.nextChunkIndex = 0,
    required this.updatedAt,
    this.message = '',
  });

  MeetingRecordingUpload copyWith({
    String? recordingId,
    String? status,
    int? nextChunkIndex,
    String? message,
    DateTime? updatedAt,
    bool clearSession = false,
  }) =>
      MeetingRecordingUpload(
        localId: localId,
        localPath: localPath,
        title: title,
        purpose: purpose,
        conversationId: conversationId,
        processMode: processMode,
        contentType: contentType,
        recordingId: clearSession ? '' : (recordingId ?? this.recordingId),
        status: status ?? this.status,
        durationSec: durationSec,
        nextChunkIndex:
            clearSession ? 0 : (nextChunkIndex ?? this.nextChunkIndex),
        updatedAt: updatedAt ?? this.updatedAt,
        message: message ?? this.message,
      );
}
