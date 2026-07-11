import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'local_memory_controller.dart';
import 'local_memory_note.dart';

/// Save a free-form or prefilled note into on-device memory (+ Hub when online).
Future<LocalMemoryNote?> showRememberMemoryDialog(
  BuildContext context,
  WidgetRef ref, {
  String initialTitle = '',
  String initialContent = '',
  String initialCategory = 'user_fact',
}) async {
  final titleCtrl = TextEditingController(text: initialTitle);
  final contentCtrl = TextEditingController(text: initialContent);
  var pin = false;
  var category = localMemoryCategories.containsKey(initialCategory)
      ? initialCategory
      : 'user_fact';

  final ok = await showModalBottomSheet<bool>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (ctx) {
      return StatefulBuilder(
        builder: (ctx, setLocal) {
          final bottom = MediaQuery.viewInsetsOf(ctx).bottom;
          return Padding(
            padding: EdgeInsets.only(
              left: 20,
              right: 20,
              top: 8,
              bottom: bottom + 20,
            ),
            child: SingleChildScrollView(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    '记住这条',
                    style: Theme.of(ctx).textTheme.titleLarge?.copyWith(
                          fontWeight: FontWeight.w700,
                        ),
                  ),
                  const SizedBox(height: 6),
                  Text(
                    '写入本机记忆库。仅「启用」的条目会进入对话上下文，并受条数/字数预算限制，避免撑爆上下文。',
                    style: Theme.of(ctx).textTheme.bodyMedium?.copyWith(
                          color: Theme.of(ctx).colorScheme.onSurfaceVariant,
                        ),
                  ),
                  const SizedBox(height: 16),
                  TextField(
                    controller: titleCtrl,
                    textInputAction: TextInputAction.next,
                    decoration: const InputDecoration(
                      labelText: '标题（可选）',
                      hintText: '例如：客户偏好 / 服务器约定',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 12),
                  DropdownButtonFormField<String>(
                    // ignore: deprecated_member_use
                    value: category,
                    decoration: const InputDecoration(
                      labelText: '分类',
                      border: OutlineInputBorder(),
                    ),
                    items: [
                      for (final e in localMemoryCategories.entries)
                        DropdownMenuItem(value: e.key, child: Text(e.value)),
                    ],
                    onChanged: (v) {
                      if (v != null) setLocal(() => category = v);
                    },
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: contentCtrl,
                    minLines: 4,
                    maxLines: 10,
                    decoration: const InputDecoration(
                      labelText: '要记住的内容',
                      hintText: '重要事实、偏好、约定等（建议精炼，过长会在召回时截断）',
                      border: OutlineInputBorder(),
                      alignLabelWithHint: true,
                    ),
                  ),
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('置顶（优先进入上下文）'),
                    value: pin,
                    onChanged: (v) => setLocal(() => pin = v),
                  ),
                  FilledButton.icon(
                    onPressed: () => Navigator.of(ctx).pop(true),
                    icon: const Icon(Icons.bookmark_add_outlined),
                    label: const Text('保存记忆'),
                  ),
                ],
              ),
            ),
          );
        },
      );
    },
  );

  final title = titleCtrl.text;
  final content = contentCtrl.text;
  titleCtrl.dispose();
  contentCtrl.dispose();
  if (ok != true || !context.mounted) return null;
  try {
    final note = await ref.read(localMemoryProvider.notifier).remember(
          title: title,
          content: content,
          category: category,
          pinned: pin,
        );
    if (context.mounted) {
      final st = ref.read(localMemoryProvider.notifier).status;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            note.syncedToHub
                ? '已记住（上下文 ${st.contextItems}/${st.contextBudgetItems} 条 · ${st.contextRunes} 字）'
                : '已存本机；联网后同步 Hub。上下文 ${st.contextItems}/${st.contextBudgetItems} 条',
          ),
        ),
      );
    }
    return note;
  } on Object catch (e) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('保存失败：$e')),
      );
    }
    return null;
  }
}

/// Full memory management (list / status / compress) — mobile counterpart of GUI panel.
Future<void> showLocalMemoryListSheet(
  BuildContext context,
  WidgetRef ref,
) async {
  await showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (ctx) {
      return const _MemoryManagementSheet();
    },
  );
}

class _MemoryManagementSheet extends ConsumerStatefulWidget {
  const _MemoryManagementSheet();

  @override
  ConsumerState<_MemoryManagementSheet> createState() =>
      _MemoryManagementSheetState();
}

class _MemoryManagementSheetState extends ConsumerState<_MemoryManagementSheet>
    with SingleTickerProviderStateMixin {
  late final TabController _tabs;
  String _filter = '';
  String _category = '';

  @override
  void initState() {
    super.initState();
    _tabs = TabController(length: 3, vsync: this);
  }

  @override
  void dispose() {
    _tabs.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final memories = ref.watch(localMemoryProvider);
    final status = computeLocalMemoryStatus(
      memories.valueOrNull ?? const <LocalMemoryNote>[],
    );
    final height = MediaQuery.sizeOf(context).height * 0.85;

    return SafeArea(
      child: SizedBox(
        height: height,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 4, 12, 0),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '记忆管理',
                      style: Theme.of(context).textTheme.titleLarge?.copyWith(
                            fontWeight: FontWeight.w700,
                          ),
                    ),
                  ),
                  TextButton.icon(
                    onPressed: () => showRememberMemoryDialog(context, ref),
                    icon: const Icon(Icons.add),
                    label: const Text('新建'),
                  ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Text(
                '类似电脑端：分类管理、启用/停用、压缩整理。对话只注入启用且在预算内的条目'
                '（最多 $kLocalMemoryContextMaxItems 条 / $kLocalMemoryContextMaxRunes 字）。',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
              ),
            ),
            TabBar(
              controller: _tabs,
              tabs: const [
                Tab(text: '条目'),
                Tab(text: '状态'),
                Tab(text: '整理'),
              ],
            ),
            Expanded(
              child: TabBarView(
                controller: _tabs,
                children: [
                  _buildListTab(memories, status),
                  _buildStatusTab(status),
                  _buildMaintainTab(status),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildListTab(
    AsyncValue<List<LocalMemoryNote>> memories,
    LocalMemoryStatus status,
  ) {
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 8, 12, 4),
          child: TextField(
            decoration: const InputDecoration(
              isDense: true,
              prefixIcon: Icon(Icons.search),
              hintText: '搜索标题或内容',
              border: OutlineInputBorder(),
            ),
            onChanged: (v) => setState(() => _filter = v.trim().toLowerCase()),
          ),
        ),
        SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          padding: const EdgeInsets.symmetric(horizontal: 12),
          child: Row(
            children: [
              FilterChip(
                label: const Text('全部'),
                selected: _category.isEmpty,
                onSelected: (_) => setState(() => _category = ''),
              ),
              const SizedBox(width: 6),
              for (final e in localMemoryCategories.entries) ...[
                FilterChip(
                  label: Text(e.value),
                  selected: _category == e.key,
                  onSelected: (_) => setState(() => _category = e.key),
                ),
                const SizedBox(width: 6),
              ],
            ],
          ),
        ),
        Expanded(
          child: memories.when(
            data: (items) {
              var list = items;
              if (_category.isNotEmpty) {
                list = list.where((n) => n.category == _category).toList();
              }
              if (_filter.isNotEmpty) {
                list = list
                    .where(
                      (n) =>
                          n.title.toLowerCase().contains(_filter) ||
                          n.content.toLowerCase().contains(_filter),
                    )
                    .toList();
              }
              if (list.isEmpty) {
                return const Center(
                  child: Padding(
                    padding: EdgeInsets.all(24),
                    child: Text(
                      '没有匹配的记忆。点「新建」或在 AI 助手回答下点「记住」。',
                      textAlign: TextAlign.center,
                    ),
                  ),
                );
              }
              return ListView.separated(
                padding: const EdgeInsets.fromLTRB(8, 8, 8, 16),
                itemCount: list.length,
                separatorBuilder: (_, __) => const Divider(height: 1),
                itemBuilder: (context, index) {
                  final note = list[index];
                  return _MemoryTile(note: note);
                },
              );
            },
            loading: () => const Center(child: CircularProgressIndicator()),
            error: (e, _) => Center(child: Text('加载失败：$e')),
          ),
        ),
      ],
    );
  }

  Widget _buildStatusTab(LocalMemoryStatus status) {
    final scheme = Theme.of(context).colorScheme;
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Text('容量与上下文预算', style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 12),
        _StatCard(
          title: '本机存储',
          value: '${status.total} / ${status.storedBudget} 条',
          subtitle: '置顶 ${status.pinned} · 启用 ${status.active} · 停用 ${status.inactive}',
        ),
        const SizedBox(height: 8),
        _StatCard(
          title: '对话注入（本次预算）',
          value: '${status.contextItems} / ${status.contextBudgetItems} 条',
          subtitle:
              '${status.contextRunes} / ${status.contextBudgetRunes} 字 · 填充 ${(status.contextFillRatio * 100).toStringAsFixed(0)}%',
        ),
        const SizedBox(height: 8),
        LinearProgressIndicator(
          value: status.contextFillRatio,
          minHeight: 8,
          borderRadius: BorderRadius.circular(4),
          color: status.contextFillRatio > 0.85
              ? scheme.error
              : scheme.primary,
        ),
        const SizedBox(height: 16),
        Text(
          '说明：停用的记忆仍保存在手机，但不会进入提示词。置顶记忆优先占用预算。'
          '超过预算的条目不会发送给模型，因此不会无限撑爆上下文。',
          style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: scheme.onSurfaceVariant,
              ),
        ),
        const SizedBox(height: 12),
        _StatCard(
          title: 'Hub 同步',
          value: '${status.synced} 条已同步',
          subtitle: '联网时「记住」会写入 Hub 知识库，便于官方助手检索',
        ),
      ],
    );
  }

  Widget _buildMaintainTab(LocalMemoryStatus status) {
    final notifier = ref.read(localMemoryProvider.notifier);
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Text('整理（类似 GUI 压缩）', style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 8),
        Text(
          '去重相同内容，并在超过 $kLocalMemoryMaxStored 条时删除最旧的非置顶记忆。'
          '置顶条目不会被自动删除。',
          style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
        ),
        const SizedBox(height: 16),
        FilledButton.tonalIcon(
          onPressed: () async {
            final result = await notifier.compress();
            if (!mounted) return;
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text(
                  '整理完成：移除 ${result.removedDuplicates} 条，剩余 ${result.remaining} 条'
                  '（其中停用 ${result.inactiveKept}）',
                ),
              ),
            );
            setState(() {});
          },
          icon: const Icon(Icons.compress),
          label: const Text('立即压缩 / 去重'),
        ),
        const SizedBox(height: 12),
        OutlinedButton.icon(
          onPressed: () async {
            await notifier.syncPendingToHub();
            if (!mounted) return;
            ScaffoldMessenger.of(context).showSnackBar(
              const SnackBar(content: Text('已尝试同步未上传的记忆到 Hub')),
            );
          },
          icon: const Icon(Icons.cloud_upload_outlined),
          label: const Text('同步未上传到 Hub'),
        ),
        const SizedBox(height: 12),
        OutlinedButton.icon(
          onPressed: () async {
            final ok = await showDialog<bool>(
              context: context,
              builder: (ctx) => AlertDialog(
                title: const Text('清除停用记忆？'),
                content: const Text('将删除所有未启用（不进上下文）的条目，无法恢复。'),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.pop(ctx, false),
                    child: const Text('取消'),
                  ),
                  FilledButton(
                    onPressed: () => Navigator.pop(ctx, true),
                    child: const Text('清除'),
                  ),
                ],
              ),
            );
            if (ok == true) {
              await notifier.clearInactive();
              if (mounted) setState(() {});
            }
          },
          icon: const Icon(Icons.visibility_off_outlined),
          label: Text('清除停用记忆（${status.inactive}）'),
        ),
        const SizedBox(height: 12),
        OutlinedButton.icon(
          onPressed: () async {
            final ok = await showDialog<bool>(
              context: context,
              builder: (ctx) => AlertDialog(
                title: const Text('清除非置顶记忆？'),
                content: const Text('只保留置顶条目，其它全部删除。'),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.pop(ctx, false),
                    child: const Text('取消'),
                  ),
                  FilledButton(
                    onPressed: () => Navigator.pop(ctx, true),
                    child: const Text('清除'),
                  ),
                ],
              ),
            );
            if (ok == true) {
              await notifier.clearUnpinned();
              if (mounted) setState(() {});
            }
          },
          icon: const Icon(Icons.delete_sweep_outlined),
          label: const Text('只保留置顶'),
        ),
      ],
    );
  }
}

class _StatCard extends StatelessWidget {
  final String title;
  final String value;
  final String subtitle;

  const _StatCard({
    required this.title,
    required this.value,
    required this.subtitle,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: Theme.of(context).textTheme.labelLarge),
            const SizedBox(height: 4),
            Text(value, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 4),
            Text(
              subtitle,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
            ),
          ],
        ),
      ),
    );
  }
}

class _MemoryTile extends ConsumerWidget {
  final LocalMemoryNote note;

  const _MemoryTile({required this.note});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    return ListTile(
      isThreeLine: true,
      leading: Icon(
        note.pinned
            ? Icons.push_pin
            : (note.active ? Icons.bookmark_outline : Icons.visibility_off_outlined),
        color: note.active ? scheme.primary : scheme.onSurfaceVariant,
      ),
      title: Text(
        note.title.trim().isEmpty
            ? (note.content.length > 36
                ? '${note.content.substring(0, 36)}…'
                : note.content)
            : note.title,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: TextStyle(
          decoration: note.active ? null : TextDecoration.lineThrough,
          color: note.active ? null : scheme.onSurfaceVariant,
        ),
      ),
      subtitle: Text(
        '[${localMemoryCategoryLabel(note.category)}] ${note.content}',
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
      ),
      trailing: PopupMenuButton<String>(
        onSelected: (action) async {
          final n = ref.read(localMemoryProvider.notifier);
          switch (action) {
            case 'pin':
              await n.togglePin(note.id);
            case 'active':
              await n.toggleActive(note.id);
            case 'edit':
              await _editNote(context, ref, note);
            case 'delete':
              await n.remove(note.id);
          }
        },
        itemBuilder: (context) => [
          PopupMenuItem(
            value: 'pin',
            child: Text(note.pinned ? '取消置顶' : '置顶'),
          ),
          PopupMenuItem(
            value: 'active',
            child: Text(note.active ? '停用（不进上下文）' : '启用（进入上下文）'),
          ),
          const PopupMenuItem(value: 'edit', child: Text('编辑')),
          const PopupMenuItem(value: 'delete', child: Text('删除')),
        ],
      ),
      onTap: () => _editNote(context, ref, note),
    );
  }

  Future<void> _editNote(
    BuildContext context,
    WidgetRef ref,
    LocalMemoryNote note,
  ) async {
    final titleCtrl = TextEditingController(text: note.title);
    final contentCtrl = TextEditingController(text: note.content);
    var category = note.category;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) {
        return StatefulBuilder(
          builder: (ctx, setLocal) {
            return AlertDialog(
              title: const Text('编辑记忆'),
              content: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    TextField(
                      controller: titleCtrl,
                      decoration: const InputDecoration(labelText: '标题'),
                    ),
                    const SizedBox(height: 8),
                    DropdownButtonFormField<String>(
                      // ignore: deprecated_member_use
                      value: localMemoryCategories.containsKey(category)
                          ? category
                          : 'other',
                      items: [
                        for (final e in localMemoryCategories.entries)
                          DropdownMenuItem(value: e.key, child: Text(e.value)),
                      ],
                      onChanged: (v) {
                        if (v != null) setLocal(() => category = v);
                      },
                      decoration: const InputDecoration(labelText: '分类'),
                    ),
                    const SizedBox(height: 8),
                    TextField(
                      controller: contentCtrl,
                      minLines: 3,
                      maxLines: 8,
                      decoration: const InputDecoration(labelText: '内容'),
                    ),
                  ],
                ),
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(ctx, false),
                  child: const Text('取消'),
                ),
                FilledButton(
                  onPressed: () => Navigator.pop(ctx, true),
                  child: const Text('保存'),
                ),
              ],
            );
          },
        );
      },
    );
    final title = titleCtrl.text;
    final content = contentCtrl.text;
    titleCtrl.dispose();
    contentCtrl.dispose();
    if (ok != true) return;
    await ref.read(localMemoryProvider.notifier).updateNote(
          note.id,
          title: title,
          content: content,
          category: category,
        );
  }
}
