import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

/// Handles authentication with the Hub server.
/// Reuses the Hub's email-based login flow (email-request → email-confirm → poll).
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
      );
      return resp.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

  /// Step 1: Request email login link.
  Future<void> requestEmailLogin(String email) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/auth/email-request'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'email': email}),
    );
    if (resp.statusCode >= 400) {
      final body = jsonDecode(resp.body);
      throw AuthException(body['message'] as String? ?? 'Request failed');
    }
  }

  /// Step 2: Poll for login confirmation (user clicks link in email/IM).
  /// Returns true when confirmed, false if still pending.
  Future<bool> pollLogin(String email) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/auth/email-poll'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'email': email}),
    );
    if (resp.statusCode >= 400) return false;
    final body = jsonDecode(resp.body) as Map<String, dynamic>;
    final confirmed = body['confirmed'] as bool? ?? false;
    if (confirmed) {
      _token = body['token'] as String?;
      _userId = body['user_id'] as String?;
      if (_token != null) {
        final prefs = await SharedPreferences.getInstance();
        await prefs.setString('auth_token', _token!);
        await prefs.setString('auth_user_id', _userId!);
      }
    }
    return confirmed;
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

class AuthException implements Exception {
  final String message;
  const AuthException(this.message);
  @override
  String toString() => 'AuthException: $message';
}
