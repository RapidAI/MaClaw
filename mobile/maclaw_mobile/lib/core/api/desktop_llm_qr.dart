import 'dart:convert';

const maclawMobileLlmAuthorizationType = 'maclaw_mobile_llm_authorization';

class DesktopLlmQrPayload {
  final String raw;
  final String type;
  final String sessionId;
  final String hubUrl;
  final DateTime? expiresAt;

  const DesktopLlmQrPayload({
    required this.raw,
    required this.type,
    required this.sessionId,
    required this.hubUrl,
    this.expiresAt,
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
  const allowedKeys = {
    'v',
    'type',
    'session_id',
    'hub_url',
    'expires_at',
    'issued_at',
    'authorization_id',
    'nonce',
    'signature',
  };
  final unexpectedKeys = decoded.keys
      .whereType<String>()
      .where((key) => !allowedKeys.contains(key))
      .toList();
  if (unexpectedKeys.isNotEmpty) {
    throw const FormatException(
      'QR payload must not contain provider settings, endpoint URLs, or API keys.',
    );
  }
  for (final forbiddenKey in decoded.keys.whereType<String>()) {
    final normalizedKey = forbiddenKey.toLowerCase().replaceAll('_', '');
    if (normalizedKey.contains('apikey') ||
        normalizedKey == 'key' ||
        normalizedKey.contains('secret') ||
        normalizedKey.contains('provider') ||
        normalizedKey.contains('baseurl') ||
        normalizedKey == 'url' ||
        normalizedKey.contains('endpoint') ||
        normalizedKey.contains('model')) {
      throw const FormatException(
        'QR payload must not contain provider settings, endpoint URLs, or API keys.',
      );
    }
  }
  final sessionId = (decoded['session_id'] as String? ?? '').trim();
  if (sessionId.isEmpty) {
    throw const FormatException('session_id is required.');
  }
  if (!RegExp(r'^[A-Za-z0-9._:-]{6,}$').hasMatch(sessionId)) {
    throw const FormatException(
      'session_id must be a desktop GUI authorization session ID.',
    );
  }
  final hubUrl = (decoded['hub_url'] as String? ?? '').trim();
  final parsedHubUrl = Uri.tryParse(hubUrl);
  if (parsedHubUrl == null ||
      parsedHubUrl.scheme != 'https' ||
      parsedHubUrl.host.isEmpty) {
    throw const FormatException(
      'hub_url must be the HTTPS MaClaw Hub URL from the desktop GUI QR.',
    );
  }
  final expiresAt = _parseExpiry(decoded['expires_at']);
  if (expiresAt != null && !expiresAt.isAfter(DateTime.now().toUtc())) {
    throw const FormatException(
      'The MaClaw desktop GUI QR authorization session has expired.',
    );
  }
  return DesktopLlmQrPayload(
    raw: raw,
    type: type,
    sessionId: sessionId,
    hubUrl: hubUrl,
    expiresAt: expiresAt,
  );
}

DateTime? _parseExpiry(Object? value) {
  if (value == null) return null;
  if (value is! String || value.trim().isEmpty) {
    throw const FormatException('expires_at must be an ISO-8601 timestamp.');
  }
  final parsed = DateTime.tryParse(value.trim());
  if (parsed == null) {
    throw const FormatException('expires_at must be an ISO-8601 timestamp.');
  }
  return parsed.toUtc();
}
