import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:xterm/xterm.dart';

import '../../core/api/api_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../core/security/mobile_redaction.dart';
import '../../shared/surface.dart';
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
  final _backendSessionKey = GlobalKey<_BackendSSHSessionCardState>();
  ServerProfile? _analysisProfile;
  String? _analysisBackendSessionId;
  var _settingLogFromBackendSession = false;

  @override
  void initState() {
    super.initState();
    _logController.addListener(() {
      if (!_settingLogFromBackendSession) {
        _analysisProfile = null;
        _analysisBackendSessionId = null;
      }
    });
  }

  @override
  void dispose() {
    _commandController.dispose();
    _logController.dispose();
    super.dispose();
  }

  Future<void> _refreshServerProfiles() async {
    try {
      final profiles =
          await ref.read(serverProfilesProvider.notifier).refreshFromHub();
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('已同步 ${profiles.length} 个后台服务器档案')),
      );
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('服务器档案同步失败：$error')),
      );
    }
  }

  Future<void> _clearCachedServer(ServerProfile profile) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('清除本机缓存？'),
        content: Text(
          '将从手机本机缓存中移除 ${profile.name} 的服务器档案。真实 SSH 配置和凭据仍由 MaClaw GUI/agent 管理，可再次从 Hub 同步。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('清除'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await ref
        .read(serverProfilesProvider.notifier)
        .clearCachedProfile(profile.id);
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('已清除 ${profile.name} 的本机缓存')),
    );
  }

  Future<bool> _sendCommandToBackendSession(String command) async {
    return _backendSessionKey.currentState?.sendCommand(command) ?? false;
  }

  Future<bool> _startBackendTaskFromCommand(String command) async {
    return _backendSessionKey.currentState?.startBackgroundTask(command) ??
        false;
  }

  Future<void> _analyzeManualLog() async {
    final output = _logController.text;
    final confirmed = await confirmMobileSSHLogAnalysis(
      context,
      output,
      source: 'manual',
    );
    if (!confirmed || !mounted) return;
    await ref.read(sshAnalysisProvider.notifier).analyze(
          redactMobileSensitiveText(output),
          backendSessionId: _analysisBackendSessionId,
        );
  }

  @override
  Widget build(BuildContext context) {
    final risk = classifyCommandRisk(_commandController.text);
    final analysis = ref.watch(sshAnalysisProvider);
    final profiles = ref.watch(serverProfilesProvider);
    return ScreenScaffold(
      title: '应急服务器',
      subtitle: '通过 Hub 让 MaClaw GUI/agent 接管后台 SSH 会话，AI 只解释日志和生成命令草案。',
      trailing: IconButton.filledTonal(
        tooltip: '同步服务器档案',
        onPressed: _refreshServerProfiles,
        icon: const Icon(Icons.sync),
      ),
      children: [
        _ServerProfileCard(
          profiles: profiles,
          onRefresh: _refreshServerProfiles,
          onClearCache: _clearCachedServer,
        ),
        const SizedBox(height: 12),
        _BackendSSHSessionCard(
          key: _backendSessionKey,
          profiles: profiles,
          onAnalyzeOutput: (output) {
            _settingLogFromBackendSession = true;
            _logController.text = output;
            _analysisProfile = _backendSessionKey.currentState?.activeProfile;
            _analysisBackendSessionId =
                _backendSessionKey.currentState?.activeBackendSessionId;
            _settingLogFromBackendSession = false;
            ref.read(sshAnalysisProvider.notifier).analyze(
                  redactMobileSensitiveText(output),
                  backendSessionId: _analysisBackendSessionId,
                );
          },
        ),
        const SizedBox(height: 12),
        _CommandRiskCard(
          controller: _commandController,
          risk: risk,
          onChanged: () => setState(() {}),
          onSendCommand: _sendCommandToBackendSession,
          onStartBackgroundTask: _startBackendTaskFromCommand,
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
          backendSessionId: _analysisBackendSessionId,
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
  final AsyncValue<List<ServerProfile>> profiles;
  final VoidCallback onRefresh;
  final Future<void> Function(ServerProfile profile) onClearCache;

  const _ServerProfileCard({
    required this.profiles,
    required this.onRefresh,
    required this.onClearCache,
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
                Text(
                  '后台服务器档案',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ],
            ),
            const SizedBox(height: 12),
            const Text(
              '服务器档案来自官方 Hub 同步的 MaClaw GUI/agent 授权配置。'
              '手机只发起后台会话、发送确认后的输入并查看输出，不录入或直连 SSH 凭据。',
            ),
            const SizedBox(height: 12),
            FilledButton.icon(
              onPressed: onRefresh,
              icon: const Icon(Icons.sync),
              label: const Text('同步服务器档案'),
            ),
            profiles.when(
              data: (items) => items.isEmpty
                  ? const Padding(
                      padding: EdgeInsets.only(top: 12),
                      child: Text(
                        '暂无可用服务器档案。请在 MaClaw GUI 配置 SSH 主机并保持桌面/agent 在线。',
                      ),
                    )
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
                              tooltip: '清除本机缓存',
                              onPressed: () => onClearCache(server),
                              icon:
                                  const Icon(Icons.cleaning_services_outlined),
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

class _BackendSSHSessionCard extends ConsumerStatefulWidget {
  final AsyncValue<List<ServerProfile>> profiles;
  final ValueChanged<String> onAnalyzeOutput;

  const _BackendSSHSessionCard({
    super.key,
    required this.profiles,
    required this.onAnalyzeOutput,
  });

  @override
  ConsumerState<_BackendSSHSessionCard> createState() =>
      _BackendSSHSessionCardState();
}

enum _MobileSSHConnectionState { disconnected, connecting, connected }

@visibleForTesting
final mobileBackendSshInitialOutputProvider = Provider<String>((ref) => '');

typedef MobileClipboardWriter = Future<void> Function(String text);

@visibleForTesting
final mobileClipboardWriterProvider = Provider<MobileClipboardWriter>(
  (ref) => (text) => Clipboard.setData(ClipboardData(text: text)),
);

String? backendSshCommandPayload(String command) {
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
    '\u8be5\u547d\u4ee4\u53ef\u80fd\u91cd\u542f\u670d\u52a1\u3001\u5220\u9664\u6570\u636e\u6216\u5f71\u54cd\u7cfb\u7edf\u53ef\u7528\u6027\u3002\u53d1\u9001\u540e\u4f1a\u8fdb\u5165\u5f53\u524d\u540e\u53f0 SSH \u4f1a\u8bdd\uff0c\u8bf7\u786e\u8ba4\u4f60\u7406\u89e3\u98ce\u9669\u3002';
const _confirmSaveHighRiskBody =
    '\u8be5\u547d\u4ee4\u53ef\u80fd\u91cd\u542f\u670d\u52a1\u3001\u5220\u9664\u6570\u636e\u6216\u5f71\u54cd\u7cfb\u7edf\u53ef\u7528\u6027\u3002\u4fdd\u5b58\u540e\u4ecd\u9700\u624b\u52a8\u590d\u5236/\u6267\u884c\uff0c\u8bf7\u786e\u8ba4\u4f60\u7406\u89e3\u98ce\u9669\u3002';
const _cancelLabel = '\u53d6\u6d88';
const _confirmSendLabel = '\u786e\u8ba4\u53d1\u9001';
const _confirmSaveLabel = '\u786e\u8ba4\u4fdd\u5b58';
const _sendBackendSessionOutputTitle =
    '\u53d1\u9001\u540e\u53f0\u4f1a\u8bdd\u8f93\u51fa\u7ed9 AI\uff1f';
const _sendRecentBackendSessionOutputBody =
    '\u5c06\u628a\u6700\u8fd1\u540e\u53f0 SSH \u4f1a\u8bdd\u8f93\u51fa\u53d1\u9001\u5230 MaClaw \u5b98\u65b9\u670d\u52a1\u8fdb\u884c\u5206\u6790\u3002';
const _sendPastedBackendSessionOutputBody =
    '\u5c06\u628a\u5f53\u524d\u7c98\u8d34\u7684\u540e\u53f0 SSH \u4f1a\u8bdd\u8f93\u51fa\u6216\u670d\u52a1\u5668\u65e5\u5fd7\u53d1\u9001\u5230 MaClaw \u5b98\u65b9\u670d\u52a1\u8fdb\u884c\u5206\u6790\u3002';
const _backendSessionOutputSummaryPrefix = '\u5171\u7ea6';
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
  final automatic = source == 'backend_session';
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text(_sendBackendSessionOutputTitle),
      content: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              automatic
                  ? _sendRecentBackendSessionOutputBody
                  : _sendPastedBackendSessionOutputBody,
            ),
            const SizedBox(height: 8),
            Text(
              '$_backendSessionOutputSummaryPrefix $lineCount $_lineCountUnit'
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

class _BackendSSHSessionCardState
    extends ConsumerState<_BackendSSHSessionCard> {
  static const _maxCapturedOutputChars = 12000;

  final _backendSessionTerminal = Terminal(maxLines: 1000);
  final _capturedOutput = StringBuffer();
  final _fileLocalPathController = TextEditingController();
  final _fileRemotePathController = TextEditingController();
  String _fileOperationAction = 'stat';
  String? _selectedId;
  String? _lastBackendSessionId;
  String? _activeManagedBackendSessionId;
  int _lastRealtimeOutputSeq = 0;
  ServerProfile? _activeProfile;
  _MobileSSHConnectionState _connectionState =
      _MobileSSHConnectionState.disconnected;
  String? _lastError;

  bool get _connecting =>
      _connectionState == _MobileSSHConnectionState.connecting;
  bool get _connected =>
      _connectionState == _MobileSSHConnectionState.connected;

  ServerProfile? get activeProfile => _activeProfile;

  String? get activeBackendSessionId => _activeManagedBackendSessionId;

  @override
  void initState() {
    super.initState();
    _writeBackendSessionOutput(
      '选择服务器后，通过 Hub 请求 MaClaw GUI/agent 创建或附着后台 SSH 会话。\r\n',
      capture: false,
    );
    final initialOutput = ref.read(mobileBackendSshInitialOutputProvider);
    if (initialOutput.trim().isNotEmpty) {
      _writeBackendSessionOutput(initialOutput);
    }
  }

  @override
  void dispose() {
    _fileLocalPathController.dispose();
    _fileRemotePathController.dispose();
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
    _capturedOutput.clear();
    setState(() {
      _connectionState = _MobileSSHConnectionState.connecting;
      _activeProfile = selectedProfile;
      _selectedId = selectedProfile.id;
      _lastError = null;
    });
    try {
      final controller = ref.read(backendSshSessionsProvider.notifier);
      final session = preferredProfileId == null ||
              _lastBackendSessionId == null
          ? await controller.createSession(serverProfileId: selectedProfile.id)
          : await controller.reconnectSession(_lastBackendSessionId!);
      _lastBackendSessionId = session.sessionId;
      _activeManagedBackendSessionId = mobileBackendSessionHandoffId(session);
      _lastRealtimeOutputSeq = session.outputSeq;
      _writeBackendSessionOutput(
        '已创建/附着后台 SSH 会话 ${session.sessionId} · ${selectedProfile.name}\r\n',
      );
      if (session.recentOutput.trim().isNotEmpty) {
        _writeBackendSessionOutput(session.recentOutput);
      }
      if (mounted) {
        setState(() => _connectionState = _MobileSSHConnectionState.connected);
      }
    } catch (error) {
      _writeBackendSessionOutput('后台 SSH 会话创建失败：$error\r\n');
      if (mounted) {
        setState(() {
          _connectionState = _MobileSSHConnectionState.disconnected;
          _lastError = error.toString();
        });
      }
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: 'SSH 后台会话异常',
            body: '${selectedProfile.name} 后台会话创建失败',
            payload: mobileServerProfileNotificationPayload(
              selectedProfile.id,
            ),
          );
    }
  }

  Future<void> _attachExistingSession(
    MobileBackendSSHSession existing,
    List<ServerProfile> profiles,
  ) async {
    if (_connecting || _connected) return;
    ServerProfile? selected;
    for (final profile in profiles) {
      if (profile.id == existing.serverProfileId) {
        selected = profile;
        break;
      }
    }
    setState(() {
      _connectionState = _MobileSSHConnectionState.connecting;
      _activeProfile = selected;
      _selectedId = existing.serverProfileId;
      _lastError = null;
    });
    try {
      final session =
          await ref.read(backendSshSessionsProvider.notifier).attachSession(
                existing.sessionId,
              );
      _lastBackendSessionId = session.sessionId;
      _activeManagedBackendSessionId = mobileBackendSessionHandoffId(
        session,
        fallback: mobileBackendSessionHandoffId(existing),
      );
      _lastRealtimeOutputSeq = session.outputSeq;
      _writeBackendSessionOutput(
        '已附着后台 SSH 会话 ${session.sessionId}'
        '${selected == null ? '' : ' · ${selected.name}'}\r\n',
      );
      final output = session.recentOutput.trim().isNotEmpty
          ? session.recentOutput
          : existing.recentOutput;
      if (output.trim().isNotEmpty) {
        _writeBackendSessionOutput(output);
      }
      if (mounted) {
        setState(() {
          _connectionState = session.connected
              ? _MobileSSHConnectionState.connected
              : _MobileSSHConnectionState.connecting;
        });
      }
    } catch (error) {
      _writeBackendSessionOutput('后台 SSH 会话附着失败：$error\r\n');
      if (mounted) {
        setState(() {
          _connectionState = _MobileSSHConnectionState.disconnected;
          _lastError = error.toString();
        });
      }
    }
  }

  Future<void> _closeActiveConnection({required bool manual}) async {
    final wasConnected = _connected;
    final sessionId = _lastBackendSessionId;
    if (manual && wasConnected && sessionId != null) {
      try {
        await ref.read(backendSshSessionsProvider.notifier).closeSession(
              sessionId,
            );
        _writeBackendSessionOutput('已关闭后台 SSH 会话 $sessionId。\r\n');
      } catch (error) {
        _writeBackendSessionOutput('后台 SSH 会话关闭失败：$error\r\n');
        if (mounted) {
          setState(() => _lastError = error.toString());
        }
        return;
      }
    }
    if (mounted) {
      setState(() {
        _connectionState = _MobileSSHConnectionState.disconnected;
        _lastRealtimeOutputSeq = 0;
        _activeManagedBackendSessionId = null;
      });
    }
  }

  Future<void> _interruptActiveConnection() async {
    final sessionId = _lastBackendSessionId;
    if (!_connected || sessionId == null) return;
    try {
      final session =
          await ref.read(backendSshSessionsProvider.notifier).interruptSession(
                sessionId,
              );
      _activeManagedBackendSessionId = mobileBackendSessionHandoffId(
        session,
        fallback: _activeManagedBackendSessionId,
      );
      _writeBackendSessionOutput('已请求中断后台 SSH 会话 $sessionId。\r\n');
    } catch (error) {
      _writeBackendSessionOutput('后台 SSH 会话中断请求失败：$error\r\n');
      if (mounted) {
        setState(() => _lastError = error.toString());
      }
    }
  }

  String _statusLabel() {
    return switch (_connectionState) {
      _MobileSSHConnectionState.connecting => '后台处理中',
      _MobileSSHConnectionState.connected => 'GUI/agent 已接管',
      _MobileSSHConnectionState.disconnected => '未接管',
    };
  }

  String? _reconnectProfileId(List<ServerProfile> profiles) {
    return mobileSshReconnectProfileId(
      selectedId: _selectedId,
      activeProfileId: _activeProfile?.id,
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

  void _writeBackendSessionOutput(String data, {bool capture = true}) {
    _backendSessionTerminal.write(data);
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
    final output = _capturedOutput.toString().trim();
    if (output.isEmpty || _recentOutputHasEvidenceLine(output)) {
      return output;
    }
    final evidence = _backendSessionEvidenceLine();
    if (evidence == null) return output;
    return '$evidence\n$output';
  }

  String? _backendSessionEvidenceLine() {
    final sessionId = _lastBackendSessionId?.trim() ?? '';
    final backendSessionId = _activeManagedBackendSessionId?.trim() ?? '';
    if (sessionId.isEmpty ||
        backendSessionId.isEmpty ||
        _lastRealtimeOutputSeq <= 0) {
      return null;
    }
    return 'GUI/agent 后台会话证据：Hub session $sessionId · '
        'backend_session_id $backendSessionId · '
        'claimed_by GUI/agent worker · output_seq $_lastRealtimeOutputSeq';
  }

  Future<bool> sendCommand(String command) async {
    final payload = backendSshCommandPayload(command);
    final sessionId = _lastBackendSessionId;
    if (!_connected || payload == null || sessionId == null) return false;
    final result =
        await ref.read(backendSshSessionsProvider.notifier).sendInput(
              sessionId: sessionId,
              input: payload,
            );
    if (result.output.trim().isNotEmpty) {
      _writeBackendSessionOutput(result.output);
    } else {
      _writeBackendSessionOutput(
        '命令已投递到后台 SSH 会话 $sessionId，等待 GUI/agent 处理。\r\n',
      );
    }
    return true;
  }

  Future<bool> startBackgroundTask(String command) async {
    final text = command.trim();
    final sessionId = _lastBackendSessionId;
    if (!_connected || text.isEmpty || sessionId == null) return false;
    final task =
        await ref.read(backendSshTasksProvider.notifier).startBackgroundTask(
              sessionId: sessionId,
              command: text,
              tailLines: 80,
            );
    _writeBackendSessionOutput(
      '已请求 GUI/agent 后台任务 ${task.taskId}'
      '${task.status.trim().isEmpty ? '' : ' · ${task.status.trim()}'}\r\n',
    );
    if (task.logTail.trim().isNotEmpty) {
      _writeBackendSessionOutput(task.logTail);
    }
    return true;
  }

  Future<void> _refreshBackgroundTasks() async {
    final sessionId = _lastBackendSessionId;
    if (!_connected || sessionId == null) return;
    try {
      final tasks = await ref
          .read(backendSshTasksProvider.notifier)
          .refreshForSession(sessionId);
      _writeBackendSessionOutput(
        '已刷新 GUI/agent 后台任务 ${tasks.length} 个。\r\n',
      );
    } catch (error) {
      _writeBackendSessionOutput('后台任务刷新失败：$error\r\n');
      if (mounted) {
        setState(() => _lastError = error.toString());
      }
    }
  }

  Future<void> _waitBackgroundTask(MobileBackendSSHTask task) async {
    final sessionId = _lastBackendSessionId;
    final taskId = task.taskId.trim();
    if (!_connected || sessionId == null || taskId.isEmpty) return;
    try {
      final updated = await ref.read(backendSshTasksProvider.notifier).waitTask(
            sessionId: sessionId,
            taskId: taskId,
            timeoutSeconds: 30,
            tailLines: 120,
          );
      _writeBackendSessionOutput(
        '后台任务 ${updated.taskId} 等待结果：${updated.status}\r\n',
      );
      if (updated.logTail.trim().isNotEmpty) {
        _writeBackendSessionOutput(updated.logTail);
      }
    } catch (error) {
      _writeBackendSessionOutput('后台任务等待失败：$error\r\n');
      if (mounted) {
        setState(() => _lastError = error.toString());
      }
    }
  }

  Future<void> _killBackgroundTask(MobileBackendSSHTask task) async {
    final sessionId = _lastBackendSessionId;
    final taskId = task.taskId.trim();
    if (!_connected || sessionId == null || taskId.isEmpty) return;
    try {
      final updated = await ref.read(backendSshTasksProvider.notifier).killTask(
            sessionId: sessionId,
            taskId: taskId,
          );
      _writeBackendSessionOutput(
        '已请求终止后台任务 ${updated.taskId}：${updated.status}\r\n',
      );
    } catch (error) {
      _writeBackendSessionOutput('后台任务终止失败：$error\r\n');
      if (mounted) {
        setState(() => _lastError = error.toString());
      }
    }
  }

  Future<void> _requestFileOperation() async {
    final sessionId = _lastBackendSessionId;
    final action = _fileOperationAction.trim();
    final remotePath = _fileRemotePathController.text.trim();
    final localPath = _fileLocalPathController.text.trim();
    if (!_connected || sessionId == null || action.isEmpty) return;
    if (remotePath.isEmpty && localPath.isEmpty) {
      setState(() => _lastError = '请填写远端路径，或填写 GUI/agent 侧本地路径。');
      return;
    }
    try {
      final operation =
          await ref.read(backendSshTasksProvider.notifier).requestFileOperation(
                sessionId: sessionId,
                action: action,
                localPath: localPath,
                remotePath: remotePath,
              );
      final parts = [
        if (operation.operationId.trim().isNotEmpty)
          operation.operationId.trim(),
        if (operation.action.trim().isNotEmpty) operation.action.trim(),
        if (operation.status.trim().isNotEmpty) operation.status.trim(),
        if (operation.claimedBy.trim().isNotEmpty)
          '接管者 ${operation.claimedBy.trim()}',
      ];
      _writeBackendSessionOutput(
        '已请求 GUI/agent 文件操作 ${parts.join(' · ')}\r\n',
      );
      final detail = [
        if (operation.remotePath.trim().isNotEmpty)
          '远端 ${operation.remotePath.trim()}',
        if (operation.localPath.trim().isNotEmpty)
          'GUI/agent 侧本地 ${operation.localPath.trim()}',
        if (operation.bytesTransferred > 0) '字节 ${operation.bytesTransferred}',
        if (operation.downloadUrl.trim().isNotEmpty)
          '下载 ${operation.downloadUrl.trim()}',
        if (operation.message.trim().isNotEmpty) operation.message.trim(),
      ];
      if (detail.isNotEmpty) {
        _writeBackendSessionOutput('${detail.join(' · ')}\r\n');
      }
      if (mounted) {
        setState(() => _lastError = null);
      }
    } catch (error) {
      _writeBackendSessionOutput('后台文件操作请求失败：$error\r\n');
      if (mounted) {
        setState(() => _lastError = error.toString());
      }
    }
  }

  void _applyBackendSessionUpdate(MobileBackendSSHSession session) {
    if (!mounted || session.sessionId != _lastBackendSessionId) return;
    _activeManagedBackendSessionId = mobileBackendSessionHandoffId(
      session,
      fallback: _activeManagedBackendSessionId,
    );
    var changed = false;
    final chunk = session.outputChunk;
    if (chunk.isNotEmpty && session.outputSeq > _lastRealtimeOutputSeq) {
      _writeBackendSessionOutput(chunk);
      _lastRealtimeOutputSeq = session.outputSeq;
      changed = true;
    }
    final nextState = session.connected
        ? _MobileSSHConnectionState.connected
        : session.status == 'closed' || session.state == 'closed'
            ? _MobileSSHConnectionState.disconnected
            : session.status == 'failed'
                ? _MobileSSHConnectionState.disconnected
                : _connectionState;
    final nextError = session.status == 'failed' ? session.message : null;
    if (nextState != _connectionState || nextError != _lastError) {
      setState(() {
        _connectionState = nextState;
        _lastError = nextError;
      });
      return;
    }
    if (changed) {
      setState(() {});
    }
  }

  Future<void> _sendOutputToAI() async {
    final output = _recentOutputForAI();
    if (output.isEmpty) return;
    final confirmed = await confirmMobileSSHLogAnalysis(
      context,
      output,
      source: 'backend_session',
    );
    if (!confirmed || !mounted) return;
    widget.onAnalyzeOutput(output);
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('已发送最近后台会话输出给 AI 分析')),
    );
  }

  bool _recentOutputHasEvidenceLine(String output) {
    final normalized = output.toLowerCase();
    return (normalized.contains('gui/agent 后台会话证据') ||
            normalized.contains('gui/agent evidence line')) &&
        normalized.contains('hub session') &&
        normalized.contains('backend_session_id') &&
        normalized.contains('claimed_by') &&
        normalized.contains('output_seq');
  }

  Future<void> _copyRecentOutput() async {
    final output = _recentOutputForAI();
    if (output.isEmpty) return;
    if (!_recentOutputHasEvidenceLine(output)) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('等待 GUI/agent 证据行后再复制后台会话输出'),
        ),
      );
      return;
    }
    await ref.read(mobileClipboardWriterProvider)(
      redactMobileSensitiveText(output),
    );
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('后台会话输出已复制')),
    );
  }
  void _clearCapturedOutput() {
    _capturedOutput.clear();
    _backendSessionTerminal.write('\x1B[2J\x1B[H');
    _writeBackendSessionOutput('后台会话输出已清空。\r\n', capture: false);
    setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    ref.listen<AsyncValue<Map<String, MobileBackendSSHSession>>>(
      backendSshSessionsProvider,
      (previous, next) {
        final sessionId = _lastBackendSessionId;
        if (sessionId == null) return;
        final session = next.valueOrNull?[sessionId];
        if (session != null) {
          _applyBackendSessionUpdate(session);
        }
      },
    );
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: widget.profiles.when(
          data: (profiles) {
            final scheme = Theme.of(context).colorScheme;
            final statusColor = _statusColor(scheme);
            final reconnectId = _reconnectProfileId(profiles);
            final backendSessions = ref.watch(backendSshSessionsProvider);
            final backendTasks = ref.watch(backendSshTasksProvider);
            return Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Icon(Icons.terminal_outlined, color: scheme.primary),
                    const SizedBox(width: 8),
                    Text(
                      'GUI/agent 后台 SSH 会话',
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
                      onPressed: _connecting || _connected
                          ? null
                          : () => _connect(profiles),
                      icon: const Icon(Icons.power_settings_new),
                      label: Text(_connected ? 'GUI/agent 已接管' : '请求后台会话'),
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
                        label: const Text('请求重接管'),
                      ),
                    OutlinedButton.icon(
                      onPressed: _connected
                          ? () => _closeActiveConnection(manual: true)
                          : null,
                      icon: const Icon(Icons.link_off_outlined),
                      label: const Text('关闭会话'),
                    ),
                    OutlinedButton.icon(
                      onPressed: _connected ? _interruptActiveConnection : null,
                      icon: const Icon(Icons.stop_circle_outlined),
                      label: const Text('中断'),
                    ),
                    OutlinedButton.icon(
                      onPressed:
                          _recentOutputForAI().isEmpty ? null : _sendOutputToAI,
                      icon: const Icon(Icons.psychology_alt_outlined),
                      label: const Text('交给 AI 分析'),
                    ),
                    IconButton.outlined(
                      tooltip: '复制后台会话输出',
                      onPressed: _recentOutputForAI().isEmpty
                          ? null
                          : _copyRecentOutput,
                      icon: const Icon(Icons.content_copy_outlined),
                    ),
                    IconButton.outlined(
                      tooltip: '清空后台会话输出',
                      onPressed:
                          _capturedOutput.isEmpty ? null : _clearCapturedOutput,
                      icon: const Icon(Icons.cleaning_services_outlined),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                _BackendSessionList(
                  sessions: backendSessions,
                  profiles: profiles,
                  activeSessionId: _lastBackendSessionId,
                  disabled: _connecting || _connected,
                  onRefresh: () =>
                      ref.read(backendSshSessionsProvider.notifier).refresh(),
                  onAttach: (session) =>
                      _attachExistingSession(session, profiles),
                ),
                const SizedBox(height: 12),
                _BackendTaskList(
                  tasks: backendTasks,
                  activeSessionId: _lastBackendSessionId,
                  connected: _connected,
                  onRefresh: _refreshBackgroundTasks,
                  onWait: _waitBackgroundTask,
                  onKill: _killBackgroundTask,
                ),
                const SizedBox(height: 12),
                _BackendFileOperationCard(
                  action: _fileOperationAction,
                  localPathController: _fileLocalPathController,
                  remotePathController: _fileRemotePathController,
                  connected: _connected,
                  onActionChanged: (value) => setState(
                    () => _fileOperationAction = value ?? _fileOperationAction,
                  ),
                  onSubmit: _requestFileOperation,
                ),
                const SizedBox(height: 12),
                Text(
                  '手机不会直连服务器；会话由 MaClaw GUI/agent 使用已授权配置创建、接管和回传输出。',
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                ),
                const SizedBox(height: 12),
                SizedBox(
                  height: 280,
                  child: TerminalView(_backendSessionTerminal),
                ),
              ],
            );
          },
          error: (error, _) => Text('后台会话加载失败：$error'),
          loading: () => const LinearProgressIndicator(),
        ),
      ),
    );
  }
}

String? mobileBackendSessionHandoffId(
  MobileBackendSSHSession session, {
  String? fallback,
}) {
  final backendSessionId = session.backendSessionId.trim();
  if (backendSessionId.isNotEmpty) return backendSessionId;
  final fallbackId = fallback?.trim() ?? '';
  return fallbackId.isEmpty ? null : fallbackId;
}

class _BackendSessionList extends StatelessWidget {
  final AsyncValue<Map<String, MobileBackendSSHSession>> sessions;
  final List<ServerProfile> profiles;
  final String? activeSessionId;
  final bool disabled;
  final VoidCallback onRefresh;
  final ValueChanged<MobileBackendSSHSession> onAttach;

  const _BackendSessionList({
    required this.sessions,
    required this.profiles,
    required this.activeSessionId,
    required this.disabled,
    required this.onRefresh,
    required this.onAttach,
  });

  String _profileName(String profileId) {
    for (final profile in profiles) {
      if (profile.id == profileId) return profile.name;
    }
    return profileId.isEmpty ? '未知服务器' : profileId;
  }

  String _formatLastActivity(DateTime? value) {
    if (value == null) return '';
    final local = value.toLocal();
    String two(int n) => n.toString().padLeft(2, '0');
    return '${local.year}-${two(local.month)}-${two(local.day)} '
        '${two(local.hour)}:${two(local.minute)}';
  }

  String _sessionSubtitle(MobileBackendSSHSession session) {
    final createdAt = _formatLastActivity(session.createdAt);
    final updatedAt = _formatLastActivity(session.updatedAt);
    final lastActivity = _formatLastActivity(session.lastActivityAt);
    final parts = [
      _profileName(session.serverProfileId),
      if (session.status.trim().isNotEmpty) '状态 ${session.status.trim()}',
      if (session.state.trim().isNotEmpty) session.state.trim(),
      if (session.claimedBy.trim().isNotEmpty)
        '接管者 ${session.claimedBy.trim()}',
      if (session.backendSessionId.trim().isNotEmpty)
        '后端 ${session.backendSessionId.trim()}',
      if (createdAt.isNotEmpty) '创建 $createdAt',
      if (updatedAt.isNotEmpty) '更新 $updatedAt',
      if (lastActivity.isNotEmpty) '最后活动 $lastActivity',
      if (session.outputSeq > 0) '输出序号 ${session.outputSeq}',
      if (session.pendingInputCount > 0) '待处理输入 ${session.pendingInputCount}',
      if (session.message.trim().isNotEmpty) session.message.trim(),
    ];
    return parts.join(' · ');
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return DecoratedBox(
      decoration: BoxDecoration(
        border: Border.all(color: scheme.outlineVariant),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.storage_outlined, color: scheme.primary),
                const SizedBox(width: 8),
                Text(
                  '已有后台会话',
                  style: Theme.of(context).textTheme.titleSmall,
                ),
                const Spacer(),
                IconButton(
                  tooltip: '刷新后台会话',
                  onPressed: onRefresh,
                  icon: const Icon(Icons.refresh),
                ),
              ],
            ),
            sessions.when(
              data: (items) {
                final values = items.values.toList()
                  ..sort((a, b) {
                    final left = a.lastActivityAt ??
                        DateTime.fromMillisecondsSinceEpoch(0);
                    final right = b.lastActivityAt ??
                        DateTime.fromMillisecondsSinceEpoch(0);
                    return right.compareTo(left);
                  });
                if (values.isEmpty) {
                  return const Padding(
                    padding: EdgeInsets.only(top: 4),
                    child: Text('暂无可附着的后台 SSH 会话'),
                  );
                }
                return Column(
                  children: [
                    for (final session in values.take(3))
                      ListTile(
                        dense: true,
                        contentPadding: EdgeInsets.zero,
                        leading: Icon(
                          session.connected
                              ? Icons.link_outlined
                              : Icons.link_off_outlined,
                          color: session.connected
                              ? scheme.primary
                              : scheme.onSurfaceVariant,
                        ),
                        title: Text(
                          session.sessionId,
                          overflow: TextOverflow.ellipsis,
                        ),
                        subtitle: Text(
                          _sessionSubtitle(session),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                        ),
                        trailing: TextButton(
                          onPressed:
                              disabled || activeSessionId == session.sessionId
                                  ? null
                                  : () => onAttach(session),
                          child: Text(
                            activeSessionId == session.sessionId
                                ? '当前会话'
                                : '附着',
                          ),
                        ),
                      ),
                  ],
                );
              },
              error: (error, _) => Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text('后台会话刷新失败：$error'),
              ),
              loading: () => const Padding(
                padding: EdgeInsets.only(top: 8),
                child: LinearProgressIndicator(),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _BackendTaskList extends StatelessWidget {
  final AsyncValue<Map<String, List<MobileBackendSSHTask>>> tasks;
  final String? activeSessionId;
  final bool connected;
  final VoidCallback onRefresh;
  final ValueChanged<MobileBackendSSHTask> onWait;
  final ValueChanged<MobileBackendSSHTask> onKill;

  const _BackendTaskList({
    required this.tasks,
    required this.activeSessionId,
    required this.connected,
    required this.onRefresh,
    required this.onWait,
    required this.onKill,
  });

  String _taskSubtitle(MobileBackendSSHTask task) {
    final parts = [
      if (task.status.trim().isNotEmpty) '状态 ${task.status.trim()}',
      if (task.backendSessionId.trim().isNotEmpty)
        '后端 ${task.backendSessionId.trim()}',
      if (task.exitCode != null) '退出码 ${task.exitCode}',
      if (task.command.trim().isNotEmpty) task.command.trim(),
      if (task.logTail.trim().isNotEmpty) task.logTail.trim(),
    ];
    return parts.join(' · ');
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final sessionId = activeSessionId?.trim() ?? '';
    return DecoratedBox(
      decoration: BoxDecoration(
        border: Border.all(color: scheme.outlineVariant),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.pending_actions_outlined, color: scheme.primary),
                const SizedBox(width: 8),
                Text(
                  'GUI/agent 后台任务',
                  style: Theme.of(context).textTheme.titleSmall,
                ),
                const Spacer(),
                IconButton(
                  tooltip: '刷新后台任务',
                  onPressed:
                      connected && sessionId.isNotEmpty ? onRefresh : null,
                  icon: const Icon(Icons.refresh),
                ),
              ],
            ),
            if (sessionId.isEmpty)
              const Padding(
                padding: EdgeInsets.only(top: 4),
                child: Text('请先创建或附着后台 SSH 会话'),
              )
            else
              tasks.when(
                data: (items) {
                  final values =
                      items[sessionId] ?? const <MobileBackendSSHTask>[];
                  if (values.isEmpty) {
                    return const Padding(
                      padding: EdgeInsets.only(top: 4),
                      child: Text('暂无 GUI/agent 后台任务'),
                    );
                  }
                  return Column(
                    children: [
                      for (final task in values.take(4))
                        ListTile(
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                          leading: Icon(
                            task.running
                                ? Icons.play_circle_outline
                                : Icons.task_alt_outlined,
                            color: task.running
                                ? scheme.primary
                                : scheme.onSurfaceVariant,
                          ),
                          title: Text(
                            task.taskId.isEmpty ? '未知任务' : task.taskId,
                            overflow: TextOverflow.ellipsis,
                          ),
                          subtitle: Text(
                            _taskSubtitle(task),
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                          ),
                          trailing: Wrap(
                            spacing: 4,
                            children: [
                              TextButton(
                                onPressed:
                                    connected ? () => onWait(task) : null,
                                child: const Text('等待'),
                              ),
                              TextButton(
                                onPressed:
                                    connected ? () => onKill(task) : null,
                                child: const Text('终止'),
                              ),
                            ],
                          ),
                        ),
                    ],
                  );
                },
                error: (error, _) => Padding(
                  padding: const EdgeInsets.only(top: 4),
                  child: Text('后台任务刷新失败：$error'),
                ),
                loading: () => const Padding(
                  padding: EdgeInsets.only(top: 8),
                  child: LinearProgressIndicator(),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _BackendFileOperationCard extends StatelessWidget {
  final String action;
  final TextEditingController localPathController;
  final TextEditingController remotePathController;
  final bool connected;
  final ValueChanged<String?> onActionChanged;
  final VoidCallback onSubmit;

  const _BackendFileOperationCard({
    required this.action,
    required this.localPathController,
    required this.remotePathController,
    required this.connected,
    required this.onActionChanged,
    required this.onSubmit,
  });

  String _actionLabel(String value) {
    return switch (value) {
      'download' => '下载到 GUI/agent',
      'upload' => '从 GUI/agent 上传',
      'list' => '列目录',
      _ => '查看文件状态',
    };
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return DecoratedBox(
      decoration: BoxDecoration(
        border: Border.all(color: scheme.outlineVariant),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.folder_copy_outlined, color: scheme.primary),
                const SizedBox(width: 8),
                Text(
                  'GUI/agent 文件操作',
                  style: Theme.of(context).textTheme.titleSmall,
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              '手机只创建 Hub 文件操作控制记录；实际读写由 MaClaw GUI/agent 在后台会话侧处理。',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<String>(
              initialValue: action,
              items: [
                for (final value in const [
                  'stat',
                  'list',
                  'download',
                  'upload',
                ])
                  DropdownMenuItem(
                    value: value,
                    child: Text(_actionLabel(value)),
                  ),
              ],
              onChanged: connected ? onActionChanged : null,
              decoration: const InputDecoration(labelText: '操作'),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: remotePathController,
              enabled: connected,
              decoration: const InputDecoration(
                labelText: '远端路径',
                prefixIcon: Icon(Icons.dns_outlined),
              ),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: localPathController,
              enabled: connected,
              decoration: const InputDecoration(
                labelText: 'GUI/agent 侧本地路径（可选）',
                prefixIcon: Icon(Icons.computer_outlined),
              ),
            ),
            const SizedBox(height: 12),
            FilledButton.icon(
              onPressed: connected ? onSubmit : null,
              icon: const Icon(Icons.add_task_outlined),
              label: const Text('请求文件操作'),
            ),
          ],
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
  final Future<bool> Function(String command) onStartBackgroundTask;
  final ValueChanged<String> onUseCommand;

  const _CommandRiskCard({
    required this.controller,
    required this.risk,
    required this.onChanged,
    required this.onSendCommand,
    required this.onStartBackgroundTask,
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
              onStartBackgroundTask: onStartBackgroundTask,
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
  final Future<bool> Function(String command) onStartBackgroundTask;
  final ValueChanged<String> onUseCommand;

  const _CommandActionBar({
    required this.command,
    required this.onSendCommand,
    required this.onStartBackgroundTask,
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
        content: Text(sent ? '命令已投递到后台 SSH 会话' : '请先创建或附着后台 SSH 会话'),
      ),
    );
  }

  Future<void> _startBackgroundTask(
    BuildContext context,
    WidgetRef ref,
  ) async {
    if (!await confirmMobileHighRiskCommand(
      context,
      command,
      action: 'send',
    )) {
      return;
    }
    if (!context.mounted) return;
    final started = await onStartBackgroundTask(command);
    if (!context.mounted) return;
    if (started) {
      await ref.read(serverCommandsProvider.notifier).record(command);
    }
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          started ? '后台任务请求已提交给 GUI/agent' : '请先创建或附着后台 SSH 会话',
        ),
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
              label: const Text('投递到后台会话'),
            ),
            FilledButton.tonalIcon(
              onPressed: command.trim().isEmpty
                  ? null
                  : () => _startBackgroundTask(context, ref),
              icon: const Icon(Icons.pending_actions_outlined),
              label: const Text('作为后台任务运行'),
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
  final String? backendSessionId;
  final VoidCallback onAnalyze;
  final ValueChanged<String> onUseCommand;

  const _SSHAnalysisCard({
    required this.controller,
    required this.analysis,
    required this.serverProfile,
    required this.backendSessionId,
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
        const SnackBar(content: Text('请先粘贴后台会话输出或错误日志。')),
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
          context: mobileSSHOutputTaskContext(
            output,
            profile: serverProfile,
            backendSessionId: backendSessionId,
          ),
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
                  'AI 分析后台会话输出',
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
                labelText: '后台会话输出或错误日志',
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
  String? backendSessionId,
}) {
  final summary = mobileSSHOutputSubmissionSummary(output);
  return mobileSSHOutputTaskContextForSummary(
    summary,
    profile: profile,
    backendSessionId: backendSessionId,
  );
}

Map<String, String> mobileSSHOutputTaskContextForSummary(
  MobileSSHOutputSubmissionSummary summary, {
  ServerProfile? profile,
  String? backendSessionId,
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
  final backendSession = sanitizedMetadata(backendSessionId);
  return {
    'source': 'maclaw_mobile',
    'handoff': 'ssh_output',
    'task_surface': 'servers',
    'line_count': summary.lineCount.toString(),
    'char_count': summary.charCount.toString(),
    'manual_confirmation_required': 'true',
    'execution_boundary': 'draft_only_until_mobile_user_confirms',
    'manual_confirmation_scope': 'destructive_or_high_risk_server_operations',
    if (backendSession != null) 'backend_session_id': backendSession,
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
  return '\u8bf7\u5206\u6790\u4e0b\u9762\u8fd9\u6bb5 MaClaw GUI/agent '
      '\u540e\u53f0 SSH \u4f1a\u8bdd\u8f93\u51fa\u6216\u670d\u52a1\u5668\u65e5\u5fd7\uff0c\u8bf4\u660e\u5f02\u5e38\u3001\u5f71\u54cd\u8303\u56f4\u3001\u6392\u67e5\u4f9d\u636e\u548c\u4e0b\u4e00\u6b65\u5efa\u8bae\u3002'
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
    final backendSessionId = value.backendSessionId.trim();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(value.summary),
        const SizedBox(height: 6),
        Text(value.recommendation),
        if (backendSessionId.isNotEmpty) ...[
          const SizedBox(height: 8),
          SelectableText('后台会话：$backendSessionId'),
        ],
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
