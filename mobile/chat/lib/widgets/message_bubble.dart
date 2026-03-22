import 'package:flutter/material.dart';
import 'package:cached_network_image/cached_network_image.dart';
import '../models/message.dart';

/// Renders a single message bubble (text, image, or voice note).
class MessageBubble extends StatelessWidget {
  final Message message;
  final bool isMe;
  final void Function(String messageId)? onRecall;
  final void Function(String messageId, String currentContent)? onEdit;

  const MessageBubble({
    super.key,
    required this.message,
    required this.isMe,
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
      child: Align(
        alignment: isMe ? Alignment.centerRight : Alignment.centerLeft,
        child: Container(
          margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
          constraints: BoxConstraints(
            maxWidth: MediaQuery.of(context).size.width * 0.72,
          ),
          decoration: BoxDecoration(
            color: isMe
                ? Theme.of(context).colorScheme.primary
                : Theme.of(context).colorScheme.surfaceContainerHighest,
            borderRadius: BorderRadius.only(
              topLeft: const Radius.circular(16),
              topRight: const Radius.circular(16),
              bottomLeft: Radius.circular(isMe ? 16 : 4),
              bottomRight: Radius.circular(isMe ? 4 : 16),
            ),
          ),
          child: _buildContent(context),
        ),
      ),
    );
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
                onTap: () {
                  Navigator.pop(ctx);
                  onEdit!(message.id, message.content);
                },
              ),
            if (onRecall != null)
              ListTile(
                leading: const Icon(Icons.undo, color: Colors.red),
                title: const Text('Recall', style: TextStyle(color: Colors.red)),
                onTap: () {
                  Navigator.pop(ctx);
                  onRecall!(message.id);
                },
              ),
          ],
        ),
      ),
    );
  }

  Widget _buildContent(BuildContext context) {
    final textColor = isMe ? Colors.white : Theme.of(context).colorScheme.onSurface;

    switch (message.type) {
      case MessageType.image:
        return _ImageContent(message: message);
      case MessageType.voiceNote:
        return _VoiceNoteContent(message: message, textColor: textColor);
      case MessageType.file:
        return _FileContent(message: message, textColor: textColor);
      case MessageType.text:
      default:
        return Column(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            Text(message.content, style: TextStyle(color: textColor)),
            if (message.sendStatus == SendStatus.sending)
              const Padding(
                padding: EdgeInsets.only(top: 4),
                child: SizedBox(width: 12, height: 12, child: CircularProgressIndicator(strokeWidth: 1.5)),
              ),
            if (message.sendStatus == SendStatus.failed)
              const Padding(
                padding: EdgeInsets.only(top: 4),
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
    return Align(
      alignment: isMe ? Alignment.centerRight : Alignment.centerLeft,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
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
        borderRadius: BorderRadius.circular(8),
        child: CachedNetworkImage(
          imageUrl: thumbUrl,
          width: 200,
          fit: BoxFit.cover,
          placeholder: (_, __) => const SizedBox(
            width: 200,
            height: 150,
            child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
          ),
          errorWidget: (_, __, ___) => const SizedBox(
            width: 200,
            height: 150,
            child: Center(child: Icon(Icons.broken_image, size: 32)),
          ),
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
    // AudioPlayer integration — play from attachment URL.
    final url = widget.message.attachments.isNotEmpty
        ? widget.message.attachments.first.url
        : '';
    if (url.isEmpty) return;

    setState(() => _playing = !_playing);
    // Actual playback handled by a shared AudioPlayer instance
    // injected via Provider in a production app. Keeping UI-only here.
  }

  @override
  Widget build(BuildContext context) {
    final durationMs = widget.message.attachments.isNotEmpty
        ? widget.message.attachments.first.durationMs ?? 0
        : 0;
    final seconds = (durationMs / 1000).ceil();
    return GestureDetector(
      onTap: _togglePlay,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            _playing ? Icons.pause : Icons.play_arrow,
            color: widget.textColor,
            size: 20,
          ),
          const SizedBox(width: 8),
          Text('${seconds}s', style: TextStyle(color: widget.textColor)),
          const SizedBox(width: 8),
          ...List.generate(
            8,
            (i) => Container(
              width: 3,
              height: 6.0 + (i % 3) * 6,
              margin: const EdgeInsets.symmetric(horizontal: 1),
              decoration: BoxDecoration(
                color: widget.textColor.withAlpha(153),
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),
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

/// Full-screen image viewer with pinch-to-zoom.
class _FullScreenImage extends StatelessWidget {
  final String url;
  const _FullScreenImage({required this.url});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: Colors.transparent,
        iconTheme: const IconThemeData(color: Colors.white),
      ),
      body: Center(
        child: InteractiveViewer(
          child: CachedNetworkImage(
            imageUrl: url,
            fit: BoxFit.contain,
            placeholder: (_, __) =>
                const Center(child: CircularProgressIndicator()),
            errorWidget: (_, __, ___) =>
                const Center(child: Icon(Icons.broken_image, color: Colors.white, size: 48)),
          ),
        ),
      ),
    );
  }
}
