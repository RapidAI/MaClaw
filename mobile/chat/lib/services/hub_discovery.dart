import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

/// Discovers the real Hub server URL via HubCenter or direct probe.
///
/// Seed URLs mirror corelib/remote/defaults.go DefaultRemoteHubCenterURLs.
/// At runtime, [selectBestCenter] probes all seeds concurrently and picks the
/// fastest reachable node — the same algorithm as the Go client's
/// SelectBestCenter + DiscoverHubCenterURLs.
class HubDiscovery {
  /// Seed HubCenter URLs — must stay in sync with
  /// corelib/remote/defaults.go DefaultRemoteHubCenterURLs.
  static const defaultCenterUrls = [
    'https://hubs.mypapers.top',
    'https://hubs.maclaw.top',
    'https://hubs2.maclaw.top',
  ];

  /// Backward-compat alias: first seed URL.
  static String get defaultCenterUrl => defaultCenterUrls[0];
  static const _prefKeyHubUrl = 'discovered_hub_url';
  static const _prefKeyHubName = 'discovered_hub_name';

  static String _trimSlash(String url) => url.replaceAll(RegExp(r'/+$'), '');

  /// Resolve hubs for an email via HubCenter.
  /// Tries all seed URLs concurrently, picks the fastest reachable node,
  /// then calls POST /api/entry/resolve on it. Falls back to the next node
  /// on failure — mirrors Go's EnrollmentClient.ResolveHubs().
  static Future<ResolveResult> resolve(String email, {String? centerUrl}) async {
    final seeds = centerUrl != null && centerUrl.isNotEmpty
        ? [_trimSlash(centerUrl), ...defaultCenterUrls]
        : defaultCenterUrls;

    // Deduplicate while preserving order.
    final seen = <String>{};
    final unique = <String>[];
    for (final u in seeds) {
      final norm = _trimSlash(u);
      if (norm.isNotEmpty && seen.add(norm)) unique.add(norm);
    }

    // Probe all nodes concurrently, rank by response time.
    final ordered = await selectBestCenter(unique);

    // Try resolve on each node in quality order.
    Object? lastError;
    for (final center in ordered) {
      try {
        final resp = await http.post(
          Uri.parse('$center/api/entry/resolve'),
          headers: {'Content-Type': 'application/json'},
          body: jsonEncode({'email': email}),
        ).timeout(const Duration(seconds: 15));

        if (resp.statusCode >= 400) {
          final body = resp.body.isNotEmpty ? jsonDecode(resp.body) : {};
          lastError = HubDiscoveryException(
              body['message'] as String? ?? 'Resolve failed (${resp.statusCode})');
          continue;
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
            invitationCodeRequired:
                m['invitation_code_required'] as bool? ?? false,
          );
        }).toList();

        return ResolveResult(
          mode: mode,
          message: message,
          defaultHubId: defaultHubId,
          hubs: hubs,
        );
      } catch (e) {
        lastError = e;
        continue;
      }
    }

    throw lastError is HubDiscoveryException
        ? lastError
        : HubDiscoveryException('All hub centers unreachable: $lastError');
  }

  /// Concurrently probe all [urls] via GET /api/client/quality and return them
  /// sorted by reachability + response time (fastest first).
  /// Mirrors Go's SelectBestCenter in corelib/remote/hubcenter_probe.go.
  static Future<List<String>> selectBestCenter(List<String> urls) async {
    if (urls.length <= 1) return List.of(urls);

    final futures = urls.map((url) async {
      final sw = Stopwatch()..start();
      try {
        final resp = await http
            .get(Uri.parse('$url/api/client/quality'))
            .timeout(const Duration(seconds: 4));
        sw.stop();
        if (resp.statusCode == 200) {
          final body = jsonDecode(resp.body) as Map<String, dynamic>;
          return _CenterProbe(
            url: url,
            reachable: true,
            routable: body['routable'] as bool? ?? false,
            qualityScore: body['quality_score'] as int? ?? 0,
            rttMs: sw.elapsedMilliseconds,
          );
        }
      } catch (_) {
        // unreachable
      }
      return _CenterProbe(url: url, reachable: false, routable: false, qualityScore: 0, rttMs: 99999);
    });

    final probes = List.of(await Future.wait(futures));
    probes.sort((a, b) {
      if (a.reachable != b.reachable) return a.reachable ? -1 : 1;
      if (a.routable != b.routable) return a.routable ? -1 : 1;
      if (a.qualityScore != b.qualityScore) return b.qualityScore.compareTo(a.qualityScore);
      return a.rttMs.compareTo(b.rttMs);
    });

    return probes.map((p) => p.url).toList();
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
  const ProbeResult({
    required this.status, required this.message, required this.pwaUrl,
    required this.enrollmentMode, required this.invitationCodeRequired,
  });
}

class HubDiscoveryException implements Exception {
  final String message;
  const HubDiscoveryException(this.message);
  @override
  String toString() => 'HubDiscoveryException: $message';
}

/// Internal probe result for [HubDiscovery.selectBestCenter].
class _CenterProbe {
  final String url;
  final bool reachable;
  final bool routable;
  final int qualityScore;
  final int rttMs;
  const _CenterProbe({
    required this.url,
    required this.reachable,
    required this.routable,
    required this.qualityScore,
    required this.rttMs,
  });
}
