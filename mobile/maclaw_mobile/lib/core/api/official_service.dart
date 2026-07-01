import 'package:dio/dio.dart';

const maclawOfficialServiceUrl = 'https://hubs.mypapers.top';
final _maclawOfficialServiceUri = Uri.parse(maclawOfficialServiceUrl);

Dio officialServiceDio(Dio? dio) {
  if (dio == null) {
    return Dio(BaseOptions(baseUrl: maclawOfficialServiceUrl));
  }
  final baseUrl = dio.options.baseUrl.trim();
  if (baseUrl.isNotEmpty && baseUrl != maclawOfficialServiceUrl) {
    throw UnsupportedError(
      'MaClaw Mobile only supports the official MaClaw service.',
    );
  }
  dio.options.baseUrl = maclawOfficialServiceUrl;
  return dio;
}

bool isMaclawOfficialServiceUrl(String url) {
  final uri = Uri.tryParse(url.trim());
  if (uri == null || !uri.hasScheme || uri.host.isEmpty) return false;
  return uri.scheme == _maclawOfficialServiceUri.scheme &&
      uri.host == _maclawOfficialServiceUri.host &&
      uri.hasPort == _maclawOfficialServiceUri.hasPort &&
      uri.port == _maclawOfficialServiceUri.port;
}

String maclawOfficialAbsoluteUrl(String pathOrUrl) {
  final value = pathOrUrl.trim();
  if (value.startsWith('http://') || value.startsWith('https://')) {
    if (!isMaclawOfficialServiceUrl(value)) {
      throw UnsupportedError(
        'MaClaw Mobile only supports downloads from the official MaClaw service.',
      );
    }
    return value;
  }
  if (value.startsWith('/')) return '$maclawOfficialServiceUrl$value';
  return '$maclawOfficialServiceUrl/$value';
}
