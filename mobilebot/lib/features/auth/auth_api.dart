import 'dart:io' show Platform;

import 'package:dio/dio.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../../core/account_store.dart';
import '../../core/hub_config.dart';
import '../../core/locale.dart';
import 'desktop_auth_qr.dart';

/// OS key reported to the Hub at enrollment so user management can show the
/// last used mobile client platform (Android / iOS / HarmonyOS).
String clientPlatformTag() {
  switch (Platform.operatingSystem) {
    case 'android':
      return 'android';
    case 'ios':
      return 'ios';
    case 'ohos':
    case 'harmonyos':
      return 'harmonyos';
    default:
      return 'mobile';
  }
}

/// Version string reported at enrollment, prefixed so distinct mobile apps
/// sharing an account remain identifiable on the admin user card.
Future<String> clientAppVersion() async {
  try {
    final version = (await PackageInfo.fromPlatform()).version.trim();
    return version.isEmpty ? 'mobilebot' : 'mobilebot/$version';
  } catch (_) {
    return 'mobilebot';
  }
}

/// Where a code-request left off: the Hub to verify against plus display
/// hints for the code step.
class CodeRequest {
  final String identity;
  final AuthMethod method;
  final HubRoute hub;
  final String hubCenterUrl;
  final String message;

  const CodeRequest({
    required this.identity,
    required this.method,
    required this.hub,
    required this.hubCenterUrl,
    this.message = '',
  });
}

enum AuthMethod { email, phone }

/// Outcome of `verify-and-start`.
class VerifyResult {
  final String status;
  final Account? account;
  final String message;

  const VerifyResult({this.status = '', this.account, this.message = ''});

  /// The hub enrolled this device; the account is usable.
  bool get confirmed => account != null && account!.token.isNotEmpty;

  /// The enrollment needs admin approval before it can call Hub APIs.
  bool get pendingApproval => !confirmed;
}

/// Login against the official HubCenters: resolve the user's Hub, send a
/// code, then enroll this phone as a machine on that account.
class AuthApi {
  /// Sends the login code. Returns the resolved Hub the verify step posts to.
  Future<CodeRequest> requestCode({
    required AuthMethod method,
    required String identity,
    String? preferredHubCenter,
  }) async {
    final normalized = method == AuthMethod.phone
        ? _normalizePhone(identity)
        : _normalizeEmail(identity);
    final resolve = await _resolveIdentity(
      method: method,
      identity: normalized,
      preferredHubCenter: preferredHubCenter,
    );
    final hub = resolve.hub;
    final dio = hubDio(hub.baseUrl);
    final path = method == AuthMethod.phone
        ? '/api/mobile/auth/phone/send-code'
        : '/api/enroll/email/send-code';
    final body = method == AuthMethod.phone
        ? {
            'phone_number': normalized,
            if (hub.tenantId.isNotEmpty) 'tenant_id': hub.tenantId,
          }
        : {
            'email': normalized,
            if (hub.tenantId.isNotEmpty) 'tenant_id': hub.tenantId,
          };
    late final Response<Map<String, dynamic>> response;
    try {
      response = await dio.post<Map<String, dynamic>>(
        path,
        data: body,
        options: Options(
          sendTimeout: const Duration(seconds: 15),
          receiveTimeout: const Duration(seconds: 45),
          headers: {'X-MaClaw-HubCenter-URL': resolve.hubCenterUrl},
        ),
      );
    } on DioException catch (error) {
      throw hubExceptionFrom(error);
    }
    final data = response.data ?? const <String, dynamic>{};
    return CodeRequest(
      identity: normalized,
      method: method,
      hub: hub,
      hubCenterUrl: resolve.hubCenterUrl,
      message: (data['message'] ?? '').toString().trim(),
    );
  }

  /// Verifies the code and enrolls this device on the resolved Hub.
  Future<VerifyResult> verifyCode({
    required CodeRequest request,
    required String code,
    required String machineName,
    required String clientId,
    String language = '',
  }) async {
    final hub = request.hub;
    final dio = hubDio(hub.baseUrl);
    final isPhone = request.method == AuthMethod.phone;
    final path = isPhone
        ? '/api/mobile/auth/phone/verify-and-start'
        : '/api/enroll/email/verify-and-start';
    final body = <String, dynamic>{
      if (isPhone)
        'phone_number': request.identity
      else
        'email': request.identity,
      'verify_code': code.trim(),
      'machine_name': machineName,
      'platform': clientPlatformTag(),
      'app_version': await clientAppVersion(),
      'client_id': clientId,
      if (hub.tenantId.isNotEmpty) 'tenant_id': hub.tenantId,
      if (language.trim().isNotEmpty) 'language': language.trim(),
    };
    late final Response<Map<String, dynamic>> response;
    try {
      response = await dio.post<Map<String, dynamic>>(
        path,
        data: body,
        options: Options(
          sendTimeout: const Duration(seconds: 15),
          receiveTimeout: const Duration(seconds: 45),
          headers: {'X-MaClaw-HubCenter-URL': request.hubCenterUrl},
        ),
      );
    } on DioException catch (error) {
      throw hubExceptionFrom(error);
    }
    final data = response.data ?? const <String, dynamic>{};
    final status = (data['status'] ?? '').toString().trim();
    final token = (data['access_token'] ?? data['viewer_token'] ?? '')
        .toString()
        .trim();
    final message = (data['message'] ?? '').toString().trim();
    if (token.isEmpty) {
      return VerifyResult(
        status: status.isNotEmpty ? status : 'pending',
        message: message.isNotEmpty ? message : sharedL10n.pendingApprovalHint,
      );
    }
    final account = Account(
      key: isPhone ? 'phone:${request.identity}' : 'email:${request.identity}',
      label: request.identity,
      hubUrl: normalizeHubUrl(hub.baseUrl),
      hubId: hub.hubId,
      hubCenterUrl: request.hubCenterUrl,
      tenantId: (data['tenant_id'] ?? hub.tenantId).toString().trim(),
      tenantName: (data['tenant_name'] ?? hub.tenantName).toString().trim(),
      userId: (data['user_id'] ?? '').toString().trim(),
      email: (data['email'] ?? (isPhone ? '' : request.identity))
          .toString()
          .trim(),
      phoneNumber: (data['phone_number'] ?? (isPhone ? request.identity : ''))
          .toString()
          .trim(),
      token: token,
      machineId: (data['machine_id'] ?? '').toString().trim(),
      machineToken: (data['machine_token'] ?? '').toString().trim(),
      clientId: clientId,
      addedAt: DateTime.now(),
    );
    return VerifyResult(status: status, account: account, message: message);
  }

  /// Exchanges a scanned desktop authentication QR (About → 移动认证二维码)
  /// for a machine enrollment on the QR owner's account. The QR is the
  /// credential — no prior login on this phone is needed.
  Future<VerifyResult> redeemDesktopQr({
    required DesktopAuthQrPayload payload,
    required String machineName,
    required String clientId,
    String language = '',
  }) async {
    final dio = hubDio(payload.hubUrl);
    late final Response<Map<String, dynamic>> response;
    try {
      response = await dio.post<Map<String, dynamic>>(
        '/api/mobile/auth/desktop-qr-sessions/redeem',
        data: {
          'qr_payload': payload.raw,
          'machine_name': machineName,
          'platform': clientPlatformTag(),
          'app_version': await clientAppVersion(),
          'client_id': clientId,
          if (language.isNotEmpty) 'language': language,
        },
        options: Options(
          sendTimeout: const Duration(seconds: 15),
          receiveTimeout: const Duration(seconds: 45),
        ),
      );
    } on DioException catch (error) {
      throw hubExceptionFrom(error);
    }
    final data = response.data ?? const <String, dynamic>{};
    final status = (data['status'] ?? '').toString().trim();
    final token = (data['access_token'] ?? data['viewer_token'] ?? '')
        .toString()
        .trim();
    final message = (data['message'] ?? '').toString().trim();
    if (token.isEmpty) {
      return VerifyResult(
        status: status.isNotEmpty ? status : 'pending',
        message: message.isNotEmpty ? message : sharedL10n.pendingApprovalHint,
      );
    }
    final phone = (data['phone_number'] ?? '').toString().trim();
    final email = (data['email'] ?? '').toString().trim();
    final identity = phone.isNotEmpty ? phone : email;
    final account = Account(
      key: phone.isNotEmpty ? 'phone:$phone' : 'email:${email.toLowerCase()}',
      label: email.isNotEmpty ? email : identity,
      hubUrl: normalizeHubUrl(payload.hubUrl),
      tenantId: (data['tenant_id'] ?? '').toString().trim(),
      tenantName: (data['tenant_name'] ?? '').toString().trim(),
      userId: (data['user_id'] ?? '').toString().trim(),
      email: email,
      phoneNumber: phone,
      token: token,
      machineId: (data['machine_id'] ?? '').toString().trim(),
      machineToken: (data['machine_token'] ?? '').toString().trim(),
      clientId: clientId,
      addedAt: DateTime.now(),
    );
    return VerifyResult(status: status, account: account, message: message);
  }

  /// Authorizes a third-party client's scan-to-login QR with the currently
  /// signed-in maclaw account. Sends this account's viewer_token to the Hub so
  /// the Hub can mint a fresh service credential for the polling client.
  ///
  /// The request goes to `accountHubUrl` (the Hub this account belongs to),
  /// not necessarily the hub_url embedded in the QR — the QR's hub_url only
  /// identifies which Hub holds the session. On the official service both are
  /// the same Hub. Returns the client display name from the Hub on success.
  Future<String> authorizeThirdParty({
    required ThirdPartyAuthQrPayload payload,
    required String accountHubUrl,
    required String accessToken,
  }) async {
    final dio = hubDio(accountHubUrl);
    late final Response<Map<String, dynamic>> response;
    try {
      response = await dio.post<Map<String, dynamic>>(
        '/api/mobile/auth/third-party-qr-sessions/authorize',
        data: {'qr_payload': payload.raw},
        options: Options(
          headers: {'Authorization': 'Bearer $accessToken'},
          sendTimeout: const Duration(seconds: 15),
          receiveTimeout: const Duration(seconds: 45),
        ),
      );
    } on DioException catch (error) {
      throw hubExceptionFrom(error);
    }
    final data = response.data ?? const <String, dynamic>{};
    final status = (data['status'] ?? '').toString().trim();
    if (status != 'authorized') {
      throw HubApiException('AUTH_NOT_CONFIRMED',
          (data['message'] ?? sharedL10n.pendingApprovalHint).toString());
    }
    return (data['client_name'] ?? '').toString().trim();
  }

  /// Relay flow step 1: creates a third-party session on the account's own
  /// Hub (`hubUrl`). Returns the parsed Hub QR payload (type
  /// maclaw_third_party_authorization) plus the session's poll_token —
  /// authorize the payload via [authorizeThirdParty], then exchange
  /// session_id + poll_token for the minted credential via
  /// [fetchThirdPartyToken]. The Hub never learns it was reached through a
  /// HubCenter relay.
  Future<({ThirdPartyAuthQrPayload payload, String pollToken})>
      createThirdPartyHubSession({
    required String hubUrl,
    String clientId = 'maclawbot-relay',
    String clientName = '第三方客户端',
  }) async {
    final dio = hubDio(hubUrl);
    late final Response<Map<String, dynamic>> response;
    try {
      response = await dio.post<Map<String, dynamic>>(
        '/api/mobile/auth/third-party-qr-sessions',
        data: {'client_id': clientId, 'name': clientName},
        options: Options(
          sendTimeout: const Duration(seconds: 15),
          receiveTimeout: const Duration(seconds: 45),
        ),
      );
    } on DioException catch (error) {
      throw hubExceptionFrom(error);
    }
    final data = response.data ?? const <String, dynamic>{};
    final qrPayload = (data['qr_payload'] ?? '').toString().trim();
    final pollToken = (data['poll_token'] ?? '').toString().trim();
    if (qrPayload.isEmpty || pollToken.isEmpty) {
      throw HubApiException('SESSION_CREATE_FAILED', 'Hub 未返回授权会话');
    }
    return (
      payload: parseThirdPartyAuthQrPayload(qrPayload),
      pollToken: pollToken,
    );
  }

  /// Relay flow step 3: fetches the minted viewer_token from the Hub session
  /// created by [createThirdPartyHubSession]. Call after
  /// [authorizeThirdParty]; the token is delivered exactly once.
  Future<({String accessToken, String email})> fetchThirdPartyToken({
    required String hubUrl,
    required String sessionId,
    required String pollToken,
  }) async {
    final dio = hubDio(hubUrl);
    late final Response<Map<String, dynamic>> response;
    try {
      response = await dio.post<Map<String, dynamic>>(
        '/api/mobile/auth/third-party-qr-sessions/token',
        data: {'session_id': sessionId, 'poll_token': pollToken},
        options: Options(
          sendTimeout: const Duration(seconds: 15),
          receiveTimeout: const Duration(seconds: 45),
        ),
      );
    } on DioException catch (error) {
      throw hubExceptionFrom(error);
    }
    final data = response.data ?? const <String, dynamic>{};
    final status = (data['status'] ?? '').toString().trim();
    final token = (data['access_token'] ?? '').toString().trim();
    if (status != 'authorized' || token.isEmpty) {
      throw HubApiException(
          'TOKEN_NOT_READY', status.isEmpty ? '授权凭证未就绪' : status);
    }
    return (
      accessToken: token,
      email: (data['email'] ?? '').toString().trim(),
    );
  }

  /// Relay flow step 4: hands {hub_url, access_token, email} to the
  /// HubCenter mailbox so the polling desktop client can pick the credential
  /// up (once). The phone's own viewer_token never leaves its Hub.
  Future<void> deliverThirdPartyToken({
    required String centerUrl,
    required String sessionId,
    required String hubUrl,
    required String accessToken,
    String email = '',
  }) async {
    final dio = hubCenterDio(baseUrl: centerUrl);
    late final Response<Map<String, dynamic>> response;
    try {
      response = await dio.post<Map<String, dynamic>>(
        '/api/third-party/sessions/deliver',
        data: {
          'session_id': sessionId,
          'hub_url': normalizeHubUrl(hubUrl),
          'access_token': accessToken,
          if (email.isNotEmpty) 'email': email,
        },
        options: Options(
          sendTimeout: const Duration(seconds: 15),
          receiveTimeout: const Duration(seconds: 45),
        ),
      );
    } on DioException catch (error) {
      throw hubExceptionFrom(error);
    }
    final status = ((response.data?['status']) ?? '').toString().trim();
    if (status != 'ok' && status != 'delivered') {
      throw HubApiException('DELIVER_FAILED', '授权凭证回传失败');
    }
  }

  Future<({HubRoute hub, String hubCenterUrl})> _resolveIdentity({
    required AuthMethod method,
    required String identity,
    String? preferredHubCenter,
  }) async {
    final candidates = <String>{
      if (preferredHubCenter != null && preferredHubCenter.trim().isNotEmpty)
        normalizeHubUrl(preferredHubCenter),
      ...maclawOfficialHubCenterUrls,
    }.toList();
    Object? lastError;
    for (final hubCenter in candidates) {
      try {
        final dio = hubCenterDio(baseUrl: hubCenter);
        final response = await dio.post<Map<String, dynamic>>(
          '/api/entry/resolve',
          data: {
            if (method == AuthMethod.phone)
              'phone_number': identity
            else
              'email': identity,
          },
          options: Options(headers: {'X-MaClaw-HubCenter-URL': hubCenter}),
        );
        final result = EntryResolveResult.fromJson(
          response.data ?? const <String, dynamic>{},
        );
        final hub = result.selectedHub;
        if (hub == null || hub.baseUrl.isEmpty) {
          throw HubApiException(
            'NO_HUB',
            result.message.isNotEmpty
                ? result.message
                : sharedL10n.hubNotFoundForAccount,
          );
        }
        return (hub: hub, hubCenterUrl: hubCenter);
      } on DioException catch (error) {
        // Try the next preset endpoint only on transport-level failures.
        final status = error.response?.statusCode ?? 0;
        if (status >= 400 && status < 500) throw hubExceptionFrom(error);
        lastError = error;
      }
    }
    throw hubExceptionFrom(
      lastError ?? HubApiException('NO_HUB', sharedL10n.hubCenterUnreachable),
    );
  }

  String _normalizeEmail(String raw) {
    final email = raw.trim().toLowerCase();
    if (!email.contains('@') || email.startsWith('@') || email.endsWith('@')) {
      throw ArgumentError.value(raw, 'email', sharedL10n.invalidEmail);
    }
    return email;
  }

  String _normalizePhone(String raw) {
    final digits = raw.replaceAll(RegExp(r'[^0-9]'), '');
    if (digits.length < 8 || digits.length > 15) {
      throw ArgumentError.value(raw, 'phoneNumber', sharedL10n.invalidPhone);
    }
    return digits;
  }
}
