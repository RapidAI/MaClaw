import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:file_picker/file_picker.dart';
import 'package:image_picker/image_picker.dart';
import 'package:share_plus/share_plus.dart';

import '../assistant/assistant_voice_input.dart';
import '../../core/api/api_client.dart';
import '../../core/security/mobile_redaction.dart';
import '../../l10n/app_strings.dart';
import '../../shared/surface.dart';
import '../../shared/theme.dart';
import '../documents/document_draft.dart';
import '../documents/documents_controller.dart';
import 'digital_employee.dart';
import 'digital_employees_controller.dart';

class DigitalEmployeeChatScreen extends ConsumerStatefulWidget {
  final DigitalEmployee employee;

  const DigitalEmployeeChatScreen({super.key, required this.employee});

  @override
  ConsumerState<DigitalEmployeeChatScreen> createState() =>
      _DigitalEmployeeChatScreenState();
}

class _DigitalEmployeeChatScreenState
    extends ConsumerState<DigitalEmployeeChatScreen> {
  final _inputController = TextEditingController();
  final _scrollController = ScrollController();
  late final AssistantVoiceInput _voiceInput;
  final _messages = <_EmployeeChatMessage>[];
  String? _activeTaskId;
  bool _listening = false;
  bool _greetingSeeded = false;

  @override
  void initState() {
    super.initState();
    _voiceInput = ref.read(assistantVoiceInputProvider);
    ref.listenManual<AsyncValue<MobileDigitalEmployeeTask?>>(
      digitalEmployeeTaskProvider,
      (previous, next) {
        final s = ref.read(appStringsProvider);
        final task = next.valueOrNull;
        if (!mounted || task == null || task.taskId != _activeTaskId) return;

        // Live progress from Hub realtime / poll: update streaming bubble.
        if (task.status != 'done' && task.status != 'failed') {
          final preview = _taskProgressPreview(task);
          if (preview == null || preview.isEmpty) return;
          setState(() {
            final idx = _messages.lastIndexWhere(
              (m) => m.taskId == task.taskId && m.streaming,
            );
            final bubble = _EmployeeChatMessage.employee(
              preview,
              taskId: task.taskId,
              task: task,
              streaming: true,
            );
            if (idx >= 0) {
              _messages[idx] = bubble;
            } else {
              // Replace static "running" ack with live progress when possible.
              final ackIdx = _messages.lastIndexWhere(
                (m) => m.taskId == task.taskId && !m.fromUser && !m.failed,
              );
              if (ackIdx >= 0 && !_messages[ackIdx].streaming) {
                _messages[ackIdx] = bubble;
              } else {
                _messages.add(bubble);
              }
            }
          });
          _scrollToEnd();
          return;
        }

        _activeTaskId = null;
        setState(() {
          _messages.removeWhere((m) => m.taskId == task.taskId && m.streaming);
          _messages.add(
            _EmployeeChatMessage.employee(
              task.status == 'failed'
                  ? (task.message.trim().isEmpty
                      ? s.taskFailedDefault
                      : task.message)
                  : (task.result.trim().isEmpty ? task.message : task.result),
              taskId: task.taskId,
              task: task,
              failed: task.status == 'failed',
            ),
          );
        });
        _scrollToEnd();
      },
    );
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_greetingSeeded) return;
    _greetingSeeded = true;
    final s = ref.read(appStringsProvider);
    _messages.add(
      _EmployeeChatMessage.employee(
        s.employeeGreeting(widget.employee.name),
      ),
    );
  }

  @override
  void dispose() {
    unawaited(_voiceInput.stop());
    _inputController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final s = ref.watch(appStringsProvider);
    final taskState = ref.watch(digitalEmployeeTaskProvider);
    final sending = _activeTaskId != null || taskState.isLoading;
    final activeTask = taskState.valueOrNull;
    final unavailable = !widget.employee.canSubmitTask;
    final scheme = Theme.of(context).colorScheme;
    final dark = Theme.of(context).brightness == Brightness.dark;
    return Scaffold(
      backgroundColor:
          dark ? MaClawColors.darkScaffold : scheme.surfaceContainerLowest,
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(widget.employee.name),
            Text(
              widget.employee.online ? s.online : s.offline,
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                    color: widget.employee.online
                        ? scheme.secondary
                        : scheme.onSurfaceVariant,
                  ),
            ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: s.refreshEmployeeStatus,
            onPressed: () =>
                ref.read(digitalEmployeesProvider.notifier).refresh(),
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: Column(
        children: [
          _EmployeeChatHeader(employee: widget.employee),
          if (unavailable)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
              child: StatusBanner(
                tone: StatusTone.warning,
                icon: Icons.cloud_off_outlined,
                message: s.chatUnavailable,
              ),
            ),
          if (activeTask != null &&
              activeTask.employeeId == widget.employee.id &&
              activeTask.status != 'done' &&
              activeTask.status != 'failed')
            _EmployeeTaskStatus(task: activeTask),
          Expanded(
            child: ListView.builder(
              controller: _scrollController,
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 18),
              itemCount: _messages.length,
              itemBuilder: (context, index) =>
                  _EmployeeChatBubble(message: _messages[index]),
            ),
          ),
          if (sending)
            LinearProgressIndicator(
              minHeight: 2,
              backgroundColor: scheme.surfaceContainerHighest,
            ),
          ChatComposerDock(
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                IconButton.outlined(
                  tooltip: s.addAttachment,
                  onPressed: sending || unavailable
                      ? null
                      : () => _showAttachmentMenu(context),
                  icon: const Icon(Icons.attach_file),
                ),
                const SizedBox(width: 6),
                IconButton.outlined(
                  tooltip: _listening ? s.stopVoiceInput : s.voiceInput,
                  onPressed: sending || unavailable ? null : _toggleVoice,
                  icon: Icon(
                    _listening ? Icons.stop : Icons.mic_none,
                    color: _listening ? scheme.primary : null,
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: TextField(
                    controller: _inputController,
                    minLines: 1,
                    maxLines: 5,
                    textInputAction: TextInputAction.newline,
                    enabled: !sending && !unavailable,
                    decoration: InputDecoration(
                      hintText: s.chatHint,
                    ),
                    onSubmitted: (_) => _send(),
                  ),
                ),
                const SizedBox(width: 8),
                IconButton.filled(
                  tooltip: s.send,
                  onPressed: sending || unavailable ? null : _send,
                  icon: const Icon(Icons.arrow_upward),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _send() async {
    final s = ref.read(appStringsProvider);
    final prompt = _inputController.text.trim();
    if (prompt.isEmpty ||
        _activeTaskId != null ||
        !widget.employee.canSubmitTask) {
      return;
    }
    _inputController.clear();
    setState(() {
      _messages.add(_EmployeeChatMessage.user(prompt));
    });
    _scrollToEnd();
    await ref.read(digitalEmployeeTaskProvider.notifier).createTask(
      employeeId: widget.employee.id,
      prompt: prompt,
      taskType: 'general',
      context: const {
        'source': 'maclaw_mobile_employee_chat',
        'conversation_mode': 'employee_chat',
        'manual_confirmation_required': 'true',
      },
    );
    if (!mounted) return;
    final task = ref.read(digitalEmployeeTaskProvider).valueOrNull;
    if (task == null) {
      setState(() {
        _messages.add(
          _EmployeeChatMessage.employee(
            s.taskSubmitFailed,
            failed: true,
          ),
        );
      });
      return;
    }
    if (task.status == 'done' || task.status == 'failed') {
      setState(() {
        _messages.add(
          _EmployeeChatMessage.employee(
            task.status == 'failed'
                ? (task.message.trim().isEmpty
                    ? s.taskFailedDefault
                    : task.message)
                : (task.result.trim().isEmpty ? task.message : task.result),
            taskId: task.taskId,
            task: task,
            failed: task.status == 'failed',
          ),
        );
      });
    } else {
      setState(() => _activeTaskId = task.taskId);
      _messages.add(
        _EmployeeChatMessage.employee(
          s.taskRunningAck,
          taskId: task.taskId,
        ),
      );
    }
    _scrollToEnd();
  }

  Future<void> _showAttachmentMenu(BuildContext context) async {
    final s = ref.read(appStringsProvider);
    final action = await showModalBottomSheet<_EmployeeAttachmentAction>(
      context: context,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.photo_camera_outlined),
              title: Text(s.takePhotoUpload),
              onTap: () => Navigator.pop(
                context,
                _EmployeeAttachmentAction.camera,
              ),
            ),
            ListTile(
              leading: const Icon(Icons.photo_library_outlined),
              title: Text(s.pickFromGallery),
              onTap: () => Navigator.pop(
                context,
                _EmployeeAttachmentAction.gallery,
              ),
            ),
            ListTile(
              leading: const Icon(Icons.upload_file_outlined),
              title: Text(s.pickDocument),
              onTap: () => Navigator.pop(
                context,
                _EmployeeAttachmentAction.file,
              ),
            ),
          ],
        ),
      ),
    );
    if (!mounted || action == null) return;
    String? path;
    try {
      path = switch (action) {
        _EmployeeAttachmentAction.camera =>
          (await ImagePicker().pickImage(source: ImageSource.camera))?.path,
        _EmployeeAttachmentAction.gallery =>
          (await ImagePicker().pickImage(source: ImageSource.gallery))?.path,
        _EmployeeAttachmentAction.file =>
          (await FilePicker.platform.pickFiles())?.files.single.path,
      };
    } on Object {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(s.attachmentPickerFailed)),
      );
      return;
    }
    final selectedPath = path;
    if (!mounted || selectedPath == null || selectedPath.trim().isEmpty) return;
    setState(() {
      _messages.add(
        _EmployeeChatMessage.user(s.attachmentSubmitting),
      );
    });
    _scrollToEnd();
    await ref
        .read(documentsControllerProvider.notifier)
        .uploadSharedDocument(selectedPath);
    if (!mounted) return;
    final documentState = ref.read(documentsControllerProvider);
    final upload = documentState.valueOrNull?.uploadTask;
    setState(() {
      _messages.add(
        _EmployeeChatMessage.employee(
          documentState.hasError
              ? s.attachmentSubmitFailed
              : (upload?.taskId.isNotEmpty == true
                  ? s.attachmentSubmittedContinue
                  : s.attachmentSubmitted),
          failed: documentState.hasError,
        ),
      );
    });
    _scrollToEnd();
  }

  Future<void> _toggleVoice() async {
    final s = ref.read(appStringsProvider);
    if (_listening) {
      await _voiceInput.stop();
      if (mounted) setState(() => _listening = false);
      return;
    }
    setState(() => _listening = true);
    try {
      final localeId = s.isZh ? 'zh_CN' : 'en_US';
      final started = await _voiceInput.start(
        localeId: localeId,
        onText: (transcript) {
          if (!mounted || transcript.trim().isEmpty) return;
          _inputController.text = transcript.trim();
          _inputController.selection = TextSelection.fromPosition(
            TextPosition(offset: _inputController.text.length),
          );
        },
        onStatus: (status) {
          if (mounted && (status == 'done' || status == 'notListening')) {
            setState(() => _listening = false);
          }
        },
      );
      if (!started && mounted) setState(() => _listening = false);
    } on Object {
      if (mounted) {
        setState(() => _listening = false);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(s.voiceUnavailable)),
        );
      }
    }
  }

  void _scrollToEnd() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scrollController.hasClients) return;
      _scrollController.animateTo(
        _scrollController.position.maxScrollExtent,
        duration: const Duration(milliseconds: 220),
        curve: Curves.easeOutCubic,
      );
    });
  }
}

class _EmployeeChatHeader extends ConsumerWidget {
  final DigitalEmployee employee;

  const _EmployeeChatHeader({required this.employee});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Material(
      color: scheme.surface,
      child: DecoratedBox(
        decoration: BoxDecoration(
          border: Border(
            bottom: BorderSide(color: scheme.outlineVariant),
          ),
        ),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
          child: Row(
            children: [
              CircleAvatar(
                radius: 22,
                backgroundColor: employee.online
                    ? scheme.secondaryContainer
                    : scheme.surfaceContainerHighest,
                child: Icon(
                  employee.online ? Icons.smart_toy_outlined : Icons.cloud_off,
                  color: employee.online
                      ? scheme.onSecondaryContainer
                      : scheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      employee.skillDescription,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: text.bodyMedium?.copyWith(height: 1.35),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      employee.online ? s.onlineViaHub : s.offlineCannotSubmit,
                      style: text.bodySmall?.copyWith(
                        color: employee.online
                            ? scheme.secondary
                            : scheme.onSurfaceVariant,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _EmployeeChatMessage {
  final String text;
  final bool fromUser;
  final bool failed;
  final bool streaming;
  final String taskId;
  final MobileDigitalEmployeeTask? task;

  const _EmployeeChatMessage({
    required this.text,
    required this.fromUser,
    this.failed = false,
    this.streaming = false,
    this.taskId = '',
    this.task,
  });

  const _EmployeeChatMessage.user(String text)
      : this(text: text, fromUser: true);

  const _EmployeeChatMessage.employee(
    String text, {
    String taskId = '',
    MobileDigitalEmployeeTask? task,
    bool failed = false,
    bool streaming = false,
  }) : this(
          text: text,
          fromUser: false,
          taskId: taskId,
          task: task,
          failed: failed,
          streaming: streaming,
        );
}

enum _EmployeeAttachmentAction { camera, gallery, file }

class _EmployeeTaskStatus extends ConsumerWidget {
  final MobileDigitalEmployeeTask task;

  const _EmployeeTaskStatus({required this.task});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final waitingForApproval = {
      'approval_required',
      'pending_approval',
      'awaiting_approval',
      'authorization_required',
      'waiting_authorization',
    }.contains(task.status);
    final queuedStuck =
        task.status == 'queued' && _taskQueuedLongerThan(task, const Duration(seconds: 25));
    final running = task.status == 'claimed' ||
        task.status == 'running' ||
        task.status == 'in_progress';
    final label = waitingForApproval
        ? s.employeeTaskStatusLabel('approval_required')
        : switch (task.status) {
            'queued' => queuedStuck
                ? s.remoteStillUnclaimed
                : s.employeeTaskStatusLabel('queued'),
            'claimed' || 'running' || 'in_progress' =>
              s.employeeTaskStatusLabel('running'),
            'authorization_denied' ||
            'approval_denied' ||
            'rejected' =>
              s.employeeTaskStatusLabel('rejected'),
            _ => s.taskProcessing,
          };
    final defaultMessage = switch (task.status) {
      'queued' when queuedStuck => s.queuedStuckHint,
      'queued' => s.taskSubmittedWaitingClaim,
      _ => s.taskStillProcessingRemote,
    };
    final messageLooksLikeWaitRemote = task.message.contains('等待远程') ||
        task.message.toLowerCase().contains('waiting');
    // Prefer live agent output (result) while running so the banner shows
    // progressive generation text from Hub realtime patches.
    final progressPreview = _taskProgressPreview(task);
    final String bannerMessage;
    if (running && progressPreview != null && progressPreview.isNotEmpty) {
      bannerMessage = progressPreview;
    } else if (task.message.trim().isEmpty) {
      bannerMessage = defaultMessage;
    } else if (queuedStuck && messageLooksLikeWaitRemote) {
      bannerMessage = defaultMessage;
    } else {
      bannerMessage = task.message.trim();
    }
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          StatusBanner(
            tone: waitingForApproval || queuedStuck
                ? StatusTone.warning
                : StatusTone.info,
            icon: waitingForApproval || queuedStuck
                ? Icons.gpp_maybe_outlined
                : Icons.sync,
            title: label,
            message: bannerMessage,
          ),
          if (running) ...[
            const SizedBox(height: 6),
            ClipRRect(
              borderRadius: BorderRadius.circular(999),
              child: LinearProgressIndicator(
                minHeight: 3,
                backgroundColor:
                    Theme.of(context).colorScheme.surfaceContainerHighest,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// Prefer newest agent text for live progress (result), then message.
String? _taskProgressPreview(MobileDigitalEmployeeTask task) {
  final preview = digitalEmployeeTaskProgressPreview(
    result: task.result,
    message: task.message,
  );
  return preview.isEmpty ? null : preview;
}

bool _taskQueuedLongerThan(MobileDigitalEmployeeTask task, Duration threshold) {
  final raw = task.createdAt.trim();
  if (raw.isEmpty) return false;
  final created = DateTime.tryParse(raw);
  if (created == null) return false;
  return DateTime.now().toUtc().difference(created.toUtc()) >= threshold;
}

class _EmployeeChatBubble extends ConsumerWidget {
  final _EmployeeChatMessage message;

  const _EmployeeChatBubble({required this.message});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    final s = ref.watch(appStringsProvider);
    return Column(
      crossAxisAlignment:
          message.fromUser ? CrossAxisAlignment.end : CrossAxisAlignment.start,
      children: [
        ChatBubble(
          text: message.text,
          fromUser: message.fromUser,
          failed: message.failed,
          footer: !message.fromUser &&
                  !message.streaming &&
                  message.task != null &&
                  message.task!.result.trim().isNotEmpty
              ? _EmployeeResultActions(task: message.task!)
              : null,
        ),
        if (message.streaming)
          Padding(
            padding: const EdgeInsets.only(left: 12, top: 2, bottom: 8),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                SizedBox(
                  width: 10,
                  height: 10,
                  child: CircularProgressIndicator(
                    strokeWidth: 1.5,
                    color: scheme.primary,
                  ),
                ),
                const SizedBox(width: 6),
                Text(
                  s.employeeTaskStatusLabel('in_progress'),
                  style: Theme.of(context).textTheme.labelSmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

class _EmployeeResultActions extends ConsumerWidget {
  final MobileDigitalEmployeeTask task;

  const _EmployeeResultActions({required this.task});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    return Wrap(
      spacing: 4,
      runSpacing: 4,
      children: [
        IconButton(
          tooltip: s.copyResult,
          visualDensity: VisualDensity.compact,
          onPressed: () async {
            await Clipboard.setData(
              ClipboardData(text: redactMobileSensitiveText(task.result)),
            );
            if (!context.mounted) return;
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(content: Text(s.taskResultCopied)),
            );
          },
          icon: const Icon(Icons.content_copy_outlined, size: 18),
        ),
        IconButton(
          tooltip: s.shareResultTooltip,
          visualDensity: VisualDensity.compact,
          onPressed: () async {
            await Share.share(redactMobileSensitiveText(task.result));
          },
          icon: const Icon(Icons.ios_share_outlined, size: 18),
        ),
        IconButton(
          tooltip: s.makeDraftFromResult,
          visualDensity: VisualDensity.compact,
          onPressed: () async {
            await ref.read(documentsControllerProvider.notifier).createDraft(
                  title: s.employeeTaskResultTitle,
                  template: DocumentTemplate.report,
                  content: redactMobileSensitiveText(task.result),
                );
            if (!context.mounted) return;
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(content: Text(s.draftFromResultOk)),
            );
          },
          icon: const Icon(Icons.article_outlined, size: 18),
        ),
      ],
    );
  }
}
