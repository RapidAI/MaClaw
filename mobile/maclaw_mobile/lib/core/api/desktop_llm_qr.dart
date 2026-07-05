import 'dart:convert';

const maclawMobileLlmAuthorizationType = 'maclaw_mobile_llm_authorization';

class DesktopLlmQrPayload {
  final String raw;
  final String type;
  final String sessionId;
  final String hubUrl;

  const DesktopLlmQrPayload({
    required this.raw,
    required this.type,
    required this.sessionId,
    required this.hubUrl,
  });
}

DesktopLlmQrPayload parseMaclawDesktopLlmQrPayload(String qrPayload) {
  final raw = qrPayload.trim();
  if (raw.isEmpty) {
    throw const FormatException('QR payload is required.');
  }
  final Object? decoded;
  try {
    decoded = jsonDecode(raw);
  } on FormatException {
    throw const FormatException(
      'QR payload must be MaClaw desktop GUI JSON.',
    );
  }
  if (decoded is! Map) {
    throw const FormatException(
      'QR payload must be MaClaw desktop GUI JSON.',
    );
  }
  final type = (decoded['type'] as String? ?? '').trim();
  if (type != maclawMobileLlmAuthorizationType) {
    throw const FormatException(
      'QR payload must be a MaClaw GUI LLM authorization session.',
    );
  }
  final sessionId = (decoded['session_id'] as String? ?? '').trim();
  if (sessionId.isEmpty) {
    throw const FormatException('session_id is required.');
  }
  final hubUrl = (decoded['hub_url'] as String? ?? '').trim();
  return DesktopLlmQrPayload(
    raw: raw,
    type: type,
    sessionId: sessionId,
    hubUrl: hubUrl,
  );
}
