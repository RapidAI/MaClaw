import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';

const meetingRecordAudioMarker = '__RECORD_AUDIO__';

class MeetingRecordingRequest {
  final String title;
  final String purpose;
  final String hint;
  final String sourceQuery;

  const MeetingRecordingRequest(
      {required this.title,
      this.purpose = '',
      this.hint = '',
      this.sourceQuery = ''});
}

MeetingRecordingRequest? parseMeetingRecordingRequest(String value,
    {String sourceQuery = ''}) {
  if (!value.startsWith(meetingRecordAudioMarker)) return null;
  try {
    final raw = jsonDecode(value.substring(meetingRecordAudioMarker.length));
    final map = Map<String, dynamic>.from(raw as Map);
    final title = (map['title'] as String? ?? '').trim();
    return MeetingRecordingRequest(
        title: title.isEmpty ? '会议录音' : title,
        purpose: (map['purpose'] as String? ?? '').trim(),
        hint: (map['hint'] as String? ?? '').trim(),
        sourceQuery: sourceQuery);
  } on Object {
    return null;
  }
}

final meetingRecordingRequestProvider =
    StateProvider<MeetingRecordingRequest?>((_) => null);
