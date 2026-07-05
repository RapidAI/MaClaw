import 'dart:typed_data';

import 'package:dio/dio.dart';

import '../../features/digital_employees/digital_employee.dart';
import '../../features/documents/document_draft.dart';
import 'desktop_llm_qr.dart';
import 'official_service.dart';
import '../storage/secure_vault.dart';
import 'mobile_bootstrap.dart';

class ApiClient {
  final Dio _dio;
  final SecureVault _vault;
  final String _hubUrl;

  ApiClient({
    SecureVault? vault,
    Dio? dio,
    String hubUrl = maclawDefaultHubCenterUrl,
  })  : _vault = vault ?? const SecureVault(),
        _hubUrl = normalizeDiscoveredHubUrl(hubUrl),
        _dio = discoveredHubDio(dio, hubUrl: hubUrl) {
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

  Future<LlmServiceStatus> llmServiceStatus([String path = '']) async {
    final statusPath = _hubScopedPath(
      path: path,
      fallbackPath: '/api/llm/service/status',
    );
    final response = await _dio.get<Map<String, dynamic>>(
      statusPath,
    );
    final data = response.data ?? const {};
    return LlmServiceStatus.fromJson(
      Map<String, dynamic>.from(
        data['service_status'] as Map? ?? data['status'] as Map? ?? data,
      ),
    );
  }

  Future<MobileBootstrap> authorizeThirdPartyLlmWithDesktopQr(
    String qrPayload,
  ) async {
    final payload = parseMaclawDesktopLlmQrPayload(qrPayload);
    if (payload.type != maclawMobileLlmAuthorizationType) {
      throw const FormatException(
        'Desktop LLM QR authorization requires an LLM authorization session.',
      );
    }
    if (payload.hubUrl.isNotEmpty && !sameOrigin(payload.hubUrl, _hubUrl)) {
      throw UnsupportedError(
        'Desktop LLM QR authorization must belong to the current MaClaw Hub.',
      );
    }
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/llm/desktop-qr-authorizations',
      data: {'qr_payload': payload.raw},
    );
    final data = response.data ?? const {};
    return MobileBootstrap.fromJson(
      Map<String, dynamic>.from(data['bootstrap'] as Map? ?? data),
    );
  }

  Future<SearchAnswer> search(String query) async {
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

  Future<DocumentDraft> createDocumentDraft({
    required String title,
    required DocumentTemplate template,
    String content = '',
  }) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/documents/drafts',
      data: {
        'title': title,
        'template': documentTemplateWireValue(template),
        'content': content,
      },
    );
    final data = response.data ?? const {};
    return DocumentDraft.fromJson(
      Map<String, dynamic>.from(data['draft'] as Map? ?? const {}),
    );
  }

  Future<DocumentDraft> updateDocumentDraft({
    required String draftId,
    required String title,
    required String markdown,
  }) async {
    final encodedDraftId = Uri.encodeComponent(draftId);
    final response = await _dio.patch<Map<String, dynamic>>(
      '/api/mobile/documents/drafts/$encodedDraftId',
      data: {
        'title': title,
        'markdown': markdown,
      },
    );
    final data = response.data ?? const {};
    return DocumentDraft.fromJson(
      Map<String, dynamic>.from(data['draft'] as Map? ?? const {}),
    );
  }

  Future<DocumentDraft> processDocumentDraft({
    required String draftId,
    required String action,
  }) async {
    final encodedDraftId = Uri.encodeComponent(draftId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/documents/drafts/$encodedDraftId/process',
      data: {'action': action},
    );
    final data = response.data ?? const {};
    return DocumentDraft.fromJson(
      Map<String, dynamic>.from(data['draft'] as Map? ?? const {}),
    );
  }

  Future<DocumentExportJob> exportDocument({
    required String draftId,
    required DocumentExportFormat format,
  }) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/documents/export',
      data: {
        'draft_id': draftId,
        'format': documentExportFormatWireValue(format),
      },
    );
    return DocumentExportJob.fromJson(response.data ?? const {});
  }

  Future<DocumentExportJob> getDocumentExportJob(String jobId) async {
    final encodedJobId = Uri.encodeComponent(jobId);
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/documents/export/$encodedJobId',
    );
    return DocumentExportJob.fromJson(response.data ?? const {});
  }

  Future<Uint8List> downloadDocumentExport(DocumentExportJob job) async {
    if (job.downloadUrl.isEmpty) {
      throw StateError('export job has no download URL');
    }
    final downloadUrl = maclawHubAbsoluteUrl(
      hubUrl: _hubUrl,
      pathOrUrl: job.downloadUrl,
    );
    final response = await _dio.get<List<int>>(
      downloadUrl,
      options: Options(responseType: ResponseType.bytes),
    );
    return Uint8List.fromList(response.data ?? const []);
  }

  String absoluteUrl(String path) {
    return maclawHubAbsoluteUrl(hubUrl: _hubUrl, pathOrUrl: path);
  }

  String _hubScopedPath({
    required String path,
    required String fallbackPath,
  }) {
    final value = path.trim();
    if (value.isEmpty) return fallbackPath;
    if (value.startsWith('http://') || value.startsWith('https://')) {
      return maclawHubAbsoluteUrl(hubUrl: _hubUrl, pathOrUrl: value);
    }
    return value;
  }

  Future<MobileDocumentUploadTask> uploadDocument(String path) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/documents/upload',
      data: FormData.fromMap({
        'file': await MultipartFile.fromFile(path),
      }),
    );
    return MobileDocumentUploadTask.fromJson(response.data ?? const {});
  }

  Future<MobileDocumentUploadTask> getDocumentUploadTask(String taskId) async {
    final encodedTaskId = Uri.encodeComponent(taskId);
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/documents/upload/$encodedTaskId',
    );
    return MobileDocumentUploadTask.fromJson(response.data ?? const {});
  }

  Future<MobileSSHAnalysis> analyzeSSHOutput(String output) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/analyze',
      data: {'output': output},
    );
    return MobileSSHAnalysis.fromJson(response.data ?? const {});
  }

  Future<MobileDigitalEmployeeTask> createDigitalEmployeeTask({
    required String employeeId,
    required String prompt,
    String taskType = 'general',
    Map<String, String> context = const {},
  }) async {
    final encodedEmployeeId = Uri.encodeComponent(employeeId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/digital-employees/$encodedEmployeeId/tasks',
      data: {
        'prompt': prompt,
        'task_type': taskType,
        if (context.isNotEmpty) 'context': context,
      },
    );
    return MobileDigitalEmployeeTask.fromJson(response.data ?? const {});
  }

  Future<MobileDigitalEmployeeTask> getDigitalEmployeeTask(
    String taskId,
  ) async {
    final encodedTaskId = Uri.encodeComponent(taskId);
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/digital-employees/tasks/$encodedTaskId',
    );
    return MobileDigitalEmployeeTask.fromJson(response.data ?? const {});
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
  final String snippet;

  const SearchCitation({
    required this.title,
    required this.url,
    required this.snippet,
  });

  factory SearchCitation.fromJson(Map<String, dynamic> json) {
    return SearchCitation(
      title: json['title'] as String? ?? '',
      url: json['url'] as String? ?? '',
      snippet: json['snippet'] as String? ?? '',
    );
  }
}

class MobileDocumentUploadTask {
  final String taskId;
  final String filename;
  final String status;
  final String draftId;
  final String message;
  final DocumentDraft? draft;

  const MobileDocumentUploadTask({
    required this.taskId,
    required this.filename,
    required this.status,
    this.draftId = '',
    this.message = '',
    this.draft,
  });

  factory MobileDocumentUploadTask.fromJson(Map<String, dynamic> json) {
    final draftJson = json['draft'];
    return MobileDocumentUploadTask(
      taskId: json['task_id'] as String? ?? '',
      filename: json['filename'] as String? ?? '',
      status: json['status'] as String? ?? 'unknown',
      draftId: json['draft_id'] as String? ?? '',
      message: json['message'] as String? ?? '',
      draft: draftJson is Map
          ? DocumentDraft.fromJson(Map<String, dynamic>.from(draftJson))
          : null,
    );
  }
}

class MobileSSHAnalysis {
  final String summary;
  final String recommendation;
  final String commandDraft;

  const MobileSSHAnalysis({
    required this.summary,
    required this.recommendation,
    required this.commandDraft,
  });

  factory MobileSSHAnalysis.fromJson(Map<String, dynamic> json) {
    return MobileSSHAnalysis(
      summary: json['summary'] as String? ?? '',
      recommendation: json['recommendation'] as String? ?? '',
      commandDraft: json['command_draft'] as String? ?? '',
    );
  }
}

class MobileDigitalEmployeeTask {
  final String taskId;
  final String employeeId;
  final String prompt;
  final String taskType;
  final Map<String, String> context;
  final String status;
  final String result;
  final String message;
  final String claimedBy;

  const MobileDigitalEmployeeTask({
    required this.taskId,
    required this.employeeId,
    required this.prompt,
    this.taskType = 'general',
    this.context = const {},
    required this.status,
    required this.result,
    this.message = '',
    required this.claimedBy,
  });

  factory MobileDigitalEmployeeTask.fromJson(Map<String, dynamic> json) {
    return MobileDigitalEmployeeTask(
      taskId: json['task_id'] as String? ?? '',
      employeeId: json['employee_id'] as String? ?? '',
      prompt: json['prompt'] as String? ?? '',
      taskType: json['task_type'] as String? ?? 'general',
      context: {
        for (final entry
            in Map<String, dynamic>.from(json['context'] as Map? ?? const {})
                .entries)
          entry.key: entry.value.toString(),
      },
      status: json['status'] as String? ?? 'unknown',
      result: json['result'] as String? ?? '',
      message: json['message'] as String? ?? json['error'] as String? ?? '',
      claimedBy: json['claimed_by'] as String? ?? '',
    );
  }
}
