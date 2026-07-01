import 'dart:convert';
import 'dart:io';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';

import '../settings/app_preferences_model.dart';
import '../../features/assistant/search_history.dart';
import '../../features/digital_employees/digital_employee_prompt.dart';
import '../../features/documents/document_draft.dart';
import '../../features/servers/server_command.dart';
import '../../features/servers/server_profile.dart';

final mobileLocalStoreProvider =
    Provider<MobileLocalStore>((ref) => MobileLocalStore());

class MobileLocalStore {
  final Future<Directory> Function() _documentsDirectory;

  MobileLocalStore({
    Future<Directory> Function()? documentsDirectory,
  }) : _documentsDirectory =
            documentsDirectory ?? getApplicationDocumentsDirectory;

  Future<List<ServerProfile>> loadServerProfiles() async {
    final file = await _serverProfilesFile();
    if (!await file.exists()) return const [];
    final raw = await file.readAsString();
    if (raw.trim().isEmpty) return const [];
    final data = jsonDecode(raw) as List;
    return [
      for (final item in data)
        ServerProfile.fromJson(Map<String, dynamic>.from(item as Map)),
    ];
  }

  Future<void> saveServerProfiles(List<ServerProfile> profiles) async {
    final file = await _serverProfilesFile();
    await file.parent.create(recursive: true);
    final data = [for (final profile in profiles) profile.toJson()];
    await file.writeAsString(jsonEncode(data));
  }

  Future<List<SearchHistoryEntry>> loadSearchHistory() async {
    final file = await _searchHistoryFile();
    if (!await file.exists()) return const [];
    final raw = await file.readAsString();
    if (raw.trim().isEmpty) return const [];
    final data = jsonDecode(raw) as List;
    return [
      for (final item in data)
        SearchHistoryEntry.fromJson(Map<String, dynamic>.from(item as Map)),
    ];
  }

  Future<void> saveSearchHistory(List<SearchHistoryEntry> entries) async {
    final file = await _searchHistoryFile();
    await file.parent.create(recursive: true);
    final data = [for (final entry in entries) entry.toJson()];
    await file.writeAsString(jsonEncode(data));
  }

  Future<DocumentDraft?> loadLastDocumentDraft() async {
    final file = await _lastDocumentDraftFile();
    if (!await file.exists()) return null;
    final raw = await file.readAsString();
    if (raw.trim().isEmpty) return null;
    return DocumentDraft.fromJson(
      Map<String, dynamic>.from(jsonDecode(raw) as Map),
    );
  }

  Future<void> saveLastDocumentDraft(DocumentDraft draft) async {
    final file = await _lastDocumentDraftFile();
    await file.parent.create(recursive: true);
    await file.writeAsString(jsonEncode(draft.toJson()));
  }

  Future<List<ServerCommandEntry>> loadServerCommands() async {
    final file = await _serverCommandsFile();
    if (!await file.exists()) return const [];
    final raw = await file.readAsString();
    if (raw.trim().isEmpty) return const [];
    final data = jsonDecode(raw) as List;
    return [
      for (final item in data)
        ServerCommandEntry.fromJson(Map<String, dynamic>.from(item as Map)),
    ];
  }

  Future<void> saveServerCommands(List<ServerCommandEntry> entries) async {
    final file = await _serverCommandsFile();
    await file.parent.create(recursive: true);
    final data = [for (final entry in entries) entry.toJson()];
    await file.writeAsString(jsonEncode(data));
  }

  Future<List<DigitalEmployeePromptEntry>> loadDigitalEmployeePrompts() async {
    final file = await _digitalEmployeePromptsFile();
    if (!await file.exists()) return const [];
    final raw = await file.readAsString();
    if (raw.trim().isEmpty) return const [];
    final data = jsonDecode(raw) as List;
    return [
      for (final item in data)
        DigitalEmployeePromptEntry.fromJson(
          Map<String, dynamic>.from(item as Map),
        ),
    ];
  }

  Future<void> saveDigitalEmployeePrompts(
    List<DigitalEmployeePromptEntry> entries,
  ) async {
    final file = await _digitalEmployeePromptsFile();
    await file.parent.create(recursive: true);
    final data = [for (final entry in entries) entry.toJson()];
    await file.writeAsString(jsonEncode(data));
  }

  Future<AppPreferences> loadAppPreferences() async {
    final file = await _appPreferencesFile();
    if (!await file.exists()) return const AppPreferences();
    final raw = await file.readAsString();
    if (raw.trim().isEmpty) return const AppPreferences();
    return AppPreferences.fromJson(
      Map<String, dynamic>.from(jsonDecode(raw) as Map),
    );
  }

  Future<void> saveAppPreferences(AppPreferences preferences) async {
    final file = await _appPreferencesFile();
    await file.parent.create(recursive: true);
    await file.writeAsString(jsonEncode(preferences.toJson()));
  }

  Future<void> clearLocalCache() async {
    final files = [
      await _serverProfilesFile(),
      await _searchHistoryFile(),
      await _serverCommandsFile(),
      await _lastDocumentDraftFile(),
      await _digitalEmployeePromptsFile(),
      await _appPreferencesFile(),
    ];
    for (final file in files) {
      if (await file.exists()) {
        await file.delete();
      }
    }
  }

  Future<File> _serverProfilesFile() async {
    final dir = await _documentsDirectory();
    return File('${dir.path}/maclaw_mobile/server_profiles.json');
  }

  Future<File> _searchHistoryFile() async {
    final dir = await _documentsDirectory();
    return File('${dir.path}/maclaw_mobile/search_history.json');
  }

  Future<File> _serverCommandsFile() async {
    final dir = await _documentsDirectory();
    return File('${dir.path}/maclaw_mobile/server_commands.json');
  }

  Future<File> _lastDocumentDraftFile() async {
    final dir = await _documentsDirectory();
    return File('${dir.path}/maclaw_mobile/last_document_draft.json');
  }

  Future<File> _digitalEmployeePromptsFile() async {
    final dir = await _documentsDirectory();
    return File('${dir.path}/maclaw_mobile/digital_employee_prompts.json');
  }

  Future<File> _appPreferencesFile() async {
    final dir = await _documentsDirectory();
    return File('${dir.path}/maclaw_mobile/app_preferences.json');
  }
}
