import 'package:dio/dio.dart';

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

  /// SMS send can be slow (Aliyun + upstream); keep longer than generic Hub timeouts.
  static const smsSendReceiveTimeout = Duration(seconds: 45);
  static const smsSendConnectTimeout = Duration(seconds: 12);

  Future<PhoneLoginRequestResult> requestPhoneLogin(String phoneNumber) async {
    final normalizedPhone = _requireNormalizedPhoneNumber(phoneNumber);
    // Resolve Hub first so even a timed-out send-code still yields a verifiable
    // pending session (SMS may already have left the server).
    final routed =
        await tryOfficialHubCenters<({PhoneLoginHub hub, String hubCenterUrl})>(
      dio: _dio,
      preferredHubCenterUrl: _selectedHubCenterUrl,
      operation: (client, hubCenterUrl) async {
        final routeResponse = await client.post<Map<String, dynamic>>(
          '/api/entry/resolve',
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
        return (hub: hub, hubCenterUrl: hubCenterUrl);
      },
    );
    _selectedHubCenterUrl = routed.selectedHubCenterUrl;
    final hub = routed.value.hub;
    final hubCenterUrl = routed.value.hubCenterUrl;
    final normalizedHubUrl = normalizeDiscoveredHubUrl(hub.baseUrl);

    try {
      return await requestPhoneLoginOnHub(
        hubUrl: normalizedHubUrl,
        phoneNumber: normalizedPhone,
        tenantId: hub.tenantId,
        hubCenterUrl: hubCenterUrl,
      ).then(
        (result) => result.copyWith(
          hubId: hub.hubId.isNotEmpty ? hub.hubId : result.hubId,
          tenantName:
              hub.tenantName.isNotEmpty ? hub.tenantName : result.tenantName,
        ),
      );
    } on DioException catch (error) {
      // SMS often already dispatched when the client times out waiting for ACK.
      if (_isLikelyPostSendTransportError(error)) {
        return PhoneLoginRequestResult(
          status: 'sent_unconfirmed',
          message:
              '短信可能已发出，但网络回执超时。若已收到验证码请直接输入；未收到请 ${PhoneLoginRequestResult.defaultResendCooldownSeconds} 秒后重试。',
          phoneNumber: normalizedPhone,
          hubUrl: normalizedHubUrl,
          hubId: hub.hubId,
          tenantId: hub.tenantId,
          tenantName: hub.tenantName,
          hubCenterUrl: hubCenterUrl,
          expiresMinutes: 5,
          codeLength: 6,
          resendCooldownSeconds:
              PhoneLoginRequestResult.defaultResendCooldownSeconds,
          deliveryUnconfirmed: true,
        );
      }
      rethrow;
    }
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
      '/api/mobile/auth/phone/send-code',
      data: {
        'phone_number': normalizedPhone,
        if (tenantId.trim().isNotEmpty) 'tenant_id': tenantId.trim(),
      },
      options: Options(
        headers: {
          if (hubCenterUrl.trim().isNotEmpty)
            'X-MaClaw-HubCenter-URL': hubCenterUrl.trim(),
        },
        // Longer than default Hub timeouts — SMS gateway can be slow.
        sendTimeout: smsSendConnectTimeout,
        receiveTimeout: smsSendReceiveTimeout,
        // Accept 2xx only; non-2xx still throws so caller can classify.
        validateStatus: (code) => code != null && code >= 200 && code < 300,
      ),
    );
    final body = response.data ?? const <String, dynamic>{};
    final parsed = PhoneLoginRequestResult.fromJson(body).copyWith(
      phoneNumber: normalizedPhone,
      hubUrl: normalizedHubUrl,
      tenantId: tenantId,
      hubCenterUrl: hubCenterUrl,
    );
    // Server returned 2xx with ok:false (rare) — still surface message.
    final okFlag = body['ok'];
    if (okFlag is bool && !okFlag) {
      final msg = (body['message'] as String?)?.trim() ??
          (body['error'] is Map
              ? (body['error']['message'] as String? ?? '')
              : '');
      throw DioException(
        requestOptions: response.requestOptions,
        response: response,
        type: DioExceptionType.badResponse,
        message: msg.isEmpty ? 'SMS send rejected by Hub' : msg,
      );
    }
    return parsed;
  }

  bool _isLikelyPostSendTransportError(DioException error) {
    switch (error.type) {
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.sendTimeout:
      case DioExceptionType.receiveTimeout:
      case DioExceptionType.connectionError:
        return true;
      case DioExceptionType.unknown:
        // Connection reset / socket errors after request left the device.
        final msg = '${error.message ?? ''} ${error.error ?? ''}'.toLowerCase();
        return msg.contains('timeout') ||
            msg.contains('socket') ||
            msg.contains('connection') ||
            msg.contains('reset') ||
            msg.contains('closed');
      case DioExceptionType.badResponse:
        // 502/504 after SMS gateway may still have dispatched.
        final code = error.response?.statusCode ?? 0;
        return code == 502 || code == 503 || code == 504;
      default:
        return false;
    }
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
      '/api/mobile/auth/phone/verify-and-start',
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
    if (!_phoneAccountValueCanNormalize(phoneNumber) ||
        normalizedPhone.length < 8 ||
        normalizedPhone.length > 15) {
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
  final String defaultHubId;
  final List<PhoneLoginHub> hubs;

  const PhoneLoginRouteResult({
    required this.mode,
    required this.message,
    required this.defaultHubId,
    required this.hubs,
  });

  PhoneLoginHub? get selectedHub {
    if (hubs.isEmpty) return null;
    final usable = hubs.where((hub) => hub.baseUrl.isNotEmpty).toList();
    if (usable.isEmpty) return hubs.first;
    final preferred = defaultHubId.trim();
    if (preferred.isNotEmpty) {
      final preferredHubs =
          usable.where((hub) => hub.hubId == preferred).toList(growable: false);
      if (preferredHubs.isNotEmpty) {
        return _mostCompleteHub(preferredHubs);
      }
    }
    final online =
        usable.where((hub) => hub.status == 'online').toList(growable: false);
    if (online.isNotEmpty) {
      return _mostCompleteHub(online);
    }
    return _mostCompleteHub(usable);
  }

  factory PhoneLoginRouteResult.fromJson(Map<String, dynamic> json) {
    final rawHubs = json['hubs'];
    return PhoneLoginRouteResult(
      mode: json['mode'] as String? ?? '',
      message: json['message'] as String? ?? '',
      defaultHubId: json['default_hub_id'] as String? ?? '',
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

PhoneLoginHub _mostCompleteHub(Iterable<PhoneLoginHub> candidates) {
  return candidates.reduce((best, candidate) {
    return _hubCompletenessScore(candidate) > _hubCompletenessScore(best)
        ? candidate
        : best;
  });
}

int _hubCompletenessScore(PhoneLoginHub hub) {
  return (hub.status == 'online' ? 8 : 0) +
      (hub.tenantId.isNotEmpty ? 4 : 0) +
      (hub.tenantName.isNotEmpty ? 2 : 0) +
      (hub.name.isNotEmpty ? 1 : 0);
}

class PhoneLoginRequestResult {
  static const defaultResendCooldownSeconds = 60;

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
  final int resendCooldownSeconds;

  /// True when Hub resolve succeeded but send-code ACK was lost/timed out.
  final bool deliveryUnconfirmed;

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
    this.resendCooldownSeconds = defaultResendCooldownSeconds,
    this.deliveryUnconfirmed = false,
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
      resendCooldownSeconds:
          (json['resend_cooldown_seconds'] as num?)?.toInt() ??
              defaultResendCooldownSeconds,
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
    int? resendCooldownSeconds,
    bool? deliveryUnconfirmed,
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
      resendCooldownSeconds:
          resendCooldownSeconds ?? this.resendCooldownSeconds,
      deliveryUnconfirmed: deliveryUnconfirmed ?? this.deliveryUnconfirmed,
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
