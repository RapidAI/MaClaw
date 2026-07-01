import 'package:dio/dio.dart';

import '../../features/digital_employees/digital_employee.dart';
import '../storage/secure_vault.dart';
import 'mobile_bootstrap.dart';

class ApiClient {
  final Dio _dio;
  final SecureVault _vault;

  ApiClient({
    required String baseUrl,
    SecureVault? vault,
    Dio? dio,
  })  : _vault = vault ?? const SecureVault(),
        _dio = dio ?? Dio(BaseOptions(baseUrl: baseUrl)) {
    _dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) async {
          final token = await _vault.readToken();
          if (token != null && token.isNotEmpty) {
            options.headers['Authorization'] = 'Bearer $token';
          }
          handler.next(options);
        },
      ),
    );
  }

  Future<MobileBootstrap> bootstrap() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/bootstrap',
    );
    return MobileBootstrap.fromJson(response.data ?? const {});
  }

  Future<SearchAnswer> search(String query) async {
    // Hub search endpoint will be wired to existing WebSearch/LLM services.
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/search',
      data: {'query': query},
    );
    return SearchAnswer.fromJson(response.data ?? const {});
  }

  Future<List<DigitalEmployee>> listDigitalEmployees() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/digital-employees',
    );
    final data = response.data ?? const {};
    return [
      for (final item in (data['employees'] as List? ?? const []))
        DigitalEmployee.fromJson(Map<String, dynamic>.from(item as Map)),
    ];
  }
}

class SearchAnswer {
  final String answer;
  final List<SearchCitation> citations;

  const SearchAnswer({required this.answer, required this.citations});

  factory SearchAnswer.fromJson(Map<String, dynamic> json) {
    return SearchAnswer(
      answer: json['answer'] as String? ?? '',
      citations: [
        for (final item in (json['citations'] as List? ?? const []))
          SearchCitation.fromJson(Map<String, dynamic>.from(item as Map)),
      ],
    );
  }
}

class SearchCitation {
  final String title;
  final String url;

  const SearchCitation({required this.title, required this.url});

  factory SearchCitation.fromJson(Map<String, dynamic> json) {
    return SearchCitation(
      title: json['title'] as String? ?? '',
      url: json['url'] as String? ?? '',
    );
  }
}
