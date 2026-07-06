import 'dart:typed_data';

import 'package:dio/dio.dart';

import '../../features/digital_employees/digital_employee.dart';
import '../../features/documents/document_draft.dart';
import '../../features/servers/server_profile.dart';
import '../security/mobile_redaction.dart';
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

  Future<MobileSSHAnalysis> analyzeSSHOutput(
    String output, {
    String? backendSessionId,
  }) async {
    final sessionId = backendSessionId?.trim() ?? '';
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/analyze',
      data: {
        'output': output,
        if (sessionId.isNotEmpty) 'backend_session_id': sessionId,
      },
    );
    return MobileSSHAnalysis.fromJson(response.data ?? const {});
  }

  Future<List<ServerProfile>> listServerProfiles() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/server-profiles',
    );
    final data = response.data ?? const {};
    return [
      for (final item in (data['profiles'] as List? ?? const []))
        ServerProfile.fromJson(Map<String, dynamic>.from(item as Map)),
    ]
        .where((profile) => profile.isValid && profile.id.trim().isNotEmpty)
        .toList();
  }

  Future<MobileBackendSSHSession> createBackendSSHSession({
    required String serverProfileId,
  }) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions',
      data: {'server_profile_id': serverProfileId},
    );
    return MobileBackendSSHSession.fromJson(
      _sessionPayload(response.data ?? const {}),
    );
  }

  Future<List<MobileBackendSSHSession>> listBackendSSHSessions() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions',
    );
    final data = response.data ?? const {};
    return [
      for (final item in (data['sessions'] as List? ?? const []))
        MobileBackendSSHSession.fromJson(
          Map<String, dynamic>.from(item as Map),
        ),
    ];
  }

  Future<MobileBackendSSHSession> attachBackendSSHSession(
    String sessionId,
  ) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/attach',
    );
    return MobileBackendSSHSession.fromJson(
      _sessionPayload(response.data ?? const {}),
    );
  }

  Future<MobileBackendSSHSession> reconnectBackendSSHSession(
    String sessionId,
  ) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/reconnect',
    );
    return MobileBackendSSHSession.fromJson(
      _sessionPayload(response.data ?? const {}),
    );
  }

  Future<MobileBackendSSHSession> interruptBackendSSHSession(
    String sessionId,
  ) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/interrupt',
    );
    return MobileBackendSSHSession.fromJson(
      _sessionPayload(response.data ?? const {}),
    );
  }

  Future<MobileBackendSSHSessionInputResult> sendBackendSSHSessionInput({
    required String sessionId,
    required String input,
  }) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/input',
      data: {'input': input},
    );
    return MobileBackendSSHSessionInputResult.fromJson(
      response.data ?? const {},
    );
  }

  Future<void> closeBackendSSHSession(String sessionId) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    await _dio.delete<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId',
    );
  }

  Future<MobileBackendSSHTask> startBackendSSHBackgroundTask({
    required String sessionId,
    required String command,
    int? tailLines,
  }) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/tasks',
      data: {
        'action': 'exec_background',
        'command': command,
        if (tailLines != null) 'tail_lines': tailLines,
      },
    );
    return MobileBackendSSHTask.fromJson(
      _taskPayload(response.data ?? const {}),
    );
  }

  Future<List<MobileBackendSSHTask>> listBackendSSHBackgroundTasks(
    String sessionId,
  ) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/tasks',
    );
    final data = response.data ?? const {};
    return [
      for (final item in (data['tasks'] as List? ?? const []))
        MobileBackendSSHTask.fromJson(Map<String, dynamic>.from(item as Map)),
    ];
  }

  Future<MobileBackendSSHTask> getBackendSSHBackgroundTask({
    required String sessionId,
    required String taskId,
  }) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final encodedTaskId = Uri.encodeComponent(taskId);
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/tasks/$encodedTaskId',
    );
    return MobileBackendSSHTask.fromJson(
      _taskPayload(response.data ?? const {}),
    );
  }

  Future<MobileBackendSSHTask> waitBackendSSHBackgroundTask({
    required String sessionId,
    required String taskId,
    int? timeoutSeconds,
    int? tailLines,
  }) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final encodedTaskId = Uri.encodeComponent(taskId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/tasks/$encodedTaskId/wait',
      data: {
        if (timeoutSeconds != null) 'timeout': timeoutSeconds,
        if (tailLines != null) 'tail_lines': tailLines,
      },
    );
    return MobileBackendSSHTask.fromJson(
      _taskPayload(response.data ?? const {}),
    );
  }

  Future<MobileBackendSSHTask> killBackendSSHBackgroundTask({
    required String sessionId,
    required String taskId,
  }) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final encodedTaskId = Uri.encodeComponent(taskId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/tasks/$encodedTaskId/kill',
    );
    return MobileBackendSSHTask.fromJson(
      _taskPayload(response.data ?? const {}),
    );
  }

  Future<MobileBackendSSHFileOperation> requestBackendSSHFileOperation({
    required String sessionId,
    required String action,
    String localPath = '',
    String remotePath = '',
  }) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/files',
      data: {
        'action': action,
        if (localPath.trim().isNotEmpty) 'local_path': localPath,
        if (remotePath.trim().isNotEmpty) 'remote_path': remotePath,
      },
    );
    return MobileBackendSSHFileOperation.fromJson(
      _fileOperationPayload(response.data ?? const {}),
    );
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

Map<String, dynamic> _sessionPayload(Map<String, dynamic> data) {
  return Map<String, dynamic>.from(data['session'] as Map? ?? data);
}

Map<String, dynamic> _taskPayload(Map<String, dynamic> data) {
  return Map<String, dynamic>.from(data['task'] as Map? ?? data);
}

Map<String, dynamic> _fileOperationPayload(Map<String, dynamic> data) {
  return Map<String, dynamic>.from(data['operation'] as Map? ?? data);
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
  final String backendSessionId;

  const MobileSSHAnalysis({
    required this.summary,
    required this.recommendation,
    required this.commandDraft,
    this.backendSessionId = '',
  });

  factory MobileSSHAnalysis.fromJson(Map<String, dynamic> json) {
    return MobileSSHAnalysis(
      summary: json['summary'] as String? ?? '',
      recommendation: json['recommendation'] as String? ?? '',
      commandDraft: json['command_draft'] as String? ?? '',
      backendSessionId: redactMobileSensitiveText(
        json['backend_session_id'] as String? ?? '',
      ),
    );
  }
}

class MobileBackendSSHSession {
  final String sessionId;
  final String serverProfileId;
  final String backendSessionId;
  final String status;
  final String state;
  final String message;
  final String recentOutput;
  final String outputChunk;
  final int outputSeq;
  final int pendingInputCount;
  final String claimedBy;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final DateTime? lastActivityAt;

  const MobileBackendSSHSession({
    required this.sessionId,
    required this.serverProfileId,
    this.backendSessionId = '',
    required this.status,
    this.state = '',
    this.message = '',
    this.recentOutput = '',
    this.outputChunk = '',
    this.outputSeq = 0,
    this.pendingInputCount = 0,
    this.claimedBy = '',
    this.createdAt,
    this.updatedAt,
    this.lastActivityAt,
  });

  bool get connected =>
      status == 'connected' ||
      status == 'running' ||
      status == 'attached' ||
      state == 'connected' ||
      state == 'running' ||
      state == 'attached';

  factory MobileBackendSSHSession.fromJson(Map<String, dynamic> json) {
    final updatedAt = DateTime.tryParse(json['updated_at'] as String? ?? '');
    return MobileBackendSSHSession(
      sessionId: json['session_id'] as String? ??
          json['id'] as String? ??
          json['ssh_session_id'] as String? ??
          '',
      serverProfileId: json['server_profile_id'] as String? ??
          json['profile_id'] as String? ??
          '',
      backendSessionId: json['backend_session_id'] as String? ?? '',
      status:
          json['status'] as String? ?? json['state'] as String? ?? 'unknown',
      state: json['state'] as String? ?? '',
      message: json['message'] as String? ?? json['error'] as String? ?? '',
      recentOutput: json['recent_output'] as String? ??
          json['output'] as String? ??
          json['output_chunk'] as String? ??
          '',
      outputChunk: json['output_chunk'] as String? ?? '',
      outputSeq: json['output_seq'] is int
          ? json['output_seq'] as int
          : int.tryParse('${json['output_seq'] ?? ''}') ?? 0,
      pendingInputCount: json['pending_input_count'] is int
          ? json['pending_input_count'] as int
          : int.tryParse('${json['pending_input_count'] ?? ''}') ?? 0,
      claimedBy: json['claimed_by'] as String? ?? '',
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? ''),
      updatedAt: updatedAt,
      lastActivityAt: DateTime.tryParse(
        json['last_activity_at'] as String? ??
            json['updated_at'] as String? ??
            '',
      ),
    );
  }
}

class MobileBackendSSHSessionInputResult {
  final String sessionId;
  final String output;
  final String status;
  final String message;

  const MobileBackendSSHSessionInputResult({
    required this.sessionId,
    this.output = '',
    this.status = '',
    this.message = '',
  });

  factory MobileBackendSSHSessionInputResult.fromJson(
    Map<String, dynamic> json,
  ) {
    final session =
        Map<String, dynamic>.from(json['session'] as Map? ?? const {});
    return MobileBackendSSHSessionInputResult(
      sessionId: json['session_id'] as String? ??
          session['session_id'] as String? ??
          session['id'] as String? ??
          '',
      output: json['output'] as String? ??
          json['output_chunk'] as String? ??
          json['recent_output'] as String? ??
          session['recent_output'] as String? ??
          '',
      status: json['status'] as String? ?? session['status'] as String? ?? '',
      message: json['message'] as String? ?? json['error'] as String? ?? '',
    );
  }
}

class MobileBackendSSHTask {
  final String taskId;
  final String sessionId;
  final String backendSessionId;
  final String command;
  final String status;
  final String message;
  final String logTail;
  final int? exitCode;
  final String claimedBy;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  const MobileBackendSSHTask({
    required this.taskId,
    this.sessionId = '',
    this.backendSessionId = '',
    this.command = '',
    this.status = 'unknown',
    this.message = '',
    this.logTail = '',
    this.exitCode,
    this.claimedBy = '',
    this.createdAt,
    this.updatedAt,
  });

  bool get running => status == 'running' || status == 'queued';

  factory MobileBackendSSHTask.fromJson(Map<String, dynamic> json) {
    final exitCodeValue = json['exit_code'];
    return MobileBackendSSHTask(
      taskId: json['task_id'] as String? ?? json['id'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      backendSessionId: json['backend_session_id'] as String? ?? '',
      command: json['command'] as String? ?? '',
      status: json['status'] as String? ?? 'unknown',
      message: json['message'] as String? ?? json['error'] as String? ?? '',
      logTail: json['log_tail'] as String? ??
          json['tail'] as String? ??
          json['output'] as String? ??
          '',
      exitCode: exitCodeValue is int
          ? exitCodeValue
          : int.tryParse('${exitCodeValue ?? ''}'),
      claimedBy: json['claimed_by'] as String? ?? '',
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? ''),
      updatedAt: DateTime.tryParse(json['updated_at'] as String? ?? ''),
    );
  }
}

class MobileBackendSSHFileOperation {
  final String operationId;
  final String sessionId;
  final String backendSessionId;
  final String action;
  final String localPath;
  final String remotePath;
  final String status;
  final String message;
  final int bytesTransferred;
  final String downloadUrl;
  final String claimedBy;

  const MobileBackendSSHFileOperation({
    required this.operationId,
    this.sessionId = '',
    this.backendSessionId = '',
    this.action = '',
    this.localPath = '',
    this.remotePath = '',
    this.status = 'unknown',
    this.message = '',
    this.bytesTransferred = 0,
    this.downloadUrl = '',
    this.claimedBy = '',
  });

  factory MobileBackendSSHFileOperation.fromJson(Map<String, dynamic> json) {
    final bytesValue = json['bytes_transferred'];
    return MobileBackendSSHFileOperation(
      operationId:
          json['operation_id'] as String? ?? json['id'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      backendSessionId: json['backend_session_id'] as String? ?? '',
      action: json['action'] as String? ?? '',
      localPath: json['local_path'] as String? ?? '',
      remotePath: json['remote_path'] as String? ?? '',
      status: json['status'] as String? ?? 'unknown',
      message: json['message'] as String? ?? json['error'] as String? ?? '',
      bytesTransferred: bytesValue is int
          ? bytesValue
          : int.tryParse('${bytesValue ?? ''}') ?? 0,
      downloadUrl: json['download_url'] as String? ?? '',
      claimedBy: json['claimed_by'] as String? ?? '',
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
