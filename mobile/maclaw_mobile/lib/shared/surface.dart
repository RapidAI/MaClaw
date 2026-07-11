import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/network/mobile_network_status.dart';
import 'theme.dart';

/// Shared page chrome used by main tab screens.
class ScreenScaffold extends ConsumerWidget {
  final String title;
  final String subtitle;
  final List<Widget> children;
  final Widget? trailing;

  const ScreenScaffold({
    super.key,
    required this.title,
    required this.subtitle,
    required this.children,
    this.trailing,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final text = Theme.of(context).textTheme;
    final scheme = Theme.of(context).colorScheme;
    final network = ref.watch(mobileNetworkStatusProvider);
    return ListView(
      padding: const EdgeInsets.fromLTRB(
        MaClawColors.spaceXl,
        MaClawColors.spaceLg,
        MaClawColors.spaceXl,
        MaClawColors.spaceXxl,
      ),
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: text.headlineSmall?.copyWith(
                      fontWeight: FontWeight.w700,
                      letterSpacing: -0.01,
                    ),
                  ),
                  const SizedBox(height: 6),
                  Text(
                    subtitle,
                    style: text.bodyMedium?.copyWith(
                      color: scheme.onSurfaceVariant,
                      height: 1.4,
                    ),
                  ),
                ],
              ),
            ),
            if (trailing != null) ...[
              const SizedBox(width: 8),
              trailing!,
            ],
          ],
        ),
        const SizedBox(height: MaClawColors.spaceXl),
        _NetworkStatusBanner(status: network),
        if (network.valueOrNull?.offline == true ||
            network.valueOrNull?.restored == true)
          const SizedBox(height: MaClawColors.spaceMd),
        ...children,
      ],
    );
  }
}

class _NetworkStatusBanner extends StatelessWidget {
  final AsyncValue<MobileNetworkSnapshot> status;

  const _NetworkStatusBanner({required this.status});

  @override
  Widget build(BuildContext context) {
    final snapshot = status.valueOrNull;
    if (snapshot == null || (!snapshot.offline && !snapshot.restored)) {
      return const SizedBox.shrink();
    }
    return StatusBanner(
      tone: snapshot.restored ? StatusTone.success : StatusTone.danger,
      icon: snapshot.restored ? Icons.wifi_outlined : Icons.wifi_off_outlined,
      message: snapshot.message,
    );
  }
}

enum StatusTone { info, success, warning, danger }

/// Compact semantic banner for network, capability, and recovery states.
class StatusBanner extends StatelessWidget {
  final StatusTone tone;
  final IconData icon;
  final String message;
  final String? title;

  const StatusBanner({
    super.key,
    required this.tone,
    required this.icon,
    required this.message,
    this.title,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final colors = _toneColors(scheme, tone);
    return DecoratedBox(
      decoration: BoxDecoration(
        color: colors.background,
        borderRadius: BorderRadius.circular(MaClawColors.radiusSm),
        border: Border.all(color: colors.border),
      ),
      child: Padding(
        padding: const EdgeInsets.all(MaClawColors.spaceMd),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(icon, color: colors.foreground, size: 20),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (title != null && title!.trim().isNotEmpty) ...[
                    Text(
                      title!,
                      style: Theme.of(context).textTheme.titleSmall?.copyWith(
                            color: colors.foreground,
                            fontWeight: FontWeight.w600,
                          ),
                    ),
                    const SizedBox(height: 2),
                  ],
                  Text(
                    message,
                    style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                          color: colors.foreground,
                          height: 1.4,
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

class _ToneColors {
  final Color background;
  final Color foreground;
  final Color border;

  const _ToneColors({
    required this.background,
    required this.foreground,
    required this.border,
  });
}

_ToneColors _toneColors(ColorScheme scheme, StatusTone tone) {
  return switch (tone) {
    StatusTone.success => _ToneColors(
        background: scheme.secondaryContainer,
        foreground: scheme.onSecondaryContainer,
        border: scheme.secondary.withValues(alpha: 0.28),
      ),
    StatusTone.danger => _ToneColors(
        background: scheme.errorContainer,
        foreground: scheme.onErrorContainer,
        border: scheme.error.withValues(alpha: 0.28),
      ),
    StatusTone.warning => _ToneColors(
        background: scheme.tertiaryContainer,
        foreground: scheme.onTertiaryContainer,
        border: scheme.tertiary.withValues(alpha: 0.28),
      ),
    StatusTone.info => _ToneColors(
        background: scheme.primaryContainer.withValues(alpha: 0.55),
        foreground: scheme.onPrimaryContainer,
        border: scheme.primary.withValues(alpha: 0.22),
      ),
  };
}

/// Section title row used inside cards and workspaces.
class SectionHeader extends StatelessWidget {
  final IconData icon;
  final String title;
  final String? trailing;
  final Widget? action;

  const SectionHeader({
    super.key,
    required this.icon,
    required this.title,
    this.trailing,
    this.action,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Row(
      children: [
        Icon(icon, color: scheme.primary, size: 20),
        const SizedBox(width: 8),
        Expanded(
          child: Text(
            title,
            style: text.titleMedium?.copyWith(fontWeight: FontWeight.w600),
          ),
        ),
        if (trailing != null && trailing!.isNotEmpty)
          Text(
            trailing!,
            style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
          ),
        if (action != null) action!,
      ],
    );
  }
}

/// Loading placeholder that matches card language across screens.
class LoadingCard extends StatelessWidget {
  final String? label;

  const LoadingCard({super.key, this.label});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(MaClawColors.spaceLg),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (label != null && label!.isNotEmpty) ...[
              Text(
                label!,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
              ),
              const SizedBox(height: 10),
            ],
            const LinearProgressIndicator(),
          ],
        ),
      ),
    );
  }
}

/// Empty / error educational panel used when a list has no data.
class EmptyStatePanel extends StatelessWidget {
  final IconData icon;
  final String title;
  final String message;
  final Widget? action;

  const EmptyStatePanel({
    super.key,
    required this.icon,
    required this.title,
    required this.message,
    this.action,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(MaClawColors.spaceLg),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            DecoratedBox(
              decoration: BoxDecoration(
                color: scheme.primaryContainer.withValues(alpha: 0.55),
                borderRadius: BorderRadius.circular(10),
              ),
              child: Padding(
                padding: const EdgeInsets.all(10),
                child: Icon(icon, color: scheme.primary, size: 22),
              ),
            ),
            const SizedBox(width: MaClawColors.spaceMd),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: text.titleMedium?.copyWith(
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    message,
                    style: text.bodyMedium?.copyWith(
                      color: scheme.onSurfaceVariant,
                      height: 1.4,
                    ),
                  ),
                  if (action != null) ...[
                    const SizedBox(height: MaClawColors.spaceMd),
                    action!,
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Compact labeled value row for account / status cards.
class InfoRow extends StatelessWidget {
  final String label;
  final String value;
  final bool mono;

  const InfoRow({
    super.key,
    required this.label,
    required this.value,
    this.mono = false,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 88,
            child: Text(
              label,
              style: text.bodySmall?.copyWith(
                color: scheme.onSurfaceVariant,
                fontWeight: FontWeight.w500,
              ),
            ),
          ),
          Expanded(
            child: Text(
              value.isEmpty ? '—' : value,
              style: text.bodyMedium?.copyWith(
                fontFamily: mono ? 'monospace' : null,
                height: 1.35,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Immersive chat-style page: fixed header + scrollable body + docked composer.
///
/// Used by AI 助手 and can host any message list + input dock without nesting
/// the composer inside a single scroll view.
class ChatWorkspaceScaffold extends ConsumerWidget {
  final String title;
  final String subtitle;
  final Widget? trailing;
  final Widget? topBar;
  final Widget body;
  final Widget composer;

  const ChatWorkspaceScaffold({
    super.key,
    required this.title,
    required this.subtitle,
    required this.body,
    required this.composer,
    this.trailing,
    this.topBar,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final text = Theme.of(context).textTheme;
    final scheme = Theme.of(context).colorScheme;
    final network = ref.watch(mobileNetworkStatusProvider);
    final showNetwork = network.valueOrNull?.offline == true ||
        network.valueOrNull?.restored == true;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(
            MaClawColors.spaceXl,
            MaClawColors.spaceMd,
            MaClawColors.spaceXl,
            MaClawColors.spaceSm,
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      style: text.headlineSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                        letterSpacing: -0.01,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      subtitle,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: text.bodySmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                        height: 1.35,
                      ),
                    ),
                  ],
                ),
              ),
              if (trailing != null) ...[
                const SizedBox(width: 8),
                trailing!,
              ],
            ],
          ),
        ),
        if (showNetwork)
          Padding(
            padding: const EdgeInsets.fromLTRB(
              MaClawColors.spaceXl,
              0,
              MaClawColors.spaceXl,
              MaClawColors.spaceSm,
            ),
            child: _NetworkStatusBanner(status: network),
          ),
        if (topBar != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(
              MaClawColors.spaceXl,
              0,
              MaClawColors.spaceXl,
              MaClawColors.spaceSm,
            ),
            child: topBar!,
          ),
        Expanded(child: body),
        ChatComposerDock(child: composer),
      ],
    );
  }
}

/// Bottom dock for chat composers — elevated surface, top hairline border.
class ChatComposerDock extends StatelessWidget {
  final Widget child;

  const ChatComposerDock({super.key, required this.child});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final dark = Theme.of(context).brightness == Brightness.dark;
    return Material(
      color: dark ? MaClawColors.darkElevated : scheme.surface,
      elevation: 0,
      child: DecoratedBox(
        decoration: BoxDecoration(
          border: Border(
            top: BorderSide(color: scheme.outlineVariant),
          ),
          color: dark ? MaClawColors.darkElevated : scheme.surface,
        ),
        child: SafeArea(
          top: false,
          minimum: const EdgeInsets.only(bottom: 2),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 8),
            child: child,
          ),
        ),
      ),
    );
  }
}

/// Shared chat bubble chrome for user / assistant / system messages.
class ChatBubble extends StatelessWidget {
  final String text;
  final bool fromUser;
  final bool failed;
  final Widget? footer;
  /// When set, replaces the default plain-text body (e.g. Markdown).
  final Widget? body;
  final EdgeInsetsGeometry? margin;

  const ChatBubble({
    super.key,
    required this.text,
    this.fromUser = false,
    this.failed = false,
    this.footer,
    this.body,
    this.margin,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final textTheme = Theme.of(context).textTheme;
    final Color background;
    final Color foreground;
    final Color border;

    if (fromUser) {
      background = scheme.primaryContainer;
      foreground = scheme.onPrimaryContainer;
      border = scheme.primary.withValues(alpha: 0.16);
    } else if (failed) {
      background = scheme.errorContainer;
      foreground = scheme.onErrorContainer;
      border = scheme.error.withValues(alpha: 0.22);
    } else {
      final dark = Theme.of(context).brightness == Brightness.dark;
      background =
          dark ? MaClawColors.darkCard : scheme.surfaceContainerHighest;
      foreground = scheme.onSurface;
      border = scheme.outlineVariant;
    }

    return Align(
      alignment: fromUser
          ? AlignmentDirectional.centerEnd
          : AlignmentDirectional.centerStart,
      child: Container(
        constraints: const BoxConstraints(maxWidth: 620),
        margin: margin ??
            EdgeInsets.only(
              bottom: 10,
              left: fromUser ? 40 : 0,
              right: fromUser ? 0 : 28,
            ),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
        decoration: BoxDecoration(
          color: background,
          borderRadius: BorderRadius.only(
            topLeft: const Radius.circular(14),
            topRight: const Radius.circular(14),
            bottomLeft: Radius.circular(fromUser ? 14 : 4),
            bottomRight: Radius.circular(fromUser ? 4 : 14),
          ),
          border: Border.all(color: border),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            body ??
                Text(
                  text,
                  style: textTheme.bodyMedium?.copyWith(
                    color: foreground,
                    height: 1.4,
                  ),
                ),
            if (footer != null) ...[
              const SizedBox(height: 8),
              footer!,
            ],
          ],
        ),
      ),
    );
  }
}

class ActionTile extends StatelessWidget {
  final IconData icon;
  final String title;
  final String subtitle;
  final String actionLabel;
  final VoidCallback? onPressed;

  const ActionTile({
    super.key,
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.actionLabel,
    this.onPressed,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(MaClawColors.spaceLg),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            DecoratedBox(
              decoration: BoxDecoration(
                color: scheme.primaryContainer.withValues(alpha: 0.55),
                borderRadius: BorderRadius.circular(10),
              ),
              child: Padding(
                padding: const EdgeInsets.all(10),
                child: Icon(icon, color: scheme.primary, size: 20),
              ),
            ),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: text.titleMedium?.copyWith(
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    subtitle,
                    style: text.bodyMedium?.copyWith(
                      color: scheme.onSurfaceVariant,
                      height: 1.4,
                    ),
                  ),
                  const SizedBox(height: MaClawColors.spaceMd),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: FilledButton.tonal(
                      onPressed: onPressed,
                      child: Text(actionLabel),
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
