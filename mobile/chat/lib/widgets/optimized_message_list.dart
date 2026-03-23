import 'package:flutter/material.dart';
import '../models/message.dart';
import 'message_bubble.dart';

/// Performance-optimized message list with WeChat-style time separators.
class OptimizedMessageList extends StatelessWidget {
  final List<Message> messages;
  final String currentUserId;
  final ScrollController scrollController;
  final bool showSenderName;
  /// Map of userId → display name for sender name resolution.
  final Map<String, String> memberNames;
  final void Function(String messageId)? onRecall;
  final void Function(String messageId, String content)? onEdit;

  const OptimizedMessageList({
    super.key,
    required this.messages,
    required this.currentUserId,
    required this.scrollController,
    this.showSenderName = false,
    this.memberNames = const {},
    this.onRecall,
    this.onEdit,
  });

  /// Show time separator if gap > 5 minutes from previous message.
  bool _shouldShowTime(int msgIndex) {
    if (msgIndex == 0) return true;
    final curr = messages[msgIndex];
    final prev = messages[msgIndex - 1];
    return curr.createdAt.difference(prev.createdAt).inMinutes.abs() > 5;
  }

  String _formatTime(DateTime dt) {
    final now = DateTime.now();
    final today = DateTime(now.year, now.month, now.day);
    final msgDay = DateTime(dt.year, dt.month, dt.day);
    final time = '${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
    if (msgDay == today) return time;
    final yesterday = today.subtract(const Duration(days: 1));
    if (msgDay == yesterday) return 'Yesterday $time';
    if (dt.year == now.year) {
      return '${dt.month}/${dt.day} $time';
    }
    return '${dt.year}/${dt.month}/${dt.day} $time';
  }

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      controller: scrollController,
      reverse: true,
      itemCount: messages.length,
      addAutomaticKeepAlives: false,
      addRepaintBoundaries: true,
      findChildIndexCallback: (key) {
        if (key is ValueKey<String>) {
          final id = key.value;
          for (int i = messages.length - 1; i >= 0; i--) {
            if (messages[i].id == id) return messages.length - 1 - i;
          }
        }
        return null;
      },
      itemBuilder: (context, index) {
        final msgIndex = messages.length - 1 - index;
        final msg = messages[msgIndex];
        final isMe = msg.senderId == currentUserId;
        final showTime = _shouldShowTime(msgIndex);

        return RepaintBoundary(
          key: ValueKey<String>(msg.id),
          child: Column(
            children: [
              if (showTime)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 8),
                  child: Text(
                    _formatTime(msg.createdAt),
                    style: TextStyle(fontSize: 12, color: Colors.grey[500]),
                  ),
                ),
              MessageBubble(
                message: msg,
                isMe: isMe,
                senderName: memberNames[msg.senderId] ?? msg.senderId,
                showSenderName: showSenderName,
                onRecall: isMe ? onRecall : null,
                onEdit: isMe ? onEdit : null,
              ),
            ],
          ),
        );
      },
    );
  }
}
