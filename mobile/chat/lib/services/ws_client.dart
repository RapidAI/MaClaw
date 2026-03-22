import 'dart:async';
import 'dart:convert';
import 'package:web_socket_channel/web_socket_channel.dart';

/// Lightweight WebSocket client that receives server-push hints.
/// All data operations go through HTTP; WS is notification-only.
///
/// Auth protocol:
///  1. Connect to /api/chat/ws
///  2. Send: {"type":"auth","token":"<bearer_token>"}
///  3. Receive: {"type":"auth_ok","user_id":"..."} or {"type":"auth_fail"}
class WsClient {
  final String wsUrl;
  final String Function() tokenProvider;

  WebSocketChannel? _channel;
  Timer? _heartbeatTimer;
  Timer? _reconnectTimer;
  int _reconnectDelay = 1;
  bool _disposed = false;
  bool _authenticated = false;

  final _controller = StreamController<WsEvent>.broadcast();
  Stream<WsEvent> get events => _controller.stream;

  WsClient({required this.wsUrl, required this.tokenProvider});

  void connect() {
    if (_disposed) return;
    _reconnectTimer?.cancel();
    _authenticated = false;

    final uri = Uri.parse(wsUrl);
    _channel = WebSocketChannel.connect(uri);

    _channel!.stream.listen(
      (data) => _onMessage(data as String),
      onDone: _onDisconnected,
      onError: (_) => _onDisconnected(),
    );

    // Send auth frame immediately after connect.
    final token = tokenProvider();
    _channel!.sink.add(jsonEncode({'type': 'auth', 'token': token}));
  }

  void _onMessage(String raw) {
    try {
      final json = jsonDecode(raw) as Map<String, dynamic>;
      final type = json['type'] as String? ?? json['t'] as String? ?? '';

      // Handle auth response.
      if (type == 'auth_ok') {
        _authenticated = true;
        _startHeartbeat();
        _reconnectDelay = 1;
        _controller.add(WsEvent(type: 'auth_ok', data: json));
        return;
      }
      if (type == 'auth_fail') {
        _controller.add(WsEvent(type: 'auth_fail', data: json));
        disconnect();
        return;
      }
      if (type == 'pong') return; // Ignore pong responses.

      _controller.add(WsEvent(type: type, data: json));
    } catch (_) {
      // Ignore malformed messages.
    }
  }

  void _onDisconnected() {
    _heartbeatTimer?.cancel();
    _authenticated = false;
    if (_disposed) return;
    // Exponential backoff: 1s, 2s, 4s, ... max 30s
    _reconnectTimer = Timer(Duration(seconds: _reconnectDelay), connect);
    _reconnectDelay = (_reconnectDelay * 2).clamp(1, 30);
  }

  void _startHeartbeat() {
    _heartbeatTimer?.cancel();
    _heartbeatTimer = Timer.periodic(const Duration(seconds: 30), (_) {
      _channel?.sink.add(jsonEncode({'type': 'ping'}));
    });
  }

  /// Send a typing indicator for a channel.
  void sendTyping(String channelId) {
    if (!_authenticated) return;
    _channel?.sink.add(jsonEncode({'type': 'typing', 'channel_id': channelId}));
  }

  void disconnect() {
    _heartbeatTimer?.cancel();
    _reconnectTimer?.cancel();
    _channel?.sink.close();
    _channel = null;
    _authenticated = false;
  }

  void dispose() {
    if (_disposed) return;
    _disposed = true;
    disconnect();
    _controller.close();
  }
}

class WsEvent {
  final String type;
  final Map<String, dynamic> data;
  const WsEvent({required this.type, required this.data});
}
