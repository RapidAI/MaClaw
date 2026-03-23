import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../models/channel.dart';
import '../../providers/call_provider.dart';
import '../../providers/chat_provider.dart';
import '../../widgets/optimized_message_list.dart';
import '../../widgets/chat_input_bar.dart';
import '../voice/call_screen.dart';
import 'group_settings_screen.dart';

/// WeChat-style chat room screen.
class ChatRoomScreen extends StatefulWidget {
  final Channel channel;
  const ChatRoomScreen({super.key, required this.channel});

  @override
  State<ChatRoomScreen> createState() => _ChatRoomScreenState();
}

class _ChatRoomScreenState extends State<ChatRoomScreen> {
  final _scrollController = ScrollController();
  bool _loadingOlder = false;
  late final ChatProvider _chatProvider;
  bool _channelEntered = false;
  Map<String, String>? _memberNamesCache;

  @override
  void initState() {
    super.initState();
    _scrollController.addListener(_onScroll);
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _chatProvider = context.read<ChatProvider>();
    if (!_channelEntered) {
      _channelEntered = true;
      _chatProvider.enterChannel(widget.channel.id);
    }
  }

  void _onScroll() {
    if (_loadingOlder) return;
    if (_scrollController.position.pixels <= 50) {
      _loadingOlder = true;
      _chatProvider.loadOlderMessages(widget.channel.id).then(
        (_) => _loadingOlder = false,
        onError: (_) => _loadingOlder = false,
      );
    }
  }

  @override
  void dispose() {
    _chatProvider.leaveChannel(widget.channel.id);
    _scrollController.dispose();
    super.dispose();
  }

  void _showEditDialog(BuildContext context, String messageId, String currentContent) {
    showDialog(
      context: context,
      builder: (ctx) => _EditMessageDialog(
        currentContent: currentContent,
        onSave: (newContent) {
          if (newContent.isNotEmpty && newContent != currentContent) {
            context.read<ChatProvider>().editMessage(widget.channel.id, messageId, newContent);
          }
        },
      ),
    );
  }

  /// Build a map of userId → display name from channel members (cached).
  Map<String, String> get _memberNames {
    if (_memberNamesCache != null) return _memberNamesCache!;
    final map = <String, String>{};
    for (final m in widget.channel.members) {
      if (m.nickname != null && m.nickname!.isNotEmpty) {
        map[m.userId] = m.nickname!;
      } else {
        final id = m.userId;
        map[id] = id.contains('@') ? id.split('@').first : id;
      }
    }
    _memberNamesCache = map;
    return map;
  }

  String get _title {
    final name = widget.channel.name ?? 'Chat';
    if (widget.channel.isGroup && widget.channel.members.isNotEmpty) {
      return '$name (${widget.channel.members.length})';
    }
    return name;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: const Color(0xFFEDEDED),
      appBar: AppBar(
        backgroundColor: const Color(0xFFEDEDED),
        foregroundColor: Colors.black,
        elevation: 0,
        scrolledUnderElevation: 0.5,
        titleSpacing: 0,
        title: Text(_title, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
        actions: [
          IconButton(
            icon: const Icon(Icons.chat_bubble_outline, size: 22),
            onPressed: () {},
          ),
          if (widget.channel.isGroup)
            PopupMenuButton<String>(
              icon: const Icon(Icons.more_horiz, size: 22),
              onSelected: (v) {
                if (v == 'members') {
                  Navigator.of(context).push(MaterialPageRoute(
                    builder: (_) => GroupSettingsScreen(
                      channelId: widget.channel.id,
                      channelName: widget.channel.name ?? '',
                      api: context.read<ChatProvider>().api,
                    ),
                  ));
                }
              },
              itemBuilder: (_) => [
                const PopupMenuItem(value: 'members', child: Text('Group Settings')),
              ],
            )
          else
            IconButton(
              icon: const Icon(Icons.more_horiz, size: 22),
              onPressed: () {},
            ),
          IconButton(
            icon: const Icon(Icons.phone_outlined, size: 22),
            onPressed: () {
              final call = context.read<CallProvider>();
              call.startCall(
                calleeId: widget.channel.id,
                calleeName: widget.channel.name ?? 'Unknown',
                channelId: widget.channel.id,
              );
              CallScreen.show(context);
            },
          ),
        ],
      ),
      body: Column(
        children: [
          // Pinned message bar placeholder
          // TODO: wire to real pinned messages API
          // _buildPinnedBar(),
          Expanded(
            child: Consumer<ChatProvider>(
              builder: (context, provider, _) {
                final messages = provider.messagesFor(widget.channel.id);
                return OptimizedMessageList(
                  messages: messages,
                  currentUserId: provider.currentUserId,
                  scrollController: _scrollController,
                  showSenderName: widget.channel.isGroup,
                  memberNames: _memberNames,
                  onRecall: (id) => provider.recallMessage(widget.channel.id, id),
                  onEdit: (id, content) => _showEditDialog(context, id, content),
                );
              },
            ),
          ),
          ChatInputBar(
            onSendText: (text) => context.read<ChatProvider>().sendTextMessage(widget.channel.id, text),
            onSendImage: (path) => context.read<ChatProvider>().sendImageMessage(widget.channel.id, path),
            onSendVoice: (path, durationMs) => context.read<ChatProvider>().sendVoiceMessage(widget.channel.id, path, durationMs),
          ),
        ],
      ),
    );
  }
}

/// Stateful dialog that properly manages its own TextEditingController lifecycle.
class _EditMessageDialog extends StatefulWidget {
  final String currentContent;
  final void Function(String newContent) onSave;
  const _EditMessageDialog({required this.currentContent, required this.onSave});

  @override
  State<_EditMessageDialog> createState() => _EditMessageDialogState();
}

class _EditMessageDialogState extends State<_EditMessageDialog> {
  late final TextEditingController _controller;

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController(text: widget.currentContent);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Edit message'),
      content: TextField(controller: _controller, autofocus: true, maxLines: 3),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
        TextButton(
          onPressed: () {
            widget.onSave(_controller.text.trim());
            Navigator.pop(context);
          },
          child: const Text('Save'),
        ),
      ],
    );
  }
}
