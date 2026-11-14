/// Official MaClaw HubCenter endpoints used to discover which Hub serves an
/// account. Mirrors `mobile/maclaw_mobile/lib/core/api/official_service.dart`.
library;

import 'package:dio/dio.dart';

const maclawDefaultHubCenterUrl = 'https://hubs.mypapers.top';
const maclawOfficialHubCenterUrls = [
  maclawDefaultHubCenterUrl,
  'https://hubs.maclaw.top',
  'https://hubs2.maclaw.top',
];

const hubCenterConnectTimeout = Duration(seconds: 8);
const hubCenterSendTimeout = Duration(seconds: 8);
const hubCenterReceiveTimeout = Duration(seconds: 15);

const hubConnectTimeout = Duration(seconds: 15);
const hubSendTimeout = Duration(seconds: 30);
const hubReceiveTimeout = Duration(seconds: 35);

/// Bot turns can stay admitted for minutes; the message POST itself stays
/// short because `Prefer: respond-async` answers 202 quickly.
const botPollInterval = Duration(milliseconds: 1600);

/// Matches the desktop's `BOT_PENDING_REPLY_WAIT_MS` — a turn may legally
/// run for half an hour; giving up early only hides the poll watch, the
/// journal still lands the reply either way.
const botTurnMaxWait = Duration(minutes: 31);

/// One hub a HubCenter resolve handed back.
class HubRoute {
  final String hubId;
  final String tenantId;
  final String tenantName;
  final String name;
  final String baseUrl;
  final String status;

  const HubRoute({
    this.hubId = '',
    this.tenantId = '',
    this.tenantName = '',
    this.name = '',
    this.baseUrl = '',
    this.status = '',
  });

  factory HubRoute.fromJson(Map<String, dynamic> json) => HubRoute(
        hubId: json['hub_id'] as String? ?? '',
        tenantId: json['tenant_id'] as String? ?? '',
        tenantName: json['tenant_name'] as String? ?? '',
        name: json['name'] as String? ?? '',
        baseUrl: json['base_url'] as String? ?? '',
        status: json['status'] as String? ?? '',
      );

  int get completenessScore =>
      (status == 'online' ? 8 : 0) +
      (tenantId.isNotEmpty ? 4 : 0) +
      (tenantName.isNotEmpty ? 2 : 0) +
      (name.isNotEmpty ? 1 : 0);
}

/// `POST /api/entry/resolve` response.
class EntryResolveResult {
  final String mode;
  final String message;
  final String defaultHubId;
  final List<HubRoute> hubs;

  const EntryResolveResult({
    this.mode = '',
    this.message = '',
    this.defaultHubId = '',
    this.hubs = const [],
  });

  factory EntryResolveResult.fromJson(Map<String, dynamic> json) {
    final raw = json['hubs'];
    return EntryResolveResult(
      mode: json['mode'] as String? ?? '',
      message: json['message'] as String? ?? '',
      defaultHubId: json['default_hub_id'] as String? ?? '',
      hubs: raw is List
          ? raw
              .whereType<Map>()
              .map((e) => HubRoute.fromJson(Map<String, dynamic>.from(e)))
              .toList(growable: false)
          : const [],
    );
  }

  /// Picks the most usable hub: prefer the server default, then online hubs,
  /// then whichever candidate carries the most routing detail.
  HubRoute? get selectedHub {
    if (hubs.isEmpty) return null;
    final usable = hubs.where((h) => h.baseUrl.isNotEmpty).toList();
    if (usable.isEmpty) return hubs.first;
    Iterable<HubRoute> pool = usable;
    final preferred = defaultHubId.trim();
    if (preferred.isNotEmpty) {
      final match = pool.where((h) => h.hubId == preferred).toList();
      if (match.isNotEmpty) pool = match;
    } else {
      final online = pool.where((h) => h.status == 'online').toList();
      if (online.isNotEmpty) pool = online;
    }
    return pool.reduce(
      (a, b) => b.completenessScore > a.completenessScore ? b : a,
    );
  }
}

String normalizeHubUrl(String raw) {
  var url = raw.trim();
  if (url.isEmpty) return url;
  if (!url.startsWith('http://') && !url.startsWith('https://')) {
    url = 'https://$url';
  }
  while (url.endsWith('/')) {
    url = url.substring(0, url.length - 1);
  }
  return url;
}

Dio hubCenterDio({String? baseUrl}) => Dio(
      BaseOptions(
        baseUrl: normalizeHubUrl(baseUrl ?? maclawDefaultHubCenterUrl),
        connectTimeout: hubCenterConnectTimeout,
        sendTimeout: hubCenterSendTimeout,
        receiveTimeout: hubCenterReceiveTimeout,
      ),
    );

Dio hubDio(String hubUrl) => Dio(
      BaseOptions(
        baseUrl: normalizeHubUrl(hubUrl),
        connectTimeout: hubConnectTimeout,
        sendTimeout: hubSendTimeout,
        receiveTimeout: hubReceiveTimeout,
      ),
    );

/// Error shape Hub answers with: `{"error": CODE, "message": "…"}`.
class HubApiException implements Exception {
  final String code;
  final String message;
  final int? statusCode;

  const HubApiException(this.code, this.message, {this.statusCode});

  bool get isUnauthorized =>
      statusCode == 401 ||
      statusCode == 403 ||
      code == 'UNAUTHORIZED' ||
      code == 'MACHINE_UNAUTHORIZED';

  /// Stricter than [isUnauthorized]: the Hub also answers 403 for
  /// feature-level denials (e.g. `BOT_DISABLED` when bot access is off),
  /// which must NOT bounce the session to re-login. Only a bare 401 or an
  /// explicit credential-rejection code means the token itself is dead.
  bool get isAuthRejected =>
      statusCode == 401 ||
      code == 'UNAUTHORIZED' ||
      code == 'MACHINE_UNAUTHORIZED';

  @override
  String toString() => 'HubApiException($code, $statusCode): $message';
}

HubApiException hubExceptionFrom(Object error) {
  if (error is HubApiException) return error;
  if (error is DioException) {
    final data = error.response?.data;
    var code = '';
    var message = '';
    if (data is Map) {
      final rawError = data['error'];
      // Most Hub errors are {"error":"CODE"}; gateways/proxies may nest a
      // structured {"error":{...}} object instead — unwrap it rather than
      // throwing a TypeError that hides the real failure.
      if (rawError is Map) {
        code = (rawError['code'] ?? rawError['name'] ?? rawError['error'] ?? '')
            .toString()
            .trim();
        message = (rawError['message'] ?? '').toString().trim();
      } else {
        code = (rawError as String? ?? data['code'] as String? ?? '').trim();
      }
      if (message.isEmpty) {
        message = (data['message'] as String? ?? '').trim();
      }
    }
    final status = error.response?.statusCode;
    if (message.isEmpty) {
      message = switch (error.type) {
        DioExceptionType.connectionTimeout ||
        DioExceptionType.receiveTimeout ||
        DioExceptionType.sendTimeout =>
          '网络超时，请稍后重试',
        DioExceptionType.connectionError => '无法连接服务器，请检查网络',
        DioExceptionType.badCertificate => '服务器证书不受信任',
        _ => (status != null && status >= 500) ? '服务器异常，请稍后重试' : '请求失败，请稍后重试',
      };
    }
    return HubApiException(
      code.isNotEmpty ? code : 'HTTP_$status',
      message,
      statusCode: status,
    );
  }
  return HubApiException('UNKNOWN', error.toString());
}
