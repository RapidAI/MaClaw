import 'dart:async';
import 'dart:convert';
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
          try {
            final token = await _vault.readToken();
            if (token != null && token.isNotEmpty) {
              options.headers['Authorization'] = 'Bearer $token';
            }
          } on Object {
            // Tests / hosts without secure-storage plugins should still call Hub.
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

  /// Redeem a service/authorization card code for official LLM credits.
  Future<LlmServiceCardRedeemResult> redeemLLMServiceCard(String code) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/llm/service/redeem',
      data: {'code': code.trim()},
    );
    return LlmServiceCardRedeemResult.fromJson(response.data ?? const {});
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

  Future<MobileBootstrap> revokeThirdPartyLlmAuthorization() async {
    final response = await _dio.delete<Map<String, dynamic>>(
      '/api/mobile/llm/desktop-qr-authorizations',
    );
    final data = response.data ?? const {};
    return MobileBootstrap.fromJson(
      Map<String, dynamic>.from(data['bootstrap'] as Map? ?? data),
    );
  }

  Future<MobileAgentMcpConfig> getAgentMcpConfig() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/agent/mcp',
    );
    return MobileAgentMcpConfig.fromJson(response.data ?? const {});
  }

  Future<MobileAgentMcpConfig> putAgentMcpConfig(
    List<MobileMcpServer> servers,
  ) async {
    final response = await _dio.put<Map<String, dynamic>>(
      '/api/mobile/agent/mcp',
      data: {
        'mcp_servers': [for (final s in servers) s.toJson()],
      },
    );
    return MobileAgentMcpConfig.fromJson(response.data ?? const {});
  }

  Future<MobileAgentKnowledgeStatus> getAgentKnowledgeStatus() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/agent/knowledge/status',
    );
    return MobileAgentKnowledgeStatus.fromJson(response.data ?? const {});
  }

  /// Index a free-form note into the Hub knowledge store for the official agent.
  Future<MobileAgentKnowledgeIngestResult> ingestAgentKnowledge({
    required String text,
    String title = '',
  }) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/agent/knowledge/ingest',
      data: {
        'text': text,
        if (title.trim().isNotEmpty) 'title': title.trim(),
      },
    );
    return MobileAgentKnowledgeIngestResult.fromJson(
      response.data ?? const {},
    );
  }

  Future<MobileAgentMcpHealth> probeAgentMcpHealth() async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/agent/mcp/health',
    );
    return MobileAgentMcpHealth.fromJson(response.data ?? const {});
  }

  Future<MobileAgentSkillsList> listAgentSkills() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/agent/skills',
    );
    return MobileAgentSkillsList.fromJson(response.data ?? const {});
  }

  Future<MobileAgentSkillsList> reseedAgentSkills() async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/agent/skills/reseed',
    );
    return MobileAgentSkillsList.fromJson(response.data ?? const {});
  }

  Future<MobileJobsList> listJobs() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/jobs',
    );
    return MobileJobsList.fromJson(response.data ?? const {});
  }

  Future<MobileDocumentQuota> getDocumentQuota() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/documents/quota',
    );
    return MobileDocumentQuota.fromJson(response.data ?? const {});
  }

  /// Effective plan caps (same matrix as bootstrap entitlements/limits).
  Future<MobileEntitlementsCaps> getEntitlementsCaps() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/entitlements/caps',
    );
    return MobileEntitlementsCaps.fromJson(response.data ?? const {});
  }

  /// Register this device for remote push / pending association.
  Future<void> registerPushDevice({
    required String platform,
    required String token,
    String deviceId = '',
    String path = '/api/mobile/push/devices',
  }) async {
    final p = path.trim().isEmpty ? '/api/mobile/push/devices' : path.trim();
    await _dio.post<Map<String, dynamic>>(
      p,
      data: {
        'platform': platform.trim(),
        'token': token.trim(),
        if (deviceId.trim().isNotEmpty) 'device_id': deviceId.trim(),
      },
    );
  }

  Future<void> unregisterPushDevice({
    required String token,
    String path = '/api/mobile/push/devices',
  }) async {
    final p = path.trim().isEmpty ? '/api/mobile/push/devices' : path.trim();
    await _dio.delete<Map<String, dynamic>>(
      p,
      data: {'token': token.trim()},
    );
  }

  /// Offline completions queued while realtime was down.
  Future<MobilePushPendingList> listPushPending({
    String path = '/api/mobile/push/pending',
  }) async {
    final p = path.trim().isEmpty ? '/api/mobile/push/pending' : path.trim();
    final response = await _dio.get<Map<String, dynamic>>(p);
    return MobilePushPendingList.fromJson(response.data ?? const {});
  }

  Future<int> ackPushPending({
    required List<String> ids,
    String path = '/api/mobile/push/pending/ack',
  }) async {
    final p =
        path.trim().isEmpty ? '/api/mobile/push/pending/ack' : path.trim();
    final response = await _dio.post<Map<String, dynamic>>(
      p,
      data: {'ids': ids.where((e) => e.trim().isNotEmpty).toList()},
    );
    final raw = response.data?['acked'];
    if (raw is int) return raw;
    if (raw is num) return raw.toInt();
    return int.tryParse(raw?.toString() ?? '') ?? 0;
  }

  /// Ops runtime override for plan caps (requires Hub admin token header).
  ///
  /// Set [clear] to drop all process overrides. Non-zero fields apply partially.
  Future<MobileCapsPutResult> putEntitlementsCaps({
    required String adminToken,
    int? docFreeMib,
    int? docPaidMib,
    int? exportFree,
    int? exportPaid,
    int? hubFileDownloadMib,
    bool clear = false,
  }) async {
    final token = adminToken.trim();
    if (token.isEmpty) {
      throw ArgumentError('adminToken is required');
    }
    final body = <String, dynamic>{
      if (clear) 'clear': true,
      if (!clear && docFreeMib != null && docFreeMib > 0)
        'doc_free_mib': docFreeMib,
      if (!clear && docPaidMib != null && docPaidMib > 0)
        'doc_paid_mib': docPaidMib,
      if (!clear && exportFree != null && exportFree > 0)
        'export_free': exportFree,
      if (!clear && exportPaid != null && exportPaid > 0)
        'export_paid': exportPaid,
      if (!clear && hubFileDownloadMib != null && hubFileDownloadMib > 0)
        'hub_file_download_mib': hubFileDownloadMib,
    };
    final response = await _dio.put<Map<String, dynamic>>(
      '/api/mobile/entitlements/caps',
      data: body,
      options: Options(
        headers: {'X-Maclaw-Caps-Admin-Token': token},
      ),
    );
    return MobileCapsPutResult.fromJson(response.data ?? const {});
  }

  Future<MobileCardStoreCatalog> listCardStoreProducts({String tenantId = ''}) async {
    final query = <String, dynamic>{};
    if (tenantId.trim().isNotEmpty) {
      query['tenant_id'] = tenantId.trim();
    }
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/card-store/products',
      queryParameters: query.isEmpty ? null : query,
    );
    return MobileCardStoreCatalog.fromJson(response.data ?? const {});
  }

  Future<MobileCardStoreOrder> createCardStoreOrder({
    required String productId,
    required String account,
    String tenantId = '',
    String paymentMethod = '',
    String payChannel = '',
  }) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/card-store/orders',
      data: {
        'product_id': productId,
        'account': account,
        if (tenantId.trim().isNotEmpty) 'tenant_id': tenantId.trim(),
        if (paymentMethod.trim().isNotEmpty) 'payment_method': paymentMethod.trim(),
        if (payChannel.trim().isNotEmpty) 'pay_channel': payChannel.trim(),
      },
    );
    return MobileCardStoreOrder.fromJson(response.data ?? const {});
  }

  /// Timeouts for long-running official assistant (agent tools / SSH).
  Options _assistantRequestOptions({
    ResponseType? responseType,
    Map<String, dynamic>? headers,
    bool Function(int?)? validateStatus,
  }) {
    return Options(
      responseType: responseType,
      headers: headers,
      validateStatus: validateStatus,
      connectTimeout: mobileAssistantConnectTimeout,
      sendTimeout: mobileAssistantSendTimeout,
      receiveTimeout: mobileAssistantReceiveTimeout,
    );
  }

  /// Enqueue a long-running official assistant job (后台执行).
  Future<MobileAgentJob> createAgentJob({
    required String query,
    List<String> context = const [],
    List<Map<String, String>> messages = const [],
    String documentId = '',
  }) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/agent/jobs',
      data: _searchRequestBody(
        query: query,
        context: context,
        messages: messages,
        documentId: documentId,
        stream: false,
      )..remove('stream'),
      options: _assistantRequestOptions(),
    );
    return MobileAgentJob.fromJson(response.data ?? const {});
  }

  Future<MobileAgentJob> getAgentJob(String jobId) async {
    final id = jobId.trim();
    if (id.isEmpty) {
      throw ArgumentError('jobId is required');
    }
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/agent/jobs/$id',
      options: _assistantRequestOptions(),
    );
    return MobileAgentJob.fromJson(response.data ?? const {});
  }

  Future<SearchAnswer> search(String query) async {
    return searchWithContext(query);
  }

  Future<SearchAnswer> searchWithContext(
    String query, {
    List<String> context = const [],
    List<Map<String, String>> messages = const [],
    String documentId = '',
    bool async = false,
  }) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/search',
      data: _searchRequestBody(
        query: query,
        context: context,
        messages: messages,
        documentId: documentId,
        stream: false,
        async: async,
      ),
      options: _assistantRequestOptions(),
    );
    return SearchAnswer.fromJson(response.data ?? const {});
  }

  /// Progressive search: emits partial [SearchAnswer] snapshots while Hub
  /// streams SSE (`meta` → `delta*` → `done`). Falls back to non-stream JSON
  /// if the server does not speak SSE.
  Stream<SearchAnswer> searchWithContextStream(
    String query, {
    List<String> context = const [],
    List<Map<String, String>> messages = const [],
    String documentId = '',
  }) async* {
    Response<ResponseBody> response;
    try {
      response = await _dio.post<ResponseBody>(
        '/api/mobile/search',
        data: _searchRequestBody(
          query: query,
          context: context,
          messages: messages,
          documentId: documentId,
          stream: true,
        ),
        options: _assistantRequestOptions(
          responseType: ResponseType.stream,
          headers: const {'Accept': 'text/event-stream'},
          // Inspect status ourselves so SSE error bodies can still be read.
          validateStatus: (_) => true,
        ),
      );
    } on DioException catch (error) {
      // Transport failures only: fall back to non-stream JSON once.
      // Auth/validation errors are rethrown so the UI can show a real failure.
      final code = error.response?.statusCode;
      if (code != null && code >= 400 && code < 500) {
        throw StateError(
          'mobile AI assistant request failed (HTTP $code)',
        );
      }
      // Do not fall back to non-stream on receive timeout — the non-stream
      // path is even longer and would surface the same timeout again.
      if (error.type == DioExceptionType.receiveTimeout ||
          error.type == DioExceptionType.connectionTimeout ||
          error.type == DioExceptionType.sendTimeout) {
        rethrow;
      }
      yield await searchWithContext(
        query,
        context: context,
        messages: messages,
        documentId: documentId,
      );
      return;
    }

    final status = response.statusCode ?? 0;
    if (status < 200 || status >= 300) {
      throw StateError('mobile AI assistant request failed (HTTP $status)');
    }

    final contentType = response.headers.value('content-type') ?? '';
    if (!contentType.contains('text/event-stream')) {
      // Older hubs may ignore stream=true and return JSON.
      final bytes = await _collectStreamBytes(response.data);
      if (bytes.isEmpty) {
        throw StateError('mobile AI assistant request failed (empty response)');
      }
      final Object decoded;
      try {
        decoded = jsonDecode(utf8.decode(bytes));
      } on FormatException {
        throw StateError(
          'mobile AI assistant request failed (invalid response)',
        );
      }
      if (decoded is Map<String, dynamic>) {
        yield SearchAnswer.fromJson(decoded);
      } else if (decoded is Map) {
        yield SearchAnswer.fromJson(Map<String, dynamic>.from(decoded));
      } else {
        throw StateError(
          'mobile AI assistant request failed (unexpected response)',
        );
      }
      return;
    }

    final body = response.data;
    if (body == null) {
      yield await searchWithContext(
        query,
        context: context,
        messages: messages,
      );
      return;
    }

    // Mutable streaming accumulator (not const — updated as deltas arrive).
    var partial = SearchAnswer(
      answer: '',
      citations: const [],
      streaming: true,
    );
    final buffer = StringBuffer();
    final toolEvents = <AssistantToolEvent>[];
    void trimToolEvents() {
      if (toolEvents.length <= mobileAssistantMaxRetainedToolEvents) return;
      final kept = retainRecentAssistantToolEvents(toolEvents);
      toolEvents
        ..clear()
        ..addAll(kept);
    }

    await for (final event in _parseSseEvents(body.stream)) {
      switch (event.event) {
        case 'meta':
          final data = event.jsonMap;
          partial = SearchAnswer(
            answer: partial.answer,
            citations: SearchAnswer.citationsFromJson(data),
            llmMode: (data['llm_mode'] as String? ?? '').trim(),
            llmRequestId: (data['llm_request_id'] as String? ?? '').trim(),
            llmUsageRecordId:
                (data['llm_usage_record_id'] as String? ?? '').trim(),
            streaming: true,
            agent: data['agent'] == true,
            toolEvents: List<AssistantToolEvent>.from(toolEvents),
          );
          yield partial;
        case 'tool_call':
          final data = event.jsonMap;
          toolEvents.add(
            AssistantToolEvent(
              kind: 'call',
              id: (data['id'] as String? ?? '').trim(),
              name: (data['name'] as String? ?? 'tool').trim(),
              detail: (data['arguments'] as String? ?? '').toString(),
            ),
          );
          trimToolEvents();
          partial = partial.copyWith(
            toolEvents: List<AssistantToolEvent>.from(toolEvents),
            agent: true,
            streaming: true,
          );
          yield partial;
        case 'tool_result':
          final data = event.jsonMap;
          toolEvents.add(
            AssistantToolEvent(
              kind: 'result',
              id: (data['id'] as String? ?? '').trim(),
              name: (data['name'] as String? ?? 'tool').trim(),
              detail: (data['result'] as String? ?? '').toString(),
            ),
          );
          trimToolEvents();
          partial = partial.copyWith(
            toolEvents: List<AssistantToolEvent>.from(toolEvents),
            agent: true,
            streaming: true,
          );
          yield partial;
        case 'delta':
          final data = event.jsonMap;
          final chunk = (data['text'] as String? ?? '').toString();
          if (chunk.isEmpty) continue;
          buffer.write(chunk);
          partial = SearchAnswer(
            answer: buffer.toString(),
            citations: partial.citations,
            llmMode: partial.llmMode,
            llmRequestId: partial.llmRequestId,
            llmUsageRecordId: partial.llmUsageRecordId,
            streaming: true,
            agent: partial.agent || toolEvents.isNotEmpty,
            toolEvents: List<AssistantToolEvent>.from(toolEvents),
          );
          yield partial;
        case 'done':
          final data = event.jsonMap;
          final done = SearchAnswer.fromJson(data).copyWith(
            streaming: false,
            toolEvents: toolEvents.isNotEmpty
                ? List<AssistantToolEvent>.from(toolEvents)
                : null,
            agent: data['agent'] == true || toolEvents.isNotEmpty
                ? true
                : null,
          );
          // Prefer assembled deltas if the done payload omitted answer text.
          if (done.answer.trim().isEmpty && buffer.isNotEmpty) {
            yield done.copyWith(answer: buffer.toString());
          } else {
            yield done;
          }
          return;
        case 'error':
          final data = event.jsonMap;
          final message =
              (data['message'] as String? ?? 'mobile AI assistant request failed')
                  .toString();
          throw StateError(message);
      }
    }
    if (buffer.isNotEmpty) {
      yield partial.copyWith(streaming: false);
      return;
    }
    yield await searchWithContext(
      query,
      context: context,
      messages: messages,
    );
  }

  Map<String, dynamic> _searchRequestBody({
    required String query,
    List<String> context = const [],
    List<Map<String, String>> messages = const [],
    String documentId = '',
    required bool stream,
    bool async = false,
  }) {
    final id = documentId.trim();
    return {
      'query': query,
      // New hubs prefer messages[]; older hubs ignore it and still read context.
      if (messages.isNotEmpty)
        'messages': [
          for (final item in messages)
            {
              'role': item['role'] ?? 'user',
              'content': item['content'] ?? '',
            },
        ],
      if (context.isNotEmpty) 'context': context,
      if (id.isNotEmpty) 'document_id': id,
      if (async) 'async': true,
      'stream': stream,
    };
  }

  Future<List<int>> _collectStreamBytes(ResponseBody? body) async {
    if (body == null) return const [];
    final builder = BytesBuilder(copy: false);
    await for (final piece in body.stream) {
      builder.add(piece);
    }
    return builder.takeBytes();
  }

  static const int _sseCarryMaxChars = 1 << 20; // 1 MiB incomplete-line budget

  Stream<_SseEvent> _parseSseEvents(Stream<List<int>> byteStream) async* {
    var carry = '';
    String eventName = 'message';
    final dataLines = <String>[];

    Future<_SseEvent?> flush() async {
      if (dataLines.isEmpty) {
        eventName = 'message';
        return null;
      }
      final data = dataLines.join('\n');
      dataLines.clear();
      final name = eventName;
      eventName = 'message';
      return _SseEvent(event: name, data: data);
    }

    await for (final bytes in byteStream) {
      carry += utf8.decode(bytes, allowMalformed: true);
      // Drop oldest half if a pathological stream never sends newlines.
      if (carry.length > _sseCarryMaxChars) {
        carry = carry.substring(carry.length - (_sseCarryMaxChars ~/ 2));
      }
      while (true) {
        final nl = carry.indexOf('\n');
        if (nl < 0) break;
        var line = carry.substring(0, nl);
        carry = carry.substring(nl + 1);
        if (line.endsWith('\r')) {
          line = line.substring(0, line.length - 1);
        }
        if (line.isEmpty) {
          final event = await flush();
          if (event != null) yield event;
          continue;
        }
        if (line.startsWith(':')) continue;
        if (line.startsWith('event:')) {
          eventName = line.substring(6).trim();
          continue;
        }
        if (line.startsWith('data:')) {
          var value = line.substring(5);
          if (value.startsWith(' ')) value = value.substring(1);
          dataLines.add(value);
          // Bound multi-line data accumulation for a single event.
          if (dataLines.length > 256) {
            dataLines.removeRange(0, dataLines.length - 128);
          }
        }
      }
    }
    final trailing = await flush();
    if (trailing != null) yield trailing;
  }

  Future<List<DigitalEmployee>> listDigitalEmployees() async {
    final catalog = await listDigitalEmployeesCatalog();
    return catalog.employees;
  }

  Future<MobileDigitalEmployeesCatalog> listDigitalEmployeesCatalog() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/digital-employees',
    );
    return MobileDigitalEmployeesCatalog.fromJson(response.data ?? const {});
  }

  /// List Hub emergency drafts (same library as MaClaw GUI top-bar).
  Future<List<DocumentDraft>> listDocumentDrafts({
    int limit = 50,
    bool includeBody = false,
  }) async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/documents/drafts',
      queryParameters: {
        'limit': limit.clamp(1, 200),
        if (includeBody) 'include_body': '1',
      },
    );
    final raw = response.data?['drafts'];
    if (raw is! List) return const [];
    return raw
        .whereType<Map>()
        .map((e) => DocumentDraft.fromJson(Map<String, dynamic>.from(e)))
        .where((d) => d.id.trim().isNotEmpty)
        .toList(growable: false);
  }

  Future<DocumentDraft> getDocumentDraft(String draftId) async {
    final id = draftId.trim();
    if (id.isEmpty) {
      throw ArgumentError('draftId is required');
    }
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/documents/drafts/${Uri.encodeComponent(id)}',
    );
    final data = response.data ?? const {};
    final draftMap = data['draft'] is Map
        ? Map<String, dynamic>.from(data['draft'] as Map)
        : Map<String, dynamic>.from(data);
    return DocumentDraft.fromJson(draftMap);
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

  /// Delete a Hub shared draft (same library as desktop GUI Mobile 文稿库).
  Future<void> deleteDocumentDraft(String draftId) async {
    final id = draftId.trim();
    if (id.isEmpty) {
      throw ArgumentError('draftId is required');
    }
    await _dio.delete<Map<String, dynamic>>(
      '/api/mobile/documents/drafts/${Uri.encodeComponent(id)}',
    );
  }

  /// Process a draft. Large docs or [async] return a background job; this method
  /// polls until ready (or returns immediately for sync processed).
  Future<DocumentDraft> processDocumentDraft({
    required String draftId,
    required String action,
    bool async = false,
  }) async {
    final encodedDraftId = Uri.encodeComponent(draftId);
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/documents/drafts/$encodedDraftId/process',
      data: {
        'action': action,
        if (async) 'async': true,
      },
      options: Options(
        // 202 Accepted is success for async document process.
        validateStatus: (code) =>
            code != null && code >= 200 && code < 300,
      ),
    );
    final data = response.data ?? const {};
    final status = (data['status'] as String? ?? '').trim();
    if (status == 'accepted' || data['async'] == true) {
      final jobId = (data['job_id'] as String? ?? '').trim();
      if (jobId.isEmpty) {
        throw StateError('document process accepted without job_id');
      }
      return _pollDocumentProcessJob(jobId);
    }
    return DocumentDraft.fromJson(
      Map<String, dynamic>.from(data['draft'] as Map? ?? const {}),
    );
  }

  Future<DocumentDraft> _pollDocumentProcessJob(String jobId) async {
    final encoded = Uri.encodeComponent(jobId);
    // Hub processing is fast for deterministic transforms; still poll a few times.
    for (var i = 0; i < 40; i++) {
      final response = await _dio.get<Map<String, dynamic>>(
        '/api/mobile/documents/process-jobs/$encoded',
      );
      final data = response.data ?? const {};
      final status = (data['status'] as String? ?? '').toLowerCase();
      if (status == 'ready') {
        final draftMap = data['draft'];
        if (draftMap is Map) {
          return DocumentDraft.fromJson(Map<String, dynamic>.from(draftMap));
        }
        throw StateError('document process ready without draft payload');
      }
      if (status.contains('fail') || status.contains('error')) {
        throw StateError(
          data['message'] as String? ?? 'document process failed',
        );
      }
      await Future<void>.delayed(const Duration(milliseconds: 150));
    }
    throw StateError('document process job timed out: $jobId');
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

  /// Binary downloads (originals/exports) can be multi-MB on slow mobile links.
  /// Default Hub Dio receiveTimeout is 15s — too short for 25MB originals.
  static const _binaryDownloadReceiveTimeout = Duration(seconds: 90);
  static const _binaryDownloadSendTimeout = Duration(seconds: 30);

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
      options: Options(
        responseType: ResponseType.bytes,
        receiveTimeout: _binaryDownloadReceiveTimeout,
        sendTimeout: _binaryDownloadSendTimeout,
      ),
    );
    return Uint8List.fromList(response.data ?? const []);
  }

  /// Download the original file attached to a Hub draft (for WeChat share etc.).
  Future<Uint8List> downloadDocumentOriginal(DocumentDraft draft) async {
    final path = draft.sourceDownloadUrl.trim().isNotEmpty
        ? draft.sourceDownloadUrl.trim()
        : '/api/mobile/documents/drafts/${Uri.encodeComponent(draft.id)}/source';
    final downloadUrl = maclawHubAbsoluteUrl(hubUrl: _hubUrl, pathOrUrl: path);
    final response = await _dio.get<List<int>>(
      downloadUrl,
      options: Options(
        responseType: ResponseType.bytes,
        receiveTimeout: _binaryDownloadReceiveTimeout,
        sendTimeout: _binaryDownloadSendTimeout,
      ),
    );
    final data = response.data;
    if (data == null || data.isEmpty) {
      throw StateError('empty original file / 原件为空');
    }
    return Uint8List.fromList(data);
  }

  /// Download a short-lived hub_exec file blob (`/api/mobile/ssh/files/download/{token}`).
  Future<Uint8List> downloadHubSSHFile(String pathOrUrl) async {
    final path = pathOrUrl.trim();
    if (path.isEmpty) {
      throw StateError('download url is empty / 下载地址为空');
    }
    final downloadUrl = maclawHubAbsoluteUrl(hubUrl: _hubUrl, pathOrUrl: path);
    final response = await _dio.get<List<int>>(
      downloadUrl,
      options: Options(
        responseType: ResponseType.bytes,
        receiveTimeout: _binaryDownloadReceiveTimeout,
        sendTimeout: _binaryDownloadSendTimeout,
      ),
    );
    final data = response.data;
    if (data == null || data.isEmpty) {
      throw StateError('empty download');
    }
    return Uint8List.fromList(data);
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

  /// Upsert server profiles to Hub so hub_exec / AI assistant ssh tools can see them.
  /// Secrets are NOT sent here — use [putSSHVaultSecret].
  Future<int> upsertServerProfiles(List<ServerProfile> profiles) async {
    final payload = <Map<String, dynamic>>[
      for (final p in profiles)
        if (p.isValid && p.id.trim().isNotEmpty)
          {
            'id': p.id.trim(),
            'name': p.name.trim().isEmpty ? p.id.trim() : p.name.trim(),
            'host': p.host.trim(),
            'port': p.port <= 0 ? 22 : p.port,
            'username': p.username.trim(),
            if (p.authMode.trim().isNotEmpty) 'auth_mode': p.authMode.trim(),
            if ((p.tag ?? '').trim().isNotEmpty) 'tag': p.tag!.trim(),
            if ((p.note ?? '').trim().isNotEmpty) 'note': p.note!.trim(),
          },
    ];
    if (payload.isEmpty) return 0;
    final response = await _dio.put<Map<String, dynamic>>(
      '/api/mobile/server-profiles',
      data: {'profiles': payload},
    );
    final data = response.data ?? const {};
    final count = data['count'];
    if (count is int) return count;
    if (count is num) return count.toInt();
    return payload.length;
  }

  Future<MobileBackendSSHSession> createBackendSSHSession({
    required String serverProfileId,
    String execMode = '',
  }) async {
    final mode = execMode.trim();
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions',
      data: {
        'server_profile_id': serverProfileId,
        if (mode.isNotEmpty) 'exec_mode': mode,
      },
    );
    return MobileBackendSSHSession.fromJson(
      _sessionPayload(response.data ?? const {}),
    );
  }

  /// One-shot setup: host + username + password → Hub profile + vault for AI ssh.
  /// User does not manage profiles separately.
  Future<MobileSSHQuickConnectResult> quickConnectSSH({
    required String host,
    required String username,
    required String password,
    int port = 22,
    String label = '',
  }) async {
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/quick-connect',
      data: {
        'host': host.trim(),
        'username': username.trim(),
        'password': password,
        'port': port <= 0 ? 22 : port,
        if (label.trim().isNotEmpty) 'label': label.trim(),
      },
    );
    return MobileSSHQuickConnectResult.fromJson(response.data ?? const {});
  }

  /// Store encrypted SSH secret on Hub for hub_exec (never returned in GET).
  Future<MobileSSHVaultStatus> putSSHVaultSecret({
    required String profileId,
    required String secret,
    String authMode = 'password',
    String passphrase = '',
  }) async {
    final id = Uri.encodeComponent(profileId.trim());
    final response = await _dio.put<Map<String, dynamic>>(
      '/api/mobile/ssh/vault/$id',
      data: {
        'auth_mode': authMode,
        'secret': secret,
        if (passphrase.trim().isNotEmpty) 'passphrase': passphrase.trim(),
      },
    );
    return MobileSSHVaultStatus.fromJson(response.data ?? const {});
  }

  Future<MobileSSHVaultStatus> getSSHVaultStatus(String profileId) async {
    final id = Uri.encodeComponent(profileId.trim());
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/ssh/vault/$id',
    );
    return MobileSSHVaultStatus.fromJson(response.data ?? const {});
  }

  /// List Hub vault metadata for this user (no secrets).
  Future<List<MobileSSHVaultStatus>> listSSHVault() async {
    final response = await _dio.get<Map<String, dynamic>>(
      '/api/mobile/ssh/vault',
    );
    final data = response.data ?? const {};
    final raw = data['items'];
    final list = <MobileSSHVaultStatus>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map) {
          list.add(
            MobileSSHVaultStatus.fromJson(Map<String, dynamic>.from(item)),
          );
        }
      }
    }
    return list;
  }

  Future<void> deleteSSHVaultSecret(String profileId) async {
    final id = Uri.encodeComponent(profileId.trim());
    await _dio.delete<Map<String, dynamic>>('/api/mobile/ssh/vault/$id');
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
    bool raw = false,
    /// When true, send [input] as base64 `data_b64` (hub_exec binary PTY frame).
    bool asBinary = false,
  }) async {
    final encodedSessionId = Uri.encodeComponent(sessionId);
    final data = <String, dynamic>{};
    if (asBinary || raw) {
      // Prefer data_b64 for raw/control so JSON never mangles binary bytes.
      data['data_b64'] = base64Encode(utf8.encode(input));
      data['raw'] = true;
    } else {
      data['input'] = input;
    }
    final response = await _dio.post<Map<String, dynamic>>(
      '/api/mobile/ssh/sessions/$encodedSessionId/input',
      data: data,
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

/// One Hub-side agent tool event streamed during /api/mobile/search.
class AssistantToolEvent {
  final String kind; // call | result
  final String id;
  final String name;
  final String detail;

  const AssistantToolEvent({
    required this.kind,
    required this.id,
    required this.name,
    this.detail = '',
  });

  bool get isCall => kind == 'call';
  bool get isResult => kind == 'result';

  AssistantToolEvent copyWith({
    String? kind,
    String? id,
    String? name,
    String? detail,
  }) {
    return AssistantToolEvent(
      kind: kind ?? this.kind,
      id: id ?? this.id,
      name: name ?? this.name,
      detail: detail ?? this.detail,
    );
  }
}

/// Aggregated per-tool counters for compact UI (avoids 20× "调用 ssh" chips).
class AssistantToolEventSummary {
  final String name;
  final int callCount;
  final int resultCount;

  const AssistantToolEventSummary({
    required this.name,
    required this.callCount,
    required this.resultCount,
  });

  int get totalEvents => callCount + resultCount;
  bool get inProgress => callCount > resultCount;
}

/// Collapse raw SSE tool events into one row per tool name.
List<AssistantToolEventSummary> summarizeAssistantToolEvents(
  Iterable<AssistantToolEvent> events,
) {
  final order = <String>[];
  final calls = <String, int>{};
  final results = <String, int>{};
  for (final event in events) {
    final name = event.name.trim().isEmpty ? 'tool' : event.name.trim();
    if (!calls.containsKey(name) && !results.containsKey(name)) {
      order.add(name);
    }
    if (event.isCall) {
      calls[name] = (calls[name] ?? 0) + 1;
    } else if (event.isResult) {
      results[name] = (results[name] ?? 0) + 1;
    } else {
      // Unknown kind: count as call so it still surfaces.
      calls[name] = (calls[name] ?? 0) + 1;
    }
  }
  return [
    for (final name in order)
      AssistantToolEventSummary(
        name: name,
        callCount: calls[name] ?? 0,
        resultCount: results[name] ?? 0,
      ),
  ];
}

/// Cap retained raw tool events to avoid unbounded SSE buffers on long agent runs.
const mobileAssistantMaxRetainedToolEvents = 40;

List<AssistantToolEvent> retainRecentAssistantToolEvents(
  List<AssistantToolEvent> events, {
  int maxEvents = mobileAssistantMaxRetainedToolEvents,
}) {
  if (maxEvents <= 0 || events.length <= maxEvents) {
    return events;
  }
  return events.sublist(events.length - maxEvents);
}

class SearchAnswer {
  final String answer;
  final List<SearchCitation> citations;
  final String llmMode;
  final String llmRequestId;
  final String llmUsageRecordId;
  final bool streaming;
  final bool agent;
  final List<AssistantToolEvent> toolEvents;

  const SearchAnswer({
    required this.answer,
    required this.citations,
    this.llmMode = '',
    this.llmRequestId = '',
    this.llmUsageRecordId = '',
    this.streaming = false,
    this.agent = false,
    this.toolEvents = const [],
  });

  bool get hasLlmTrace =>
      llmMode.isNotEmpty ||
      llmRequestId.isNotEmpty ||
      llmUsageRecordId.isNotEmpty;

  SearchAnswer copyWith({
    String? answer,
    List<SearchCitation>? citations,
    String? llmMode,
    String? llmRequestId,
    String? llmUsageRecordId,
    bool? streaming,
    bool? agent,
    List<AssistantToolEvent>? toolEvents,
  }) {
    return SearchAnswer(
      answer: answer ?? this.answer,
      citations: citations ?? this.citations,
      llmMode: llmMode ?? this.llmMode,
      llmRequestId: llmRequestId ?? this.llmRequestId,
      llmUsageRecordId: llmUsageRecordId ?? this.llmUsageRecordId,
      streaming: streaming ?? this.streaming,
      agent: agent ?? this.agent,
      toolEvents: toolEvents ?? this.toolEvents,
    );
  }

  factory SearchAnswer.fromJson(Map<String, dynamic> json) {
    return SearchAnswer(
      answer: json['answer'] as String? ?? '',
      citations: citationsFromJson(json),
      llmMode: (json['llm_mode'] as String? ?? '').trim(),
      llmRequestId: (json['llm_request_id'] as String? ?? '').trim(),
      llmUsageRecordId: (json['llm_usage_record_id'] as String? ?? '').trim(),
      streaming: json['status'] == 'streaming',
      agent: json['agent'] == true,
      toolEvents: toolEventsFromJson(json),
    );
  }

  static List<SearchCitation> citationsFromJson(Map<String, dynamic> json) {
    final raw = json['citations'];
    if (raw is! List) return const [];
    final citations = <SearchCitation>[];
    for (final item in raw) {
      if (item is Map) {
        citations.add(
          SearchCitation.fromJson(Map<String, dynamic>.from(item)),
        );
      }
    }
    return citations;
  }

  static List<AssistantToolEvent> toolEventsFromJson(Map<String, dynamic> json) {
    final raw = json['tool_events'];
    if (raw is! List) return const [];
    final events = <AssistantToolEvent>[];
    for (final item in raw) {
      if (item is! Map) continue;
      final map = Map<String, dynamic>.from(item);
      final name = (map['name'] as String? ?? '').trim();
      if (name.isEmpty) continue;
      events.add(
        AssistantToolEvent(
          kind: (map['kind'] as String? ?? 'call').trim(),
          id: (map['id'] as String? ?? '').trim(),
          name: name,
          detail: (map['detail'] as String? ??
                  map['result'] as String? ??
                  map['arguments'] as String? ??
                  '')
              .toString(),
        ),
      );
    }
    return events;
  }
}

class _SseEvent {
  final String event;
  final String data;

  const _SseEvent({required this.event, required this.data});

  Map<String, dynamic> get jsonMap {
    if (data.trim().isEmpty) return const {};
    try {
      final decoded = jsonDecode(data);
      if (decoded is Map<String, dynamic>) return decoded;
      if (decoded is Map) return Map<String, dynamic>.from(decoded);
    } on FormatException {
      // Malformed SSE data line — treat as empty payload.
    } on Object {
      // Ignore unexpected decode failures for a single event.
    }
    return const {};
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
  final String execMode;
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
    this.execMode = 'desktop_exec',
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

  bool get isHubExec =>
      execMode == 'hub_exec' || execMode == 'hub' || execMode == 'server';

  bool get connected {
    final s = status.trim().toLowerCase();
    final st = state.trim().toLowerCase();
    if (s == 'closed' ||
        s == 'close_requested' ||
        st == 'closed' ||
        st == 'closing' ||
        st == 'hub_closed') {
      return false;
    }
    return s == 'connected' ||
        s == 'running' ||
        s == 'attached' ||
        s == 'ready' ||
        st == 'connected' ||
        st == 'running' ||
        st == 'attached' ||
        st == 'hub_connected' ||
        st == 'hub_streaming';
  }

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
      execMode: (json['exec_mode'] as String? ?? 'desktop_exec').trim().isEmpty
          ? 'desktop_exec'
          : (json['exec_mode'] as String? ?? 'desktop_exec').trim(),
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

class MobileSSHVaultStatus {
  final String profileId;
  final bool hasSecret;
  final String authMode;
  final String updatedAt;
  final String status;

  const MobileSSHVaultStatus({
    this.profileId = '',
    this.hasSecret = false,
    this.authMode = '',
    this.updatedAt = '',
    this.status = '',
  });

  factory MobileSSHVaultStatus.fromJson(Map<String, dynamic> json) {
    return MobileSSHVaultStatus(
      profileId: json['profile_id'] as String? ?? '',
      hasSecret: json['has_secret'] as bool? ?? false,
      authMode: json['auth_mode'] as String? ?? '',
      updatedAt: json['updated_at'] as String? ?? '',
      status: json['status'] as String? ?? '',
    );
  }
}

class MobileSSHQuickConnectResult {
  final String profileId;
  final String label;
  final String host;
  final int port;
  final String username;
  final bool hasSecret;
  final String message;
  final String assistantHint;

  const MobileSSHQuickConnectResult({
    this.profileId = '',
    this.label = '',
    this.host = '',
    this.port = 22,
    this.username = '',
    this.hasSecret = false,
    this.message = '',
    this.assistantHint = '',
  });

  factory MobileSSHQuickConnectResult.fromJson(Map<String, dynamic> json) {
    final portRaw = json['port'];
    final port = portRaw is int
        ? portRaw
        : (portRaw is num ? portRaw.toInt() : 22);
    return MobileSSHQuickConnectResult(
      profileId: json['profile_id'] as String? ?? '',
      label: json['label'] as String? ?? '',
      host: json['host'] as String? ?? '',
      port: port <= 0 ? 22 : port,
      username: json['username'] as String? ?? '',
      hasSecret: json['has_secret'] as bool? ?? false,
      message: json['message'] as String? ?? '',
      assistantHint: json['assistant_hint'] as String? ?? '',
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
  final String createdAt;
  final String updatedAt;

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
    this.createdAt = '',
    this.updatedAt = '',
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
      createdAt: json['created_at'] as String? ?? '',
      updatedAt: json['updated_at'] as String? ?? '',
    );
  }
}

class MobileMcpServer {
  final String id;
  final String name;
  final String endpointUrl;
  final String authType;
  final String authSecret;
  final bool hasAuthSecret;

  const MobileMcpServer({
    required this.id,
    required this.name,
    required this.endpointUrl,
    this.authType = 'none',
    this.authSecret = '',
    this.hasAuthSecret = false,
  });

  factory MobileMcpServer.fromJson(Map<String, dynamic> json) {
    return MobileMcpServer(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      endpointUrl: json['endpoint_url'] as String? ?? '',
      authType: json['auth_type'] as String? ?? 'none',
      authSecret: '',
      hasAuthSecret: json['has_auth_secret'] as bool? ?? false,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'name': name,
      'endpoint_url': endpointUrl,
      'auth_type': authType,
      if (authSecret.trim().isNotEmpty) 'auth_secret': authSecret,
    };
  }

  MobileMcpServer copyWith({
    String? id,
    String? name,
    String? endpointUrl,
    String? authType,
    String? authSecret,
    bool? hasAuthSecret,
  }) {
    return MobileMcpServer(
      id: id ?? this.id,
      name: name ?? this.name,
      endpointUrl: endpointUrl ?? this.endpointUrl,
      authType: authType ?? this.authType,
      authSecret: authSecret ?? this.authSecret,
      hasAuthSecret: hasAuthSecret ?? this.hasAuthSecret,
    );
  }
}

class MobileAgentMcpConfig {
  final List<MobileMcpServer> servers;
  final bool localMcpAllowed;

  const MobileAgentMcpConfig({
    this.servers = const [],
    this.localMcpAllowed = false,
  });

  factory MobileAgentMcpConfig.fromJson(Map<String, dynamic> json) {
    final raw = json['mcp_servers'];
    final list = <MobileMcpServer>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map) {
          list.add(
            MobileMcpServer.fromJson(Map<String, dynamic>.from(item)),
          );
        }
      }
    }
    return MobileAgentMcpConfig(
      servers: list,
      localMcpAllowed: json['local_mcp_allowed'] as bool? ?? false,
    );
  }
}

class MobileAgentKnowledgeStatus {
  final bool available;
  final int sources;
  final int cards;
  final int facts;
  final String mode;
  final String message;

  const MobileAgentKnowledgeStatus({
    this.available = false,
    this.sources = 0,
    this.cards = 0,
    this.facts = 0,
    this.mode = '',
    this.message = '',
  });

  factory MobileAgentKnowledgeStatus.fromJson(Map<String, dynamic> json) {
    return MobileAgentKnowledgeStatus(
      available: json['available'] as bool? ?? false,
      sources: json['sources'] as int? ?? 0,
      cards: json['cards'] as int? ?? 0,
      facts: json['facts'] as int? ?? 0,
      mode: json['mode'] as String? ?? '',
      message: json['message'] as String? ?? '',
    );
  }
}

class MobileAgentKnowledgeIngestResult {
  final bool ok;
  final String sourceId;
  final String title;
  final int runeCount;
  final String mode;

  const MobileAgentKnowledgeIngestResult({
    this.ok = false,
    this.sourceId = '',
    this.title = '',
    this.runeCount = 0,
    this.mode = '',
  });

  factory MobileAgentKnowledgeIngestResult.fromJson(Map<String, dynamic> json) {
    return MobileAgentKnowledgeIngestResult(
      ok: json['ok'] as bool? ?? false,
      sourceId: json['source_id'] as String? ?? '',
      title: json['title'] as String? ?? '',
      runeCount: json['rune_count'] as int? ?? 0,
      mode: json['mode'] as String? ?? '',
    );
  }
}

class MobileMcpServerHealth {
  final String id;
  final String name;
  final String healthStatus;
  final int toolCount;
  final bool running;
  final String endpointUrl;

  const MobileMcpServerHealth({
    required this.id,
    required this.name,
    required this.healthStatus,
    this.toolCount = 0,
    this.running = false,
    this.endpointUrl = '',
  });

  factory MobileMcpServerHealth.fromJson(Map<String, dynamic> json) {
    return MobileMcpServerHealth(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      healthStatus: json['health_status'] as String? ?? 'unknown',
      toolCount: json['tool_count'] as int? ?? 0,
      running: json['running'] as bool? ?? false,
      endpointUrl: json['endpoint_url'] as String? ?? '',
    );
  }

  bool get isHealthy =>
      healthStatus == 'healthy' || healthStatus == 'slow';
}

class MobileAgentMcpHealth {
  final int serverCount;
  final int healthyCount;
  final int availableTools;
  final List<MobileMcpServerHealth> servers;
  final String probedAt;

  const MobileAgentMcpHealth({
    this.serverCount = 0,
    this.healthyCount = 0,
    this.availableTools = 0,
    this.servers = const [],
    this.probedAt = '',
  });

  factory MobileAgentMcpHealth.fromJson(Map<String, dynamic> json) {
    final raw = json['servers'];
    final list = <MobileMcpServerHealth>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map) {
          list.add(
            MobileMcpServerHealth.fromJson(Map<String, dynamic>.from(item)),
          );
        }
      }
    }
    return MobileAgentMcpHealth(
      serverCount: json['server_count'] as int? ?? list.length,
      healthyCount: json['healthy_count'] as int? ?? 0,
      availableTools: json['available_tools'] as int? ?? 0,
      servers: list,
      probedAt: json['probed_at'] as String? ?? '',
    );
  }
}

class MobileAgentSkill {
  final String name;
  final String description;
  final String type;
  final String status;
  final String version;
  final String source;
  final int stepCount;

  const MobileAgentSkill({
    required this.name,
    this.description = '',
    this.type = 'executable',
    this.status = 'active',
    this.version = '',
    this.source = '',
    this.stepCount = 0,
  });

  factory MobileAgentSkill.fromJson(Map<String, dynamic> json) {
    return MobileAgentSkill(
      name: json['name'] as String? ?? '',
      description: json['description'] as String? ?? '',
      type: json['type'] as String? ?? 'executable',
      status: json['status'] as String? ?? 'active',
      version: json['version'] as String? ?? '',
      source: json['source'] as String? ?? '',
      stepCount: json['step_count'] as int? ?? 0,
    );
  }
}

class MobileAgentSkillsList {
  final List<MobileAgentSkill> skills;
  final int count;

  const MobileAgentSkillsList({
    this.skills = const [],
    this.count = 0,
  });

  factory MobileAgentSkillsList.fromJson(Map<String, dynamic> json) {
    final raw = json['skills'];
    final list = <MobileAgentSkill>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map) {
          list.add(MobileAgentSkill.fromJson(Map<String, dynamic>.from(item)));
        }
      }
    }
    return MobileAgentSkillsList(
      skills: list,
      count: json['count'] as int? ?? list.length,
    );
  }
}

/// Unified long-running job row from GET /api/mobile/jobs (design §5).
class MobileJob {
  final String jobId;
  final String kind;
  final String title;
  final String status;
  final double? progress;
  final String message;
  final String deepLink;
  final String updatedAt;
  final String draftId;
  final String employeeId;
  final String sessionId;

  const MobileJob({
    required this.jobId,
    required this.kind,
    this.title = '',
    this.status = '',
    this.progress,
    this.message = '',
    this.deepLink = '',
    this.updatedAt = '',
    this.draftId = '',
    this.employeeId = '',
    this.sessionId = '',
  });

  bool get isActive {
    final s = status.toLowerCase();
    if (s.isEmpty) return false;
    if (s.contains('ready') ||
        s.contains('done') ||
        s.contains('complete') ||
        s.contains('success') ||
        s.contains('fail') ||
        s.contains('error') ||
        s.contains('cancel')) {
      return false;
    }
    return true;
  }

  factory MobileJob.fromJson(Map<String, dynamic> json) {
    double? progress;
    final rawProgress = json['progress'];
    if (rawProgress is num) {
      progress = rawProgress.toDouble();
    }
    return MobileJob(
      jobId: json['job_id'] as String? ?? '',
      kind: json['kind'] as String? ?? '',
      title: json['title'] as String? ?? '',
      status: json['status'] as String? ?? '',
      progress: progress,
      message: json['message'] as String? ?? '',
      deepLink: json['deep_link'] as String? ?? '',
      updatedAt: json['updated_at'] as String? ?? '',
      draftId: json['draft_id'] as String? ?? '',
      employeeId: json['employee_id'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
    );
  }
}

class MobileJobsList {
  final List<MobileJob> jobs;
  final int count;
  final int activeCount;
  final String generatedAt;

  const MobileJobsList({
    this.jobs = const [],
    this.count = 0,
    this.activeCount = 0,
    this.generatedAt = '',
  });

  factory MobileJobsList.fromJson(Map<String, dynamic> json) {
    final raw = json['jobs'];
    final list = <MobileJob>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map) {
          list.add(MobileJob.fromJson(Map<String, dynamic>.from(item)));
        }
      }
    }
    return MobileJobsList(
      jobs: list,
      count: json['count'] as int? ?? list.length,
      activeCount: json['active_count'] as int? ?? 0,
      generatedAt: json['generated_at'] as String? ?? '',
    );
  }
}

/// Long-running official assistant job (POST /api/mobile/agent/jobs).
class MobileAgentJob {
  final String jobId;
  final String kind;
  final String query;
  final String title;
  final String status;
  final String message;
  final String answer;
  final String documentId;
  final String deepLink;
  final String llmRequestId;
  final double? progress;

  const MobileAgentJob({
    required this.jobId,
    this.kind = 'assistant',
    this.query = '',
    this.title = '',
    this.status = '',
    this.message = '',
    this.answer = '',
    this.documentId = '',
    this.deepLink = '/tasks',
    this.llmRequestId = '',
    this.progress,
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

  factory MobileAgentJob.fromJson(Map<String, dynamic> json) {
    // Nested job object from search async acceptance payload.
    final nested = json['job'];
    if (nested is Map && (json['job_id'] == null || json['status'] == null)) {
      return MobileAgentJob.fromJson(Map<String, dynamic>.from(nested));
    }
    double? progress;
    final rawProgress = json['progress'];
    if (rawProgress is num) progress = rawProgress.toDouble();
    return MobileAgentJob(
      jobId: json['job_id'] as String? ?? '',
      kind: json['kind'] as String? ?? 'assistant',
      query: json['query'] as String? ?? '',
      title: json['title'] as String? ?? '',
      status: json['status'] as String? ?? '',
      message: json['message'] as String? ?? '',
      answer: json['answer'] as String? ?? '',
      documentId: json['document_id'] as String? ?? '',
      deepLink: json['deep_link'] as String? ?? '/tasks',
      llmRequestId: json['llm_request_id'] as String? ?? '',
      progress: progress,
    );
  }
}


class LlmServiceCardRedeemResult {
  final bool success;
  final String message;
  final LlmServiceStatus? status;

  const LlmServiceCardRedeemResult({
    this.success = false,
    this.message = '',
    this.status,
  });

  factory LlmServiceCardRedeemResult.fromJson(Map<String, dynamic> json) {
    final statusRaw = json['service_status'];
    LlmServiceStatus? status;
    if (statusRaw is Map) {
      status = LlmServiceStatus.fromJson(Map<String, dynamic>.from(statusRaw));
    }
    final success = json['success'] as bool? ?? status != null;
    return LlmServiceCardRedeemResult(
      success: success,
      message: success ? '���񿨶һ��ɹ�' : (json['message'] as String? ?? ''),
      status: status,
    );
  }
}

class MobileDocumentQuota {
  final int documentQuotaBytes;
  final int documentQuotaUsedBytes;
  final int documentQuotaRemaining;

  const MobileDocumentQuota({
    this.documentQuotaBytes = 0,
    this.documentQuotaUsedBytes = 0,
    this.documentQuotaRemaining = 0,
  });

  factory MobileDocumentQuota.fromJson(Map<String, dynamic> json) {
    int asInt(Object? v) {
      if (v is int) return v;
      if (v is num) return v.toInt();
      return int.tryParse(v?.toString() ?? '') ?? 0;
    }

    return MobileDocumentQuota(
      documentQuotaBytes: asInt(json['document_quota_bytes']),
      documentQuotaUsedBytes: asInt(json['document_quota_used_bytes']),
      documentQuotaRemaining: asInt(json['document_quota_remaining']),
    );
  }
}

/// Live plan caps from GET /api/mobile/entitlements/caps.
class MobilePushPendingItem {
  final String id;
  final String type;
  final String title;
  final String body;
  final String payload;
  final String status;
  final String taskId;
  final DateTime? createdAt;

  const MobilePushPendingItem({
    this.id = '',
    this.type = '',
    this.title = '',
    this.body = '',
    this.payload = '',
    this.status = '',
    this.taskId = '',
    this.createdAt,
  });

  factory MobilePushPendingItem.fromJson(Map<String, dynamic> json) {
    DateTime? created;
    final raw = json['created_at']?.toString() ?? '';
    if (raw.isNotEmpty) {
      created = DateTime.tryParse(raw);
    }
    return MobilePushPendingItem(
      id: json['id'] as String? ?? '',
      type: json['type'] as String? ?? '',
      title: json['title'] as String? ?? '',
      body: json['body'] as String? ?? '',
      payload: json['payload'] as String? ?? '',
      status: json['status'] as String? ?? '',
      taskId: json['task_id'] as String? ?? '',
      createdAt: created,
    );
  }
}

class MobilePushPendingList {
  final List<MobilePushPendingItem> items;
  final int count;
  final bool remoteConfigured;
  final bool pendingSync;

  const MobilePushPendingList({
    this.items = const [],
    this.count = 0,
    this.remoteConfigured = false,
    this.pendingSync = true,
  });

  factory MobilePushPendingList.fromJson(Map<String, dynamic> json) {
    final raw = json['items'];
    final items = <MobilePushPendingItem>[];
    if (raw is List) {
      for (final e in raw) {
        if (e is Map) {
          items.add(
            MobilePushPendingItem.fromJson(Map<String, dynamic>.from(e)),
          );
        }
      }
    }
    final transport = json['transport'] is Map
        ? Map<String, dynamic>.from(json['transport'] as Map)
        : const <String, dynamic>{};
    return MobilePushPendingList(
      items: items,
      count: json['count'] is int
          ? json['count'] as int
          : items.length,
      remoteConfigured: transport['remote_configured'] as bool? ?? false,
      pendingSync: transport['pending_sync'] as bool? ?? true,
    );
  }
}

class MobileCapsPutResult {
  final bool ok;
  final Map<String, int> runtimeOverrides;
  final Map<String, int> effective;
  final String serverTime;

  const MobileCapsPutResult({
    this.ok = false,
    this.runtimeOverrides = const {},
    this.effective = const {},
    this.serverTime = '',
  });

  factory MobileCapsPutResult.fromJson(Map<String, dynamic> json) {
    int asInt(Object? v) {
      if (v is int) return v;
      if (v is num) return v.toInt();
      return int.tryParse(v?.toString() ?? '') ?? 0;
    }

    Map<String, int> asIntMap(Object? raw) {
      final out = <String, int>{};
      if (raw is Map) {
        raw.forEach((k, v) {
          out['$k'] = asInt(v);
        });
      }
      return out;
    }

    return MobileCapsPutResult(
      ok: json['ok'] as bool? ?? false,
      runtimeOverrides: asIntMap(json['runtime_overrides']),
      effective: asIntMap(json['effective']),
      serverTime: json['server_time'] as String? ?? '',
    );
  }
}

class MobileEntitlementsCaps {
  final String plan;
  final int documentQuotaBytes;
  final int maxUploadBytes;
  final int maxExportJobs;
  final bool sharedEmployees;
  final bool hubSshExec;
  final bool mobileAgent;
  final bool documentAi;
  final int hubFileDownloadMaxBytes;
  final bool hubFileDownloadChunked;
  final int hubFileSingleShotBytes;
  final int hubFileChunkRawBytes;
  final Map<String, String> envOverrides;
  final Map<String, int> runtimeOverrides;
  final String serverTime;

  const MobileEntitlementsCaps({
    this.plan = 'free',
    this.documentQuotaBytes = 0,
    this.maxUploadBytes = 0,
    this.maxExportJobs = 0,
    this.sharedEmployees = false,
    this.hubSshExec = false,
    this.mobileAgent = false,
    this.documentAi = false,
    this.hubFileDownloadMaxBytes = 0,
    this.hubFileDownloadChunked = false,
    this.hubFileSingleShotBytes = 0,
    this.hubFileChunkRawBytes = 0,
    this.envOverrides = const {},
    this.runtimeOverrides = const {},
    this.serverTime = '',
  });

  bool get hasRuntimeOverrides =>
      runtimeOverrides.values.any((v) => v > 0);

  factory MobileEntitlementsCaps.fromJson(Map<String, dynamic> json) {
    int asInt(Object? v) {
      if (v is int) return v;
      if (v is num) return v.toInt();
      return int.tryParse(v?.toString() ?? '') ?? 0;
    }

    final caps = json['caps'] is Map
        ? Map<String, dynamic>.from(json['caps'] as Map)
        : <String, dynamic>{};
    final envRaw = json['env_overrides'];
    final env = <String, String>{};
    if (envRaw is Map) {
      envRaw.forEach((k, v) {
        env['$k'] = '$v';
      });
    }
    final rtRaw = json['runtime_overrides'];
    final runtime = <String, int>{};
    if (rtRaw is Map) {
      rtRaw.forEach((k, v) {
        runtime['$k'] = asInt(v);
      });
    }
    return MobileEntitlementsCaps(
      plan: (json['plan'] as String? ?? 'free').trim().isEmpty
          ? 'free'
          : (json['plan'] as String? ?? 'free').trim(),
      documentQuotaBytes: asInt(caps['document_quota_bytes']),
      maxUploadBytes: asInt(caps['max_upload_bytes']),
      maxExportJobs: asInt(caps['max_export_jobs']),
      sharedEmployees: caps['shared_employees'] as bool? ?? false,
      hubSshExec: caps['hub_ssh_exec'] as bool? ?? false,
      mobileAgent: caps['mobile_agent'] as bool? ?? false,
      documentAi: caps['document_ai'] as bool? ?? false,
      hubFileDownloadMaxBytes: asInt(caps['hub_file_download_max_bytes']),
      hubFileDownloadChunked: caps['hub_file_download_chunked'] as bool? ?? false,
      hubFileSingleShotBytes: asInt(caps['hub_file_single_shot_bytes']),
      hubFileChunkRawBytes: asInt(caps['hub_file_chunk_raw_bytes']),
      envOverrides: env,
      runtimeOverrides: runtime,
      serverTime: json['server_time'] as String? ?? '',
    );
  }
}

class MobileCardStoreProduct {
  final String id;
  final String kind;
  final String label;
  final bool enabled;
  final double price;
  final int durationDays;
  final double credits;

  const MobileCardStoreProduct({
    required this.id,
    this.kind = '',
    this.label = '',
    this.enabled = true,
    this.price = 0,
    this.durationDays = 0,
    this.credits = 0,
  });

  factory MobileCardStoreProduct.fromJson(Map<String, dynamic> json) {
    double asDouble(Object? v) {
      if (v is num) return v.toDouble();
      return double.tryParse(v?.toString() ?? '') ?? 0;
    }

    int asInt(Object? v) {
      if (v is int) return v;
      if (v is num) return v.toInt();
      return int.tryParse(v?.toString() ?? '') ?? 0;
    }

    return MobileCardStoreProduct(
      id: json['id'] as String? ?? '',
      kind: json['kind'] as String? ?? '',
      label: json['label'] as String? ?? '',
      enabled: json['enabled'] as bool? ?? true,
      price: asDouble(json['price']),
      durationDays: asInt(json['duration_days']),
      credits: asDouble(json['credits']),
    );
  }
}

class MobileCardStoreCatalog {
  final bool enabled;
  final String tenantId;
  final String paymentMode;
  final List<MobileCardStoreProduct> products;

  const MobileCardStoreCatalog({
    this.enabled = false,
    this.tenantId = '',
    this.paymentMode = '',
    this.products = const [],
  });

  factory MobileCardStoreCatalog.fromJson(Map<String, dynamic> json) {
    final raw = json['products'];
    final list = <MobileCardStoreProduct>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map) {
          list.add(MobileCardStoreProduct.fromJson(Map<String, dynamic>.from(item)));
        }
      }
    }
    return MobileCardStoreCatalog(
      enabled: json['enabled'] as bool? ?? false,
      tenantId: json['tenant_id'] as String? ?? '',
      paymentMode: json['payment_mode'] as String? ?? '',
      products: list,
    );
  }
}

class MobileCardStoreOrder {
  final String orderNo;
  final String productId;
  final String productLabel;
  final String status;
  final String payUrl;
  final String payQrUrl;
  final String paymentMode;
  final double amount;

  const MobileCardStoreOrder({
    this.orderNo = '',
    this.productId = '',
    this.productLabel = '',
    this.status = '',
    this.payUrl = '',
    this.payQrUrl = '',
    this.paymentMode = '',
    this.amount = 0,
  });

  factory MobileCardStoreOrder.fromJson(Map<String, dynamic> json) {
    double asDouble(Object? v) {
      if (v is num) return v.toDouble();
      return double.tryParse(v?.toString() ?? '') ?? 0;
    }

    return MobileCardStoreOrder(
      orderNo: json['order_no'] as String? ?? '',
      productId: json['product_id'] as String? ?? '',
      productLabel: json['product_label'] as String? ?? '',
      status: json['status'] as String? ?? '',
      payUrl: json['pay_url'] as String? ?? '',
      payQrUrl: json['pay_qr_url'] as String? ?? '',
      paymentMode: json['payment_mode'] as String? ?? '',
      amount: asDouble(json['amount']),
    );
  }
}
