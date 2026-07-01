import 'package:dio/dio.dart';

const maclawOfficialServiceUrl = 'https://hubs.mypapers.top';

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
