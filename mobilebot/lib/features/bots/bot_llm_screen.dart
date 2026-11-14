import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'bots_controller.dart';

/// 模型设置 — 对应桌面 GUI 的 BotLLMSettingsDialog：`GET/PUT /api/v1/bots/llm`
/// 的配置对所有 Bot 生效。手机端提供「默认 / 自定义单一提供方」两种模式；
/// 桌面端的多角色细分（前台/后台/电脑操作/定位）在 Hub 侧按顶层字段展开。
class BotLLMScreen extends ConsumerStatefulWidget {
  const BotLLMScreen({super.key});

  @override
  ConsumerState<BotLLMScreen> createState() => _BotLLMScreenState();
}

class _BotLLMScreenState extends ConsumerState<BotLLMScreen> {
  final _nameCtrl = TextEditingController();
  final _urlCtrl = TextEditingController();
  final _keyCtrl = TextEditingController();
  final _modelCtrl = TextEditingController();

  String _mode = 'default';
  String _protocol = 'openai';
  String _wireApi = 'chat';
  bool _keySet = false;
  bool _loading = true;
  bool _saving = false;
  String _warning = '';
  String _error = '';
  Map<String, dynamic> _effective = const {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _nameCtrl.dispose();
    _urlCtrl.dispose();
    _keyCtrl.dispose();
    _modelCtrl.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final api = ref.read(botApiProvider);
    if (api == null) {
      setState(() => _loading = false);
      return;
    }
    try {
      final view = await api.getLLMSettings();
      if (!mounted) return;
      setState(() {
        _mode = (view['mode'] as String? ?? 'default') == 'custom'
            ? 'custom'
            : 'default';
        _nameCtrl.text = (view['name'] as String? ?? '').toString();
        _urlCtrl.text = (view['url'] as String? ?? '').toString();
        _modelCtrl.text = (view['model'] as String? ?? '').toString();
        _keySet = view['key_set'] == true;
        _warning = (view['warning'] as String? ?? '').toString();
        _effective = view['effective'] is Map
            ? Map<String, dynamic>.from(view['effective'] as Map)
            : const {};
        final fg = _effective['foreground'];
        if (fg is Map) {
          _protocol = (fg['protocol'] as String? ?? 'openai')
              .toString()
              .toLowerCase();
          _wireApi = (fg['wire_api'] as String? ?? 'chat')
              .toString()
              .toLowerCase();
        }
        _loading = false;
      });
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = failureMessage(error);
      });
    }
  }

  Future<void> _save() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      final body = <String, dynamic>{'mode': _mode};
      if (_mode == 'custom') {
        body.addAll({
          'name': _nameCtrl.text.trim(),
          'url': _urlCtrl.text.trim(),
          'model': _modelCtrl.text.trim(),
          'protocol': _protocol,
          'wire_api': _wireApi,
        });
        final key = _keyCtrl.text.trim();
        if (key.isNotEmpty) body['key'] = key;
        // Roles omitted: Hub expands the single provider to every role,
        // the same path an older desktop GUI takes.
      }
      await api.putLLMSettings(body);
      if (!mounted) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(const SnackBar(content: Text('已保存')));
      Navigator.of(context).pop();
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = failureMessage(error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('模型设置'),
        actions: [
          TextButton(
            onPressed: _loading || _saving ? null : _save,
            child: _saving
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2))
                : const Text('保存'),
          ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.all(16),
              children: [
                if (_warning.isNotEmpty)
                  Container(
                    margin: const EdgeInsets.only(bottom: 12),
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: theme.colorScheme.tertiaryContainer,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: Text(_warning,
                        style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onTertiaryContainer)),
                  ),
                if (_error.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: Text(_error,
                        style: TextStyle(color: theme.colorScheme.error)),
                  ),
                SegmentedButton<String>(
                  segments: const [
                    ButtonSegment(
                      value: 'default',
                      label: Text('默认'),
                      icon: Icon(Icons.auto_awesome),
                    ),
                    ButtonSegment(
                      value: 'custom',
                      label: Text('自定义'),
                      icon: Icon(Icons.tune),
                    ),
                  ],
                  selected: {_mode},
                  onSelectionChanged: (v) => setState(() => _mode = v.first),
                ),
                const SizedBox(height: 8),
                Text(
                  _mode == 'default'
                      ? '使用平台为账号配置的默认模型'
                      : '为你的所有 Bot 使用自己的模型服务',
                  style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant),
                ),
                if (_mode == 'custom') ...[
                  const SizedBox(height: 20),
                  TextField(
                    controller: _nameCtrl,
                    decoration: const InputDecoration(
                      labelText: '提供方名称',
                      hintText: '如 openai-main',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: _urlCtrl,
                    keyboardType: TextInputType.url,
                    decoration: const InputDecoration(
                      labelText: 'API 地址',
                      hintText: 'https://…/v1',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: _keyCtrl,
                    obscureText: true,
                    decoration: InputDecoration(
                      labelText: _keySet ? 'API Key（已设置，留空保持不变）' : 'API Key',
                      border: const OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: _modelCtrl,
                    decoration: const InputDecoration(
                      labelText: '模型',
                      hintText: '如 gpt-4o / claude-sonnet-4',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 12),
                  Row(
                    children: [
                      Expanded(
                        child: DropdownButtonFormField<String>(
                          initialValue: _protocol,
                          decoration: const InputDecoration(
                            labelText: '协议',
                            border: OutlineInputBorder(),
                          ),
                          items: const [
                            DropdownMenuItem(
                                value: 'openai', child: Text('OpenAI')),
                            DropdownMenuItem(
                                value: 'anthropic',
                                child: Text('Anthropic')),
                          ],
                          onChanged: (v) =>
                              setState(() => _protocol = v ?? 'openai'),
                        ),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: DropdownButtonFormField<String>(
                          initialValue: _wireApi,
                          decoration: const InputDecoration(
                            labelText: '接口形式',
                            border: OutlineInputBorder(),
                          ),
                          items: const [
                            DropdownMenuItem(
                                value: 'chat', child: Text('chat')),
                            DropdownMenuItem(
                                value: 'responses',
                                child: Text('responses')),
                            DropdownMenuItem(
                                value: 'responses-ws',
                                child: Text('responses-ws')),
                          ],
                          onChanged: (v) =>
                              setState(() => _wireApi = v ?? 'chat'),
                        ),
                      ),
                    ],
                  ),
                ],
                if (_effective.isNotEmpty) ...[
                  const SizedBox(height: 24),
                  Text('当前生效', style: theme.textTheme.titleSmall),
                  const SizedBox(height: 8),
                  for (final role in const [
                    ('foreground', '前台对话'),
                    ('background', '后台任务'),
                    ('computer_use', '电脑操作'),
                    ('grounder', '界面定位'),
                  ])
                    _EffectiveRoleRow(
                      label: role.$2,
                      role: _effective[role.$1],
                    ),
                ],
              ],
            ),
    );
  }
}

class _EffectiveRoleRow extends StatelessWidget {
  final String label;
  final Object? role;

  const _EffectiveRoleRow({required this.label, this.role});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    if (role is! Map) return const SizedBox.shrink();
    final map = Map<String, dynamic>.from(role as Map);
    final unused = map['unused'] == true;
    final official = map['official'] == true;
    final detail = unused
        ? '未启用'
        : official
            ? '官方模型'
            : [
                (map['name'] as String? ?? '').toString(),
                (map['model'] as String? ?? '').toString(),
              ].where((s) => s.isNotEmpty).join(' · ');
    return ListTile(
      dense: true,
      contentPadding: EdgeInsets.zero,
      title: Text(label, style: theme.textTheme.bodyMedium),
      trailing: Text(
        detail.isEmpty ? '—' : detail,
        style: theme.textTheme.bodySmall
            ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
      ),
    );
  }
}
