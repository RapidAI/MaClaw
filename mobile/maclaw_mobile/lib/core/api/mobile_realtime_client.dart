import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../storage/secure_vault.dart';
import 'official_service.dart';

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
    } finally {
      await channel.sink.close();
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
      await channel.sink.close();
    }
  }

  String encodePing() => jsonEncode({
        'type': 'ping',
        'client': 'maclaw_mobile',
      });
}

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
  bool get documentTask => type == 'document_task';
  bool get digitalEmployeeTask => type == 'digital_employee_task';
  bool get sshSession => type == 'ssh_session';
  bool get sshTask => type == 'ssh_task';
  bool get sshFileOperation => type == 'ssh_file_operation';

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
}
