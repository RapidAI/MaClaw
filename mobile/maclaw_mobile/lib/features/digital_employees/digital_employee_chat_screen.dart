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

  @override
  void initState() {
    super.initState();
    _voiceInput = ref.read(assistantVoiceInputProvider);
    _messages.add(
      _EmployeeChatMessage.employee(
        '你好，我是${widget.employee.name}。告诉我需要处理的服务器、电脑或资料任务，我会通过所属 Hub 执行并把结果带回来。',
      ),
    );
    ref.listenManual<AsyncValue<MobileDigitalEmployeeTask?>>(
      digitalEmployeeTaskProvider,
      (previous, next) {
        final task = next.valueOrNull;
        if (!mounted || task == null || task.taskId != _activeTaskId) return;
        if (task.status != 'done' && task.status != 'failed') return;
        _activeTaskId = null;
        setState(() {
          _messages.add(
            _EmployeeChatMessage.employee(
              task.status == 'failed'
                  ? (task.message.trim().isEmpty ? '任务执行失败。' : task.message)
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
  void dispose() {
    unawaited(_voiceInput.stop());
    _inputController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
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
              widget.employee.online ? '在线' : '离线',
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
            tooltip: '刷新员工状态',
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
            const Padding(
              padding: EdgeInsets.fromLTRB(16, 0, 16, 8),
              child: StatusBanner(
                tone: StatusTone.warning,
                icon: Icons.cloud_off_outlined,
                message: '该数字员工当前不可提交任务，请确认远程端在线且运行时可用。',
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
                  tooltip: '添加附件',
                  onPressed: sending || unavailable
                      ? null
                      : () => _showAttachmentMenu(context),
                  icon: const Icon(Icons.attach_file),
                ),
                const SizedBox(width: 6),
                IconButton.outlined(
                  tooltip: _listening ? '停止语音输入' : '语音输入',
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
                    decoration: const InputDecoration(
                      hintText: '描述要处理的事情…',
                    ),
                    onSubmitted: (_) => _send(),
                  ),
                ),
                const SizedBox(width: 8),
                IconButton.filled(
                  tooltip: '发送',
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
          const _EmployeeChatMessage.employee(
            '任务提交失败，请检查登录状态或网络连接。',
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
                ? (task.message.trim().isEmpty ? '任务执行失败。' : task.message)
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
          '已收到，我正在处理这项任务。完成后会把结果发回这里。',
          taskId: task.taskId,
        ),
      );
    }
    _scrollToEnd();
  }

  Future<void> _showAttachmentMenu(BuildContext context) async {
    final action = await showModalBottomSheet<_EmployeeAttachmentAction>(
      context: context,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.photo_camera_outlined),
              title: const Text('拍照并上传'),
              onTap: () => Navigator.pop(
                context,
                _EmployeeAttachmentAction.camera,
              ),
            ),
            ListTile(
              leading: const Icon(Icons.photo_library_outlined),
              title: const Text('从相册选择图片'),
              onTap: () => Navigator.pop(
                context,
                _EmployeeAttachmentAction.gallery,
              ),
            ),
            ListTile(
              leading: const Icon(Icons.upload_file_outlined),
              title: const Text('选择文档'),
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
        const SnackBar(content: Text('无法打开附件选择器，请检查权限后重试。')),
      );
      return;
    }
    final selectedPath = path;
    if (!mounted || selectedPath == null || selectedPath.trim().isEmpty) return;
    setState(() {
      _messages.add(
        const _EmployeeChatMessage.user('已选择附件，正在提交 Hub 文档解析。'),
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
              ? '附件提交失败，请到文档页查看错误并重试。'
              : '附件已提交到 Hub 文档解析${upload?.taskId.isNotEmpty == true ? '，解析完成后可以继续告诉我如何处理。' : '。'}',
          failed: documentState.hasError,
        ),
      );
    });
    _scrollToEnd();
  }

  Future<void> _toggleVoice() async {
    if (_listening) {
      await _voiceInput.stop();
      if (mounted) setState(() => _listening = false);
      return;
    }
    setState(() => _listening = true);
    try {
      final started = await _voiceInput.start(
        localeId: 'zh_CN',
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
          const SnackBar(content: Text('语音输入不可用，请检查麦克风权限。')),
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

class _EmployeeChatHeader extends StatelessWidget {
  final DigitalEmployee employee;

  const _EmployeeChatHeader({required this.employee});

  @override
  Widget build(BuildContext context) {
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
                      employee.online ? '在线 · 通过所属 Hub 接入' : '离线 · 暂不可提交任务',
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
  final String taskId;
  final MobileDigitalEmployeeTask? task;

  const _EmployeeChatMessage({
    required this.text,
    required this.fromUser,
    this.failed = false,
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
  }) : this(
          text: text,
          fromUser: false,
          taskId: taskId,
          task: task,
          failed: failed,
        );
}

enum _EmployeeAttachmentAction { camera, gallery, file }

class _EmployeeTaskStatus extends StatelessWidget {
  final MobileDigitalEmployeeTask task;

  const _EmployeeTaskStatus({required this.task});

  @override
  Widget build(BuildContext context) {
    final waitingForApproval = {
      'approval_required',
      'pending_approval',
      'awaiting_approval',
      'authorization_required',
      'waiting_authorization',
    }.contains(task.status);
    final label = waitingForApproval
        ? '等待远程授权'
        : switch (task.status) {
            'queued' => '等待远程领取',
            'claimed' || 'running' || 'in_progress' => '远程处理中',
            'authorization_denied' ||
            'approval_denied' ||
            'rejected' =>
              '远程授权被拒绝',
            _ => '任务处理中',
          };
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
      child: StatusBanner(
        tone: waitingForApproval ? StatusTone.warning : StatusTone.info,
        icon: waitingForApproval ? Icons.gpp_maybe_outlined : Icons.sync,
        title: label,
        message: task.message.trim().isEmpty
            ? '任务仍在远程处理中'
            : task.message.trim(),
      ),
    );
  }
}

class _EmployeeChatBubble extends StatelessWidget {
  final _EmployeeChatMessage message;

  const _EmployeeChatBubble({required this.message});

  @override
  Widget build(BuildContext context) {
    return ChatBubble(
      text: message.text,
      fromUser: message.fromUser,
      failed: message.failed,
      footer: !message.fromUser &&
              message.task != null &&
              message.task!.result.trim().isNotEmpty
          ? _EmployeeResultActions(task: message.task!)
          : null,
    );
  }
}

class _EmployeeResultActions extends ConsumerWidget {
  final MobileDigitalEmployeeTask task;

  const _EmployeeResultActions({required this.task});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Wrap(
      spacing: 4,
      runSpacing: 4,
      children: [
        IconButton(
          tooltip: '复制结果',
          visualDensity: VisualDensity.compact,
          onPressed: () async {
            await Clipboard.setData(
              ClipboardData(text: redactMobileSensitiveText(task.result)),
            );
            if (!context.mounted) return;
            ScaffoldMessenger.of(context).showSnackBar(
              const SnackBar(content: Text('任务结果已复制')),
            );
          },
          icon: const Icon(Icons.content_copy_outlined, size: 18),
        ),
        IconButton(
          tooltip: '分享结果',
          visualDensity: VisualDensity.compact,
          onPressed: () async {
            await Share.share(redactMobileSensitiveText(task.result));
          },
          icon: const Icon(Icons.ios_share_outlined, size: 18),
        ),
        IconButton(
          tooltip: '整理为草稿',
          visualDensity: VisualDensity.compact,
          onPressed: () async {
            await ref.read(documentsControllerProvider.notifier).createDraft(
                  title: '数字员工任务结果',
                  template: DocumentTemplate.report,
                  content: redactMobileSensitiveText(task.result),
                );
            if (!context.mounted) return;
            ScaffoldMessenger.of(context).showSnackBar(
              const SnackBar(content: Text('已整理为文档草稿')),
            );
          },
          icon: const Icon(Icons.article_outlined, size: 18),
        ),
      ],
    );
  }
}
