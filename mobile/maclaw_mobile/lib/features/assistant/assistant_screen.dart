import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../shared/surface.dart';

final assistantQueryProvider = StateProvider<String>((ref) => '');

class AssistantScreen extends ConsumerWidget {
  const AssistantScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final query = ref.watch(assistantQueryProvider);
    return ScreenScaffold(
      title: '查信息',
      subtitle: '联网搜索、整理来源，把结果转成可分享文本或文档草稿。',
      trailing: IconButton.filledTonal(
        tooltip: '语音提问',
        onPressed: () {},
        icon: const Icon(Icons.mic_none),
      ),
      children: [
        TextField(
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
                onPressed: query.trim().isEmpty ? null : () {},
                icon: const Icon(Icons.travel_explore),
                label: const Text('联网查询'),
              ),
            ),
            const SizedBox(width: 10),
            IconButton.outlined(
              tooltip: '拍照提问',
              onPressed: () {},
              icon: const Icon(Icons.photo_camera_outlined),
            ),
            const SizedBox(width: 8),
            IconButton.outlined(
              tooltip: '导入截图或文件',
              onPressed: () {},
              icon: const Icon(Icons.attach_file),
            ),
          ],
        ),
        const SizedBox(height: 18),
        const ActionTile(
          icon: Icons.article_outlined,
          title: '整理为文档草稿',
          subtitle: '把搜索结果转成通知、报告、邮件或会议纪要。',
          actionLabel: '选择模板',
        ),
        const SizedBox(height: 12),
        const ActionTile(
          icon: Icons.bookmark_add_outlined,
          title: '收藏常用问题',
          subtitle: '保存高频查询，保留来源和最近一次答案。',
          actionLabel: '查看收藏',
        ),
      ],
    );
  }
}

