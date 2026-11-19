import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api_client.dart';
import '../../core/providers.dart';

/// Long-running official assistant job (`POST /api/mobile/agent/jobs`).
/// The Hub runs a full server-side agent (LLM + tools) — no Bot needed.
class AgentJob {
  final String jobId;
  final String status;
  final String answer;
  final String message;

  const AgentJob({
    this.jobId = '',
    this.status = '',
    this.answer = '',
    this.message = '',
  });

  bool get isActive {
    final s = status.toLowerCase();
    return s == 'queued' || s == 'running' || s == 'processing';
  }

  bool get isReady => status.toLowerCase() == 'ready';

  bool get isFailed {
    final s = status.toLowerCase();
    return s.contains('fail') || s.contains('error');
  }

  factory AgentJob.fromJson(Map<String, dynamic> json) {
    // The enqueue answer may nest the record under `job`.
    final nested = json['job'];
    if (nested is Map && (json['job_id'] == null || json['status'] == null)) {
      return AgentJob.fromJson(Map<String, dynamic>.from(nested));
    }
    return AgentJob(
      jobId: json['job_id'] as String? ?? '',
      status: json['status'] as String? ?? '',
      answer: json['answer'] as String? ?? '',
      message: json['message'] as String? ?? '',
    );
  }
}

/// Resumable document upload task (`POST /api/mobile/documents/upload`);
/// a finished upload yields `draftId`, bound to a job via `document_id`
/// so Hub-side OCR/parse results reach the agent.
class DocUpload {
  final String taskId;
  final String filename;
  final String status;
  final String draftId;
  final String message;

  const DocUpload({
    this.taskId = '',
    this.filename = '',
    this.status = '',
    this.draftId = '',
    this.message = '',
  });

  bool get isReady => status == 'ready' || draftId.isNotEmpty;

  bool get isFailed {
    final s = status.toLowerCase();
    return s.contains('fail') || s.contains('error');
  }

  factory DocUpload.fromJson(Map<String, dynamic> json) => DocUpload(
    taskId: json['task_id'] as String? ?? '',
    filename: json['filename'] as String? ?? '',
    status: json['status'] as String? ?? 'unknown',
    draftId: json['draft_id'] as String? ?? '',
    message: json['message'] as String? ?? '',
  );
}

class AssistantApi {
  final ApiClient _client;

  AssistantApi(this._client);

  Future<AgentJob> createJob({
    required String query,
    List<Map<String, String>> messages = const [],
    String documentId = '',
  }) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/mobile/agent/jobs',
        data: {
          'query': query,
          if (messages.isNotEmpty) 'messages': messages,
          if (documentId.isNotEmpty) 'document_id': documentId,
        },
        options: _client.opts(receiveTimeout: const Duration(seconds: 30)),
      ),
    );
    return AgentJob.fromJson(data);
  }

  Future<AgentJob> getJob(String jobId) async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/mobile/agent/jobs/$jobId',
        options: _client.opts(),
      ),
    );
    return AgentJob.fromJson(data);
  }

  Future<DocUpload> uploadDocument(String path) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/mobile/documents/upload',
        data: FormData.fromMap({'file': MultipartFile.fromFileSync(path)}),
        options: _client.opts(
          sendTimeout: const Duration(minutes: 2),
          receiveTimeout: const Duration(seconds: 30),
        ),
      ),
    );
    return DocUpload.fromJson(data);
  }

  Future<DocUpload> getUploadTask(String taskId) async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/mobile/documents/upload/${Uri.encodeComponent(taskId)}',
        options: _client.opts(),
      ),
    );
    return DocUpload.fromJson(data);
  }
}

final assistantApiProvider = Provider<AssistantApi?>((ref) {
  final client = ref.watch(apiClientProvider);
  return client == null ? null : AssistantApi(client);
});
