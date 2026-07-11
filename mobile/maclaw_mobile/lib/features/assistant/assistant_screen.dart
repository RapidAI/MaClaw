import 'dart:async';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:image_picker/image_picker.dart';
import 'package:share_plus/share_plus.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_credits.dart';
import '../../core/platform/mobile_permission_evidence.dart';
import '../../core/security/mobile_redaction.dart';
import '../../core/settings/app_preferences.dart';
import '../../core/shared_intents/mobile_shared_intent.dart';
import '../../core/shared_intents/shared_intent_bootstrap.dart';
import '../../core/text/html_text.dart';
import '../../shared/surface.dart';
import '../auth/session_controller.dart';
import '../documents/document_draft.dart';
import '../documents/documents_controller.dart';
import 'assistant_controller.dart';
import 'assistant_digital_twin_gate.dart';
import 'assistant_employee_handoff.dart';
import 'assistant_inline_card.dart';
import 'assistant_markdown.dart';
import 'assistant_voice_input.dart';
import 'search_history.dart';

bool canRetryAssistantQuery(String query) => query.trim().isNotEmpty;

String assistantQueryWithVoiceTranscript(
  String currentQuery,
  String transcript, {
  String previousTranscript = '',
}) {
  final recognized = transcript.trim();
  if (recognized.isEmpty) return currentQuery;

  var base = currentQuery.trimRight();
  final previous = previousTranscript.trim();
  if (previous.isNotEmpty && base.endsWith(previous)) {
    base = base.substring(0, base.length - previous.length).trimRight();
  }
  if (base.isEmpty) return recognized;
  if (base == recognized || base.endsWith(recognized)) return base;
  return '$base\n$recognized';
}

typedef AssistantDocumentPathPicker = Future<String?> Function();
typedef AssistantTextAction = Future<void> Function(String text);

final assistantFilePathPickerProvider = Provider<AssistantDocumentPathPicker>(
  (ref) => () async {
    final file = await FilePicker.platform.pickFiles();
    return file?.files.single.path;
  },
);

final assistantCameraImagePathPickerProvider =
    Provider<AssistantDocumentPathPicker>(
  (ref) => () async {
    final image = await ImagePicker().pickImage(source: ImageSource.camera);
    return image?.path;
  },
);

final assistantGalleryImagePathPickerProvider =
    Provider<AssistantDocumentPathPicker>(
  (ref) => () async {
    final image = await ImagePicker().pickImage(source: ImageSource.gallery);
    return image?.path;
  },
);

final assistantClipboardWriterProvider = Provider<AssistantTextAction>(
  (ref) => (text) => Clipboard.setData(ClipboardData(text: text)),
);

final assistantShareProvider = Provider<AssistantTextAction>(
  (ref) => (text) async {
    await Share.share(text);
  },
);

List<SearchCitation> mergeAssistantCitations(
  List<SearchCitation> citations,
  SearchCitation? fallbackCitation,
) {
  final seenUrls = <String>{};
  final merged = <SearchCitation>[];
  for (final citation in [
    ...citations,
    if (fallbackCitation != null) fallbackCitation,
  ]) {
    final normalizedUrl = citation.url.trim().toLowerCase();
    if (normalizedUrl.isNotEmpty && !seenUrls.add(normalizedUrl)) continue;
    merged.add(citation);
  }
  return merged;
}

class AssistantScreen extends ConsumerStatefulWidget {
  const AssistantScreen({super.key});

  @override
  ConsumerState<AssistantScreen> createState() => _AssistantScreenState();
}

class _AssistantScreenState extends ConsumerState<AssistantScreen> {
  final _queryController = TextEditingController();
  final _scrollController = ScrollController();
  late final AssistantVoiceInput _voiceInput;
  bool _listening = false;
  String? _voiceStatus;
  String? _handledSharedIntentId;
  String _lastVoiceTranscript = '';
  String? _voicePermissionEvidence;
  int _voiceSessionGeneration = 0;

  @override
  void initState() {
    super.initState();
    _voiceInput = ref.read(assistantVoiceInputProvider);
  }

  @override
  void dispose() {
    unawaited(_voiceInput.stop());
    _queryController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  void _scrollConversationToEnd() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_scrollController.hasClients) return;
      final max = _scrollController.position.maxScrollExtent;
      if (max <= 0) return;
      _scrollController.animateTo(
        max,
        duration: const Duration(milliseconds: 220),
        curve: Curves.easeOutCubic,
      );
    });
  }

  void _setQuery(String value) {
    _queryController.text = value;
    ref.read(assistantTabsProvider.notifier).updateActiveQuery(value);
    ref.read(assistantQueryProvider.notifier).state = value;
  }

  void _searchManually(String query) {
    ref.read(assistantSharedCitationProvider.notifier).state = null;
    ref.read(assistantTabsProvider.notifier).setActiveSharedCitation(null);
    unawaited(ref.read(assistantSearchProvider.notifier).search(query));
    _setQuery('');
    _scrollConversationToEnd();
  }

  Future<void> _toggleVoiceInput() async {
    if (_listening) {
      _voiceSessionGeneration++;
      try {
        await _voiceInput.stop();
      } on Object {
        // The platform service may already have stopped after a permission or
        // lifecycle change; the UI still needs to leave listening mode.
      }
      setState(() {
        _listening = false;
        _voiceStatus = '语音输入已停止';
      });
      return;
    }
    final language = ref.read(appPreferencesProvider).valueOrNull?.language ??
        appLanguageChinese;
    final localeId = assistantSpeechLocaleForLanguage(language);
    _lastVoiceTranscript = '';
    _voicePermissionEvidence = null;
    final sessionGeneration = ++_voiceSessionGeneration;
    setState(() {
      _listening = true;
      _voiceStatus = '正在启动语音输入';
    });
    bool ready = false;
    try {
      ready = await _voiceInput.start(
        onStatus: (status) {
          if (!mounted ||
              sessionGeneration != _voiceSessionGeneration ||
              !_listening ||
              (status != 'done' &&
                  status != 'notListening' &&
                  status != 'error')) {
            return;
          }
          setState(() {
            _listening = false;
            _voiceStatus = status == 'error'
                ? '\u8bed\u97f3\u8bc6\u522b\u670d\u52a1\u4e2d\u65ad\uff0c\u53ef\u7ee7\u7eed\u4f7f\u7528\u6587\u5b57\u8f93\u5165'
                : '\u8bed\u97f3\u8f93\u5165\u5df2\u5b8c\u6210\uff0c\u8bf7\u68c0\u67e5\u8bc6\u522b\u7ed3\u679c\u540e\u53ef\u53d1\u9001';
          });
        },
        localeId: localeId,
        onText: (text) {
          if (!mounted || sessionGeneration != _voiceSessionGeneration) {
            return;
          }
          final merged = assistantQueryWithVoiceTranscript(
            _queryController.text,
            text,
            previousTranscript: _lastVoiceTranscript,
          );
          _lastVoiceTranscript = text.trim();
          _setQuery(merged);
          setState(() => _voiceStatus = '已识别语音，检查后可发送给 AI 助手');
        },
      );
    } on Object {
      // Keep typed assistant input available when a platform speech service
      // throws before it can report a normal unavailable result.
      ready = false;
    }
    if (!mounted || sessionGeneration != _voiceSessionGeneration) return;
    if (!ready) {
      setState(() {
        _listening = false;
        _voiceStatus = '语音输入不可用，请检查麦克风权限';
      });
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('语音输入不可用，请检查麦克风权限')),
      );
      return;
    }
    if (_listening) {
      setState(() {
        _voicePermissionEvidence = mobilePermissionGrantEvidence('microphone');
        _voiceStatus = '正在听写，识别结果会填入 AI 助手输入框';
      });
    }
  }

  Future<void> _pickImage() async {
    String? path;
    try {
      path = await ref.read(assistantCameraImagePathPickerProvider)();
    } on Object {
      _showInputFailure('无法打开相机，请检查相机权限后重试。');
      return;
    }
    if (path == null || path.isEmpty) return;
    _setQuery('请分析刚拍摄的图片，并给出可执行结论。');
    await _uploadToDocuments(
      path,
      permissionEvidence: mobilePermissionGrantEvidence('camera'),
      successMessage: '图片已提交文档解析，完成后可在“文档”页继续处理。',
    );
  }

  Future<void> _pickGalleryImage() async {
    String? path;
    try {
      path = await ref.read(assistantGalleryImagePathPickerProvider)();
    } on Object {
      _showInputFailure('无法打开相册，请检查照片权限后重试。');
      return;
    }
    if (path == null || path.isEmpty) return;
    _setQuery('请分析这张截图或相册图片，提取关键信息并给出下一步。');
    await _uploadToDocuments(
      path,
      successMessage: '截图/图片已提交文档解析，完成后可在“文档”页继续处理。',
    );
  }

  Future<void> _pickFile() async {
    String? path;
    try {
      path = await ref.read(assistantFilePathPickerProvider)();
    } on Object {
      _showInputFailure('无法打开文件选择器，请重试。');
      return;
    }
    if (path == null || path.isEmpty) return;
    _setQuery('请总结刚导入文件或截图的关键信息。');
    await _uploadToDocuments(
      path,
      successMessage: '文件已提交文档解析，完成后可在“文档”页继续处理。',
    );
  }

  Future<void> _uploadToDocuments(
    String path, {
    required String successMessage,
    String? permissionEvidence,
  }) async {
    await ref
        .read(documentsControllerProvider.notifier)
        .uploadSharedDocument(path);
    if (!mounted) return;
    final documentState = ref.read(documentsControllerProvider);
    if (documentState.hasError) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('文档解析提交失败：${documentState.error}')),
      );
      return;
    }
    final uploadTaskId = documentState.valueOrNull?.uploadTask?.taskId;
    final effectivePermissionEvidence =
        permissionEvidence ?? mobilePermissionGrantEvidence('media');
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(successMessage),
            Text(effectivePermissionEvidence),
            if (uploadTaskId != null && uploadTaskId.isNotEmpty)
              Text('upload task $uploadTaskId'),
          ],
        ),
      ),
    );
  }

  void _showInputFailure(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text(message)));
  }

  void _consumeSharedIntent() {
    final shared = ref.watch(mobileSharedIntentProvider);
    final session = ref.watch(sessionControllerProvider);
    if (shared == null ||
        !shared.opensAssistant ||
        shared.id == _handledSharedIntentId) {
      return;
    }
    if (session.isLoading) return;
    final assistantEnabled =
        session.valueOrNull?.bootstrap?.features.assistant ?? true;
    _handledSharedIntentId = shared.id;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      final prompt = shared.assistantPrompt;
      if (shared.kind == MobileSharedIntentKind.link) {
        final citationUrl = shared.sharedUrl ?? shared.value;
        const snippet = '从系统分享进入 MaClaw Mobile 的链接。';
        final citation = SearchCitation(
          title: '分享来源',
          url: citationUrl,
          snippet: snippet,
        );
        ref.read(assistantSharedCitationProvider.notifier).state = citation;
        ref
            .read(assistantTabsProvider.notifier)
            .setActiveSharedCitation(citation);
      }
      _setQuery(prompt);
      if (assistantEnabled) {
        ref.read(assistantSearchProvider.notifier).search(prompt);
      }
      ref.read(mobileSharedIntentProvider.notifier).clear(shared.id);
    });
  }

  @override
  Widget build(BuildContext context) {
    final bootstrap =
        ref.watch(sessionControllerProvider).valueOrNull?.bootstrap;
    if (usesDigitalTwinAssistant(bootstrap)) {
      return const AssistantDigitalTwinGate();
    }
    _consumeSharedIntent();
    final tabs = ref.watch(assistantTabsProvider);
    final activeTab = tabs.activeTab;
    final query = ref.watch(assistantQueryProvider);
    // Never leak another tab's in-flight stream into an untouched tab.
    // Search always writes per-tab via setResultForTab (resultTouched=true).
    final result = activeTab.resultTouched
        ? activeTab.result
        : const AsyncData<SearchAnswer?>(null);
    final sharedCitation =
        activeTab.sharedCitation ?? ref.watch(assistantSharedCitationProvider);
    final history = ref.watch(searchHistoryProvider);
    final assistantEnabled = ref
            .watch(sessionControllerProvider)
            .valueOrNull
            ?.bootstrap
            ?.features
            .assistant ??
        true;
    if (_queryController.text != query) {
      _queryController.text = query;
    }
    final scheme = Theme.of(context).colorScheme;
    final hasConversation = activeTab.messages.isNotEmpty;
    final hasAnswer = result.maybeWhen(
      data: (answer) => answer != null,
      orElse: () => false,
    );
    final compactComposer = hasConversation || hasAnswer || result.isLoading;
    ref.listen(assistantTabsProvider, (previous, next) {
      final prevLen = previous?.activeTab.messages.length ?? 0;
      final nextLen = next.activeTab.messages.length;
      if (nextLen > prevLen || next.activeTabId != previous?.activeTabId) {
        _scrollConversationToEnd();
      }
    });

    return ChatWorkspaceScaffold(
      title: 'AI助手',
      subtitle: '像桌面端一样，随时聊聊、一起处理事情',
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          IconButton.filledTonal(
            tooltip: '对话历史',
            onPressed: () => _openHistorySheet(history),
            icon: const Icon(Icons.history),
          ),
          if (ref
                  .watch(sessionControllerProvider)
                  .valueOrNull
                  ?.bootstrap
                  ?.features
                  .backendSshSessions ??
              true) ...[
            const SizedBox(width: 6),
            IconButton.filledTonal(
              tooltip: '远程维护',
              onPressed: () => context.push('/servers'),
              icon: const Icon(Icons.lan_outlined),
            ),
          ],
          const SizedBox(width: 6),
          IconButton.filledTonal(
            tooltip: _listening ? '停止语音输入' : '开始语音输入',
            onPressed: _toggleVoiceInput,
            icon: Icon(_listening ? Icons.mic : Icons.mic_none),
          ),
        ],
      ),
      topBar: _AssistantTabStrip(
        tabs: tabs,
        onActivate: (tab) {
          ref.read(assistantTabsProvider.notifier).activate(tab.id);
          final active = ref.read(assistantTabsProvider).activeTab;
          ref.read(assistantSharedCitationProvider.notifier).state =
              active.sharedCitation;
          ref.read(assistantQueryProvider.notifier).state = tab.query;
        },
        onAdd: () {
          final tab = ref.read(assistantTabsProvider.notifier).addTab();
          ref.read(assistantSharedCitationProvider.notifier).state =
              tab.sharedCitation;
          ref.read(assistantQueryProvider.notifier).state = tab.query;
        },
        onClose: (tab) {
          ref.read(assistantTabsProvider.notifier).close(tab.id);
          final active = ref.read(assistantTabsProvider).activeTab;
          ref.read(assistantSharedCitationProvider.notifier).state =
              active.sharedCitation;
          ref.read(assistantQueryProvider.notifier).state = active.query;
        },
      ),
      body: ListView(
        controller: _scrollController,
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 20),
        children: [
          if (!hasConversation)
            result.when(
              data: (answer) => answer == null
                  ? const _AssistantCompanionIntro()
                  : _AssistantReplyBubble(
                      query: query,
                      answer: answer.answer,
                      citations: answer.citations,
                      llmMode: answer.llmMode,
                      llmRequestId: answer.llmRequestId,
                      llmUsageRecordId: answer.llmUsageRecordId,
                      fallbackCitation: sharedCitation,
                    ),
              error: (error, _) => _AssistantErrorBubble(
                error: error,
                query: query,
              ),
              loading: () => const _AssistantTypingIndicator(),
            )
          else
            _AssistantConversationView(
              messages: activeTab.messages,
              result: result,
              fallbackCitation: sharedCitation,
            ),
        ],
      ),
      composer: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (!assistantEnabled) ...[
            const _AssistantSearchUnavailableBanner(),
            const SizedBox(height: 8),
          ],

          if (!compactComposer) ...[
            _AssistantQuickPrompts(onSelect: _setQuery),
            const SizedBox(height: 8),
          ],
          if (_voiceStatus != null) ...[
            Row(
              children: [
                Icon(
                  _listening ? Icons.mic : Icons.info_outline,
                  size: 16,
                  color: scheme.primary,
                ),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(
                    _voiceStatus!,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: scheme.onSurfaceVariant,
                        ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 6),
          ],
          if (_voicePermissionEvidence != null) ...[
            Text(
              _voicePermissionEvidence!,
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 4),
          ],
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              IconButton(
                tooltip: '拍照提问',
                onPressed: _pickImage,
                icon: const Icon(Icons.photo_camera_outlined),
              ),
              IconButton(
                tooltip: '从相册选择截图',
                onPressed: _pickGalleryImage,
                icon: const Icon(Icons.photo_library_outlined),
              ),
              IconButton(
                tooltip: '导入截图或文件',
                onPressed: _pickFile,
                icon: const Icon(Icons.attach_file),
              ),
              Expanded(
                child: TextField(
                  controller: _queryController,
                  minLines: 1,
                  maxLines: 5,
                  textInputAction: TextInputAction.newline,
                  onChanged: (value) => _setQuery(value),
                  onSubmitted: (value) {
                    if (value.trim().isEmpty || !assistantEnabled) return;
                    _searchManually(value);
                  },
                  decoration: InputDecoration(
                    isDense: true,
                    hintText: '说点什么…',
                    border: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(22),
                    ),
                    enabledBorder: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(22),
                      borderSide: BorderSide(color: scheme.outlineVariant),
                    ),
                    focusedBorder: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(22),
                      borderSide:
                          BorderSide(color: scheme.primary, width: 1.4),
                    ),
                    contentPadding: const EdgeInsets.symmetric(
                      horizontal: 14,
                      vertical: 10,
                    ),
                    suffixIcon: IconButton(
                      tooltip: _listening ? '停止语音输入' : '语音输入',
                      onPressed: _toggleVoiceInput,
                      icon: Icon(
                        _listening ? Icons.mic : Icons.mic_none,
                        color: _listening ? scheme.primary : null,
                      ),
                    ),
                  ),
                ),
              ),
              const SizedBox(width: 6),
              IconButton.filledTonal(
                tooltip: '后台执行（长任务）',
                onPressed: query.trim().isEmpty || !assistantEnabled
                    ? null
                    : () => _enqueueBackground(query),
                icon: const Icon(Icons.schedule_send_outlined),
              ),
              const SizedBox(width: 4),
              IconButton.filled(
                tooltip: '发送给 AI 助手',
                onPressed: query.trim().isEmpty || !assistantEnabled
                    ? null
                    : () => _searchManually(query),
                icon: const Icon(Icons.arrow_upward),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Future<void> _enqueueBackground(String rawQuery) async {
    final query = rawQuery.trim();
    if (query.isEmpty) return;
    try {
      final job =
          await ref.read(assistantSearchProvider.notifier).enqueueBackground(query);
      if (!mounted) return;
      _setQuery('');
      if (job == null) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('后台任务未创建')),
        );
        return;
      }
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('已提交后台任务 ${job.jobId}'),
          action: SnackBarAction(
            label: '后台',
            onPressed: () {
              try {
                context.go('/tasks');
              } on Object {
                // Tests without GoRouter.
              }
            },
          ),
        ),
      );
    } on Object catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('后台提交失败：$e')),
      );
    }
  }

  Future<void> _openHistorySheet(
    AsyncValue<List<SearchHistoryEntry>> history,
  ) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (context) {
        return SafeArea(
          child: Padding(
            padding: EdgeInsets.only(
              left: 16,
              right: 16,
              bottom: MediaQuery.viewInsetsOf(context).bottom + 16,
              top: 4,
            ),
            child: ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.sizeOf(context).height * 0.72,
              ),
              child: SingleChildScrollView(
                child: _AssistantHistoryCard(
                  history: history,
                  onSelect: (value) {
                    Navigator.of(context).pop();
                    _setQuery(value);
                  },
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}

class _AssistantConversationView extends StatelessWidget {
  final List<AssistantConversationMessage> messages;
  final AsyncValue<SearchAnswer?> result;
  final SearchCitation? fallbackCitation;

  const _AssistantConversationView({
    required this.messages,
    required this.result,
    this.fallbackCitation,
  });

  @override
  Widget build(BuildContext context) {
    final lastUserQuery = messages
        .where((message) => message.role == 'user')
        .map((message) => message.text)
        .lastOrNull;
    final streamingAnswer = result.valueOrNull;
    final hasToolActivity =
        streamingAnswer != null && streamingAnswer.toolEvents.isNotEmpty;
    final showStreamingBubble = streamingAnswer != null &&
        streamingAnswer.streaming &&
        (streamingAnswer.answer.trim().isNotEmpty || hasToolActivity);
    final showTyping = result.isLoading ||
        (streamingAnswer != null &&
            streamingAnswer.streaming &&
            streamingAnswer.answer.trim().isEmpty &&
            !hasToolActivity);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (var index = 0; index < messages.length; index++)
          _buildMessage(context, messages[index], index == messages.length - 1),
        if (showTyping) const _AssistantTypingIndicator(),
        if (showStreamingBubble)
          _AssistantReplyBubble(
            query: lastUserQuery ?? '',
            answer: streamingAnswer.answer,
            citations: streamingAnswer.citations,
            llmMode: streamingAnswer.llmMode,
            llmRequestId: streamingAnswer.llmRequestId,
            llmUsageRecordId: streamingAnswer.llmUsageRecordId,
            fallbackCitation: fallbackCitation,
            streaming: true,
            toolEvents: streamingAnswer.toolEvents,
          ),
        if (result.hasError)
          _AssistantErrorBubble(
            error: result.error!,
            query: lastUserQuery ?? '',
          ),
      ],
    );
  }

  Widget _buildMessage(
    BuildContext context,
    AssistantConversationMessage message,
    bool isLatest,
  ) {
    if (message.role == 'user') {
      return ChatBubble(text: message.text, fromUser: true);
    }
    return _AssistantReplyBubble(
      query: message.query,
      answer: message.text,
      citations: message.citations,
      llmMode: message.llmMode,
      llmRequestId: message.llmRequestId,
      llmUsageRecordId: message.llmUsageRecordId,
      fallbackCitation: isLatest ? fallbackCitation : null,
    );
  }
}

class _AssistantCompanionIntro extends StatelessWidget {
  const _AssistantCompanionIntro();

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final textTheme = Theme.of(context).textTheme;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SizedBox(height: 24),
        Center(
          child: CircleAvatar(
            radius: 28,
            backgroundColor: scheme.primaryContainer.withValues(alpha: 0.7),
            child: Icon(
              Icons.auto_awesome,
              color: scheme.primary,
              size: 28,
            ),
          ),
        ),
        const SizedBox(height: 16),
        Center(
          child: Text(
            'MaClaw AI 助手',
            style: textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
          ),
        ),
        const SizedBox(height: 8),
        Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 320),
            child: Text(
              '你好。像桌面端一样，直接和我聊就行——查资料、写草稿、看日志、派员工，想到什么说什么。',
              textAlign: TextAlign.center,
              style: textTheme.bodyMedium?.copyWith(
                color: scheme.onSurfaceVariant,
                height: 1.45,
              ),
            ),
          ),
        ),
        const SizedBox(height: 18),
        // Capability labels kept for discoverability / tests.
        const Center(
          child: Wrap(
            alignment: WrapAlignment.center,
            spacing: 8,
            runSpacing: 8,
            children: [
              _AssistantCapabilityChip(icon: Icons.mic_none, label: '语音输入'),
              _AssistantCapabilityChip(
                icon: Icons.travel_explore_outlined,
                label: '助手联网',
              ),
              _AssistantCapabilityChip(
                icon: Icons.image_search_outlined,
                label: '截图提问',
              ),
              _AssistantCapabilityChip(
                icon: Icons.article_outlined,
                label: '文档草稿',
              ),
              _AssistantCapabilityChip(
                icon: Icons.manage_search_outlined,
                label: '远程排障',
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class _AssistantCapabilityChip extends StatelessWidget {
  final IconData icon;
  final String label;

  const _AssistantCapabilityChip({
    required this.icon,
    required this.label,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Chip(
      avatar: Icon(icon, size: 16, color: scheme.primary),
      label: Text(label),
      visualDensity: VisualDensity.compact,
      side: BorderSide(color: scheme.outlineVariant),
      backgroundColor: scheme.surface.withValues(alpha: 0.9),
    );
  }
}

class _AssistantTypingIndicator extends StatelessWidget {
  const _AssistantTypingIndicator();

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Align(
      alignment: AlignmentDirectional.centerStart,
      child: Container(
        margin: const EdgeInsets.only(bottom: 10, right: 48),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        decoration: BoxDecoration(
          color: scheme.surfaceContainerHighest,
          borderRadius: const BorderRadius.only(
            topLeft: Radius.circular(14),
            topRight: Radius.circular(14),
            bottomLeft: Radius.circular(4),
            bottomRight: Radius.circular(14),
          ),
          border: Border.all(color: scheme.outlineVariant),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: scheme.primary,
              ),
            ),
            const SizedBox(width: 10),
            Text(
              '正在想…',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
          ],
        ),
      ),
    );
  }
}

class _AssistantSearchUnavailableBanner extends StatelessWidget {
  const _AssistantSearchUnavailableBanner();

  @override
  Widget build(BuildContext context) {
    return const StatusBanner(
      tone: StatusTone.info,
      icon: Icons.info_outline,
      message:
          '当前 Hub 未启用 AI 助手服务能力，发送给 AI 助手暂不可用。仍可语音输入、导入图片/文件，或把内容整理成文档草稿。',
    );
  }
}

class _AssistantQuickPrompts extends StatelessWidget {
  final ValueChanged<String> onSelect;

  const _AssistantQuickPrompts({required this.onSelect});

  static const _prompts = [
    (
      label: '自由对话',
      icon: Icons.chat_bubble_outline,
      text: '我想和你随便聊聊一个问题，先帮我理清思路，有需要再追问我。',
    ),
    (
      label: '助手联网',
      icon: Icons.travel_explore_outlined,
      text: '帮我联网核对这件事，用普通人能懂的话说清楚，并带上来源。',
    ),
    (
      label: '文档草稿',
      icon: Icons.article_outlined,
      text: '帮我把下面要点整理成一份可直接发送的应急文档草稿。',
    ),
    (
      label: '日志排障',
      icon: Icons.fact_check_outlined,
      text: '帮我分析这段服务器日志，先说明风险，再给出需要人工确认的排查命令草案。',
    ),
  ];

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        children: [
          for (final prompt in _prompts) ...[
            ActionChip(
              avatar: Icon(prompt.icon, size: 16),
              label: Text(prompt.label),
              tooltip: prompt.text,
              visualDensity: VisualDensity.compact,
              onPressed: () => onSelect(prompt.text),
            ),
            const SizedBox(width: 8),
          ],
        ],
      ),
    );
  }
}

class _AssistantTabStrip extends StatelessWidget {
  final AssistantTabsState tabs;
  final ValueChanged<AssistantConversationTab> onActivate;
  final ValueChanged<AssistantConversationTab> onClose;
  final VoidCallback onAdd;

  const _AssistantTabStrip({
    required this.tabs,
    required this.onActivate,
    required this.onClose,
    required this.onAdd,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SizedBox(
      height: 44,
      child: Row(
        children: [
          Expanded(
            child: ListView.separated(
              scrollDirection: Axis.horizontal,
              itemBuilder: (context, index) {
                final tab = tabs.tabs[index];
                final selected = tab.id == tabs.activeTabId;
                return InputChip(
                  selected: selected,
                  showCheckmark: false,
                  avatar: Icon(
                    tab.primary ? Icons.chat_bubble_outline : Icons.tab,
                    size: 18,
                    color: selected ? scheme.primary : scheme.onSurfaceVariant,
                  ),
                  label: ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 120),
                    child: Text(
                      tab.title,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  onPressed: () => onActivate(tab),
                  onDeleted: tab.primary ? null : () => onClose(tab),
                  deleteIcon: const Icon(Icons.close, size: 18),
                );
              },
              separatorBuilder: (context, index) => const SizedBox(width: 8),
              itemCount: tabs.tabs.length,
            ),
          ),
          const SizedBox(width: 8),
          IconButton.outlined(
            tooltip: '新建副对话',
            onPressed: onAdd,
            icon: const Icon(Icons.add),
          ),
        ],
      ),
    );
  }
}

class _AssistantErrorBubble extends ConsumerWidget {
  final Object error;
  final String query;

  const _AssistantErrorBubble({
    required this.error,
    required this.query,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return ChatBubble(
      text: '刚才没接上：$error',
      failed: true,
      footer: Align(
        alignment: Alignment.centerLeft,
        child: TextButton.icon(
          onPressed: canRetryAssistantQuery(query)
              ? () => ref.read(assistantSearchProvider.notifier).search(query)
              : null,
          icon: const Icon(Icons.refresh, size: 18),
          label: const Text('重新发送'),
        ),
      ),
    );
  }
}

class _AssistantHistoryCard extends ConsumerWidget {
  final AsyncValue<List<SearchHistoryEntry>> history;
  final ValueChanged<String> onSelect;
  final bool bare;

  const _AssistantHistoryCard({
    required this.history,
    required this.onSelect,
    this.bare = false,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final content = history.when(
      data: (items) => _buildHistory(context, ref, items),
      error: (error, _) => Text('助手历史加载失败：$error'),
      loading: () => const LinearProgressIndicator(),
    );
    if (bare) return content;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: content,
      ),
    );
  }

  Widget _buildHistory(
    BuildContext context,
    WidgetRef ref,
    List<SearchHistoryEntry> items,
  ) {
    final favorites = items.where((item) => item.favorite).take(4).toList();
    final recent = items.where((item) => !item.favorite).take(6).toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SectionHeader(
          icon: Icons.history,
          title: '助手历史',
          action: items.any((item) => !item.favorite)
              ? TextButton.icon(
                  onPressed: () => _confirmClearNonFavorites(context, ref),
                  icon: const Icon(Icons.cleaning_services_outlined),
                  label: const Text('清理'),
                )
              : null,
        ),
        if (items.isEmpty) ...[
          const SizedBox(height: 8),
          Text(
            '发送给 AI 助手后的结果会保存在这里。',
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
          ),
        ] else ...[
          if (favorites.isNotEmpty) ...[
            const SizedBox(height: 8),
            _HistorySectionTitle(
              icon: Icons.star,
              label: '常用问题',
              count: favorites.length,
            ),
            for (final item in favorites)
              _HistoryItem(item: item, onSelect: onSelect),
          ],
          if (recent.isNotEmpty) ...[
            const SizedBox(height: 8),
            _HistorySectionTitle(
              icon: Icons.schedule,
              label: '最近对话',
              count: recent.length,
            ),
            for (final item in recent)
              _HistoryItem(item: item, onSelect: onSelect),
          ],
        ],
      ],
    );
  }

  Future<void> _confirmClearNonFavorites(
    BuildContext context,
    WidgetRef ref,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('只保留收藏问题？'),
        content: const Text(
          '将清理未收藏的助手历史，已收藏的常用问题会继续保留在本机，方便应急时快速复用。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('只保留收藏'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await ref.read(searchHistoryProvider.notifier).clearNonFavorites();
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('已清理未收藏历史，常用问题已保留')),
    );
  }
}

class _HistorySectionTitle extends StatelessWidget {
  final IconData icon;
  final String label;
  final int count;

  const _HistorySectionTitle({
    required this.icon,
    required this.label,
    required this.count,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Row(
      children: [
        Icon(icon, size: 18, color: scheme.primary),
        const SizedBox(width: 6),
        Text(label, style: Theme.of(context).textTheme.labelLarge),
        const SizedBox(width: 6),
        Text(
          '$count',
          style: Theme.of(context).textTheme.labelSmall?.copyWith(
                color: scheme.onSurfaceVariant,
              ),
        ),
      ],
    );
  }
}

class _HistoryItem extends ConsumerWidget {
  final SearchHistoryEntry item;
  final ValueChanged<String> onSelect;

  const _HistoryItem({
    required this.item,
    required this.onSelect,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return ListTile(
      dense: true,
      contentPadding: EdgeInsets.zero,
      leading: IconButton(
        tooltip: item.favorite ? '取消收藏' : '收藏',
        onPressed: () =>
            ref.read(searchHistoryProvider.notifier).toggleFavorite(item.id),
        icon: Icon(item.favorite ? Icons.star : Icons.star_border),
      ),
      title: Text(item.query),
      subtitle: Text(item.answerPreview),
      trailing: IconButton(
        tooltip: '删除',
        onPressed: () =>
            ref.read(searchHistoryProvider.notifier).remove(item.id),
        icon: const Icon(Icons.delete_outline),
      ),
      onTap: () => onSelect(item.query),
    );
  }
}

class _AssistantReplyBubble extends ConsumerWidget {
  final String query;
  final String answer;
  final List<SearchCitation> citations;
  final String llmMode;
  final String llmRequestId;
  final String llmUsageRecordId;
  final SearchCitation? fallbackCitation;
  final bool streaming;
  final List<AssistantToolEvent> toolEvents;

  const _AssistantReplyBubble({
    required this.query,
    required this.answer,
    required this.citations,
    required this.llmMode,
    required this.llmRequestId,
    required this.llmUsageRecordId,
    this.fallbackCitation,
    this.streaming = false,
    this.toolEvents = const [],
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    final mergedCitations = _mergedCitations();
    final cleanedAnswer =
        cleanSearchSnippet(answer, maxLength: 0, preserveNewlines: true);
    final markdown = _answerMarkdown(mergedCitations);
    final trace = assistantLlmTraceText(
      llmMode: llmMode,
      llmRequestId: llmRequestId,
      llmUsageRecordId: llmUsageRecordId,
    );
    final display = cleanedAnswer.isEmpty ? answer : cleanedAnswer;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // Soft speaker label — keeps chat conversational, not form-like.
        Padding(
          padding: const EdgeInsets.only(left: 4, bottom: 4),
          child: Text(
            streaming ? '助手正在回答…' : '助手回答',
            style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                  fontWeight: FontWeight.w600,
                ),
          ),
        ),
        ChatBubble(
          text: display.isEmpty && toolEvents.isNotEmpty ? ' ' : display,
          body: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (toolEvents.isNotEmpty) ...[
                _AssistantToolEventsStrip(events: toolEvents),
                if (display.trim().isNotEmpty) const SizedBox(height: 8),
              ],
              if (display.trim().isNotEmpty)
                AssistantMarkdownBody(
                  data: display,
                  textColor: scheme.onSurface,
                )
              else if (streaming && toolEvents.isNotEmpty)
                Text(
                  '工具执行中，正在整理回答…',
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                ),
            ],
          ),
          footer: streaming
              ? Padding(
                  padding: const EdgeInsets.only(top: 4),
                  child: Row(
                    children: [
                      SizedBox(
                        width: 12,
                        height: 12,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: scheme.primary,
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          toolEvents.isNotEmpty ? '工具调用 / 生成中' : '生成中',
                          style:
                              Theme.of(context).textTheme.labelSmall?.copyWith(
                                    color: scheme.onSurfaceVariant,
                                  ),
                        ),
                      ),
                      TextButton(
                        onPressed: () async {
                          try {
                            final job = await ref
                                .read(assistantSearchProvider.notifier)
                                .upgradeActiveToBackground();
                            if (!context.mounted) return;
                            if (job == null) {
                              ScaffoldMessenger.of(context).showSnackBar(
                                const SnackBar(content: Text('当前没有可转后台的生成任务')),
                              );
                              return;
                            }
                            ScaffoldMessenger.of(context).showSnackBar(
                              SnackBar(
                                content: Text('已转后台 ${job.jobId}'),
                                action: SnackBarAction(
                                  label: '后台',
                                  onPressed: () {
                                    try {
                                      context.go('/tasks');
                                    } on Object {
                                      // Tests without GoRouter.
                                    }
                                  },
                                ),
                              ),
                            );
                          } on Object catch (e) {
                            if (!context.mounted) return;
                            ScaffoldMessenger.of(context).showSnackBar(
                              SnackBar(content: Text('转后台失败：$e')),
                            );
                          }
                        },
                        child: const Text('转后台'),
                      ),
                    ],
                  ),
                )
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    // Lightweight share/copy stay as text actions (low chrome).
                    Wrap(
                      spacing: 4,
                      runSpacing: 0,
                      children: [
                        TextButton(
                          onPressed: () =>
                              ref.read(assistantShareProvider).call(markdown),
                          child: const Text('分享结果'),
                        ),
                        TextButton(
                          onPressed: () =>
                              _copyMarkdown(context, ref, markdown),
                          child: const Text('复制结果'),
                        ),
                      ],
                    ),
                    if (trace.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      _AssistantLlmTrace(text: trace),
                    ],
                    // Sources before task card so citation actions stay reachable.
                    if (mergedCitations.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      _AssistantSourcesPanel(citations: mergedCitations),
                    ],
                    // Follow-up actions only after a real answer body — not on
                    // empty/async placeholder bubbles (user input alone is not enough).
                    if (_hasUsableAssistantResult(
                      answer: display,
                      streaming: streaming,
                    ))
                      _AssistantNextStepsCard(
                        query: query,
                        answerMarkdown: markdown,
                        summaryLines: [
                          if (query.trim().isNotEmpty)
                            '问：${_clipRunes(redactMobileSensitiveText(query.trim()), 48)}',
                          if (mergedCitations.isNotEmpty)
                            '来源 ${mergedCitations.length} 条可展开核对',
                        ],
                        includeEmployee: ref
                                .watch(sessionControllerProvider)
                                .valueOrNull
                                ?.bootstrap
                                ?.features
                                .digitalEmployees !=
                            false,
                        includeDocuments: ref
                                .watch(sessionControllerProvider)
                                .valueOrNull
                                ?.bootstrap
                                ?.features
                                .documents !=
                            false,
                        onDraft: () =>
                            _createDocumentDraft(context, ref, markdown),
                      ),
                  ],
                ),
        ),
      ],
    );
  }

  List<SearchCitation> _mergedCitations() {
    return mergeAssistantCitations(citations, fallbackCitation);
  }

  String _answerMarkdown(List<SearchCitation> mergedCitations) {
    return assistantAnswerMarkdown(
      query: query,
      answer: answer,
      citations: mergedCitations,
    );
  }

  Future<void> _copyMarkdown(
    BuildContext context,
    WidgetRef ref,
    String markdown,
  ) async {
    await ref.read(assistantClipboardWriterProvider).call(markdown);
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('助手回答已复制')),
    );
  }

  Future<void> _createDocumentDraft(
    BuildContext context,
    WidgetRef ref,
    String markdown,
  ) async {
    final template = await showModalBottomSheet<DocumentTemplate>(
      context: context,
      builder: (context) => const _DocumentTemplateSheet(),
    );
    if (template == null || !context.mounted) return;
    await ref.read(documentsControllerProvider.notifier).createDraft(
          title: assistantDraftTitle(query),
          template: template,
          content: markdown,
        );
    if (!context.mounted) return;
    final err = ref.read(documentsControllerProvider).error;
    if (err != null) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('创建草稿失败：${_friendlyError(err)}')),
      );
      return;
    }
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text('已整理为${documentTemplateLabel(template)}草稿'),
        action: SnackBarAction(
          label: '查看',
          onPressed: () {
            if (!context.mounted) return;
            try {
              context.go('/documents');
            } on Object {
              // Widget tests host AssistantScreen without GoRouter.
            }
          },
        ),
      ),
    );
  }
}

String _friendlyError(Object err) {
  final text = err.toString();
  const prefixes = ['Bad state: ', 'StateError: ', 'Exception: '];
  for (final p in prefixes) {
    if (text.startsWith(p)) {
      return text.substring(p.length);
    }
  }
  return text;
}

/// Post-answer task card (GUI InlineChatCard-inspired, Dart-only).
class _AssistantNextStepsCard extends ConsumerStatefulWidget {
  final String query;
  final String answerMarkdown;
  final List<String> summaryLines;
  final bool includeEmployee;
  final bool includeDocuments;
  final Future<void> Function() onDraft;

  const _AssistantNextStepsCard({
    required this.query,
    required this.answerMarkdown,
    required this.summaryLines,
    required this.includeEmployee,
    required this.includeDocuments,
    required this.onDraft,
  });

  @override
  ConsumerState<_AssistantNextStepsCard> createState() =>
      _AssistantNextStepsCardState();
}

class _AssistantNextStepsCardState
    extends ConsumerState<_AssistantNextStepsCard> {
  String? _resolvedKey;
  String? _resolvedLabel;

  @override
  Widget build(BuildContext context) {
    return AssistantInlineCard(
      title: '可以继续',
      description: '把回答落到草稿、文档，或交给数字员工跟进。',
      summaryLines: widget.summaryLines,
      testId: 'assistant-next-steps-card',
      resolved: _resolvedKey != null,
      resolvedLabel: _resolvedLabel,
      actions: assistantDefaultNextStepActions(
        includeEmployee: widget.includeEmployee,
        includeDocuments: widget.includeDocuments,
      ),
      onAction: _handleAction,
    );
  }

  Future<void> _handleAction(String key) async {
    switch (key) {
      case 'draft':
        await widget.onDraft();
        // Keep card open so multi-template draft flows still work.
      case 'employee':
        if (!mounted) return;
        offerAssistantEmployeeHandoff(
          ref,
          query: widget.query,
          answer: widget.answerMarkdown,
        );
        // Tests host AssistantScreen without GoRouter; navigation is best-effort.
        try {
          context.go('/employees');
        } on Object {
          // Ignore missing router in pure widget tests.
        }
        if (!mounted) return;
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('已带入助手结论，正在打开数字员工任务草稿…'),
          ),
        );
        setState(() {
          _resolvedKey = key;
          _resolvedLabel = '已交接给数字员工';
        });
      case 'documents':
        if (!mounted) return;
        try {
          context.go('/documents');
        } on Object {
          // Ignore missing router in pure widget tests.
        }
        if (!mounted) return;
        setState(() {
          _resolvedKey = key;
          _resolvedLabel = '已打开文档';
        });
    }
  }
}

String _clipRunes(String text, int maxRunes) {
  final runes = text.runes.toList();
  if (runes.length <= maxRunes) return text;
  return '${String.fromCharCodes(runes.take(maxRunes))}…';
}

/// Whether the assistant bubble has a finished answer worth follow-up actions.
/// Empty / streaming / pure placeholder text must not show「可以继续」.
bool _hasUsableAssistantResult({
  required String answer,
  required bool streaming,
}) {
  if (streaming) return false;
  final body = answer.trim();
  if (body.isEmpty) return false;
  // Ignore very short system placeholders.
  if (body == '…' || body == '...' || body == '生成中') return false;
  return true;
}

class _AssistantToolEventsStrip extends StatelessWidget {
  final List<AssistantToolEvent> events;

  const _AssistantToolEventsStrip({required this.events});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          '工具调用',
          style: Theme.of(context).textTheme.labelSmall?.copyWith(
                color: scheme.onSurfaceVariant,
                fontWeight: FontWeight.w700,
              ),
        ),
        const SizedBox(height: 6),
        Wrap(
          spacing: 6,
          runSpacing: 6,
          children: [
            for (final event in events)
              Chip(
                visualDensity: VisualDensity.compact,
                avatar: Icon(
                  event.isCall ? Icons.build_circle_outlined : Icons.check_circle_outline,
                  size: 16,
                  color: event.isCall ? scheme.primary : scheme.tertiary,
                ),
                label: Text(
                  event.isCall
                      ? '调用 ${event.name}'
                      : '完成 ${event.name}',
                ),
                side: BorderSide(color: scheme.outlineVariant),
                backgroundColor: scheme.surfaceContainerHighest,
              ),
          ],
        ),
      ],
    );
  }
}

String assistantLlmTraceText({
  required String llmMode,
  required String llmRequestId,
  String llmUsageRecordId = '',
}) {
  final normalizedMode = llmMode.trim();
  final normalizedRequestId = llmRequestId.trim();
  final normalizedUsageRecordId = llmUsageRecordId.trim();
  if (normalizedMode.isEmpty &&
      normalizedRequestId.isEmpty &&
      normalizedUsageRecordId.isEmpty) {
    return '';
  }
  final modeLabel = switch (normalizedMode) {
    'official' => 'MaClaw 官方服务',
    'desktop_qr_third_party' => '桌面 GUI 授权服务',
    _ when normalizedMode.isEmpty => 'Hub LLM 服务',
    _ => 'Hub LLM 服务（$normalizedMode）',
  };
  final lines = <String>[modeLabel];
  if (normalizedRequestId.isNotEmpty) {
    lines.add('请求 ID: $normalizedRequestId');
  }
  if (normalizedUsageRecordId.isNotEmpty) {
    lines.add('用量记录: $normalizedUsageRecordId');
  }
  return lines.join('\n');
}

class _AssistantLlmTrace extends ConsumerWidget {
  final String text;

  const _AssistantLlmTrace({required this.text});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Padding(
        padding: const EdgeInsetsDirectional.fromSTEB(10, 8, 4, 8),
        child: Row(
          children: [
            Icon(
              Icons.receipt_long_outlined,
              size: 18,
              color: scheme.onSurfaceVariant,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                '请求追踪\n$text',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
              ),
            ),
            IconButton(
              tooltip: '复制请求追踪',
              onPressed: () async {
                await ref.read(assistantClipboardWriterProvider).call(text);
                if (!context.mounted) return;
                ScaffoldMessenger.of(context).showSnackBar(
                  const SnackBar(content: Text('请求追踪已复制')),
                );
              },
              icon: const Icon(Icons.content_copy_outlined),
            ),
          ],
        ),
      ),
    );
  }
}

String assistantDraftTitle(String query) {
  final compact = redactMobileSensitiveText(
    query.replaceAll(RegExp(r'\s+'), ' ').trim(),
  );
  if (compact.isEmpty) return 'AI助手整理';
  final title = _assistantDraftTitlePreview(compact);
  return 'AI助手：$title';
}

String _assistantDraftTitlePreview(String text) {
  const maxLength = 28;
  if (text.length <= maxLength) return text;
  var end = maxLength;
  for (final marker in const [
    '[REDACTED_SECRET]',
    '[REDACTED_TOKEN]',
    '[REDACTED_PRIVATE_KEY]',
    '[REDACTED_CREDENTIALS]',
  ]) {
    final start = text.lastIndexOf(marker, end);
    if (start >= 0 && start + marker.length > end) {
      end = start + marker.length;
      break;
    }
  }
  return '${text.substring(0, end)}...';
}

String assistantAnswerMarkdown({
  required String query,
  required String answer,
  required List<SearchCitation> citations,
}) {
  final buffer = StringBuffer();
  final normalizedQuery = query.trim();
  if (normalizedQuery.isNotEmpty) {
    buffer
      ..writeln('## 问题')
      ..writeln()
      ..writeln(redactMobileSensitiveText(normalizedQuery))
      ..writeln();
  }
  buffer
    ..writeln('## 结论')
    ..writeln()
    ..writeln(redactMobileSensitiveText(answer.trim()));
  if (citations.isNotEmpty) {
    buffer
      ..writeln()
      ..writeln('## 来源');
    for (final citation in citations) {
      buffer.writeln(assistantCitationMarkdown(citation));
    }
  }
  return buffer.toString().trimRight();
}

@Deprecated('Use assistantDraftTitle for the mobile AI assistant workspace.')
String assistantSearchDraftTitle(String query) => assistantDraftTitle(query);

@Deprecated(
  'Use assistantAnswerMarkdown for the mobile AI assistant workspace.',
)
String assistantSearchResultMarkdown({
  required String query,
  required String answer,
  required List<SearchCitation> citations,
}) =>
    assistantAnswerMarkdown(
      query: query,
      answer: answer,
      citations: citations,
    );

class _AssistantSourcesPanel extends StatelessWidget {
  final List<SearchCitation> citations;

  const _AssistantSourcesPanel({required this.citations});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final count = citations.length;
    return Theme(
      data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
      child: ExpansionTile(
        tilePadding: EdgeInsets.zero,
        childrenPadding: const EdgeInsets.only(bottom: 4),
        initiallyExpanded: false,
        title: Text(
          '来源 · $count 条',
          style: Theme.of(context).textTheme.labelLarge?.copyWith(
                color: scheme.onSurfaceVariant,
                fontWeight: FontWeight.w600,
              ),
        ),
        subtitle: Text(
          '展开查看参考链接',
          style: Theme.of(context).textTheme.labelSmall?.copyWith(
                color: scheme.onSurfaceVariant,
              ),
        ),
        children: [
          for (final citation in citations)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: _CitationTile(citation: citation),
            ),
        ],
      ),
    );
  }
}

class _CitationTile extends ConsumerWidget {
  final SearchCitation citation;

  const _CitationTile({required this.citation});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    final citationText = assistantCitationMarkdown(citation);
    final title = cleanSearchSnippet(
      citation.title.isEmpty ? citation.url : citation.title,
      maxLength: 80,
    );
    final host = citationHostLabel(citation.url);
    final snippet = cleanSearchSnippet(citation.snippet, maxLength: 100);
    return DecoratedBox(
      decoration: BoxDecoration(
        border: Border.all(color: scheme.outlineVariant),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(10, 8, 10, 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              title.isEmpty ? '来源' : title,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
            ),
            if (citation.url.isNotEmpty) ...[
              const SizedBox(height: 2),
              SelectableText(
                citation.url,
                maxLines: 1,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.primary,
                    ),
              ),
            ] else if (host.isNotEmpty) ...[
              const SizedBox(height: 2),
              Text(
                host,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.primary,
                    ),
              ),
            ],
            if (snippet.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(
                snippet,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
              ),
            ],
            const SizedBox(height: 4),
            Wrap(
              spacing: 4,
              children: [
                TextButton(
                  onPressed: citation.url.isEmpty
                      ? null
                      : () => _copyText(
                            context,
                            ref,
                            redactMobileSensitiveText(citation.url),
                            '来源链接已复制',
                          ),
                  child: const Text('复制链接'),
                ),
                TextButton(
                  onPressed: () =>
                      _copyText(context, ref, citationText, '来源引用已复制'),
                  child: const Text('复制引用'),
                ),
                TextButton(
                  onPressed: () =>
                      ref.read(assistantShareProvider).call(citationText),
                  child: const Text('分享来源'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _copyText(
    BuildContext context,
    WidgetRef ref,
    String text,
    String message,
  ) async {
    await ref.read(assistantClipboardWriterProvider).call(text);
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(message)),
    );
  }
}

String assistantCitationMarkdown(SearchCitation citation) {
  final title = redactMobileSensitiveText(
    cleanSearchSnippet(citation.title.trim(), maxLength: 0),
  );
  final url = redactMobileSensitiveText(citation.url.trim());
  final snippet = redactMobileSensitiveText(
    cleanSearchSnippet(citation.snippet.trim(), maxLength: 0),
  );
  final buffer = StringBuffer();
  if (title.isNotEmpty && url.isNotEmpty) {
    buffer.write('- $title $url');
  } else if (title.isNotEmpty) {
    buffer.write('- $title');
  } else if (url.isNotEmpty) {
    buffer.write('- $url');
  } else {
    buffer.write('- 来源');
  }
  if (snippet.isNotEmpty) {
    buffer
      ..writeln()
      ..write('  $snippet');
  }
  return buffer.toString();
}

class _DocumentTemplateSheet extends StatelessWidget {
  const _DocumentTemplateSheet();

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('选择草稿模板', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 8),
              for (final template in DocumentTemplate.values)
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: Icon(_documentTemplateIcon(template)),
                  title: Text(documentTemplateLabel(template)),
                  onTap: () => Navigator.of(context).pop(template),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

IconData _documentTemplateIcon(DocumentTemplate template) {
  return switch (template) {
    DocumentTemplate.notice => Icons.campaign_outlined,
    DocumentTemplate.report => Icons.summarize_outlined,
    DocumentTemplate.email => Icons.mail_outline,
    DocumentTemplate.proposal => Icons.lightbulb_outline,
    DocumentTemplate.meetingMinutes => Icons.groups_outlined,
    DocumentTemplate.statement => Icons.description_outlined,
  };
}
