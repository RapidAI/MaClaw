import 'package:dio/dio.dart';

import '../../core/api/official_service.dart';
import '../../core/storage/secure_vault.dart';

class AuthService {
  final Dio _dio;
  final SecureVault _vault;

  AuthService({
    SecureVault? vault,
    Dio? dio,
  })  : _vault = vault ?? const SecureVault(),
        _dio = dio ?? Dio(BaseOptions(baseUrl: maclawOfficialServiceUrl));

  Future<EmailLoginRequestResult> requestEmailLogin(String email) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/auth/email-request',
      data: {'email': email.trim()},
    );
    return EmailLoginRequestResult.fromJson(response.data ?? const {});
  }

  Future<EmailLoginPollResult> pollEmailLogin(String pollId) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/auth/email-poll',
      data: {'poll_id': pollId},
    );
    final result = EmailLoginPollResult.fromJson(response.data ?? const {});
    if (result.confirmed && result.accessToken.isNotEmpty) {
      await _vault.saveSession(
        hubUrl: maclawOfficialServiceUrl,
        token: result.accessToken,
      );
    }
    return result;
  }
}

class EmailLoginRequestResult {
  final String status;
  final String message;
  final String pollId;

  const EmailLoginRequestResult({
    required this.status,
    required this.message,
    required this.pollId,
  });

  factory EmailLoginRequestResult.fromJson(Map<String, dynamic> json) {
    return EmailLoginRequestResult(
      status: json['status'] as String? ?? '',
      message: json['message'] as String? ?? '',
      pollId: json['poll_id'] as String? ?? '',
    );
  }
}

class EmailLoginPollResult {
  final String status;
  final String accessToken;
  final String email;
  final String tenantId;

  const EmailLoginPollResult({
    required this.status,
    required this.accessToken,
    required this.email,
    required this.tenantId,
  });

  bool get confirmed => status == 'confirmed';

  factory EmailLoginPollResult.fromJson(Map<String, dynamic> json) {
    final user = Map<String, dynamic>.from(json['user'] as Map? ?? const {});
    return EmailLoginPollResult(
      status: json['status'] as String? ?? '',
      accessToken: json['access_token'] as String? ?? '',
      email: json['email'] as String? ?? user['email'] as String? ?? '',
      tenantId:
          json['tenant_id'] as String? ?? user['tenant_id'] as String? ?? '',
    );
  }
}
