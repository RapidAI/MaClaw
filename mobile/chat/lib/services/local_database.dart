import 'package:sqflite/sqflite.dart';
import 'package:path/path.dart' as p;
import '../models/message.dart';
import '../models/voice_call.dart';

/// Local SQLite cache for messages, enabling instant UI render.
class LocalDatabase {
  Database? _db;

  Future<Database> get database async {
    _db ??= await _open();
    return _db!;
  }

  Future<Database> _open() async {
    final dbPath = await getDatabasesPath();
    return openDatabase(
      p.join(dbPath, 'maclaw_chat.db'),
      version: 2,
      onCreate: (db, version) async {
        await db.execute('''
          CREATE TABLE messages (
            id TEXT PRIMARY KEY,
            channel_id TEXT NOT NULL,
            seq INTEGER NOT NULL,
            sender_id TEXT NOT NULL,
            content TEXT NOT NULL DEFAULT '',
            msg_type INTEGER NOT NULL DEFAULT 0,
            attachments TEXT NOT NULL DEFAULT '[]',
            created_at INTEGER NOT NULL,
            client_msg_id TEXT,
            recalled INTEGER NOT NULL DEFAULT 0,
            edited_at INTEGER,
            UNIQUE(channel_id, seq)
          )
        ''');
        await db.execute(
          'CREATE INDEX idx_msg_ch_seq ON messages(channel_id, seq)',
        );
        await _createCallsTable(db);
      },
      onUpgrade: (db, oldVersion, newVersion) async {
        if (oldVersion < 2) {
          await _createCallsTable(db);
        }
      },
    );
  }

  Future<void> _createCallsTable(Database db) async {
    await db.execute('''
      CREATE TABLE IF NOT EXISTS calls (
        id TEXT PRIMARY KEY,
        channel_id TEXT,
        caller_id TEXT NOT NULL,
        call_type INTEGER NOT NULL DEFAULT 0,
        status INTEGER NOT NULL DEFAULT 0,
        created_at INTEGER NOT NULL,
        started_at INTEGER,
        ended_at INTEGER
      )
    ''');
    await db.execute(
      'CREATE INDEX IF NOT EXISTS idx_calls_time ON calls(created_at DESC)',
    );
  }

  Future<void> insertMessage(Message msg) async {
    final db = await database;
    await db.insert('messages', msg.toJson(), conflictAlgorithm: ConflictAlgorithm.ignore);
  }

  Future<int> getMaxSeq(String channelId) async {
    final db = await database;
    final result = await db.rawQuery(
      'SELECT MAX(seq) as max_seq FROM messages WHERE channel_id = ?',
      [channelId],
    );
    return (result.first['max_seq'] as int?) ?? 0;
  }

  Future<List<Message>> getMessages(String channelId, {int limit = 50}) async {
    final db = await database;
    final rows = await db.query(
      'messages',
      where: 'channel_id = ?',
      whereArgs: [channelId],
      orderBy: 'seq DESC',
      limit: limit,
    );
    return rows.map((r) => Message.fromJson(r)).toList().reversed.toList();
  }

  Future<List<Message>> getMessagesBefore(
    String channelId,
    int beforeSeq, {
    int limit = 50,
  }) async {
    final db = await database;
    final rows = await db.query(
      'messages',
      where: 'channel_id = ? AND seq < ?',
      whereArgs: [channelId, beforeSeq],
      orderBy: 'seq DESC',
      limit: limit,
    );
    return rows.map((r) => Message.fromJson(r)).toList().reversed.toList();
  }

  Future<void> close() async => _db?.close();

  // ── Call History ──────────────────────────────────────────

  Future<void> insertCall(VoiceCall call) async {
    final db = await database;
    await db.insert('calls', {
      'id': call.id,
      'channel_id': call.channelId,
      'caller_id': call.callerId,
      'call_type': call.type.index,
      'status': call.status.index,
      'created_at': call.createdAt.millisecondsSinceEpoch,
      'started_at': call.startedAt?.millisecondsSinceEpoch,
      'ended_at': call.endedAt?.millisecondsSinceEpoch,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<List<VoiceCall>> getCallHistory({int limit = 50}) async {
    final db = await database;
    final rows = await db.query(
      'calls',
      orderBy: 'created_at DESC',
      limit: limit,
    );
    return rows.map((r) => VoiceCall.fromJson(r)).toList();
  }

  // ── Channel Seq Tracking ──────────────────────────────────

  Future<int> getChannelSeq(String channelId) async => getMaxSeq(channelId);

  Future<void> updateChannelSeq(String channelId, int seq) async {
    // Seq is derived from max message seq, no separate table needed.
  }
}
