import 'package:flutter/foundation.dart';
import 'package:uuid/uuid.dart';
import '../models/channel.dart';
import '../models/message.dart';
import '../services/api_client.dart';
import '../services/image_compressor.dart';
import '../services/sync_service.dart';

/// Central state management for chat data.
class ChatProvider extends ChangeNotifier {
  final ApiClient api;
  final SyncService sync;
  final String currentUserId;

  List<Channel> _channels = [];
  final Map<String, List<Message>> _messages = {};
  String? _activeChannelId;

  static const _uuid = Uuid();

  ChatProvider({
    required this.api,
    required this.sync,
    required this.currentUserId,
  }) {
    sync.onNewMessage.listen(_onNewMessage);
  }

  List<Channel> get channels => _channels;
  List<Message> messagesFor(String channelId) => _messages[channelId] ?? [];

  /// Load channel list from server.
  Future<void> loadChannels() async {
    _channels = await api.getChannels();
    notifyListeners();
  }

  /// Enter a channel: load cached + sync incremental.
  Future<void> enterChannel(String channelId) async {
    _activeChannelId = channelId;
    // Instant render from cache.
    _messages[channelId] = await sync.loadCached(channelId);
    notifyListeners();
    // Then sync from server.
    final newMsgs = await sync.syncChannel(channelId);
    if (newMsgs.isNotEmpty) {
      _messages[channelId] = await sync.loadCached(channelId);
      notifyListeners();
    }
  }

  void leaveChannel(String channelId) {
    _activeChannelId = null;
    // Report read receipt.
    final msgs = _messages[channelId];
    if (msgs != null && msgs.isNotEmpty) {
      api.sendReadReceipts([
        {'ch': channelId, 'seq': msgs.last.seq},
      ]);
    }
  }

  /// Load older messages (scroll up).
  Future<void> loadOlderMessages(String channelId) async {
    final msgs = _messages[channelId];
    if (msgs == null || msgs.isEmpty) return;
    final oldest = msgs.first.seq;
    if (oldest <= 1) return;
    final older = await sync.loadOlder(channelId, oldest);
    if (older.isNotEmpty) {
      _messages[channelId] = [...older, ...msgs];
      notifyListeners();
    }
  }

  /// Send a text message with optimistic update.
  Future<void> sendTextMessage(String channelId, String text) async {
    final clientMsgId = _uuid.v4();
    final optimistic = Message(
      id: clientMsgId,
      channelId: channelId,
      seq: 0,
      senderId: currentUserId,
      content: text,
      type: MessageType.text,
      createdAt: DateTime.now(),
      clientMsgId: clientMsgId,
      sendStatus: SendStatus.sending,
    );
    _addMessage(channelId, optimistic);

    try {
      final sent = await api.sendMessage(
        channelId: channelId,
        content: text,
        type: MessageType.text,
        clientMsgId: clientMsgId,
      );
      _replaceOptimistic(channelId, clientMsgId, sent);
    } catch (_) {
      _markFailed(channelId, clientMsgId);
    }
  }

  /// Send an image message.
  Future<void> sendImageMessage(String channelId, String filePath) async {
    final clientMsgId = _uuid.v4();
    try {
      final fileInfo = await api.uploadFile(channelId, filePath, 'image.jpg');
      await api.sendMessage(
        channelId: channelId,
        content: '',
        type: MessageType.image,
        clientMsgId: clientMsgId,
        attachments: [fileInfo],
      );
    } catch (_) {
      _markFailed(channelId, clientMsgId);
    }
  }

  /// Send a voice note message.
  Future<void> sendVoiceMessage(String channelId, String filePath, int durationMs) async {
    final clientMsgId = _uuid.v4();
    try {
      final fileInfo = await api.uploadFile(channelId, filePath, 'voice.m4a');
      fileInfo['duration_ms'] = durationMs;
      fileInfo['type'] = 'voice';
      await api.sendMessage(
        channelId: channelId,
        content: '',
        type: MessageType.voiceNote,
        clientMsgId: clientMsgId,
        attachments: [fileInfo],
      );
    } catch (_) {
      _markFailed(channelId, clientMsgId);
    }
  }

  // ── Internal helpers ──────────────────────────────────────

  /// Recall (delete) a message. Only the sender can recall within time limit.
  Future<bool> recallMessage(String channelId, String messageId) async {
    try {
      await api.recallMessage(channelId, messageId);
      final list = _messages[channelId];
      if (list != null) {
        final idx = list.indexWhere((m) => m.id == messageId);
        if (idx >= 0) {
          list[idx] = Message(
            id: list[idx].id,
            channelId: channelId,
            seq: list[idx].seq,
            senderId: list[idx].senderId,
            content: '',
            createdAt: list[idx].createdAt,
            clientMsgId: list[idx].clientMsgId,
            recalled: true,
          );
          notifyListeners();
        }
      }
      return true;
    } catch (_) {
      return false;
    }
  }

  /// Edit a text message content.
  Future<bool> editMessage(
      String channelId, String messageId, String newContent) async {
    try {
      final updated = await api.editMessage(channelId, messageId, newContent);
      final list = _messages[channelId];
      if (list != null) {
        final idx = list.indexWhere((m) => m.id == messageId);
        if (idx >= 0) {
          list[idx] = updated;
          notifyListeners();
        }
      }
      return true;
    } catch (_) {
      return false;
    }
  }

  void _onNewMessage(Message msg) {
    _addMessage(msg.channelId, msg);
    // Update channel list order.
    final idx = _channels.indexWhere((c) => c.id == msg.channelId);
    if (idx >= 0) {
      final ch = _channels[idx].copyWith(lastSeq: msg.seq, lastMessage: msg);
      _channels.removeAt(idx);
      _channels.insert(0, ch);
      notifyListeners();
    }
  }

  void _addMessage(String channelId, Message msg) {
    _messages.putIfAbsent(channelId, () => []);
    _messages[channelId]!.add(msg);
    notifyListeners();
  }

  void _replaceOptimistic(String channelId, String clientMsgId, Message real) {
    final list = _messages[channelId];
    if (list == null) return;
    final idx = list.indexWhere((m) => m.clientMsgId == clientMsgId);
    if (idx >= 0) {
      list[idx] = real;
      notifyListeners();
    }
  }

  void _markFailed(String channelId, String clientMsgId) {
    final list = _messages[channelId];
    if (list == null) return;
    final idx = list.indexWhere((m) => m.clientMsgId == clientMsgId);
    if (idx >= 0) {
      list[idx] = list[idx].copyWith(sendStatus: SendStatus.failed);
      notifyListeners();
    }
  }
}
