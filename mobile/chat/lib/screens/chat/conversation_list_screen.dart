import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../models/channel.dart';
import '../../providers/chat_provider.dart';
import 'chat_room_screen.dart';

/// Displays the list of conversations (channels).
class ConversationListScreen extends StatefulWidget {
  const ConversationListScreen({super.key});

  @override
  State<ConversationListScreen> createState() => _ConversationListScreenState();
}

class _ConversationListScreenState extends State<ConversationListScreen> {
  @override
  void initState() {
    super.initState();
    // Load channels on first render.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      context.read<ChatProvider>().loadChannels();
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Messages'),
        actions: [
          IconButton(
            icon: const Icon(Icons.group_add_outlined),
            onPressed: () {
              // TODO: navigate to create group screen
            },
          ),
        ],
      ),
      body: Consumer<ChatProvider>(
        builder: (context, provider, _) {
          final channels = provider.channels;
          if (channels.isEmpty) {
            return const Center(child: Text('No conversations yet'));
          }
          return RefreshIndicator(
            onRefresh: () => provider.loadChannels(),
            child: ListView.builder(
              itemCount: channels.length,
              itemBuilder: (context, index) {
                final ch = channels[index];
                return _ChannelTile(channel: ch);
              },
            ),
          );
        },
      ),
    );
  }
}

class _ChannelTile extends StatelessWidget {
  final Channel channel;
  const _ChannelTile({required this.channel});

  @override
  Widget build(BuildContext context) {
    final subtitle = channel.lastMessage?.content ?? '';
    final unread = channel.unreadCount;

    return ListTile(
      leading: CircleAvatar(
        child: Text(
          (channel.name ?? '?').substring(0, 1).toUpperCase(),
        ),
      ),
      title: Text(channel.name ?? 'Direct Message'),
      subtitle: Text(subtitle, maxLines: 1, overflow: TextOverflow.ellipsis),
      trailing: unread > 0
          ? Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
              decoration: BoxDecoration(
                color: Theme.of(context).colorScheme.primary,
                borderRadius: BorderRadius.circular(12),
              ),
              child: Text('$unread',
                  style: const TextStyle(color: Colors.white, fontSize: 12)),
            )
          : null,
      onTap: () {
        Navigator.of(context).push(
          MaterialPageRoute(
              builder: (_) => ChatRoomScreen(channel: channel)),
        );
      },
    );
  }
}
