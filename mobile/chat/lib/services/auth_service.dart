import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

/// Handles authentication with the Hub server.
/// Reuses the Hub's email-based login flow:
///   POST /api/auth/email-request  → { status, poll_id, message }
///   POST /api/auth/email-poll     → { status, access_token, email, sn }
class AuthService {
  final String baseUrl;
  String? _token;
  String? _userId;

  AuthService({required this.baseUrl});

  String? get token => _token;
  String? get userId => _userId;
  bool get isLoggedIn => _token != null && _userId != null;

  /// Restore saved session from local storage.
  Future<bool> restoreSession() async {
    final prefs = await SharedPreferences.getInstance();
    _token = prefs.getString('auth_token');
    _userId = prefs.getString('auth_user_id');
    if (_token == null) return false;
    // Validate token is still valid.
    try {
      final resp = await http.get(
        Uri.parse('$baseUrl/api/chat/channels'),
        headers: {'Authorization': 'Bearer $_token'},
      ).timeout(const Duration(seconds: 10));
      return resp.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

  /// Step 1: Request email login link.
  /// Returns the poll_id for polling, and the status message.
  Future<EmailLoginResult> requestEmailLogin(String email) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/auth/email-request'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'email': email}),
    );
    if (resp.statusCode >= 400) {
      final body = jsonDecode(resp.body);
      throw AuthException(body['message'] as String? ?? 'Request failed');
    }
    final body = jsonDecode(resp.body) as Map<String, dynamic>;
    return EmailLoginResult(
      status: body['status'] as String? ?? '',
      message: body['message'] as String? ?? '',
      pollId: body['poll_id'] as String? ?? '',
    );
  }

  /// Step 2: Poll for login confirmation.
  /// Returns true when confirmed (token received), false if still pending.
  Future<bool> pollLogin(String pollId) async {
    try {
      final resp = await http.post(
        Uri.parse('$baseUrl/api/auth/email-poll'),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({'poll_id': pollId}),
      ).timeout(const Duration(seconds: 10));
      if (resp.statusCode >= 400) return false;
      final body = jsonDecode(resp.body) as Map<String, dynamic>;
      final status = body['status'] as String? ?? '';
      if (status == 'confirmed' && body['access_token'] != null) {
        _token = body['access_token'] as String;
        _userId = body['email'] as String? ?? body['sn'] as String? ?? '';
        final prefs = await SharedPreferences.getInstance();
        await prefs.setString('auth_token', _token!);
        await prefs.setString('auth_user_id', _userId!);
        return true;
      }
      return false;
    } catch (_) {
      return false;
    }
  }

  /// Logout: clear local session.
  Future<void> logout() async {
    _token = null;
    _userId = null;
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove('auth_token');
    await prefs.remove('auth_user_id');
  }
}

class EmailLoginResult {
  final String status;
  final String message;
  final String pollId;
  const EmailLoginResult({
    required this.status,
    required this.message,
    required this.pollId,
  });
}

class AuthException implements Exception {
  final String message;
  const AuthException(this.message);
  @override
  String toString() => 'AuthException: $message';
}
