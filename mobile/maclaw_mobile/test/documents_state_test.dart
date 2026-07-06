import 'dart:io';
import 'dart:typed_data';

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
import 'package:path_provider_platform_interface/path_provider_platform_interface.dart';

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
            backendSshSessions: true,
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

class _UploadReadyApiClient extends ApiClient {
  _UploadReadyApiClient() : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<MobileDocumentUploadTask> getDocumentUploadTask(String taskId) async {
    return MobileDocumentUploadTask(
      taskId: taskId,
      filename: 'incident.pdf',
      status: 'ready',
      draftId: 'draft-upload-refresh',
      draft: DocumentDraft(
        id: 'draft-upload-refresh',
        title: '现场说明',
        template: DocumentTemplate.statement,
        markdown: '# 现场说明',
        updatedAt: DateTime.utc(2026, 7, 2),
      ),
    );
  }
}

class _RecordingUploadApiClient extends ApiClient {
  final uploadedPaths = <String>[];

  _RecordingUploadApiClient() : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<MobileDocumentUploadTask> uploadDocument(String path) async {
    uploadedPaths.add(path);
    return MobileDocumentUploadTask(
      taskId: 'upload-${uploadedPaths.length}',
      filename: path.split(RegExp(r'[\\/]')).last,
      status: 'queued',
    );
  }
}

class _QueuedUploadDocumentsController extends DocumentsController {
  @override
  Future<DocumentsState> build() async => const DocumentsState(
        uploadTask: MobileDocumentUploadTask(
          taskId: 'upload-queued',
          filename: 'incident.pdf',
          status: 'in_progress',
        ),
        lastUploadPath: '/tmp/incident.pdf',
      );
}

class _ProcessDraftApiClient extends ApiClient {
  _ProcessDraftApiClient() : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<DocumentDraft> processDocumentDraft({
    required String draftId,
    required String action,
  }) async {
    return DocumentDraft(
      id: '$draftId-processed',
      title: 'incident summary',
      template: DocumentTemplate.report,
      markdown: '# incident summary\n\nProcessed with $action.',
      updatedAt: DateTime.utc(2026, 7, 2),
    );
  }
}

class _RecordingDraftApiClient extends ApiClient {
  final created = <({String title, String content})>[];
  final updated = <({String title, String markdown})>[];

  _RecordingDraftApiClient() : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<DocumentDraft> createDocumentDraft({
    required String title,
    required DocumentTemplate template,
    String content = '',
  }) async {
    created.add((title: title, content: content));
    return DocumentDraft(
      id: 'draft-created',
      title: title,
      template: template,
      markdown: content,
      updatedAt: DateTime.utc(2026, 7, 2),
    );
  }

  @override
  Future<DocumentDraft> updateDocumentDraft({
    required String draftId,
    required String title,
    required String markdown,
  }) async {
    updated.add((title: title, markdown: markdown));
    return DocumentDraft(
      id: draftId,
      title: title,
      template: DocumentTemplate.report,
      markdown: markdown,
      updatedAt: DateTime.utc(2026, 7, 2, 1),
    );
  }
}
class _DownloadExportApiClient extends ApiClient {
  _DownloadExportApiClient() : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<Uint8List> downloadDocumentExport(DocumentExportJob job) async {
    return Uint8List.fromList('export ${job.jobId}'.codeUnits);
  }
}

class _FakePathProvider extends PathProviderPlatform {
  final String temporaryPath;

  _FakePathProvider(this.temporaryPath);

  @override
  Future<String?> getTemporaryPath() async => temporaryPath;
}

void _useFakeTemporaryPath(String path) {
  final previous = PathProviderPlatform.instance;
  PathProviderPlatform.instance = _FakePathProvider(path);
  addTearDown(() {
    PathProviderPlatform.instance = previous;
  });
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('document draft create and edit redact secrets before API', () async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final client = _RecordingDraftApiClient();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        apiClientProvider.overrideWithValue(client),
      ],
    );
    addTearDown(container.dispose);
    addTearDown(store.close);
    await container.read(documentsControllerProvider.future);

    await container.read(documentsControllerProvider.notifier).createDraft(
          title: 'incident token=raw-title-token',
          template: DocumentTemplate.report,
          content: 'password=raw-content-password\n'
              'Authorization: Bearer raw-content-bearer',
        );
    await container.read(documentsControllerProvider.notifier).saveDraftEdits(
          title: 'updated token=raw-edit-token',
          markdown: 'secret=raw-edit-secret\n'
              '-----BEGIN PRIVATE KEY-----\nraw-edit-key\n'
              '-----END PRIVATE KEY-----',
        );

    expect(client.created.single.title, contains('token=[REDACTED_SECRET]'));
    expect(
      client.created.single.content,
      contains('password=[REDACTED_SECRET]'),
    );
    expect(
      client.created.single.content,
      contains('Authorization: Bearer [REDACTED_TOKEN]'),
    );
    expect(client.updated.single.title, contains('token=[REDACTED_SECRET]'));
    expect(client.updated.single.markdown, contains('secret=[REDACTED_SECRET]'));
    expect(client.updated.single.markdown, contains('[REDACTED_PRIVATE_KEY]'));
    final outbound = '${client.created.single.title}\n'
        '${client.created.single.content}\n'
        '${client.updated.single.title}\n'
        '${client.updated.single.markdown}';
    expect(outbound, isNot(contains('raw-title-token')));
    expect(outbound, isNot(contains('raw-content-password')));
    expect(outbound, isNot(contains('raw-content-bearer')));
    expect(outbound, isNot(contains('raw-edit-token')));
    expect(outbound, isNot(contains('raw-edit-secret')));
    expect(outbound, isNot(contains('raw-edit-key')));
  });
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

  test('document upload retry rejects non-failed imports even with source path',
      () async {
    final container = ProviderContainer(
      overrides: [
        documentsControllerProvider.overrideWith(
          _QueuedUploadDocumentsController.new,
        ),
      ],
    );
    addTearDown(container.dispose);
    await container.read(documentsControllerProvider.future);

    await container
        .read(documentsControllerProvider.notifier)
        .retryLastUpload();

    final state = container.read(documentsControllerProvider);
    expect(state.hasError, isTrue);
    expect(state.error.toString(), contains('没有失败的导入任务可重试'));
  });

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

  test('validates mobile document import extensions', () {
    expect(validateMobileDocumentImportPath('/tmp/incident.PDF'), isNull);
    expect(
      validateMobileDocumentImportPath('/tmp/notice.docx?shared=true'),
      isNull,
    );
    expect(validateMobileDocumentImportPath('/tmp/table.csv'), isNull);
    expect(validateMobileDocumentImportPath('/tmp/photo.jpeg'), isNull);
    expect(
      validateMobileDocumentImportPath('/tmp/archive.zip'),
      contains('暂不支持该文件类型'),
    );
    expect(
      validateMobileDocumentImportPath('/tmp/README'),
      contains('暂不支持该文件类型'),
    );
  });

  test('shared document upload rejects unsupported files before uploading',
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
    final client = _RecordingUploadApiClient();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        apiClientProvider.overrideWithValue(client),
      ],
    );
    addTearDown(container.dispose);
    await container.read(documentsControllerProvider.future);

    await container
        .read(documentsControllerProvider.notifier)
        .uploadSharedDocument('/tmp/server-backup.zip');

    final state = container.read(documentsControllerProvider);
    expect(state.hasError, isTrue);
    expect(state.error.toString(), contains('暂不支持该文件类型'));
    expect(client.uploadedPaths, isEmpty);
  });

  test('shared document upload accepts supported emergency file types',
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
    final client = _RecordingUploadApiClient();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        apiClientProvider.overrideWithValue(client),
      ],
    );
    addTearDown(container.dispose);
    await container.read(documentsControllerProvider.future);

    for (final path in [
      '/tmp/incident.pdf',
      '/tmp/notice.docx',
      '/tmp/table.xlsx',
      '/tmp/export.csv',
      '/tmp/photo.jpg',
    ]) {
      await container
          .read(documentsControllerProvider.notifier)
          .uploadSharedDocument(path);
    }

    expect(client.uploadedPaths, [
      '/tmp/incident.pdf',
      '/tmp/notice.docx',
      '/tmp/table.xlsx',
      '/tmp/export.csv',
      '/tmp/photo.jpg',
    ]);
    expect(
      container.read(documentsControllerProvider).valueOrNull?.lastUploadPath,
      '/tmp/photo.jpg',
    );
    final cachedUpload = await store.loadLastDocumentUploadTask();
    final cachedUploadPath = await store.loadLastDocumentUploadPath();
    expect(cachedUpload?.taskId, 'upload-5');
    expect(cachedUpload?.filename, 'photo.jpg');
    expect(cachedUpload?.status, 'queued');
    expect(cachedUploadPath, '/tmp/photo.jpg');
  });

  test('document process completion caches draft and uses typed payload',
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
    await store.saveLastDocumentDraft(
      DocumentDraft(
        id: 'draft-process',
        title: 'incident summary',
        template: DocumentTemplate.report,
        markdown: '# incident summary',
        updatedAt: DateTime.utc(2026, 7, 2),
      ),
    );
    final notifications = _RecordingNotificationService();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        apiClientProvider.overrideWithValue(_ProcessDraftApiClient()),
      ],
    );
    addTearDown(container.dispose);
    await container.read(documentsControllerProvider.future);

    await container
        .read(documentsControllerProvider.notifier)
        .processDraft('summarize');

    final state = container.read(documentsControllerProvider).valueOrNull;
    final cachedDraft = await store.loadLastDocumentDraft();
    expect(state?.draft?.id, 'draft-process-processed');
    expect(cachedDraft?.id, 'draft-process-processed');
    expect(notifications.shown, hasLength(1));
    expect(notifications.shown.single.title, '文档处理完成');
    expect(
      notifications.shown.single.payload,
      'document-draft:draft-process-processed',
    );
  });

  test(
      'document upload waits for session before enforcing official mobile limit',
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
      'document-export:export-1',
    );
  });

  test('document export download uses phone-friendly safe filenames', () async {
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
    _useFakeTemporaryPath(cacheDir.path);
    addTearDown(store.close);
    await store.saveLastDocumentDraft(
      DocumentDraft(
        id: 'draft-long',
        title: '现场/应急:报告?需要 很 长 很 长 很 长 很 长 很 长 很 长 很 长 很 长 很 长 很 长 的标题<>',
        template: DocumentTemplate.report,
        markdown: '# report',
        updatedAt: DateTime.utc(2026, 7, 2),
      ),
    );
    final job = DocumentExportJob(
      jobId: 'export-long',
      draftId: 'draft-long',
      format: DocumentExportFormat.word,
      status: 'ready',
      downloadUrl: '/api/mobile/documents/exports/export-long/download',
      createdAt: DateTime.utc(2026, 7, 2),
    );
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        apiClientProvider.overrideWithValue(_DownloadExportApiClient()),
      ],
    );
    addTearDown(container.dispose);
    await container.read(documentsControllerProvider.future);

    final file = await container
        .read(documentsControllerProvider.notifier)
        .downloadExportFile(job);

    final filename = file.path.split(RegExp(r'[\\/]')).last;
    expect(filename, endsWith('.docx'));
    expect(filename.length, lessThanOrEqualTo(77));
    expect(filename, isNot(contains(RegExp(r'[\\/:*?"<>|]'))));
    expect(filename, isNot(contains('__')));
    expect(await file.readAsString(), 'export export-long');
  });

  test('document export download redacts sensitive title fragments in filename',
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
    _useFakeTemporaryPath(cacheDir.path);
    addTearDown(store.close);
    await store.saveLastDocumentDraft(
      DocumentDraft(
        id: 'draft-secret-title',
        title: 'incident token=raw-title-token password=raw-title-password',
        template: DocumentTemplate.report,
        markdown: '# report',
        updatedAt: DateTime.utc(2026, 7, 2),
      ),
    );
    final job = DocumentExportJob(
      jobId: 'export-secret-title',
      draftId: 'draft-secret-title',
      format: DocumentExportFormat.pdf,
      status: 'ready',
      downloadUrl: '/api/mobile/documents/exports/export-secret-title/download',
      createdAt: DateTime.utc(2026, 7, 2),
    );
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        apiClientProvider.overrideWithValue(_DownloadExportApiClient()),
      ],
    );
    addTearDown(container.dispose);
    await container.read(documentsControllerProvider.future);

    final file = await container
        .read(documentsControllerProvider.notifier)
        .downloadExportFile(job);

    final filename = file.path.split(RegExp(r'[\\/]')).last;
    expect(filename, endsWith('.pdf'));
    expect(filename, contains('token=[REDACTED_SECRET]'));
    expect(filename, contains('password=[REDACTED_SECRET]'));
    expect(filename, isNot(contains('raw-title-token')));
    expect(filename, isNot(contains('raw-title-password')));
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
    expect(
      notifications.shown.single.payload,
      'document-export:export-external',
    );
  });

  test('document failed export notifications redact sensitive messages',
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
    await store.saveLastDocumentDraft(
      DocumentDraft(
        id: 'draft-secret',
        title: 'incident token: draft-secret-token',
        template: DocumentTemplate.report,
        markdown: '# incident',
        updatedAt: DateTime.utc(2026, 7, 2),
      ),
    );
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
        'job_id': 'export-secret',
        'draft_id': 'draft-secret',
        'format': 'pdf',
        'status': 'failed',
        'message': 'export failed password=export-password',
        'created_at': '2026-07-02T00:00:00Z',
      },
    );
    await container
        .read(documentsControllerProvider.notifier)
        .applyRealtimeEvent(event);

    expect(notifications.shown, hasLength(1));
    expect(notifications.shown.single.title, '文档导出失败');
    expect(
      notifications.shown.single.body,
      contains('token=[REDACTED_SECRET]'),
    );
    expect(
      notifications.shown.single.body,
      contains('password=[REDACTED_SECRET]'),
    );
    expect(
      notifications.shown.single.body,
      isNot(contains('draft-secret-token')),
    );
    expect(
      notifications.shown.single.body,
      isNot(contains('export-password')),
    );
    expect(notifications.shown.single.payload, 'document-export:export-secret');
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
    expect(notifications.shown.single.payload, 'document-draft:draft-upload-1');
  });

  test('document realtime upload accepts top-level task id fallback', () async {
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

    final event = MobileRealtimeEvent.tryParse({
      'type': 'document_task',
      'task_id': 'upload-top-1',
      'status': 'ready',
      'payload': {
        'filename': 'incident.pdf',
        'draft_id': 'draft-upload-top-1',
        'draft': {
          'id': 'draft-upload-top-1',
          'title': 'field note',
          'template': 'statement',
          'markdown': '# field note',
          'updated_at': '2026-07-02T00:00:00Z',
        },
      },
    });

    await container
        .read(documentsControllerProvider.notifier)
        .applyRealtimeEvent(event!);

    final state = container.read(documentsControllerProvider).valueOrNull;
    final cachedUpload = await store.loadLastDocumentUploadTask();
    expect(state?.uploadTask?.taskId, 'upload-top-1');
    expect(cachedUpload?.taskId, 'upload-top-1');
    expect(notifications.shown, hasLength(1));
    expect(
      notifications.shown.single.payload,
      'document-draft:draft-upload-top-1',
    );
  });

  test('document failed upload notifications redact sensitive messages',
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
        'task_id': 'upload-secret',
        'filename': 'token=filename-secret.pdf',
        'status': 'failed',
        'message': 'ocr failed api_key=upload-api-key',
      },
    );
    await container
        .read(documentsControllerProvider.notifier)
        .applyRealtimeEvent(event);

    expect(notifications.shown, hasLength(1));
    expect(notifications.shown.single.title, '文档解析失败');
    expect(
      notifications.shown.single.body,
      contains('token=[REDACTED_SECRET]'),
    );
    expect(
      notifications.shown.single.body,
      contains('api_key=[REDACTED_SECRET]'),
    );
    expect(
      notifications.shown.single.body,
      isNot(contains('filename-secret')),
    );
    expect(
      notifications.shown.single.body,
      isNot(contains('upload-api-key')),
    );
    expect(notifications.shown.single.payload, 'document-upload:upload-secret');
  });

  test('document upload refresh completion caches draft and notifies once',
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
    await store.saveLastDocumentUploadTask(
      const MobileDocumentUploadTask(
        taskId: 'upload-refresh',
        filename: 'incident.pdf',
        status: 'in_progress',
      ),
      sourcePath: '/tmp/incident.pdf',
    );
    final notifications = _RecordingNotificationService();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        apiClientProvider.overrideWithValue(_UploadReadyApiClient()),
      ],
    );
    addTearDown(container.dispose);
    await container.read(documentsControllerProvider.future);

    await container
        .read(documentsControllerProvider.notifier)
        .refreshUploadTask();
    await container
        .read(documentsControllerProvider.notifier)
        .refreshUploadTask();

    final state = container.read(documentsControllerProvider).valueOrNull;
    final cachedDraft = await store.loadLastDocumentDraft();
    final cachedUpload = await store.loadLastDocumentUploadTask();
    expect(state?.draft?.id, 'draft-upload-refresh');
    expect(cachedDraft?.title, '现场说明');
    expect(cachedUpload?.status, 'ready');
    expect(notifications.shown, hasLength(1));
    expect(notifications.shown.single.title, '文档解析完成');
    expect(notifications.shown.single.body, contains('incident.pdf 已生成移动草稿'));
    expect(
      notifications.shown.single.payload,
      'document-draft:draft-upload-refresh',
    );
  });
}
