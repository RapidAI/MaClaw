import 'dart:convert';

import 'package:dio/dio.dart';

import '../../core/api/official_service.dart';
import '../../core/storage/secure_vault.dart';

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

  Future<EmailLoginRequestResult> requestEmailLogin(String email) async {
    final resolution = await tryOfficialHubCenters<EmailLoginRequestResult>(
      dio: _dio,
      preferredHubCenterUrl: _selectedHubCenterUrl,
      operation: (client, hubCenterUrl) async {
        final response = await client.post<Map<String, dynamic>>(
          '/api/auth/email-request',
          data: {'email': email.trim()},
          options: Options(
            headers: {'X-MaClaw-HubCenter-URL': hubCenterUrl},
          ),
        );
        return EmailLoginRequestResult.fromJson(
          response.data ?? const {},
        ).copyWith(hubCenterUrl: hubCenterUrl);
      },
    );
    _selectedHubCenterUrl = resolution.selectedHubCenterUrl;
    return resolution.value;
  }

  Future<EmailLoginPollResult> pollEmailLogin(String pollId) async {
    final resolution = await tryOfficialHubCenters<EmailLoginPollResult>(
      dio: _dio,
      preferredHubCenterUrl: _selectedHubCenterUrl,
      operation: (client, hubCenterUrl) async {
        final response = await client.post<Map<String, dynamic>>(
          '/api/auth/email-poll',
          data: {'poll_id': pollId},
          options: Options(
            headers: {'X-MaClaw-HubCenter-URL': hubCenterUrl},
          ),
        );
        return EmailLoginPollResult.fromJson(
          response.data ?? const {},
        ).copyWith(hubCenterUrl: hubCenterUrl);
      },
    );
    _selectedHubCenterUrl = resolution.selectedHubCenterUrl;
    final result = resolution.value;
    if (result.confirmed &&
        (result.accessToken.isEmpty || result.hubUrl.isEmpty)) {
      throw StateError(
        'HubCenter confirmed login but did not return a Hub session.',
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

  Future<EmailLoginRequestResult> requestEmailLoginOnHub({
    required String hubUrl,
    required String email,
    String hubCenterUrl = '',
  }) async {
    final normalizedHubUrl = normalizeDiscoveredHubUrl(hubUrl);
    final client = _discoveredHubClient(normalizedHubUrl);
    final response = await client.post<Map<String, dynamic>>(
      '/api/auth/email-request',
      data: {'email': email.trim()},
      options: _hubCenterHeaderOptions(hubCenterUrl),
    );
    return EmailLoginRequestResult.fromJson(
      response.data ?? const {},
    ).copyWith(hubCenterUrl: hubCenterUrl);
  }

  Future<EmailLoginPollResult> pollEmailLoginOnHub({
    required String hubUrl,
    required String pollId,
    String hubCenterUrl = '',
  }) async {
    final normalizedHubUrl = normalizeDiscoveredHubUrl(hubUrl);
    final client = _discoveredHubClient(normalizedHubUrl);
    final response = await client.post<Map<String, dynamic>>(
      '/api/auth/email-poll',
      data: {'poll_id': pollId},
      options: _hubCenterHeaderOptions(hubCenterUrl),
    );
    final result = EmailLoginPollResult.fromJson(
      response.data ?? const {},
    ).copyWith(
      hubUrl: normalizedHubUrl,
      hubCenterUrl: hubCenterUrl,
    );
    if (result.confirmed && result.accessToken.isEmpty) {
      throw StateError(
        'Hub confirmed login but did not return a mobile token.',
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
    final hubUrl = _desktopQrHubUrl(qrPayload);
    if (hubUrl.isNotEmpty) {
      return _consumeDesktopLlmQrOnHub(
        hubUrl: hubUrl,
        qrPayload: qrPayload.trim(),
      );
    }
    return _connectWithOfficialHubCenter(
      data: {'qr_payload': qrPayload.trim()},
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

  String _desktopQrHubUrl(String qrPayload) {
    try {
      final decoded = jsonDecode(qrPayload.trim());
      if (decoded is! Map) return '';
      final type = decoded['type'] as String? ?? '';
      if (type != 'maclaw_mobile_llm_authorization') return '';
      return decoded['hub_url'] as String? ?? '';
    } catch (_) {
      return '';
    }
  }
}

class EmailLoginRequestResult {
  final String status;
  final String message;
  final String pollId;
  final String hubCenterUrl;

  const EmailLoginRequestResult({
    required this.status,
    required this.message,
    required this.pollId,
    required this.hubCenterUrl,
  });

  factory EmailLoginRequestResult.fromJson(Map<String, dynamic> json) {
    return EmailLoginRequestResult(
      status: json['status'] as String? ?? '',
      message: json['message'] as String? ?? '',
      pollId: json['poll_id'] as String? ?? '',
      hubCenterUrl: json['hubcenter_url'] as String? ??
          json['hub_center_url'] as String? ??
          '',
    );
  }

  EmailLoginRequestResult copyWith({
    String? status,
    String? message,
    String? pollId,
    String? hubCenterUrl,
  }) {
    return EmailLoginRequestResult(
      status: status ?? this.status,
      message: message ?? this.message,
      pollId: pollId ?? this.pollId,
      hubCenterUrl: hubCenterUrl ?? this.hubCenterUrl,
    );
  }
}

class EmailLoginPollResult {
  final String status;
  final String accessToken;
  final String email;
  final String tenantId;
  final String hubUrl;
  final String hubId;
  final String hubCenterUrl;
  final String llmMode;
  final String llmAuthorizationId;

  const EmailLoginPollResult({
    required this.status,
    required this.accessToken,
    required this.email,
    required this.tenantId,
    required this.hubUrl,
    required this.hubId,
    required this.hubCenterUrl,
    required this.llmMode,
    required this.llmAuthorizationId,
  });

  bool get confirmed => status == 'confirmed';

  factory EmailLoginPollResult.fromJson(Map<String, dynamic> json) {
    final user = Map<String, dynamic>.from(json['user'] as Map? ?? const {});
    final hub = Map<String, dynamic>.from(json['hub'] as Map? ?? const {});
    final llm = Map<String, dynamic>.from(json['llm'] as Map? ?? const {});
    final hubUrl = json['hub_url'] as String? ??
        hub['base_url'] as String? ??
        hub['url'] as String? ??
        '';
    return EmailLoginPollResult(
      status: json['status'] as String? ?? '',
      accessToken: json['access_token'] as String? ?? '',
      email: json['email'] as String? ?? user['email'] as String? ?? '',
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
    );
  }

  EmailLoginPollResult copyWith({
    String? status,
    String? accessToken,
    String? email,
    String? tenantId,
    String? hubUrl,
    String? hubId,
    String? hubCenterUrl,
    String? llmMode,
    String? llmAuthorizationId,
  }) {
    return EmailLoginPollResult(
      status: status ?? this.status,
      accessToken: accessToken ?? this.accessToken,
      email: email ?? this.email,
      tenantId: tenantId ?? this.tenantId,
      hubUrl: hubUrl ?? this.hubUrl,
      hubId: hubId ?? this.hubId,
      hubCenterUrl: hubCenterUrl ?? this.hubCenterUrl,
      llmMode: llmMode ?? this.llmMode,
      llmAuthorizationId: llmAuthorizationId ?? this.llmAuthorizationId,
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
        (status == 'requires_email_login' || nextAction.isNotEmpty);
  }
}

class MobileServiceConnectionPendingException implements Exception {
  final MobileServiceConnectResult result;

  const MobileServiceConnectionPendingException(this.result);

  @override
  String toString() {
    final message = result.message.trim();
    if (message.isNotEmpty) return message;
    if (result.nextAction == 'email_login') {
      return '兑换码已匹配到所属 Hub，请继续完成邮箱登录，由 Hub 签发手机访问凭据。';
    }
    return '兑换码已匹配到所属 Hub，还需要继续完成下一步接入。';
  }
}
