import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';
import '../services/image_compressor.dart';
import 'voice_recorder_sheet.dart';

/// WeChat-style chat input bar: multi-line text area + bottom toolbar + send button.
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
            ListTile(leading: const Icon(Icons.camera_alt), title: const Text('Camera'), onTap: () => Navigator.pop(ctx, ImageSource.camera)),
            ListTile(leading: const Icon(Icons.photo_library), title: const Text('Gallery'), onTap: () => Navigator.pop(ctx, ImageSource.gallery)),
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
        onRecorded: (path, durationMs) { Navigator.pop(ctx); widget.onSendVoice(path, durationMs); },
        onCancel: () => Navigator.pop(ctx),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final bottomPad = MediaQuery.of(context).padding.bottom;
    return Container(
      decoration: BoxDecoration(
        color: const Color(0xFFF7F7F7),
        border: Border(top: BorderSide(color: Colors.grey.shade300, width: 0.5)),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          // Text input area
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 4),
            child: Container(
              constraints: const BoxConstraints(minHeight: 80, maxHeight: 160),
              decoration: BoxDecoration(
                color: Colors.white,
                borderRadius: BorderRadius.circular(8),
                border: Border.all(color: Colors.grey.shade300, width: 0.5),
              ),
              child: TextField(
                controller: _controller,
                maxLines: null,
                textInputAction: TextInputAction.newline,
                style: const TextStyle(fontSize: 16),
                decoration: const InputDecoration(
                  border: InputBorder.none,
                  contentPadding: EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                  hintText: '',
                ),
              ),
            ),
          ),
          // Bottom toolbar
          Padding(
            padding: EdgeInsets.only(left: 4, right: 8, bottom: bottomPad + 4, top: 2),
            child: Row(
              children: [
                _toolBtn(Icons.emoji_emotions_outlined, () {}),
                _toolBtn(Icons.alternate_email, () {}),
                _toolBtn(Icons.folder_outlined, _pickImage),
                _toolBtn(Icons.content_cut, () {}),
                _toolBtn(Icons.radio_button_unchecked, () {}),
                _toolBtn(Icons.mic_none, _showVoiceRecorder),
                const Spacer(),
                SizedBox(
                  height: 34,
                  child: TextButton(
                    onPressed: _showSend ? _send : null,
                    style: TextButton.styleFrom(
                      backgroundColor: _showSend ? const Color(0xFF07C160) : const Color(0xFFE0E0E0),
                      foregroundColor: Colors.white,
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(6)),
                      padding: const EdgeInsets.symmetric(horizontal: 16),
                    ),
                    child: Text('发送', style: TextStyle(fontSize: 14, color: _showSend ? Colors.white : Colors.grey[500])),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _toolBtn(IconData icon, VoidCallback onTap) {
    return IconButton(
      icon: Icon(icon, size: 24, color: Colors.grey[700]),
      onPressed: onTap,
      padding: const EdgeInsets.all(6),
      constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
    );
  }
}
