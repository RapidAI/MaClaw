import 'package:flutter/material.dart';

import '../../shared/surface.dart';
import 'digital_employee.dart';

const _sampleEmployees = [
  DigitalEmployee(
    id: 'demo-ops',
    machineId: 'ops-workstation',
    name: '运维助手',
    skillDescription: '可协助检查服务器日志、服务状态和常用维护命令。',
    onlineStatus: 'online',
    accessPolicy: 'public',
    resident: true,
    runtimeMissing: false,
  ),
  DigitalEmployee(
    id: 'demo-office',
    machineId: 'office-pc',
    name: '办公电脑助手',
    skillDescription: '可调用远程电脑上的文件、文档和企业应用能力。',
    onlineStatus: 'offline',
    accessPolicy: 'per_request',
    resident: false,
    runtimeMissing: false,
  ),
];

class DigitalEmployeesScreen extends StatelessWidget {
  const DigitalEmployeesScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return ScreenScaffold(
      title: '数字员工',
      subtitle: '接入远程服务器/电脑上的能力，让手机发起任务、查看结果和请求授权。',
      trailing: IconButton.filledTonal(
        tooltip: '刷新',
        onPressed: () {},
        icon: const Icon(Icons.refresh),
      ),
      children: [
        for (final employee in _sampleEmployees) ...[
          _DigitalEmployeeCard(employee: employee),
          const SizedBox(height: 12),
        ],
        const ActionTile(
          icon: Icons.security_outlined,
          title: '权限说明',
          subtitle: '私有或按次授权的数字员工会先向拥有者发起确认，手机不会绕过远程电脑策略。',
          actionLabel: '查看策略',
        ),
      ],
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
                  child: FilledButton.icon(
                    onPressed: employee.online ? () {} : null,
                    icon: const Icon(Icons.chat_outlined),
                    label: const Text('发起会话'),
                  ),
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

