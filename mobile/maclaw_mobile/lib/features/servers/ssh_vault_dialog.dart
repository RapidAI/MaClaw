import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../auth/session_controller.dart';
import 'server_profile.dart';

/// Dialog to store an SSH secret on Hub for hub_exec (encrypted at rest).
Future<bool> showHubSSHVaultDialog(
  BuildContext context,
  WidgetRef ref,
  ServerProfile profile,
) async {
  final client = ref.read(apiClientProvider);
  if (client == null) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('请先登录官方服务。')),
      );
    }
    return false;
  }

  MobileSSHVaultStatus? existing;
  try {
    existing = await client.getSSHVaultStatus(profile.id);
  } on Object {
    existing = null;
  }
  if (!context.mounted) return false;

  final secretCtrl = TextEditingController();
  final passCtrl = TextEditingController();
  var authMode =
      (existing?.authMode.isNotEmpty == true)
          ? existing!.authMode
          : (profile.authMode == 'private_key' ||
                  profile.authMode == 'key'
              ? 'private_key'
              : 'password');
  var obscure = true;
  var saving = false;

  final result = await showDialog<bool>(
    context: context,
    builder: (ctx) {
      return StatefulBuilder(
        builder: (ctx, setLocal) {
          return AlertDialog(
            title: Text('Hub 凭据 · ${profile.name}'),
            content: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    existing?.hasSecret == true
                        ? '该档案已有 Hub 密钥。重新保存将覆盖。密钥仅加密存于 Hub，不会回传到手机。'
                        : '将密钥加密存入 Hub 后，手机可走 hub_exec（不依赖 PC 在线）。密钥不会回传到手机。',
                    style: Theme.of(ctx).textTheme.bodySmall,
                  ),
                  const SizedBox(height: 12),
                  DropdownButtonFormField<String>(
                    // ignore: deprecated_member_use
                    value: authMode,
                    decoration: const InputDecoration(
                      labelText: '认证方式',
                      border: OutlineInputBorder(),
                    ),
                    items: const [
                      DropdownMenuItem(
                        value: 'password',
                        child: Text('密码'),
                      ),
                      DropdownMenuItem(
                        value: 'private_key',
                        child: Text('私钥 PEM'),
                      ),
                    ],
                    onChanged: saving
                        ? null
                        : (v) {
                            if (v == null) return;
                            setLocal(() => authMode = v);
                          },
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: secretCtrl,
                    obscureText: authMode == 'password' ? obscure : false,
                    maxLines: authMode == 'private_key' ? 6 : 1,
                    minLines: authMode == 'private_key' ? 4 : 1,
                    decoration: InputDecoration(
                      labelText:
                          authMode == 'private_key' ? '私钥 PEM' : 'SSH 密码',
                      border: const OutlineInputBorder(),
                      suffixIcon: authMode == 'password'
                          ? IconButton(
                              tooltip: obscure ? '显示' : '隐藏',
                              onPressed: () =>
                                  setLocal(() => obscure = !obscure),
                              icon: Icon(
                                obscure
                                    ? Icons.visibility_outlined
                                    : Icons.visibility_off_outlined,
                              ),
                            )
                          : null,
                    ),
                  ),
                  if (authMode == 'private_key') ...[
                    const SizedBox(height: 12),
                    TextField(
                      controller: passCtrl,
                      obscureText: true,
                      decoration: const InputDecoration(
                        labelText: '私钥口令（可选）',
                        border: OutlineInputBorder(),
                      ),
                    ),
                  ],
                ],
              ),
            ),
            actions: [
              if (existing?.hasSecret == true)
                TextButton(
                  onPressed: saving
                      ? null
                      : () async {
                          setLocal(() => saving = true);
                          try {
                            await client.deleteSSHVaultSecret(profile.id);
                            if (ctx.mounted) Navigator.of(ctx).pop(true);
                          } on Object catch (e) {
                            setLocal(() => saving = false);
                            if (ctx.mounted) {
                              ScaffoldMessenger.of(ctx).showSnackBar(
                                SnackBar(content: Text('删除失败：$e')),
                              );
                            }
                          }
                        },
                  child: const Text('清除密钥'),
                ),
              TextButton(
                onPressed:
                    saving ? null : () => Navigator.of(ctx).pop(false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: saving
                    ? null
                    : () async {
                        final secret = secretCtrl.text.trim();
                        if (secret.isEmpty) {
                          ScaffoldMessenger.of(ctx).showSnackBar(
                            const SnackBar(content: Text('请输入密钥')),
                          );
                          return;
                        }
                        setLocal(() => saving = true);
                        try {
                          // Publish profile metadata to Hub so AI assistant
                          // can enable label-based ssh (vault alone is not enough).
                          try {
                            await client.upsertServerProfiles([profile]);
                          } on Object {
                            // Best-effort; vault store may still succeed.
                          }
                          await client.putSSHVaultSecret(
                            profileId: profile.id,
                            secret: secret,
                            authMode: authMode,
                            passphrase: passCtrl.text,
                          );
                          if (ctx.mounted) Navigator.of(ctx).pop(true);
                        } on Object catch (e) {
                          setLocal(() => saving = false);
                          if (ctx.mounted) {
                            ScaffoldMessenger.of(ctx).showSnackBar(
                              SnackBar(content: Text('保存失败：$e')),
                            );
                          }
                        }
                      },
                child: saving
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('加密保存到 Hub'),
              ),
            ],
          );
        },
      );
    },
  );

  secretCtrl.dispose();
  passCtrl.dispose();
  return result == true;
}
