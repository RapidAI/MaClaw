import 'dart:async';
import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';

import '../../core/api/api_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import 'document_draft.dart';

final documentsControllerProvider =
    AsyncNotifierProvider<DocumentsController, DocumentsState>(
  DocumentsController.new,
);

class DocumentsController extends AsyncNotifier<DocumentsState> {
  Timer? _exportPollTimer;
  Timer? _uploadPollTimer;

  @override
  Future<DocumentsState> build() async {
    ref.onDispose(() {
      _exportPollTimer?.cancel();
      _uploadPollTimer?.cancel();
      _exportPollTimer = null;
      _uploadPollTimer = null;
    });
    final cachedDraft =
        await ref.read(mobileLocalStoreProvider).loadLastDocumentDraft();
    return DocumentsState(draft: cachedDraft);
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
      final draft = await client.createDocumentDraft(
        title: title,
        template: template,
        content: content,
      );
      await _cacheDraft(draft);
      return DocumentsState(draft: draft, uploadTask: current.uploadTask);
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
      final job = await client.exportDocument(draftId: draft.id, format: format);
      if (job.status == 'ready') {
        await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
              title: '文档导出完成',
              body: '${draft.title} 已生成 ${documentExportFormatWireValue(format)}。',
              payload: job.downloadUrl,
            );
      }
      return DocumentsState(
        draft: draft,
        exportJob: job,
        uploadTask: current.uploadTask,
      );
    });
    state = next;
    final job = next.valueOrNull?.exportJob;
    if (job != null) {
      _ensureExportPolling(job);
    }
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
      final updated = await client.updateDocumentDraft(
        draftId: draft.id,
        title: normalizedTitle,
        markdown: normalizedMarkdown,
      );
      await _cacheDraft(updated);
      return DocumentsState(
        draft: updated,
        uploadTask: current.uploadTask,
      );
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
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: '文档处理完成',
            body: '${draft.title} 已完成 $action。',
            payload: updated.id,
          );
      return DocumentsState(
        draft: updated,
        uploadTask: current.uploadTask,
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
      if (refreshed.status == 'ready') {
        await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
              title: '文档导出完成',
              body: '导出任务 ${refreshed.jobId} 已可下载。',
              payload: refreshed.downloadUrl,
            );
      }
      return DocumentsState(
        draft: current.draft,
        exportJob: refreshed,
        uploadTask: current.uploadTask,
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
      await _notifyUploadReady(refreshed);
      if (refreshed.draft != null) {
        await _cacheDraft(refreshed.draft!);
      }
      return DocumentsState(
        draft: refreshed.draft ?? current.draft,
        exportJob: refreshed.draft == null ? current.exportJob : null,
        uploadTask: refreshed,
      );
    });
    state = next;
    final refreshed = next.valueOrNull?.uploadTask;
    if (refreshed != null) {
      _ensureUploadPolling(refreshed);
    }
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
    final picked = await FilePicker.platform.pickFiles(
      type: FileType.custom,
      allowedExtensions: const [
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
      ],
    );
    final path = picked?.files.single.path;
    if (path == null || path.isEmpty) return;
    await uploadSharedDocument(path);
  }

  Future<void> uploadSharedDocument(String path) async {
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(
        StateError('请先登录 MaClaw 官方服务。'),
        StackTrace.current,
      );
      return;
    }
    await _uploadDocumentPath(client: client, path: path);
  }

  Future<void> _uploadDocumentPath({
    required ApiClient client,
    required String path,
  }) async {
    final current = state.valueOrNull ?? const DocumentsState();
    state = const AsyncLoading();
    final next = await AsyncValue.guard(() async {
      final upload = await client.uploadDocument(path);
      await _notifyUploadReady(upload);
      if (upload.draft != null) {
        await _cacheDraft(upload.draft!);
      }
      return DocumentsState(
        draft: upload.draft ?? current.draft,
        exportJob: upload.draft == null ? current.exportJob : null,
        uploadTask: upload,
      );
    });
    state = next;
    final upload = next.valueOrNull?.uploadTask;
    if (upload != null) {
      _ensureUploadPolling(upload);
    }
  }

  Future<void> _notifyUploadReady(MobileDocumentUploadTask upload) async {
    if (upload.status == 'ready' || upload.status == 'failed') {
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: upload.status == 'failed' ? '文档解析失败' : '文档解析完成',
            body: upload.status == 'failed'
                ? '${upload.filename} 解析失败：${upload.message.isEmpty ? '请重新导入或改用文本/文档格式。' : upload.message}'
                : '${upload.filename} 已生成移动草稿。',
            payload: upload.draftId.isEmpty ? upload.taskId : upload.draftId,
          );
    }
  }

  Future<void> _cacheDraft(DocumentDraft draft) {
    return ref.read(mobileLocalStoreProvider).saveLastDocumentDraft(draft);
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
    return upload.status == 'ready' ||
        upload.status == 'failed';
  }

  String _exportFilename(String title, DocumentExportJob job) {
    final base = title
        .trim()
        .replaceAll(RegExp(r'[\\/:*?"<>|]+'), '_')
        .replaceAll(RegExp(r'\s+'), '_');
    final safeBase = base.isEmpty ? job.jobId : base;
    return '$safeBase.${_exportExtension(job.format)}';
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

  const DocumentsState({this.draft, this.exportJob, this.uploadTask});
}
