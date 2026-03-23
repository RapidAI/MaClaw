import 'package:flutter/material.dart';
import 'package:cached_network_image/cached_network_image.dart';
import '../models/message.dart';

/// WeChat-style message bubble with avatar on the outside.
class MessageBubble extends StatelessWidget {
  final Message message;
  final bool isMe;
  final String? senderName;
  final bool showSenderName;
  final void Function(String messageId)? onRecall;
  final void Function(String messageId, String currentContent)? onEdit;

  const MessageBubble({
    super.key,
    required this.message,
    required this.isMe,
    this.senderName,
    this.showSenderName = false,
    this.onRecall,
    this.onEdit,
  });

  @override
  Widget build(BuildContext context) {
    if (message.recalled) {
      return _RecalledBubble(isMe: isMe);
    }

    return GestureDetector(
      onLongPress: isMe ? () => _showContextMenu(context) : null,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
        child: Row(
          mainAxisAlignment: isMe ? MainAxisAlignment.end : MainAxisAlignment.start,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (!isMe) _buildAvatar(context),
            if (!isMe) const SizedBox(width: 8),
            Flexible(child: _buildBubbleColumn(context)),
            if (isMe) const SizedBox(width: 8),
            if (isMe) _buildAvatar(context),
          ],
        ),
      ),
    );
  }

  Widget _buildAvatar(BuildContext context) {
    final name = senderName ?? message.senderId;
    final initial = name.isNotEmpty ? name[0].toUpperCase() : '?';
    return Container(
      width: 40,
      height: 40,
      decoration: BoxDecoration(
        color: _avatarColor(name),
        borderRadius: BorderRadius.circular(6),
      ),
      alignment: Alignment.center,
      child: Text(initial, style: const TextStyle(color: Colors.white, fontSize: 16, fontWeight: FontWeight.w600)),
    );
  }

  static const _avatarColors = [
    Color(0xFF4CAF50), Color(0xFF2196F3), Color(0xFFFF9800),
    Color(0xFF9C27B0), Color(0xFFE91E63), Color(0xFF00BCD4),
    Color(0xFF795548), Color(0xFF607D8B),
  ];

  Color _avatarColor(String name) {
    return _avatarColors[(name.hashCode & 0x7FFFFFFF) % _avatarColors.length];
  }

  Widget _buildBubbleColumn(BuildContext context) {
    return Column(
      crossAxisAlignment: isMe ? CrossAxisAlignment.end : CrossAxisAlignment.start,
      children: [
        if (showSenderName && !isMe && senderName != null)
          Padding(
            padding: const EdgeInsets.only(bottom: 2, left: 2),
            child: Text(senderName!, style: TextStyle(fontSize: 12, color: Colors.grey[600])),
          ),
        Container(
          constraints: BoxConstraints(maxWidth: MediaQuery.of(context).size.width * 0.65),
          padding: _contentPadding,
          decoration: BoxDecoration(
            color: isMe ? const Color(0xFF95EC69) : Colors.white,
            borderRadius: BorderRadius.circular(6),
          ),
          child: _buildContent(context),
        ),
      ],
    );
  }

  EdgeInsets get _contentPadding {
    if (message.type == MessageType.image) return const EdgeInsets.all(3);
    return const EdgeInsets.symmetric(horizontal: 12, vertical: 10);
  }

  void _showContextMenu(BuildContext context) {
    showModalBottomSheet(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (message.isText && onEdit != null)
              ListTile(
                leading: const Icon(Icons.edit),
                title: const Text('Edit'),
                onTap: () { Navigator.pop(ctx); onEdit!(message.id, message.content); },
              ),
            if (onRecall != null)
              ListTile(
                leading: const Icon(Icons.undo, color: Colors.red),
                title: const Text('Recall', style: TextStyle(color: Colors.red)),
                onTap: () { Navigator.pop(ctx); onRecall!(message.id); },
              ),
          ],
        ),
      ),
    );
  }

  Widget _buildContent(BuildContext context) {
    const textColor = Color(0xFF000000);

    switch (message.type) {
      case MessageType.image:
        return _ImageContent(message: message);
      case MessageType.voiceNote:
        return _VoiceNoteContent(message: message, textColor: textColor);
      case MessageType.file:
        return _FileContent(message: message, textColor: textColor);
      case MessageType.text:
      default:
        return Row(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            Flexible(child: Text(message.content, style: const TextStyle(color: textColor, fontSize: 16))),
            if (message.sendStatus == SendStatus.sending)
              const Padding(
                padding: EdgeInsets.only(left: 4),
                child: SizedBox(width: 12, height: 12, child: CircularProgressIndicator(strokeWidth: 1.5)),
              ),
            if (message.sendStatus == SendStatus.failed)
              const Padding(
                padding: EdgeInsets.only(left: 4),
                child: Icon(Icons.error_outline, size: 14, color: Colors.red),
              ),
          ],
        );
    }
  }
}

class _RecalledBubble extends StatelessWidget {
  final bool isMe;
  const _RecalledBubble({required this.isMe});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 4),
        child: Text(
          isMe ? 'You recalled a message' : 'Message recalled',
          style: TextStyle(color: Colors.grey[500], fontSize: 12, fontStyle: FontStyle.italic),
        ),
      ),
    );
  }
}

class _ImageContent extends StatelessWidget {
  final Message message;
  const _ImageContent({required this.message});

  @override
  Widget build(BuildContext context) {
    final att = message.attachments.isNotEmpty ? message.attachments.first : null;
    final thumbUrl = att?.thumbUrl ?? att?.url ?? '';
    final fullUrl = att?.url ?? '';
    if (thumbUrl.isEmpty) return const Text('[Image]');

    return GestureDetector(
      onTap: fullUrl.isNotEmpty
          ? () => Navigator.of(context).push(MaterialPageRoute(
                builder: (_) => _FullScreenImage(url: fullUrl),
              ))
          : null,
      child: ClipRRect(
        borderRadius: BorderRadius.circular(4),
        child: CachedNetworkImage(
          imageUrl: thumbUrl,
          width: 200,
          fit: BoxFit.cover,
          placeholder: (_, __) => const SizedBox(width: 200, height: 150, child: Center(child: CircularProgressIndicator(strokeWidth: 2))),
          errorWidget: (_, __, ___) => const SizedBox(width: 200, height: 150, child: Center(child: Icon(Icons.broken_image, size: 32))),
        ),
      ),
    );
  }
}

class _VoiceNoteContent extends StatefulWidget {
  final Message message;
  final Color textColor;
  const _VoiceNoteContent({required this.message, required this.textColor});
  @override
  State<_VoiceNoteContent> createState() => _VoiceNoteContentState();
}

class _VoiceNoteContentState extends State<_VoiceNoteContent> {
  bool _playing = false;
  void _togglePlay() {
    final url = widget.message.attachments.isNotEmpty ? widget.message.attachments.first.url : '';
    if (url.isEmpty) return;
    setState(() => _playing = !_playing);
  }

  @override
  Widget build(BuildContext context) {
    final durationMs = widget.message.attachments.isNotEmpty ? widget.message.attachments.first.durationMs ?? 0 : 0;
    final seconds = (durationMs / 1000).ceil();
    return GestureDetector(
      onTap: _togglePlay,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(_playing ? Icons.pause : Icons.play_arrow, color: widget.textColor, size: 20),
          const SizedBox(width: 8),
          Text('${seconds}s', style: TextStyle(color: widget.textColor)),
          const SizedBox(width: 8),
          ...List.generate(8, (i) => Container(
            width: 3, height: 6.0 + (i % 3) * 6,
            margin: const EdgeInsets.symmetric(horizontal: 1),
            decoration: BoxDecoration(color: widget.textColor.withAlpha(153), borderRadius: BorderRadius.circular(2)),
          )),
        ],
      ),
    );
  }
}

class _FileContent extends StatelessWidget {
  final Message message;
  final Color textColor;
  const _FileContent({required this.message, required this.textColor});

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(Icons.insert_drive_file_outlined, color: textColor),
        const SizedBox(width: 8),
        Flexible(child: Text(message.content, style: TextStyle(color: textColor))),
      ],
    );
  }
}

class _FullScreenImage extends StatelessWidget {
  final String url;
  const _FullScreenImage({required this.url});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(backgroundColor: Colors.transparent, iconTheme: const IconThemeData(color: Colors.white)),
      body: Center(
        child: InteractiveViewer(
          child: CachedNetworkImage(
            imageUrl: url, fit: BoxFit.contain,
            placeholder: (_, __) => const Center(child: CircularProgressIndicator()),
            errorWidget: (_, __, ___) => const Center(child: Icon(Icons.broken_image, color: Colors.white, size: 48)),
          ),
        ),
      ),
    );
  }
}
