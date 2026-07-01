import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';
import 'package:share_plus/share_plus.dart';
import 'package:speech_to_text/speech_to_text.dart';

import '../../core/api/api_client.dart';
import '../../shared/surface.dart';
import '../documents/document_draft.dart';
import '../documents/documents_controller.dart';
import 'assistant_controller.dart';
import 'search_history.dart';

class AssistantScreen extends ConsumerStatefulWidget {
  const AssistantScreen({super.key});

  @override
  ConsumerState<AssistantScreen> createState() => _AssistantScreenState();
}

class _AssistantScreenState extends ConsumerState<AssistantScreen> {
  final _queryController = TextEditingController();
  final _speech = SpeechToText();
  bool _listening = false;

  @override
  void dispose() {
    _queryController.dispose();
    super.dispose();
  }

  void _setQuery(String value) {
    _queryController.text = value;
    ref.read(assistantQueryProvider.notifier).state = value;
  }

  Future<void> _toggleVoiceInput() async {
    if (_listening) {
      await _speech.stop();
      setState(() => _listening = false);
      return;
    }
    final ready = await _speech.initialize();
    if (!ready) return;
    setState(() => _listening = true);
    await _speech.listen(
      localeId: 'zh_CN',
      onResult: (result) => _setQuery(result.recognizedWords),
    );
  }

  Future<void> _pickImage() async {
    final image = await ImagePicker().pickImage(source: ImageSource.camera);
    if (image == null) return;
    _setQuery('请分析这张图片，并给出可执行结论：${image.path}');
  }

  Future<void> _pickFile() async {
    final file = await FilePicker.platform.pickFiles();
    final path = file?.files.single.path;
    if (path == null || path.isEmpty) return;
    _setQuery('请总结这个文件或截图的关键信息：$path');
  }

  @override
  Widget build(BuildContext context) {
    final query = ref.watch(assistantQueryProvider);
    final result = ref.watch(assistantSearchProvider);
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
        TextField(
          controller: _queryController,
          minLines: 3,
          maxLines: 6,
          onChanged: (value) =>
              ref.read(assistantQueryProvider.notifier).state = value,
          decoration: const InputDecoration(
            labelText: '要查什么？',
            hintText: '例如：总结这个链接的关键事实，保留来源引用',
            prefixIcon: Icon(Icons.search),
          ),
        ),
        const SizedBox(height: 12),
        Row(
          children: [
            Expanded(
              child: FilledButton.icon(
                onPressed: query.trim().isEmpty
                    ? null
                    : () => ref
                        .read(assistantSearchProvider.notifier)
                        .search(query),
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
                  answer: answer.answer,
                  citations: answer.citations,
                ),
          error: (error, _) => Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Text('查询失败：$error'),
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
        const ActionTile(
          icon: Icons.article_outlined,
          title: '整理为文档草稿',
          subtitle: '把搜索结果转成通知、报告、邮件或会议纪要。',
          actionLabel: '选择模板',
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
          data: (items) => Column(
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
                      onPressed: () => ref
                          .read(searchHistoryProvider.notifier)
                          .clearNonFavorites(),
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
                const SizedBox(height: 8),
                for (final item in _orderedItems(items).take(8))
                  ListTile(
                    dense: true,
                    contentPadding: EdgeInsets.zero,
                    leading: IconButton(
                      tooltip: item.favorite ? '取消收藏' : '收藏',
                      onPressed: () => ref
                          .read(searchHistoryProvider.notifier)
                          .toggleFavorite(item.id),
                      icon: Icon(
                        item.favorite ? Icons.star : Icons.star_border,
                      ),
                    ),
                    title: Text(item.query),
                    subtitle: Text(item.answerPreview),
                    trailing: IconButton(
                      tooltip: '删除',
                      onPressed: () => ref
                          .read(searchHistoryProvider.notifier)
                          .remove(item.id),
                      icon: const Icon(Icons.delete_outline),
                    ),
                    onTap: () => onSelect(item.query),
                  ),
              ],
            ],
          ),
          error: (error, _) => Text('搜索历史加载失败：$error'),
          loading: () => const LinearProgressIndicator(),
        ),
      ),
    );
  }

  List<SearchHistoryEntry> _orderedItems(List<SearchHistoryEntry> items) {
    return [
      ...items.where((item) => item.favorite),
      ...items.where((item) => !item.favorite),
    ];
  }
}

class _SearchResultCard extends ConsumerWidget {
  final String answer;
  final List<SearchCitation> citations;

  const _SearchResultCard({
    required this.answer,
    required this.citations,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    final markdown = _answerMarkdown();
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
                  onPressed: () => Share.share(markdown),
                  icon: const Icon(Icons.ios_share_outlined),
                  label: const Text('分享结果'),
                ),
                OutlinedButton.icon(
                  onPressed: () => ref
                      .read(documentsControllerProvider.notifier)
                      .createDraft(
                        title: '信息查询整理',
                        template: DocumentTemplate.report,
                        content: markdown,
                      ),
                  icon: const Icon(Icons.article_outlined),
                  label: const Text('整理为草稿'),
                ),
              ],
            ),
            if (citations.isNotEmpty) ...[
              const SizedBox(height: 12),
              Text(
                '来源',
                style: Theme.of(context).textTheme.labelLarge?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
              ),
              const SizedBox(height: 6),
              for (final citation in citations)
                Padding(
                  padding: const EdgeInsets.only(bottom: 6),
                  child: Text(
                    [
                      '- ${citation.title} ${citation.url}',
                      if (citation.snippet.isNotEmpty) citation.snippet,
                    ].join('\n'),
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
            ],
          ],
        ),
      ),
    );
  }

  String _answerMarkdown() {
    final buffer = StringBuffer(answer.trim());
    if (citations.isNotEmpty) {
      buffer.writeln();
      buffer.writeln();
      buffer.writeln('来源：');
      for (final citation in citations) {
        buffer.writeln('- ${citation.title} ${citation.url}');
        if (citation.snippet.isNotEmpty) {
          buffer.writeln('  ${citation.snippet}');
        }
      }
    }
    return buffer.toString();
  }
}
