import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/settings/app_preferences.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/assistant/search_history.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee_prompt.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';
import 'package:maclaw_mobile/features/servers/server_command.dart';
import 'package:maclaw_mobile/features/servers/server_profile.dart';

void main() {
  test('opens sqlite database once when first reads are concurrent', () async {
    final dir = await Directory.systemTemp.createTemp(
      'maclaw_mobile_concurrent_open_',
    );
    final releaseDirectory = Completer<void>();
    var directoryRequests = 0;
    final store = MobileLocalStore(
      documentsDirectory: () async {
        directoryRequests++;
        await releaseDirectory.future;
        return dir;
      },
    );
    addTearDown(() async {
      await store.close();
      if (await dir.exists()) {
        await dir.delete(recursive: true);
      }
    });

    final prompts = store.loadDigitalEmployeePrompts();
    final tasks = store.loadRecentDigitalEmployeeTasks();

    await Future<void>.delayed(Duration.zero);
    expect(directoryRequests, 1);

    releaseDirectory.complete();
    expect(await prompts, isEmpty);
    expect(await tasks, isEmpty);
  });

  test('persists local mobile cache in sqlite and clears it', () async {
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
        answerPreview: 'ok token=search-secret',
        createdAt: now,
      ),
    ]);
    await store.saveServerCommands([
      ServerCommandEntry(
        id: 'cmd-1',
        command: 'deploy password=command-secret',
        label: 'deploy password=command-secret',
        favorite: true,
        createdAt: now,
        lastUsedAt: now,
      ),
    ]);
    await store.saveDigitalEmployeePrompts([
      DigitalEmployeePromptEntry(
        id: 'prompt-1',
        employeeId: 'employee-1',
        prompt: 'check logs token=prompt-secret',
        createdAt: now,
      ),
    ]);
    await store.saveLastDigitalEmployeeTask(
      const MobileDigitalEmployeeTask(
        taskId: 'task-1',
        employeeId: 'employee-1',
        prompt: 'check remote host token=task-prompt-secret',
        context: {
          'source': 'maclaw_mobile',
          'error': 'db password=context-secret',
        },
        status: 'queued',
        result: 'result api_key=task-result-secret',
        message: 'waiting token=task-message-secret',
        claimedBy: 'runner password=task-runner-secret',
      ),
    );
    await store.saveLastDocumentDraft(
      DocumentDraft(
        id: 'draft-1',
        title: 'Incident',
        template: DocumentTemplate.report,
        markdown: '# Incident',
        updatedAt: now,
      ),
    );
    await store.saveLastDocumentUploadTask(
      const MobileDocumentUploadTask(
        taskId: 'upload-1',
        filename: 'incident.pdf',
        status: 'queued',
        draftId: '',
        message: 'waiting',
      ),
      sourcePath: '/tmp/incident.pdf',
    );
    await store.saveLastDocumentExportJob(
      DocumentExportJob(
        jobId: 'export-1',
        draftId: 'draft-1',
        format: DocumentExportFormat.pdf,
        status: 'queued',
        downloadUrl: '',
        message: '等待官方服务生成文件',
        createdAt: now,
      ),
    );
    await store.saveAppPreferences(
      const AppPreferences(
        themeMode: ThemeMode.dark,
        language: appLanguageEnglish,
      ),
    );

    expect(
      await File('${dir.path}/maclaw_mobile/maclaw_mobile.sqlite').exists(),
      isTrue,
    );
    expect(await store.loadServerProfiles(), isNotEmpty);
    final searchHistory = await store.loadSearchHistory();
    expect(searchHistory.single.answerPreview, 'ok token=[REDACTED_SECRET]');
    expect(
      searchHistory.single.answerPreview,
      isNot(contains('search-secret')),
    );
    final commands = await store.loadServerCommands();
    expect(commands.single.command, contains('command-secret'));
    expect(commands.single.label, 'deploy password=[REDACTED_SECRET]');
    expect(commands.single.label, isNot(contains('command-secret')));
    final prompts = await store.loadDigitalEmployeePrompts();
    expect(prompts.single.prompt, 'check logs token=[REDACTED_SECRET]');
    expect(prompts.single.prompt, isNot(contains('prompt-secret')));
    final digitalEmployeeTask = await store.loadLastDigitalEmployeeTask();
    expect(digitalEmployeeTask?.taskId, 'task-1');
    expect(
      digitalEmployeeTask?.prompt,
      'check remote host token=[REDACTED_SECRET]',
    );
    expect(
      digitalEmployeeTask?.context['error'],
      'db password=[REDACTED_SECRET]',
    );
    expect(
      digitalEmployeeTask?.result,
      'result api_key=[REDACTED_SECRET]',
    );
    expect(
      digitalEmployeeTask?.message,
      'waiting token=[REDACTED_SECRET]',
    );
    expect(
      digitalEmployeeTask?.claimedBy,
      'runner password=[REDACTED_SECRET]',
    );
    expect(
      jsonEncode(digitalEmployeeTask?.context),
      isNot(contains('context-secret')),
    );
    expect(digitalEmployeeTask?.prompt, isNot(contains('task-prompt-secret')));
    expect(digitalEmployeeTask?.result, isNot(contains('task-result-secret')));
    expect(
      digitalEmployeeTask?.message,
      isNot(contains('task-message-secret')),
    );
    expect(
      digitalEmployeeTask?.claimedBy,
      isNot(contains('task-runner-secret')),
    );
    expect(await store.loadLastDocumentDraft(), isNotNull);
    expect((await store.loadLastDocumentUploadTask())?.taskId, 'upload-1');
    expect(await store.loadLastDocumentUploadPath(), '/tmp/incident.pdf');
    final exportJob = await store.loadLastDocumentExportJob();
    expect(exportJob?.jobId, 'export-1');
    expect(exportJob?.message, '等待官方服务生成文件');
    expect((await store.loadAppPreferences()).themeMode, ThemeMode.dark);

    await store.clearLocalCache();

    expect(await store.loadServerProfiles(), isEmpty);
    expect(await store.loadSearchHistory(), isEmpty);
    expect(await store.loadServerCommands(), isEmpty);
    expect(await store.loadDigitalEmployeePrompts(), isEmpty);
    expect(await store.loadLastDigitalEmployeeTask(), isNull);
    expect(await store.loadLastDocumentDraft(), isNull);
    expect(await store.loadLastDocumentUploadTask(), isNull);
    expect(await store.loadLastDocumentUploadPath(), isNull);
    expect(await store.loadLastDocumentExportJob(), isNull);
    expect((await store.loadAppPreferences()).themeMode, ThemeMode.system);

    await store.close();
    await dir.delete(recursive: true);
  });

  test('clears work cache and preferences separately from server profiles',
      () async {
    final dir = await Directory.systemTemp.createTemp('maclaw_mobile_store_');
    final store = MobileLocalStore(documentsDirectory: () async => dir);
    final now = DateTime.utc(2026, 7, 2);

    await store.saveServerProfiles(const [
      ServerProfile(
        id: 'srv-keep',
        name: 'prod',
        host: '10.0.0.8',
        port: 22,
        username: 'ops',
        authMode: serverAuthModePassword,
      ),
    ]);
    await store.saveSearchHistory([
      SearchHistoryEntry(
        id: 'search-clear',
        query: 'emergency lookup',
        answerPreview: 'summary',
        createdAt: now,
      ),
    ]);
    await store.saveLastDocumentDraft(
      DocumentDraft(
        id: 'draft-clear',
        title: 'Emergency',
        template: DocumentTemplate.report,
        markdown: '# Emergency',
        updatedAt: now,
      ),
    );
    await store.saveLastDocumentUploadTask(
      const MobileDocumentUploadTask(
        taskId: 'upload-clear',
        filename: 'incident.pdf',
        status: 'failed',
        draftId: '',
        message: 'parse failed',
      ),
      sourcePath: '/tmp/incident.pdf',
    );
    await store.saveLastDocumentExportJob(
      DocumentExportJob(
        jobId: 'export-clear',
        draftId: 'draft-clear',
        format: DocumentExportFormat.word,
        status: 'failed',
        downloadUrl: '',
        message: 'export failed',
        createdAt: now,
      ),
    );
    await store.saveServerCommands([
      ServerCommandEntry(
        id: 'cmd-clear',
        command: 'journalctl -u app -n 100',
        label: 'journalctl',
        favorite: true,
        createdAt: now,
        lastUsedAt: now,
      ),
    ]);
    await store.saveDigitalEmployeePrompts([
      DigitalEmployeePromptEntry(
        id: 'prompt-clear',
        employeeId: 'employee-1',
        prompt: 'check remote server',
        createdAt: now,
      ),
    ]);
    await store.saveLastDigitalEmployeeTask(
      const MobileDigitalEmployeeTask(
        taskId: 'task-clear',
        employeeId: 'employee-1',
        prompt: 'check remote server',
        status: 'done',
        result: 'ok',
        message: 'completed',
        claimedBy: 'runner-1',
      ),
    );
    await store.saveAppPreferences(
      const AppPreferences(
        themeMode: ThemeMode.dark,
        language: appLanguageEnglish,
      ),
    );

    await store.clearLocalWorkCache();

    expect((await store.loadServerProfiles()).single.id, 'srv-keep');
    expect((await store.loadAppPreferences()).themeMode, ThemeMode.system);
    expect((await store.loadAppPreferences()).language, appLanguageChinese);
    expect(await store.loadSearchHistory(), isEmpty);
    expect(await store.loadLastDocumentDraft(), isNull);
    expect(await store.loadLastDocumentUploadTask(), isNull);
    expect(await store.loadLastDocumentUploadPath(), isNull);
    expect(await store.loadLastDocumentExportJob(), isNull);
    expect(await store.loadServerCommands(), isEmpty);
    expect(await store.loadDigitalEmployeePrompts(), isEmpty);
    expect(await store.loadLastDigitalEmployeeTask(), isNull);

    await store.clearServerProfiles();

    expect(await store.loadServerProfiles(), isEmpty);
    expect((await store.loadAppPreferences()).themeMode, ThemeMode.system);

    await store.close();
    await dir.delete(recursive: true);
  });

  test('keeps recent digital employee task history with mobile context',
      () async {
    final dir = await Directory.systemTemp.createTemp('maclaw_mobile_tasks_');
    final store = MobileLocalStore(documentsDirectory: () async => dir);

    for (var i = 0; i < 22; i++) {
      await store.saveLastDigitalEmployeeTask(
        MobileDigitalEmployeeTask(
          taskId: 'task-$i',
          employeeId: 'employee-1',
          prompt: 'check remote host $i',
          taskType: i.isEven ? 'server_maintenance' : 'desktop_assist',
          context: {'source': 'maclaw_mobile', 'index': '$i'},
          status: i == 21 ? 'done' : 'queued',
          result: i == 21 ? 'ok' : '',
          message: 'message $i',
          claimedBy: '',
        ),
      );
      await Future<void>.delayed(const Duration(milliseconds: 1));
    }

    final recent = await store.loadRecentDigitalEmployeeTasks();

    expect(recent, hasLength(20));
    expect(recent.first.taskId, 'task-21');
    expect(recent.first.taskType, 'desktop_assist');
    expect(recent.first.context['source'], 'maclaw_mobile');
    expect(recent.last.taskId, 'task-2');
    expect((await store.loadLastDigitalEmployeeTask())?.taskId, 'task-21');

    await store.close();
    await dir.delete(recursive: true);
  });

  test('keeps recent document draft history for emergency edits', () async {
    final dir = await Directory.systemTemp.createTemp('maclaw_mobile_store_');
    final store = MobileLocalStore(documentsDirectory: () async => dir);

    for (var i = 0; i < 25; i++) {
      await store.saveLastDocumentDraft(
        DocumentDraft(
          id: 'draft-$i',
          title: 'Emergency Draft $i',
          template: i.isEven ? DocumentTemplate.report : DocumentTemplate.email,
          markdown: '# Draft $i',
          updatedAt: DateTime.utc(2026, 7, 2, 0, i),
        ),
      );
    }

    final recent = await store.loadRecentDocumentDrafts();
    expect(recent, hasLength(20));
    expect(recent.first.id, 'draft-24');
    expect(recent.first.title, 'Emergency Draft 24');
    expect(recent.last.id, 'draft-5');
    expect((await store.loadLastDocumentDraft())?.id, 'draft-24');

    await store.saveLastDocumentDraft(
      DocumentDraft(
        id: 'draft-8',
        title: 'Updated Draft 8',
        template: DocumentTemplate.statement,
        markdown: '# Updated',
        updatedAt: DateTime.utc(2026, 7, 2, 1),
      ),
    );

    final updatedRecent = await store.loadRecentDocumentDrafts(limit: 3);
    expect(updatedRecent.map((draft) => draft.id), [
      'draft-8',
      'draft-24',
      'draft-23',
    ]);
    expect(updatedRecent.first.title, 'Updated Draft 8');
    expect(updatedRecent.first.template, DocumentTemplate.statement);

    await store.close();
    await dir.delete(recursive: true);
  });

  test('migrates legacy json cache into sqlite', () async {
    final dir = await Directory.systemTemp.createTemp('maclaw_mobile_legacy_');
    final cacheDir = Directory('${dir.path}/maclaw_mobile');
    await cacheDir.create(recursive: true);
    final now = DateTime.utc(2026, 7, 1);

    await File('${cacheDir.path}/server_profiles.json').writeAsString(
      jsonEncode([
        const ServerProfile(
          id: 'srv-json',
          name: 'legacy',
          host: '192.0.2.8',
          port: 2222,
          username: 'ops',
          authMode: serverAuthModePrivateKey,
        ).toJson(),
      ]),
    );
    await File('${cacheDir.path}/search_history.json').writeAsString(
      jsonEncode([
        SearchHistoryEntry(
          id: 'search-json',
          query: 'incident',
          answerPreview: 'legacy result token=legacy-search-secret',
          createdAt: now,
          favorite: true,
        ).toJson(),
      ]),
    );
    await File('${cacheDir.path}/server_commands.json').writeAsString(
      jsonEncode([
        ServerCommandEntry(
          id: 'cmd-json',
          command: 'deploy password=legacy-command-secret',
          label: 'deploy password=legacy-command-secret',
          favorite: true,
          createdAt: now,
          lastUsedAt: now,
        ).toJson(),
      ]),
    );
    await File('${cacheDir.path}/digital_employee_prompts.json').writeAsString(
      jsonEncode([
        DigitalEmployeePromptEntry(
          id: 'prompt-json',
          employeeId: 'employee-1',
          prompt: 'check server token=legacy-prompt-secret',
          createdAt: now,
        ).toJson(),
      ]),
    );
    await File('${cacheDir.path}/last_document_draft.json').writeAsString(
      jsonEncode(
        DocumentDraft(
          id: 'draft-json',
          title: 'Legacy Draft',
          template: DocumentTemplate.notice,
          markdown: '# Legacy',
          updatedAt: now,
        ).toJson(),
      ),
    );

    final store = MobileLocalStore(documentsDirectory: () async => dir);

    expect((await store.loadServerProfiles()).single.id, 'srv-json');
    final migratedSearch = (await store.loadSearchHistory()).single;
    expect(migratedSearch.favorite, isTrue);
    expect(
      migratedSearch.answerPreview,
      'legacy result token=[REDACTED_SECRET]',
    );
    expect(
      migratedSearch.answerPreview,
      isNot(contains('legacy-search-secret')),
    );
    final migratedCommand = (await store.loadServerCommands()).single;
    expect(migratedCommand.command, contains('legacy-command-secret'));
    expect(migratedCommand.label, 'deploy password=[REDACTED_SECRET]');
    expect(migratedCommand.label, isNot(contains('legacy-command-secret')));
    final migratedPrompt = (await store.loadDigitalEmployeePrompts()).single;
    expect(migratedPrompt.prompt, 'check server token=[REDACTED_SECRET]');
    expect(migratedPrompt.prompt, isNot(contains('legacy-prompt-secret')));
    expect((await store.loadLastDocumentDraft())?.title, 'Legacy Draft');
    expect(
      await File('${cacheDir.path}/maclaw_mobile.sqlite').exists(),
      isTrue,
    );

    await store.close();
    await dir.delete(recursive: true);
  });
}
