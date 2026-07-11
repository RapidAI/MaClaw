import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../shared/surface.dart';
import '../digital_employees/digital_employee.dart';
import '../digital_employees/digital_employee_chat_screen.dart';
import '../digital_employees/digital_employees_controller.dart';

/// First-tab surface when MaClaw official Mobile LLM is not available:
/// talk to the user's own digital twin, or guide activation / open PC.
class AssistantDigitalTwinGate extends ConsumerWidget {
  const AssistantDigitalTwinGate({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final employees = ref.watch(digitalEmployeesProvider);
    final scope = ref.watch(digitalEmployeesScopeProvider);
    final shared = ref.watch(digitalEmployeesSharedFlagProvider);
    return ScreenScaffold(
      title: '数字分身',
      subtitle: '未开通官方 Mobile 服务时，可连接你自己电脑上的数字分身。',
      trailing: IconButton.filledTonal(
        tooltip: '刷新',
        onPressed: () => ref.read(digitalEmployeesProvider.notifier).refresh(),
        icon: const Icon(Icons.refresh),
      ),
      children: [
        StatusBanner(
          tone: StatusTone.info,
          icon: Icons.info_outline,
          message: shared || scope == 'shared'
              ? '当前未使用官方 AI 助手，但套餐已允许共享员工池（scope=$scope）。仍可与自己的分身交谈，或开通官方助手。'
              : '当前未使用 MaClaw 官方 AI 助手。免费档仅自己的数字分身（scope=own）；服务卡可查看共享池并开通 Hub 云端助手。',
        ),
        const SizedBox(height: 12),
        Row(
          children: [
            Expanded(
              child: FilledButton.tonalIcon(
                onPressed: () => context.push('/llm-setup'),
                icon: const Icon(Icons.card_membership_outlined),
                label: const Text('服务授权'),
              ),
            ),
            const SizedBox(width: 8),
            Expanded(
              child: OutlinedButton.icon(
                onPressed: () => context.push('/account'),
                icon: const Icon(Icons.storefront_outlined),
                label: const Text('购买/兑换'),
              ),
            ),
          ],
        ),
        const SizedBox(height: 8),
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            onPressed: () =>
                ref.read(digitalEmployeesProvider.notifier).refresh(),
            icon: const Icon(Icons.desktop_windows_outlined, size: 18),
            label: const Text('刷新分身状态'),
          ),
        ),
        const SizedBox(height: 12),
        employees.when(
          data: (items) {
            // Hub free scope returns own-only; shared scope may include pool.
            final mine = items;
            if (mine.isEmpty) {
              return Card(
                child: ListTile(
                  leading: const Icon(Icons.smart_toy_outlined),
                  title: const Text('还没有在线的数字分身'),
                  subtitle: Text(
                    shared || scope == 'shared'
                        ? '仅展示在线分身。请在电脑上启用你的分身并保持在线后刷新；'
                            '也可到「我的」开通官方 AI 助手。'
                        : '仅展示在线分身。请在电脑上打开 MaClaw 并登录同一账号，待分身上线后刷新；'
                            '或到「我的」购买/兑换服务卡开通官方 AI 助手。',
                  ),
                ),
              );
            }
            return Column(
              children: [
                for (final employee in mine) ...[
                  _TwinTile(employee: employee),
                  const SizedBox(height: 12),
                ],
              ],
            );
          },
          loading: () => const LoadingCard(label: '加载数字分身…'),
          error: (error, _) => Card(
            child: ListTile(
              leading: const Icon(Icons.error_outline),
              title: const Text('无法加载数字分身'),
              subtitle: Text('$error'),
            ),
          ),
        ),
      ],
    );
  }
}

class _TwinTile extends StatelessWidget {
  final DigitalEmployee employee;

  const _TwinTile({required this.employee});

  @override
  Widget build(BuildContext context) {
    final online = employee.canSubmitTask;
    return Card(
      child: ListTile(
        leading: Icon(
          online ? Icons.smart_toy : Icons.smart_toy_outlined,
          color: online ? null : Theme.of(context).disabledColor,
        ),
        title: Text(
          employee.name,
          style: TextStyle(
            color: online ? null : Theme.of(context).disabledColor,
          ),
        ),
        subtitle: Text(
          online
              ? '${employee.runtimeLabel} · 点击交谈'
              : '${employee.runtimeLabel} · 分身离线，请打开电脑上的 MaClaw 或开通官方服务',
        ),
        trailing: online
            ? const Icon(Icons.chevron_right)
            : Icon(Icons.block, color: Theme.of(context).disabledColor),
        enabled: online,
        onTap: online
            ? () {
                Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (context) =>
                        DigitalEmployeeChatScreen(employee: employee),
                  ),
                );
              }
            : null,
      ),
    );
  }
}
