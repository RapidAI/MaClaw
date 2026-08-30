import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../shared/surface.dart';
import '../auth/session_controller.dart';
import 'account_agent_status_card.dart';

/// Manage remote MCP servers for the Hub mobile full agent.
class AccountMcpScreen extends ConsumerStatefulWidget {
  const AccountMcpScreen({super.key});

  @override
  ConsumerState<AccountMcpScreen> createState() => _AccountMcpScreenState();
}

class _AccountMcpScreenState extends ConsumerState<AccountMcpScreen> {
  List<MobileMcpServer> _servers = const [];
  var _loaded = false;
  var _saving = false;
  var _probing = false;
  MobileAgentMcpHealth? _health;
  String? _error;

  @override
  Widget build(BuildContext context) {
    final asyncConfig = ref.watch(accountMcpConfigProvider);
    return ScreenScaffold(
      title: 'Agent MCP',
      subtitle: '配置官方助手可用的远程 MCP（stdio 本地 MCP 在共享 Hub 上禁用）。',
      trailing: IconButton.filledTonal(
        tooltip: '刷新',
        onPressed: _saving
            ? null
            : () {
                setState(() {
                  _loaded = false;
                  _error = null;
                  _health = null;
                });
                ref.invalidate(accountMcpConfigProvider);
                ref.invalidate(accountMcpHealthProvider);
              },
        icon: const Icon(Icons.refresh),
      ),
      children: [
        asyncConfig.when(
          data: (config) {
            if (!_loaded) {
              WidgetsBinding.instance.addPostFrameCallback((_) {
                if (!mounted || _loaded) return;
                setState(() {
                  _servers = List<MobileMcpServer>.from(config.servers);
                  _loaded = true;
                });
              });
            }
            final healthById = {
              for (final s in _health?.servers ?? const <MobileMcpServerHealth>[])
                s.id: s,
            };
            return Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                StatusBanner(
                  tone: StatusTone.info,
                  icon: Icons.info_outline,
                  message: config.localMcpAllowed
                      ? '当前 Hub 允许本地 MCP。'
                      : '共享 Hub 仅支持远程 HTTP(S) MCP；密钥不会回传到手机列表。',
                ),
                const SizedBox(height: 12),
                if (_health != null) ...[
                  StatusBanner(
                    tone: _health!.healthyCount > 0
                        ? StatusTone.success
                        : StatusTone.warning,
                    icon: Icons.monitor_heart_outlined,
                    message:
                        '健康 ${_health!.healthyCount}/${_health!.serverCount} · 可用工具 ${_health!.availableTools}'
                        '${_health!.probedAt.isEmpty ? '' : ' · ${_health!.probedAt}'}',
                  ),
                  const SizedBox(height: 12),
                ],
                if (_error != null) ...[
                  StatusBanner(
                    tone: StatusTone.danger,
                    icon: Icons.error_outline,
                    message: _error!,
                  ),
                  const SizedBox(height: 12),
                ],
                for (var i = 0; i < _servers.length; i++) ...[
                  _McpServerCard(
                    server: _servers[i],
                    health: healthById[_servers[i].id],
                    onChanged: (next) {
                      setState(() {
                        _servers = [
                          for (var j = 0; j < _servers.length; j++)
                            if (j == i) next else _servers[j],
                        ];
                      });
                    },
                    onRemove: () {
                      setState(() {
                        _servers = [
                          for (var j = 0; j < _servers.length; j++)
                            if (j != i) _servers[j],
                        ];
                      });
                    },
                  ),
                  const SizedBox(height: 12),
                ],
                OutlinedButton.icon(
                  onPressed: _saving
                      ? null
                      : () {
                          setState(() {
                            _servers = [
                              ..._servers,
                              MobileMcpServer(
                                id: 'mcp-${DateTime.now().millisecondsSinceEpoch}',
                                name: '新 MCP',
                                endpointUrl: 'https://',
                                authType: 'none',
                              ),
                            ];
                          });
                        },
                  icon: const Icon(Icons.add),
                  label: const Text('添加远程 MCP'),
                ),
                const SizedBox(height: 12),
                OutlinedButton.icon(
                  onPressed: (_saving || _probing) ? null : _probeHealth,
                  icon: _probing
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.monitor_heart_outlined),
                  label: Text(_probing ? '探测中…' : '探测健康'),
                ),
                const SizedBox(height: 12),
                FilledButton.icon(
                  onPressed: _saving ? null : () => _save(context),
                  icon: _saving
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.save_outlined),
                  label: Text(_saving ? '保存中…' : '保存到 Hub'),
                ),
              ],
            );
          },
          loading: () => const LoadingCard(label: '加载 MCP 配置…'),
          error: (error, _) => Card(
            child: ListTile(
              leading: const Icon(Icons.error_outline),
              title: const Text('无法加载 MCP 配置'),
              subtitle: Text('$error'),
            ),
          ),
        ),
      ],
    );
  }

  Future<void> _probeHealth() async {
    setState(() {
      _probing = true;
      _error = null;
    });
    try {
      await ref.read(accountMcpHealthProvider.notifier).probe();
      final health = ref.read(accountMcpHealthProvider).valueOrNull;
      if (!mounted) return;
      setState(() {
        _health = health;
        _probing = false;
      });
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _probing = false;
        _error = '健康探测失败：$error';
      });
    }
  }

  Future<void> _save(BuildContext context) async {
    final client = ref.read(apiClientProvider);
    if (client == null) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final cleaned = <MobileMcpServer>[];
      for (final s in _servers) {
        final id = s.id.trim();
        final url = s.endpointUrl.trim();
        if (id.isEmpty || url.isEmpty || url == 'https://') {
          throw StateError('请填写完整的 MCP id 与 endpoint_url');
        }
        if (!url.startsWith('http://') && !url.startsWith('https://')) {
          throw StateError('endpoint 必须以 http:// 或 https:// 开头');
        }
        cleaned.add(
          s.copyWith(
            id: id,
            name: s.name.trim().isEmpty ? id : s.name.trim(),
            endpointUrl: url,
            authType: s.authType.trim().isEmpty ? 'none' : s.authType.trim(),
          ),
        );
      }
      final saved = await client.putAgentMcpConfig(cleaned);
      if (!mounted) return;
      final messenger = ScaffoldMessenger.of(context);
      setState(() {
        _servers = List<MobileMcpServer>.from(saved.servers);
        _loaded = true;
        _saving = false;
      });
      ref.invalidate(accountMcpConfigProvider);
      messenger.showSnackBar(
        const SnackBar(content: Text('MCP 配置已保存到 Hub')),
      );
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = '$error';
      });
    }
  }
}

class _McpServerCard extends StatelessWidget {
  final MobileMcpServer server;
  final MobileMcpServerHealth? health;
  final ValueChanged<MobileMcpServer> onChanged;
  final VoidCallback onRemove;

  const _McpServerCard({
    required this.server,
    required this.onChanged,
    required this.onRemove,
    this.health,
  });

  @override
  Widget build(BuildContext context) {
    final healthLabel = health == null
        ? null
        : '${health!.healthStatus} · 工具 ${health!.toolCount}';
    final healthColor = health == null
        ? null
        : (health!.isHealthy
            ? Theme.of(context).colorScheme.primary
            : Theme.of(context).colorScheme.error);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        server.name.isEmpty ? server.id : server.name,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      if (healthLabel != null)
                        Text(
                          healthLabel,
                          style: Theme.of(context).textTheme.bodySmall?.copyWith(
                                color: healthColor,
                              ),
                        ),
                    ],
                  ),
                ),
                IconButton(
                  tooltip: '删除',
                  onPressed: onRemove,
                  icon: const Icon(Icons.delete_outline),
                ),
              ],
            ),
            TextFormField(
              initialValue: server.id,
              decoration: const InputDecoration(
                labelText: 'ID',
                border: OutlineInputBorder(),
              ),
              onChanged: (v) => onChanged(server.copyWith(id: v)),
            ),
            const SizedBox(height: 8),
            TextFormField(
              initialValue: server.name,
              decoration: const InputDecoration(
                labelText: '名称',
                border: OutlineInputBorder(),
              ),
              onChanged: (v) => onChanged(server.copyWith(name: v)),
            ),
            const SizedBox(height: 8),
            TextFormField(
              initialValue: server.endpointUrl,
              decoration: const InputDecoration(
                labelText: 'MCP 地址',
                border: OutlineInputBorder(),
              ),
              onChanged: (v) => onChanged(server.copyWith(endpointUrl: v)),
            ),
            const SizedBox(height: 8),
            DropdownButtonFormField<String>(
              // Controlled field: parent rebuilds on change.
              // ignore: deprecated_member_use
              value: switch (server.authType) {
                'api_key' || 'bearer' || 'none' => server.authType,
                _ => 'none',
              },
              decoration: const InputDecoration(
                labelText: '认证类型',
                border: OutlineInputBorder(),
              ),
              items: const [
                DropdownMenuItem(value: 'none', child: Text('none')),
                DropdownMenuItem(value: 'bearer', child: Text('bearer')),
                DropdownMenuItem(value: 'api_key', child: Text('api_key')),
              ],
              onChanged: (v) {
                if (v == null) return;
                onChanged(server.copyWith(authType: v));
              },
            ),
            const SizedBox(height: 8),
            TextFormField(
              decoration: InputDecoration(
                labelText: server.hasAuthSecret
                    ? 'Auth Secret（已配置，留空保持原值）'
                    : 'Auth Secret（可选）',
                border: const OutlineInputBorder(),
              ),
              obscureText: true,
              onChanged: (v) => onChanged(server.copyWith(authSecret: v)),
            ),
          ],
        ),
      ),
    );
  }
}
