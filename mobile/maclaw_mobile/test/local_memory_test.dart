import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/memory/local_memory_note.dart';

void main() {
  test('buildLocalMemoryContextBlock only includes active, pinned first', () {
    final notes = [
      LocalMemoryNote(
        id: '1',
        title: '普通',
        content: 'A' * 100,
        createdAt: DateTime.utc(2026, 1, 1),
        active: true,
      ),
      LocalMemoryNote(
        id: '2',
        title: '置顶约定',
        content: '客户只接受早班部署',
        createdAt: DateTime.utc(2026, 1, 2),
        pinned: true,
        active: true,
      ),
      LocalMemoryNote(
        id: '3',
        title: '已停用',
        content: '不应出现在上下文',
        createdAt: DateTime.utc(2026, 1, 3),
        active: false,
      ),
    ];
    final block = buildLocalMemoryContextBlock(notes, maxItems: 5);
    expect(block, contains('用户本机记忆'));
    expect(block.indexOf('置顶约定'), lessThan(block.indexOf('普通')));
    expect(block, isNot(contains('不应出现在上下文')));
  });

  test('context budget stops when maxRunes exceeded', () {
    final notes = List.generate(
      20,
      (i) => LocalMemoryNote(
        id: '$i',
        title: 'n$i',
        content: '内容' * 80,
        createdAt: DateTime.utc(2026, 1, i + 1),
        active: true,
      ),
    );
    final block = buildLocalMemoryContextBlock(
      notes,
      maxItems: 20,
      maxRunes: 500,
    );
    expect(block.runes.length, lessThanOrEqualTo(500));
    expect(block.split('\n').where((l) => l.startsWith('-')).length, lessThan(20));
  });

  test('compressLocalMemories dedupes and keeps pinned', () {
    final notes = [
      LocalMemoryNote(
        id: 'a',
        title: 't1',
        content: 'same body',
        createdAt: DateTime.utc(2026, 1, 1),
      ),
      LocalMemoryNote(
        id: 'b',
        title: 't2',
        content: 'same body',
        createdAt: DateTime.utc(2026, 1, 2),
        pinned: true,
      ),
      ...List.generate(
        5,
        (i) => LocalMemoryNote(
          id: 'x$i',
          title: 'x',
          content: 'unique-$i',
          createdAt: DateTime.utc(2026, 1, 3 + i),
        ),
      ),
    ];
    final compressed = compressLocalMemories(notes, maxKeep: 3);
    expect(compressed.any((n) => n.pinned), isTrue);
    expect(compressed.length, lessThanOrEqualTo(3));
    expect(
      compressed.where((n) => n.content == 'same body').length,
      1,
    );
  });

  test('empty notes yield empty context', () {
    expect(buildLocalMemoryContextBlock(const []), isEmpty);
  });

  test('computeLocalMemoryStatus counts inactive separately', () {
    final status = computeLocalMemoryStatus([
      LocalMemoryNote(
        id: '1',
        title: 'a',
        content: 'x',
        createdAt: DateTime.utc(2026),
        active: true,
        pinned: true,
      ),
      LocalMemoryNote(
        id: '2',
        title: 'b',
        content: 'y',
        createdAt: DateTime.utc(2026),
        active: false,
      ),
    ]);
    expect(status.total, 2);
    expect(status.active, 1);
    expect(status.inactive, 1);
    expect(status.pinned, 1);
  });
}
