import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/mobile_credits.dart';
import '../../shared/surface.dart';
import '../auth/session_controller.dart';
import 'llm_qr_authorization_screen.dart';

class LlmSetupScreen extends ConsumerWidget {
  const LlmSetupScreen({super.key});

  Future<void> _refresh(BuildContext context, WidgetRef ref) async {
    try {
      await ref.read(sessionControllerProvider.notifier).refreshBootstrap();
      if (!context.mounted) return;
      final bootstrap =
          ref.read(sessionControllerProvider).valueOrNull?.bootstrap;
      if (isMobileLlmConfigured(bootstrap)) {
        context.go('/assistant');
        return;
      }
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('官方 LLM 尚未可用，请稍后重试或使用桌面二维码授权。')),
      );
    } catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('刷新官方服务失败：$error')),
      );
    }
  }

  Future<void> _openQr(BuildContext context) async {
    await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => const LlmQrAuthorizationScreen()),
    );
    if (!context.mounted) return;
    final bootstrap = ProviderScope.containerOf(context, listen: false)
        .read(sessionControllerProvider)
        .valueOrNull
        ?.bootstrap;
    if (isMobileLlmConfigured(bootstrap)) context.go('/assistant');
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final bootstrap =
        ref.watch(sessionControllerProvider).valueOrNull?.bootstrap;
    final llm = bootstrap?.llmAccess;
    return Scaffold(
      appBar: AppBar(
        title: const Text('配置 MaClaw LLM'),
        automaticallyImplyLeading: false,
      ),
      body: ScreenScaffold(
        title: '先连接 AI 服务',
        subtitle: '手机号登录已完成。配置成功后，MaClaw Mobile 会直接进入 AI 助手。',
        children: [
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(
                    Icons.account_balance_wallet_outlined,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'MaClaw 官方服务',
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        const SizedBox(height: 6),
                        Text(
                          '默认使用当前手机号账户绑定的官方 Credits，不需要在手机上填写 API Key 或服务器地址。',
                          style: Theme.of(context).textTheme.bodyMedium,
                        ),
                        const SizedBox(height: 8),
                        Text(
                          '当前状态：${llm?.status.isNotEmpty == true ? llm!.status : '正在读取'}',
                          style: Theme.of(context).textTheme.bodySmall,
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 12),
          ActionTile(
            icon: Icons.refresh_outlined,
            title: '检查官方服务',
            subtitle: '重新读取手机号账户的 Credits、模型和服务状态。',
            actionLabel: '刷新状态',
            onPressed: () => _refresh(context, ref),
          ),
          const SizedBox(height: 12),
          ActionTile(
            icon: Icons.qr_code_scanner_outlined,
            title: '可选：接入其他 LLM',
            subtitle: '扫描 MaClaw GUI 的 LLM 配置页生成的服务商二维码。二维码授权会通过当前 Hub 生效。',
            actionLabel: '扫描桌面二维码',
            onPressed: () => _openQr(context),
          ),
          const SizedBox(height: 12),
          Text(
            '安全提示：移动端不接受任意第三方 URL、API Key 或外部 Hub 地址。',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
          ),
        ],
      ),
    );
  }
}
