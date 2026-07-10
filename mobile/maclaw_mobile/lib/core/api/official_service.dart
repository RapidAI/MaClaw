import 'package:dio/dio.dart';

const maclawDefaultHubCenterUrl = 'https://hubs.mypapers.top';
const maclawOfficialHubCenterUrls = [
  maclawDefaultHubCenterUrl,
  'https://hubs.maclaw.top',
  'https://hubs2.maclaw.top',
];
const maclawMobileRealtimePath = '/api/mobile/realtime';

// Keep candidate discovery responsive on mobile networks. A stalled official
// endpoint must not prevent trying the next preset HubCenter.
const maclawHubCenterConnectTimeout = Duration(seconds: 8);
const maclawHubCenterSendTimeout = Duration(seconds: 8);
const maclawHubCenterReceiveTimeout = Duration(seconds: 15);

@Deprecated('Use maclawDefaultHubCenterUrl for HubCenter discovery.')
const maclawOfficialServiceUrl = maclawDefaultHubCenterUrl;
@Deprecated('Use maclawMobileRealtimePath.')
const maclawOfficialRealtimePath = maclawMobileRealtimePath;

final _maclawOfficialHubCenterUris =
    maclawOfficialHubCenterUrls.map(Uri.parse).toList(growable: false);

class OfficialHubCenterAttempt {
  final String url;
  final bool available;
  final String message;

  const OfficialHubCenterAttempt({
    required this.url,
    required this.available,
    required this.message,
  });
}

class OfficialHubCenterResolution<T> {
  final String selectedHubCenterUrl;
  final T value;
  final List<OfficialHubCenterAttempt> attempts;

  const OfficialHubCenterResolution({
    required this.selectedHubCenterUrl,
    required this.value,
    required this.attempts,
  });
}

class OfficialHubCenterUnavailableException implements Exception {
  final List<OfficialHubCenterAttempt> attempts;

  const OfficialHubCenterUnavailableException(this.attempts);

  @override
  String toString() {
    return 'No official MaClaw HubCenter is currently reachable.';
  }
}

typedef OfficialHubCenterOperation<T> = Future<T> Function(
  Dio dio,
  String hubCenterUrl,
);

Future<OfficialHubCenterResolution<T>> tryOfficialHubCenters<T>({
  Dio? dio,
  String? preferredHubCenterUrl,
  List<String> hubCenterUrls = maclawOfficialHubCenterUrls,
  required OfficialHubCenterOperation<T> operation,
}) async {
  final candidates = _orderedOfficialHubCenters(
    preferredHubCenterUrl: preferredHubCenterUrl,
    hubCenterUrls: hubCenterUrls,
  );
  final attempts = <OfficialHubCenterAttempt>[];
  for (final candidate in candidates) {
    try {
      final client = officialHubCenterDio(dio, hubCenterUrl: candidate);
      final value = await operation(client, candidate);
      attempts.add(
        OfficialHubCenterAttempt(
          url: candidate,
          available: true,
          message: 'ok',
        ),
      );
      return OfficialHubCenterResolution(
        selectedHubCenterUrl: candidate,
        value: value,
        attempts: attempts,
      );
    } on DioException catch (error) {
      if (!_shouldTryNextHubCenter(error)) rethrow;
      attempts.add(
        OfficialHubCenterAttempt(
          url: candidate,
          available: false,
          message: _hubCenterErrorMessage(error),
        ),
      );
    }
  }
  throw OfficialHubCenterUnavailableException(attempts);
}

Dio officialHubCenterDio(Dio? dio, {String? hubCenterUrl}) {
  final selectedHubCenter = hubCenterUrl ?? maclawDefaultHubCenterUrl;
  if (!isMaclawOfficialHubCenterUrl(selectedHubCenter)) {
    throw UnsupportedError(
      'MaClaw Mobile only supports preset official HubCenter endpoints.',
    );
  }
  if (dio == null) {
    return Dio(
      BaseOptions(
        baseUrl: selectedHubCenter,
        connectTimeout: maclawHubCenterConnectTimeout,
        sendTimeout: maclawHubCenterSendTimeout,
        receiveTimeout: maclawHubCenterReceiveTimeout,
      ),
    );
  }
  final baseUrl = dio.options.baseUrl.trim();
  if (baseUrl.isNotEmpty && !isMaclawOfficialHubCenterUrl(baseUrl)) {
    throw UnsupportedError(
      'MaClaw Mobile only supports preset official HubCenter endpoints.',
    );
  }
  dio.options.baseUrl = selectedHubCenter;
  dio.options.connectTimeout ??= maclawHubCenterConnectTimeout;
  dio.options.sendTimeout ??= maclawHubCenterSendTimeout;
  dio.options.receiveTimeout ??= maclawHubCenterReceiveTimeout;
  return dio;
}

List<String> _orderedOfficialHubCenters({
  required String? preferredHubCenterUrl,
  required List<String> hubCenterUrls,
}) {
  final ordered = <String>[];
  void addCandidate(String? candidate) {
    final value = candidate?.trim() ?? '';
    if (value.isEmpty) return;
    if (!isMaclawOfficialHubCenterUrl(value)) {
      throw UnsupportedError(
        'MaClaw Mobile only supports preset official HubCenter endpoints.',
      );
    }
    if (!ordered.any((item) => sameOrigin(item, value))) ordered.add(value);
  }

  addCandidate(preferredHubCenterUrl);
  for (final candidate in hubCenterUrls) {
    addCandidate(candidate);
  }
  return ordered;
}

bool _shouldTryNextHubCenter(DioException error) {
  final statusCode = error.response?.statusCode;
  if (statusCode == null) return true;
  // A missing route or an upstream timeout means this preset is unavailable;
  // keep discovery moving through the remaining official candidates.
  return statusCode == 404 || statusCode == 408 || statusCode >= 500;
}

String _hubCenterErrorMessage(DioException error) {
  final statusCode = error.response?.statusCode;
  if (statusCode != null) return 'HTTP $statusCode';
  final message = error.message?.trim() ?? '';
  if (message.isNotEmpty) return message;
  return error.type.name;
}

Dio discoveredHubDio(Dio? dio, {required String hubUrl}) {
  final normalizedHubUrl = normalizeDiscoveredHubUrl(hubUrl);
  if (dio == null) {
    return Dio(
      BaseOptions(
        baseUrl: normalizedHubUrl,
        connectTimeout: maclawHubCenterConnectTimeout,
        sendTimeout: maclawHubCenterSendTimeout,
        receiveTimeout: maclawHubCenterReceiveTimeout,
      ),
    );
  }
  final baseUrl = dio.options.baseUrl.trim();
  if (baseUrl.isNotEmpty && !sameOrigin(baseUrl, normalizedHubUrl)) {
    throw UnsupportedError(
      'MaClaw Mobile API clients must use the Hub discovered by HubCenter.',
    );
  }
  dio.options.baseUrl = normalizedHubUrl;
  dio.options.connectTimeout ??= maclawHubCenterConnectTimeout;
  dio.options.sendTimeout ??= maclawHubCenterSendTimeout;
  dio.options.receiveTimeout ??= maclawHubCenterReceiveTimeout;
  return dio;
}

@Deprecated('Use officialHubCenterDio or discoveredHubDio.')
Dio officialServiceDio(Dio? dio) => officialHubCenterDio(dio);

bool isMaclawOfficialHubCenterUrl(String url) {
  final uri = Uri.tryParse(url.trim());
  if (uri == null || !uri.hasScheme || uri.host.isEmpty) return false;
  return _maclawOfficialHubCenterUris.any(
    (candidate) =>
        uri.scheme == candidate.scheme &&
        uri.host == candidate.host &&
        _effectivePort(uri) == _effectivePort(candidate),
  );
}

@Deprecated('Use isMaclawOfficialHubCenterUrl.')
bool isMaclawOfficialServiceUrl(String url) =>
    isMaclawOfficialHubCenterUrl(url);

String normalizeDiscoveredHubUrl(String url) {
  final value = url.trim();
  final uri = Uri.tryParse(value);
  if (uri == null || !uri.hasScheme || uri.host.isEmpty) {
    throw UnsupportedError('HubCenter returned an invalid Hub URL.');
  }
  if (uri.scheme != 'https') {
    throw UnsupportedError('MaClaw Mobile requires HTTPS Hub endpoints.');
  }
  final origin = uri.hasPort
      ? Uri(scheme: uri.scheme, host: uri.host, port: uri.port)
      : Uri(scheme: uri.scheme, host: uri.host);
  return origin.toString().replaceAll(RegExp(r'/$'), '');
}

String maclawHubAbsoluteUrl({
  required String hubUrl,
  required String pathOrUrl,
}) {
  final normalizedHubUrl = normalizeDiscoveredHubUrl(hubUrl);
  final value = pathOrUrl.trim();
  if (value.startsWith('http://') || value.startsWith('https://')) {
    if (!sameOrigin(value, normalizedHubUrl)) {
      throw UnsupportedError(
        'MaClaw Mobile only supports downloads from the discovered Hub.',
      );
    }
    return value;
  }
  if (value.startsWith('/')) return '$normalizedHubUrl$value';
  return '$normalizedHubUrl/$value';
}

@Deprecated('Use maclawHubAbsoluteUrl with a discovered Hub URL.')
String maclawOfficialAbsoluteUrl(String pathOrUrl) {
  return maclawHubAbsoluteUrl(
    hubUrl: maclawDefaultHubCenterUrl,
    pathOrUrl: pathOrUrl,
  );
}

String maclawHubWebSocketUrl({
  required String hubUrl,
  String path = maclawMobileRealtimePath,
}) {
  final normalizedHubUri = Uri.parse(normalizeDiscoveredHubUrl(hubUrl));
  final normalizedPath =
      path.trim().isEmpty ? maclawMobileRealtimePath : path.trim();
  if (_isAbsoluteRealtimeUrl(normalizedPath)) {
    if (!isMaclawHubWebSocketUrl(normalizedPath, hubUrl)) {
      throw UnsupportedError(
        'MaClaw Mobile realtime only supports the discovered Hub.',
      );
    }
    return normalizedPath;
  }
  if (normalizedPath.startsWith('http://') ||
      normalizedPath.startsWith('https://')) {
    throw UnsupportedError(
      'MaClaw Mobile realtime paths must stay on the discovered Hub.',
    );
  }
  final uri = normalizedHubUri.replace(
    scheme: normalizedHubUri.scheme == 'https' ? 'wss' : 'ws',
    path: normalizedPath.startsWith('/') ? normalizedPath : '/$normalizedPath',
  );
  return uri.toString();
}

@Deprecated('Use maclawHubWebSocketUrl with a discovered Hub URL.')
String maclawOfficialWebSocketUrl([String path = maclawMobileRealtimePath]) {
  return maclawHubWebSocketUrl(hubUrl: maclawDefaultHubCenterUrl, path: path);
}

bool sameOrigin(String url, String expectedOrigin) {
  final uri = Uri.tryParse(url.trim());
  if (uri == null || !uri.hasScheme || uri.host.isEmpty) return false;
  final expected = Uri.tryParse(expectedOrigin.trim());
  if (expected == null || !expected.hasScheme || expected.host.isEmpty) {
    return false;
  }
  return uri.scheme == expected.scheme &&
      uri.host == expected.host &&
      _effectivePort(uri) == _effectivePort(expected);
}

bool _isAbsoluteRealtimeUrl(String value) {
  return value.startsWith('ws://') || value.startsWith('wss://');
}

bool isMaclawHubWebSocketUrl(String url, String hubUrl) {
  final uri = Uri.tryParse(url.trim());
  if (uri == null || !uri.hasScheme || uri.host.isEmpty) return false;
  final hubUri = Uri.parse(normalizeDiscoveredHubUrl(hubUrl));
  final expectedScheme = hubUri.scheme == 'https' ? 'wss' : 'ws';
  return uri.scheme == expectedScheme &&
      uri.host == hubUri.host &&
      _effectivePort(uri) == _effectivePort(hubUri);
}

@Deprecated('Use isMaclawHubWebSocketUrl with a discovered Hub URL.')
bool isMaclawOfficialWebSocketUrl(String url) =>
    isMaclawHubWebSocketUrl(url, maclawDefaultHubCenterUrl);

int _effectivePort(Uri uri) {
  if (uri.hasPort) return uri.port;
  return switch (uri.scheme) {
    'http' || 'ws' => 80,
    'https' || 'wss' => 443,
    _ => uri.port,
  };
}
