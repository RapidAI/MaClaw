import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter/services.dart';
import 'package:image_picker/image_picker.dart';
import 'package:share_plus/share_plus.dart';

import '../../core/shared_intents/shared_intent_bootstrap.dart';
import '../../shared/surface.dart';
import 'document_draft.dart';
import 'documents_controller.dart';

typedef DocumentsExportFileShare = Future<ShareResult> Function(
  List<XFile> files, {
  String? text,
});

final documentsExportFileShareProvider = Provider<DocumentsExportFileShare>(
  (ref) => Share.shareXFiles,
);

class DocumentsScreen extends ConsumerStatefulWidget {
  const DocumentsScreen({super.key});

  @override
  ConsumerState<DocumentsScreen> createState() => _DocumentsScreenState();
}

class _DocumentsScreenState extends ConsumerState<DocumentsScreen> {
  final _titleController = TextEditingController(text: '应急情况说明');
  final _contentController = TextEditingController();
  DocumentTemplate _template = DocumentTemplate.report;
  String? _handledSharedIntentId;

  @override
  void dispose() {
    _titleController.dispose();
    _contentController.dispose();
    super.dispose();
  }

  Future<void> _createDraft() {
    return ref.read(documentsControllerProvider.notifier).createDraft(
          title: _titleController.text.trim(),
          template: _template,
          content: _contentController.text.trim(),
        );
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
    _consumeSharedIntent(state);
    return ScreenScaffold(
      title: '应急文档',
      subtitle: '快速生成、导入、轻编辑、导出和分享。',
      trailing: IconButton.filledTonal(
        tooltip: '导入文档',
        onPressed: () => ref
            .read(documentsControllerProvider.notifier)
            .pickAndUploadDocument(),
        icon: const Icon(Icons.upload_file_outlined),
      ),
      children: [
        _DraftComposer(
          titleController: _titleController,
          contentController: _contentController,
          template: _template,
          onTemplateChanged: (value) => setState(() => _template = value),
          onCreate: _createDraft,
        ),
        const SizedBox(height: 12),
        const _MobileDocumentImportPanel(),
        const SizedBox(height: 12),
        state.when(
          data: (value) => Column(
            children: [
              _UploadStatus(state: value),
              if (value.uploadTask != null && value.draft != null)
                const SizedBox(height: 12),
              _DraftPreview(state: value),
              const SizedBox(height: 12),
              _DocumentDraftHistoryCard(currentDraftId: value.draft?.id),
            ],
          ),
          error: (error, _) => Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Text('文档处理失败：$error'),
            ),
          ),
          loading: () => const Card(
            child: Padding(
              padding: EdgeInsets.all(16),
              child: LinearProgressIndicator(),
            ),
          ),
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
    final loading = ref.watch(documentsControllerProvider).isLoading;
    final controller = ref.read(documentsControllerProvider.notifier);
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
                Text('移动导入', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              '从文件、拍照或相册快速提交给官方服务解析，适合截图、纸质通知、现场照片和临时材料。',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                OutlinedButton.icon(
                  onPressed:
                      loading ? null : () => controller.pickAndUploadDocument(),
                  icon: const Icon(Icons.upload_file_outlined),
                  label: const Text('文件导入'),
                ),
                OutlinedButton.icon(
                  onPressed: loading
                      ? null
                      : () => controller.pickImageAndUploadDocument(
                            ImageSource.camera,
                          ),
                  icon: const Icon(Icons.photo_camera_outlined),
                  label: const Text('拍照导入'),
                ),
                OutlinedButton.icon(
                  onPressed: loading
                      ? null
                      : () => controller.pickImageAndUploadDocument(
                            ImageSource.gallery,
                          ),
                  icon: const Icon(Icons.photo_library_outlined),
                  label: const Text('相册导入'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _DocumentDraftHistoryCard extends ConsumerWidget {
  final String? currentDraftId;

  const _DocumentDraftHistoryCard({required this.currentDraftId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final history = ref.watch(documentDraftHistoryProvider);
    return history.when(
      data: (drafts) {
        if (drafts.isEmpty) return const SizedBox.shrink();
        final shown = drafts.take(8).toList();
        return Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '最近文档草稿',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 8),
                for (final draft in shown)
                  ListTile(
                    dense: true,
                    contentPadding: EdgeInsets.zero,
                    leading: Icon(
                      draft.id == currentDraftId
                          ? Icons.radio_button_checked
                          : Icons.description_outlined,
                    ),
                    title: Text(
                      draft.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    subtitle: Text(
                      '${documentTemplateLabel(draft.template)} · ${_draftPreviewText(draft.markdown)}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    onTap: draft.id == currentDraftId
                        ? null
                        : () => ref
                            .read(documentsControllerProvider.notifier)
                            .selectDraft(draft),
                  ),
              ],
            ),
          ),
        );
      },
      error: (_, __) => const SizedBox.shrink(),
      loading: () => const SizedBox.shrink(),
    );
  }
}

String _draftPreviewText(String markdown) {
  final compact = markdown.replaceAll(RegExp(r'\s+'), ' ').trim();
  if (compact.isEmpty) return '空白草稿';
  return compact.length > 32 ? '${compact.substring(0, 32)}...' : compact;
}

class _DraftComposer extends StatelessWidget {
  final TextEditingController titleController;
  final TextEditingController contentController;
  final DocumentTemplate template;
  final ValueChanged<DocumentTemplate> onTemplateChanged;
  final VoidCallback onCreate;

  const _DraftComposer({
    required this.titleController,
    required this.contentController,
    required this.template,
    required this.onTemplateChanged,
    required this.onCreate,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            TextField(
              controller: titleController,
              decoration: const InputDecoration(
                labelText: '标题',
                prefixIcon: Icon(Icons.title),
              ),
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<DocumentTemplate>(
              initialValue: template,
              items: [
                for (final item in DocumentTemplate.values)
                  DropdownMenuItem(
                    value: item,
                    child: Text(documentTemplateLabel(item)),
                  ),
              ],
              onChanged: (value) {
                if (value != null) onTemplateChanged(value);
              },
              decoration: const InputDecoration(
                labelText: '模板',
                prefixIcon: Icon(Icons.article_outlined),
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: contentController,
              minLines: 4,
              maxLines: 8,
              decoration: const InputDecoration(
                labelText: '要点或原始内容',
                hintText: '粘贴会议记录、告警信息、邮件要点或截图识别文本',
              ),
            ),
            const SizedBox(height: 14),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: onCreate,
                icon: const Icon(Icons.note_add_outlined),
                label: const Text('生成草稿'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _DraftPreview extends ConsumerStatefulWidget {
  final DocumentsState state;

  const _DraftPreview({required this.state});

  @override
  ConsumerState<_DraftPreview> createState() => _DraftPreviewState();
}

class _DraftPreviewState extends ConsumerState<_DraftPreview> {
  final _editTitleController = TextEditingController();
  final _editMarkdownController = TextEditingController();
  String? _loadedDraftId;
  String? _sharingExportJobId;

  @override
  void dispose() {
    _editTitleController.dispose();
    _editMarkdownController.dispose();
    super.dispose();
  }

  void _syncDraft(DocumentDraft draft) {
    if (_loadedDraftId == draft.id) return;
    _loadedDraftId = draft.id;
    _editTitleController.text = draft.title;
    _editMarkdownController.text = draft.markdown;
  }

  Future<void> _shareExport(DocumentExportJob job, DocumentDraft draft) async {
    setState(() => _sharingExportJobId = job.jobId);
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('正在准备导出文件...')),
    );
    try {
      final file = await ref
          .read(documentsControllerProvider.notifier)
          .downloadExportFile(job);
      final result = await ref.read(documentsExportFileShareProvider)(
        [XFile(file.path)],
        text: draft.title,
      );
      if (!mounted) return;
      final message = switch (result.status) {
        ShareResultStatus.success => '导出文件已交给系统分享。',
        ShareResultStatus.dismissed => '已取消分享，文件仍保存在临时导出目录。',
        ShareResultStatus.unavailable => '系统分享不可用，文件已保存到 ${file.path}',
      };
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(message)),
      );
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('分享导出文件失败：$error')),
      );
    } finally {
      if (mounted) {
        setState(() => _sharingExportJobId = null);
      }
    }
  }

  void _insertSnippet(String snippet) {
    final text = _editMarkdownController.text;
    final selection = _editMarkdownController.selection;
    final start = selection.isValid ? selection.start : text.length;
    final end = selection.isValid ? selection.end : text.length;
    final next = text.replaceRange(start, end, snippet);
    _editMarkdownController.value = TextEditingValue(
      text: next,
      selection: TextSelection.collapsed(offset: start + snippet.length),
    );
  }

  String _draftShareText() {
    final title = _editTitleController.text.trim();
    final markdown = _editMarkdownController.text.trim();
    if (title.isEmpty) return markdown;
    if (markdown.isEmpty) return title;
    return '# $title\n\n$markdown';
  }

  Future<void> _copyDraftText() async {
    final text = _draftShareText();
    if (text.trim().isEmpty) return;
    await Clipboard.setData(ClipboardData(text: text));
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('草稿文本已复制')),
    );
  }

  Future<void> _shareDraftText() async {
    final text = _draftShareText();
    if (text.trim().isEmpty) return;
    await Share.share(text, subject: _editTitleController.text.trim());
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.state;
    final draft = state.draft;
    if (draft == null) return const SizedBox.shrink();
    _syncDraft(draft);
    final scheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('轻量编辑', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            Text(
              documentTemplateLabel(draft.template),
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _editTitleController,
              decoration: const InputDecoration(
                labelText: '标题',
                prefixIcon: Icon(Icons.title),
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _editMarkdownController,
              minLines: 8,
              maxLines: 16,
              decoration: const InputDecoration(
                labelText: '正文 Markdown',
                alignLabelWithHint: true,
                prefixIcon: Icon(Icons.notes_outlined),
              ),
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                OutlinedButton.icon(
                  onPressed: () => _insertSnippet(
                    '\n\n| 项目 | 内容 | 负责人 |\n| --- | --- | --- |\n|  |  |  |\n',
                  ),
                  icon: const Icon(Icons.table_chart_outlined),
                  label: const Text('插入表格'),
                ),
                OutlinedButton.icon(
                  onPressed: () => _insertSnippet('\n\n> 批注：\n'),
                  icon: const Icon(Icons.comment_outlined),
                  label: const Text('插入批注'),
                ),
              ],
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton.icon(
                  onPressed: () => ref
                      .read(documentsControllerProvider.notifier)
                      .saveDraftEdits(
                        title: _editTitleController.text,
                        markdown: _editMarkdownController.text,
                      ),
                  icon: const Icon(Icons.save_outlined),
                  label: const Text('保存修改'),
                ),
                OutlinedButton.icon(
                  onPressed: _copyDraftText,
                  icon: const Icon(Icons.content_copy_outlined),
                  label: const Text('复制草稿'),
                ),
                OutlinedButton.icon(
                  onPressed: _shareDraftText,
                  icon: const Icon(Icons.ios_share_outlined),
                  label: const Text('分享文本'),
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
                '导出任务 ${state.exportJob!.jobId}：${_exportStatusLabel(state.exportJob!.status)}',
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
                  '文件已生成，可直接调起系统分享；分享前会先下载到本机临时目录。',
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
                    label: const Text('刷新状态'),
                  ),
                  if (state.canRetryLastExport)
                    OutlinedButton.icon(
                      onPressed: () => ref
                          .read(documentsControllerProvider.notifier)
                          .retryLastExport(),
                      icon: const Icon(Icons.replay_outlined),
                      label: const Text('重试导出'),
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
                            ? '准备分享'
                            : '分享文件',
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
    final state = ref.watch(documentsControllerProvider);
    final hasDraft = state.valueOrNull?.draft != null;
    final loading = state.isLoading;
    const actions = [
      _DocumentProcessAction('summarize', '摘要', Icons.summarize_outlined),
      _DocumentProcessAction('translate', '翻译', Icons.translate_outlined),
      _DocumentProcessAction('rewrite', '改写', Icons.edit_note_outlined),
      _DocumentProcessAction('expand', '扩写', Icons.unfold_more_outlined),
      _DocumentProcessAction('polish', '润色', Icons.auto_fix_high_outlined),
      _DocumentProcessAction('format', '整理', Icons.format_list_bulleted),
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
                Text('AI 处理', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              '对当前草稿执行摘要、翻译、改写、扩写、润色或格式整理。',
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
    final upload = state.uploadTask;
    if (upload == null) return const SizedBox.shrink();
    final statusLabel = _uploadStatusLabel(upload.status);
    final failed = upload.status == 'failed';
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
                  Text('导入任务 ${upload.taskId}：$statusLabel'),
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
                      '可重试导入，或改用文本、PDF、Word、图片截图等移动端更稳定的格式。',
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
                        icon: const Icon(Icons.refresh),
                        label: const Text('刷新状态'),
                      ),
                      if (state.canRetryLastUpload)
                        OutlinedButton.icon(
                          onPressed: () => ref
                              .read(documentsControllerProvider.notifier)
                              .retryLastUpload(),
                          icon: const Icon(Icons.replay_outlined),
                          label: const Text('重试导入'),
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

String _uploadStatusLabel(String status) {
  return switch (status) {
    'queued' => '等待远程解析',
    'in_progress' => '远程解析中',
    'needs_ocr' => '等待 OCR/视觉识别',
    'ready' => '已生成草稿',
    'failed' => '解析失败',
    _ => status,
  };
}

String _exportStatusLabel(String status) {
  return switch (status) {
    'queued' => '等待生成',
    'in_progress' => '生成中',
    'ready' => '已可分享',
    'failed' => '导出失败',
    _ => status,
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
