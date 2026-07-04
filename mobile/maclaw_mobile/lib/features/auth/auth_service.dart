import 'package:dio/dio.dart';

import '../../core/api/desktop_llm_qr.dart';
import '../../core/api/official_service.dart';
import '../../core/storage/secure_vault.dart';

String _digitsOnly(String value) {
  final buffer = StringBuffer();
  for (final codeUnit in value.trim().codeUnits) {
    if (codeUnit >= 48 && codeUnit <= 57) {
      buffer.writeCharCode(codeUnit);
    }
  }
  return buffer.toString();
}

String _normalizePhoneCreditsAccount(String value) {
  final trimmed = value.trim();
  if (!trimmed.toLowerCase().startsWith('phone:')) return trimmed;
  final phone = trimmed.substring(trimmed.indexOf(':') + 1);
  if (!_phoneAccountValueCanNormalize(phone)) return trimmed;
  final digits = _digitsOnly(phone);
  return digits.isEmpty ? trimmed : 'phone:$digits';
}

bool _phoneAccountValueCanNormalize(String value) {
  var hasDigit = false;
  for (final codeUnit in value.trim().codeUnits) {
    if (codeUnit >= 48 && codeUnit <= 57) {
      hasDigit = true;
      continue;
    }
    if (codeUnit == 32 ||
        codeUnit == 43 ||
        codeUnit == 45 ||
        codeUnit == 40 ||
        codeUnit == 41) {
      continue;
    }
    return false;
  }
  return hasDigit;
}

class AuthService {
  final Dio _dio;
  final SecureVault _vault;
  String? _selectedHubCenterUrl;

  AuthService({
    SecureVault? vault,
    Dio? dio,
    String? hubCenterUrl,
  })  : _vault = vault ?? const SecureVault(),
        _selectedHubCenterUrl = hubCenterUrl,
        _dio = officialHubCenterDio(dio, hubCenterUrl: hubCenterUrl);

  Future<PhoneLoginRequestResult> requestPhoneLogin(String phoneNumber) async {
    final normalizedPhone = _requireNormalizedPhoneNumber(phoneNumber);
    final resolution = await tryOfficialHubCenters<PhoneLoginRequestResult>(
      dio: _dio,
      preferredHubCenterUrl: _selectedHubCenterUrl,
      operation: (client, hubCenterUrl) async {
        final routeResponse = await client.post<Map<String, dynamic>>(
          '/api/entry/probe',
          data: {'phone_number': normalizedPhone},
          options: Options(
            headers: {'X-MaClaw-HubCenter-URL': hubCenterUrl},
          ),
        );
        final route = PhoneLoginRouteResult.fromJson(
          routeResponse.data ?? const {},
        );
        final hub = route.selectedHub;
        if (hub == null || hub.baseUrl.isEmpty) {
          throw StateError(
            route.message.isEmpty
                ? 'HubCenter did not return an available Hub for this phone.'
                : route.message,
          );
        }
        final normalizedHubUrl = normalizeDiscoveredHubUrl(hub.baseUrl);
        final hubClient = _discoveredHubClient(normalizedHubUrl);
        final response = await hubClient.post<Map<String, dynamic>>(
          '/api/enroll/sms/send-code',
          data: {
            'phone_number': normalizedPhone,
            if (hub.tenantId.isNotEmpty) 'tenant_id': hub.tenantId,
          },
          options: _hubCenterHeaderOptions(hubCenterUrl),
        );
        return PhoneLoginRequestResult.fromJson(
          response.data ?? const {},
        ).copyWith(
          phoneNumber: normalizedPhone,
          hubUrl: normalizedHubUrl,
          hubId: hub.hubId,
          tenantId: hub.tenantId,
          tenantName: hub.tenantName,
          hubCenterUrl: hubCenterUrl,
        );
      },
    );
    _selectedHubCenterUrl = resolution.selectedHubCenterUrl;
    return resolution.value;
  }

  Future<PhoneLoginRequestResult> requestPhoneLoginOnHub({
    required String hubUrl,
    required String phoneNumber,
    String tenantId = '',
    String hubCenterUrl = '',
  }) async {
    final normalizedHubUrl = normalizeDiscoveredHubUrl(hubUrl);
    final normalizedPhone = _requireNormalizedPhoneNumber(phoneNumber);
    final client = _discoveredHubClient(normalizedHubUrl);
    final response = await client.post<Map<String, dynamic>>(
      '/api/enroll/sms/send-code',
      data: {
        'phone_number': normalizedPhone,
        if (tenantId.trim().isNotEmpty) 'tenant_id': tenantId.trim(),
      },
      options: _hubCenterHeaderOptions(hubCenterUrl),
    );
    return PhoneLoginRequestResult.fromJson(
      response.data ?? const {},
    ).copyWith(
      phoneNumber: normalizedPhone,
      hubUrl: normalizedHubUrl,
      tenantId: tenantId,
      hubCenterUrl: hubCenterUrl,
    );
  }

  Future<PhoneLoginVerifyResult> verifyPhoneLoginOnHub({
    required String hubUrl,
    required String phoneNumber,
    required String verifyCode,
    String tenantId = '',
    String hubCenterUrl = '',
  }) async {
    final normalizedHubUrl = normalizeDiscoveredHubUrl(hubUrl);
    final normalizedPhone = _requireNormalizedPhoneNumber(phoneNumber);
    final client = _discoveredHubClient(normalizedHubUrl);
    final response = await client.post<Map<String, dynamic>>(
      '/api/enroll/sms/verify-and-start',
      data: {
        'phone_number': normalizedPhone,
        'verify_code': verifyCode.trim(),
        'machine_name': 'MaClaw Mobile',
        'platform': 'mobile',
        'client_id': 'maclaw-mobile',
        if (tenantId.trim().isNotEmpty) 'tenant_id': tenantId.trim(),
      },
      options: _hubCenterHeaderOptions(hubCenterUrl),
    );
    final result = PhoneLoginVerifyResult.fromJson(
      response.data ?? const {},
    ).copyWith(
      phoneNumber: normalizedPhone,
      creditsAccount: 'phone:$normalizedPhone',
      hubUrl: normalizedHubUrl,
      hubCenterUrl: hubCenterUrl,
    );
    if (result.confirmed && result.accessToken.isEmpty) {
      throw StateError(
        'Hub confirmed phone login but did not return a mobile token.',
      );
    }
    if (result.confirmed) {
      await _vault.saveSession(
        hubUrl: result.hubUrl,
        token: result.accessToken,
      );
    }
    return result;
  }

  Future<MobileServiceConnectResult> redeemOfficialServiceCode(
    String code,
  ) async {
    return _connectWithOfficialHubCenter(
      data: {'code': code.trim()},
      path: '/api/mobile/service-redemptions',
    );
  }

  Future<MobileServiceConnectResult> connectWithDesktopLlmQr(
    String qrPayload,
  ) async {
    final payload = parseMaclawDesktopLlmQrPayload(qrPayload);
    if (payload.hubUrl.isNotEmpty) {
      return _consumeDesktopLlmQrOnHub(
        hubUrl: payload.hubUrl,
        qrPayload: payload.raw,
      );
    }
    return _connectWithOfficialHubCenter(
      data: {'qr_payload': payload.raw},
      path: '/api/mobile/llm/desktop-qr-sessions',
    );
  }

  Future<MobileServiceConnectResult> _connectWithOfficialHubCenter({
    required String path,
    required Map<String, dynamic> data,
  }) async {
    final resolution = await tryOfficialHubCenters<MobileServiceConnectResult>(
      dio: _dio,
      preferredHubCenterUrl: _selectedHubCenterUrl,
      operation: (client, hubCenterUrl) async {
        final response = await client.post<Map<String, dynamic>>(
          path,
          data: data,
          options: Options(
            headers: {'X-MaClaw-HubCenter-URL': hubCenterUrl},
          ),
        );
        return MobileServiceConnectResult.fromJson(
          response.data ?? const {},
        ).copyWith(hubCenterUrl: hubCenterUrl);
      },
    );
    _selectedHubCenterUrl = resolution.selectedHubCenterUrl;
    final result = resolution.value;
    if (result.accessToken.isEmpty || result.hubUrl.isEmpty) {
      if (result.requiresFollowUp) {
        throw MobileServiceConnectionPendingException(result);
      }
      throw StateError('Official service did not return a mobile Hub session.');
    }
    await _vault.saveSession(
      hubUrl: result.hubUrl,
      token: result.accessToken,
    );
    return result;
  }

  Future<MobileServiceConnectResult> _consumeDesktopLlmQrOnHub({
    required String hubUrl,
    required String qrPayload,
  }) async {
    final normalizedHubUrl = normalizeDiscoveredHubUrl(hubUrl);
    final client = _discoveredHubClient(normalizedHubUrl);
    final response = await client.post<Map<String, dynamic>>(
      '/api/mobile/llm/desktop-qr-sessions/consume',
      data: {'qr_payload': qrPayload},
    );
    final data = response.data ?? const {};
    final result = MobileServiceConnectResult.fromJson(data).copyWith(
      hubUrl: normalizedHubUrl,
    );
    if (result.accessToken.isEmpty) {
      throw StateError('Hub did not return a mobile token for desktop QR.');
    }
    await _vault.saveSession(
      hubUrl: result.hubUrl,
      token: result.accessToken,
    );
    return result;
  }

  Dio _discoveredHubClient(String hubUrl) {
    final client = discoveredHubDio(null, hubUrl: hubUrl);
    client.httpClientAdapter = _dio.httpClientAdapter;
    return client;
  }

  Options? _hubCenterHeaderOptions(String hubCenterUrl) {
    final value = hubCenterUrl.trim();
    if (value.isEmpty) return null;
    return Options(headers: {'X-MaClaw-HubCenter-URL': value});
  }

  String _normalizePhoneNumber(String phoneNumber) {
    return _digitsOnly(phoneNumber);
  }

  String _requireNormalizedPhoneNumber(String phoneNumber) {
    final normalizedPhone = _normalizePhoneNumber(phoneNumber);
    if (normalizedPhone.length < 8 || normalizedPhone.length > 15) {
      throw ArgumentError.value(
        phoneNumber,
        'phoneNumber',
        'Enter a valid phone number',
      );
    }
    return normalizedPhone;
  }
}

class PhoneLoginHub {
  final String hubId;
  final String tenantId;
  final String tenantName;
  final String name;
  final String baseUrl;
  final String status;

  const PhoneLoginHub({
    required this.hubId,
    required this.tenantId,
    required this.tenantName,
    required this.name,
    required this.baseUrl,
    required this.status,
  });

  factory PhoneLoginHub.fromJson(Map<String, dynamic> json) {
    return PhoneLoginHub(
      hubId: json['hub_id'] as String? ?? '',
      tenantId: json['tenant_id'] as String? ?? '',
      tenantName: json['tenant_name'] as String? ?? '',
      name: json['name'] as String? ?? '',
      baseUrl: json['base_url'] as String? ?? '',
      status: json['status'] as String? ?? '',
    );
  }
}

class PhoneLoginRouteResult {
  final String mode;
  final String message;
  final List<PhoneLoginHub> hubs;

  const PhoneLoginRouteResult({
    required this.mode,
    required this.message,
    required this.hubs,
  });

  PhoneLoginHub? get selectedHub {
    if (hubs.isEmpty) return null;
    for (final hub in hubs) {
      if (hub.status == 'online' && hub.baseUrl.isNotEmpty) return hub;
    }
    for (final hub in hubs) {
      if (hub.baseUrl.isNotEmpty) return hub;
    }
    return hubs.first;
  }

  factory PhoneLoginRouteResult.fromJson(Map<String, dynamic> json) {
    final rawHubs = json['hubs'];
    return PhoneLoginRouteResult(
      mode: json['mode'] as String? ?? '',
      message: json['message'] as String? ?? '',
      hubs: rawHubs is List
          ? rawHubs
              .whereType<Map>()
              .map(
                (item) => PhoneLoginHub.fromJson(
                  Map<String, dynamic>.from(item),
                ),
              )
              .toList(growable: false)
          : const [],
    );
  }
}

class PhoneLoginRequestResult {
  final String status;
  final String message;
  final String phoneNumber;
  final String hubUrl;
  final String hubId;
  final String tenantId;
  final String tenantName;
  final String hubCenterUrl;
  final int expiresMinutes;
  final int codeLength;

  const PhoneLoginRequestResult({
    required this.status,
    required this.message,
    required this.phoneNumber,
    required this.hubUrl,
    required this.hubId,
    required this.tenantId,
    required this.tenantName,
    required this.hubCenterUrl,
    required this.expiresMinutes,
    required this.codeLength,
  });

  factory PhoneLoginRequestResult.fromJson(Map<String, dynamic> json) {
    return PhoneLoginRequestResult(
      status: json['status'] as String? ??
          ((json['ok'] as bool? ?? false) ? 'sent' : ''),
      message: json['message'] as String? ?? '',
      phoneNumber: json['phone_number'] as String? ?? '',
      hubUrl: json['hub_url'] as String? ?? '',
      hubId: json['hub_id'] as String? ?? '',
      tenantId: json['tenant_id'] as String? ?? '',
      tenantName: json['tenant_name'] as String? ?? '',
      hubCenterUrl: json['hubcenter_url'] as String? ??
          json['hub_center_url'] as String? ??
          '',
      expiresMinutes: (json['expires_min'] as num?)?.toInt() ?? 0,
      codeLength: (json['code_length'] as num?)?.toInt() ?? 0,
    );
  }

  PhoneLoginRequestResult copyWith({
    String? status,
    String? message,
    String? phoneNumber,
    String? hubUrl,
    String? hubId,
    String? tenantId,
    String? tenantName,
    String? hubCenterUrl,
    int? expiresMinutes,
    int? codeLength,
  }) {
    return PhoneLoginRequestResult(
      status: status ?? this.status,
      message: message ?? this.message,
      phoneNumber: phoneNumber ?? this.phoneNumber,
      hubUrl: hubUrl ?? this.hubUrl,
      hubId: hubId ?? this.hubId,
      tenantId: tenantId ?? this.tenantId,
      tenantName: tenantName ?? this.tenantName,
      hubCenterUrl: hubCenterUrl ?? this.hubCenterUrl,
      expiresMinutes: expiresMinutes ?? this.expiresMinutes,
      codeLength: codeLength ?? this.codeLength,
    );
  }
}

class PhoneLoginVerifyResult {
  final String status;
  final String accessToken;
  final String phoneNumber;
  final String account;
  final String creditsAccount;
  final String tenantId;
  final String hubUrl;
  final String hubId;
  final String hubCenterUrl;
  final String llmMode;
  final String llmAuthorizationId;
  final bool isNewUser;

  const PhoneLoginVerifyResult({
    required this.status,
    required this.accessToken,
    required this.phoneNumber,
    required this.account,
    required this.creditsAccount,
    required this.tenantId,
    required this.hubUrl,
    required this.hubId,
    required this.hubCenterUrl,
    required this.llmMode,
    required this.llmAuthorizationId,
    required this.isNewUser,
  });

  bool get confirmed => accessToken.isNotEmpty || status == 'approved';

  factory PhoneLoginVerifyResult.fromJson(Map<String, dynamic> json) {
    final user = Map<String, dynamic>.from(json['user'] as Map? ?? const {});
    final hub = Map<String, dynamic>.from(json['hub'] as Map? ?? const {});
    final llm = Map<String, dynamic>.from(json['llm'] as Map? ?? const {});
    final hubUrl = json['hub_url'] as String? ??
        hub['base_url'] as String? ??
        hub['url'] as String? ??
        '';
    final rawPhoneNumber = json['phone_number'] as String? ??
        user['phone_number'] as String? ??
        '';
    final phoneNumber = _digitsOnly(rawPhoneNumber);
    final phoneAccount = phoneNumber.isEmpty ? '' : 'phone:$phoneNumber';
    final creditsAccount = _normalizePhoneCreditsAccount(
      json['credits_account'] as String? ??
          user['credits_account'] as String? ??
          llm['credits_account'] as String? ??
          phoneAccount,
    );
    final account = phoneAccount.isNotEmpty
        ? phoneAccount
        : json['account'] as String? ??
            user['account_id'] as String? ??
            json['email'] as String? ??
            user['email'] as String? ??
            '';
    return PhoneLoginVerifyResult(
      status: json['status'] as String? ?? '',
      accessToken: json['access_token'] as String? ??
          json['viewer_token'] as String? ??
          '',
      phoneNumber: phoneNumber,
      account: _normalizePhoneCreditsAccount(account),
      creditsAccount: creditsAccount,
      tenantId:
          json['tenant_id'] as String? ?? user['tenant_id'] as String? ?? '',
      hubUrl: hubUrl.isEmpty ? '' : normalizeDiscoveredHubUrl(hubUrl),
      hubId: json['hub_id'] as String? ?? hub['id'] as String? ?? '',
      hubCenterUrl: json['hubcenter_url'] as String? ??
          json['hub_center_url'] as String? ??
          '',
      llmMode: json['llm_mode'] as String? ??
          llm['mode'] as String? ??
          'maclaw_official',
      llmAuthorizationId: json['llm_authorization_id'] as String? ??
          llm['authorization_id'] as String? ??
          '',
      isNewUser: !(json['rebound_existing_user'] as bool? ?? false),
    );
  }

  PhoneLoginVerifyResult copyWith({
    String? status,
    String? accessToken,
    String? phoneNumber,
    String? account,
    String? creditsAccount,
    String? tenantId,
    String? hubUrl,
    String? hubId,
    String? hubCenterUrl,
    String? llmMode,
    String? llmAuthorizationId,
    bool? isNewUser,
  }) {
    return PhoneLoginVerifyResult(
      status: status ?? this.status,
      accessToken: accessToken ?? this.accessToken,
      phoneNumber: phoneNumber ?? this.phoneNumber,
      account: account ?? this.account,
      creditsAccount: creditsAccount == null
          ? this.creditsAccount
          : _normalizePhoneCreditsAccount(creditsAccount),
      tenantId: tenantId ?? this.tenantId,
      hubUrl: hubUrl ?? this.hubUrl,
      hubId: hubId ?? this.hubId,
      hubCenterUrl: hubCenterUrl ?? this.hubCenterUrl,
      llmMode: llmMode ?? this.llmMode,
      llmAuthorizationId: llmAuthorizationId ?? this.llmAuthorizationId,
      isNewUser: isNewUser ?? this.isNewUser,
    );
  }
}

class MobileServiceConnectResult {
  final String status;
  final String nextAction;
  final String message;
  final String accessToken;
  final String hubUrl;
  final String hubId;
  final String tenantId;
  final String hubCenterUrl;

  const MobileServiceConnectResult({
    required this.status,
    required this.nextAction,
    required this.message,
    required this.accessToken,
    required this.hubUrl,
    required this.hubId,
    required this.tenantId,
    required this.hubCenterUrl,
  });

  factory MobileServiceConnectResult.fromJson(Map<String, dynamic> json) {
    final hub = Map<String, dynamic>.from(json['hub'] as Map? ?? const {});
    final user = Map<String, dynamic>.from(json['user'] as Map? ?? const {});
    final hubUrl = json['hub_url'] as String? ??
        hub['base_url'] as String? ??
        hub['url'] as String? ??
        '';
    return MobileServiceConnectResult(
      status: json['status'] as String? ?? '',
      nextAction: json['next_action'] as String? ?? '',
      message: json['message'] as String? ?? '',
      accessToken: json['access_token'] as String? ?? '',
      hubUrl: hubUrl.isEmpty ? '' : normalizeDiscoveredHubUrl(hubUrl),
      hubId: json['hub_id'] as String? ?? hub['id'] as String? ?? '',
      tenantId:
          json['tenant_id'] as String? ?? user['tenant_id'] as String? ?? '',
      hubCenterUrl: json['hubcenter_url'] as String? ??
          json['hub_center_url'] as String? ??
          '',
    );
  }

  MobileServiceConnectResult copyWith({
    String? status,
    String? nextAction,
    String? message,
    String? accessToken,
    String? hubUrl,
    String? hubId,
    String? tenantId,
    String? hubCenterUrl,
  }) {
    return MobileServiceConnectResult(
      status: status ?? this.status,
      nextAction: nextAction ?? this.nextAction,
      message: message ?? this.message,
      accessToken: accessToken ?? this.accessToken,
      hubUrl: hubUrl ?? this.hubUrl,
      hubId: hubId ?? this.hubId,
      tenantId: tenantId ?? this.tenantId,
      hubCenterUrl: hubCenterUrl ?? this.hubCenterUrl,
    );
  }

  bool get requiresFollowUp {
    return accessToken.isEmpty &&
        hubUrl.isNotEmpty &&
        (status == 'requires_phone_login' || nextAction.isNotEmpty);
  }
}

class MobileServiceConnectionPendingException implements Exception {
  final MobileServiceConnectResult result;

  const MobileServiceConnectionPendingException(this.result);

  @override
  String toString() {
    final message = result.message.trim();
    if (message.isNotEmpty) return message;
    if (result.nextAction == 'phone_login') {
      return '兑换码已匹配到所属 Hub，请继续完成手机号验证码登录，由 Hub 签发手机访问凭据。';
    }
    return '兑换码已匹配到所属 Hub，还需要继续完成下一步接入。';
  }
}
