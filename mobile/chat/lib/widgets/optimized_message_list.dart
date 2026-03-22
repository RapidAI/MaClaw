import 'package:flutter/material.dart';
import '../models/message.dart';
import 'message_bubble.dart';

/// Performance-optimized message list using:
/// - `addAutomaticKeepAlives: false` to reduce memory for off-screen items
/// - `addRepaintBoundaries: true` to isolate repaints
/// - Unique keys per message to avoid unnecessary rebuilds
/// - `findChildIndexCallback` for efficient reordering
class OptimizedMessageList extends StatelessWidget {
  final List<Message> messages;
  final String currentUserId;
  final ScrollController scrollController;
  final void Function(String messageId)? onRecall;
  final void Function(String messageId, String content)? onEdit;

  const OptimizedMessageList({
    super.key,
    required this.messages,
    required this.currentUserId,
    required this.scrollController,
    this.onRecall,
    this.onEdit,
  });

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      controller: scrollController,
      reverse: true,
      itemCount: messages.length,
      addAutomaticKeepAlives: false,
      addRepaintBoundaries: true,
      // Help Flutter find items efficiently when list changes.
      findChildIndexCallback: (key) {
        if (key is ValueKey<String>) {
          final id = key.value;
          // Search from end since reverse list shows newest first.
          for (int i = messages.length - 1; i >= 0; i--) {
            if (messages[i].id == id) return messages.length - 1 - i;
          }
        }
        return null;
      },
      itemBuilder: (context, index) {
        final msg = messages[messages.length - 1 - index];
        final isMe = msg.senderId == currentUserId;
        return RepaintBoundary(
          key: ValueKey<String>(msg.id),
          child: MessageBubble(
            message: msg,
            isMe: isMe,
            onRecall: isMe ? onRecall : null,
            onEdit: isMe ? onEdit : null,
          ),
        );
      },
    );
  }
}
