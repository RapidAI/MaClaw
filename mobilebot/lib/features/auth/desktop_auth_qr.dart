import 'dart:convert';

/// `type` the desktop About-panel "移动认证二维码" carries.
const desktopAuthQrType = 'maclaw_mobile_desktop_authorization';

/// The decoded authentication QR payload shown by the desktop GUI.
/// The QR itself is the credential: `hub_url` + one-time `session_id`.
class DesktopAuthQrPayload {
  final String raw;
  final String sessionId;
  final String hubUrl;
  final DateTime? expiresAt;

  const DesktopAuthQrPayload({
    required this.raw,
    required this.sessionId,
    required this.hubUrl,
    this.expiresAt,
  });
}

DesktopAuthQrPayload parseDesktopAuthQrPayload(String qrPayload) {
  final raw = qrPayload.trim();
  if (raw.isEmpty) {
    throw const FormatException('二维码内容为空');
  }
  final Object? decoded;
  try {
    decoded = jsonDecode(raw);
  } on FormatException {
    throw const FormatException('二维码不是 MaClaw 桌面端生成的 JSON');
  }
  if (decoded is! Map) {
    throw const FormatException('二维码不是 MaClaw 桌面端生成的 JSON');
  }
  final type = (decoded['type'] as String? ?? '').trim();
  if (type != desktopAuthQrType) {
    throw const FormatException('二维码不是桌面端"移动认证"会话');
  }
  final sessionId = (decoded['session_id'] as String? ?? '').trim();
  if (sessionId.isEmpty ||
      !RegExp(r'^[A-Za-z0-9._:-]{6,}$').hasMatch(sessionId)) {
    throw const FormatException('二维码中的会话标识无效');
  }
  final hubUrl = (decoded['hub_url'] as String? ?? '').trim();
  final parsedHubUrl = Uri.tryParse(hubUrl);
  if (parsedHubUrl == null ||
      parsedHubUrl.scheme != 'https' ||
      parsedHubUrl.host.isEmpty) {
    throw const FormatException('二维码中的 Hub 地址无效');
  }
  final expiresAt = _parseExpiry(decoded['expires_at']);
  if (expiresAt != null && !expiresAt.isAfter(DateTime.now().toUtc())) {
    throw const FormatException('二维码已过期，请在电脑端重新生成');
  }
  return DesktopAuthQrPayload(
    raw: raw,
    sessionId: sessionId,
    hubUrl: hubUrl,
    expiresAt: expiresAt,
  );
}

DateTime? _parseExpiry(Object? value) {
  if (value == null) return null;
  if (value is! String || value.trim().isEmpty) return null;
  return DateTime.tryParse(value.trim())?.toUtc();
}
