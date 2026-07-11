import 'dart:async';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';
import 'package:path_provider/path_provider.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_realtime_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../core/platform/mobile_permission_evidence.dart';
import '../../core/security/mobile_redaction.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import '../tasks/mobile_jobs_provider.dart';
import 'document_draft.dart';

/// Map Hub DOCUMENT_QUOTA_EXCEEDED (HTTP 507) to a user-facing message.
Object mapDocumentStorageError(Object error) {
  if (error is! DioException) return error;
  final code = error.response?.statusCode ?? 0;
  final data = error.response?.data;
  var apiCode = '';
  var message = '';
  if (data is Map) {
    apiCode = '${data['code'] ?? data['error'] ?? ''}'.trim();
    message = '${data['message'] ?? ''}'.trim();
  }
  final blob = '$apiCode $message ${error.message ?? ''}'.toLowerCase();
  if (code == 507 ||
      apiCode == 'DOCUMENT_QUOTA_EXCEEDED' ||
      blob.contains('document storage quota') ||
      blob.contains('quota exceeded')) {
    return StateError(
      '文档空间不足（已达配额上限）。请删除部分草稿或导入文件，'
      '或在「我的」兑换/购买服务卡扩容后重试。',
    );
  }
  if (message.isNotEmpty) {
    return StateError(message);
  }
  return error;
}

final documentsControllerProvider =
    AsyncNotifierProvider<DocumentsController, DocumentsState>(
  DocumentsController.new,
);

final documentPermissionEvidenceProvider =
    StateProvider<String?>((ref) => null);

final documentDraftHistoryProvider =
    AsyncNotifierProvider<DocumentDraftHistoryController, List<DocumentDraft>>(
  DocumentDraftHistoryController.new,
);

const mobileDocumentImportExtensions = [
  'docx',
  'doc',
  'pdf',
  'xlsx',
  'xls',
  'txt',
  'md',
  'markdown',
  'log',
  'csv',
  'json',
  'png',
  'jpg',
  'jpeg',
];

class DocumentDraftHistoryController
    extends AsyncNotifier<List<DocumentDraft>> {
  @override
  Future<List<DocumentDraft>> build() => _load();

  Future<void> refresh() async {
    state = await AsyncValue.guard(_load);
  }

  /// Prefer Hub library (same as MaClaw GUI); fall back to phone cache offline.
  Future<List<DocumentDraft>> _load() async {
    final client = ref.read(apiClientProvider);
    if (client != null) {
      try {
        final remote = await client.listDocumentDrafts(limit: 80);
        if (remote.isNotEmpty) return remote;
      } on Object {
        // Fall through to local cache.
      }
    }
    return ref.read(mobileLocalStoreProvider).loadRecentDocumentDrafts();
  }
}

class DocumentsController extends AsyncNotifier<DocumentsState> {
  Timer? _exportPollTimer;
  Timer? _uploadPollTimer;
  final Set<String> _notifiedExportJobs = {};
  final Set<String> _notifiedUploadTasks = {};

  @override
  Future<DocumentsState> build() async {
    ref.onDispose(() {
      _exportPollTimer?.cancel();
      _uploadPollTimer?.cancel();
      _exportPollTimer = null;
      _uploadPollTimer = null;
    });
    final store = ref.read(mobileLocalStoreProvider);
    final cachedDraft = await store.loadLastDocumentDraft();
    final cachedUpload = await store.loadLastDocumentUploadTask();
    final cachedUploadPath = await store.loadLastDocumentUploadPath();
    final cachedExport = await store.loadLastDocumentExportJob();
    if (cachedUpload != null) {
      _ensureUploadPolling(cachedUpload);
    }
    if (cachedExport != null) {
      _ensureExportPolling(cachedExport);
    }
    return DocumentsState(
      draft: cachedDraft,
      uploadTask: cachedUpload,
      exportJob: cachedExport,
      lastUploadPath: cachedUploadPath,
    );
  }

  Future<void> createDraft({
    required String title,
    required DocumentTemplate template,
    String content = '',
  }) async {
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(
        StateError('请先登录 MaClaw 官方服务。'),
        StackTrace.current,
      );
      return;
    }
    final current = state.valueOrNull ?? const DocumentsState();
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
      try {
        final safeTitle = redactMobileSensitiveText(title.trim());
        final safeContent = redactMobileSensitiveText(content.trim());
        final draft = await client.createDocumentDraft(
          title: safeTitle,
          template: template,
          content: safeContent,
        );
        await _cacheDraft(draft);
        ref.invalidate(documentQuotaProvider);
        return DocumentsState(
          draft: draft,
          uploadTask: current.uploadTask,
          lastUploadPath: current.lastUploadPath,
        );
      } on Object catch (error) {
        throw mapDocumentStorageError(error);
      }
    });
  }

  Future<void> exportDraft(DocumentExportFormat format) async {
    final client = ref.read(apiClientProvider);
    final current = state.valueOrNull;
    final draft = current?.draft;
    if (client == null || current == null || draft == null) {
      state = AsyncError(
        StateError('请先创建文档草稿。'),
        StackTrace.current,
      );
      return;
    }
    state = const AsyncLoading();
    final next = await AsyncValue.guard(() async {
      final job =
          await client.exportDocument(draftId: draft.id, format: format);
      await ref.read(mobileLocalStoreProvider).saveLastDocumentExportJob(job);
      await _notifyExportFinished(job, draft.title);
      return DocumentsState(
        draft: draft,
        exportJob: job,
        uploadTask: current.uploadTask,
        lastUploadPath: current.lastUploadPath,
      );
    });
    state = next;
    final job = next.valueOrNull?.exportJob;
    if (job != null) {
      _ensureExportPolling(job);
    }
  }

  Future<void> retryLastExport() async {
    final current = state.valueOrNull;
    final job = current?.exportJob;
    if (current == null || job == null || !current.canRetryLastExport) {
      state = AsyncError(
        StateError('没有可重试的导出任务。'),
        StackTrace.current,
      );
      return;
    }
    await exportDraft(job.format);
  }

  Future<void> saveDraftEdits({
    required String title,
    required String markdown,
  }) async {
    final client = ref.read(apiClientProvider);
    final current = state.valueOrNull;
    final draft = current?.draft;
    if (client == null || current == null || draft == null) {
      state = AsyncError(
        StateError('请先创建文档草稿。'),
        StackTrace.current,
      );
      return;
    }
    final normalizedTitle = title.trim();
    final normalizedMarkdown = markdown.trim();
    if (normalizedTitle.isEmpty || normalizedMarkdown.isEmpty) {
      state = AsyncError(
        StateError('标题和正文不能为空。'),
        StackTrace.current,
      );
      return;
    }
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
      try {
        final safeTitle = redactMobileSensitiveText(normalizedTitle);
        final safeMarkdown = redactMobileSensitiveText(normalizedMarkdown);
        final updated = await client.updateDocumentDraft(
          draftId: draft.id,
          title: safeTitle,
          markdown: safeMarkdown,
        );
        await _cacheDraft(updated);
        ref.invalidate(documentQuotaProvider);
        return DocumentsState(
          draft: updated,
          uploadTask: current.uploadTask,
          exportJob: current.exportJob,
          lastUploadPath: current.lastUploadPath,
        );
      } on Object catch (error) {
        throw mapDocumentStorageError(error);
      }
    });
  }

  Future<void> processDraft(String action) async {
    final client = ref.read(apiClientProvider);
    final current = state.valueOrNull;
    final draft = current?.draft;
    if (client == null || current == null || draft == null) {
      state = AsyncError(
        StateError('请先创建文档草稿。'),
        StackTrace.current,
      );
      return;
    }
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
      final updated = await client.processDocumentDraft(
        draftId: draft.id,
        action: action,
      );
      await _cacheDraft(updated);
      ref.invalidate(mobileJobsProvider);
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: '文档处理完成',
            body: _documentNotificationBody('${draft.title} 已完成 $action。'),
            payload: mobileDocumentDraftNotificationPayload(updated.id),
          );
      return DocumentsState(
        draft: updated,
        uploadTask: current.uploadTask,
        exportJob: current.exportJob,
        lastUploadPath: current.lastUploadPath,
      );
    });
  }

  Future<void> refreshExportJob({bool silent = false}) async {
    final client = ref.read(apiClientProvider);
    final current = state.valueOrNull;
    final job = current?.exportJob;
    if (client == null || current == null || job == null) return;
    if (!silent) {
      state = const AsyncLoading();
    }
    final next = await AsyncValue.guard(() async {
      final refreshed = await client.getDocumentExportJob(job.jobId);
      await ref
          .read(mobileLocalStoreProvider)
          .saveLastDocumentExportJob(refreshed);
      await _notifyExportFinished(refreshed, current.draft?.title ?? '文档');
      return DocumentsState(
        draft: current.draft,
        exportJob: refreshed,
        uploadTask: current.uploadTask,
        lastUploadPath: current.lastUploadPath,
      );
    });
    state = next;
    final refreshed = next.valueOrNull?.exportJob;
    if (refreshed != null) {
      _ensureExportPolling(refreshed);
    }
  }

  Future<void> refreshUploadTask({bool silent = false}) async {
    final client = ref.read(apiClientProvider);
    final current = state.valueOrNull;
    final upload = current?.uploadTask;
    if (client == null || current == null || upload == null) return;
    if (!silent) {
      state = const AsyncLoading();
    }
    final next = await AsyncValue.guard(() async {
      final refreshed = await client.getDocumentUploadTask(upload.taskId);
      await ref.read(mobileLocalStoreProvider).saveLastDocumentUploadTask(
            refreshed,
            sourcePath: current.lastUploadPath,
          );
      await _notifyUploadReady(refreshed);
      if (refreshed.draft != null) {
        await _cacheDraft(refreshed.draft!);
      }
      return DocumentsState(
        draft: refreshed.draft ?? current.draft,
        exportJob: refreshed.draft == null ? current.exportJob : null,
        uploadTask: refreshed,
        lastUploadPath: current.lastUploadPath,
      );
    });
    state = next;
    final refreshed = next.valueOrNull?.uploadTask;
    if (refreshed != null) {
      _ensureUploadPolling(refreshed);
    }
  }

  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    if (!event.documentTask) return;
    final payload = event.payload;
    if (payload.isEmpty) return;
    if ((payload['job_id'] as String? ?? '').isNotEmpty) {
      await _applyRealtimeExport(payload);
      return;
    }
    if ((payload['task_id'] as String? ?? '').isNotEmpty) {
      await _applyRealtimeUpload(payload);
    }
  }

  Future<void> _applyRealtimeExport(Map<String, dynamic> payload) async {
    final current = state.valueOrNull ?? const DocumentsState();
    final job = DocumentExportJob.fromJson(payload);
    if (job.jobId.isEmpty) return;
    await ref.read(mobileLocalStoreProvider).saveLastDocumentExportJob(job);
    await _notifyExportFinished(job, current.draft?.title ?? '文档');
    state = AsyncData(
      DocumentsState(
        draft: current.draft,
        exportJob: job,
        uploadTask: current.uploadTask,
        lastUploadPath: current.lastUploadPath,
      ),
    );
    _ensureExportPolling(job);
  }

  Future<void> _applyRealtimeUpload(Map<String, dynamic> payload) async {
    final current = state.valueOrNull ?? const DocumentsState();
    final upload = MobileDocumentUploadTask.fromJson(payload);
    if (upload.taskId.isEmpty) return;
    await ref.read(mobileLocalStoreProvider).saveLastDocumentUploadTask(
          upload,
          sourcePath: current.lastUploadPath,
        );
    await _notifyUploadReady(upload);
    if (upload.draft != null) {
      await _cacheDraft(upload.draft!);
    }
    state = AsyncData(
      DocumentsState(
        draft: upload.draft ?? current.draft,
        exportJob: upload.draft == null ? current.exportJob : null,
        uploadTask: upload,
        lastUploadPath: current.lastUploadPath,
      ),
    );
    _ensureUploadPolling(upload);
  }

  String? exportDownloadUrl(DocumentExportJob job) {
    final client = ref.read(apiClientProvider);
    if (client == null || job.downloadUrl.isEmpty) return null;
    return client.absoluteUrl(job.downloadUrl);
  }

  Future<File> downloadExportFile(DocumentExportJob job) async {
    final client = ref.read(apiClientProvider);
    final current = state.valueOrNull;
    if (client == null || current?.draft == null) {
      throw StateError('请先登录并创建文档草稿。');
    }
    if (job.status != 'ready') {
      throw StateError('导出任务尚未完成。');
    }
    final bytes = await client.downloadDocumentExport(job);
    final directory = await getTemporaryDirectory();
    final filename = _exportFilename(current!.draft!.title, job);
    final file = File('${directory.path}${Platform.pathSeparator}$filename');
    return file.writeAsBytes(bytes, flush: true);
  }

  Future<void> pickAndUploadDocument() async {
    try {
      final picked = await FilePicker.platform.pickFiles(
        type: FileType.custom,
        allowedExtensions: mobileDocumentImportExtensions,
      );
      final path = picked?.files.single.path;
      if (path == null || path.isEmpty) return;
      ref.read(documentPermissionEvidenceProvider.notifier).state =
          mobilePermissionGrantEvidence('media');
      await uploadSharedDocument(path);
    } on Object {
      _setImportError('无法打开文件选择器，请检查文件权限后重试。');
    }
  }

  Future<void> pickImageAndUploadDocument(ImageSource source) async {
    try {
      final picked = await ImagePicker().pickImage(source: source);
      final path = picked?.path;
      if (path == null || path.isEmpty) return;
      ref.read(documentPermissionEvidenceProvider.notifier).state =
          mobilePermissionGrantEvidence(
        source == ImageSource.camera ? 'camera' : 'media',
      );
      await uploadSharedDocument(path);
    } on Object {
      _setImportError(
        source == ImageSource.camera
            ? '无法打开相机，请检查相机权限后重试。'
            : '无法打开相册，请检查照片权限后重试。',
      );
    }
  }

  void _setImportError(String message) {
    final current = state.valueOrNull ?? const DocumentsState();
    state = AsyncData(current.copyWith(operationError: message));
  }

  Future<void> uploadSharedDocument(String path) async {
    await ref.read(sessionControllerProvider.future);
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(
        StateError('请先登录 MaClaw 官方服务。'),
        StackTrace.current,
      );
      return;
    }
    final typeError = validateMobileDocumentImportPath(path);
    if (typeError != null) {
      state = AsyncError(StateError(typeError), StackTrace.current);
      return;
    }
    final sizeError = await _validateUploadSize(path);
    if (sizeError != null) {
      state = AsyncError(StateError(sizeError), StackTrace.current);
      return;
    }
    await _uploadDocumentPath(client: client, path: path);
  }

  Future<void> retryLastUpload() async {
    final current = state.valueOrNull;
    final path = current?.lastUploadPath;
    if (current == null || !current.canRetryLastUpload || path == null) {
      state = AsyncError(
        StateError('没有失败的导入任务可重试。'),
        StackTrace.current,
      );
      return;
    }
    await uploadSharedDocument(path);
  }

  Future<void> _uploadDocumentPath({
    required ApiClient client,
    required String path,
  }) async {
    final current = state.valueOrNull ?? const DocumentsState();
    state = const AsyncLoading();
    final next = await AsyncValue.guard(() async {
      try {
        final upload = await client.uploadDocument(path);
        await ref.read(mobileLocalStoreProvider).saveLastDocumentUploadTask(
              upload,
              sourcePath: path,
            );
        await _notifyUploadReady(upload);
        if (upload.draft != null) {
          await _cacheDraft(upload.draft!);
        }
        ref.invalidate(documentQuotaProvider);
        return DocumentsState(
          draft: upload.draft ?? current.draft,
          exportJob: upload.draft == null ? current.exportJob : null,
          uploadTask: upload,
          lastUploadPath: path,
        );
      } on Object catch (error) {
        throw mapDocumentStorageError(error);
      }
    });
    state = next;
    final upload = next.valueOrNull?.uploadTask;
    if (upload != null) {
      _ensureUploadPolling(upload);
    }
  }

  Future<String?> _validateUploadSize(String path) async {
    final session = await ref.read(sessionControllerProvider.future);
    final maxUploadBytes = session.bootstrap?.limits.maxUploadBytes ?? 0;
    if (maxUploadBytes <= 0) return null;
    final file = File(path);
    if (!await file.exists()) return null;
    final length = await file.length();
    if (length <= maxUploadBytes) return null;
    return '文件大小 ${formatMobileFileSize(length)} 超过官方服务上传限制 '
        '${formatMobileFileSize(maxUploadBytes)}，请压缩或拆分后再导入。';
  }

  Future<void> _notifyUploadReady(MobileDocumentUploadTask upload) async {
    if (!_uploadFinished(upload) || upload.taskId.isEmpty) return;
    if (!_notifiedUploadTasks.add(upload.taskId)) return;
    await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
          title: upload.status == 'failed' ? '文档解析失败' : '文档解析完成',
          body: _documentNotificationBody(
            upload.status == 'failed'
                ? '${upload.filename} 解析失败：${upload.message.isEmpty ? '请重新导入或改用文本/文档格式。' : upload.message}'
                : '${upload.filename} 已生成移动草稿。',
          ),
          payload: upload.draftId.isEmpty
              ? mobileDocumentUploadNotificationPayload(upload.taskId)
              : mobileDocumentDraftNotificationPayload(upload.draftId),
        );
  }

  Future<void> _notifyExportFinished(
    DocumentExportJob job,
    String draftTitle,
  ) async {
    if (!_exportFinished(job) || job.jobId.isEmpty) return;
    if (!_notifiedExportJobs.add(job.jobId)) return;
    final payload = _exportNotificationPayload(job);
    await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
          title: job.status == 'failed' ? '文档导出失败' : '文档导出完成',
          body: _documentNotificationBody(
            job.status == 'failed'
                ? '$draftTitle 导出失败：${job.message.isEmpty ? '请重试或改用其他格式。' : job.message}'
                : '$draftTitle 已生成 ${documentExportFormatWireValue(job.format)}。',
          ),
          payload: payload,
        );
  }

  String _documentNotificationBody(String body) {
    return redactMobileSensitiveText(body);
  }

  String _exportNotificationPayload(DocumentExportJob job) {
    return mobileDocumentExportNotificationPayload(job.jobId);
  }

  Future<void> _cacheDraft(DocumentDraft draft) {
    return ref.read(mobileLocalStoreProvider).saveLastDocumentDraft(draft).then(
          (_) => ref.invalidate(documentDraftHistoryProvider),
        );
  }

  Future<void> selectDraft(DocumentDraft draft) async {
    var full = draft;
    final client = ref.read(apiClientProvider);
    // List items often only have preview; fetch full body for edit/share.
    if (client != null && draft.id.trim().isNotEmpty) {
      try {
        full = await client.getDocumentDraft(draft.id);
      } on Object {
        // Keep list payload if detail fetch fails.
      }
    }
    await _cacheDraft(full);
    final current = state.valueOrNull ?? const DocumentsState();
    state = AsyncData(
      DocumentsState(
        draft: full,
        uploadTask: current.uploadTask,
        lastUploadPath: current.lastUploadPath,
        exportJob: current.exportJob,
      ),
    );
  }

  String shareTextForDraft(DocumentDraft draft) {
    final title =
        draft.title.trim().isEmpty ? 'MaClaw 文档' : draft.title.trim();
    return redactMobileSensitiveText('# $title\n\n${draft.markdown}');
  }

  void _ensureExportPolling(DocumentExportJob job) {
    if (_exportFinished(job)) {
      _exportPollTimer?.cancel();
      _exportPollTimer = null;
      return;
    }
    if (_exportPollTimer?.isActive ?? false) {
      return;
    }
    _exportPollTimer = Timer.periodic(const Duration(seconds: 6), (_) {
      final current = state.valueOrNull;
      final job = current?.exportJob;
      if (job == null || _exportFinished(job) || state.isLoading) {
        if (job == null || _exportFinished(job)) {
          _exportPollTimer?.cancel();
          _exportPollTimer = null;
        }
        return;
      }
      unawaited(refreshExportJob(silent: true));
    });
  }

  void _ensureUploadPolling(MobileDocumentUploadTask upload) {
    if (_uploadFinished(upload)) {
      _uploadPollTimer?.cancel();
      _uploadPollTimer = null;
      return;
    }
    if (_uploadPollTimer?.isActive ?? false) {
      return;
    }
    _uploadPollTimer = Timer.periodic(const Duration(seconds: 6), (_) {
      final current = state.valueOrNull;
      final upload = current?.uploadTask;
      if (upload == null || _uploadFinished(upload) || state.isLoading) {
        if (upload == null || _uploadFinished(upload)) {
          _uploadPollTimer?.cancel();
          _uploadPollTimer = null;
        }
        return;
      }
      unawaited(refreshUploadTask(silent: true));
    });
  }

  bool _exportFinished(DocumentExportJob job) {
    return job.status == 'ready' || job.status == 'failed';
  }

  bool _uploadFinished(MobileDocumentUploadTask upload) {
    return upload.status == 'ready' || upload.status == 'failed';
  }

  String _exportFilename(String title, DocumentExportJob job) {
    final base = _exportFilenameBase(title, fallback: job.jobId);
    return '$base.${_exportExtension(job.format)}';
  }

  String _exportFilenameBase(String title, {required String fallback}) {
    final normalized = redactMobileSensitiveText(title)
        .trim()
        .replaceAll(RegExp(r'[\\/:*?"<>|]+'), '_')
        .replaceAll(RegExp(r'\s+'), '_')
        .replaceAll(RegExp(r'_+'), '_')
        .replaceAll(RegExp(r'^_+|_+$'), '');
    final safeBase = normalized.isEmpty ? fallback : normalized;
    if (safeBase.length <= 72) return safeBase;
    final shortened = safeBase.substring(0, 72).replaceAll(RegExp(r'_+$'), '');
    return shortened.isEmpty ? fallback : shortened;
  }

  String _exportExtension(DocumentExportFormat format) {
    return switch (format) {
      DocumentExportFormat.pdf => 'pdf',
      DocumentExportFormat.word => 'docx',
      DocumentExportFormat.markdown => 'md',
    };
  }
}

class DocumentsState {
  final DocumentDraft? draft;
  final DocumentExportJob? exportJob;
  final MobileDocumentUploadTask? uploadTask;
  final String? lastUploadPath;
  final String? operationError;

  const DocumentsState({
    this.draft,
    this.exportJob,
    this.uploadTask,
    this.lastUploadPath,
    this.operationError,
  });

  DocumentsState copyWith({
    DocumentDraft? draft,
    DocumentExportJob? exportJob,
    MobileDocumentUploadTask? uploadTask,
    String? lastUploadPath,
    String? operationError,
  }) {
    return DocumentsState(
      draft: draft ?? this.draft,
      exportJob: exportJob ?? this.exportJob,
      uploadTask: uploadTask ?? this.uploadTask,
      lastUploadPath: lastUploadPath ?? this.lastUploadPath,
      operationError: operationError,
    );
  }

  bool get canRetryLastUpload =>
      uploadTask?.status == 'failed' && (lastUploadPath?.isNotEmpty ?? false);

  bool get canRetryLastExport => draft != null && exportJob?.status == 'failed';
}

String formatMobileFileSize(int bytes) {
  if (bytes < 1024) return '$bytes B';
  final units = ['KB', 'MB', 'GB'];
  var value = bytes / 1024.0;
  var unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024.0;
    unitIndex++;
  }
  final text = value >= 10
      ? value.toStringAsFixed(0)
      : value.toStringAsFixed(1).replaceFirst(RegExp(r'\.0$'), '');
  return '$text ${units[unitIndex]}';
}

String? validateMobileDocumentImportPath(String path) {
  final extension = _mobileDocumentExtension(path);
  if (extension == null ||
      !mobileDocumentImportExtensions.contains(extension.toLowerCase())) {
    return '暂不支持该文件类型。请导入 Word、PDF、Excel、图片、Markdown、'
        'CSV、JSON、TXT 或日志文件。';
  }
  return null;
}

String? _mobileDocumentExtension(String path) {
  final withoutQuery = path.split('?').first.split('#').first.trim();
  final parts = withoutQuery.split(RegExp(r'[\\/]'));
  final filename = parts.where((part) => part.isNotEmpty).lastOrNull;
  if (filename == null) return null;
  final dot = filename.lastIndexOf('.');
  if (dot <= 0 || dot == filename.length - 1) return null;
  return filename.substring(dot + 1);
}
