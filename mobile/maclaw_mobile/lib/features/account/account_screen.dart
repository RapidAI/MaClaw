import 'package:flutter/material.dart';

import '../../shared/surface.dart';

class AccountScreen extends StatelessWidget {
  const AccountScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return ScreenScaffold(
      title: '我的',
      subtitle: 'Hub 绑定、服务额度、凭据、缓存与隐私设置。',
      children: const [
        ActionTile(
          icon: Icons.hub_outlined,
          title: 'Hub 服务',
          subtitle: '登录绑定、模型状态、搜索服务状态、额度与用量。',
          actionLabel: '连接 Hub',
        ),
        SizedBox(height: 12),
        ActionTile(
          icon: Icons.security_outlined,
          title: '凭据与隐私',
          subtitle: '管理 token、SSH 密码、私钥口令和导出文件权限。',
          actionLabel: '管理凭据',
        ),
        SizedBox(height: 12),
        ActionTile(
          icon: Icons.cleaning_services_outlined,
          title: '本地缓存',
          subtitle: '清理搜索历史、文档草稿、导出记录和服务器日志。',
          actionLabel: '查看缓存',
        ),
      ],
    );
  }
}

