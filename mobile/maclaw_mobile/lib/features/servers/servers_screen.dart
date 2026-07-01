import 'dart:convert';

import 'package:dartssh2/dartssh2.dart';
import 'package:flutter/material.dart';
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
  final _commandController = TextEditingController(text: 'journalctl -u nginx -n 100');
  final _logController = TextEditingController();
  final _nameController = TextEditingController(text: '生产服务器');
  final _hostController = TextEditingController();
  final _portController = TextEditingController(text: '22');
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();

  @override
  void dispose() {
    _commandController.dispose();
    _logController.dispose();
    _nameController.dispose();
    _hostController.dispose();
    _portController.dispose();
    _usernameController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  void _addServer() {
    final profile = ServerProfile(
      id: DateTime.now().microsecondsSinceEpoch.toString(),
      name: _nameController.text.trim().isEmpty
          ? _hostController.text.trim()
          : _nameController.text.trim(),
      host: _hostController.text.trim(),
      port: int.tryParse(_portController.text.trim()) ?? 22,
      username: _usernameController.text.trim(),
      authMode: 'password',
    );
    if (!profile.isValid) return;
    ref.read(serverProfilesProvider.notifier).addProfile(
          profile,
          password: _passwordController.text,
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
          hostController: _hostController,
          portController: _portController,
          usernameController: _usernameController,
          passwordController: _passwordController,
          profiles: profiles,
          onAdd: _addServer,
        ),
        const SizedBox(height: 12),
        _SSHTerminalCard(
          profiles: profiles,
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
                  : () => ref
                      .read(serverCommandsProvider.notifier)
                      .record(command, favorite: true),
              icon: const Icon(Icons.star_border),
              label: const Text('保存常用'),
            ),
            OutlinedButton.icon(
              onPressed: command.trim().isEmpty
                  ? null
                  : () =>
                      ref.read(serverCommandsProvider.notifier).record(command),
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
              onPressed: () => ref
                  .read(serverCommandsProvider.notifier)
                  .remove(item.id),
              icon: const Icon(Icons.delete_outline),
            ),
            onTap: () => onUseCommand(item.command),
          ),
      ],
    );
  }
}

class _ServerProfileCard extends StatelessWidget {
  final TextEditingController nameController;
  final TextEditingController hostController;
  final TextEditingController portController;
  final TextEditingController usernameController;
  final TextEditingController passwordController;
  final AsyncValue<List<ServerProfile>> profiles;
  final VoidCallback onAdd;

  const _ServerProfileCard({
    required this.nameController,
    required this.hostController,
    required this.portController,
    required this.usernameController,
    required this.passwordController,
    required this.profiles,
    required this.onAdd,
  });

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
            TextField(
              controller: hostController,
              decoration: const InputDecoration(
                labelText: 'Host',
                prefixIcon: Icon(Icons.cloud_outlined),
              ),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: passwordController,
              obscureText: true,
              decoration: const InputDecoration(
                labelText: '密码',
                prefixIcon: Icon(Icons.password_outlined),
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
                              '${server.username}@${server.host}:${server.port}',
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

  const _SSHTerminalCard({required this.profiles});

  @override
  ConsumerState<_SSHTerminalCard> createState() => _SSHTerminalCardState();
}

class _SSHTerminalCardState extends ConsumerState<_SSHTerminalCard> {
  final _terminal = Terminal(maxLines: 1000);
  SSHClient? _client;
  String? _selectedId;
  bool _connecting = false;

  @override
  void initState() {
    super.initState();
    _terminal.write('选择服务器后连接 SSH。\r\n');
  }

  @override
  void dispose() {
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
    setState(() => _connecting = true);
    try {
      final password =
          await ref.read(secureVaultProvider).readServerPassword(selected.id);
      final socket = await SSHSocket.connect(selected.host, selected.port);
      final client = SSHClient(
        socket,
        username: selected.username,
        onPasswordRequest: () => password ?? '',
      );
      final session = await client.shell(
        pty: SSHPtyConfig(
          width: _terminal.viewWidth,
          height: _terminal.viewHeight,
        ),
      );
      _client = client;
      _terminal.write('已连接 ${selected.name}\r\n');
      _terminal.onOutput = (data) {
        session.write(utf8.encode(data));
      };
      _terminal.onResize = (width, height, pixelWidth, pixelHeight) {
        session.resizeTerminal(width, height, pixelWidth, pixelHeight);
      };
      session.stdout
          .cast<List<int>>()
          .transform(const Utf8Decoder())
          .listen(_terminal.write);
      session.stderr
          .cast<List<int>>()
          .transform(const Utf8Decoder())
          .listen(_terminal.write);
    } catch (error) {
      _terminal.write('连接失败：$error\r\n');
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: 'SSH 连接异常',
            body: '${selected.name} 连接失败',
            payload: selected.id,
          );
    } finally {
      if (mounted) setState(() => _connecting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: widget.profiles.when(
          data: (profiles) => Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(
                    Icons.terminal_outlined,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                  const SizedBox(width: 8),
                  Text('手动 SSH 终端', style: Theme.of(context).textTheme.titleMedium),
                ],
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                value: _selectedId,
                items: [
                  for (final profile in profiles)
                    DropdownMenuItem(
                      value: profile.id,
                      child: Text(profile.name),
                    ),
                ],
                onChanged: (value) => setState(() => _selectedId = value),
                decoration: const InputDecoration(labelText: '服务器'),
              ),
              const SizedBox(height: 12),
              FilledButton.icon(
                onPressed: _connecting ? null : () => _connect(profiles),
                icon: const Icon(Icons.power_settings_new),
                label: Text(_connecting ? '连接中' : '连接 SSH'),
              ),
              const SizedBox(height: 12),
              SizedBox(
                height: 280,
                child: TerminalView(_terminal),
              ),
            ],
          ),
          error: (error, _) => Text('终端加载失败：$error'),
          loading: () => const LinearProgressIndicator(),
        ),
      ),
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
                Text('AI 分析终端输出', style: Theme.of(context).textTheme.titleMedium),
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
