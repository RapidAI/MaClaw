import '../security/mobile_redaction.dart';
import '../documents/mobile_document_import.dart';

enum MobileSharedIntentKind { text, link, file, image }

class MobileSharedIntentPayload {
  final String value;
  final String typeName;
  final String? mimeType;
  final String? message;

  const MobileSharedIntentPayload({
    required this.value,
    required this.typeName,
    this.mimeType,
    this.message,
  });
}

class MobileSharedIntent {
  final String id;
  final MobileSharedIntentKind kind;
  final String value;
  final String? mimeType;
  final String? message;
  final DateTime receivedAt;

  const MobileSharedIntent({
    required this.id,
    required this.kind,
    required this.value,
    required this.receivedAt,
    this.mimeType,
    this.message,
  });

  bool get opensAssistant =>
      kind == MobileSharedIntentKind.text ||
      kind == MobileSharedIntentKind.link;

  bool get opensDocuments =>
      kind == MobileSharedIntentKind.file ||
      kind == MobileSharedIntentKind.image;

  String? get sharedUrl {
    final fromMessage = _firstHttpUrl(message ?? '');
    if (fromMessage != null) return fromMessage;
    return _firstHttpUrl(value);
  }

  String get assistantPrompt {
    final rawText =
        message?.trim().isNotEmpty == true ? message!.trim() : value;
    final text = redactMobileSensitiveText(rawText);
    if (kind == MobileSharedIntentKind.link) {
      final url = sharedUrl;
      final safeUrl = url == null ? null : redactMobileSensitiveText(url);
      if (safeUrl != null && text != safeUrl) {
        return '请交给 MaClaw AI 助手处理这个链接，保留来源引用：$safeUrl\n\n分享附带说明：$text';
      }
      return '请交给 MaClaw AI 助手处理这个链接，保留来源引用：${safeUrl ?? text}';
    }
    return text;
  }

  factory MobileSharedIntent.fromMedia({
    required String value,
    required String typeName,
    String? mimeType,
    String? message,
    DateTime? receivedAt,
  }) {
    final normalizedValue = value.trim();
    final normalizedType = typeName.trim().toLowerCase();
    final normalizedMime = (mimeType ?? '').trim().toLowerCase();
    final normalizedMessage = message?.trim();
    final textCandidate = (normalizedMessage?.isNotEmpty == true)
        ? normalizedMessage!
        : normalizedValue;
    final url = _firstHttpUrl(textCandidate);
    final kind = _kindFor(
      value: textCandidate,
      typeName: normalizedType,
      mimeType: normalizedMime,
      extractedUrl: url,
    );
    return MobileSharedIntent(
      id: '${DateTime.now().microsecondsSinceEpoch}:$normalizedValue',
      kind: kind,
      value: normalizedValue,
      mimeType: normalizedMime.isEmpty ? null : normalizedMime,
      message: normalizedMessage?.isEmpty == true ? null : normalizedMessage,
      receivedAt: receivedAt ?? DateTime.now().toUtc(),
    );
  }

  static MobileSharedIntent? fromPayloads(
    Iterable<MobileSharedIntentPayload> payloads, {
    DateTime? receivedAt,
  }) {
    final intents = <MobileSharedIntent>[];
    for (final payload in payloads) {
      final intent = _fromPayload(payload, receivedAt: receivedAt);
      if (intent != null) intents.add(intent);
    }
    for (final intent in intents) {
      if (intent.opensDocuments && _canImportSharedDocumentIntent(intent)) {
        return intent;
      }
    }
    for (final intent in intents) {
      if (!intent.opensDocuments) return intent;
    }
    return intents.firstOrNull;
  }

  static MobileSharedIntent? _fromPayload(
    MobileSharedIntentPayload payload, {
    DateTime? receivedAt,
  }) {
    final value = payload.value.trim();
    final message = payload.message?.trim();
    if (value.isEmpty && (message == null || message.isEmpty)) {
      return null;
    }
    if (value.isEmpty && message != null && message.isNotEmpty) {
      return MobileSharedIntent.fromMedia(
        value: message,
        typeName: 'text',
        mimeType: 'text/plain',
        message: message,
        receivedAt: receivedAt,
      );
    }
    if (_payloadLooksLikeDocument(payload) &&
        !canImportMobileDocumentPath(value) &&
        message != null &&
        message.isNotEmpty) {
      return MobileSharedIntent.fromMedia(
        value: message,
        typeName: 'text',
        mimeType: 'text/plain',
        message: message,
        receivedAt: receivedAt,
      );
    }
    return MobileSharedIntent.fromMedia(
      value: value,
      typeName: payload.typeName,
      mimeType: payload.mimeType,
      message: message,
      receivedAt: receivedAt,
    );
  }

  static MobileSharedIntentKind _kindFor({
    required String value,
    required String typeName,
    required String mimeType,
    String? extractedUrl,
  }) {
    final lower = value.trim().toLowerCase();
    if (typeName == 'url') {
      return MobileSharedIntentKind.link;
    }
    if (typeName == 'file' && !mimeType.startsWith('image/')) {
      return MobileSharedIntentKind.file;
    }
    if (typeName == 'image' || mimeType.startsWith('image/')) {
      return MobileSharedIntentKind.image;
    }
    if (extractedUrl != null) {
      return MobileSharedIntentKind.link;
    }
    if (typeName == 'text' || mimeType.startsWith('text/')) {
      if (lower.startsWith('http://') || lower.startsWith('https://')) {
        return MobileSharedIntentKind.link;
      }
      return MobileSharedIntentKind.text;
    }
    return MobileSharedIntentKind.file;
  }
}

String? _firstHttpUrl(String value) {
  final match = RegExp(r'https?://[^\s<>"\]\)]+').firstMatch(value.trim());
  return match?.group(0);
}

bool _canImportSharedDocumentIntent(MobileSharedIntent intent) {
  if (!intent.opensDocuments) return false;
  return canImportMobileDocumentPath(intent.value);
}

bool _payloadLooksLikeDocument(MobileSharedIntentPayload payload) {
  final typeName = payload.typeName.trim().toLowerCase();
  final mimeType = (payload.mimeType ?? '').trim().toLowerCase();
  return typeName == 'file' ||
      typeName == 'image' ||
      mimeType.startsWith('image/');
}
