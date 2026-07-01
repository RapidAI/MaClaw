import 'package:file_picker/file_picker.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../auth/session_controller.dart';
import 'document_draft.dart';

final documentsControllerProvider =
    AsyncNotifierProvider<DocumentsController, DocumentsState>(
  DocumentsController.new,
);

class DocumentsController extends AsyncNotifier<DocumentsState> {
  @override
  Future<DocumentsState> build() async => const DocumentsState();

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
    state = await AsyncValue.guard(() async {
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
      return DocumentsState(
        draft: updated,
        exportJob: current.exportJob,
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
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: '文档处理完成',
            body: '${draft.title} 已完成 $action。',
            payload: updated.id,
          );
      return DocumentsState(
        draft: updated,
        exportJob: current.exportJob,
        uploadTask: current.uploadTask,
      );
    });
  }

  Future<void> refreshExportJob() async {
    final client = ref.read(apiClientProvider);
    final current = state.valueOrNull;
    final job = current?.exportJob;
    if (client == null || current == null || job == null) return;
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
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
  }

  Future<void> refreshUploadTask() async {
    final client = ref.read(apiClientProvider);
    final current = state.valueOrNull;
    final upload = current?.uploadTask;
    if (client == null || current == null || upload == null) return;
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
      final refreshed = await client.getDocumentUploadTask(upload.taskId);
      final draft = refreshed.draft ?? current.draft;
      if ((refreshed.status == 'ready' || refreshed.status == 'needs_ocr') &&
          refreshed.draft != null) {
        await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
              title: '文档解析完成',
              body: refreshed.status == 'needs_ocr'
                  ? '${refreshed.filename} 已生成待 OCR 草稿。'
                  : '${refreshed.filename} 已生成移动草稿。',
              payload: refreshed.draftId,
            );
      }
      return DocumentsState(
        draft: draft,
        exportJob: current.exportJob,
        uploadTask: refreshed,
      );
    });
  }

  String? exportDownloadUrl(DocumentExportJob job) {
    final client = ref.read(apiClientProvider);
    if (client == null || job.downloadUrl.isEmpty) return null;
    return client.absoluteUrl(job.downloadUrl);
  }

  Future<void> pickAndUploadDocument() async {
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(
        StateError('请先登录 MaClaw 官方服务。'),
        StackTrace.current,
      );
      return;
    }
    final picked = await FilePicker.platform.pickFiles(
      type: FileType.custom,
      allowedExtensions: [
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
    final current = state.valueOrNull ?? const DocumentsState();
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
      final upload = await client.uploadDocument(path);
      final draft = upload.draft ?? current.draft;
      if ((upload.status == 'ready' || upload.status == 'needs_ocr') &&
          upload.draft != null) {
        await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
              title: '文档解析完成',
              body: upload.status == 'needs_ocr'
                  ? '${upload.filename} 已生成待 OCR 草稿。'
                  : '${upload.filename} 已生成移动草稿。',
              payload: upload.draftId,
            );
      }
      return DocumentsState(
        draft: draft,
        exportJob: current.exportJob,
        uploadTask: upload,
      );
    });
  }
}

class DocumentsState {
  final DocumentDraft? draft;
  final DocumentExportJob? exportJob;
  final MobileDocumentUploadTask? uploadTask;

  const DocumentsState({this.draft, this.exportJob, this.uploadTask});
}
