import 'dart:convert';

/// `type` the desktop About-panel "移动认证二维码" carries.
const desktopAuthQrType = 'maclaw_mobile_desktop_authorization';

/// `type` carried by a third-party client's "scan to authorize" QR
/// (e.g. the RapidEditor AI assistant asking for the maclaw official service).
const thirdPartyAuthQrType = 'maclaw_third_party_authorization';

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

/// A third-party client's "scan to authorize" QR. It shares the same
/// transport fields as the desktop auth QR (type / session_id / hub_url /
/// expires_at); only the `type` discriminator differs.
class ThirdPartyAuthQrPayload {
  final String raw;
  final String sessionId;
  final String hubUrl;
  final String clientName;
  final DateTime? expiresAt;

  const ThirdPartyAuthQrPayload({
    required this.raw,
    required this.sessionId,
    required this.hubUrl,
    this.clientName = '',
    this.expiresAt,
  });
}

/// Common JSON / field validation shared by both QR payload kinds.
Map<String, dynamic> _decodeQrMap(String qrPayload) {
  final raw = qrPayload.trim();
  if (raw.isEmpty) {
    throw const FormatException('二维码内容为空');
  }
  final Object? decoded;
  try {
    decoded = jsonDecode(raw);
  } on FormatException {
    throw const FormatException('二维码不是 MaClaw 生成的 JSON');
  }
  if (decoded is! Map) {
    throw const FormatException('二维码不是 MaClaw 生成的 JSON');
  }
  return Map<String, dynamic>.from(decoded);
}

({String sessionId, String hubUrl, DateTime? expiresAt}) _readQrCore(
  Map<String, dynamic> decoded,
) {
  final sessionId = ((decoded['session_id'] ?? '').toString()).trim();
  if (sessionId.isEmpty ||
      !RegExp(r'^[A-Za-z0-9._:-]{6,}$').hasMatch(sessionId)) {
    throw const FormatException('二维码中的会话标识无效');
  }
  final hubUrl = ((decoded['hub_url'] ?? '').toString()).trim();
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
  return (sessionId: sessionId, hubUrl: hubUrl, expiresAt: expiresAt);
}

DesktopAuthQrPayload parseDesktopAuthQrPayload(String qrPayload) {
  final decoded = _decodeQrMap(qrPayload);
  final type = ((decoded['type'] ?? '').toString()).trim();
  if (type != desktopAuthQrType) {
    throw const FormatException('二维码不是桌面端"移动认证"会话');
  }
  final core = _readQrCore(decoded);
  return DesktopAuthQrPayload(
    raw: qrPayload.trim(),
    sessionId: core.sessionId,
    hubUrl: core.hubUrl,
    expiresAt: core.expiresAt,
  );
}

ThirdPartyAuthQrPayload parseThirdPartyAuthQrPayload(String qrPayload) {
  final decoded = _decodeQrMap(qrPayload);
  final type = ((decoded['type'] ?? '').toString()).trim();
  if (type != thirdPartyAuthQrType) {
    throw const FormatException('二维码不是第三方设备授权请求');
  }
  final core = _readQrCore(decoded);
  return ThirdPartyAuthQrPayload(
    raw: qrPayload.trim(),
    sessionId: core.sessionId,
    hubUrl: core.hubUrl,
    clientName: ((decoded['name'] ?? '').toString()).trim(),
    expiresAt: core.expiresAt,
  );
}

DateTime? _parseExpiry(Object? value) {
  if (value == null) return null;
  if (value is! String || value.trim().isEmpty) return null;
  return DateTime.tryParse(value.trim())?.toUtc();
}
