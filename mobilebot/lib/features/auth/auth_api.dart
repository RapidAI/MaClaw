import 'package:dio/dio.dart';

import '../../core/account_store.dart';
import '../../core/hub_config.dart';
import 'desktop_auth_qr.dart';

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
      message: (data['message'] as String? ?? '').trim(),
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
      if (isPhone) 'phone_number': request.identity else 'email': request.identity,
      'verify_code': code.trim(),
      'machine_name': machineName,
      'platform': 'mobile',
      'app_version': 'mobilebot/1.0.0',
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
    final status = (data['status'] as String? ?? '').trim();
    final token = (data['access_token'] as String? ??
            data['viewer_token'] as String? ??
            '')
        .trim();
    final message = (data['message'] as String? ?? '').trim();
    if (token.isEmpty) {
      return VerifyResult(
        status: status.isNotEmpty ? status : 'pending',
        message: message.isNotEmpty ? message : '注册已提交，等待审批通过后再登录',
      );
    }
    final account = Account(
      key: isPhone ? 'phone:${request.identity}' : 'email:${request.identity}',
      label: request.identity,
      hubUrl: normalizeHubUrl(hub.baseUrl),
      hubId: hub.hubId,
      hubCenterUrl: request.hubCenterUrl,
      tenantId: (data['tenant_id'] as String? ?? hub.tenantId).trim(),
      tenantName:
          (data['tenant_name'] as String? ?? hub.tenantName).trim(),
      userId: (data['user_id'] as String? ?? '').trim(),
      email: (data['email'] as String? ??
              (isPhone ? '' : request.identity))
          .trim(),
      phoneNumber: (data['phone_number'] as String? ??
              (isPhone ? request.identity : ''))
          .trim(),
      token: token,
      machineId: (data['machine_id'] as String? ?? '').trim(),
      machineToken: (data['machine_token'] as String? ?? '').trim(),
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
          'platform': 'mobile',
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
    final status = (data['status'] as String? ?? '').trim();
    final token = (data['access_token'] as String? ??
            data['viewer_token'] as String? ??
            '')
        .trim();
    final message = (data['message'] as String? ?? '').trim();
    if (token.isEmpty) {
      return VerifyResult(
        status: status.isNotEmpty ? status : 'pending',
        message: message.isNotEmpty ? message : '注册已提交，等待审批通过后再登录',
      );
    }
    final phone = (data['phone_number'] as String? ?? '').trim();
    final email = (data['email'] as String? ?? '').trim();
    final identity = phone.isNotEmpty ? phone : email;
    final account = Account(
      key: phone.isNotEmpty ? 'phone:$phone' : 'email:${email.toLowerCase()}',
      label: email.isNotEmpty ? email : identity,
      hubUrl: normalizeHubUrl(payload.hubUrl),
      tenantId: (data['tenant_id'] as String? ?? '').trim(),
      tenantName: (data['tenant_name'] as String? ?? '').trim(),
      userId: (data['user_id'] as String? ?? '').trim(),
      email: email,
      phoneNumber: phone,
      token: token,
      machineId: (data['machine_id'] as String? ?? '').trim(),
      machineToken: (data['machine_token'] as String? ?? '').trim(),
      clientId: clientId,
      addedAt: DateTime.now(),
    );
    return VerifyResult(status: status, account: account, message: message);
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
          options: Options(
            headers: {'X-MaClaw-HubCenter-URL': hubCenter},
          ),
        );
        final result =
            EntryResolveResult.fromJson(response.data ?? const <String, dynamic>{});
        final hub = result.selectedHub;
        if (hub == null || hub.baseUrl.isEmpty) {
          throw HubApiException(
            'NO_HUB',
            result.message.isNotEmpty ? result.message : '未能为该账号找到可用的 Hub',
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
    throw hubExceptionFrom(lastError ?? const HubApiException('NO_HUB', '无法连接 HubCenter'));
  }

  String _normalizeEmail(String raw) {
    final email = raw.trim().toLowerCase();
    if (!email.contains('@') || email.startsWith('@') || email.endsWith('@')) {
      throw ArgumentError.value(raw, 'email', '请输入有效的邮箱地址');
    }
    return email;
  }

  String _normalizePhone(String raw) {
    final digits = raw.replaceAll(RegExp(r'[^0-9]'), '');
    if (digits.length < 8 || digits.length > 15) {
      throw ArgumentError.value(raw, 'phoneNumber', '请输入有效的手机号');
    }
    return digits;
  }
}
