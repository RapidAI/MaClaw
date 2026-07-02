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
    final text = message?.trim().isNotEmpty == true ? message!.trim() : value;
    if (kind == MobileSharedIntentKind.link) {
      final url = sharedUrl;
      if (url != null && text != url) {
        return '请联网查证并总结这个链接，保留来源引用：$url\n\n分享附带说明：$text';
      }
      return '请联网查证并总结这个链接，保留来源引用：${url ?? text}';
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
    for (final payload in payloads) {
      final value = payload.value.trim();
      final message = payload.message?.trim();
      if (value.isEmpty && (message == null || message.isEmpty)) {
        continue;
      }
      return MobileSharedIntent.fromMedia(
        value: value,
        typeName: payload.typeName,
        mimeType: payload.mimeType,
        message: message,
        receivedAt: receivedAt,
      );
    }
    return null;
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
