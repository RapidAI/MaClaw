import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:drift/drift.dart';
import 'package:drift/native.dart';
import 'package:drift_flutter/drift_flutter.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';

import '../settings/app_preferences_model.dart';
import '../api/api_client.dart';
import '../../features/assistant/search_history.dart';
import '../../features/digital_employees/digital_employee_prompt.dart';
import '../../features/documents/document_draft.dart';
import '../../features/servers/server_command.dart';
import '../../features/servers/server_profile.dart';

final mobileLocalStoreProvider = Provider<MobileLocalStore>((ref) {
  final store = MobileLocalStore();
  ref.onDispose(() => unawaited(store.close()));
  return store;
});

Map<String, String> _stringMapFromJson(String raw) {
  try {
    return {
      for (final entry
          in Map<String, dynamic>.from(jsonDecode(raw) as Map).entries)
        entry.key: entry.value.toString(),
    };
  } catch (_) {
    return const {};
  }
}

String _stringMapToJson(Map<String, String> value) {
  return jsonEncode(value);
}

class MobileLocalStore {
  final Future<Directory> Function() _documentsDirectory;
  final QueryExecutor? _executor;
  final bool _useInjectedDirectoryDatabase;
  final bool _migrateLegacyFiles;
  _MobileSqliteDatabase? _database;
  Future<_MobileSqliteDatabase>? _openingDatabase;

  MobileLocalStore({
    Future<Directory> Function()? documentsDirectory,
    QueryExecutor? executor,
  })  : _documentsDirectory =
            documentsDirectory ?? getApplicationDocumentsDirectory,
        _executor = executor,
        _useInjectedDirectoryDatabase = documentsDirectory != null,
        _migrateLegacyFiles = executor == null;

  Future<List<ServerProfile>> loadServerProfiles() async {
    final db = await _db();
    final rows = await db
        .customSelect(
          'SELECT id, name, host, port, username, auth_mode, tag, note '
          'FROM server_profiles ORDER BY name COLLATE NOCASE, host',
        )
        .get();
    return [
      for (final row in rows)
        ServerProfile(
          id: row.read<String>('id'),
          name: row.read<String>('name'),
          host: row.read<String>('host'),
          port: row.read<int>('port'),
          username: row.read<String>('username'),
          authMode: row.read<String>('auth_mode'),
          tag: row.readNullable<String>('tag'),
          note: row.readNullable<String>('note'),
        ),
    ];
  }

  Future<void> saveServerProfiles(List<ServerProfile> profiles) async {
    final db = await _db();
    await db.transaction(() async {
      await db.customStatement('DELETE FROM server_profiles');
      for (final profile in profiles) {
        await _upsertServerProfile(db, profile);
      }
    });
  }

  Future<List<SearchHistoryEntry>> loadSearchHistory() async {
    final db = await _db();
    final rows = await db
        .customSelect(
          'SELECT id, query, answer_preview, created_at, favorite '
          'FROM search_history ORDER BY created_at DESC',
        )
        .get();
    return [
      for (final row in rows)
        SearchHistoryEntry(
          id: row.read<String>('id'),
          query: row.read<String>('query'),
          answerPreview: row.read<String>('answer_preview'),
          createdAt: _readDate(row.read<String>('created_at')),
          favorite: row.read<int>('favorite') == 1,
        ),
    ];
  }

  Future<void> saveSearchHistory(List<SearchHistoryEntry> entries) async {
    final db = await _db();
    await db.transaction(() async {
      await db.customStatement('DELETE FROM search_history');
      for (final entry in entries) {
        await _upsertSearchHistory(db, entry);
      }
    });
  }

  Future<DocumentDraft?> loadLastDocumentDraft() async {
    final db = await _db();
    final rows = await db
        .customSelect(
          'SELECT id, title, template, markdown, updated_at '
          'FROM document_drafts WHERE is_last = 1 '
          'ORDER BY updated_at DESC LIMIT 1',
        )
        .get();
    if (rows.isEmpty) return null;
    final row = rows.first;
    return DocumentDraft(
      id: row.read<String>('id'),
      title: row.read<String>('title'),
      template: documentTemplateFromWire(row.read<String>('template')),
      markdown: row.read<String>('markdown'),
      updatedAt: _readDate(row.read<String>('updated_at')),
    );
  }

  Future<List<DocumentDraft>> loadRecentDocumentDrafts({
    int limit = 20,
  }) async {
    final db = await _db();
    final rows = await db.customSelect(
      'SELECT id, title, template, markdown, updated_at '
      'FROM document_drafts ORDER BY is_last DESC, updated_at DESC LIMIT ?',
      variables: [Variable<int>(limit.clamp(1, 100))],
    ).get();
    return [
      for (final row in rows)
        DocumentDraft(
          id: row.read<String>('id'),
          title: row.read<String>('title'),
          template: documentTemplateFromWire(row.read<String>('template')),
          markdown: row.read<String>('markdown'),
          updatedAt: _readDate(row.read<String>('updated_at')),
        ),
    ];
  }

  Future<void> saveLastDocumentDraft(DocumentDraft draft) async {
    final db = await _db();
    await db.transaction(() async {
      await db.customStatement('UPDATE document_drafts SET is_last = 0');
      await _upsertDocumentDraft(db, draft);
      await db.customStatement(
        'DELETE FROM document_drafts WHERE id NOT IN ('
        'SELECT id FROM document_drafts '
        'ORDER BY is_last DESC, updated_at DESC LIMIT 20)',
      );
    });
  }

  Future<MobileDocumentUploadTask?> loadLastDocumentUploadTask() async {
    final db = await _db();
    final rows = await db
        .customSelect(
          'SELECT task_id, filename, status, draft_id, message '
          'FROM document_upload_tasks ORDER BY updated_at DESC LIMIT 1',
        )
        .get();
    if (rows.isEmpty) return null;
    final row = rows.first;
    return MobileDocumentUploadTask(
      taskId: row.read<String>('task_id'),
      filename: row.read<String>('filename'),
      status: row.read<String>('status'),
      draftId: row.read<String>('draft_id'),
      message: row.read<String>('message'),
    );
  }

  Future<String?> loadLastDocumentUploadPath() async {
    final db = await _db();
    final rows = await db
        .customSelect(
          'SELECT last_upload_path FROM document_upload_tasks '
          'ORDER BY updated_at DESC LIMIT 1',
        )
        .get();
    if (rows.isEmpty) return null;
    return rows.first.readNullable<String>('last_upload_path');
  }

  Future<void> saveLastDocumentUploadTask(
    MobileDocumentUploadTask task, {
    String? sourcePath,
  }) async {
    final db = await _db();
    await db.transaction(() async {
      await db.customStatement('DELETE FROM document_upload_tasks');
      await _upsertDocumentUploadTask(db, task, sourcePath: sourcePath);
    });
  }

  Future<DocumentExportJob?> loadLastDocumentExportJob() async {
    final db = await _db();
    final rows = await db
        .customSelect(
          'SELECT job_id, draft_id, format, status, download_url, message, created_at '
          'FROM document_export_jobs ORDER BY updated_at DESC LIMIT 1',
        )
        .get();
    if (rows.isEmpty) return null;
    final row = rows.first;
    return DocumentExportJob(
      jobId: row.read<String>('job_id'),
      draftId: row.read<String>('draft_id'),
      format: documentExportFormatFromWire(row.read<String>('format')),
      status: row.read<String>('status'),
      downloadUrl: row.read<String>('download_url'),
      message: row.read<String>('message'),
      createdAt: _readDate(row.read<String>('created_at')),
    );
  }

  Future<void> saveLastDocumentExportJob(DocumentExportJob job) async {
    final db = await _db();
    await db.transaction(() async {
      await db.customStatement('DELETE FROM document_export_jobs');
      await _upsertDocumentExportJob(db, job);
    });
  }

  Future<List<ServerCommandEntry>> loadServerCommands() async {
    final db = await _db();
    final rows = await db
        .customSelect(
          'SELECT id, command, label, favorite, created_at, last_used_at '
          'FROM server_commands ORDER BY favorite DESC, last_used_at DESC',
        )
        .get();
    return [
      for (final row in rows)
        ServerCommandEntry(
          id: row.read<String>('id'),
          command: row.read<String>('command'),
          label: row.read<String>('label'),
          favorite: row.read<int>('favorite') == 1,
          createdAt: _readDate(row.read<String>('created_at')),
          lastUsedAt: _readDate(row.read<String>('last_used_at')),
        ),
    ];
  }

  Future<void> saveServerCommands(List<ServerCommandEntry> entries) async {
    final db = await _db();
    await db.transaction(() async {
      await db.customStatement('DELETE FROM server_commands');
      for (final entry in entries) {
        await _upsertServerCommand(db, entry);
      }
    });
  }

  Future<List<DigitalEmployeePromptEntry>> loadDigitalEmployeePrompts() async {
    final db = await _db();
    final rows = await db
        .customSelect(
          'SELECT id, employee_id, prompt, created_at '
          'FROM digital_employee_prompts ORDER BY created_at DESC',
        )
        .get();
    return [
      for (final row in rows)
        DigitalEmployeePromptEntry(
          id: row.read<String>('id'),
          employeeId: row.read<String>('employee_id'),
          prompt: row.read<String>('prompt'),
          createdAt: _readDate(row.read<String>('created_at')),
        ),
    ];
  }

  Future<void> saveDigitalEmployeePrompts(
    List<DigitalEmployeePromptEntry> entries,
  ) async {
    final db = await _db();
    await db.transaction(() async {
      await db.customStatement('DELETE FROM digital_employee_prompts');
      for (final entry in entries) {
        await _upsertDigitalEmployeePrompt(db, entry);
      }
    });
  }

  Future<MobileDigitalEmployeeTask?> loadLastDigitalEmployeeTask() async {
    final tasks = await loadRecentDigitalEmployeeTasks(limit: 1);
    return tasks.isEmpty ? null : tasks.first;
  }

  Future<List<MobileDigitalEmployeeTask>> loadRecentDigitalEmployeeTasks({
    int limit = 20,
  }) async {
    final db = await _db();
    final rows = await db.customSelect(
      'SELECT task_id, employee_id, prompt, task_type, context_json, status, '
      'result, message, claimed_by, updated_at FROM digital_employee_tasks '
      'ORDER BY updated_at DESC LIMIT ?',
      variables: [Variable<int>(limit.clamp(1, 100))],
    ).get();
    return [
      for (final row in rows)
        MobileDigitalEmployeeTask(
          taskId: row.read<String>('task_id'),
          employeeId: row.read<String>('employee_id'),
          prompt: row.read<String>('prompt'),
          taskType: row.read<String>('task_type'),
          context: _stringMapFromJson(row.read<String>('context_json')),
          status: row.read<String>('status'),
          result: row.read<String>('result'),
          message: row.read<String>('message'),
          claimedBy: row.read<String>('claimed_by'),
        ),
    ];
  }

  Future<void> saveLastDigitalEmployeeTask(
    MobileDigitalEmployeeTask task,
  ) async {
    final db = await _db();
    await db.transaction(() async {
      await _upsertDigitalEmployeeTask(db, task);
      await db.customStatement(
        'DELETE FROM digital_employee_tasks WHERE task_id NOT IN ('
        'SELECT task_id FROM digital_employee_tasks '
        'ORDER BY updated_at DESC LIMIT 20)',
      );
    });
  }

  Future<AppPreferences> loadAppPreferences() async {
    final db = await _db();
    final rows = await db
        .customSelect(
          'SELECT theme_mode, language FROM app_preferences WHERE id = 1',
        )
        .get();
    if (rows.isEmpty) return const AppPreferences();
    final row = rows.first;
    return AppPreferences(
      themeMode: themeModeFromWire(row.read<String>('theme_mode')),
      language: appLanguageFromWire(row.read<String>('language')),
    );
  }

  Future<void> saveAppPreferences(AppPreferences preferences) async {
    final db = await _db();
    await db.customStatement(
      'INSERT INTO app_preferences (id, theme_mode, language) '
      'VALUES (1, ?, ?) '
      'ON CONFLICT(id) DO UPDATE SET '
      'theme_mode = excluded.theme_mode, language = excluded.language',
      [
        themeModeWireValue(preferences.themeMode),
        preferences.language,
      ],
    );
  }

  Future<void> clearLocalCache() async {
    final db = await _db();
    await db.transaction(() async {
      for (final table in _cacheTables) {
        await db.customStatement('DELETE FROM $table');
      }
    });
    for (final file in await _legacyJsonFiles()) {
      if (await file.exists()) {
        await file.delete();
      }
    }
  }

  Future<void> clearLocalWorkCache() async {
    final db = await _db();
    await db.transaction(() async {
      for (final table in _workCacheTables) {
        await db.customStatement('DELETE FROM $table');
      }
    });
    for (final file in await _legacyWorkCacheFiles()) {
      if (await file.exists()) {
        await file.delete();
      }
    }
  }

  Future<void> clearServerProfiles() async {
    final db = await _db();
    await db.customStatement('DELETE FROM server_profiles');
    final file = await _serverProfilesFile();
    if (await file.exists()) {
      await file.delete();
    }
  }

  Future<void> close() async {
    final opening = _openingDatabase;
    _openingDatabase = null;
    if (opening != null && _database == null) {
      try {
        _database = await opening;
      } catch (_) {
        // Opening already failed; there is no database handle to close.
      }
    }
    await _database?.close();
    _database = null;
  }

  Future<_MobileSqliteDatabase> _db() async {
    final existing = _database;
    if (existing != null) return existing;
    final opening = _openingDatabase;
    if (opening != null) return opening;
    final future = _openDatabase();
    _openingDatabase = future;
    try {
      final db = await future;
      _database = db;
      return db;
    } finally {
      if (identical(_openingDatabase, future)) {
        _openingDatabase = null;
      }
    }
  }

  Future<_MobileSqliteDatabase> _openDatabase() async {
    final executor = _executor ?? await _openExecutor();
    final db = _MobileSqliteDatabase(executor);
    await _initializeDatabase(db);
    if (_migrateLegacyFiles) {
      await _migrateLegacyJsonCache(db);
    }
    return db;
  }

  Future<QueryExecutor> _openExecutor() async {
    if (!_useInjectedDirectoryDatabase) {
      return driftDatabase(name: 'maclaw_mobile');
    }
    final dir = await _mobileCacheDirectory();
    await dir.create(recursive: true);
    return NativeDatabase.createInBackground(
      File('${dir.path}/maclaw_mobile.sqlite'),
    );
  }

  Future<void> _initializeDatabase(_MobileSqliteDatabase db) async {
    await db.customStatement('PRAGMA foreign_keys = ON');
    await db.customStatement(
      'CREATE TABLE IF NOT EXISTS mobile_meta ('
      'key TEXT PRIMARY KEY, value TEXT NOT NULL)',
    );
    await db.customStatement(
      'CREATE TABLE IF NOT EXISTS server_profiles ('
      'id TEXT PRIMARY KEY, name TEXT NOT NULL, host TEXT NOT NULL, '
      'port INTEGER NOT NULL, username TEXT NOT NULL, auth_mode TEXT NOT NULL, '
      'tag TEXT, note TEXT)',
    );
    await db.customStatement(
      'CREATE TABLE IF NOT EXISTS search_history ('
      'id TEXT PRIMARY KEY, query TEXT NOT NULL, '
      'answer_preview TEXT NOT NULL, created_at TEXT NOT NULL, '
      'favorite INTEGER NOT NULL DEFAULT 0)',
    );
    await db.customStatement(
      'CREATE TABLE IF NOT EXISTS document_drafts ('
      'id TEXT PRIMARY KEY, title TEXT NOT NULL, template TEXT NOT NULL, '
      'markdown TEXT NOT NULL, updated_at TEXT NOT NULL, '
      'is_last INTEGER NOT NULL DEFAULT 1)',
    );
    await db.customStatement(
      'CREATE TABLE IF NOT EXISTS document_upload_tasks ('
      'task_id TEXT PRIMARY KEY, filename TEXT NOT NULL, '
      'status TEXT NOT NULL, draft_id TEXT NOT NULL, message TEXT NOT NULL, '
      'last_upload_path TEXT, updated_at TEXT NOT NULL)',
    );
    await db.customStatement(
      'CREATE TABLE IF NOT EXISTS document_export_jobs ('
      'job_id TEXT PRIMARY KEY, draft_id TEXT NOT NULL, format TEXT NOT NULL, '
      'status TEXT NOT NULL, download_url TEXT NOT NULL, message TEXT NOT NULL DEFAULT "", '
      'created_at TEXT NOT NULL, updated_at TEXT NOT NULL)',
    );
    await _ensureColumn(
      db,
      table: 'document_export_jobs',
      column: 'message',
      definition: 'TEXT NOT NULL DEFAULT ""',
    );
    await db.customStatement(
      'CREATE TABLE IF NOT EXISTS server_commands ('
      'id TEXT PRIMARY KEY, command TEXT NOT NULL, label TEXT NOT NULL, '
      'favorite INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, '
      'last_used_at TEXT NOT NULL)',
    );
    await db.customStatement(
      'CREATE TABLE IF NOT EXISTS digital_employee_prompts ('
      'id TEXT PRIMARY KEY, employee_id TEXT NOT NULL, prompt TEXT NOT NULL, '
      'created_at TEXT NOT NULL)',
    );
    await db.customStatement(
      'CREATE TABLE IF NOT EXISTS digital_employee_tasks ('
      'task_id TEXT PRIMARY KEY, employee_id TEXT NOT NULL, '
      'prompt TEXT NOT NULL, status TEXT NOT NULL, result TEXT NOT NULL, '
      'message TEXT NOT NULL DEFAULT "", '
      'task_type TEXT NOT NULL DEFAULT "general", '
      'context_json TEXT NOT NULL DEFAULT "{}", '
      'claimed_by TEXT NOT NULL, updated_at TEXT NOT NULL)',
    );
    await _ensureColumn(
      db,
      table: 'digital_employee_tasks',
      column: 'message',
      definition: 'TEXT NOT NULL DEFAULT ""',
    );
    await _ensureColumn(
      db,
      table: 'digital_employee_tasks',
      column: 'task_type',
      definition: 'TEXT NOT NULL DEFAULT "general"',
    );
    await _ensureColumn(
      db,
      table: 'digital_employee_tasks',
      column: 'context_json',
      definition: 'TEXT NOT NULL DEFAULT "{}"',
    );
    await db.customStatement(
      'CREATE TABLE IF NOT EXISTS app_preferences ('
      'id INTEGER PRIMARY KEY CHECK(id = 1), theme_mode TEXT NOT NULL, '
      'language TEXT NOT NULL)',
    );
  }

  Future<void> _migrateLegacyJsonCache(_MobileSqliteDatabase db) async {
    final migrated = await db
        .customSelect(
          "SELECT value FROM mobile_meta WHERE key = 'legacy_json_migrated'",
        )
        .get();
    if (migrated.isNotEmpty) return;

    await db.transaction(() async {
      await _migrateListFile<ServerProfile>(
        await _serverProfilesFile(),
        (json) => ServerProfile.fromJson(json),
        (profile) => _upsertServerProfile(db, profile),
      );
      await _migrateListFile<SearchHistoryEntry>(
        await _searchHistoryFile(),
        (json) => SearchHistoryEntry.fromJson(json),
        (entry) => _upsertSearchHistory(db, entry),
      );
      await _migrateListFile<ServerCommandEntry>(
        await _serverCommandsFile(),
        (json) => ServerCommandEntry.fromJson(json),
        (entry) => _upsertServerCommand(db, entry),
      );
      await _migrateListFile<DigitalEmployeePromptEntry>(
        await _digitalEmployeePromptsFile(),
        (json) => DigitalEmployeePromptEntry.fromJson(json),
        (entry) => _upsertDigitalEmployeePrompt(db, entry),
      );
      final draftFile = await _lastDocumentDraftFile();
      final draftJson = await _readJsonMap(draftFile);
      if (draftJson != null) {
        await _upsertDocumentDraft(db, DocumentDraft.fromJson(draftJson));
      }
      final preferencesFile = await _appPreferencesFile();
      final preferencesJson = await _readJsonMap(preferencesFile);
      if (preferencesJson != null) {
        await _upsertAppPreferences(
          db,
          AppPreferences.fromJson(preferencesJson),
        );
      }
      await db.customStatement(
        'INSERT OR REPLACE INTO mobile_meta (key, value) VALUES (?, ?)',
        const [
          'legacy_json_migrated',
          'true',
        ],
      );
    });
  }

  Future<void> _migrateListFile<T>(
    File file,
    T Function(Map<String, dynamic>) decode,
    Future<void> Function(T) save,
  ) async {
    if (!await file.exists()) return;
    final raw = await file.readAsString();
    if (raw.trim().isEmpty) return;
    final data = jsonDecode(raw) as List;
    for (final item in data) {
      await save(decode(Map<String, dynamic>.from(item as Map)));
    }
  }

  Future<Map<String, dynamic>?> _readJsonMap(File file) async {
    if (!await file.exists()) return null;
    final raw = await file.readAsString();
    if (raw.trim().isEmpty) return null;
    return Map<String, dynamic>.from(jsonDecode(raw) as Map);
  }

  Future<void> _upsertServerProfile(
    _MobileSqliteDatabase db,
    ServerProfile profile,
  ) {
    return db.customStatement(
      'INSERT INTO server_profiles '
      '(id, name, host, port, username, auth_mode, tag, note) '
      'VALUES (?, ?, ?, ?, ?, ?, ?, ?) '
      'ON CONFLICT(id) DO UPDATE SET '
      'name = excluded.name, host = excluded.host, port = excluded.port, '
      'username = excluded.username, auth_mode = excluded.auth_mode, '
      'tag = excluded.tag, note = excluded.note',
      [
        profile.id,
        profile.name,
        profile.host,
        profile.port,
        profile.username,
        profile.authMode,
        profile.tag,
        profile.note,
      ],
    );
  }

  Future<void> _upsertSearchHistory(
    _MobileSqliteDatabase db,
    SearchHistoryEntry entry,
  ) {
    return db.customStatement(
      'INSERT INTO search_history '
      '(id, query, answer_preview, created_at, favorite) '
      'VALUES (?, ?, ?, ?, ?) '
      'ON CONFLICT(id) DO UPDATE SET '
      'query = excluded.query, answer_preview = excluded.answer_preview, '
      'created_at = excluded.created_at, favorite = excluded.favorite',
      [
        entry.id,
        entry.query,
        entry.answerPreview,
        _dateWireValue(entry.createdAt),
        entry.favorite ? 1 : 0,
      ],
    );
  }

  Future<void> _upsertDocumentDraft(
    _MobileSqliteDatabase db,
    DocumentDraft draft,
  ) {
    return db.customStatement(
      'INSERT INTO document_drafts '
      '(id, title, template, markdown, updated_at, is_last) '
      'VALUES (?, ?, ?, ?, ?, 1) '
      'ON CONFLICT(id) DO UPDATE SET '
      'title = excluded.title, template = excluded.template, '
      'markdown = excluded.markdown, updated_at = excluded.updated_at, '
      'is_last = 1',
      [
        draft.id,
        draft.title,
        documentTemplateWireValue(draft.template),
        draft.markdown,
        _dateWireValue(draft.updatedAt),
      ],
    );
  }

  Future<void> _upsertDocumentUploadTask(
    _MobileSqliteDatabase db,
    MobileDocumentUploadTask task, {
    String? sourcePath,
  }) {
    return db.customStatement(
      'INSERT INTO document_upload_tasks '
      '(task_id, filename, status, draft_id, message, last_upload_path, updated_at) '
      'VALUES (?, ?, ?, ?, ?, ?, ?) '
      'ON CONFLICT(task_id) DO UPDATE SET '
      'filename = excluded.filename, status = excluded.status, '
      'draft_id = excluded.draft_id, message = excluded.message, '
      'last_upload_path = excluded.last_upload_path, '
      'updated_at = excluded.updated_at',
      [
        task.taskId,
        task.filename,
        task.status,
        task.draftId,
        task.message,
        sourcePath,
        _dateWireValue(DateTime.now()),
      ],
    );
  }

  Future<void> _upsertDocumentExportJob(
    _MobileSqliteDatabase db,
    DocumentExportJob job,
  ) {
    return db.customStatement(
      'INSERT INTO document_export_jobs '
      '(job_id, draft_id, format, status, download_url, message, created_at, updated_at) '
      'VALUES (?, ?, ?, ?, ?, ?, ?, ?) '
      'ON CONFLICT(job_id) DO UPDATE SET '
      'draft_id = excluded.draft_id, format = excluded.format, '
      'status = excluded.status, download_url = excluded.download_url, '
      'message = excluded.message, '
      'created_at = excluded.created_at, updated_at = excluded.updated_at',
      [
        job.jobId,
        job.draftId,
        documentExportFormatWireValue(job.format),
        job.status,
        job.downloadUrl,
        job.message,
        _dateWireValue(job.createdAt),
        _dateWireValue(DateTime.now()),
      ],
    );
  }

  Future<void> _ensureColumn(
    _MobileSqliteDatabase db, {
    required String table,
    required String column,
    required String definition,
  }) async {
    final columns = await db.customSelect('PRAGMA table_info($table)').get();
    final exists = columns.any((row) => row.read<String>('name') == column);
    if (exists) return;
    await db
        .customStatement('ALTER TABLE $table ADD COLUMN $column $definition');
  }

  Future<void> _upsertServerCommand(
    _MobileSqliteDatabase db,
    ServerCommandEntry entry,
  ) {
    return db.customStatement(
      'INSERT INTO server_commands '
      '(id, command, label, favorite, created_at, last_used_at) '
      'VALUES (?, ?, ?, ?, ?, ?) '
      'ON CONFLICT(id) DO UPDATE SET '
      'command = excluded.command, label = excluded.label, '
      'favorite = excluded.favorite, created_at = excluded.created_at, '
      'last_used_at = excluded.last_used_at',
      [
        entry.id,
        entry.command,
        entry.label,
        entry.favorite ? 1 : 0,
        _dateWireValue(entry.createdAt),
        _dateWireValue(entry.lastUsedAt),
      ],
    );
  }

  Future<void> _upsertDigitalEmployeePrompt(
    _MobileSqliteDatabase db,
    DigitalEmployeePromptEntry entry,
  ) {
    return db.customStatement(
      'INSERT INTO digital_employee_prompts '
      '(id, employee_id, prompt, created_at) '
      'VALUES (?, ?, ?, ?) '
      'ON CONFLICT(id) DO UPDATE SET '
      'employee_id = excluded.employee_id, prompt = excluded.prompt, '
      'created_at = excluded.created_at',
      [
        entry.id,
        entry.employeeId,
        entry.prompt,
        _dateWireValue(entry.createdAt),
      ],
    );
  }

  Future<void> _upsertDigitalEmployeeTask(
    _MobileSqliteDatabase db,
    MobileDigitalEmployeeTask task,
  ) {
    return db.customStatement(
      'INSERT INTO digital_employee_tasks '
      '(task_id, employee_id, prompt, task_type, context_json, status, result, message, claimed_by, updated_at) '
      'VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) '
      'ON CONFLICT(task_id) DO UPDATE SET '
      'employee_id = excluded.employee_id, prompt = excluded.prompt, '
      'task_type = excluded.task_type, context_json = excluded.context_json, '
      'status = excluded.status, result = excluded.result, '
      'message = excluded.message, '
      'claimed_by = excluded.claimed_by, updated_at = excluded.updated_at',
      [
        task.taskId,
        task.employeeId,
        task.prompt,
        task.taskType,
        _stringMapToJson(task.context),
        task.status,
        task.result,
        task.message,
        task.claimedBy,
        _dateWireValue(DateTime.now()),
      ],
    );
  }

  Future<void> _upsertAppPreferences(
    _MobileSqliteDatabase db,
    AppPreferences preferences,
  ) {
    return db.customStatement(
      'INSERT INTO app_preferences (id, theme_mode, language) '
      'VALUES (1, ?, ?) '
      'ON CONFLICT(id) DO UPDATE SET '
      'theme_mode = excluded.theme_mode, language = excluded.language',
      [
        themeModeWireValue(preferences.themeMode),
        preferences.language,
      ],
    );
  }

  Future<Directory> _mobileCacheDirectory() async {
    final dir = await _documentsDirectory();
    return Directory('${dir.path}/maclaw_mobile');
  }

  Future<List<File>> _legacyJsonFiles() async {
    return [
      await _serverProfilesFile(),
      await _searchHistoryFile(),
      await _serverCommandsFile(),
      await _lastDocumentDraftFile(),
      await _digitalEmployeePromptsFile(),
      await _appPreferencesFile(),
    ];
  }

  Future<List<File>> _legacyWorkCacheFiles() async {
    return [
      await _searchHistoryFile(),
      await _serverCommandsFile(),
      await _lastDocumentDraftFile(),
      await _digitalEmployeePromptsFile(),
    ];
  }

  Future<File> _serverProfilesFile() async {
    final dir = await _mobileCacheDirectory();
    return File('${dir.path}/server_profiles.json');
  }

  Future<File> _searchHistoryFile() async {
    final dir = await _mobileCacheDirectory();
    return File('${dir.path}/search_history.json');
  }

  Future<File> _serverCommandsFile() async {
    final dir = await _mobileCacheDirectory();
    return File('${dir.path}/server_commands.json');
  }

  Future<File> _lastDocumentDraftFile() async {
    final dir = await _mobileCacheDirectory();
    return File('${dir.path}/last_document_draft.json');
  }

  Future<File> _digitalEmployeePromptsFile() async {
    final dir = await _mobileCacheDirectory();
    return File('${dir.path}/digital_employee_prompts.json');
  }

  Future<File> _appPreferencesFile() async {
    final dir = await _mobileCacheDirectory();
    return File('${dir.path}/app_preferences.json');
  }
}

class _MobileSqliteDatabase extends GeneratedDatabase {
  _MobileSqliteDatabase(super.executor);

  @override
  int get schemaVersion => 1;

  @override
  Iterable<TableInfo<Table, dynamic>> get allTables => const [];

  @override
  List<DatabaseSchemaEntity> get allSchemaEntities => const [];
}

const _cacheTables = [
  'server_profiles',
  'search_history',
  'document_drafts',
  'document_upload_tasks',
  'document_export_jobs',
  'server_commands',
  'digital_employee_prompts',
  'digital_employee_tasks',
  'app_preferences',
];

const _workCacheTables = [
  'search_history',
  'document_drafts',
  'document_upload_tasks',
  'document_export_jobs',
  'server_commands',
  'digital_employee_prompts',
  'digital_employee_tasks',
  'app_preferences',
];

String _dateWireValue(DateTime value) => value.toUtc().toIso8601String();

DateTime _readDate(String value) =>
    DateTime.tryParse(value) ?? DateTime.fromMillisecondsSinceEpoch(0);
