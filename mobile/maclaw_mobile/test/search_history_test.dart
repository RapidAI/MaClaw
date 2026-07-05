import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/assistant/search_history.dart';

void main() {
  test('round trips assistant history json', () {
    final entry = SearchHistoryEntry(
      id: 's1',
      query: 'nginx 502',
      answerPreview: 'check upstream',
      createdAt: DateTime.utc(2026, 7, 1),
      favorite: true,
    );

    final restored = SearchHistoryEntry.fromJson(entry.toJson());

    expect(restored.id, entry.id);
    expect(restored.query, entry.query);
    expect(restored.answerPreview, entry.answerPreview);
    expect(restored.createdAt, entry.createdAt);
    expect(restored.favorite, isTrue);
  });

  test('defaults old assistant history entries to non favorite', () {
    final restored = SearchHistoryEntry.fromJson({
      'id': 's1',
      'query': 'nginx 502',
      'answer_preview': 'check upstream',
      'created_at': '2026-07-01T00:00:00Z',
    });

    expect(restored.favorite, isFalse);
  });

  test('copyWith toggles favorite without changing content', () {
    final entry = SearchHistoryEntry(
      id: 's1',
      query: 'nginx 502',
      answerPreview: 'check upstream',
      createdAt: DateTime.utc(2026, 7, 1),
    );

    final updated = entry.copyWith(favorite: true);

    expect(updated.id, entry.id);
    expect(updated.query, entry.query);
    expect(updated.answerPreview, entry.answerPreview);
    expect(updated.createdAt, entry.createdAt);
    expect(updated.favorite, isTrue);
  });
}
