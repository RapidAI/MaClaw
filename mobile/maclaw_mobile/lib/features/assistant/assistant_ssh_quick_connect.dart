import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import '../servers/server_profile.dart';
import '../servers/servers_controller.dart';

/// Minimal connect dialog: IP + username + password → Hub ready for AI ssh.
/// No profile management UI.
Future<MobileSSHQuickConnectResult?> showAssistantSSHQuickConnectDialog(
  BuildContext context,
  WidgetRef ref,
) async {
  final client = ref.read(apiClientProvider);
  if (client == null) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('请先登录官方服务')),
      );
    }
    return null;
  }

  final hostCtrl = TextEditingController();
  final userCtrl = TextEditingController(text: 'root');
  final passCtrl = TextEditingController();
  final portCtrl = TextEditingController(text: '22');
  var obscure = true;
  var saving = false;
  var showPort = false;

  final result = await showModalBottomSheet<MobileSSHQuickConnectResult>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (ctx) {
      return StatefulBuilder(
        builder: (ctx, setLocal) {
          final bottom = MediaQuery.viewInsetsOf(ctx).bottom;
          return Padding(
            padding: EdgeInsets.only(
              left: 20,
              right: 20,
              top: 8,
              bottom: bottom + 20,
            ),
            child: SingleChildScrollView(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    '连服务器',
                    style: Theme.of(ctx).textTheme.titleLarge?.copyWith(
                          fontWeight: FontWeight.w700,
                        ),
                  ),
                  const SizedBox(height: 6),
                  Text(
                    '只需填写 IP、用户名、密码。保存后直接让 AI 助手操作，不用再做档案管理。',
                    style: Theme.of(ctx).textTheme.bodyMedium?.copyWith(
                          color: Theme.of(ctx).colorScheme.onSurfaceVariant,
                        ),
                  ),
                  const SizedBox(height: 16),
                  TextField(
                    controller: hostCtrl,
                    keyboardType: TextInputType.url,
                    textInputAction: TextInputAction.next,
                    autofillHints: const [AutofillHints.url],
                    decoration: const InputDecoration(
                      labelText: '服务器 IP / 域名',
                      hintText: '例如 192.168.1.10',
                      prefixIcon: Icon(Icons.dns_outlined),
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: userCtrl,
                    textInputAction: TextInputAction.next,
                    autofillHints: const [AutofillHints.username],
                    decoration: const InputDecoration(
                      labelText: '用户名',
                      hintText: 'root',
                      prefixIcon: Icon(Icons.person_outline),
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: passCtrl,
                    obscureText: obscure,
                    textInputAction: TextInputAction.done,
                    autofillHints: const [AutofillHints.password],
                    onSubmitted: (_) {},
                    decoration: InputDecoration(
                      labelText: '密码',
                      prefixIcon: const Icon(Icons.lock_outline),
                      border: const OutlineInputBorder(),
                      suffixIcon: IconButton(
                        onPressed: () => setLocal(() => obscure = !obscure),
                        icon: Icon(
                          obscure
                              ? Icons.visibility_outlined
                              : Icons.visibility_off_outlined,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(height: 4),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton(
                      onPressed: saving
                          ? null
                          : () => setLocal(() => showPort = !showPort),
                      child: Text(showPort ? '隐藏端口' : '高级：改端口'),
                    ),
                  ),
                  if (showPort) ...[
                    TextField(
                      controller: portCtrl,
                      keyboardType: TextInputType.number,
                      decoration: const InputDecoration(
                        labelText: '端口',
                        hintText: '22',
                        prefixIcon: Icon(Icons.numbers),
                        border: OutlineInputBorder(),
                      ),
                    ),
                    const SizedBox(height: 12),
                  ],
                  FilledButton.icon(
                    onPressed: saving
                        ? null
                        : () async {
                            final host = hostCtrl.text.trim();
                            final user = userCtrl.text.trim();
                            final pass = passCtrl.text;
                            final port =
                                int.tryParse(portCtrl.text.trim()) ?? 22;
                            if (host.isEmpty || user.isEmpty || pass.isEmpty) {
                              ScaffoldMessenger.of(ctx).showSnackBar(
                                const SnackBar(
                                  content: Text('请填写 IP、用户名和密码'),
                                ),
                              );
                              return;
                            }
                            setLocal(() => saving = true);
                            try {
                              final connected = await client.quickConnectSSH(
                                host: host,
                                username: user,
                                password: pass,
                                port: port,
                              );
                              // Cache profile locally (no secret).
                              try {
                                final store = ref.read(mobileLocalStoreProvider);
                                final existing =
                                    await store.loadServerProfiles();
                                final profile = ServerProfile(
                                  id: connected.profileId,
                                  name: connected.label,
                                  host: connected.host,
                                  port: connected.port,
                                  username: connected.username,
                                  authMode: serverAuthModePassword,
                                  tag: 'quick',
                                  note: 'AI quick connect',
                                  sourceMachineId: 'mobile-quick',
                                );
                                final next = [
                                  profile,
                                  ...existing.where(
                                    (p) => p.id != profile.id,
                                  ),
                                ];
                                await store.saveServerProfiles(next);
                                ref.invalidate(serverProfilesProvider);
                              } on Object {
                                // Local cache is best-effort.
                              }
                              if (ctx.mounted) {
                                Navigator.of(ctx).pop(connected);
                              }
                            } on Object catch (e) {
                              setLocal(() => saving = false);
                              if (ctx.mounted) {
                                ScaffoldMessenger.of(ctx).showSnackBar(
                                  SnackBar(content: Text('连接失败：$e')),
                                );
                              }
                            }
                          },
                    icon: saving
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.link),
                    label: Text(saving ? '接入中…' : '接入并给 AI 使用'),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    '密码仅加密保存在 Hub，不会出现在对话记录里。',
                    style: Theme.of(ctx).textTheme.labelSmall?.copyWith(
                          color: Theme.of(ctx).colorScheme.onSurfaceVariant,
                        ),
                    textAlign: TextAlign.center,
                  ),
                ],
              ),
            ),
          );
        },
      );
    },
  );

  hostCtrl.dispose();
  userCtrl.dispose();
  passCtrl.dispose();
  portCtrl.dispose();
  return result;
}
