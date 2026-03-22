import 'dart:convert';
import 'package:http/http.dart' as http;
import '../models/channel.dart';
import '../models/message.dart';
import '../models/user.dart';

/// HTTP REST client for Hub Chat API.
class ApiClient {
  final String baseUrl;
  String? _token;

  ApiClient({required this.baseUrl});

  void setToken(String token) => _token = token;

  Map<String, String> get _headers => {
        'Content-Type': 'application/json',
        if (_token != null) 'Authorization': 'Bearer $_token',
      };

  /// Exposed for services that need to make direct HTTP calls.
  Map<String, String> get headers => _headers;

  // ── Channels ──────────────────────────────────────────────

  Future<List<Channel>> getChannels() async {
    final resp = await http.get(
      Uri.parse('$baseUrl/api/chat/channels'),
      headers: _headers,
    );
    _checkResponse(resp);
    final list = jsonDecode(resp.body)['channels'] as List<dynamic>;
    return list.map((j) => Channel.fromJson(j as Map<String, dynamic>)).toList();
  }

  Future<Channel> createChannel({
    required ChannelType type,
    String? name,
    required List<String> memberIds,
  }) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/chat/channels'),
      headers: _headers,
      body: jsonEncode({
        'type': type.name,
        'name': name,
        'member_ids': memberIds,
      }),
    );
    _checkResponse(resp);
    return Channel.fromJson(jsonDecode(resp.body) as Map<String, dynamic>);
  }

  // ── Messages ──────────────────────────────────────────────

  Future<List<Message>> getMessages(
    String channelId, {
    int? afterSeq,
    int? beforeSeq,
    int limit = 50,
  }) async {
    final params = <String, String>{'limit': '$limit'};
    if (afterSeq != null) params['after_seq'] = '$afterSeq';
    if (beforeSeq != null) params['before_seq'] = '$beforeSeq';

    final uri = Uri.parse('$baseUrl/api/chat/channels/$channelId/messages')
        .replace(queryParameters: params);
    final resp = await http.get(uri, headers: _headers);
    _checkResponse(resp);
    final list = jsonDecode(resp.body)['messages'] as List<dynamic>;
    return list.map((j) => Message.fromJson(j as Map<String, dynamic>)).toList();
  }

  Future<Message> sendMessage({
    required String channelId,
    required String content,
    required MessageType type,
    required String clientMsgId,
    List<Map<String, dynamic>>? attachments,
  }) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/chat/channels/$channelId/messages'),
      headers: _headers,
      body: jsonEncode({
        'content': content,
        'msg_type': type.index,
        'client_msg_id': clientMsgId,
        'attachments': attachments ?? [],
      }),
    );
    _checkResponse(resp);
    return Message.fromJson(jsonDecode(resp.body) as Map<String, dynamic>);
  }

  // ── Channel Members & Mentions ─────────────────────────────

  Future<List<Map<String, dynamic>>> getChannelMembers(String channelId) async {
    final resp = await http.get(
      Uri.parse('$baseUrl/api/chat/channels/$channelId/members'),
      headers: _headers,
    );
    _checkResponse(resp);
    final list = jsonDecode(resp.body)['members'] as List<dynamic>;
    return list.map((e) => Map<String, dynamic>.from(e as Map)).toList();
  }

  Future<void> addChannelMembers(
      String channelId, List<String> userIds) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/chat/channels/$channelId/members'),
      headers: _headers,
      body: jsonEncode({'user_ids': userIds}),
    );
    _checkResponse(resp);
  }

  Future<void> removeChannelMember(String channelId, String userId) async {
    final resp = await http.delete(
      Uri.parse('$baseUrl/api/chat/channels/$channelId/members/$userId'),
      headers: _headers,
    );
    _checkResponse(resp);
  }

  Future<void> updateChannelName(String channelId, String name) async {
    final resp = await http.put(
      Uri.parse('$baseUrl/api/chat/channels/$channelId'),
      headers: _headers,
      body: jsonEncode({'name': name}),
    );
    _checkResponse(resp);
  }

  // ── Read Receipts ─────────────────────────────────────────

  Future<void> recallMessage(String channelId, String messageId) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/chat/channels/$channelId/messages/$messageId/recall'),
      headers: _headers,
    );
    _checkResponse(resp);
  }

  Future<Message> editMessage(
      String channelId, String messageId, String newContent) async {
    final resp = await http.put(
      Uri.parse('$baseUrl/api/chat/channels/$channelId/messages/$messageId'),
      headers: _headers,
      body: jsonEncode({'content': newContent}),
    );
    _checkResponse(resp);
    return Message.fromJson(jsonDecode(resp.body) as Map<String, dynamic>);
  }

  Future<void> sendReadReceipts(List<Map<String, dynamic>> receipts) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/chat/read-receipts'),
      headers: _headers,
      body: jsonEncode({'receipts': receipts}),
    );
    _checkResponse(resp);
  }

  // ── Files ─────────────────────────────────────────────────

  Future<Map<String, dynamic>> uploadFile(
    String channelId,
    String filePath,
    String filename,
  ) async {
    final request = http.MultipartRequest(
      'POST',
      Uri.parse('$baseUrl/api/chat/files/upload'),
    );
    request.headers.addAll(<String, String>{
      if (_token != null) 'Authorization': 'Bearer $_token',
    });
    request.fields['channel_id'] = channelId;
    request.files.add(await http.MultipartFile.fromPath('file', filePath, filename: filename));

    final streamResp = await request.send();
    final resp = await http.Response.fromStream(streamResp);
    _checkResponse(resp);
    return jsonDecode(resp.body) as Map<String, dynamic>;
  }

  // ── Presence ──────────────────────────────────────────────

  Future<ChatUser> getUserPresence(String userId) async {
    final resp = await http.get(
      Uri.parse('$baseUrl/api/chat/users/$userId/presence'),
      headers: _headers,
    );
    _checkResponse(resp);
    return ChatUser.fromJson(jsonDecode(resp.body) as Map<String, dynamic>);
  }

  // ── Voice Signaling ───────────────────────────────────────

  Future<Map<String, dynamic>> initiateCall({
    required String calleeId,
    String? channelId,
    required int callType,
  }) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/chat/voice/call'),
      headers: _headers,
      body: jsonEncode({
        'callee_id': calleeId,
        'channel_id': channelId,
        'call_type': callType,
      }),
    );
    _checkResponse(resp);
    return jsonDecode(resp.body) as Map<String, dynamic>;
  }

  Future<void> answerCall(String callId, {required bool accept}) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/chat/voice/answer'),
      headers: _headers,
      body: jsonEncode({'call_id': callId, 'accept': accept}),
    );
    _checkResponse(resp);
  }

  Future<void> sendIceCandidate(String callId, Map<String, dynamic> candidate) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/chat/voice/ice'),
      headers: _headers,
      body: jsonEncode({'call_id': callId, 'candidate': candidate}),
    );
    _checkResponse(resp);
  }

  Future<void> hangup(String callId) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/chat/voice/hangup'),
      headers: _headers,
      body: jsonEncode({'call_id': callId}),
    );
    _checkResponse(resp);
  }

  // ── Push ───────────────────────────────────────────────────

  Future<void> registerPushToken(String platform, String token) async {
    final resp = await http.post(
      Uri.parse('$baseUrl/api/chat/push/register'),
      headers: _headers,
      body: jsonEncode({'platform': platform, 'token': token}),
    );
    _checkResponse(resp);
  }

  // ── Voiceprint ─────────────────────────────────────────────

  Future<Map<String, dynamic>> enrollVoiceprint(String filePath, {String label = 'default'}) async {
    final request = http.MultipartRequest(
      'POST',
      Uri.parse('$baseUrl/api/chat/voiceprint/enroll'),
    );
    final authHeaders = <String, String>{
      if (_token != null) 'Authorization': 'Bearer $_token',
    };
    request.headers.addAll(authHeaders);
    request.fields['label'] = label;
    request.files.add(await http.MultipartFile.fromPath('file', filePath, filename: 'audio.wav'));

    final streamResp = await request.send();
    final resp = await http.Response.fromStream(streamResp);
    _checkResponse(resp);
    return jsonDecode(resp.body) as Map<String, dynamic>;
  }

  Future<List<Map<String, dynamic>>> listMyVoiceprints() async {
    final resp = await http.get(
      Uri.parse('$baseUrl/api/chat/voiceprint/list'),
      headers: _headers,
    );
    _checkResponse(resp);
    final list = jsonDecode(resp.body)['voiceprints'] as List<dynamic>;
    return list.map((e) => Map<String, dynamic>.from(e as Map)).toList();
  }

  Future<void> deleteVoiceprint(String id) async {
    final uri = Uri.parse('$baseUrl/api/chat/voiceprint')
        .replace(queryParameters: {'id': id});
    final resp = await http.delete(uri, headers: _headers);
    _checkResponse(resp);
  }

  // ── Helpers ───────────────────────────────────────────────

  void _checkResponse(http.Response resp) {
    if (resp.statusCode >= 400) {
      final body = resp.body.isNotEmpty ? jsonDecode(resp.body) : {};
      throw ApiException(
        statusCode: resp.statusCode,
        message: body['message'] as String?
            ?? body['error'] as String?
            ?? resp.reasonPhrase
            ?? 'Unknown error',
      );
    }
  }
}

class ApiException implements Exception {
  final int statusCode;
  final String message;
  const ApiException({required this.statusCode, required this.message});

  @override
  String toString() => 'ApiException($statusCode): $message';
}
