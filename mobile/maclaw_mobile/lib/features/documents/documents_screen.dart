import 'package:flutter/material.dart';

import '../../shared/surface.dart';

class DocumentsScreen extends StatelessWidget {
  const DocumentsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return ScreenScaffold(
      title: '应急文档',
      subtitle: '快速生成、轻编辑、导出 Word/PDF/Markdown。',
      trailing: IconButton.filledTonal(
        tooltip: '导入文档',
        onPressed: () {},
        icon: const Icon(Icons.upload_file_outlined),
      ),
      children: const [
        ActionTile(
          icon: Icons.note_add_outlined,
          title: '新建文档',
          subtitle: '通知、报告、方案、会议纪要、邮件回复等模板。',
          actionLabel: '新建',
        ),
        SizedBox(height: 12),
        ActionTile(
          icon: Icons.auto_fix_high_outlined,
          title: 'AI 处理',
          subtitle: '摘要、提取、翻译、改写、扩写、润色、格式整理。',
          actionLabel: '选择文件',
        ),
        SizedBox(height: 12),
        ActionTile(
          icon: Icons.ios_share_outlined,
          title: '导出和分享',
          subtitle: '生成 PDF、Word、Markdown 后保存或分享给 IM/邮件。',
          actionLabel: '查看导出',
        ),
      ],
    );
  }
}

