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

/// A third-party client's "scan to authorize" QR. Two variants share the
/// same `type` discriminator:
///
/// - Legacy (Hub-issued): carries `hub_url` — the session lives on that Hub.
/// - Relay (HubCenter-issued): carries `center_url` and NO `hub_url`. The
///   phone authorizes on its own account's Hub, then delivers the minted
///   token back to the center, which hands it to the waiting client.
class ThirdPartyAuthQrPayload {
  final String raw;
  final String sessionId;

  /// Legacy variant only: the Hub holding the session.
  final String hubUrl;

  /// Relay variant only: the HubCenter that will relay the credential.
  final String centerUrl;
  final String clientName;
  final DateTime? expiresAt;

  const ThirdPartyAuthQrPayload({
    required this.raw,
    required this.sessionId,
    this.hubUrl = '',
    this.centerUrl = '',
    this.clientName = '',
    this.expiresAt,
  });

  /// True when the QR was issued by a HubCenter and the credential must be
  /// delivered back to it after the Hub authorizes.
  bool get isRelay => centerUrl.isNotEmpty && hubUrl.isEmpty;
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

({String sessionId, String? hubUrl, DateTime? expiresAt}) _readQrCore(
  Map<String, dynamic> decoded,
) {
  final sessionId = ((decoded['session_id'] ?? '').toString()).trim();
  if (sessionId.isEmpty ||
      !RegExp(r'^[A-Za-z0-9._:-]{6,}$').hasMatch(sessionId)) {
    throw const FormatException('二维码中的会话标识无效');
  }
  String? hubUrl;
  final rawHub = ((decoded['hub_url'] ?? '').toString()).trim();
  if (rawHub.isNotEmpty) {
    final parsedHubUrl = Uri.tryParse(rawHub);
    if (parsedHubUrl == null ||
        parsedHubUrl.scheme != 'https' ||
        parsedHubUrl.host.isEmpty) {
      throw const FormatException('二维码中的 Hub 地址无效');
    }
    hubUrl = rawHub;
  }
  final expiresAt = _parseExpiry(decoded['expires_at']);
  if (expiresAt != null && !expiresAt.isAfter(DateTime.now().toUtc())) {
    throw const FormatException('二维码已过期，请在电脑端重新生成');
  }
  return (sessionId: sessionId, hubUrl: hubUrl, expiresAt: expiresAt);
}

/// Validates an https `center_url`; returns '' when absent.
String _readCenterUrl(Map<String, dynamic> decoded) {
  final raw = ((decoded['center_url'] ?? '').toString()).trim();
  if (raw.isEmpty) return '';
  final parsed = Uri.tryParse(raw);
  if (parsed == null || parsed.scheme != 'https' || parsed.host.isEmpty) {
    throw const FormatException('二维码中的 HubCenter 地址无效');
  }
  return raw;
}

DesktopAuthQrPayload parseDesktopAuthQrPayload(String qrPayload) {
  final decoded = _decodeQrMap(qrPayload);
  final type = ((decoded['type'] ?? '').toString()).trim();
  if (type != desktopAuthQrType) {
    throw const FormatException('二维码不是桌面端"移动认证"会话');
  }
  final core = _readQrCore(decoded);
  final hubUrl = core.hubUrl;
  if (hubUrl == null || hubUrl.isEmpty) {
    throw const FormatException('二维码中的 Hub 地址无效');
  }
  return DesktopAuthQrPayload(
    raw: qrPayload.trim(),
    sessionId: core.sessionId,
    hubUrl: hubUrl,
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
  final centerUrl = _readCenterUrl(decoded);
  // A third-party QR must point somewhere: a Hub session (legacy) or a
  // HubCenter relay session (new). Anything else is not actionable.
  if ((core.hubUrl ?? '').isEmpty && centerUrl.isEmpty) {
    throw const FormatException('二维码缺少有效的服务地址');
  }
  return ThirdPartyAuthQrPayload(
    raw: qrPayload.trim(),
    sessionId: core.sessionId,
    hubUrl: core.hubUrl ?? '',
    centerUrl: centerUrl,
    clientName: ((decoded['name'] ?? '').toString()).trim(),
    expiresAt: core.expiresAt,
  );
}

DateTime? _parseExpiry(Object? value) {
  if (value == null) return null;
  if (value is! String || value.trim().isEmpty) return null;
  return DateTime.tryParse(value.trim())?.toUtc();
}
