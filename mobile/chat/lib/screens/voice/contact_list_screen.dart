import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../providers/call_provider.dart';
import '../../providers/chat_provider.dart';
import 'call_screen.dart';

/// Lists channels/contacts available for voice calls.
class ContactListScreen extends StatelessWidget {
  const ContactListScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Voice')),
      body: Consumer<ChatProvider>(
        builder: (context, chat, _) {
          final channels = chat.channels;
          if (channels.isEmpty) {
            return const Center(child: Text('No contacts yet'));
          }
          return ListView.builder(
            itemCount: channels.length,
            itemBuilder: (context, index) {
              final ch = channels[index];
              return ListTile(
                leading: CircleAvatar(
                  child: Text(
                    (ch.name ?? 'C').isNotEmpty
                        ? (ch.name ?? 'C')[0].toUpperCase()
                        : 'C',
                  ),
                ),
                title: Text(ch.name ?? 'Channel'),
                subtitle: Text(ch.isGroup ? 'Group call' : '1v1 call'),
                trailing: IconButton(
                  icon: const Icon(Icons.call),
                  onPressed: () => _startCall(context, ch.id, ch.name ?? 'Unknown'),
                ),
              );
            },
          );
        },
      ),
    );
  }

  void _startCall(BuildContext context, String channelId, String name) {
    final callProvider = context.read<CallProvider>();
    callProvider.startCall(
      calleeId: channelId, // In real app, resolve to user ID
      calleeName: name,
      channelId: channelId,
    );
    CallScreen.show(context);
  }
}
