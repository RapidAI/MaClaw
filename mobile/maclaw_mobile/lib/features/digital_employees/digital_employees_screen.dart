import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../shared/surface.dart';
import 'digital_employee.dart';
import 'digital_employees_controller.dart';

class DigitalEmployeesScreen extends ConsumerWidget {
  const DigitalEmployeesScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final employees = ref.watch(digitalEmployeesProvider);
    return ScreenScaffold(
      title: '数字员工',
      subtitle: '接入远程服务器/电脑上的能力，让手机发起任务、查看结果和请求授权。',
      trailing: IconButton.filledTonal(
        tooltip: '刷新',
        onPressed: () =>
            ref.read(digitalEmployeesProvider.notifier).refresh(),
        icon: const Icon(Icons.refresh),
      ),
      children: [
        employees.when(
          data: (items) => items.isEmpty
              ? const _EmptyEmployees()
              : Column(
                  children: [
                    for (final employee in items) ...[
                      _DigitalEmployeeCard(employee: employee),
                      const SizedBox(height: 12),
                    ],
                  ],
                ),
          error: (error, _) => _EmployeeError(error: error),
          loading: () => const _EmployeeLoading(),
        ),
        const SizedBox(height: 12),
        const ActionTile(
          icon: Icons.security_outlined,
          title: '权限说明',
          subtitle: '私有或按次授权的数字员工会先向拥有者发起确认，手机不会绕过远程电脑策略。',
          actionLabel: '查看策略',
        ),
        const SizedBox(height: 12),
        const _TaskStatusCard(),
      ],
    );
  }
}

class _TaskStatusCard extends ConsumerWidget {
  const _TaskStatusCard();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final task = ref.watch(digitalEmployeeTaskProvider);
    return task.when(
      data: (value) {
        if (value == null) return const SizedBox.shrink();
        return Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('最近任务', style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: 6),
                Text('状态：${value.status}'),
                if (value.prompt.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text('任务：${value.prompt}'),
                ],
                if (value.claimedBy.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text('领取者：${value.claimedBy}'),
                ],
                if (value.result.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text(value.result),
                ],
                const SizedBox(height: 10),
                OutlinedButton.icon(
                  onPressed: () => ref
                      .read(digitalEmployeeTaskProvider.notifier)
                      .refreshTask(),
                  icon: const Icon(Icons.refresh),
                  label: const Text('刷新状态'),
                ),
              ],
            ),
          ),
        );
      },
      error: (error, _) => Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Text('任务状态加载失败：$error'),
        ),
      ),
      loading: () => const Card(
        child: Padding(
          padding: EdgeInsets.all(16),
          child: LinearProgressIndicator(),
        ),
      ),
    );
  }
}

class _EmployeeLoading extends StatelessWidget {
  const _EmployeeLoading();

  @override
  Widget build(BuildContext context) {
    return const Card(
      child: Padding(
        padding: EdgeInsets.all(16),
        child: LinearProgressIndicator(),
      ),
    );
  }
}

class _EmployeeError extends StatelessWidget {
  final Object error;

  const _EmployeeError({required this.error});

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Text('数字员工加载失败：$error'),
      ),
    );
  }
}

class _EmptyEmployees extends StatelessWidget {
  const _EmptyEmployees();

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(Icons.desktop_access_disabled_outlined, color: scheme.primary),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '暂无可用数字员工',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 4),
                  Text(
                    '在远程服务器或办公电脑上启用数字员工后，手机端会在这里显示可调用能力。',
                    style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                          color: scheme.onSurfaceVariant,
                        ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _DigitalEmployeeCard extends StatelessWidget {
  final DigitalEmployee employee;

  const _DigitalEmployeeCard({required this.employee});

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
                CircleAvatar(
                  backgroundColor: employee.online
                      ? scheme.secondaryContainer
                      : scheme.surfaceContainerHighest,
                  child: Icon(
                    employee.online ? Icons.smart_toy_outlined : Icons.cloud_off,
                    color: employee.online
                        ? scheme.onSecondaryContainer
                        : scheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        employee.name,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      Text(
                        employee.machineId,
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                              color: scheme.onSurfaceVariant,
                            ),
                      ),
                    ],
                  ),
                ),
                _StatusChip(online: employee.online),
              ],
            ),
            const SizedBox(height: 12),
            Text(employee.skillDescription),
            const SizedBox(height: 14),
            Row(
              children: [
                Expanded(
                  child: _TaskButton(employee: employee),
                ),
                const SizedBox(width: 10),
                IconButton.outlined(
                  tooltip: '分析日志/输出',
                  onPressed: employee.online ? () {} : null,
                  icon: const Icon(Icons.plagiarism_outlined),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _TaskButton extends ConsumerWidget {
  final DigitalEmployee employee;

  const _TaskButton({required this.employee});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final task = ref.watch(digitalEmployeeTaskProvider);
    return FilledButton.icon(
      onPressed: employee.online
          ? () => _showTaskSheet(context, ref)
          : null,
      icon: const Icon(Icons.chat_outlined),
      label: Text(
        task.isLoading ? '提交中' : '发起任务',
      ),
    );
  }

  Future<void> _showTaskSheet(BuildContext context, WidgetRef ref) async {
    final prompt = await showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      builder: (context) => _DigitalEmployeeTaskSheet(employee: employee),
    );
    if (prompt == null || prompt.trim().isEmpty) return;
    await ref.read(digitalEmployeeTaskProvider.notifier).createTask(
          employeeId: employee.id,
          prompt: prompt,
        );
  }
}

class _DigitalEmployeeTaskSheet extends ConsumerStatefulWidget {
  final DigitalEmployee employee;

  const _DigitalEmployeeTaskSheet({required this.employee});

  @override
  ConsumerState<_DigitalEmployeeTaskSheet> createState() =>
      _DigitalEmployeeTaskSheetState();
}

class _DigitalEmployeeTaskSheetState
    extends ConsumerState<_DigitalEmployeeTaskSheet> {
  late final TextEditingController _promptController;

  static const _templates = [
    '请检查远程电脑/服务器当前运行状态，列出异常、风险和建议操作。',
    '请查看最近的服务错误日志，整理可能原因和下一步排查命令。',
    '请检查磁盘、内存、CPU、网络连接状态，并给出应急处理建议。',
    '请帮我在远程电脑上整理指定目录/文件的关键信息，并返回摘要。',
  ];

  @override
  void initState() {
    super.initState();
    _promptController = TextEditingController(text: _templates.first);
  }

  @override
  void dispose() {
    _promptController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final history = ref.watch(digitalEmployeePromptHistoryProvider);
    final bottom = MediaQuery.of(context).viewInsets.bottom;
    return Padding(
      padding: EdgeInsets.fromLTRB(16, 16, 16, 16 + bottom),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '发给 ${widget.employee.name}',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _promptController,
              minLines: 4,
              maxLines: 8,
              decoration: const InputDecoration(
                labelText: '任务说明',
                alignLabelWithHint: true,
                prefixIcon: Icon(Icons.task_alt_outlined),
              ),
            ),
            const SizedBox(height: 12),
            Text('任务模板', style: Theme.of(context).textTheme.labelLarge),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final template in _templates)
                  ActionChip(
                    label: Text(template),
                    onPressed: () => _promptController.text = template,
                  ),
              ],
            ),
            const SizedBox(height: 12),
            history.when(
              data: (items) {
                final recent = items
                    .where((item) => item.employeeId == widget.employee.id)
                    .take(5)
                    .toList();
                if (recent.isEmpty) return const SizedBox.shrink();
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('最近任务', style: Theme.of(context).textTheme.labelLarge),
                    const SizedBox(height: 8),
                    for (final item in recent)
                      ListTile(
                        dense: true,
                        contentPadding: EdgeInsets.zero,
                        leading: const Icon(Icons.history),
                        title: Text(item.prompt),
                        onTap: () => _promptController.text = item.prompt,
                      ),
                  ],
                );
              },
              error: (error, _) => Text('最近任务加载失败：$error'),
              loading: () => const LinearProgressIndicator(),
            ),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: () =>
                    Navigator.of(context).pop(_promptController.text),
                icon: const Icon(Icons.send_outlined),
                label: const Text('提交任务'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  final bool online;

  const _StatusChip({required this.online});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Chip(
      visualDensity: VisualDensity.compact,
      label: Text(online ? '在线' : '离线'),
      avatar: Icon(online ? Icons.check_circle : Icons.radio_button_unchecked),
      backgroundColor:
          online ? scheme.secondaryContainer : scheme.surfaceContainerHighest,
    );
  }
}
