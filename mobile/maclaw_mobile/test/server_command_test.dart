import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/servers/server_command.dart';

void main() {
  test('round trips server command json', () {
    final entry = ServerCommandEntry(
      id: 'cmd-1',
      command: 'journalctl -u nginx -n 100 --no-pager',
      label: 'journalctl -u nginx',
      favorite: true,
      createdAt: DateTime.utc(2026, 7, 1),
      lastUsedAt: DateTime.utc(2026, 7, 1, 1),
    );

    final restored = ServerCommandEntry.fromJson(entry.toJson());

    expect(restored.id, entry.id);
    expect(restored.command, entry.command);
    expect(restored.label, entry.label);
    expect(restored.favorite, isTrue);
    expect(restored.createdAt, entry.createdAt);
    expect(restored.lastUsedAt, entry.lastUsedAt);
  });

  test('defaults old server commands to non favorite', () {
    final restored = ServerCommandEntry.fromJson({
      'id': 'cmd-1',
      'command': 'df -h',
      'label': 'df',
      'created_at': '2026-07-01T00:00:00Z',
    });

    expect(restored.favorite, isFalse);
    expect(restored.lastUsedAt, restored.createdAt);
  });

  test('copyWith toggles favorite without changing command', () {
    final entry = ServerCommandEntry(
      id: 'cmd-1',
      command: 'df -h',
      label: 'df',
      favorite: false,
      createdAt: DateTime.utc(2026, 7, 1),
      lastUsedAt: DateTime.utc(2026, 7, 1),
    );

    final updated = entry.copyWith(favorite: true);

    expect(updated.command, entry.command);
    expect(updated.favorite, isTrue);
  });
}
