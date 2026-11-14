import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';

import '../bots/bot_models.dart';
import '../bots/bots_controller.dart';
import '../bots/widgets.dart';
import '../status/status_screen.dart' show shareInboxFile;

final _roomsProvider = FutureProvider.autoDispose<List<BotRoom>>((ref) async {
  final api = ref.watch(botApiProvider);
  if (api == null) return const [];
  return api.rooms();
});

/// Bot 房间列表 — 桌面 GUI BotRoomsDialog 的手机版：人 + 多个 Bot 的群聊，
/// 每条发言按成员 attention（all/mentions/off）唤醒对应 Bot。
class RoomsScreen extends ConsumerWidget {
  const RoomsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final data = ref.watch(_roomsProvider);
    final bots = ref.watch(botsProvider).bots;
    return Scaffold(
      appBar: AppBar(title: const Text('Bot 房间')),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _openEditor(context, ref, bots, null),
        icon: const Icon(Icons.add),
        label: const Text('新建房间'),
      ),
      body: RefreshIndicator(
        onRefresh: () async {
          ref.invalidate(_roomsProvider);
          try {
            await ref.read(_roomsProvider.future);
          } on Object {
            // Error state renders via data.when(error:).
          }
        },
        child: data.when(
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (error, _) => ListView(
            children: [
              const SizedBox(height: 120),
              Center(child: Text(failureMessage(error))),
            ],
          ),
          data: (rooms) {
            if (rooms.isEmpty) {
              return ListView(
                children: const [
                  SizedBox(height: 140),
                  EmptyState(
                    icon: Icons.groups_outlined,
                    title: '还没有房间',
                    hint: '建一个让你和几个 Bot 群聊',
                  ),
                ],
              );
            }
            return ListView.builder(
              padding: const EdgeInsets.all(12),
              itemCount: rooms.length,
              itemBuilder: (context, index) {
                final room = rooms[index];
                final memberNames = [
                  for (final id in room.members)
                    bots.where((b) => b.id == id).firstOrNull?.title ?? id,
                ].join('、');
                return Card(
                  child: ListTile(
                    leading: CircleAvatar(
                      child: Text(
                        room.name.isEmpty ? '房' : room.name.characters.first,
                      ),
                    ),
                    title: Text(
                      room.name.isEmpty ? '未命名房间' : room.name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    subtitle: Text(
                      memberNames.isEmpty ? '还没有成员' : memberNames,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    trailing: IconButton(
                      icon: const Icon(Icons.tune),
                      tooltip: '编辑房间',
                      onPressed: () => _openEditor(context, ref, bots, room),
                    ),
                    onTap: () => context.push(
                      '/rooms/${Uri.encodeComponent(room.id)}',
                      extra: room,
                    ),
                  ),
                );
              },
            );
          },
        ),
      ),
    );
  }

  Future<void> _openEditor(
    BuildContext context,
    WidgetRef ref,
    List<Bot> bots,
    BotRoom? room,
  ) async {
    final changed = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      builder: (_) => RoomEditorSheet(bots: bots, room: room),
    );
    if (changed == true) ref.invalidate(_roomsProvider);
  }
}

/// 创建/编辑房间：名称、成员勾选、每成员 attention（全部/提及/关闭）。
class RoomEditorSheet extends ConsumerStatefulWidget {
  final List<Bot> bots;
  final BotRoom? room;

  const RoomEditorSheet({super.key, required this.bots, this.room});

  @override
  ConsumerState<RoomEditorSheet> createState() => _RoomEditorSheetState();
}

class _RoomEditorSheetState extends ConsumerState<RoomEditorSheet> {
  late final TextEditingController _nameCtrl;
  late final Set<String> _members;
  late final Map<String, String> _attention;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _nameCtrl = TextEditingController(text: widget.room?.name ?? '');
    _members = {...(widget.room?.members ?? const <String>[])};
    _attention = {...(widget.room?.attention ?? const <String, String>{})};
  }

  @override
  void dispose() {
    _nameCtrl.dispose();
    super.dispose();
  }

  String _attentionOf(String botId) =>
      _attention[botId] == 'mentions' || _attention[botId] == 'off'
      ? _attention[botId]!
      : 'all';

  Future<void> _save() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    setState(() => _saving = true);
    try {
      await api.saveRoom(
        id: widget.room?.id ?? '',
        name: _nameCtrl.text.trim(),
        members: _members.toList(),
        attention: {for (final id in _members) id: _attentionOf(id)},
      );
      if (mounted) Navigator.of(context).pop(true);
    } on Object catch (error) {
      if (mounted) {
        setState(() => _saving = false);
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    }
  }

  Future<void> _delete() async {
    final api = ref.read(botApiProvider);
    if (api == null || widget.room == null) return;
    setState(() => _saving = true);
    try {
      await api.deleteRoom(widget.room!.id);
      if (mounted) Navigator.of(context).pop(true);
    } on Object catch (error) {
      if (mounted) {
        setState(() => _saving = false);
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 16,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            widget.room == null ? '新建房间' : '编辑房间',
            style: theme.textTheme.titleMedium,
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _nameCtrl,
            decoration: const InputDecoration(
              labelText: '房间名',
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 12),
          Text('成员', style: theme.textTheme.titleSmall),
          Flexible(
            child: ListView(
              shrinkWrap: true,
              children: [
                for (final bot in widget.bots)
                  CheckboxListTile(
                    dense: true,
                    contentPadding: EdgeInsets.zero,
                    value: _members.contains(bot.id),
                    onChanged: (v) => setState(() {
                      if (v == true) {
                        _members.add(bot.id);
                      } else {
                        _members.remove(bot.id);
                        _attention.remove(bot.id);
                      }
                    }),
                    title: Text(bot.title),
                    secondary: _members.contains(bot.id)
                        ? DropdownButton<String>(
                            value: _attentionOf(bot.id),
                            underline: const SizedBox.shrink(),
                            items: const [
                              DropdownMenuItem(value: 'all', child: Text('全部')),
                              DropdownMenuItem(
                                value: 'mentions',
                                child: Text('仅@提及'),
                              ),
                              DropdownMenuItem(value: 'off', child: Text('不听')),
                            ],
                            onChanged: (v) =>
                                setState(() => _attention[bot.id] = v!),
                          )
                        : null,
                  ),
              ],
            ),
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              if (widget.room != null)
                TextButton(
                  onPressed: _saving ? null : _delete,
                  child: Text(
                    '删除',
                    style: TextStyle(color: theme.colorScheme.error),
                  ),
                ),
              const Spacer(),
              FilledButton(
                onPressed: _saving ? null : _save,
                child: _saving
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : Text(widget.room == null ? '创建' : '保存'),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// 房间聊天 — `GET /bots/said?room=<rid>` 拉对话，
/// `POST /bot-rooms/{rid}/messages` 发言并唤醒成员 Bot。
class RoomChatScreen extends ConsumerStatefulWidget {
  final BotRoom room;

  const RoomChatScreen({super.key, required this.room});

  @override
  ConsumerState<RoomChatScreen> createState() => _RoomChatScreenState();
}

class _RoomChatScreenState extends ConsumerState<RoomChatScreen> {
  final _inputCtrl = TextEditingController();
  final _scrollCtrl = ScrollController();
  bool _sending = false;

  @override
  void dispose() {
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    final api = ref.read(botApiProvider);
    final text = _inputCtrl.text.trim();
    if (api == null || text.isEmpty || _sending) return;
    setState(() => _sending = true);
    try {
      final woken = await api.postRoomMessage(widget.room.id, text);
      _inputCtrl.clear();
      ref.invalidate(_roomSaidProvider(widget.room.id));
      // Land on the just-sent bubble instead of leaving the scroll where it
      // was — matches every chat client's post-send convention.
      try {
        await ref.read(_roomSaidProvider(widget.room.id).future);
      } on Object {
        // Error state renders via data.when(error:).
      }
      if (mounted && _scrollCtrl.hasClients) {
        _scrollCtrl.animateTo(
          _scrollCtrl.position.maxScrollExtent,
          duration: const Duration(milliseconds: 250),
          curve: Curves.easeOut,
        );
      }
      if (mounted && woken.isNotEmpty) {
        final bots = ref.read(botsProvider).bots;
        final names = [
          for (final id in woken)
            bots.where((b) => b.id == id).firstOrNull?.title ?? id,
        ].join('、');
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('已唤醒：$names')));
      }
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final bots = ref.watch(botsProvider).bots;
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(widget.room.name.isEmpty ? '房间' : widget.room.name),
            Text(
              widget.room.members
                  .map(
                    (id) =>
                        bots.where((b) => b.id == id).firstOrNull?.title ?? id,
                  )
                  .join('、'),
              style: Theme.of(context).textTheme.bodySmall,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ],
        ),
      ),
      body: Column(
        children: [
          Expanded(
            child: _RoomTranscript(
              room: widget.room,
              bots: bots,
              scrollCtrl: _scrollCtrl,
            ),
          ),
          SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 6, 12, 8),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _inputCtrl,
                      minLines: 1,
                      maxLines: 4,
                      decoration: InputDecoration(
                        hintText: '在房间里说话，@Bot名 可点名',
                        filled: true,
                        fillColor: Theme.of(
                          context,
                        ).colorScheme.surfaceContainerHighest,
                        contentPadding: const EdgeInsets.symmetric(
                          horizontal: 14,
                          vertical: 10,
                        ),
                        border: OutlineInputBorder(
                          borderRadius: BorderRadius.circular(22),
                          borderSide: BorderSide.none,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton.filled(
                    onPressed: _sending ? null : _send,
                    icon: const Icon(Icons.send),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

final _roomSaidProvider = FutureProvider.autoDispose
    .family<List<SaidNote>, String>((ref, roomId) async {
      final api = ref.watch(botApiProvider);
      if (api == null) return const [];
      return api.said(room: roomId);
    });

class _RoomTranscript extends ConsumerWidget {
  final BotRoom room;
  final List<Bot> bots;
  final ScrollController scrollCtrl;

  const _RoomTranscript({
    required this.room,
    required this.bots,
    required this.scrollCtrl,
  });

  String _botName(String id) =>
      bots.where((b) => b.id == id).firstOrNull?.title ?? id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return ref
        .watch(_roomSaidProvider(room.id))
        .when(
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (error, _) => Center(child: Text(failureMessage(error))),
          data: (notes) {
            if (notes.isEmpty) {
              return const Center(child: Text('在房间里说点什么，Bot 们会听到'));
            }
            return RefreshIndicator(
              onRefresh: () async {
                ref.invalidate(_roomSaidProvider(room.id));
                try {
                  await ref.read(_roomSaidProvider(room.id).future);
                } on Object {
                  // Error state renders via data.when(error:).
                }
              },
              child: ListView.builder(
                controller: scrollCtrl,
                padding: const EdgeInsets.all(12),
                itemCount: notes.length,
                itemBuilder: (context, index) {
                  final note = notes[index];
                  final mine = note.isMine;
                  return Align(
                    alignment: mine
                        ? Alignment.centerRight
                        : Alignment.centerLeft,
                    child: Container(
                      constraints: BoxConstraints(
                        maxWidth: MediaQuery.of(context).size.width * 0.82,
                      ),
                      margin: const EdgeInsets.only(bottom: 8),
                      padding: const EdgeInsets.symmetric(
                        horizontal: 12,
                        vertical: 8,
                      ),
                      decoration: BoxDecoration(
                        color: mine
                            ? theme.colorScheme.primary
                            : theme.colorScheme.surfaceContainerHighest,
                        borderRadius: BorderRadius.circular(14),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          if (!mine)
                            Padding(
                              padding: const EdgeInsets.only(bottom: 2),
                              child: Text(
                                _botName(note.from),
                                style: theme.textTheme.labelSmall?.copyWith(
                                  color: theme.colorScheme.primary,
                                ),
                              ),
                            ),
                          LinkifiedText(
                            note.text,
                            style: TextStyle(
                              color: mine
                                  ? theme.colorScheme.onPrimary
                                  : theme.colorScheme.onSurface,
                              height: 1.35,
                            ),
                            linkStyle: mine
                                ? TextStyle(
                                    color: theme.colorScheme.onPrimary,
                                    height: 1.35,
                                    fontWeight: FontWeight.w600,
                                    decoration: TextDecoration.underline,
                                    decorationColor:
                                        theme.colorScheme.onPrimary,
                                  )
                                : null,
                          ),
                          if (note.files.isNotEmpty)
                            Wrap(
                              spacing: 6,
                              children: [
                                for (final name in note.files)
                                  ActionChip(
                                    avatar: const Icon(
                                      Icons.attach_file,
                                      size: 14,
                                    ),
                                    label: Text(
                                      name,
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                    ),
                                    onPressed: () => shareInboxFile(
                                      context,
                                      ref,
                                      note.from,
                                      note.id,
                                      name,
                                    ),
                                  ),
                              ],
                            ),
                          if (note.createdAt != null)
                            Padding(
                              padding: const EdgeInsets.only(top: 2),
                              child: Text(
                                DateFormat(
                                  'MM-dd HH:mm',
                                ).format(note.createdAt!.toLocal()),
                                style: TextStyle(
                                  fontSize: 10,
                                  color: mine
                                      ? theme.colorScheme.onPrimary.withValues(
                                          alpha: 0.7,
                                        )
                                      : theme.colorScheme.onSurfaceVariant,
                                ),
                              ),
                            ),
                        ],
                      ),
                    ),
                  );
                },
              ),
            );
          },
        );
  }
}
