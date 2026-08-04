import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter/services.dart';
import 'package:image_picker/image_picker.dart';
import 'package:share_plus/share_plus.dart';

import '../../core/security/mobile_redaction.dart';
import '../../core/shared_intents/shared_intent_bootstrap.dart';
import '../../l10n/app_strings.dart';
import '../../shared/surface.dart';
import 'document_draft.dart';
import 'document_original_image.dart';
import 'documents_controller.dart';
import '../tasks/mobile_jobs_provider.dart';

typedef DocumentsExportFileShare = Future<ShareResult> Function(
  List<XFile> files, {
  String? text,
  String? subject,
});

typedef DocumentsDraftTextShare = Future<void> Function(
  String text, {
  String? subject,
});

final documentsExportFileShareProvider = Provider<DocumentsExportFileShare>(
  (ref) => Share.shareXFiles,
);

final documentsDraftTextShareProvider = Provider<DocumentsDraftTextShare>(
  (ref) => Share.share,
);

class DocumentsScreen extends ConsumerStatefulWidget {
  const DocumentsScreen({super.key});

  @override
  ConsumerState<DocumentsScreen> createState() => _DocumentsScreenState();
}

class _DocumentsScreenState extends ConsumerState<DocumentsScreen> {
  String? _handledSharedIntentId;
  bool _deleteInFlight = false;

  Future<void> _shareDraft(DocumentDraft draft) async {
    final s = ref.read(appStringsProvider);
    final subject =
        draft.title.trim().isEmpty ? s.unnamedDocument : draft.title;
    try {
      // Prefer original file (docx/pdf/image) for WeChat etc.
      final file = await ref
          .read(documentsControllerProvider.notifier)
          .prepareOriginalShareFile(draft);
      if (file != null) {
        final name = draft.sourceFilename.trim().isEmpty
            ? file.uri.pathSegments.last
            : draft.sourceFilename.trim();
        await ref.read(documentsExportFileShareProvider).call(
          [XFile(file.path, name: name)],
          subject: subject,
        );
      } else {
        final text = ref
            .read(documentsControllerProvider.notifier)
            .shareTextForDraft(draft);
        await ref.read(documentsDraftTextShareProvider).call(
              text,
              subject: subject,
            );
      }
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(s.shareOpened)),
      );
    } on Object catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(s.shareFailed(e))),
      );
    }
  }

  Future<void> _deleteDraft(DocumentDraft draft) async {
    // setState is asynchronous; retain a synchronous guard so a rapid second
    // tap cannot open another confirmation or send another delete request.
    if (_deleteInFlight) return;
    _deleteInFlight = true;
    final s = ref.read(appStringsProvider);
    final title =
        draft.title.trim().isEmpty ? s.unnamedDocument : draft.title.trim();
    try {
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: Text(
            draft.isManagedMeetingResult
                ? s.deleteMeetingResultTitle
                : s.deleteDocumentTitle,
          ),
          content: Text(
            draft.isManagedMeetingResult
                ? s.deleteMeetingResultConfirm(title)
                : s.deleteDocumentConfirm(title),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(ctx).pop(false),
              child: Text(s.cancel),
            ),
            FilledButton(
              onPressed: () => Navigator.of(ctx).pop(true),
              child: Text(
                draft.isManagedMeetingResult ? s.deleteAll : s.delete,
              ),
            ),
          ],
        ),
      );
      if (confirmed != true || !mounted) return;
      try {
        await ref
            .read(documentDraftHistoryProvider.notifier)
            .deleteDraft(draft);
        if (!mounted) return;
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              draft.isManagedMeetingResult
                  ? s.deletedMeetingResult(title)
                  : s.deletedDocument(title),
            ),
          ),
        );
      } on Object catch (e) {
        if (!mounted) return;
        final msg = e is StateError ? e.message : e.toString();
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(s.deleteFailed(msg))),
        );
      }
    } finally {
      _deleteInFlight = false;
    }
  }

  void _consumeSharedIntent(AsyncValue<DocumentsState> documentsState) {
    if (documentsState.isLoading) return;
    final shared = ref.watch(mobileSharedIntentProvider);
    if (shared == null ||
        !shared.opensDocuments ||
        shared.id == _handledSharedIntentId) {
      return;
    }
    _handledSharedIntentId = shared.id;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      ref.read(documentsControllerProvider.notifier).uploadSharedDocument(
            shared.value,
          );
      ref.read(mobileSharedIntentProvider.notifier).clear(shared.id);
    });
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(documentsControllerProvider);
    final s = ref.watch(appStringsProvider);
    _consumeSharedIntent(state);
    return ScreenScaffold(
      title: s.documentsTitle,
      subtitle: s.documentsSubtitle,
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          IconButton.filledTonal(
            tooltip: s.refreshLibrary,
            onPressed: () =>
                ref.read(documentDraftHistoryProvider.notifier).refresh(),
            icon: const Icon(Icons.refresh),
          ),
          IconButton.filledTonal(
            tooltip: s.importDocument,
            onPressed: () => ref
                .read(documentsControllerProvider.notifier)
                .pickAndUploadDocument(),
            icon: const Icon(Icons.upload_file_outlined),
          ),
        ],
      ),
      children: [
        StatusBanner(
          tone: StatusTone.info,
          icon: Icons.devices_outlined,
          message: s.documentsLibraryHint,
        ),
        const SizedBox(height: 12),
        _HubDocumentLibraryCard(
          currentDraftId: state.valueOrNull?.draft?.id,
          onOpen: (draft) async {
            await ref
                .read(documentsControllerProvider.notifier)
                .selectDraft(draft);
          },
          onShare: (draft) => _shareDraft(draft),
          onDelete: (draft) => _deleteDraft(draft),
        ),
        const SizedBox(height: 12),
        const _MobileDocumentImportPanel(),
        const SizedBox(height: 12),
        state.when(
          data: (value) => Column(
            children: [
              if (value.operationError != null) ...[
                Card(
                  color: Theme.of(context).colorScheme.errorContainer,
                  child: ListTile(
                    leading: Icon(
                      Icons.error_outline,
                      color: Theme.of(context).colorScheme.onErrorContainer,
                    ),
                    title: Text(
                      s.operationIncomplete,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.onErrorContainer,
                      ),
                    ),
                    subtitle: Text(
                      value.operationError!,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.onErrorContainer,
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: 12),
              ],
              _UploadStatus(state: value),
              if (value.uploadTask != null && value.draft != null)
                const SizedBox(height: 12),
              _DocumentReadonlyPreview(state: value),
              if (value.draft != null) ...[
                const SizedBox(height: 12),
                _ActiveDocumentActions(
                  draft: value.draft!,
                  onShare: () => _shareDraft(value.draft!),
                  onDelete: () => _deleteDraft(value.draft!),
                ),
              ],
            ],
          ),
          error: (error, _) {
            final msg = error is StateError
                ? error.message
                : error.toString().contains('DioException')
                    ? s.documentServiceError
                    : error.toString();
            return Card(
              color: Theme.of(context).colorScheme.errorContainer,
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      s.operationIncomplete,
                      style: Theme.of(context).textTheme.titleSmall?.copyWith(
                            color:
                                Theme.of(context).colorScheme.onErrorContainer,
                          ),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      msg,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.onErrorContainer,
                      ),
                    ),
                    const SizedBox(height: 12),
                    TextButton(
                      onPressed: () {
                        ref.invalidate(documentsControllerProvider);
                      },
                      child: Text(s.retryRefresh),
                    ),
                  ],
                ),
              ),
            );
          },
          loading: () => LoadingCard(label: s.processingDocument),
        ),
        const SizedBox(height: 12),
        const _DocumentAIProcessPanel(),
      ],
    );
  }
}

class _MobileDocumentImportPanel extends ConsumerWidget {
  const _MobileDocumentImportPanel();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final loading = ref.watch(documentsControllerProvider).isLoading;
    final controller = ref.read(documentsControllerProvider.notifier);
    final quota = ref.watch(documentQuotaProvider).valueOrNull;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.add_photo_alternate_outlined,
                  color: Theme.of(context).colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Text(s.mobileImport,
                    style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              s.isZh
                  ? '支持任意格式；自动压缩后单文件不超过 100 MB。压缩包及 DOCX/XLSX/PPTX 不重复压缩。'
                  : 'Any file type; maximum 100 MB after automatic compression. Archives and DOCX/XLSX/PPTX are not recompressed.',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
            ),
            if (quota != null) ...[
              const SizedBox(height: 10),
              Semantics(
                label: s.isZh ? '文稿库存储空间' : 'Document storage usage',
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Wrap(
                      spacing: 12,
                      runSpacing: 4,
                      children: [
                        Text(s.isZh
                            ? '已用 ${formatMobileFileSize(quota.documentQuotaUsedBytes)}'
                            : '${formatMobileFileSize(quota.documentQuotaUsedBytes)} used'),
                        Text(s.isZh
                            ? '剩余 ${formatMobileFileSize(quota.documentQuotaRemaining)}'
                            : '${formatMobileFileSize(quota.documentQuotaRemaining)} remaining'),
                        Text(s.isZh
                            ? '总限额 ${formatMobileFileSize(quota.documentQuotaBytes)}'
                            : '${formatMobileFileSize(quota.documentQuotaBytes)} total'),
                      ],
                    ),
                    const SizedBox(height: 6),
                    LinearProgressIndicator(
                      value: quota.documentQuotaBytes <= 0
                          ? 0
                          : (quota.documentQuotaUsedBytes /
                                  quota.documentQuotaBytes)
                              .clamp(0, 1)
                              .toDouble(),
                    ),
                  ],
                ),
              ),
            ],
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                OutlinedButton.icon(
                  onPressed:
                      loading ? null : () => controller.pickAndUploadDocument(),
                  icon: const Icon(Icons.upload_file_outlined),
                  label: Text(s.importFromFile),
                ),
                OutlinedButton.icon(
                  onPressed: loading
                      ? null
                      : () => controller.pickImageAndUploadDocument(
                            ImageSource.camera,
                          ),
                  icon: const Icon(Icons.photo_camera_outlined),
                  label: Text(s.importFromCamera),
                ),
                OutlinedButton.icon(
                  onPressed: loading
                      ? null
                      : () => controller.pickImageAndUploadDocument(
                            ImageSource.gallery,
                          ),
                  icon: const Icon(Icons.photo_library_outlined),
                  label: Text(s.importFromGallery),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _HubDocumentLibraryCard extends ConsumerWidget {
  final String? currentDraftId;
  final Future<void> Function(DocumentDraft draft) onOpen;
  final Future<void> Function(DocumentDraft draft) onShare;
  final Future<void> Function(DocumentDraft draft) onDelete;

  const _HubDocumentLibraryCard({
    required this.currentDraftId,
    required this.onOpen,
    required this.onShare,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final history = ref.watch(documentDraftHistoryProvider);
    final s = ref.watch(appStringsProvider);
    return history.when(
      data: (drafts) {
        final shown = drafts.take(30).toList();
        return Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Icon(
                      Icons.cloud_done_outlined,
                      color: Theme.of(context).colorScheme.primary,
                      size: 20,
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        s.hubLibrary,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                    ),
                    Text(
                      '${shown.length}',
                      style: Theme.of(context).textTheme.labelMedium?.copyWith(
                            color:
                                Theme.of(context).colorScheme.onSurfaceVariant,
                          ),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                if (shown.isEmpty)
                  Text(
                    s.hubLibraryEmpty,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                  )
                else
                  for (final draft in shown)
                    ListTile(
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      leading: Stack(
                        alignment: Alignment.bottomRight,
                        children: [
                          DocumentOriginalImageThumb(draft: draft),
                          if (draft.id == currentDraftId)
                            Icon(
                              Icons.check_circle,
                              size: 14,
                              color: Theme.of(context).colorScheme.primary,
                            ),
                        ],
                      ),
                      title: Text(
                        draft.title.trim().isEmpty
                            ? s.unnamedDocument
                            : draft.title,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      subtitle: Text(
                        '${documentTemplateLabel(draft.template, isZh: s.isZh)} · ${_draftPreviewText(draft.markdown, emptyLabel: s.blankDraft)}',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          IconButton(
                            tooltip: s.share,
                            icon:
                                const Icon(Icons.ios_share_outlined, size: 20),
                            onPressed: () => onShare(draft),
                          ),
                          IconButton(
                            tooltip: s.delete,
                            icon: Icon(
                              Icons.delete_outline,
                              size: 20,
                              color: Theme.of(context).colorScheme.error,
                            ),
                            onPressed: () => onDelete(draft),
                          ),
                        ],
                      ),
                      onTap: () => onOpen(draft),
                    ),
              ],
            ),
          ),
        );
      },
      error: (e, _) => Card(
        child: ListTile(
          leading: const Icon(Icons.cloud_off_outlined),
          title: Text(s.hubLibraryUnavailable),
          subtitle: Text('$e'),
          trailing: TextButton(
            onPressed: () =>
                ref.read(documentDraftHistoryProvider.notifier).refresh(),
            child: Text(s.retry),
          ),
        ),
      ),
      loading: () => LoadingCard(label: s.hubLibraryLoading),
    );
  }
}

class _ActiveDocumentActions extends ConsumerWidget {
  final DocumentDraft draft;
  final VoidCallback onShare;
  final VoidCallback onDelete;

  const _ActiveDocumentActions({
    required this.draft,
    required this.onShare,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final loading = ref.watch(documentsControllerProvider).isLoading;
    final controller = ref.read(documentsControllerProvider.notifier);
    final title = draft.title.trim().isEmpty ? draft.id : draft.title;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              s.continueProcessingFor(title),
              style: Theme.of(context).textTheme.titleSmall,
            ),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton.tonalIcon(
                  onPressed: loading
                      ? null
                      : () => controller.processDraft('summarize'),
                  icon: const Icon(Icons.auto_awesome, size: 18),
                  label: Text(s.summarize),
                ),
                FilledButton.tonalIcon(
                  onPressed:
                      loading ? null : () => controller.processDraft('polish'),
                  icon: const Icon(Icons.spellcheck, size: 18),
                  label: Text(s.polish),
                ),
                FilledButton.tonalIcon(
                  onPressed: loading
                      ? null
                      : () => controller.exportDraft(
                            DocumentExportFormat.markdown,
                          ),
                  icon: const Icon(Icons.file_download_outlined, size: 18),
                  label: Text(s.export),
                ),
                OutlinedButton.icon(
                  onPressed: loading ? null : onDelete,
                  icon: Icon(
                    Icons.delete_outline,
                    size: 18,
                    color: Theme.of(context).colorScheme.error,
                  ),
                  label: Text(
                    s.delete,
                    style:
                        TextStyle(color: Theme.of(context).colorScheme.error),
                  ),
                ),
                FilledButton.icon(
                  onPressed: loading ? null : onShare,
                  icon: const Icon(Icons.ios_share, size: 18),
                  label: Text(
                    draft.hasOriginal ? s.shareOriginal : s.shareToWechat,
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

String _draftPreviewText(String markdown, {String emptyLabel = 'Empty draft'}) {
  final compact = markdown.replaceAll(RegExp(r'\s+'), ' ').trim();
  if (compact.isEmpty) return emptyLabel;
  return compact.length > 32 ? '${compact.substring(0, 32)}...' : compact;
}

String emergencyDocumentTemplateSkeleton(DocumentTemplate template) {
  return switch (template) {
    DocumentTemplate.notice => '''
## 通知事项

- 事项：
- 对象：
- 时间：
- 地点/范围：

## 处理要求

1. 
2. 

## 联系方式

- 联系人：
- 电话/群组：
'''
        .trim(),
    DocumentTemplate.report => '''
## 背景

- 事件/任务：
- 当前状态：

## 关键事实

- 
- 

## 影响与风险

- 

## 处理建议

1. 
2. 

## 来源或附件

- 
'''
        .trim(),
    DocumentTemplate.email => '''
收件人：
抄送：
主题：

您好，

## 事项说明


## 需要确认/处理

1. 
2. 

谢谢。
'''
        .trim(),
    DocumentTemplate.proposal => '''
## 目标


## 当前约束

- 时间：
- 人员/资源：
- 风险：

## 建议方案

1. 
2. 

## 下一步

- 
'''
        .trim(),
    DocumentTemplate.meetingMinutes => '''
## 会议信息

- 时间：
- 参会人：
- 主题：

## 讨论要点

- 
- 

## 决议

- 

## 待办

| 事项 | 负责人 | 截止时间 |
| --- | --- | --- |
|  |  |  |
'''
        .trim(),
    DocumentTemplate.statement => '''
## 说明对象


## 事实经过

1. 
2. 

## 当前状态


## 补充说明

- 
'''
        .trim(),
  };
}

/// Read-only preview of the selected Hub document (no phone-side editing).
class _DocumentReadonlyPreview extends ConsumerStatefulWidget {
  final DocumentsState state;

  const _DocumentReadonlyPreview({required this.state});

  @override
  ConsumerState<_DocumentReadonlyPreview> createState() =>
      _DocumentReadonlyPreviewState();
}

class _DocumentReadonlyPreviewState
    extends ConsumerState<_DocumentReadonlyPreview> {
  String? _sharingExportJobId;

  Future<void> _shareExport(DocumentExportJob job, DocumentDraft draft) async {
    final s = ref.read(appStringsProvider);
    setState(() => _sharingExportJobId = job.jobId);
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(s.preparingExport)),
    );
    try {
      final file = await ref
          .read(documentsControllerProvider.notifier)
          .downloadExportFile(job);
      final result = await ref.read(documentsExportFileShareProvider)(
        [XFile(file.path)],
        text: redactMobileSensitiveText(draft.title),
      );
      if (!mounted) return;
      final message = switch (result.status) {
        ShareResultStatus.success => s.exportShareSuccess,
        ShareResultStatus.dismissed => s.exportShareDismissed,
        ShareResultStatus.unavailable => s.exportShareUnavailable(file.path),
      };
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(message)),
      );
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(s.exportShareFailed(error))),
      );
    } finally {
      if (mounted) {
        setState(() => _sharingExportJobId = null);
      }
    }
  }

  String _draftShareText(DocumentDraft draft) {
    final title = draft.title.trim();
    final markdown = draft.markdown.trim();
    if (title.isEmpty) return markdown;
    if (markdown.isEmpty) return title;
    return '# $title\n\n$markdown';
  }

  Future<void> _copyDraftText(DocumentDraft draft) async {
    final s = ref.read(appStringsProvider);
    final text = _draftShareText(draft);
    if (text.trim().isEmpty) return;
    await Clipboard.setData(ClipboardData(text: text));
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(s.documentTextCopied)),
    );
  }

  Future<void> _shareDraftText(DocumentDraft draft) async {
    final s = ref.read(appStringsProvider);
    final text = _draftShareText(draft);
    if (text.trim().isEmpty) return;
    await ref.read(documentsDraftTextShareProvider)(
      text,
      subject: draft.title.trim(),
    );
    if (!mounted) return;
    ScaffoldMessenger.of(context)
      ..clearSnackBars()
      ..showSnackBar(
        SnackBar(content: Text(s.documentTextShared)),
      );
  }

  Future<void> _shareOriginalFile(DocumentDraft draft) async {
    final s = ref.read(appStringsProvider);
    final subject =
        draft.title.trim().isEmpty ? s.unnamedDocument : draft.title;
    try {
      final file = await ref
          .read(documentsControllerProvider.notifier)
          .prepareOriginalShareFile(draft);
      if (file == null) {
        if (!mounted) return;
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(s.noOriginalToShare)),
        );
        return;
      }
      final name = draft.sourceFilename.trim().isEmpty
          ? file.uri.pathSegments.last
          : draft.sourceFilename.trim();
      await ref.read(documentsExportFileShareProvider).call(
        [XFile(file.path, name: name)],
        subject: subject,
      );
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(s.shareOpened)),
      );
    } on Object catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(s.shareOriginalFailed(e))),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.state;
    final draft = state.draft;
    if (draft == null) return const SizedBox.shrink();
    final scheme = Theme.of(context).colorScheme;
    final s = ref.watch(appStringsProvider);
    final body = draft.markdown.trim().isEmpty
        ? (draft.hasOriginal
            ? (s.isZh ? '不支持内容预览' : 'Content preview is not supported')
            : s.documentBodyEmpty)
        : draft.markdown;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(s.documentPreview,
                style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 4),
            Text(
              s.documentPreviewHint,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 8),
            Text(
              documentTemplateLabel(draft.template, isZh: s.isZh),
              style: Theme.of(context).textTheme.labelMedium?.copyWith(
                    color: scheme.primary,
                  ),
            ),
            const SizedBox(height: 8),
            Text(
              draft.title.trim().isEmpty ? s.unnamedDocument : draft.title,
              style: Theme.of(context).textTheme.titleSmall?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
            ),
            if (draft.hasOriginal) ...[
              const SizedBox(height: 6),
              Text(
                s.documentOriginalLabel(
                  draft.sourceFilename.isEmpty
                      ? draft.id
                      : draft.sourceFilename,
                  draft.sourceSize > 0
                      ? ' · ${(draft.sourceSize / 1024).ceil()} KB'
                      : '',
                ),
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                      color: scheme.tertiary,
                    ),
              ),
            ],
            if (draft.isImageOriginal) ...[
              const SizedBox(height: 12),
              DocumentOriginalImagePreview(draft: draft),
            ],
            const SizedBox(height: 12),
            ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: draft.isImageOriginal ? 160 : 280,
              ),
              child: SingleChildScrollView(
                child: SelectableText(
                  body,
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                        height: 1.4,
                      ),
                ),
              ),
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                OutlinedButton.icon(
                  onPressed: () => _copyDraftText(draft),
                  icon: const Icon(Icons.content_copy_outlined),
                  label: Text(s.copyText),
                ),
                OutlinedButton.icon(
                  onPressed: () => _shareDraftText(draft),
                  icon: const Icon(Icons.ios_share_outlined),
                  label: Text(s.shareText),
                ),
                if (draft.hasOriginal)
                  FilledButton.tonalIcon(
                    onPressed: () => _shareOriginalFile(draft),
                    icon: const Icon(Icons.attach_file_outlined),
                    label: Text(s.shareOriginal),
                  ),
                const _ExportButton(
                  format: DocumentExportFormat.pdf,
                  label: 'PDF',
                ),
                const _ExportButton(
                  format: DocumentExportFormat.word,
                  label: 'Word',
                ),
                const _ExportButton(
                  format: DocumentExportFormat.markdown,
                  label: 'Markdown',
                ),
              ],
            ),
            if (state.exportJob != null) ...[
              const SizedBox(height: 10),
              Text(
                s.exportJobStatus(
                  state.exportJob!.jobId,
                  s.exportStatusLabel(state.exportJob!.status),
                ),
                style: Theme.of(context).textTheme.bodySmall,
              ),
              if (state.exportJob!.message.isNotEmpty) ...[
                const SizedBox(height: 4),
                Text(
                  state.exportJob!.message,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: state.exportJob!.status == 'failed'
                            ? Theme.of(context).colorScheme.error
                            : scheme.onSurfaceVariant,
                      ),
                ),
              ],
              if (state.exportJob!.status == 'ready') ...[
                const SizedBox(height: 4),
                Text(
                  s.exportReadyHint,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                ),
              ],
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  OutlinedButton.icon(
                    onPressed: () => ref
                        .read(documentsControllerProvider.notifier)
                        .refreshExportJob(),
                    icon: const Icon(Icons.refresh),
                    label: Text(s.refreshStatus),
                  ),
                  if (state.canRetryLastExport)
                    OutlinedButton.icon(
                      onPressed: () => ref
                          .read(documentsControllerProvider.notifier)
                          .retryLastExport(),
                      icon: const Icon(Icons.replay_outlined),
                      label: Text(s.retryExport),
                    ),
                  if (state.exportJob!.downloadUrl.isNotEmpty)
                    OutlinedButton.icon(
                      onPressed: state.exportJob!.status == 'ready' &&
                              _sharingExportJobId != state.exportJob!.jobId
                          ? () => _shareExport(state.exportJob!, draft)
                          : null,
                      icon: _sharingExportJobId == state.exportJob!.jobId
                          ? const SizedBox(
                              width: 18,
                              height: 18,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : const Icon(Icons.ios_share_outlined),
                      label: Text(
                        _sharingExportJobId == state.exportJob!.jobId
                            ? s.preparingShare
                            : s.shareFile,
                      ),
                    ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _DocumentAIProcessPanel extends ConsumerWidget {
  const _DocumentAIProcessPanel();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final state = ref.watch(documentsControllerProvider);
    final hasDraft = state.valueOrNull?.draft != null;
    final loading = state.isLoading;
    final actions = [
      _DocumentProcessAction(
          'summarize', s.processSummarizeShort, Icons.summarize_outlined),
      _DocumentProcessAction(
          'translate', s.translate, Icons.translate_outlined),
      _DocumentProcessAction('rewrite', s.rewrite, Icons.edit_note_outlined),
      _DocumentProcessAction('expand', s.expand, Icons.unfold_more_outlined),
      _DocumentProcessAction('polish', s.polish, Icons.auto_fix_high_outlined),
      _DocumentProcessAction('format', s.formatDoc, Icons.format_list_bulleted),
    ];
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.auto_fix_high_outlined,
                  color: Theme.of(context).colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Text(s.aiProcess,
                    style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              s.aiProcessHint,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final action in actions)
                  OutlinedButton.icon(
                    onPressed: !hasDraft || loading
                        ? null
                        : () => ref
                            .read(documentsControllerProvider.notifier)
                            .processDraft(action.id),
                    icon: Icon(action.icon),
                    label: Text(action.label),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _DocumentProcessAction {
  final String id;
  final String label;
  final IconData icon;

  const _DocumentProcessAction(this.id, this.label, this.icon);
}

class _UploadStatus extends ConsumerWidget {
  final DocumentsState state;

  const _UploadStatus({required this.state});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final upload = state.uploadTask;
    if (upload == null) return const SizedBox.shrink();
    final permissionEvidence = ref.watch(documentPermissionEvidenceProvider);
    final statusLabel = s.uploadStatusLabel(upload.status);
    final failed = upload.status == 'failed';
    final inProgress = _uploadInProgress(upload.status);
    final scheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              failed ? Icons.error_outline : Icons.upload_file_outlined,
              color: failed ? scheme.error : scheme.primary,
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    upload.filename,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 4),
                  Text(s.uploadTaskLine(upload.taskId, statusLabel)),
                  if (permissionEvidence != null &&
                      permissionEvidence.isNotEmpty) ...[
                    const SizedBox(height: 4),
                    Text(permissionEvidence),
                  ],
                  if (upload.message.isNotEmpty) ...[
                    const SizedBox(height: 4),
                    Text(
                      upload.message,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color: failed ? scheme.error : scheme.onSurface,
                          ),
                    ),
                  ],
                  if (failed) ...[
                    const SizedBox(height: 4),
                    Text(
                      s.uploadFailedHint,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color: scheme.onSurfaceVariant,
                          ),
                    ),
                  ],
                  if (inProgress) ...[
                    const SizedBox(height: 4),
                    Text(
                      s.uploadInProgressHint,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color: scheme.onSurfaceVariant,
                          ),
                    ),
                  ],
                  if (!inProgress && !failed) ...[
                    const SizedBox(height: 4),
                    Text(
                      s.uploadDoneHint,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color: scheme.onSurfaceVariant,
                          ),
                    ),
                  ],
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      OutlinedButton.icon(
                        onPressed: () => ref
                            .read(documentsControllerProvider.notifier)
                            .refreshUploadTask(),
                        icon: Icon(inProgress ? Icons.refresh : Icons.close),
                        label: Text(inProgress ? s.refreshStatus : s.close),
                      ),
                      if (state.canRetryLastUpload)
                        OutlinedButton.icon(
                          onPressed: () => ref
                              .read(documentsControllerProvider.notifier)
                              .retryLastUpload(),
                          icon: const Icon(Icons.replay_outlined),
                          label: Text(s.retryImport),
                        ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

bool _uploadInProgress(String status) {
  return switch (status) {
    'queued' || 'in_progress' || 'needs_ocr' => true,
    _ => false,
  };
}

class _ExportButton extends ConsumerWidget {
  final DocumentExportFormat format;
  final String label;

  const _ExportButton({required this.format, required this.label});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return OutlinedButton.icon(
      onPressed: () =>
          ref.read(documentsControllerProvider.notifier).exportDraft(format),
      icon: const Icon(Icons.ios_share_outlined),
      label: Text(label),
    );
  }
}
