import 'package:flutter/material.dart';

import '../../l10n/gen/app_localizations.dart';
import 'bot_models.dart';

/// 与桌面 GUI botAppearance.ts 相同的调色板与形状集。
const botColors = [
  '#16a34a',
  '#2563eb',
  '#7c3aed',
  '#db2777',
  '#ea580c',
  '#ca8a04',
  '#0891b2',
  '#dc2626',
  '#4f46e5',
  '#475569',
];
const botShapes = ['blob', 'cloud', 'leaf', 'square', 'pill', 'circle'];

Color hexColor(String hex) {
  final value = int.tryParse(hex.replaceFirst('#', ''), radix: 16);
  return value == null ? Colors.grey : Color(0xFF000000 | value);
}

String botShapeLabel(String shape, AppLocalizations S) => switch (shape) {
  'blob' => S.shapeBlob,
  'cloud' => S.shapeCloud,
  'leaf' => S.shapeLeaf,
  'square' => S.shapeSquare,
  'pill' => S.shapePill,
  _ => S.shapeCircle,
};

/// Avatar honouring the bot's configured color/shape, falling back to a
/// deterministic palette color by id.
class BotAvatar extends StatelessWidget {
  final Bot bot;
  final double radius;

  /// showAttention draws a small badge dot at the top-right: 主 Bot 的星、
  /// 正在跑的活。只在头像右上角盖一个点，不占卡片宽度。
  final bool showAttention;

  const BotAvatar({
    super.key,
    required this.bot,
    this.radius = 20,
    this.showAttention = false,
  });


  static const _palette = [
    Color(0xFF5B8DEF),
    Color(0xFF7C5CD6),
    Color(0xFF3FA7A0),
    Color(0xFFD97B42),
    Color(0xFFC15B8F),
    Color(0xFF6AA84F),
    Color(0xFF4E79A7),
    Color(0xFFB85C38),
  ];

  Color _color() {
    if (bot.color.isNotEmpty) {
      final parsed = int.tryParse(bot.color.replaceFirst('#', ''), radix: 16);
      if (parsed != null) return Color(0xFF000000 | parsed);
    }
    return _palette[bot.id.hashCode.abs() % _palette.length];
  }

  @override
  Widget build(BuildContext context) {
    final avatar = _shape();
    if (!showAttention) return avatar;
    return Stack(
      clipBehavior: Clip.none,
      children: [
        avatar,
        Positioned(
          right: -2,
          top: -2,
          child: Container(
            width: radius * 0.62,
            height: radius * 0.62,
            decoration: BoxDecoration(
              color: const Color(0xFF3FA7A0),
              shape: BoxShape.circle,
              border: Border.all(
                color: Theme.of(context).colorScheme.surface,
                width: 1.6,
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _shape() {
    final label = Text(
      bot.initials,
      style: TextStyle(
        color: Colors.white,
        fontWeight: FontWeight.w600,
        fontSize: radius * 0.7,
      ),
    );
    final color = _color();
    if (bot.shape == 'square') {
      return Container(
        width: radius * 2,
        height: radius * 2,
        alignment: Alignment.center,
        decoration: BoxDecoration(
          color: color,
          borderRadius: BorderRadius.circular(radius * 0.45),
        ),
        child: label,
      );
    }
    if (bot.shape == 'pill') {
      return Container(
        width: radius * 2.6,
        height: radius * 2,
        alignment: Alignment.center,
        decoration: BoxDecoration(
          color: color,
          borderRadius: BorderRadius.circular(radius),
        ),
        child: label,
      );
    }
    return CircleAvatar(radius: radius, backgroundColor: color, child: label);
  }
}

Color runStatusColor(String status) => switch (status.toLowerCase()) {
  'running' || 'accepted' || 'pending' => const Color(0xFF3FA7A0),
  'paused' => const Color(0xFFD97B42),
  'waiting_for_user' || 'waiting' => const Color(0xFFC15B8F),
  'failed' || 'error' => const Color(0xFFD64545),
  'done' || 'finished' || 'completed' => const Color(0xFF6AA84F),
  _ => Colors.grey,
};
