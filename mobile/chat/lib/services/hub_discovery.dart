import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

/// Discovers the real Hub server URL via HubCenter or direct probe.
class HubDiscovery {
  static const defaultCenterUrl = 'http://hubs.mypapers.top:9388';
  static const _prefKeyHubUrl = 'discovered_hub_url';
  static const _prefKeyHubName = 'discovered_hub_name';

  static String _trimSlash(String url) => url.replaceAll(RegExp(r'/+$'), '');

  /// Resolve hubs for an email via HubCenter.
  /// POST /api/entry/resolve { email } → { hubs: [...], default_hub_id, mode }
  static Future<ResolveResult> resolve(String email, {String? centerUrl}) async {
    final center = _trimSlash(centerUrl ?? defaultCenterUrl);
    final resp = await http.post(
      Uri.parse('$center/api/entry/resolve'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'email': email}),
    ).timeout(const Duration(seconds: 15));

    if (resp.statusCode >= 400) {
      final body = resp.body.isNotEmpty ? jsonDecode(resp.body) : {};
      throw HubDiscoveryException(body['message'] as String? ?? 'Resolve failed (${resp.statusCode})');
    }

    final body = jsonDecode(resp.body) as Map<String, dynamic>;
    final mode = body['mode'] as String? ?? 'none';
    final message = body['message'] as String? ?? '';
    final defaultHubId = body['default_hub_id'] as String? ?? '';
    final hubsList = body['hubs'] as List<dynamic>? ?? [];

    final hubs = hubsList.map((h) {
      final m = h as Map<String, dynamic>;
      return DiscoveredHub(
        hubId: m['hub_id'] as String? ?? '',
        name: m['name'] as String? ?? '',
        baseUrl: _trimSlash(m['base_url'] as String? ?? ''),
        pwaUrl: m['pwa_url'] as String? ?? '',
        visibility: m['visibility'] as String? ?? '',
        enrollmentMode: m['enrollment_mode'] as String? ?? '',
        invitationCodeRequired: m['invitation_code_required'] as bool? ?? false,
      );
    }).toList();

    return ResolveResult(
      mode: mode,
      message: message,
      defaultHubId: defaultHubId,
      hubs: hubs,
    );
  }

  /// Probe a direct hub URL to check if an email is bound.
  /// POST /api/entry/probe { email } → { status, pwa_url, enrollment_mode, ... }
  static Future<ProbeResult> probe(String hubUrl, String email) async {
    hubUrl = _trimSlash(hubUrl);
    final resp = await http.post(
      Uri.parse('$hubUrl/api/entry/probe'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'email': email}),
    ).timeout(const Duration(seconds: 15));

    if (resp.statusCode >= 400) {
      final body = resp.body.isNotEmpty ? jsonDecode(resp.body) : {};
      throw HubDiscoveryException(body['message'] as String? ?? 'Probe failed (${resp.statusCode})');
    }

    final body = jsonDecode(resp.body) as Map<String, dynamic>;
    return ProbeResult(
      status: body['status'] as String? ?? '',
      message: body['message'] as String? ?? '',
      pwaUrl: body['pwa_url'] as String? ?? '',
      enrollmentMode: body['enrollment_mode'] as String? ?? '',
      invitationCodeRequired: body['invitation_code_required'] as bool? ?? false,
      feishuAutoEnroll: body['feishu_auto_enroll'] as bool? ?? false,
    );
  }

  /// Enroll on a direct hub.
  /// POST /api/enroll/start { email, invitation_code } → { status, message }
  static Future<Map<String, dynamic>> enroll(String hubUrl, String email, {String? invitationCode, String? mobile}) async {
    hubUrl = _trimSlash(hubUrl);
    final resp = await http.post(
      Uri.parse('$hubUrl/api/enroll/start'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'email': email,
        if (invitationCode != null && invitationCode.isNotEmpty) 'invitation_code': invitationCode,
        if (mobile != null && mobile.isNotEmpty) 'mobile': mobile,
      }),
    ).timeout(const Duration(seconds: 30));

    final body = jsonDecode(resp.body) as Map<String, dynamic>;
    if (resp.statusCode >= 400) {
      throw HubDiscoveryException(body['message'] as String? ?? 'Enrollment failed');
    }
    return body;
  }

  /// Save discovered hub URL to local storage.
  static Future<void> saveHub(String hubUrl, String hubName) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_prefKeyHubUrl, _trimSlash(hubUrl));
    await prefs.setString(_prefKeyHubName, hubName);
  }

  /// Load previously saved hub URL.
  static Future<String?> loadHubUrl() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_prefKeyHubUrl);
  }

  /// Load previously saved hub name.
  static Future<String?> loadHubName() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_prefKeyHubName);
  }

  /// Clear saved hub.
  static Future<void> clearHub() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_prefKeyHubUrl);
    await prefs.remove(_prefKeyHubName);
  }
}

class ResolveResult {
  final String mode;
  final String message;
  final String defaultHubId;
  final List<DiscoveredHub> hubs;
  const ResolveResult({required this.mode, required this.message, required this.defaultHubId, required this.hubs});
}

class DiscoveredHub {
  final String hubId;
  final String name;
  final String baseUrl;
  final String pwaUrl;
  final String visibility;
  final String enrollmentMode;
  final bool invitationCodeRequired;
  const DiscoveredHub({
    required this.hubId, required this.name, required this.baseUrl,
    required this.pwaUrl, required this.visibility, required this.enrollmentMode,
    required this.invitationCodeRequired,
  });
}

class ProbeResult {
  final String status;
  final String message;
  final String pwaUrl;
  final String enrollmentMode;
  final bool invitationCodeRequired;
  final bool feishuAutoEnroll;
  const ProbeResult({
    required this.status, required this.message, required this.pwaUrl,
    required this.enrollmentMode, required this.invitationCodeRequired,
    required this.feishuAutoEnroll,
  });
}

class HubDiscoveryException implements Exception {
  final String message;
  const HubDiscoveryException(this.message);
  @override
  String toString() => 'HubDiscoveryException: $message';
}
