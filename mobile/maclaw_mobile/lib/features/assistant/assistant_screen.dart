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
import '../auth/session_controller.dart';
import '../documents/document_draft.dart';
import '../documents/documents_controller.dart';
import 'assistant_controller.dart';
import 'assistant_voice_input.dart';
import 'search_history.dart';

bool canRetryAssistantQuery(String query) => query.trim().isNotEmpty;

String assistantQueryWithVoiceTranscript(
  String currentQuery,
  String transcript, {
  String previousTranscript = '',
}) {
  final recognized = transcript.trim();
  if (recognized.isEmpty) return currentQuery;

  var base = currentQuery.trimRight();
  final previous = previousTranscript.trim();
  if (previous.isNotEmpty && base.endsWith(previous)) {
    base = base.substring(0, base.length - previous.length).trimRight();
  }
  if (base.isEmpty) return recognized;
  if (base == recognized || base.endsWith(recognized)) return base;
  return '$base\n$recognized';
}

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
  String _lastVoiceTranscript = '';

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
    _lastVoiceTranscript = '';
    final ready = await _voiceInput.start(
      localeId: localeId,
      onText: (text) {
        if (!mounted) return;
        final merged = assistantQueryWithVoiceTranscript(
          _queryController.text,
          text,
          previousTranscript: _lastVoiceTranscript,
        );
        _lastVoiceTranscript = text.trim();
        _setQuery(merged);
        setState(() => _voiceStatus = '已识别语音，检查后可发送给 AI 助手');
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
      _voiceStatus = '正在听写，识别结果会填入 AI 助手输入框';
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
    final session = ref.watch(sessionControllerProvider);
    if (shared == null ||
        !shared.opensAssistant ||
        shared.id == _handledSharedIntentId) {
      return;
    }
    if (session.isLoading) return;
    final assistantEnabled =
        session.valueOrNull?.bootstrap?.features.assistant ?? true;
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
      if (assistantEnabled) {
        ref.read(assistantSearchProvider.notifier).search(prompt);
      }
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
    final assistantEnabled = ref
            .watch(sessionControllerProvider)
            .valueOrNull
            ?.bootstrap
            ?.features
            .assistant ??
        true;
    if (_queryController.text != query) {
      _queryController.text = query;
    }
    return ScreenScaffold(
      title: 'AI助手',
      subtitle: '类似 MaClaw GUI 的多对话 AI 助手，可文字或语音输入，也可接入截图、文件、服务器日志和数字员工能力。',
      trailing: IconButton.filledTonal(
        tooltip: _listening ? '停止语音输入' : '开始语音输入',
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
          decoration: InputDecoration(
            labelText: '问 MaClaw AI 助手',
            hintText: '直接输入，或点麦克风用语音告诉 AI 助手要处理什么',
            helperText: '支持普通对话、助手联网、文档处理、日志排障和应急操作草案。',
            prefixIcon: const Icon(Icons.auto_awesome_outlined),
            suffixIcon: IconButton(
              tooltip: _listening ? '停止语音输入' : '语音输入',
              onPressed: _toggleVoiceInput,
              icon: Icon(_listening ? Icons.mic : Icons.mic_none),
            ),
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
        const SizedBox(height: 10),
        _AssistantQuickPrompts(onSelect: _setQuery),
        if (!assistantEnabled) ...[
          const SizedBox(height: 10),
          const _AssistantSearchUnavailableBanner(),
        ],
        const SizedBox(height: 12),
        Row(
          children: [
            Expanded(
              child: FilledButton.icon(
                onPressed: query.trim().isEmpty || !assistantEnabled
                    ? null
                    : () => _searchManually(query),
                icon: const Icon(Icons.send_outlined),
                label: const Text('发送给 AI 助手'),
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
              ? const _AssistantWorkspaceIntro()
              : _AssistantAnswerCard(
                  query: query,
                  answer: answer.answer,
                  citations: answer.citations,
                  fallbackCitation: sharedCitation,
                ),
          error: (error, _) => _AssistantErrorCard(
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
        _AssistantHistoryCard(
          history: history,
          onSelect: _setQuery,
        ),
      ],
    );
  }
}

class _AssistantWorkspaceIntro extends StatelessWidget {
  const _AssistantWorkspaceIntro();

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final textTheme = Theme.of(context).textTheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(Icons.chat_bubble_outline, color: scheme.primary),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('MaClaw AI 助手', style: textTheme.titleMedium),
                      const SizedBox(height: 4),
                      Text(
                        '像电脑端一样用自然语言发起多轮对话；手机端重点支持语音、截图、文件、服务器日志和应急排障。',
                        style: textTheme.bodyMedium?.copyWith(
                          color: scheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 14),
            const Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                _AssistantCapabilityChip(
                  icon: Icons.mic_none,
                  label: '语音输入',
                ),
                _AssistantCapabilityChip(
                  icon: Icons.travel_explore_outlined,
                  label: '助手联网',
                ),
                _AssistantCapabilityChip(
                  icon: Icons.image_search_outlined,
                  label: '截图提问',
                ),
                _AssistantCapabilityChip(
                  icon: Icons.article_outlined,
                  label: '文档草稿',
                ),
                _AssistantCapabilityChip(
                  icon: Icons.manage_search_outlined,
                  label: '远程排障',
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _AssistantCapabilityChip extends StatelessWidget {
  final IconData icon;
  final String label;

  const _AssistantCapabilityChip({
    required this.icon,
    required this.label,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Chip(
      avatar: Icon(icon, size: 18, color: scheme.primary),
      label: Text(label),
      side: BorderSide(color: scheme.outlineVariant),
      backgroundColor: scheme.surface,
    );
  }
}

class _AssistantSearchUnavailableBanner extends StatelessWidget {
  const _AssistantSearchUnavailableBanner();

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: scheme.secondaryContainer,
        border: Border.all(color: scheme.outlineVariant),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(Icons.info_outline, color: scheme.onSecondaryContainer),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                '当前 Hub 未启用 AI 助手服务能力，发送给 AI 助手暂不可用。仍可语音输入、导入图片/文件，或把内容整理成文档草稿。',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.onSecondaryContainer,
                    ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _AssistantQuickPrompts extends StatelessWidget {
  final ValueChanged<String> onSelect;

  const _AssistantQuickPrompts({required this.onSelect});

  static const _prompts = [
    (
      label: '自由对话',
      icon: Icons.chat_bubble_outline,
      text: '我想和 MaClaw AI 助手讨论一个问题，请先帮我理清思路并追问必要信息。',
    ),
    (
      label: '助手联网',
      icon: Icons.travel_explore_outlined,
      text: '请用 MaClaw AI 助手联网核对这件事，列出关键结论和来源引用。',
    ),
    (
      label: '文档草稿',
      icon: Icons.article_outlined,
      text: '帮我把下面要点整理成一份可直接发送的应急文档草稿。',
    ),
    (
      label: '日志排障',
      icon: Icons.fact_check_outlined,
      text: '帮我分析这段服务器日志，先说明风险，再给出需要人工确认的排查命令草案。',
    ),
  ];

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final prompt in _prompts)
          ActionChip(
            avatar: Icon(prompt.icon, size: 18),
            label: Text(prompt.label),
            tooltip: prompt.text,
            onPressed: () => onSelect(prompt.text),
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

class _AssistantErrorCard extends ConsumerWidget {
  final Object error;
  final String query;

  const _AssistantErrorCard({
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
            Text('助手请求失败：$error'),
            const SizedBox(height: 10),
            OutlinedButton.icon(
              onPressed: canRetryAssistantQuery(query)
                  ? () =>
                      ref.read(assistantSearchProvider.notifier).search(query)
                  : null,
              icon: const Icon(Icons.refresh),
              label: const Text('重新发送'),
            ),
          ],
        ),
      ),
    );
  }
}

class _AssistantHistoryCard extends ConsumerWidget {
  final AsyncValue<List<SearchHistoryEntry>> history;
  final ValueChanged<String> onSelect;

  const _AssistantHistoryCard({
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
          error: (error, _) => Text('助手历史加载失败：$error'),
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
            Text('助手历史', style: Theme.of(context).textTheme.titleMedium),
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
            '发送给 AI 助手后的结果会保存在这里。',
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
              label: '最近对话',
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
          '将清理未收藏的助手历史，已收藏的常用问题会继续保留在本机，方便应急时快速复用。',
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

class _AssistantAnswerCard extends ConsumerWidget {
  final String query;
  final String answer;
  final List<SearchCitation> citations;
  final SearchCitation? fallbackCitation;

  const _AssistantAnswerCard({
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
                Text('助手回答', style: Theme.of(context).textTheme.titleMedium),
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
    return assistantAnswerMarkdown(
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
      const SnackBar(content: Text('助手回答已复制')),
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
          title: assistantDraftTitle(query),
          template: template,
          content: markdown,
        );
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('已整理为${documentTemplateLabel(template)}草稿')),
    );
  }
}

String assistantDraftTitle(String query) {
  final compact = redactMobileSensitiveText(
    query.replaceAll(RegExp(r'\s+'), ' ').trim(),
  );
  if (compact.isEmpty) return 'AI助手整理';
  final title = _assistantDraftTitlePreview(compact);
  return 'AI助手：$title';
}

String _assistantDraftTitlePreview(String text) {
  const maxLength = 28;
  if (text.length <= maxLength) return text;
  var end = maxLength;
  for (final marker in const [
    '[REDACTED_SECRET]',
    '[REDACTED_TOKEN]',
    '[REDACTED_PRIVATE_KEY]',
    '[REDACTED_CREDENTIALS]',
  ]) {
    final start = text.lastIndexOf(marker, end);
    if (start >= 0 && start + marker.length > end) {
      end = start + marker.length;
      break;
    }
  }
  return '${text.substring(0, end)}...';
}

String assistantAnswerMarkdown({
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

@Deprecated('Use assistantDraftTitle for the mobile AI assistant workspace.')
String assistantSearchDraftTitle(String query) => assistantDraftTitle(query);

@Deprecated(
  'Use assistantAnswerMarkdown for the mobile AI assistant workspace.',
)
String assistantSearchResultMarkdown({
  required String query,
  required String answer,
  required List<SearchCitation> citations,
}) =>
    assistantAnswerMarkdown(
      query: query,
      answer: answer,
      citations: citations,
    );

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
