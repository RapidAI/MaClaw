import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/settings/app_preferences.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/assistant/search_history.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee_prompt.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';
import 'package:maclaw_mobile/features/servers/server_command.dart';
import 'package:maclaw_mobile/features/servers/server_profile.dart';

void main() {
  test('clears local mobile cache files', () async {
    final dir = await Directory.systemTemp.createTemp('maclaw_mobile_store_');
    final store = MobileLocalStore(documentsDirectory: () async => dir);
    final now = DateTime.utc(2026, 7, 1);

    await store.saveServerProfiles(const [
      ServerProfile(
        id: 'srv-1',
        name: 'prod',
        host: '10.0.0.8',
        port: 22,
        username: 'ops',
        authMode: serverAuthModePassword,
      ),
    ]);
    await store.saveSearchHistory([
      SearchHistoryEntry(
        id: 'search-1',
        query: 'status',
        answerPreview: 'ok',
        createdAt: now,
      ),
    ]);
    await store.saveServerCommands([
      ServerCommandEntry(
        id: 'cmd-1',
        command: 'df -h',
        label: 'df',
        favorite: true,
        createdAt: now,
        lastUsedAt: now,
      ),
    ]);
    await store.saveDigitalEmployeePrompts([
      DigitalEmployeePromptEntry(
        id: 'prompt-1',
        employeeId: 'employee-1',
        prompt: 'check logs',
        createdAt: now,
      ),
    ]);
    await store.saveLastDocumentDraft(
      DocumentDraft(
        id: 'draft-1',
        title: 'Incident',
        template: DocumentTemplate.report,
        markdown: '# Incident',
        updatedAt: now,
      ),
    );
    await store.saveAppPreferences(
      const AppPreferences(
        themeMode: ThemeMode.dark,
        language: appLanguageEnglish,
      ),
    );

    expect(await store.loadServerProfiles(), isNotEmpty);
    expect(await store.loadSearchHistory(), isNotEmpty);
    expect(await store.loadServerCommands(), isNotEmpty);
    expect(await store.loadDigitalEmployeePrompts(), isNotEmpty);
    expect(await store.loadLastDocumentDraft(), isNotNull);
    expect((await store.loadAppPreferences()).themeMode, ThemeMode.dark);

    await store.clearLocalCache();

    expect(await store.loadServerProfiles(), isEmpty);
    expect(await store.loadSearchHistory(), isEmpty);
    expect(await store.loadServerCommands(), isEmpty);
    expect(await store.loadDigitalEmployeePrompts(), isEmpty);
    expect(await store.loadLastDocumentDraft(), isNull);
    expect((await store.loadAppPreferences()).themeMode, ThemeMode.system);

    await dir.delete(recursive: true);
  });
}
