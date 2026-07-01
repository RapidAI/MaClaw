import 'package:flutter/material.dart';

import '../../shared/surface.dart';

class ServersScreen extends StatelessWidget {
  const ServersScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return ScreenScaffold(
      title: '应急服务器',
      subtitle: '手动 SSH 维护，AI 只解释日志和生成命令草案。',
      trailing: IconButton.filledTonal(
        tooltip: '新增服务器',
        onPressed: () {},
        icon: const Icon(Icons.add),
      ),
      children: const [
        ActionTile(
          icon: Icons.dns_outlined,
          title: '服务器配置',
          subtitle: 'Host、端口、用户名、密码或密钥会安全存储。',
          actionLabel: '添加服务器',
        ),
        SizedBox(height: 12),
        ActionTile(
          icon: Icons.terminal_outlined,
          title: '手动 SSH 终端',
          subtitle: '常用命令、历史命令、复制输出、断线重连。',
          actionLabel: '打开终端',
        ),
        SizedBox(height: 12),
        ActionTile(
          icon: Icons.psychology_alt_outlined,
          title: 'AI 分析终端输出',
          subtitle: '解释错误日志，生成排查建议和命令草案。',
          actionLabel: '粘贴日志',
        ),
      ],
    );
  }
}

