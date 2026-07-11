import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../storage/secure_vault.dart';
import 'official_service.dart';

// MCP1 binary PTY frames (must match Hub mobile_pty_binary.go).
const int _mcp1TypeIn = 1;
const int _mcp1TypeOut = 2;
const int _mcp1TypeAck = 3;
const int _mcp1FlagRaw = 1 << 0;
const int _mcp1FlagOk = 1 << 1;
const int _mcp1FlagError = 1 << 2;

final mobileRealtimeClientProvider = Provider<MobileRealtimeClient>(
  (ref) => MobileRealtimeClient(
    vault: ref.watch(_mobileRealtimeVaultProvider),
  ),
);

final _mobileRealtimeVaultProvider =
    Provider<SecureVault>((ref) => const SecureVault());

class MobileRealtimeClient {
  final SecureVault _vault;
  final WebSocketChannel Function(Uri uri) _connect;
  final Future<String?> Function()? _readToken;
  final Future<String?> Function()? _readHubUrl;

  MobileRealtimeClient({
    SecureVault? vault,
    WebSocketChannel Function(Uri uri)? connect,
    Future<String?> Function()? readToken,
    Future<String?> Function()? readHubUrl,
  })  : _vault = vault ?? const SecureVault(),
        _connect = connect ?? WebSocketChannel.connect,
        _readToken = readToken,
        _readHubUrl = readHubUrl;

  Future<WebSocketChannel> connect({
    String path = maclawMobileRealtimePath,
  }) async {
    final token = await (_readToken ?? _vault.readToken)();
    final hubUrl = await (_readHubUrl ?? _vault.readHubUrl)();
    if (hubUrl == null || hubUrl.isEmpty) {
      throw StateError('MaClaw Mobile realtime requires a discovered Hub.');
    }
    final uri = Uri.parse(
      maclawHubWebSocketUrl(hubUrl: hubUrl, path: path),
    ).replace(
      queryParameters: token == null || token.isEmpty
          ? null
          : <String, String>{'access_token': token},
    );
    if (!isMaclawHubWebSocketUrl(uri.toString(), hubUrl)) {
      throw UnsupportedError(
        'MaClaw Mobile realtime only supports the discovered Hub.',
      );
    }
    return _connect(uri);
  }

  Future<void> pingOnce({
    String path = maclawMobileRealtimePath,
    Duration timeout = const Duration(seconds: 5),
  }) async {
    final channel = await connect(path: path);
    try {
      await channel.ready.timeout(timeout);
      channel.sink.add(encodePing());
      final pong = Completer<void>();
      final subscription = channel.stream.listen((raw) {
        final event = MobileRealtimeEvent.tryParse(raw);
        if (event?.pong == true && !pong.isCompleted) {
          pong.complete();
        }
      });
      try {
        await pong.future.timeout(timeout);
      } finally {
        await subscription.cancel();
      }
    } finally {
      await _closeChannel(channel);
    }
  }

  Stream<MobileRealtimeEvent> events({
    String path = maclawMobileRealtimePath,
  }) async* {
    final channel = await connect(path: path);
    await channel.ready;
    try {
      await for (final raw in channel.stream) {
        final event = MobileRealtimeEvent.tryParse(raw);
        if (event != null) {
          yield event;
        }
      }
    } finally {
      await _closeChannel(channel);
    }
  }

  Future<void> _closeChannel(WebSocketChannel channel) async {
    try {
      await channel.sink.close();
    } on Object {
      // Closing is best effort; preserve the original connect/stream result.
    }
  }

  String encodePing() => jsonEncode({
        'type': 'ping',
        'client': 'maclaw_mobile',
      });

  /// Advertise binary PTY capability so Hub dual-writes MCP1 pty_out frames.
  String encodeHello() => jsonEncode({
        'type': 'hello',
        'client': 'maclaw_mobile',
        'caps': ['pty_binary', 'pty_data_b64', 'json'],
      });

  /// Encode hub_exec interactive input for the realtime WebSocket.
  ///
  /// Prefers MCP1 binary frames. Falls back to JSON `data_b64` / `input` only
  /// when callers explicitly request [asJson].
  Object encodePtyInput({
    required String sessionId,
    required String input,
    bool raw = false,
    bool asJson = false,
  }) {
    if (!asJson) {
      return encodePtyInputBinary(
        sessionId: sessionId,
        input: input,
        raw: raw,
      );
    }
    final body = <String, dynamic>{
      'type': 'pty_input',
      'client': 'maclaw_mobile',
      'session_id': sessionId.trim(),
    };
    if (raw) {
      body['data_b64'] = base64Encode(utf8.encode(input));
      body['raw'] = true;
    } else {
      body['input'] = input;
    }
    return jsonEncode(body);
  }

  /// MCP1 binary pty_in frame (raw control bytes stay binary on the wire).
  Uint8List encodePtyInputBinary({
    required String sessionId,
    required String input,
    bool raw = false,
  }) {
    final sid = utf8.encode(sessionId.trim());
    final payload = utf8.encode(input);
    final out = ByteData(8 + sid.length + payload.length);
    final bytes = out.buffer.asUint8List();
    bytes[0] = 0x4d; // M
    bytes[1] = 0x43; // C
    bytes[2] = 0x50; // P
    bytes[3] = 0x31; // 1
    bytes[4] = _mcp1TypeIn;
    bytes[5] = raw ? _mcp1FlagRaw : 0;
    out.setUint16(6, sid.length, Endian.big);
    bytes.setRange(8, 8 + sid.length, sid);
    bytes.setRange(8 + sid.length, 8 + sid.length + payload.length, payload);
    return bytes;
  }
}

/// Optional sender bound by [mobileRealtimeBridgeProvider] while the socket is up.
/// Accepts JSON [String] or MCP1 binary [List<int>]/[Uint8List].
typedef MobileRealtimeSender = void Function(Object encoded);

final mobileRealtimeSenderProvider =
    StateProvider<MobileRealtimeSender?>((ref) => null);

/// Whether Hub accepted binary PTY (hello_ack or first binary frame path).
final mobileRealtimeBinaryPtyProvider = StateProvider<bool>((ref) => false);

class MobileRealtimeEvent {
  final String type;
  final String userId;
  final String tenantId;
  final String taskId;
  final String status;
  final DateTime? serverTime;
  final Map<String, dynamic> payload;

  const MobileRealtimeEvent({
    required this.type,
    this.userId = '',
    this.tenantId = '',
    this.taskId = '',
    this.status = '',
    this.serverTime,
    this.payload = const {},
  });

  bool get ready => type == 'ready';
  bool get pong => type == 'pong';
  bool get ptyAck => type == 'pty_ack';
  bool get helloAck => type == 'hello_ack';
  bool get documentTask => type == 'document_task';
  bool get digitalEmployeeTask => type == 'digital_employee_task';
  bool get sshSession => type == 'ssh_session';
  bool get sshTask => type == 'ssh_task';
  bool get sshFileOperation => type == 'ssh_file_operation';
  bool get assistantJob =>
      type == 'assistant_job' || type == 'agent_job';
  bool get binaryFrame => payload['binary_frame'] == true;

  factory MobileRealtimeEvent.fromJson(Map<String, dynamic> json) {
    final nestedPayload =
        json['payload'] ?? json['task'] ?? json['session'] ?? json['operation'];
    final payload = nestedPayload is Map
        ? Map<String, dynamic>.from(nestedPayload)
        : Map<String, dynamic>.from(json);
    final topLevelTaskId = json['task_id'] as String? ?? '';
    final topLevelJobId = json['job_id'] as String? ?? '';
    final topLevelSessionId = json['session_id'] as String? ?? '';
    final topLevelOperationId = json['operation_id'] as String? ?? '';
    if (topLevelTaskId.isNotEmpty) {
      payload.putIfAbsent('task_id', () => topLevelTaskId);
    }
    if (topLevelJobId.isNotEmpty) {
      payload.putIfAbsent('job_id', () => topLevelJobId);
    }
    if (topLevelSessionId.isNotEmpty) {
      payload.putIfAbsent('session_id', () => topLevelSessionId);
    }
    if (topLevelOperationId.isNotEmpty) {
      payload.putIfAbsent('operation_id', () => topLevelOperationId);
    }
    final topLevelStatus = json['status'] as String? ?? '';
    if (topLevelStatus.isNotEmpty) {
      payload.putIfAbsent('status', () => topLevelStatus);
    }
    return MobileRealtimeEvent(
      type: json['type'] as String? ?? '',
      userId: json['user_id'] as String? ?? '',
      tenantId: json['tenant_id'] as String? ?? '',
      taskId: topLevelTaskId.isNotEmpty
          ? topLevelTaskId
          : topLevelJobId.isNotEmpty
              ? topLevelJobId
              : topLevelOperationId.isNotEmpty
                  ? topLevelOperationId
                  : topLevelSessionId,
      status: topLevelStatus,
      serverTime: DateTime.tryParse(json['server_time'] as String? ?? ''),
      payload: payload,
    );
  }

  static MobileRealtimeEvent? tryParse(Object? raw) {
    try {
      if (raw is List<int>) {
        return tryParseBinary(raw);
      }
      final decoded = raw is String ? jsonDecode(raw) : raw;
      if (decoded is! Map) return null;
      final event = MobileRealtimeEvent.fromJson(
        Map<String, dynamic>.from(decoded),
      );
      if (event.type.trim().isEmpty) return null;
      return event;
    } catch (_) {
      return null;
    }
  }

  /// Parse MCP1 binary frames into synthetic JSON-shaped events.
  static MobileRealtimeEvent? tryParseBinary(List<int> raw) {
    if (raw.length < 8) return null;
    if (raw[0] != 0x4d || raw[1] != 0x43 || raw[2] != 0x50 || raw[3] != 0x31) {
      return null;
    }
    final typeByte = raw[4];
    final flags = raw[5];
    final sidLen = (raw[6] << 8) | raw[7];
    if (sidLen < 0 || 8 + sidLen > raw.length) return null;
    final sid = utf8.decode(raw.sublist(8, 8 + sidLen), allowMalformed: true);
    final payloadBytes = raw.sublist(8 + sidLen);
    final text = utf8.decode(payloadBytes, allowMalformed: true);
    switch (typeByte) {
      case _mcp1TypeOut:
        return MobileRealtimeEvent(
          type: 'ssh_session',
          taskId: sid,
          payload: {
            'session_id': sid,
            'output_chunk': text,
            'binary_frame': true,
          },
        );
      case _mcp1TypeAck:
        final ok = (flags & _mcp1FlagOk) != 0;
        final err = (flags & _mcp1FlagError) != 0;
        return MobileRealtimeEvent(
          type: 'pty_ack',
          taskId: sid,
          status: ok ? 'ok' : 'error',
          payload: {
            'session_id': sid,
            'ok': ok && !err,
            'error': err ? text : '',
            'binary': true,
            'binary_frame': true,
          },
        );
      case _mcp1TypeIn:
        // Server should not echo inputs; ignore if seen.
        return null;
      default:
        return null;
    }
  }
}
