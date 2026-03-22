import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';
import '../services/image_compressor.dart';
import 'voice_recorder_sheet.dart';

/// Chat input bar with text, image picker, and voice recorder.
class ChatInputBar extends StatefulWidget {
  final void Function(String text) onSendText;
  final void Function(String path) onSendImage;
  final void Function(String path, int durationMs) onSendVoice;

  const ChatInputBar({
    super.key,
    required this.onSendText,
    required this.onSendImage,
    required this.onSendVoice,
  });

  @override
  State<ChatInputBar> createState() => _ChatInputBarState();
}

class _ChatInputBarState extends State<ChatInputBar> {
  final _controller = TextEditingController();
  final _imagePicker = ImagePicker();
  bool _showSend = false;

  @override
  void initState() {
    super.initState();
    _controller.addListener(() {
      final hasText = _controller.text.trim().isNotEmpty;
      if (hasText != _showSend) setState(() => _showSend = hasText);
    });
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _send() {
    final text = _controller.text.trim();
    if (text.isEmpty) return;
    widget.onSendText(text);
    _controller.clear();
  }

  Future<void> _pickImage() async {
    final source = await showModalBottomSheet<ImageSource>(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.camera_alt),
              title: const Text('Camera'),
              onTap: () => Navigator.pop(ctx, ImageSource.camera),
            ),
            ListTile(
              leading: const Icon(Icons.photo_library),
              title: const Text('Gallery'),
              onTap: () => Navigator.pop(ctx, ImageSource.gallery),
            ),
          ],
        ),
      ),
    );
    if (source == null) return;

    final picked = await _imagePicker.pickImage(source: source);
    if (picked == null) return;

    final compressed = await ImageCompressor.compress(picked.path);
    widget.onSendImage(compressed);
  }

  void _showVoiceRecorder() {
    showModalBottomSheet(
      context: context,
      isDismissible: false,
      builder: (ctx) => VoiceRecorderSheet(
        onRecorded: (path, durationMs) {
          Navigator.pop(ctx);
          widget.onSendVoice(path, durationMs);
        },
        onCancel: () => Navigator.pop(ctx),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: EdgeInsets.only(
        left: 8, right: 8, top: 8,
        bottom: MediaQuery.of(context).padding.bottom + 8,
      ),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surface,
        border: Border(top: BorderSide(color: Colors.grey.shade200)),
      ),
      child: Row(
        children: [
          IconButton(
            icon: const Icon(Icons.image_outlined),
            onPressed: _pickImage,
            tooltip: 'Send image',
          ),
          IconButton(
            icon: const Icon(Icons.mic_outlined),
            onPressed: _showVoiceRecorder,
            tooltip: 'Record voice',
          ),
          Expanded(
            child: TextField(
              controller: _controller,
              textInputAction: TextInputAction.send,
              onSubmitted: (_) => _send(),
              decoration: InputDecoration(
                hintText: 'Type a message...',
                filled: true,
                fillColor: Theme.of(context).colorScheme.surfaceContainerHighest,
                contentPadding:
                    const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(24),
                  borderSide: BorderSide.none,
                ),
              ),
            ),
          ),
          const SizedBox(width: 4),
          if (_showSend)
            IconButton(
              icon: Icon(Icons.send,
                  color: Theme.of(context).colorScheme.primary),
              onPressed: _send,
            ),
        ],
      ),
    );
  }
}
