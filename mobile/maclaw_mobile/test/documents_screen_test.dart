import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/network/mobile_network_status.dart';
import 'package:maclaw_mobile/core/shared_intents/mobile_shared_intent.dart';
import 'package:maclaw_mobile/core/shared_intents/shared_intent_bootstrap.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';
import 'package:maclaw_mobile/features/documents/documents_controller.dart';
import 'package:maclaw_mobile/features/documents/documents_screen.dart';
import 'package:share_plus/share_plus.dart';

class _TestDocumentsController extends DocumentsController {
  @override
  Future<DocumentsState> build() async => const DocumentsState();
}

class _EmptyDocumentDraftHistoryController
    extends DocumentDraftHistoryController {
  @override
  Future<List<DocumentDraft>> build() async => const [];
}

class _InitialDocumentSharedIntentController
    extends MobileSharedIntentController {
  static final cleared = <String>[];

  @override
  MobileSharedIntent? build() => MobileSharedIntent(
        id: 'shared-file-1',
        kind: MobileSharedIntentKind.file,
        value: '/tmp/incident.pdf',
        mimeType: 'application/pdf',
        receivedAt: DateTime.utc(2026, 7, 2),
      );

  @override
  void clear(String id) {
    cleared.add(id);
    super.clear(id);
  }
}

class _RecordingSharedUploadDocumentsController extends DocumentsController {
  static final uploaded = <String>[];

  @override
  Future<DocumentsState> build() async => const DocumentsState();

  @override
  Future<void> uploadSharedDocument(String path) async {
    uploaded.add(path);
    state = AsyncData(
      DocumentsState(
        uploadTask: MobileDocumentUploadTask(
          taskId: 'upload-${uploaded.length}',
          filename: path.split('/').last,
          status: 'queued',
        ),
        lastUploadPath: path,
      ),
    );
  }
}

class _FailedExportDocumentsController extends DocumentsController {
  static var retryCount = 0;

  @override
  Future<DocumentsState> build() async => DocumentsState(
        draft: DocumentDraft(
          id: 'draft-1',
          title: '应急报告',
          template: DocumentTemplate.report,
          markdown: '# 应急报告',
          updatedAt: DateTime.utc(2026, 7, 1),
        ),
        exportJob: DocumentExportJob(
          jobId: 'export-1',
          draftId: 'draft-1',
          format: DocumentExportFormat.pdf,
          status: 'failed',
          downloadUrl: '',
          message: 'PDF 转换服务暂时不可用，请稍后重试或改用 Markdown。',
          createdAt: DateTime.utc(2026, 7, 1),
        ),
      );

  @override
  Future<void> retryLastExport() async {
    retryCount += 1;
  }
}

class _ReadyExportDocumentsController extends DocumentsController {
  static final downloadedJobs = <String>[];
  static String? exportedPath;

  @override
  Future<DocumentsState> build() async => DocumentsState(
        draft: DocumentDraft(
          id: 'draft-ready',
          title: '现场说明',
          template: DocumentTemplate.statement,
          markdown: '# 现场说明',
          updatedAt: DateTime.utc(2026, 7, 1),
        ),
        exportJob: DocumentExportJob(
          jobId: 'export-ready',
          draftId: 'draft-ready',
          format: DocumentExportFormat.markdown,
          status: 'ready',
          downloadUrl: '/api/mobile/documents/exports/export-ready/download',
          createdAt: DateTime.utc(2026, 7, 1),
        ),
      );

  @override
  Future<File> downloadExportFile(DocumentExportJob job) async {
    downloadedJobs.add(job.jobId);
    final directory = Directory(
      '${Directory.systemTemp.path}${Platform.pathSeparator}'
      'maclaw-mobile-export-test-${DateTime.now().microsecondsSinceEpoch}',
    );
    directory.createSync(recursive: true);
    final file = File('${directory.path}${Platform.pathSeparator}现场说明.md');
    exportedPath = file.path;
    file.writeAsStringSync('# 现场说明');
    return file;
  }
}

class _EditableDraftDocumentsController extends DocumentsController {
  static final saved = <({String title, String markdown})>[];

  @override
  Future<DocumentsState> build() async => DocumentsState(
        draft: DocumentDraft(
          id: 'draft-copy',
          title: '应急说明',
          template: DocumentTemplate.statement,
          markdown: '请现场负责人确认恢复时间。',
          updatedAt: DateTime.utc(2026, 7, 2),
        ),
      );

  @override
  Future<void> saveDraftEdits({
    required String title,
    required String markdown,
  }) async {
    saved.add((title: title, markdown: markdown));
    state = AsyncData(
      DocumentsState(
        draft: DocumentDraft(
          id: 'draft-copy',
          title: title,
          template: DocumentTemplate.statement,
          markdown: markdown,
          updatedAt: DateTime.utc(2026, 7, 2, 1),
        ),
      ),
    );
  }
}

class _RecordingExportDocumentsController extends DocumentsController {
  static final formats = <DocumentExportFormat>[];

  @override
  Future<DocumentsState> build() async => DocumentsState(
        draft: DocumentDraft(
          id: 'draft-export',
          title: '导出测试',
          template: DocumentTemplate.report,
          markdown: '# 导出测试',
          updatedAt: DateTime.utc(2026, 7, 2),
        ),
      );

  @override
  Future<void> exportDraft(DocumentExportFormat format) async {
    formats.add(format);
  }
}

class _RecordingProcessDocumentsController extends DocumentsController {
  static final actions = <String>[];

  @override
  Future<DocumentsState> build() async => DocumentsState(
        draft: DocumentDraft(
          id: 'draft-process',
          title: 'AI 处理测试',
          template: DocumentTemplate.report,
          markdown: '# AI 处理测试',
          updatedAt: DateTime.utc(2026, 7, 2),
        ),
      );

  @override
  Future<void> processDraft(String action) async {
    actions.add(action);
  }
}

class _RecordingCreateDraftDocumentsController extends DocumentsController {
  static final created =
      <({String title, DocumentTemplate template, String content})>[];

  @override
  Future<DocumentsState> build() async => const DocumentsState();

  @override
  Future<void> createDraft({
    required String title,
    required DocumentTemplate template,
    String content = '',
  }) async {
    created.add((title: title, template: template, content: content));
    state = AsyncData(
      DocumentsState(
        draft: DocumentDraft(
          id: 'draft-${created.length}',
          title: title,
          template: template,
          markdown: content.isEmpty ? '# $title' : content,
          updatedAt: DateTime.utc(2026, 7, 2),
        ),
      ),
    );
  }
}

class _HistoryDocumentsController extends DocumentsController {
  static final selected = <String>[];

  @override
  Future<DocumentsState> build() async => DocumentsState(
        draft: DocumentDraft(
          id: 'draft-current',
          title: '当前说明',
          template: DocumentTemplate.statement,
          markdown: '# 当前说明',
          updatedAt: DateTime.utc(2026, 7, 2),
        ),
      );

  @override
  Future<void> selectDraft(DocumentDraft draft) async {
    selected.add(draft.id);
    state = AsyncData(DocumentsState(draft: draft));
  }
}

class _HistoryDocumentDraftHistoryController
    extends DocumentDraftHistoryController {
  @override
  Future<List<DocumentDraft>> build() async => [
        DocumentDraft(
          id: 'draft-current',
          title: '当前说明',
          template: DocumentTemplate.statement,
          markdown: '# 当前说明',
          updatedAt: DateTime.utc(2026, 7, 2),
        ),
        DocumentDraft(
          id: 'draft-history',
          title: '现场处置邮件',
          template: DocumentTemplate.email,
          markdown: '请同步现场处置进展和预计恢复时间。',
          updatedAt: DateTime.utc(2026, 7, 1),
        ),
      ];
}

class _FailedUploadDocumentsController extends DocumentsController {
  static var retryCount = 0;

  @override
  Future<DocumentsState> build() async => const DocumentsState(
        uploadTask: MobileDocumentUploadTask(
          taskId: 'upload-1',
          filename: '现场照片.heic',
          status: 'failed',
          message: '官方服务暂不支持该图片编码，请转换为 JPG 后重试。',
        ),
        lastUploadPath: '/tmp/site.heic',
      );

  @override
  Future<void> retryLastUpload() async {
    retryCount += 1;
  }
}

class _QueuedUploadDocumentsController extends DocumentsController {
  static var refreshCount = 0;

  @override
  Future<DocumentsState> build() async => const DocumentsState(
        uploadTask: MobileDocumentUploadTask(
          taskId: 'upload-queued',
          filename: '现场通知.pdf',
          status: 'in_progress',
          message: '正在解析 PDF 并提取正文。',
        ),
        lastUploadPath: '/tmp/notice.pdf',
      );

  @override
  Future<void> refreshUploadTask({bool silent = false}) async {
    refreshCount += 1;
  }
}

void main() {
  testWidgets('documents screen uploads shared files automatically',
      (tester) async {
    _RecordingSharedUploadDocumentsController.uploaded.clear();
    _InitialDocumentSharedIntentController.cleared.clear();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileSharedIntentProvider.overrideWith(
            _InitialDocumentSharedIntentController.new,
          ),
          documentsControllerProvider.overrideWith(
            _RecordingSharedUploadDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: DocumentsScreen())),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(_RecordingSharedUploadDocumentsController.uploaded, [
      '/tmp/incident.pdf',
    ]);
    expect(_InitialDocumentSharedIntentController.cleared, ['shared-file-1']);
  });

  testWidgets('documents screen exposes mobile import actions', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _TestDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: DocumentsScreen())),
      ),
    );
    await tester.pump();

    expect(find.text('移动导入'), findsOneWidget);
    expect(find.text('文件导入'), findsOneWidget);
    expect(find.text('拍照导入'), findsOneWidget);
    expect(find.text('相册导入'), findsOneWidget);
    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pumpAndSettle();
    expect(find.text('AI 处理'), findsOneWidget);
  });

  testWidgets('documents screen creates drafts from every mobile template',
      (tester) async {
    _RecordingCreateDraftDocumentsController.created.clear();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _RecordingCreateDraftDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: DocumentsScreen())),
      ),
    );
    await tester.pump();

    await tester.enterText(
      find.widgetWithText(TextField, '标题'),
      '值班应急材料',
    );
    await tester.enterText(
      find.widgetWithText(TextField, '要点或原始内容'),
      '1. 服务异常\n2. 需要 30 分钟内同步处置进展',
    );

    for (final template in DocumentTemplate.values) {
      await tester.tap(find.byType(DropdownButtonFormField<DocumentTemplate>));
      await tester.pumpAndSettle();
      await tester.tap(find.text(documentTemplateLabel(template)).last);
      await tester.pumpAndSettle();
      await tester.tap(find.text('生成草稿'));
      await tester.pump();
    }

    expect(
      _RecordingCreateDraftDocumentsController.created
          .map((item) => item.template),
      DocumentTemplate.values,
    );
    for (final created in _RecordingCreateDraftDocumentsController.created) {
      expect(created.title, '值班应急材料');
      expect(created.content, contains('服务异常'));
      expect(created.content, contains('30 分钟'));
    }
  });

  testWidgets('documents screen explains failed export reason', (tester) async {
    _FailedExportDocumentsController.retryCount = 0;
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _FailedExportDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: DocumentsScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -1100));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('导出任务 export-1：导出失败'), findsOneWidget);
    expect(
      find.text('PDF 转换服务暂时不可用，请稍后重试或改用 Markdown。'),
      findsOneWidget,
    );
    expect(find.text('重试导出'), findsOneWidget);
    await tester.tap(find.byIcon(Icons.replay_outlined));
    await tester.pump();
    expect(_FailedExportDocumentsController.retryCount, 1);
  });

  testWidgets('documents screen explains ready export sharing', (tester) async {
    _ReadyExportDocumentsController.downloadedJobs.clear();
    _ReadyExportDocumentsController.exportedPath = null;
    final sharedFiles = <String>[];
    String? sharedText;
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _ReadyExportDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          documentsExportFileShareProvider.overrideWithValue(
            (files, {text}) async {
              sharedFiles.addAll(files.map((file) => file.path));
              sharedText = text;
              return ShareResult.unavailable;
            },
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: DocumentsScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -1100));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('导出任务 export-ready：已可分享'), findsOneWidget);
    expect(
      find.text('文件已生成，可直接调起系统分享；分享前会先下载到本机临时目录。'),
      findsOneWidget,
    );
    expect(find.text('分享文件'), findsOneWidget);

    await tester.tap(find.text('分享文件'));
    await tester.runAsync(() async {
      await Future<void>.delayed(const Duration(milliseconds: 300));
    });
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(_ReadyExportDocumentsController.downloadedJobs, ['export-ready']);
    expect(sharedFiles, [_ReadyExportDocumentsController.exportedPath]);
    expect(sharedText, '现场说明');
  });

  testWidgets('documents screen can copy and share draft text quickly',
      (tester) async {
    String? clipboardText;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.setData') {
        final data = Map<String, dynamic>.from(call.arguments as Map);
        clipboardText = data['text'] as String?;
        return null;
      }
      if (call.method == 'Clipboard.getData') {
        return {'text': clipboardText};
      }
      return null;
    });
    addTearDown(
      () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, null),
    );

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _EditableDraftDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: DocumentsScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('复制草稿'), findsOneWidget);
    expect(find.text('分享文本'), findsOneWidget);

    await tester.tap(find.text('复制草稿'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    final clipboard = await Clipboard.getData(Clipboard.kTextPlain);
    expect(clipboard?.text, contains('应急说明'));
    expect(clipboard?.text, contains('请现场负责人确认恢复时间。'));
  });

  testWidgets('documents screen exports PDF Word and Markdown formats',
      (tester) async {
    _RecordingExportDocumentsController.formats.clear();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _RecordingExportDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: DocumentsScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pump(const Duration(milliseconds: 300));

    for (final label in ['PDF', 'Word', 'Markdown']) {
      await tester.ensureVisible(find.text(label));
      await tester.tap(find.text(label));
      await tester.pump();
    }

    expect(_RecordingExportDocumentsController.formats, [
      DocumentExportFormat.pdf,
      DocumentExportFormat.word,
      DocumentExportFormat.markdown,
    ]);
  });

  testWidgets('documents screen can restore recent draft history',
      (tester) async {
    _HistoryDocumentsController.selected.clear();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _HistoryDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _HistoryDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: DocumentsScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('最近文档草稿'), findsOneWidget);
    expect(find.text('现场处置邮件'), findsOneWidget);
    expect(find.textContaining('邮件 · 请同步现场处置进展'), findsOneWidget);

    await tester.tap(find.text('现场处置邮件'));
    await tester.pump();

    expect(_HistoryDocumentsController.selected, ['draft-history']);
  });

  testWidgets('documents screen runs all mobile AI document actions',
      (tester) async {
    _RecordingProcessDocumentsController.actions.clear();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _RecordingProcessDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: DocumentsScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -1600));
    await tester.pump(const Duration(milliseconds: 300));

    for (final label in ['摘要', '翻译', '改写', '扩写', '润色', '整理']) {
      await tester.ensureVisible(find.text(label));
      await tester.tap(find.text(label));
      await tester.pump();
    }

    expect(_RecordingProcessDocumentsController.actions, [
      'summarize',
      'translate',
      'rewrite',
      'expand',
      'polish',
      'format',
    ]);
  });

  testWidgets('documents screen inserts table and comment snippets before save',
      (tester) async {
    _EditableDraftDocumentsController.saved.clear();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _EditableDraftDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: DocumentsScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pump(const Duration(milliseconds: 300));

    final insertTable = find.byIcon(Icons.table_chart_outlined);
    final insertComment = find.byIcon(Icons.comment_outlined);
    await tester.ensureVisible(insertTable);
    await tester.tap(insertTable);
    await tester.pump();
    await tester.tap(insertComment);
    await tester.pump();
    await tester.tap(find.byIcon(Icons.save_outlined));
    await tester.pump();

    expect(_EditableDraftDocumentsController.saved, hasLength(1));
    final saved = _EditableDraftDocumentsController.saved.single;
    expect(saved.markdown, contains('|'));
    expect(saved.markdown, contains('---'));
    expect(saved.markdown, contains('批注'));
  });

  testWidgets('documents screen explains long running import tasks',
      (tester) async {
    _QueuedUploadDocumentsController.refreshCount = 0;
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _QueuedUploadDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: DocumentsScreen()),
      ),
    );
    await tester.pump();
    await tester.drag(find.byType(ListView), const Offset(0, -500));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('导入任务 upload-queued：远程解析中'), findsOneWidget);
    expect(find.text('正在解析 PDF 并提取正文。'), findsOneWidget);
    expect(
      find.text('这是官方服务长任务，可以先离开页面；完成后会通过通知或回到文档页继续处理。'),
      findsOneWidget,
    );
    expect(find.text('刷新状态'), findsOneWidget);
    await tester.tap(find.byIcon(Icons.refresh));
    await tester.pump();
    expect(_QueuedUploadDocumentsController.refreshCount, 1);
  });

  testWidgets('documents screen gives failed import retry guidance',
      (tester) async {
    _FailedUploadDocumentsController.retryCount = 0;
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          documentsControllerProvider.overrideWith(
            _FailedUploadDocumentsController.new,
          ),
          documentDraftHistoryProvider.overrideWith(
            _EmptyDocumentDraftHistoryController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: DocumentsScreen()),
      ),
    );
    await tester.pump();
    await tester.drag(find.byType(ListView), const Offset(0, -500));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('导入任务 upload-1：解析失败'), findsOneWidget);
    expect(
      find.text('官方服务暂不支持该图片编码，请转换为 JPG 后重试。'),
      findsOneWidget,
    );
    expect(
      find.text('可重试导入，或改用文本、PDF、Word、图片截图等移动端更稳定的格式。'),
      findsOneWidget,
    );
    expect(find.text('重试导入'), findsOneWidget);
    await tester.tap(find.byIcon(Icons.replay_outlined));
    await tester.pump();
    expect(_FailedUploadDocumentsController.retryCount, 1);
  });
}
