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
import '../../core/security/mobile_redaction.dart';
import '../../shared/surface.dart';
import '../auth/session_controller.dart';
import '../digital_employees/digital_employee.dart';
import '../digital_employees/digital_employees_controller.dart';
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
  final _terminalKey = GlobalKey<_SSHTerminalCardState>();
  String _authMode = serverAuthModePassword;
  ServerProfile? _analysisProfile;
  var _settingLogFromTerminal = false;

  @override
  void initState() {
    super.initState();
    _logController.addListener(() {
      if (!_settingLogFromTerminal) {
        _analysisProfile = null;
      }
    });
  }

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
    final bytes = picked.files.single.bytes;
    if (bytes == null) return;
    _privateKeyController.text = utf8.decode(bytes, allowMalformed: true);
    if (mounted) {
      setState(() => _authMode = serverAuthModePrivateKey);
    }
  }

  Future<void> _addServer() async {
    final privateKey = _privateKeyController.text.trim();
    final portText = _portController.text.trim();
    final port = int.tryParse(portText);
    if (_hostController.text.trim().isEmpty) {
      _showServerProfileError('请输入服务器 Host。');
      return;
    }
    if (port == null || port <= 0 || port > 65535) {
      _showServerProfileError('请输入 1-65535 范围内的 SSH 端口。');
      return;
    }
    if (_usernameController.text.trim().isEmpty) {
      _showServerProfileError('请输入 SSH 用户名。');
      return;
    }
    if (_authMode == serverAuthModePrivateKey && privateKey.isEmpty) {
      _showServerProfileError('私钥登录需要填写或导入私钥。');
      return;
    }
    final profile = ServerProfile(
      id: DateTime.now().microsecondsSinceEpoch.toString(),
      name: _nameController.text.trim().isEmpty
          ? _hostController.text.trim()
          : _nameController.text.trim(),
      host: _hostController.text.trim(),
      port: port,
      username: _usernameController.text.trim(),
      authMode: _authMode,
      tag: _tagController.text.trim().isEmpty
          ? null
          : _tagController.text.trim(),
      note: _noteController.text.trim().isEmpty
          ? null
          : _noteController.text.trim(),
    );
    try {
      await ref.read(serverProfilesProvider.notifier).addProfile(
            profile,
            password: _passwordController.text,
            privateKey: privateKey,
            privateKeyPassphrase: _privateKeyPassphraseController.text,
          );
    } catch (error) {
      _showServerProfileError('服务器配置保存失败：$error');
      return;
    }
    _passwordController.clear();
    _privateKeyController.clear();
    _privateKeyPassphraseController.clear();
    _noteController.clear();
  }

  void _showServerProfileError(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(message)),
    );
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

  Future<bool> _sendCommandToTerminal(String command) async {
    return _terminalKey.currentState?.sendCommand(command) ?? false;
  }

  Future<void> _analyzeManualLog() async {
    final output = _logController.text;
    final confirmed = await confirmMobileSSHLogAnalysis(
      context,
      output,
      source: 'manual',
    );
    if (!confirmed || !mounted) return;
    await ref
        .read(sshAnalysisProvider.notifier)
        .analyze(redactMobileSensitiveText(output));
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
          key: _terminalKey,
          profiles: profiles,
          onAnalyzeOutput: (output) {
            _settingLogFromTerminal = true;
            _logController.text = output;
            _analysisProfile = _terminalKey.currentState?.activeProfile;
            _settingLogFromTerminal = false;
            ref
                .read(sshAnalysisProvider.notifier)
                .analyze(redactMobileSensitiveText(output));
          },
        ),
        const SizedBox(height: 12),
        _CommandRiskCard(
          controller: _commandController,
          risk: risk,
          onChanged: () => setState(() {}),
          onSendCommand: _sendCommandToTerminal,
          onUseCommand: (command) {
            _commandController.text = command;
            setState(() {});
          },
        ),
        const SizedBox(height: 12),
        _SSHAnalysisCard(
          controller: _logController,
          analysis: analysis,
          serverProfile: _analysisProfile,
          onAnalyze: _analyzeManualLog,
          onUseCommand: (command) {
            _commandController.text = command;
            setState(() {});
          },
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
                            subtitle: Text(_serverSubtitle(server)),
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
    super.key,
    required this.profiles,
    required this.onAnalyzeOutput,
  });

  @override
  ConsumerState<_SSHTerminalCard> createState() => _SSHTerminalCardState();
}

enum _MobileSSHConnectionState { disconnected, connecting, connected }

@visibleForTesting
final mobileSshTerminalInitialOutputProvider = Provider<String>((ref) => '');

typedef MobileSshSocketConnector = Future<SSHSocket> Function(
  String host,
  int port,
);

@visibleForTesting
final mobileSshSocketConnectorProvider = Provider<MobileSshSocketConnector>(
  (ref) => (host, port) =>
      SSHSocket.connect(host, port).timeout(const Duration(seconds: 15)),
);

typedef MobileClipboardWriter = Future<void> Function(String text);

@visibleForTesting
final mobileClipboardWriterProvider = Provider<MobileClipboardWriter>(
  (ref) => (text) => Clipboard.setData(ClipboardData(text: text)),
);

String? mobileTerminalCommandPayload(String command) {
  final text = command.trim();
  if (text.isEmpty) return null;
  return '$text\r';
}

String? mobileSshReconnectProfileId({
  required String? selectedId,
  required String? activeProfileId,
  required Iterable<String> availableProfileIds,
}) {
  final available = availableProfileIds.toSet();
  if (activeProfileId != null && available.contains(activeProfileId)) {
    return activeProfileId;
  }
  if (selectedId != null && available.contains(selectedId)) {
    return selectedId;
  }
  return null;
}

const _confirmSendHighRiskTitle =
    '\u786e\u8ba4\u53d1\u9001\u9ad8\u98ce\u9669\u547d\u4ee4\uff1f';
const _confirmSaveHighRiskTitle =
    '\u786e\u8ba4\u4fdd\u5b58\u9ad8\u98ce\u9669\u547d\u4ee4\uff1f';
const _confirmSendHighRiskBody =
    '\u8be5\u547d\u4ee4\u53ef\u80fd\u91cd\u542f\u670d\u52a1\u3001\u5220\u9664\u6570\u636e\u6216\u5f71\u54cd\u7cfb\u7edf\u53ef\u7528\u6027\u3002\u53d1\u9001\u540e\u4f1a\u8fdb\u5165\u5f53\u524d SSH \u7ec8\u7aef\u6267\u884c\uff0c\u8bf7\u786e\u8ba4\u4f60\u7406\u89e3\u98ce\u9669\u3002';
const _confirmSaveHighRiskBody =
    '\u8be5\u547d\u4ee4\u53ef\u80fd\u91cd\u542f\u670d\u52a1\u3001\u5220\u9664\u6570\u636e\u6216\u5f71\u54cd\u7cfb\u7edf\u53ef\u7528\u6027\u3002\u4fdd\u5b58\u540e\u4ecd\u9700\u624b\u52a8\u590d\u5236/\u6267\u884c\uff0c\u8bf7\u786e\u8ba4\u4f60\u7406\u89e3\u98ce\u9669\u3002';
const _cancelLabel = '\u53d6\u6d88';
const _confirmSendLabel = '\u786e\u8ba4\u53d1\u9001';
const _confirmSaveLabel = '\u786e\u8ba4\u4fdd\u5b58';
const _sendTerminalOutputTitle =
    '\u53d1\u9001\u7ec8\u7aef\u8f93\u51fa\u7ed9 AI\uff1f';
const _sendRecentTerminalOutputBody =
    '\u5c06\u628a\u6700\u8fd1\u7ec8\u7aef\u8f93\u51fa\u53d1\u9001\u5230 MaClaw \u5b98\u65b9\u670d\u52a1\u8fdb\u884c\u5206\u6790\u3002';
const _sendPastedTerminalOutputBody =
    '\u5c06\u628a\u5f53\u524d\u7c98\u8d34\u7684\u7ec8\u7aef\u8f93\u51fa\u53d1\u9001\u5230 MaClaw \u5b98\u65b9\u670d\u52a1\u8fdb\u884c\u5206\u6790\u3002';
const _terminalOutputSummaryPrefix = '\u5171\u7ea6';
const _lineCountUnit = '\u884c\u3001';
const _charCountUnit = '\u4e2a\u5b57\u7b26\u3002';
const _sensitiveDataWarning =
    '\u5e38\u89c1\u5bc6\u7801\u3001Token\u3001\u79c1\u94a5\u548c\u5e26\u51ed\u636e URL \u4f1a\u5148\u672c\u5730\u8131\u654f\uff1b\u53d1\u9001\u524d\u4ecd\u8bf7\u68c0\u67e5\u5ba2\u6237\u6570\u636e\u7b49\u654f\u611f\u5185\u5bb9\u3002';

Future<bool> confirmMobileHighRiskCommand(
  BuildContext context,
  String command, {
  required String action,
}) async {
  final risk = classifyCommandRisk(command);
  if (risk != CommandRisk.dangerous) return true;
  final sending = action == 'send';
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: Text(
        sending ? _confirmSendHighRiskTitle : _confirmSaveHighRiskTitle,
      ),
      content: Text(
        sending ? _confirmSendHighRiskBody : _confirmSaveHighRiskBody,
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text(_cancelLabel),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: Text(sending ? _confirmSendLabel : _confirmSaveLabel),
        ),
      ],
    ),
  );
  return confirmed == true;
}

Future<bool> confirmMobileSSHLogAnalysis(
  BuildContext context,
  String output, {
  required String source,
}) async {
  final text = output.trim();
  if (text.isEmpty) return false;
  final lineCount = RegExp(r'\r\n|\r|\n').allMatches(text).length + 1;
  final preview = text.length > 420 ? '${text.substring(0, 420)}...' : text;
  final automatic = source == 'terminal';
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text(_sendTerminalOutputTitle),
      content: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              automatic
                  ? _sendRecentTerminalOutputBody
                  : _sendPastedTerminalOutputBody,
            ),
            const SizedBox(height: 8),
            Text(
              '$_terminalOutputSummaryPrefix $lineCount $_lineCountUnit'
              '${text.length} $_charCountUnit$_sensitiveDataWarning',
            ),
            const SizedBox(height: 12),
            SelectableText(
              preview,
              maxLines: 8,
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text(_cancelLabel),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text(_confirmSendLabel),
        ),
      ],
    ),
  );
  return confirmed == true;
}

class _SSHTerminalCardState extends ConsumerState<_SSHTerminalCard> {
  static const _maxCapturedOutputChars = 12000;

  final _terminal = Terminal(maxLines: 1000);
  final _capturedOutput = StringBuffer();
  SSHClient? _client;
  StreamSubscription<String>? _stdoutSub;
  StreamSubscription<String>? _stderrSub;
  String? _selectedId;
  String? _lastConnectedProfileId;
  ServerProfile? _activeProfile;
  _MobileSSHConnectionState _connectionState =
      _MobileSSHConnectionState.disconnected;
  String? _lastError;

  bool get _connecting =>
      _connectionState == _MobileSSHConnectionState.connecting;
  bool get _connected =>
      _connectionState == _MobileSSHConnectionState.connected;

  ServerProfile? get activeProfile => _activeProfile;

  @override
  void initState() {
    super.initState();
    _writeTerminal('选择服务器后连接 SSH。\r\n', capture: false);
    final initialOutput = ref.read(mobileSshTerminalInitialOutputProvider);
    if (initialOutput.trim().isNotEmpty) {
      _writeTerminal(initialOutput);
    }
  }

  @override
  void dispose() {
    _stdoutSub?.cancel();
    _stderrSub?.cancel();
    _client?.close();
    super.dispose();
  }

  Future<void> _connect(
    List<ServerProfile> profiles, {
    String? preferredProfileId,
  }) async {
    ServerProfile? selected;
    final targetId = preferredProfileId ?? _selectedId;
    for (final profile in profiles) {
      if (profile.id == targetId) {
        selected = profile;
        break;
      }
    }
    if (selected == null && profiles.length == 1) {
      selected = profiles.single;
    }
    if (selected == null || _connecting) return;
    final selectedProfile = selected;
    await _closeActiveConnection(manual: true);
    _capturedOutput.clear();
    setState(() {
      _connectionState = _MobileSSHConnectionState.connecting;
      _activeProfile = selectedProfile;
      _selectedId = selectedProfile.id;
      _lastError = null;
    });
    try {
      final socket = await ref.read(mobileSshSocketConnectorProvider)(
        selectedProfile.host,
        selectedProfile.port,
      );
      final client = await _buildClient(socket, selectedProfile);
      final session = await client.shell(
        pty: SSHPtyConfig(
          width: _terminal.viewWidth,
          height: _terminal.viewHeight,
        ),
      );
      _client = client;
      _lastConnectedProfileId = selectedProfile.id;
      _writeTerminal('已连接 ${selectedProfile.name}\r\n');
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
            body: '${selectedProfile.name} 连接失败',
            payload: mobileServerProfileNotificationPayload(
              selectedProfile.id,
            ),
          );
    }
  }

  Future<SSHClient> _buildClient(
    SSHSocket socket,
    ServerProfile profile,
  ) async {
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
            payload: mobileServerProfileNotificationPayload(profile.id),
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

  String? _reconnectProfileId(List<ServerProfile> profiles) {
    return mobileSshReconnectProfileId(
      selectedId: _selectedId,
      activeProfileId: _lastConnectedProfileId ?? _activeProfile?.id,
      availableProfileIds: profiles.map((profile) => profile.id),
    );
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

  bool sendCommand(String command) {
    final payload = mobileTerminalCommandPayload(command);
    if (!_connected || payload == null) return false;
    _terminal.onOutput?.call(payload);
    return true;
  }

  Future<void> _sendOutputToAI() async {
    final output = _recentOutputForAI();
    if (output.isEmpty) return;
    final confirmed = await confirmMobileSSHLogAnalysis(
      context,
      output,
      source: 'terminal',
    );
    if (!confirmed || !mounted) return;
    widget.onAnalyzeOutput(output);
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('已发送最近终端输出给 AI 分析')),
    );
  }

  Future<void> _copyRecentOutput() async {
    final output = _recentOutputForAI();
    if (output.isEmpty) return;
    await ref.read(mobileClipboardWriterProvider)(output);
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
            final reconnectId = _reconnectProfileId(profiles);
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
                  initialValue: _selectedId,
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
                    if (!_connected && reconnectId != null)
                      OutlinedButton.icon(
                        onPressed: _connecting
                            ? null
                            : () => _connect(
                                  profiles,
                                  preferredProfileId: reconnectId,
                                ),
                        icon: const Icon(Icons.restart_alt_outlined),
                        label: const Text('重连上次'),
                      ),
                    OutlinedButton.icon(
                      onPressed: _connected
                          ? () => _closeActiveConnection(manual: true)
                          : null,
                      icon: const Icon(Icons.link_off_outlined),
                      label: const Text('断开'),
                    ),
                    OutlinedButton.icon(
                      onPressed:
                          _recentOutputForAI().isEmpty ? null : _sendOutputToAI,
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
  final Future<bool> Function(String command) onSendCommand;
  final ValueChanged<String> onUseCommand;

  const _CommandRiskCard({
    required this.controller,
    required this.risk,
    required this.onChanged,
    required this.onSendCommand,
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
                labelText: '命令草稿',
                prefixIcon: Icon(Icons.terminal_outlined),
              ),
            ),
            const SizedBox(height: 10),
            Text(label, style: TextStyle(color: color)),
            const SizedBox(height: 12),
            _CommandActionBar(
              command: controller.text,
              onSendCommand: onSendCommand,
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
  final Future<bool> Function(String command) onSendCommand;
  final ValueChanged<String> onUseCommand;

  const _CommandActionBar({
    required this.command,
    required this.onSendCommand,
    required this.onUseCommand,
  });

  Future<void> _recordCommand(
    BuildContext context,
    WidgetRef ref, {
    required bool favorite,
  }) async {
    if (!await confirmMobileHighRiskCommand(
      context,
      command,
      action: 'save',
    )) {
      return;
    }
    if (!context.mounted) return;
    await ref.read(serverCommandsProvider.notifier).record(
          command,
          favorite: favorite,
        );
  }

  Future<void> _sendCommand(BuildContext context, WidgetRef ref) async {
    if (!await confirmMobileHighRiskCommand(
      context,
      command,
      action: 'send',
    )) {
      return;
    }
    if (!context.mounted) return;
    final sent = await onSendCommand(command);
    if (!context.mounted) return;
    if (sent) {
      await ref.read(serverCommandsProvider.notifier).record(command);
    }
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(sent ? '命令已发送到 SSH 终端' : '请先连接 SSH 终端'),
      ),
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final commands = ref.watch(serverCommandsProvider);
    const presets = [
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
            FilledButton.icon(
              onPressed: command.trim().isEmpty
                  ? null
                  : () => _sendCommand(context, ref),
              icon: const Icon(Icons.send_outlined),
              label: const Text('发送到终端'),
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

class _SSHAnalysisCard extends ConsumerWidget {
  final TextEditingController controller;
  final AsyncValue<MobileSSHAnalysis?> analysis;
  final ServerProfile? serverProfile;
  final VoidCallback onAnalyze;
  final ValueChanged<String> onUseCommand;

  const _SSHAnalysisCard({
    required this.controller,
    required this.analysis,
    required this.serverProfile,
    required this.onAnalyze,
    required this.onUseCommand,
  });

  Future<void> _copyCommandDraft(
    BuildContext context,
    String command,
  ) async {
    await Clipboard.setData(ClipboardData(text: command));
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('命令草稿已复制')),
    );
  }

  Future<void> _sendToDigitalEmployee(
    BuildContext context,
    WidgetRef ref,
  ) async {
    final rawOutput = controller.text.trim();
    if (rawOutput.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('请先粘贴终端输出或错误日志。')),
      );
      return;
    }
    final output = redactMobileSensitiveText(rawOutput);
    final employees = await _loadAvailableDigitalEmployees(ref);
    if (!context.mounted) return;
    if (employees.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('暂无可用数字员工，请先在远程端启用。')),
      );
      return;
    }
    final request = await _pickDigitalEmployeeForOutput(
      context,
      employees,
      output,
    );
    if (request == null || !context.mounted) return;
    await ref.read(digitalEmployeeTaskProvider.notifier).createTask(
          employeeId: request.employeeId,
          prompt: request.prompt,
          taskType: 'server_maintenance',
          context: mobileSSHOutputTaskContext(output, profile: serverProfile),
        );
    if (!context.mounted) return;
    final taskState = ref.read(digitalEmployeeTaskProvider);
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          taskState.hasError
              ? '提交数字员工失败：${taskState.error}'
              : '已提交给数字员工，远程端会按策略领取或请求授权。',
        ),
      ),
    );
  }

  Future<List<DigitalEmployee>> _loadAvailableDigitalEmployees(
    WidgetRef ref,
  ) async {
    final cached = ref.read(digitalEmployeesProvider).valueOrNull;
    if (cached != null) {
      return cached.where((employee) => employee.canSubmitTask).toList();
    }
    final employees = await ref.read(digitalEmployeesProvider.future);
    return employees.where((employee) => employee.canSubmitTask).toList();
  }

  Future<_DigitalEmployeeOutputRequest?> _pickDigitalEmployeeForOutput(
    BuildContext context,
    List<DigitalEmployee> employees,
    String output,
  ) {
    final summary = mobileSSHOutputSubmissionSummary(output);
    final promptController = TextEditingController(
      text: digitalEmployeeOutputPrompt(output),
    );
    var selectedId = employees.first.id;
    return showDialog<_DigitalEmployeeOutputRequest>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, setState) => AlertDialog(
          title: const Text('交给数字员工处理'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                DropdownButtonFormField<String>(
                  initialValue: selectedId,
                  items: [
                    for (final employee in employees)
                      DropdownMenuItem(
                        value: employee.id,
                        child: Text('${employee.name} · ${employee.machineId}'),
                      ),
                  ],
                  onChanged: (value) {
                    if (value != null) setState(() => selectedId = value);
                  },
                  decoration: const InputDecoration(labelText: '数字员工'),
                ),
                const SizedBox(height: 12),
                Text(
                  '将提交约 ${summary.lineCount} 行、${summary.charCount} 个字符到 MaClaw 官方服务。常见密码、Token、私钥和带凭据 URL 会先本地脱敏；提交前仍请检查客户数据等敏感内容。',
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                ),
                const SizedBox(height: 8),
                SelectableText(
                  summary.preview,
                  maxLines: 6,
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: promptController,
                  minLines: 6,
                  maxLines: 10,
                  decoration: const InputDecoration(
                    labelText: '任务说明',
                    alignLabelWithHint: true,
                  ),
                ),
                const SizedBox(height: 8),
                Text(
                  '任务只会提交到 MaClaw 官方服务；远程端仍按数字员工权限策略领取、确认或拒绝。',
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(context).pop(),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.of(context).pop(
                _DigitalEmployeeOutputRequest(
                  employeeId: selectedId,
                  prompt: promptController.text,
                ),
              ),
              child: const Text('确认提交'),
            ),
          ],
        ),
      ),
    ).whenComplete(promptController.dispose);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
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
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton.icon(
                  onPressed: onAnalyze,
                  icon: const Icon(Icons.manage_search_outlined),
                  label: const Text('分析输出'),
                ),
                OutlinedButton.icon(
                  onPressed: () => _sendToDigitalEmployee(context, ref),
                  icon: const Icon(Icons.smart_toy_outlined),
                  label: const Text('交给数字员工'),
                ),
              ],
            ),
            const SizedBox(height: 12),
            analysis.when(
              data: (value) => value == null
                  ? const SizedBox.shrink()
                  : _SSHAnalysisResult(
                      value: value,
                      onUseCommand: onUseCommand,
                      onCopyCommand: (command) =>
                          _copyCommandDraft(context, command),
                    ),
              error: (error, _) => Text('分析失败：$error'),
              loading: () => const LinearProgressIndicator(),
            ),
          ],
        ),
      ),
    );
  }
}

class _DigitalEmployeeOutputRequest {
  final String employeeId;
  final String prompt;

  const _DigitalEmployeeOutputRequest({
    required this.employeeId,
    required this.prompt,
  });
}

class MobileSSHOutputSubmissionSummary {
  final int lineCount;
  final int charCount;
  final String preview;

  const MobileSSHOutputSubmissionSummary({
    required this.lineCount,
    required this.charCount,
    required this.preview,
  });
}

MobileSSHOutputSubmissionSummary mobileSSHOutputSubmissionSummary(
  String output,
) {
  final text = output.trim();
  final lineCount =
      text.isEmpty ? 0 : RegExp(r'\r\n|\r|\n').allMatches(text).length + 1;
  final preview = text.length > 420 ? '${text.substring(0, 420)}...' : text;
  return MobileSSHOutputSubmissionSummary(
    lineCount: lineCount,
    charCount: text.length,
    preview: preview,
  );
}

Map<String, String> mobileSSHOutputTaskContext(
  String output, {
  ServerProfile? profile,
}) {
  final summary = mobileSSHOutputSubmissionSummary(output);
  return mobileSSHOutputTaskContextForSummary(summary, profile: profile);
}

Map<String, String> mobileSSHOutputTaskContextForSummary(
  MobileSSHOutputSubmissionSummary summary, {
  ServerProfile? profile,
}) {
  String? sanitizedMetadata(String? value) {
    final text = value?.trim() ?? '';
    if (text.isEmpty) return null;
    return redactMobileSensitiveText(text);
  }

  final serverTag = sanitizedMetadata(profile?.tag);
  final serverNote = sanitizedMetadata(profile?.note);
  final serverName = sanitizedMetadata(profile?.name);
  final serverHost = sanitizedMetadata(profile?.host);
  final serverUsername = sanitizedMetadata(profile?.username);
  return {
    'source': 'maclaw_mobile',
    'handoff': 'ssh_output',
    'task_surface': 'servers',
    'line_count': summary.lineCount.toString(),
    'char_count': summary.charCount.toString(),
    if (profile != null) ...{
      'server_profile_id': profile.id,
      if (serverName != null) 'server_name': serverName,
      if (serverHost != null) 'server_host': serverHost,
      'server_port': profile.port.toString(),
      if (serverUsername != null) 'server_username': serverUsername,
      'server_auth_mode': profile.authMode,
      if (serverTag != null) 'server_tag': serverTag,
      if (serverNote != null) 'server_note': serverNote,
    },
  };
}

String digitalEmployeeOutputPrompt(String output) {
  final text = output.trim();
  return '\u8bf7\u5206\u6790\u4e0b\u9762\u8fd9\u6bb5 SSH '
      '\u7ec8\u7aef\u8f93\u51fa\u6216\u670d\u52a1\u5668\u65e5\u5fd7\uff0c\u8bf4\u660e\u5f02\u5e38\u3001\u5f71\u54cd\u8303\u56f4\u3001\u6392\u67e5\u4f9d\u636e\u548c\u4e0b\u4e00\u6b65\u5efa\u8bae\u3002'
      '\u5982\u679c\u9700\u8981\u547d\u4ee4\uff0c\u53ea\u7ed9\u547d\u4ee4\u8349\u6848\u548c\u98ce\u9669\u8bf4\u660e\uff0c\u4e0d\u8981\u81ea\u52a8\u6267\u884c\u9ad8\u98ce\u9669\u64cd\u4f5c\u3002\n\n'
      '```text\n$text\n```';
}

class _SSHAnalysisResult extends ConsumerWidget {
  final MobileSSHAnalysis value;
  final ValueChanged<String> onUseCommand;
  final ValueChanged<String> onCopyCommand;

  const _SSHAnalysisResult({
    required this.value,
    required this.onUseCommand,
    required this.onCopyCommand,
  });

  Future<void> _saveCommandDraft(
    BuildContext context,
    WidgetRef ref,
    String command,
  ) async {
    if (!await confirmMobileHighRiskCommand(
      context,
      command,
      action: 'save',
    )) {
      return;
    }
    if (!context.mounted) return;
    await ref.read(serverCommandsProvider.notifier).record(
          command,
          favorite: true,
        );
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('命令草稿已保存为常用命令')),
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final command = value.commandDraft.trim();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(value.summary),
        const SizedBox(height: 6),
        Text(value.recommendation),
        if (command.isNotEmpty) ...[
          const SizedBox(height: 12),
          Text('命令草稿', style: Theme.of(context).textTheme.labelLarge),
          const SizedBox(height: 6),
          SelectableText(command),
          const SizedBox(height: 6),
          Text(
            'AI 只提供命令草案，不会自动执行；请先放入风险预检或复制后手动确认。',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              OutlinedButton.icon(
                onPressed: () => onUseCommand(command),
                icon: const Icon(Icons.rule_outlined),
                label: const Text('放入风险预检'),
              ),
              OutlinedButton.icon(
                onPressed: () => _saveCommandDraft(context, ref, command),
                icon: const Icon(Icons.star_border),
                label: const Text('保存常用'),
              ),
              IconButton.outlined(
                tooltip: '复制命令草稿',
                onPressed: () => onCopyCommand(command),
                icon: const Icon(Icons.content_copy_outlined),
              ),
            ],
          ),
        ],
      ],
    );
  }
}
