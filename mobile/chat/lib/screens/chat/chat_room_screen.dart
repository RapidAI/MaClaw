import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../models/channel.dart';
import '../../providers/call_provider.dart';
import '../../providers/chat_provider.dart';
import '../../widgets/optimized_message_list.dart';
import '../../widgets/chat_input_bar.dart';
import '../voice/call_screen.dart';
import 'group_settings_screen.dart';

/// The chat room screen for a single channel.
class ChatRoomScreen extends StatefulWidget {
  final Channel channel;
  const ChatRoomScreen({super.key, required this.channel});

  @override
  State<ChatRoomScreen> createState() => _ChatRoomScreenState();
}

class _ChatRoomScreenState extends State<ChatRoomScreen> {
  final _scrollController = ScrollController();
  bool _loadingOlder = false;

  @override
  void initState() {
    super.initState();
    // Load cached messages + sync incremental from server.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      context.read<ChatProvider>().enterChannel(widget.channel.id);
    });
    _scrollController.addListener(_onScroll);
  }

  void _onScroll() {
    if (_loadingOlder) return;
    if (_scrollController.position.pixels <= 50) {
      _loadingOlder = true;
      context.read<ChatProvider>().loadOlderMessages(widget.channel.id).then(
            (_) => _loadingOlder = false,
            onError: (_) => _loadingOlder = false,
          );
    }
  }

  @override
  void dispose() {
    context.read<ChatProvider>().leaveChannel(widget.channel.id);
    _scrollController.dispose();
    super.dispose();
  }

  void _showEditDialog(
      BuildContext context, String messageId, String currentContent) {
    final controller = TextEditingController(text: currentContent);
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Edit message'),
        content: TextField(
          controller: controller,
          autofocus: true,
          maxLines: 3,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () {
              final newContent = controller.text.trim();
              if (newContent.isNotEmpty && newContent != currentContent) {
                context
                    .read<ChatProvider>()
                    .editMessage(widget.channel.id, messageId, newContent);
              }
              Navigator.pop(ctx);
            },
            child: const Text('Save'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.channel.name ?? 'Chat'),
        actions: [
          if (widget.channel.isGroup)
            IconButton(
              icon: const Icon(Icons.people_outline),
              onPressed: () {
                Navigator.of(context).push(MaterialPageRoute(
                  builder: (_) => GroupSettingsScreen(
                    channelId: widget.channel.id,
                    channelName: widget.channel.name ?? '',
                    api: context.read<ChatProvider>().api,
                  ),
                ));
              },
            ),
          IconButton(
            icon: const Icon(Icons.phone_outlined),
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
          Expanded(
            child: Consumer<ChatProvider>(
              builder: (context, provider, _) {
                final messages = provider.messagesFor(widget.channel.id);
                return OptimizedMessageList(
                  messages: messages,
                  currentUserId: provider.currentUserId,
                  scrollController: _scrollController,
                  onRecall: (id) =>
                      provider.recallMessage(widget.channel.id, id),
                  onEdit: (id, content) =>
                      _showEditDialog(context, id, content),
                );
              },
            ),
          ),
          ChatInputBar(
            onSendText: (text) {
              context.read<ChatProvider>().sendTextMessage(widget.channel.id, text);
            },
            onSendImage: (path) {
              context.read<ChatProvider>().sendImageMessage(widget.channel.id, path);
            },
            onSendVoice: (path, durationMs) {
              context.read<ChatProvider>().sendVoiceMessage(widget.channel.id, path, durationMs);
            },
          ),
        ],
      ),
    );
  }
}
