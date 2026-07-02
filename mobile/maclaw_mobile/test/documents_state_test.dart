import 'dart:io';

import 'package:drift/native.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_client.dart';
import 'package:maclaw_mobile/core/notifications/mobile_notification_service.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';
import 'package:maclaw_mobile/features/documents/documents_controller.dart';

class _SignedInSessionController extends SessionController {
  @override
  Future<SessionState> build() async => const SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: MobileBootstrap(
          user: MobileUser(
            userId: 'user-1',
            email: 'mobile@example.com',
            tenantId: 'tenant-1',
          ),
          services: MobileServices(
            hubStatus: 'online',
            llmStatus: 'available',
            searchStatus: 'available',
            documentsStatus: 'available',
            digitalEmployeesStatus: 'available',
            llmStatusPath: '',
            modelsPath: '',
            searchPath: '/api/mobile/search',
            documentsPath: '/api/mobile/documents',
            digitalEmployeesPath: '/api/mobile/digital-employees',
            realtimePath: '/api/mobile/realtime',
          ),
          features: MobileFeatures(
            search: true,
            documents: true,
            localSsh: true,
            digitalEmployees: true,
            pushNotifications: false,
          ),
          limits: MobileLimits(
            maxUploadBytes: 8,
            maxExportJobs: 3,
          ),
        ),
      );
}

class _RecordingNotificationService extends MobileNotificationService {
  final shown = <({String title, String body, String? payload})>[];

  @override
  Future<void> showTaskCompleted({
    required String title,
    required String body,
    String? payload,
  }) async {
    shown.add((title: title, body: body, payload: payload));
  }
}

void main() {
  test(
    'document upload retry is available only for failed imports with source path',
    () {
      const failedUpload = MobileDocumentUploadTask(
        taskId: 'upload-1',
        filename: 'incident.pdf',
        status: 'failed',
      );

      expect(
        const DocumentsState(
          uploadTask: failedUpload,
          lastUploadPath: '/tmp/incident.pdf',
        ).canRetryLastUpload,
        isTrue,
      );
      expect(
        const DocumentsState(uploadTask: failedUpload).canRetryLastUpload,
        isFalse,
      );
      expect(
        const DocumentsState(
          uploadTask: MobileDocumentUploadTask(
            taskId: 'upload-2',
            filename: 'incident.pdf',
            status: 'ready',
          ),
          lastUploadPath: '/tmp/incident.pdf',
        ).canRetryLastUpload,
        isFalse,
      );
    },
  );

  test('document export retry is available only for failed exports with draft',
      () {
    final draft = DocumentDraft(
      id: 'draft-1',
      title: '应急报告',
      template: DocumentTemplate.report,
      markdown: '# 应急报告',
      updatedAt: DateTime.utc(2026),
    );
    final failedExport = DocumentExportJob(
      jobId: 'export-1',
      draftId: draft.id,
      format: DocumentExportFormat.pdf,
      status: 'failed',
      downloadUrl: '',
      createdAt: DateTime.utc(2026),
    );

    expect(
      DocumentsState(draft: draft, exportJob: failedExport).canRetryLastExport,
      isTrue,
    );
    expect(
      DocumentsState(exportJob: failedExport).canRetryLastExport,
      isFalse,
    );
    expect(
      DocumentsState(
        draft: draft,
        exportJob: DocumentExportJob(
          jobId: 'export-2',
          draftId: draft.id,
          format: DocumentExportFormat.pdf,
          status: 'ready',
          downloadUrl: '/api/mobile/documents/exports/export-2/download',
          createdAt: DateTime.utc(2026),
        ),
      ).canRetryLastExport,
      isFalse,
    );
  });

  test('formats mobile file sizes for upload limit messages', () {
    expect(formatMobileFileSize(512), '512 B');
    expect(formatMobileFileSize(1536), '1.5 KB');
    expect(formatMobileFileSize(12 * 1024 * 1024), '12 MB');
  });

  test('document upload rejects files above official mobile limit', () async {
    final cacheDir = await Directory.systemTemp.createTemp(
      'maclaw_mobile_store_',
    );
    addTearDown(() async {
      if (await cacheDir.exists()) {
        await cacheDir.delete(recursive: true);
      }
    });
    final store = MobileLocalStore(
      executor: NativeDatabase.memory(),
      documentsDirectory: () async => cacheDir,
    );
    addTearDown(store.close);
    final file = File('${Directory.systemTemp.path}/maclaw_mobile_big.txt');
    await file.writeAsString('0123456789abcdef');
    addTearDown(() async {
      if (await file.exists()) {
        await file.delete();
      }
    });

    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
      ],
    );
    addTearDown(container.dispose);
    await container.read(sessionControllerProvider.future);
    await container.read(documentsControllerProvider.future);

    await container
        .read(documentsControllerProvider.notifier)
        .uploadSharedDocument(file.path);

    final state = container.read(documentsControllerProvider);
    expect(state.hasError, isTrue);
    expect(state.error.toString(), contains('超过官方服务上传限制'));
    expect(state.error.toString(), contains('8 B'));
  });

  test('document realtime export completion is cached and notified once',
      () async {
    final cacheDir = await Directory.systemTemp.createTemp(
      'maclaw_mobile_store_',
    );
    addTearDown(() async {
      if (await cacheDir.exists()) {
        await cacheDir.delete(recursive: true);
      }
    });
    final store = MobileLocalStore(
      executor: NativeDatabase.memory(),
      documentsDirectory: () async => cacheDir,
    );
    addTearDown(store.close);
    final notifications = _RecordingNotificationService();
    final draft = DocumentDraft(
      id: 'draft-1',
      title: '应急报告',
      template: DocumentTemplate.report,
      markdown: '# 应急报告',
      updatedAt: DateTime.utc(2026, 7, 2),
    );
    await store.saveLastDocumentDraft(draft);
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
      ],
    );
    addTearDown(container.dispose);
    await container.read(sessionControllerProvider.future);
    await container.read(documentsControllerProvider.future);

    const event = MobileRealtimeEvent(
      type: 'document_task',
      payload: {
        'job_id': 'export-1',
        'draft_id': 'draft-1',
        'format': 'pdf',
        'status': 'ready',
        'download_url': '/api/mobile/documents/exports/export-1/download',
        'created_at': '2026-07-02T00:00:00Z',
      },
    );
    await container
        .read(documentsControllerProvider.notifier)
        .applyRealtimeEvent(event);
    await container
        .read(documentsControllerProvider.notifier)
        .applyRealtimeEvent(event);

    final state = container.read(documentsControllerProvider).valueOrNull;
    final cached = await store.loadLastDocumentExportJob();
    expect(state?.exportJob?.jobId, 'export-1');
    expect(cached?.status, 'ready');
    expect(notifications.shown, hasLength(1));
    expect(notifications.shown.single.title, '文档导出完成');
    expect(notifications.shown.single.body, contains('应急报告 已生成 pdf'));
    expect(
      notifications.shown.single.payload,
      'https://tenant-a.maclaw.top/api/mobile/documents/exports/export-1/download',
    );
  });

  test('document export notification falls back when download URL is external',
      () async {
    final cacheDir = await Directory.systemTemp.createTemp(
      'maclaw_mobile_store_',
    );
    addTearDown(() async {
      if (await cacheDir.exists()) {
        await cacheDir.delete(recursive: true);
      }
    });
    final store = MobileLocalStore(
      executor: NativeDatabase.memory(),
      documentsDirectory: () async => cacheDir,
    );
    addTearDown(store.close);
    final notifications = _RecordingNotificationService();
    final draft = DocumentDraft(
      id: 'draft-1',
      title: '应急报告',
      template: DocumentTemplate.report,
      markdown: '# 应急报告',
      updatedAt: DateTime.utc(2026, 7, 2),
    );
    await store.saveLastDocumentDraft(draft);
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
      ],
    );
    addTearDown(container.dispose);
    await container.read(sessionControllerProvider.future);
    await container.read(documentsControllerProvider.future);

    const event = MobileRealtimeEvent(
      type: 'document_task',
      payload: {
        'job_id': 'export-external',
        'draft_id': 'draft-1',
        'format': 'pdf',
        'status': 'ready',
        'download_url': 'https://example.invalid/export-external.pdf',
        'created_at': '2026-07-02T00:00:00Z',
      },
    );
    await container
        .read(documentsControllerProvider.notifier)
        .applyRealtimeEvent(event);

    expect(notifications.shown, hasLength(1));
    expect(notifications.shown.single.title, '文档导出完成');
    expect(notifications.shown.single.payload, 'export-external');
  });

  test('document realtime upload completion caches draft and notifies once',
      () async {
    final cacheDir = await Directory.systemTemp.createTemp(
      'maclaw_mobile_store_',
    );
    addTearDown(() async {
      if (await cacheDir.exists()) {
        await cacheDir.delete(recursive: true);
      }
    });
    final store = MobileLocalStore(
      executor: NativeDatabase.memory(),
      documentsDirectory: () async => cacheDir,
    );
    addTearDown(store.close);
    final notifications = _RecordingNotificationService();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
      ],
    );
    addTearDown(container.dispose);
    await container.read(documentsControllerProvider.future);

    const event = MobileRealtimeEvent(
      type: 'document_task',
      payload: {
        'task_id': 'upload-1',
        'filename': 'incident.pdf',
        'status': 'ready',
        'draft_id': 'draft-upload-1',
        'draft': {
          'id': 'draft-upload-1',
          'title': '现场说明',
          'template': 'statement',
          'markdown': '# 现场说明',
          'updated_at': '2026-07-02T00:00:00Z',
        },
      },
    );
    await container
        .read(documentsControllerProvider.notifier)
        .applyRealtimeEvent(event);
    await container
        .read(documentsControllerProvider.notifier)
        .applyRealtimeEvent(event);

    final state = container.read(documentsControllerProvider).valueOrNull;
    final cachedDraft = await store.loadLastDocumentDraft();
    final cachedUpload = await store.loadLastDocumentUploadTask();
    expect(state?.draft?.id, 'draft-upload-1');
    expect(cachedDraft?.title, '现场说明');
    expect(cachedUpload?.status, 'ready');
    expect(notifications.shown, hasLength(1));
    expect(notifications.shown.single.title, '文档解析完成');
    expect(notifications.shown.single.body, contains('incident.pdf 已生成移动草稿'));
    expect(notifications.shown.single.payload, 'draft-upload-1');
  });
}
