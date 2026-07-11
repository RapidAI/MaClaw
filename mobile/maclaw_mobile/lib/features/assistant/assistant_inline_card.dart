import 'package:flutter/material.dart';

import '../../shared/theme.dart';

/// One tappable action on an [AssistantInlineCard], inspired by GUI
/// `InlineChatCard` but reimplemented in pure Dart UI.
class AssistantInlineCardAction {
  final String key;
  final String label;
  final IconData? icon;
  final bool primary;
  final bool danger;

  const AssistantInlineCardAction({
    required this.key,
    required this.label,
    this.icon,
    this.primary = false,
    this.danger = false,
  });
}

/// Lightweight interactive card embedded in the assistant chat stream.
///
/// Used for post-answer next steps (draft / employee / open documents) and
/// simple confirmations — not a full GUI agent card system.
class AssistantInlineCard extends StatelessWidget {
  final String? title;
  final String? description;
  final List<String> summaryLines;
  final List<AssistantInlineCardAction> actions;
  final ValueChanged<String>? onAction;
  final bool resolved;
  final String? resolvedLabel;
  final String? testId;

  const AssistantInlineCard({
    super.key,
    this.title,
    this.description,
    this.summaryLines = const [],
    required this.actions,
    this.onAction,
    this.resolved = false,
    this.resolvedLabel,
    this.testId,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final textTheme = Theme.of(context).textTheme;
    final dark = Theme.of(context).brightness == Brightness.dark;
    final bg = dark ? MaClawColors.darkElevated : scheme.surfaceContainerLow;
    final border = scheme.outlineVariant;

    return Opacity(
      opacity: resolved ? 0.72 : 1,
      child: Container(
        key: testId == null ? null : ValueKey(testId),
        width: double.infinity,
        margin: const EdgeInsets.only(top: 8),
        padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
        decoration: BoxDecoration(
          color: bg,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: border),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (title != null && title!.trim().isNotEmpty)
              Text(
                title!,
                style: textTheme.labelLarge?.copyWith(
                  fontWeight: FontWeight.w700,
                  color: scheme.onSurface,
                ),
              ),
            if (description != null && description!.trim().isNotEmpty) ...[
              if (title != null) const SizedBox(height: 4),
              Text(
                description!,
                style: textTheme.bodySmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                  height: 1.35,
                ),
              ),
            ],
            if (summaryLines.isNotEmpty) ...[
              const SizedBox(height: 8),
              Container(
                width: double.infinity,
                padding:
                    const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                decoration: BoxDecoration(
                  color: scheme.surface.withValues(alpha: dark ? 0.35 : 0.7),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    for (final line in summaryLines)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 2),
                        child: Text(
                          line,
                          style: textTheme.labelSmall?.copyWith(
                            color: scheme.onSurfaceVariant,
                            fontFamily: 'monospace',
                            height: 1.35,
                          ),
                        ),
                      ),
                  ],
                ),
              ),
            ],
            if (resolved &&
                resolvedLabel != null &&
                resolvedLabel!.trim().isNotEmpty) ...[
              const SizedBox(height: 8),
              Row(
                children: [
                  Icon(Icons.check_circle_outline,
                      size: 16, color: scheme.primary),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      resolvedLabel!,
                      style: textTheme.labelMedium?.copyWith(
                        color: scheme.primary,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                ],
              ),
            ],
            if (!resolved && actions.isNotEmpty) ...[
              const SizedBox(height: 10),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final action in actions)
                    _ActionButton(
                      action: action,
                      onPressed: onAction == null
                          ? null
                          : () => onAction!(action.key),
                    ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _ActionButton extends StatelessWidget {
  final AssistantInlineCardAction action;
  final VoidCallback? onPressed;

  const _ActionButton({
    required this.action,
    required this.onPressed,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final child = Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (action.icon != null) ...[
          Icon(action.icon, size: 16),
          const SizedBox(width: 6),
        ],
        Text(action.label),
      ],
    );

    if (action.primary) {
      return FilledButton.tonal(
        onPressed: onPressed,
        style: FilledButton.styleFrom(
          visualDensity: VisualDensity.compact,
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        ),
        child: child,
      );
    }
    if (action.danger) {
      return OutlinedButton(
        onPressed: onPressed,
        style: OutlinedButton.styleFrom(
          foregroundColor: scheme.error,
          visualDensity: VisualDensity.compact,
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        ),
        child: child,
      );
    }
    return OutlinedButton(
      onPressed: onPressed,
      style: OutlinedButton.styleFrom(
        visualDensity: VisualDensity.compact,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      ),
      child: child,
    );
  }
}

/// Task-oriented next steps after an assistant answer (not share/copy).
List<AssistantInlineCardAction> assistantDefaultNextStepActions({
  bool includeEmployee = true,
  bool includeDocuments = true,
}) {
  return [
    const AssistantInlineCardAction(
      key: 'draft',
      label: '整理为草稿',
      icon: Icons.article_outlined,
      primary: true,
    ),
    if (includeEmployee)
      const AssistantInlineCardAction(
        key: 'employee',
        label: '派给员工',
        icon: Icons.badge_outlined,
      ),
    if (includeDocuments)
      const AssistantInlineCardAction(
        key: 'documents',
        label: '打开文档',
        icon: Icons.folder_open_outlined,
      ),
  ];
}
