import 'package:flutter/material.dart';

import 'bot_models.dart';

/// 与桌面 GUI botAppearance.ts 相同的调色板与形状集。
const botColors = [
  '#16a34a', '#2563eb', '#7c3aed', '#db2777', '#ea580c',
  '#ca8a04', '#0891b2', '#dc2626', '#4f46e5', '#475569',
];
const botShapes = ['blob', 'cloud', 'leaf', 'square', 'pill', 'circle'];

Color hexColor(String hex) {
  final value = int.tryParse(hex.replaceFirst('#', ''), radix: 16);
  return value == null ? Colors.grey : Color(0xFF000000 | value);
}

String botShapeLabel(String shape) => switch (shape) {
      'blob' => '团子',
      'cloud' => '云朵',
      'leaf' => '叶子',
      'square' => '方块',
      'pill' => '胶囊',
      _ => '圆形',
    };

/// Deterministic colored avatar with the bot's initials.
class BotAvatar extends StatelessWidget {
  final Bot bot;
  final double radius;

  const BotAvatar({super.key, required this.bot, this.radius = 20});

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

  @override
  Widget build(BuildContext context) {
    final color = _palette[bot.id.hashCode.abs() % _palette.length];
    return CircleAvatar(
      radius: radius,
      backgroundColor: color,
      child: Text(
        bot.initials,
        style: TextStyle(
          color: Colors.white,
          fontWeight: FontWeight.w600,
          fontSize: radius * 0.7,
        ),
      ),
    );
  }
}

/// Small state chip used on work status rows.
class StateChip extends StatelessWidget {
  final String label;
  final Color color;

  const StateChip({super.key, required this.label, required this.color});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 11,
          color: color,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
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
