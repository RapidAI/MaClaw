import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../assistant/assistant_voice_input.dart';
import '../../core/api/api_client.dart';
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
  }

  @override
  void dispose() {
    unawaited(_voiceInput.stop());
    _inputController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  void _listenForTaskUpdates() {
    ref.listen<AsyncValue<MobileDigitalEmployeeTask?>>(
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
              failed: task.status == 'failed',
            ),
          );
        });
        _scrollToEnd();
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    _listenForTaskUpdates();
    final taskState = ref.watch(digitalEmployeeTaskProvider);
    final sending = _activeTaskId != null || taskState.isLoading;
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.employee.name),
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
          const Divider(height: 1),
          Expanded(
            child: ListView.builder(
              controller: _scrollController,
              padding: const EdgeInsets.fromLTRB(16, 18, 16, 18),
              itemCount: _messages.length,
              itemBuilder: (context, index) =>
                  _EmployeeChatBubble(message: _messages[index]),
            ),
          ),
          if (sending) const LinearProgressIndicator(minHeight: 2),
          SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  IconButton(
                    tooltip: _listening ? '停止语音输入' : '语音输入',
                    onPressed: sending ? null : _toggleVoice,
                    icon: Icon(_listening ? Icons.stop : Icons.mic_none),
                  ),
                  Expanded(
                    child: TextField(
                      controller: _inputController,
                      minLines: 1,
                      maxLines: 5,
                      textInputAction: TextInputAction.newline,
                      enabled: !sending,
                      decoration: const InputDecoration(
                        hintText: '描述要处理的事情…',
                        border: OutlineInputBorder(),
                      ),
                      onSubmitted: (_) => _send(),
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton.filled(
                    tooltip: '发送',
                    onPressed: sending ? null : _send,
                    icon: const Icon(Icons.arrow_upward),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
      backgroundColor: scheme.surface,
    );
  }

  Future<void> _send() async {
    final prompt = _inputController.text.trim();
    if (prompt.isEmpty || _activeTaskId != null) return;
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
        curve: Curves.easeOut,
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
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
      child: Row(
        children: [
          CircleAvatar(
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
                ),
                const SizedBox(height: 3),
                Text(
                  employee.online ? '在线 · 通过所属 Hub 接入' : '离线 · 暂不可提交任务',
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: employee.online ? scheme.primary : scheme.error,
                      ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _EmployeeChatMessage {
  final String text;
  final bool fromUser;
  final bool failed;
  final String taskId;

  const _EmployeeChatMessage({
    required this.text,
    required this.fromUser,
    this.failed = false,
    this.taskId = '',
  });

  const _EmployeeChatMessage.user(String text)
      : this(text: text, fromUser: true);

  const _EmployeeChatMessage.employee(
    String text, {
    String taskId = '',
    bool failed = false,
  }) : this(text: text, fromUser: false, taskId: taskId, failed: failed);
}

class _EmployeeChatBubble extends StatelessWidget {
  final _EmployeeChatMessage message;

  const _EmployeeChatBubble({required this.message});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final color = message.fromUser
        ? scheme.primaryContainer
        : message.failed
            ? scheme.errorContainer
            : scheme.surfaceContainerHighest;
    return Align(
      alignment:
          message.fromUser ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        constraints: const BoxConstraints(maxWidth: 620),
        margin: const EdgeInsets.only(bottom: 12),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
        decoration: BoxDecoration(
          color: color,
          borderRadius: BorderRadius.circular(16),
        ),
        child: Text(message.text),
      ),
    );
  }
}
