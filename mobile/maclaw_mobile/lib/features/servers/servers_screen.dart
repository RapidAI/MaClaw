import 'dart:async';
import 'dart:convert';

import 'package:dartssh2/dartssh2.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:xterm/xterm.dart';

import '../../core/api/api_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../shared/surface.dart';
import '../auth/session_controller.dart';
import 'server_command.dart';
import 'server_profile.dart';
import 'servers_controller.dart';
import 'ssh_risk.dart';

class ServersScreen extends ConsumerStatefulWidget {
  const ServersScreen({super.key});

  @override
  ConsumerState<ServersScreen> createState() => _ServersScreenState();
}

class _ServersScreenState extends ConsumerState<ServersScreen> {
  final _commandController =
      TextEditingController(text: 'journalctl -u nginx -n 100 --no-pager');
  final _logController = TextEditingController();
  final _nameController = TextEditingController(text: '生产服务器');
  final _tagController = TextEditingController(text: '生产');
  final _noteController = TextEditingController();
  final _hostController = TextEditingController();
  final _portController = TextEditingController(text: '22');
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();
  final _privateKeyController = TextEditingController();
  final _privateKeyPassphraseController = TextEditingController();
  String _authMode = serverAuthModePassword;

  @override
  void dispose() {
    _commandController.dispose();
    _logController.dispose();
    _nameController.dispose();
    _tagController.dispose();
    _noteController.dispose();
    _hostController.dispose();
    _portController.dispose();
    _usernameController.dispose();
    _passwordController.dispose();
    _privateKeyController.dispose();
    _privateKeyPassphraseController.dispose();
    super.dispose();
  }

  Future<void> _importPrivateKey() async {
    final picked = await FilePicker.platform.pickFiles(
      type: FileType.any,
      withData: true,
    );
    if (picked == null || picked.files.isEmpty) return;
    final file = picked.files.single;
    final bytes = file.bytes;
    if (bytes == null) return;
    _privateKeyController.text = utf8.decode(bytes, allowMalformed: true);
    if (mounted) {
      setState(() => _authMode = serverAuthModePrivateKey);
    }
  }

  void _addServer() {
    final privateKey = _privateKeyController.text.trim();
    final profile = ServerProfile(
      id: DateTime.now().microsecondsSinceEpoch.toString(),
      name: _nameController.text.trim().isEmpty
          ? _hostController.text.trim()
          : _nameController.text.trim(),
      host: _hostController.text.trim(),
      port: int.tryParse(_portController.text.trim()) ?? 22,
      username: _usernameController.text.trim(),
      authMode: _authMode,
      tag:
          _tagController.text.trim().isEmpty ? null : _tagController.text.trim(),
      note: _noteController.text.trim().isEmpty
          ? null
          : _noteController.text.trim(),
    );
    if (!profile.isValid) return;
    if (_authMode == serverAuthModePrivateKey && privateKey.isEmpty) return;
    ref.read(serverProfilesProvider.notifier).addProfile(
          profile,
          password: _passwordController.text,
          privateKey: privateKey,
          privateKeyPassphrase: _privateKeyPassphraseController.text,
        );
    _passwordController.clear();
    _privateKeyController.clear();
    _privateKeyPassphraseController.clear();
    _noteController.clear();
  }

  Future<void> _deleteServer(ServerProfile profile) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除服务器配置？'),
        content: Text(
          '将删除 ${profile.name} 的连接配置，并清理本机保存的 SSH 密码/私钥。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await ref.read(serverProfilesProvider.notifier).removeProfile(profile.id);
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('已删除 ${profile.name}')),
    );
  }

  @override
  Widget build(BuildContext context) {
    final risk = classifyCommandRisk(_commandController.text);
    final analysis = ref.watch(sshAnalysisProvider);
    final profiles = ref.watch(serverProfilesProvider);
    return ScreenScaffold(
      title: '应急服务器',
      subtitle: '手动 SSH 维护，AI 只解释日志和生成命令草案。',
      trailing: IconButton.filledTonal(
        tooltip: '新增服务器',
        onPressed: _addServer,
        icon: const Icon(Icons.add),
      ),
      children: [
        _ServerProfileCard(
          nameController: _nameController,
          tagController: _tagController,
          noteController: _noteController,
          hostController: _hostController,
          portController: _portController,
          usernameController: _usernameController,
          passwordController: _passwordController,
          privateKeyController: _privateKeyController,
          privateKeyPassphraseController: _privateKeyPassphraseController,
          authMode: _authMode,
          onAuthModeChanged: (value) => setState(() => _authMode = value),
          onImportPrivateKey: _importPrivateKey,
          profiles: profiles,
          onAdd: _addServer,
          onDelete: _deleteServer,
        ),
        const SizedBox(height: 12),
        _SSHTerminalCard(
          profiles: profiles,
          onAnalyzeOutput: (output) {
            _logController.text = output;
            ref.read(sshAnalysisProvider.notifier).analyze(output);
          },
        ),
        const SizedBox(height: 12),
        _CommandRiskCard(
          controller: _commandController,
          risk: risk,
          onChanged: () => setState(() {}),
          onUseCommand: (command) {
            _commandController.text = command;
            setState(() {});
          },
        ),
        const SizedBox(height: 12),
        _SSHAnalysisCard(
          controller: _logController,
          analysis: analysis,
          onAnalyze: () =>
              ref.read(sshAnalysisProvider.notifier).analyze(_logController.text),
        ),
      ],
    );
  }
}

class _ServerProfileCard extends StatelessWidget {
  final TextEditingController nameController;
  final TextEditingController tagController;
  final TextEditingController noteController;
  final TextEditingController hostController;
  final TextEditingController portController;
  final TextEditingController usernameController;
  final TextEditingController passwordController;
  final TextEditingController privateKeyController;
  final TextEditingController privateKeyPassphraseController;
  final String authMode;
  final ValueChanged<String> onAuthModeChanged;
  final VoidCallback onImportPrivateKey;
  final AsyncValue<List<ServerProfile>> profiles;
  final VoidCallback onAdd;
  final Future<void> Function(ServerProfile profile) onDelete;

  const _ServerProfileCard({
    required this.nameController,
    required this.tagController,
    required this.noteController,
    required this.hostController,
    required this.portController,
    required this.usernameController,
    required this.passwordController,
    required this.privateKeyController,
    required this.privateKeyPassphraseController,
    required this.authMode,
    required this.onAuthModeChanged,
    required this.onImportPrivateKey,
    required this.profiles,
    required this.onAdd,
    required this.onDelete,
  });

  String _serverSubtitle(ServerProfile server) {
    final parts = [
      '${server.username}@${server.host}:${server.port}',
      serverAuthModeLabel(server.authMode),
      if ((server.tag ?? '').trim().isNotEmpty) server.tag!.trim(),
      if ((server.note ?? '').trim().isNotEmpty) server.note!.trim(),
    ];
    return parts.join(' · ');
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.dns_outlined, color: scheme.primary),
                const SizedBox(width: 8),
                Text('服务器配置', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 12),
            TextField(
              controller: nameController,
              decoration: const InputDecoration(
                labelText: '名称',
                prefixIcon: Icon(Icons.label_outline),
              ),
            ),
            const SizedBox(height: 10),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: tagController,
                    decoration: const InputDecoration(labelText: '标签'),
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: TextField(
                    controller: noteController,
                    decoration: const InputDecoration(labelText: '备注'),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            TextField(
              controller: hostController,
              decoration: const InputDecoration(
                labelText: 'Host',
                prefixIcon: Icon(Icons.cloud_outlined),
              ),
            ),
            const SizedBox(height: 10),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: portController,
                    keyboardType: TextInputType.number,
                    decoration: const InputDecoration(labelText: '端口'),
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: TextField(
                    controller: usernameController,
                    decoration: const InputDecoration(labelText: '用户名'),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            SegmentedButton<String>(
              segments: const [
                ButtonSegment(
                  value: serverAuthModePassword,
                  icon: Icon(Icons.password_outlined),
                  label: Text('密码'),
                ),
                ButtonSegment(
                  value: serverAuthModePrivateKey,
                  icon: Icon(Icons.vpn_key_outlined),
                  label: Text('私钥'),
                ),
              ],
              selected: {authMode},
              onSelectionChanged: (values) => onAuthModeChanged(values.first),
            ),
            const SizedBox(height: 10),
            if (authMode == serverAuthModePassword)
              TextField(
                controller: passwordController,
                obscureText: true,
                decoration: const InputDecoration(
                  labelText: '密码',
                  prefixIcon: Icon(Icons.password_outlined),
                ),
              )
            else ...[
              TextField(
                controller: privateKeyController,
                minLines: 4,
                maxLines: 8,
                decoration: InputDecoration(
                  labelText: '私钥 PEM',
                  prefixIcon: const Icon(Icons.vpn_key_outlined),
                  suffixIcon: IconButton(
                    tooltip: '从文件导入',
                    onPressed: onImportPrivateKey,
                    icon: const Icon(Icons.file_open_outlined),
                  ),
                ),
              ),
              const SizedBox(height: 10),
              TextField(
                controller: privateKeyPassphraseController,
                obscureText: true,
                decoration: const InputDecoration(
                  labelText: '私钥口令（可选）',
                  prefixIcon: Icon(Icons.lock_outline),
                ),
              ),
            ],
            const SizedBox(height: 12),
            FilledButton.icon(
              onPressed: onAdd,
              icon: const Icon(Icons.add),
              label: const Text('添加服务器'),
            ),
            profiles.when(
              data: (items) => items.isEmpty
                  ? const SizedBox.shrink()
                  : Column(
                      children: [
                        const SizedBox(height: 14),
                        for (final server in items)
                          ListTile(
                            dense: true,
                            contentPadding: EdgeInsets.zero,
                            leading: const Icon(Icons.storage_outlined),
                            title: Text(server.name),
                            subtitle: Text(
                              _serverSubtitle(server),
                            ),
                            trailing: IconButton(
                              tooltip: '删除服务器',
                              onPressed: () => onDelete(server),
                              icon: const Icon(Icons.delete_outline),
                            ),
                          ),
                      ],
                    ),
              error: (error, _) => Text('服务器配置加载失败：$error'),
              loading: () => const Padding(
                padding: EdgeInsets.only(top: 12),
                child: LinearProgressIndicator(),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SSHTerminalCard extends ConsumerStatefulWidget {
  final AsyncValue<List<ServerProfile>> profiles;
  final ValueChanged<String> onAnalyzeOutput;

  const _SSHTerminalCard({
    required this.profiles,
    required this.onAnalyzeOutput,
  });

  @override
  ConsumerState<_SSHTerminalCard> createState() => _SSHTerminalCardState();
}

enum _MobileSSHConnectionState { disconnected, connecting, connected }

class _SSHTerminalCardState extends ConsumerState<_SSHTerminalCard> {
  static const _maxCapturedOutputChars = 12000;

  final _terminal = Terminal(maxLines: 1000);
  final _capturedOutput = StringBuffer();
  SSHClient? _client;
  StreamSubscription<String>? _stdoutSub;
  StreamSubscription<String>? _stderrSub;
  String? _selectedId;
  ServerProfile? _activeProfile;
  _MobileSSHConnectionState _connectionState =
      _MobileSSHConnectionState.disconnected;
  String? _lastError;

  bool get _connecting =>
      _connectionState == _MobileSSHConnectionState.connecting;
  bool get _connected =>
      _connectionState == _MobileSSHConnectionState.connected;

  @override
  void initState() {
    super.initState();
    _writeTerminal('选择服务器后连接 SSH。\r\n', capture: false);
  }

  @override
  void dispose() {
    _stdoutSub?.cancel();
    _stderrSub?.cancel();
    _client?.close();
    super.dispose();
  }

  Future<void> _connect(List<ServerProfile> profiles) async {
    ServerProfile? selected;
    for (final profile in profiles) {
      if (profile.id == _selectedId) {
        selected = profile;
        break;
      }
    }
    if (selected == null || _connecting) return;
    await _closeActiveConnection(manual: true);
    _capturedOutput.clear();
    setState(() {
      _connectionState = _MobileSSHConnectionState.connecting;
      _activeProfile = selected;
      _lastError = null;
    });
    try {
      final socket = await SSHSocket.connect(selected.host, selected.port)
          .timeout(const Duration(seconds: 15));
      final client = await _buildClient(socket, selected);
      final session = await client.shell(
        pty: SSHPtyConfig(
          width: _terminal.viewWidth,
          height: _terminal.viewHeight,
        ),
      );
      _client = client;
      _writeTerminal('已连接 ${selected.name}\r\n');
      _terminal.onOutput = (data) {
        session.write(utf8.encode(data));
      };
      _terminal.onResize = (width, height, pixelWidth, pixelHeight) {
        session.resizeTerminal(width, height, pixelWidth, pixelHeight);
      };
      _stdoutSub = session.stdout
          .cast<List<int>>()
          .transform(const Utf8Decoder())
          .listen(
            _handleTerminalData,
            onError: _handleStreamError,
            onDone: _handleStreamDone,
          );
      _stderrSub = session.stderr
          .cast<List<int>>()
          .transform(const Utf8Decoder())
          .listen(
            _handleTerminalData,
            onError: _handleStreamError,
            onDone: _handleStreamDone,
          );
      if (mounted) {
        setState(() => _connectionState = _MobileSSHConnectionState.connected);
      }
    } catch (error) {
      _writeTerminal('连接失败：$error\r\n');
      if (mounted) {
        setState(() {
          _connectionState = _MobileSSHConnectionState.disconnected;
          _lastError = error.toString();
        });
      }
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: 'SSH 连接异常',
            body: '${selected.name} 连接失败',
            payload: selected.id,
          );
    }
  }

  Future<SSHClient> _buildClient(SSHSocket socket, ServerProfile profile) async {
    if (profile.authMode == serverAuthModePrivateKey) {
      final vault = ref.read(secureVaultProvider);
      final privateKey = await vault.readServerPrivateKey(profile.id);
      if (privateKey == null || privateKey.trim().isEmpty) {
        throw StateError('未保存 SSH 私钥。');
      }
      final passphrase =
          await vault.readServerPrivateKeyPassphrase(profile.id) ?? '';
      return SSHClient(
        socket,
        username: profile.username,
        identities: SSHKeyPair.fromPem(privateKey, passphrase).toList(),
      );
    }
    final password =
        await ref.read(secureVaultProvider).readServerPassword(profile.id);
    return SSHClient(
      socket,
      username: profile.username,
      onPasswordRequest: () => password ?? '',
    );
  }

  Future<void> _closeActiveConnection({required bool manual}) async {
    final wasConnected = _connected;
    await _stdoutSub?.cancel();
    await _stderrSub?.cancel();
    _stdoutSub = null;
    _stderrSub = null;
    _client?.close();
    _client = null;
    if (manual && wasConnected) {
      _writeTerminal('已断开 SSH 连接。\r\n');
    }
    if (mounted) {
      setState(() => _connectionState = _MobileSSHConnectionState.disconnected);
    }
  }

  void _handleStreamError(Object error, StackTrace stackTrace) {
    unawaited(_markDisconnected(error: error));
  }

  void _handleStreamDone() {
    unawaited(_markDisconnected());
  }

  Future<void> _markDisconnected({Object? error}) async {
    if (!_connected && !_connecting) return;
    final profile = _activeProfile;
    await _closeActiveConnection(manual: false);
    final message = error == null ? 'SSH 连接已断开。' : 'SSH 连接已断开：$error';
    _writeTerminal('$message\r\n');
    if (mounted) {
      setState(() => _lastError = error?.toString());
    }
    if (profile != null) {
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: 'SSH 连接已断开',
            body: profile.name,
            payload: profile.id,
          );
    }
  }

  String _statusLabel() {
    return switch (_connectionState) {
      _MobileSSHConnectionState.connecting => '连接中',
      _MobileSSHConnectionState.connected => '已连接',
      _MobileSSHConnectionState.disconnected => '未连接',
    };
  }

  Color _statusColor(ColorScheme scheme) {
    return switch (_connectionState) {
      _MobileSSHConnectionState.connecting => scheme.tertiary,
      _MobileSSHConnectionState.connected => scheme.primary,
      _MobileSSHConnectionState.disconnected => scheme.onSurfaceVariant,
    };
  }

  void _handleTerminalData(String data) {
    _writeTerminal(data);
  }

  void _writeTerminal(String data, {bool capture = true}) {
    _terminal.write(data);
    if (!capture || data.isEmpty) return;
    final wasEmpty = _capturedOutput.isEmpty;
    _capturedOutput.write(data);
    final text = _capturedOutput.toString();
    if (text.length > _maxCapturedOutputChars) {
      _capturedOutput
        ..clear()
        ..write(text.substring(text.length - _maxCapturedOutputChars));
    }
    if (wasEmpty && mounted) {
      setState(() {});
    }
  }

  String _recentOutputForAI() {
    return _capturedOutput.toString().trim();
  }

  void _sendOutputToAI() {
    final output = _recentOutputForAI();
    if (output.isEmpty) return;
    widget.onAnalyzeOutput(output);
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('已发送最近终端输出给 AI 分析')),
    );
  }

  Future<void> _copyRecentOutput() async {
    final output = _recentOutputForAI();
    if (output.isEmpty) return;
    await Clipboard.setData(ClipboardData(text: output));
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('终端输出已复制')),
    );
  }

  void _clearCapturedOutput() {
    _capturedOutput.clear();
    _terminal.write('\x1B[2J\x1B[H');
    _writeTerminal('终端输出已清空。\r\n', capture: false);
    setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: widget.profiles.when(
          data: (profiles) {
            final scheme = Theme.of(context).colorScheme;
            final statusColor = _statusColor(scheme);
            return Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Icon(Icons.terminal_outlined, color: scheme.primary),
                    const SizedBox(width: 8),
                    Text(
                      '手动 SSH 终端',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const Spacer(),
                    Chip(
                      visualDensity: VisualDensity.compact,
                      avatar: Icon(Icons.circle, size: 12, color: statusColor),
                      label: Text(_statusLabel()),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  value: _selectedId,
                  items: [
                    for (final profile in profiles)
                      DropdownMenuItem(
                        value: profile.id,
                        child: Text(
                          '${profile.name} · ${serverAuthModeLabel(profile.authMode)}',
                        ),
                      ),
                  ],
                  onChanged: _connecting || _connected
                      ? null
                      : (value) => setState(() => _selectedId = value),
                  decoration: const InputDecoration(labelText: '服务器'),
                ),
                if (_lastError != null) ...[
                  const SizedBox(height: 8),
                  Text(
                    '最近异常：$_lastError',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: scheme.error,
                        ),
                  ),
                ],
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    FilledButton.icon(
                      onPressed: _connecting ? null : () => _connect(profiles),
                      icon: const Icon(Icons.power_settings_new),
                      label: Text(_connected ? '重连 SSH' : '连接 SSH'),
                    ),
                    OutlinedButton.icon(
                      onPressed: _connected
                          ? () => _closeActiveConnection(manual: true)
                          : null,
                      icon: const Icon(Icons.link_off_outlined),
                      label: const Text('断开'),
                    ),
                    OutlinedButton.icon(
                      onPressed: _recentOutputForAI().isEmpty
                          ? null
                          : _sendOutputToAI,
                      icon: const Icon(Icons.psychology_alt_outlined),
                      label: const Text('交给 AI 分析'),
                    ),
                    IconButton.outlined(
                      tooltip: '复制终端输出',
                      onPressed: _recentOutputForAI().isEmpty
                          ? null
                          : _copyRecentOutput,
                      icon: const Icon(Icons.content_copy_outlined),
                    ),
                    IconButton.outlined(
                      tooltip: '清空终端输出',
                      onPressed:
                          _capturedOutput.isEmpty ? null : _clearCapturedOutput,
                      icon: const Icon(Icons.cleaning_services_outlined),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                SizedBox(
                  height: 280,
                  child: TerminalView(_terminal),
                ),
              ],
            );
          },
          error: (error, _) => Text('终端加载失败：$error'),
          loading: () => const LinearProgressIndicator(),
        ),
      ),
    );
  }
}

class _CommandRiskCard extends StatelessWidget {
  final TextEditingController controller;
  final CommandRisk risk;
  final VoidCallback onChanged;
  final ValueChanged<String> onUseCommand;

  const _CommandRiskCard({
    required this.controller,
    required this.risk,
    required this.onChanged,
    required this.onUseCommand,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final danger = risk == CommandRisk.dangerous;
    final caution = risk == CommandRisk.caution;
    final color = danger
        ? scheme.error
        : caution
            ? const Color(0xFF8A5A00)
            : scheme.primary;
    final label = danger
        ? '高风险，必须手动确认'
        : caution
            ? '需要谨慎检查'
            : '常规命令';
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.rule_outlined, color: color),
                const SizedBox(width: 8),
                Text('命令风险预检', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 12),
            TextField(
              controller: controller,
              minLines: 2,
              maxLines: 4,
              onChanged: (_) => onChanged(),
              decoration: const InputDecoration(
                labelText: '命令草案',
                prefixIcon: Icon(Icons.terminal_outlined),
              ),
            ),
            const SizedBox(height: 10),
            Text(label, style: TextStyle(color: color)),
            const SizedBox(height: 12),
            _CommandActionBar(
              command: controller.text,
              onUseCommand: onUseCommand,
            ),
          ],
        ),
      ),
    );
  }
}

class _CommandActionBar extends ConsumerWidget {
  final String command;
  final ValueChanged<String> onUseCommand;

  const _CommandActionBar({
    required this.command,
    required this.onUseCommand,
  });

  Future<bool> _confirmHighRiskCommand(BuildContext context) async {
    final risk = classifyCommandRisk(command);
    if (risk != CommandRisk.dangerous) return true;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('确认保存高风险命令？'),
        content: const Text(
          '该命令可能重启服务、删除数据或影响系统可用性。保存后仍需手动复制/执行，请确认你理解风险。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('确认保存'),
          ),
        ],
      ),
    );
    return confirmed == true;
  }

  Future<void> _recordCommand(
    BuildContext context,
    WidgetRef ref, {
    required bool favorite,
  }) async {
    if (!await _confirmHighRiskCommand(context)) return;
    if (!context.mounted) return;
    await ref.read(serverCommandsProvider.notifier).record(
          command,
          favorite: favorite,
        );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final commands = ref.watch(serverCommandsProvider);
    final presets = const [
      'systemctl status nginx --no-pager',
      'journalctl -u nginx -n 100 --no-pager',
      'df -h',
      'free -m',
      'docker ps --format "table {{.Names}}\\t{{.Status}}\\t{{.Ports}}"',
      'tail -n 120 /var/log/syslog',
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            OutlinedButton.icon(
              onPressed: command.trim().isEmpty
                  ? null
                  : () => _recordCommand(context, ref, favorite: true),
              icon: const Icon(Icons.star_border),
              label: const Text('保存常用'),
            ),
            OutlinedButton.icon(
              onPressed: command.trim().isEmpty
                  ? null
                  : () => _recordCommand(context, ref, favorite: false),
              icon: const Icon(Icons.history),
              label: const Text('记录历史'),
            ),
          ],
        ),
        const SizedBox(height: 12),
        Text('预置命令', style: Theme.of(context).textTheme.labelLarge),
        const SizedBox(height: 8),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (final preset in presets)
              ActionChip(
                avatar: const Icon(Icons.terminal_outlined, size: 18),
                label: Text(preset),
                onPressed: () => onUseCommand(preset),
              ),
          ],
        ),
        const SizedBox(height: 12),
        commands.when(
          data: (items) => _SavedCommandsList(
            items: items,
            onUseCommand: onUseCommand,
          ),
          error: (error, _) => Text('命令历史加载失败：$error'),
          loading: () => const LinearProgressIndicator(),
        ),
      ],
    );
  }
}

class _SavedCommandsList extends ConsumerWidget {
  final List<ServerCommandEntry> items;
  final ValueChanged<String> onUseCommand;

  const _SavedCommandsList({
    required this.items,
    required this.onUseCommand,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (items.isEmpty) {
      return Text(
        '保存常用命令或记录历史后，会显示在这里。',
        style: Theme.of(context).textTheme.bodySmall,
      );
    }
    final ordered = [
      ...items.where((item) => item.favorite),
      ...items.where((item) => !item.favorite),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('常用和历史命令', style: Theme.of(context).textTheme.labelLarge),
        const SizedBox(height: 6),
        for (final item in ordered.take(8))
          ListTile(
            dense: true,
            contentPadding: EdgeInsets.zero,
            leading: IconButton(
              tooltip: item.favorite ? '取消常用' : '设为常用',
              onPressed: () => ref
                  .read(serverCommandsProvider.notifier)
                  .toggleFavorite(item.id),
              icon: Icon(item.favorite ? Icons.star : Icons.star_border),
            ),
            title: Text(item.command),
            subtitle: Text(item.favorite ? '常用命令' : '历史命令'),
            trailing: IconButton(
              tooltip: '删除',
              onPressed: () =>
                  ref.read(serverCommandsProvider.notifier).remove(item.id),
              icon: const Icon(Icons.delete_outline),
            ),
            onTap: () => onUseCommand(item.command),
          ),
      ],
    );
  }
}

class _SSHAnalysisCard extends StatelessWidget {
  final TextEditingController controller;
  final AsyncValue<MobileSSHAnalysis?> analysis;
  final VoidCallback onAnalyze;

  const _SSHAnalysisCard({
    required this.controller,
    required this.analysis,
    required this.onAnalyze,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.psychology_alt_outlined,
                  color: Theme.of(context).colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Text(
                  'AI 分析终端输出',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ],
            ),
            const SizedBox(height: 12),
            TextField(
              controller: controller,
              minLines: 4,
              maxLines: 8,
              decoration: const InputDecoration(
                labelText: '终端输出或错误日志',
                hintText: '粘贴 systemctl、journalctl、docker logs 等输出',
              ),
            ),
            const SizedBox(height: 12),
            FilledButton.icon(
              onPressed: onAnalyze,
              icon: const Icon(Icons.manage_search_outlined),
              label: const Text('分析输出'),
            ),
            const SizedBox(height: 12),
            analysis.when(
              data: (value) => value == null
                  ? const SizedBox.shrink()
                  : Text('${value.summary}\n${value.recommendation}'),
              error: (error, _) => Text('分析失败：$error'),
              loading: () => const LinearProgressIndicator(),
            ),
          ],
        ),
      ),
    );
  }
}
