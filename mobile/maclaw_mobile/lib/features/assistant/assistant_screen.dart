import 'dart:async';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';
import 'package:share_plus/share_plus.dart';

import '../../core/api/api_client.dart';
import '../../core/security/mobile_redaction.dart';
import '../../core/settings/app_preferences.dart';
import '../../core/shared_intents/mobile_shared_intent.dart';
import '../../core/shared_intents/shared_intent_bootstrap.dart';
import '../../shared/surface.dart';
import '../documents/document_draft.dart';
import '../documents/documents_controller.dart';
import 'assistant_controller.dart';
import 'assistant_voice_input.dart';
import 'search_history.dart';

bool canRetryAssistantQuery(String query) => query.trim().isNotEmpty;

typedef AssistantDocumentPathPicker = Future<String?> Function();
typedef AssistantTextAction = Future<void> Function(String text);

final assistantFilePathPickerProvider = Provider<AssistantDocumentPathPicker>(
  (ref) => () async {
    final file = await FilePicker.platform.pickFiles();
    return file?.files.single.path;
  },
);

final assistantCameraImagePathPickerProvider =
    Provider<AssistantDocumentPathPicker>(
  (ref) => () async {
    final image = await ImagePicker().pickImage(source: ImageSource.camera);
    return image?.path;
  },
);

final assistantGalleryImagePathPickerProvider =
    Provider<AssistantDocumentPathPicker>(
  (ref) => () async {
    final image = await ImagePicker().pickImage(source: ImageSource.gallery);
    return image?.path;
  },
);

final assistantClipboardWriterProvider = Provider<AssistantTextAction>(
  (ref) => (text) => Clipboard.setData(ClipboardData(text: text)),
);

final assistantShareProvider = Provider<AssistantTextAction>(
  (ref) => (text) async {
    await Share.share(text);
  },
);

List<SearchCitation> mergeAssistantCitations(
  List<SearchCitation> citations,
  SearchCitation? fallbackCitation,
) {
  final seenUrls = <String>{};
  final merged = <SearchCitation>[];
  for (final citation in [
    ...citations,
    if (fallbackCitation != null) fallbackCitation,
  ]) {
    final normalizedUrl = citation.url.trim().toLowerCase();
    if (normalizedUrl.isNotEmpty && !seenUrls.add(normalizedUrl)) continue;
    merged.add(citation);
  }
  return merged;
}

class AssistantScreen extends ConsumerStatefulWidget {
  const AssistantScreen({super.key});

  @override
  ConsumerState<AssistantScreen> createState() => _AssistantScreenState();
}

class _AssistantScreenState extends ConsumerState<AssistantScreen> {
  final _queryController = TextEditingController();
  late final AssistantVoiceInput _voiceInput;
  bool _listening = false;
  String? _voiceStatus;
  String? _handledSharedIntentId;

  @override
  void initState() {
    super.initState();
    _voiceInput = ref.read(assistantVoiceInputProvider);
  }

  @override
  void dispose() {
    unawaited(_voiceInput.stop());
    _queryController.dispose();
    super.dispose();
  }

  void _setQuery(String value) {
    _queryController.text = value;
    ref.read(assistantTabsProvider.notifier).updateActiveQuery(value);
    ref.read(assistantQueryProvider.notifier).state = value;
  }

  void _searchManually(String query) {
    ref.read(assistantSharedCitationProvider.notifier).state = null;
    ref.read(assistantTabsProvider.notifier).setActiveSharedCitation(null);
    ref.read(assistantSearchProvider.notifier).search(query);
  }

  Future<void> _toggleVoiceInput() async {
    if (_listening) {
      await _voiceInput.stop();
      setState(() {
        _listening = false;
        _voiceStatus = '语音输入已停止';
      });
      return;
    }
    final language = ref.read(appPreferencesProvider).valueOrNull?.language ??
        appLanguageChinese;
    final localeId = assistantSpeechLocaleForLanguage(language);
    final ready = await _voiceInput.start(
      localeId: localeId,
      onText: (text) {
        if (!mounted) return;
        _setQuery(text);
        setState(() => _voiceStatus = '已识别语音，检查后可联网查询');
      },
    );
    if (!mounted) return;
    if (!ready) {
      setState(() {
        _listening = false;
        _voiceStatus = '语音输入不可用，请检查麦克风权限';
      });
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('语音输入不可用，请检查麦克风权限')),
      );
      return;
    }
    setState(() {
      _listening = true;
      _voiceStatus = '正在听写，识别结果会填入输入框';
    });
  }

  Future<void> _pickImage() async {
    final path = await ref.read(assistantCameraImagePathPickerProvider)();
    if (path == null || path.isEmpty) return;
    _setQuery('请分析刚拍摄的图片，并给出可执行结论。');
    await _uploadToDocuments(
      path,
      successMessage: '图片已提交文档解析，完成后可在“文档”页继续处理。',
    );
  }

  Future<void> _pickGalleryImage() async {
    final path = await ref.read(assistantGalleryImagePathPickerProvider)();
    if (path == null || path.isEmpty) return;
    _setQuery('请分析这张截图或相册图片，提取关键信息并给出下一步。');
    await _uploadToDocuments(
      path,
      successMessage: '截图/图片已提交文档解析，完成后可在“文档”页继续处理。',
    );
  }

  Future<void> _pickFile() async {
    final path = await ref.read(assistantFilePathPickerProvider)();
    if (path == null || path.isEmpty) return;
    _setQuery('请总结刚导入文件或截图的关键信息。');
    await _uploadToDocuments(
      path,
      successMessage: '文件已提交文档解析，完成后可在“文档”页继续处理。',
    );
  }

  Future<void> _uploadToDocuments(
    String path, {
    required String successMessage,
  }) async {
    await ref
        .read(documentsControllerProvider.notifier)
        .uploadSharedDocument(path);
    if (!mounted) return;
    final documentState = ref.read(documentsControllerProvider);
    if (documentState.hasError) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('文档解析提交失败：${documentState.error}')),
      );
      return;
    }
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(successMessage)),
    );
  }

  void _consumeSharedIntent() {
    final shared = ref.watch(mobileSharedIntentProvider);
    if (shared == null ||
        !shared.opensAssistant ||
        shared.id == _handledSharedIntentId) {
      return;
    }
    _handledSharedIntentId = shared.id;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      final prompt = shared.assistantPrompt;
      if (shared.kind == MobileSharedIntentKind.link) {
        final citationUrl = shared.sharedUrl ?? shared.value;
        const snippet = '从系统分享进入 MaClaw Mobile 的链接。';
        final citation = SearchCitation(
          title: '分享来源',
          url: citationUrl,
          snippet: snippet,
        );
        ref.read(assistantSharedCitationProvider.notifier).state = citation;
        ref
            .read(assistantTabsProvider.notifier)
            .setActiveSharedCitation(citation);
      }
      _setQuery(prompt);
      ref.read(assistantSearchProvider.notifier).search(prompt);
      ref.read(mobileSharedIntentProvider.notifier).clear(shared.id);
    });
  }

  @override
  Widget build(BuildContext context) {
    _consumeSharedIntent();
    final tabs = ref.watch(assistantTabsProvider);
    final activeTab = tabs.activeTab;
    final query = ref.watch(assistantQueryProvider);
    final fallbackResult = ref.watch(assistantSearchProvider);
    final result = activeTab.resultTouched ? activeTab.result : fallbackResult;
    final sharedCitation =
        activeTab.sharedCitation ?? ref.watch(assistantSharedCitationProvider);
    final history = ref.watch(searchHistoryProvider);
    if (_queryController.text != query) {
      _queryController.text = query;
    }
    return ScreenScaffold(
      title: '查信息',
      subtitle: '联网搜索、整理来源，把结果转成可分享文本或文档草稿。',
      trailing: IconButton.filledTonal(
        tooltip: '语音提问',
        onPressed: _toggleVoiceInput,
        icon: Icon(_listening ? Icons.mic : Icons.mic_none),
      ),
      children: [
        _AssistantTabStrip(
          tabs: tabs,
          onActivate: (tab) {
            ref.read(assistantTabsProvider.notifier).activate(tab.id);
            final active = ref.read(assistantTabsProvider).activeTab;
            ref.read(assistantSharedCitationProvider.notifier).state =
                active.sharedCitation;
            ref.read(assistantQueryProvider.notifier).state = tab.query;
          },
          onAdd: () {
            final tab = ref.read(assistantTabsProvider.notifier).addTab();
            ref.read(assistantSharedCitationProvider.notifier).state =
                tab.sharedCitation;
            ref.read(assistantQueryProvider.notifier).state = tab.query;
          },
          onClose: (tab) {
            ref.read(assistantTabsProvider.notifier).close(tab.id);
            final active = ref.read(assistantTabsProvider).activeTab;
            ref.read(assistantSharedCitationProvider.notifier).state =
                active.sharedCitation;
            ref.read(assistantQueryProvider.notifier).state = active.query;
          },
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _queryController,
          minLines: 3,
          maxLines: 6,
          onChanged: (value) => _setQuery(value),
          decoration: const InputDecoration(
            labelText: '要查什么？',
            hintText: '例如：总结这个链接的关键事实，保留来源引用',
            prefixIcon: Icon(Icons.search),
          ),
        ),
        if (_voiceStatus != null) ...[
          const SizedBox(height: 8),
          Row(
            children: [
              Icon(
                _listening ? Icons.mic : Icons.info_outline,
                size: 18,
                color: Theme.of(context).colorScheme.primary,
              ),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  _voiceStatus!,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                ),
              ),
            ],
          ),
        ],
        const SizedBox(height: 12),
        Row(
          children: [
            Expanded(
              child: FilledButton.icon(
                onPressed:
                    query.trim().isEmpty ? null : () => _searchManually(query),
                icon: const Icon(Icons.travel_explore),
                label: const Text('联网查询'),
              ),
            ),
            const SizedBox(width: 10),
            IconButton.outlined(
              tooltip: '拍照提问',
              onPressed: _pickImage,
              icon: const Icon(Icons.photo_camera_outlined),
            ),
            const SizedBox(width: 8),
            IconButton.outlined(
              tooltip: '从相册选择截图',
              onPressed: _pickGalleryImage,
              icon: const Icon(Icons.photo_library_outlined),
            ),
            const SizedBox(width: 8),
            IconButton.outlined(
              tooltip: '导入截图或文件',
              onPressed: _pickFile,
              icon: const Icon(Icons.attach_file),
            ),
          ],
        ),
        const SizedBox(height: 18),
        result.when(
          data: (answer) => answer == null
              ? const SizedBox.shrink()
              : _SearchResultCard(
                  query: query,
                  answer: answer.answer,
                  citations: answer.citations,
                  fallbackCitation: sharedCitation,
                ),
          error: (error, _) => _SearchErrorCard(
            error: error,
            query: query,
          ),
          loading: () => const Card(
            child: Padding(
              padding: EdgeInsets.all(16),
              child: LinearProgressIndicator(),
            ),
          ),
        ),
        const SizedBox(height: 12),
        _SearchHistoryCard(
          history: history,
          onSelect: _setQuery,
        ),
      ],
    );
  }
}

class _AssistantTabStrip extends StatelessWidget {
  final AssistantTabsState tabs;
  final ValueChanged<AssistantConversationTab> onActivate;
  final ValueChanged<AssistantConversationTab> onClose;
  final VoidCallback onAdd;

  const _AssistantTabStrip({
    required this.tabs,
    required this.onActivate,
    required this.onClose,
    required this.onAdd,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SizedBox(
      height: 44,
      child: Row(
        children: [
          Expanded(
            child: ListView.separated(
              scrollDirection: Axis.horizontal,
              itemBuilder: (context, index) {
                final tab = tabs.tabs[index];
                final selected = tab.id == tabs.activeTabId;
                return InputChip(
                  selected: selected,
                  showCheckmark: false,
                  avatar: Icon(
                    tab.primary ? Icons.chat_bubble_outline : Icons.tab,
                    size: 18,
                    color: selected ? scheme.primary : scheme.onSurfaceVariant,
                  ),
                  label: ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 120),
                    child: Text(
                      tab.title,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  onPressed: () => onActivate(tab),
                  onDeleted: tab.primary ? null : () => onClose(tab),
                  deleteIcon: const Icon(Icons.close, size: 18),
                );
              },
              separatorBuilder: (context, index) => const SizedBox(width: 8),
              itemCount: tabs.tabs.length,
            ),
          ),
          const SizedBox(width: 8),
          IconButton.outlined(
            tooltip: '新建副对话',
            onPressed: onAdd,
            icon: const Icon(Icons.add),
          ),
        ],
      ),
    );
  }
}

class _SearchErrorCard extends ConsumerWidget {
  final Object error;
  final String query;

  const _SearchErrorCard({
    required this.error,
    required this.query,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('查询失败：$error'),
            const SizedBox(height: 10),
            OutlinedButton.icon(
              onPressed: canRetryAssistantQuery(query)
                  ? () =>
                      ref.read(assistantSearchProvider.notifier).search(query)
                  : null,
              icon: const Icon(Icons.refresh),
              label: const Text('重试查询'),
            ),
          ],
        ),
      ),
    );
  }
}

class _SearchHistoryCard extends ConsumerWidget {
  final AsyncValue<List<SearchHistoryEntry>> history;
  final ValueChanged<String> onSelect;

  const _SearchHistoryCard({
    required this.history,
    required this.onSelect,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: history.when(
          data: (items) => _buildHistory(context, ref, items),
          error: (error, _) => Text('搜索历史加载失败：$error'),
          loading: () => const LinearProgressIndicator(),
        ),
      ),
    );
  }

  Widget _buildHistory(
    BuildContext context,
    WidgetRef ref,
    List<SearchHistoryEntry> items,
  ) {
    final favorites = items.where((item) => item.favorite).take(4).toList();
    final recent = items.where((item) => !item.favorite).take(6).toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(
              Icons.history,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(width: 8),
            Text('搜索历史', style: Theme.of(context).textTheme.titleMedium),
            const Spacer(),
            if (items.any((item) => !item.favorite))
              TextButton.icon(
                onPressed: () => _confirmClearNonFavorites(context, ref),
                icon: const Icon(Icons.cleaning_services_outlined),
                label: const Text('清理'),
              ),
          ],
        ),
        if (items.isEmpty) ...[
          const SizedBox(height: 8),
          Text(
            '完成一次联网查询后会保存在这里。',
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
          ),
        ] else ...[
          if (favorites.isNotEmpty) ...[
            const SizedBox(height: 8),
            _HistorySectionTitle(
              icon: Icons.star,
              label: '常用问题',
              count: favorites.length,
            ),
            for (final item in favorites)
              _HistoryItem(item: item, onSelect: onSelect),
          ],
          if (recent.isNotEmpty) ...[
            const SizedBox(height: 8),
            _HistorySectionTitle(
              icon: Icons.schedule,
              label: '最近查询',
              count: recent.length,
            ),
            for (final item in recent)
              _HistoryItem(item: item, onSelect: onSelect),
          ],
        ],
      ],
    );
  }

  Future<void> _confirmClearNonFavorites(
    BuildContext context,
    WidgetRef ref,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('只保留收藏问题？'),
        content: const Text(
          '将清理未收藏的搜索历史，已收藏的常用问题会继续保留在本机，方便应急时快速复用。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('只保留收藏'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await ref.read(searchHistoryProvider.notifier).clearNonFavorites();
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('已清理未收藏历史，常用问题已保留')),
    );
  }
}

class _HistorySectionTitle extends StatelessWidget {
  final IconData icon;
  final String label;
  final int count;

  const _HistorySectionTitle({
    required this.icon,
    required this.label,
    required this.count,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Row(
      children: [
        Icon(icon, size: 18, color: scheme.primary),
        const SizedBox(width: 6),
        Text(label, style: Theme.of(context).textTheme.labelLarge),
        const SizedBox(width: 6),
        Text(
          '$count',
          style: Theme.of(context).textTheme.labelSmall?.copyWith(
                color: scheme.onSurfaceVariant,
              ),
        ),
      ],
    );
  }
}

class _HistoryItem extends ConsumerWidget {
  final SearchHistoryEntry item;
  final ValueChanged<String> onSelect;

  const _HistoryItem({
    required this.item,
    required this.onSelect,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return ListTile(
      dense: true,
      contentPadding: EdgeInsets.zero,
      leading: IconButton(
        tooltip: item.favorite ? '取消收藏' : '收藏',
        onPressed: () =>
            ref.read(searchHistoryProvider.notifier).toggleFavorite(item.id),
        icon: Icon(item.favorite ? Icons.star : Icons.star_border),
      ),
      title: Text(item.query),
      subtitle: Text(item.answerPreview),
      trailing: IconButton(
        tooltip: '删除',
        onPressed: () =>
            ref.read(searchHistoryProvider.notifier).remove(item.id),
        icon: const Icon(Icons.delete_outline),
      ),
      onTap: () => onSelect(item.query),
    );
  }
}

class _SearchResultCard extends ConsumerWidget {
  final String query;
  final String answer;
  final List<SearchCitation> citations;
  final SearchCitation? fallbackCitation;

  const _SearchResultCard({
    required this.query,
    required this.answer,
    required this.citations,
    this.fallbackCitation,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    final mergedCitations = _mergedCitations();
    final markdown = _answerMarkdown(mergedCitations);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.fact_check_outlined, color: scheme.primary),
                const SizedBox(width: 8),
                Text('查询结果', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 12),
            Text(answer),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                OutlinedButton.icon(
                  onPressed: () =>
                      ref.read(assistantShareProvider).call(markdown),
                  icon: const Icon(Icons.ios_share_outlined),
                  label: const Text('分享结果'),
                ),
                OutlinedButton.icon(
                  onPressed: () => _copyMarkdown(context, ref, markdown),
                  icon: const Icon(Icons.content_copy_outlined),
                  label: const Text('复制结果'),
                ),
                OutlinedButton.icon(
                  onPressed: () => _createDocumentDraft(context, ref, markdown),
                  icon: const Icon(Icons.article_outlined),
                  label: const Text('整理为草稿'),
                ),
              ],
            ),
            if (mergedCitations.isNotEmpty) ...[
              const SizedBox(height: 12),
              Text(
                '来源',
                style: Theme.of(context).textTheme.labelLarge?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
              ),
              const SizedBox(height: 6),
              for (final citation in mergedCitations)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: _CitationTile(citation: citation),
                ),
            ],
          ],
        ),
      ),
    );
  }

  List<SearchCitation> _mergedCitations() {
    return mergeAssistantCitations(citations, fallbackCitation);
  }

  String _answerMarkdown(List<SearchCitation> mergedCitations) {
    return assistantSearchResultMarkdown(
      query: query,
      answer: answer,
      citations: mergedCitations,
    );
  }

  Future<void> _copyMarkdown(
    BuildContext context,
    WidgetRef ref,
    String markdown,
  ) async {
    await ref.read(assistantClipboardWriterProvider).call(markdown);
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('查询结果已复制')),
    );
  }

  Future<void> _createDocumentDraft(
    BuildContext context,
    WidgetRef ref,
    String markdown,
  ) async {
    final template = await showModalBottomSheet<DocumentTemplate>(
      context: context,
      builder: (context) => const _DocumentTemplateSheet(),
    );
    if (template == null || !context.mounted) return;
    await ref.read(documentsControllerProvider.notifier).createDraft(
          title: assistantSearchDraftTitle(query),
          template: template,
          content: markdown,
        );
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('已整理为${documentTemplateLabel(template)}草稿')),
    );
  }
}

String assistantSearchDraftTitle(String query) {
  final compact = query.replaceAll(RegExp(r'\s+'), ' ').trim();
  if (compact.isEmpty) return '信息查询整理';
  final title =
      compact.length > 28 ? '${compact.substring(0, 28)}...' : compact;
  return '信息查询：$title';
}

String assistantSearchResultMarkdown({
  required String query,
  required String answer,
  required List<SearchCitation> citations,
}) {
  final buffer = StringBuffer();
  final normalizedQuery = query.trim();
  if (normalizedQuery.isNotEmpty) {
    buffer
      ..writeln('## 问题')
      ..writeln()
      ..writeln(redactMobileSensitiveText(normalizedQuery))
      ..writeln();
  }
  buffer
    ..writeln('## 结论')
    ..writeln()
    ..writeln(redactMobileSensitiveText(answer.trim()));
  if (citations.isNotEmpty) {
    buffer
      ..writeln()
      ..writeln('## 来源');
    for (final citation in citations) {
      buffer.writeln(assistantCitationMarkdown(citation));
    }
  }
  return buffer.toString().trimRight();
}

class _CitationTile extends ConsumerWidget {
  final SearchCitation citation;

  const _CitationTile({required this.citation});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    final citationText = assistantCitationMarkdown(citation);
    return DecoratedBox(
      decoration: BoxDecoration(
        border: Border.all(color: scheme.outlineVariant),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Padding(
        padding: const EdgeInsets.all(10),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              citation.title.isEmpty ? citation.url : citation.title,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
            ),
            if (citation.url.isNotEmpty) ...[
              const SizedBox(height: 4),
              SelectableText(
                citation.url,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.primary,
                    ),
              ),
            ],
            if (citation.snippet.isNotEmpty) ...[
              const SizedBox(height: 6),
              Text(
                citation.snippet,
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
                  onPressed: citation.url.isEmpty
                      ? null
                      : () => _copyText(
                            context,
                            ref,
                            redactMobileSensitiveText(citation.url),
                            '来源链接已复制',
                          ),
                  icon: const Icon(Icons.link),
                  label: const Text('复制链接'),
                ),
                OutlinedButton.icon(
                  onPressed: () =>
                      _copyText(context, ref, citationText, '来源引用已复制'),
                  icon: const Icon(Icons.format_quote_outlined),
                  label: const Text('复制引用'),
                ),
                OutlinedButton.icon(
                  onPressed: () =>
                      ref.read(assistantShareProvider).call(citationText),
                  icon: const Icon(Icons.ios_share_outlined),
                  label: const Text('分享来源'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _copyText(
    BuildContext context,
    WidgetRef ref,
    String text,
    String message,
  ) async {
    await ref.read(assistantClipboardWriterProvider).call(text);
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(message)),
    );
  }
}

String assistantCitationMarkdown(SearchCitation citation) {
  final title = redactMobileSensitiveText(citation.title.trim());
  final url = redactMobileSensitiveText(citation.url.trim());
  final snippet = redactMobileSensitiveText(citation.snippet.trim());
  final buffer = StringBuffer();
  if (title.isNotEmpty && url.isNotEmpty) {
    buffer.write('- $title $url');
  } else if (title.isNotEmpty) {
    buffer.write('- $title');
  } else if (url.isNotEmpty) {
    buffer.write('- $url');
  } else {
    buffer.write('- 来源');
  }
  if (snippet.isNotEmpty) {
    buffer
      ..writeln()
      ..write('  $snippet');
  }
  return buffer.toString();
}

class _DocumentTemplateSheet extends StatelessWidget {
  const _DocumentTemplateSheet();

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('选择草稿模板', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 8),
              for (final template in DocumentTemplate.values)
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: Icon(_documentTemplateIcon(template)),
                  title: Text(documentTemplateLabel(template)),
                  onTap: () => Navigator.of(context).pop(template),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

IconData _documentTemplateIcon(DocumentTemplate template) {
  return switch (template) {
    DocumentTemplate.notice => Icons.campaign_outlined,
    DocumentTemplate.report => Icons.summarize_outlined,
    DocumentTemplate.email => Icons.mail_outline,
    DocumentTemplate.proposal => Icons.lightbulb_outline,
    DocumentTemplate.meetingMinutes => Icons.groups_outlined,
    DocumentTemplate.statement => Icons.description_outlined,
  };
}
