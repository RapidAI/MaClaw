enum MobileSharedIntentKind { text, link, file, image }

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
      kind == MobileSharedIntentKind.text || kind == MobileSharedIntentKind.link;

  bool get opensDocuments =>
      kind == MobileSharedIntentKind.file || kind == MobileSharedIntentKind.image;

  String get assistantPrompt {
    final text = message?.trim().isNotEmpty == true ? message!.trim() : value;
    if (kind == MobileSharedIntentKind.link) {
      return '请联网查证并总结这个链接，保留来源引用：$text';
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
    final kind = _kindFor(
      value: textCandidate,
      typeName: normalizedType,
      mimeType: normalizedMime,
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

  static MobileSharedIntentKind _kindFor({
    required String value,
    required String typeName,
    required String mimeType,
  }) {
    final lower = value.trim().toLowerCase();
    if (typeName == 'url') {
      return MobileSharedIntentKind.link;
    }
    if (typeName == 'text' || mimeType.startsWith('text/')) {
      if (lower.startsWith('http://') || lower.startsWith('https://')) {
        return MobileSharedIntentKind.link;
      }
      return MobileSharedIntentKind.text;
    }
    if (typeName == 'image' || mimeType.startsWith('image/')) {
      return MobileSharedIntentKind.image;
    }
    return MobileSharedIntentKind.file;
  }
}
