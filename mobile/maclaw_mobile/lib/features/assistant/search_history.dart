class SearchHistoryEntry {
  final String id;
  final String query;
  final String answerPreview;
  final DateTime createdAt;
  final bool favorite;

  const SearchHistoryEntry({
    required this.id,
    required this.query,
    required this.answerPreview,
    required this.createdAt,
    this.favorite = false,
  });

  factory SearchHistoryEntry.fromJson(Map<String, dynamic> json) {
    return SearchHistoryEntry(
      id: json['id'] as String? ?? '',
      query: json['query'] as String? ?? '',
      answerPreview: json['answer_preview'] as String? ?? '',
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? '') ??
          DateTime.fromMillisecondsSinceEpoch(0),
      favorite: json['favorite'] as bool? ?? false,
    );
  }

  SearchHistoryEntry copyWith({
    String? id,
    String? query,
    String? answerPreview,
    DateTime? createdAt,
    bool? favorite,
  }) {
    return SearchHistoryEntry(
      id: id ?? this.id,
      query: query ?? this.query,
      answerPreview: answerPreview ?? this.answerPreview,
      createdAt: createdAt ?? this.createdAt,
      favorite: favorite ?? this.favorite,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'query': query,
      'answer_preview': answerPreview,
      'created_at': createdAt.toUtc().toIso8601String(),
      'favorite': favorite,
    };
  }
}
