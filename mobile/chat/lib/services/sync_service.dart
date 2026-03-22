import 'dart:async';
import '../models/message.dart';
import 'api_client.dart';
import 'ws_client.dart';
import 'local_database.dart';

/// Manages incremental message sync between server and local cache.
/// Listens to WS hints and fetches new messages on demand.
class SyncService {
  final ApiClient api;
  final WsClient ws;
  final LocalDatabase db;

  final _messageController = StreamController<Message>.broadcast();
  Stream<Message> get onNewMessage => _messageController.stream;

  final _typingController = StreamController<TypingEvent>.broadcast();
  Stream<TypingEvent> get onTyping => _typingController.stream;

  StreamSubscription<WsEvent>? _wsSub;

  SyncService({required this.api, required this.ws, required this.db});

  void start() {
    _wsSub = ws.events.listen(_handleWsEvent);
  }

  void _handleWsEvent(WsEvent event) {
    switch (event.type) {
      case 'msg':
        final ch = (event.data['ch'] ?? event.data['channel_id']) as String?;
        final seq = (event.data['seq'] as num?)?.toInt();
        if (ch != null && seq != null) {
          _onMessageHint(ch, seq);
        }
        break;
      case 'typing':
        final ch = (event.data['ch'] ?? event.data['channel_id']) as String?;
        final uid = (event.data['uid'] ?? event.data['user_id']) as String?;
        if (ch != null && uid != null) {
          _typingController.add(TypingEvent(
            channelId: ch,
            userId: uid,
            expireSeconds: (event.data['exp'] as num?)?.toInt() ?? 3,
          ));
        }
        break;
      // recall, edit, read, call events handled similarly
    }
  }

  Future<void> _onMessageHint(String channelId, int seq) async {
    final localMaxSeq = await db.getMaxSeq(channelId);
    if (seq <= localMaxSeq) return; // Already have it.

    final messages = await api.getMessages(channelId, afterSeq: localMaxSeq);
    for (final msg in messages) {
      await db.insertMessage(msg);
      _messageController.add(msg);
    }
  }

  /// Pull incremental messages for a channel (e.g. on screen open).
  Future<List<Message>> syncChannel(String channelId) async {
    final localMaxSeq = await db.getMaxSeq(channelId);
    final messages = await api.getMessages(channelId, afterSeq: localMaxSeq);
    for (final msg in messages) {
      await db.insertMessage(msg);
    }
    return messages;
  }

  /// Load cached messages from local DB (instant render).
  Future<List<Message>> loadCached(String channelId, {int limit = 50}) {
    return db.getMessages(channelId, limit: limit);
  }

  /// Load older messages (scroll up).
  Future<List<Message>> loadOlder(String channelId, int beforeSeq) async {
    // Try local first.
    final local = await db.getMessagesBefore(channelId, beforeSeq, limit: 50);
    if (local.isNotEmpty) return local;
    // Fetch from server.
    final remote = await api.getMessages(channelId, beforeSeq: beforeSeq);
    for (final msg in remote) {
      await db.insertMessage(msg);
    }
    return remote;
  }

  void dispose() {
    _wsSub?.cancel();
    _messageController.close();
    _typingController.close();
  }
}

class TypingEvent {
  final String channelId;
  final String userId;
  final int expireSeconds;
  const TypingEvent({
    required this.channelId,
    required this.userId,
    required this.expireSeconds,
  });
}
