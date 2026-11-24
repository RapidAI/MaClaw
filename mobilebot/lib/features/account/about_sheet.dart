import 'dart:io' show Platform;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../core/locale.dart';
import '../../core/platform.dart';
import '../../core/providers.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart';

/// The product's official website. One constant so the about sheet's link,
/// its copyright line and any future surface cannot drift apart.
const kOfficialWebsiteUrl = 'https://www.maclaw.top';

/// 关于 MaclawGo — bottom sheet opened from 我的.
///
/// Everything shown here is read from the installed package or the binary
/// itself: no build number, date, or commit is invented. Only the hubs the app
/// ships with are listed, because that is the only dependency fact the phone
/// can see without asking a server.
Future<void> showAboutSheet(BuildContext context) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (_) => const _AboutSheet(),
  );
}

/// One labelled row in the facts card.
class _Fact {
  final String label;
  final Future<String> Function() value;

  const _Fact(this.label, this.value);
}

class _AboutSheet extends ConsumerStatefulWidget {
  const _AboutSheet();

  @override
  ConsumerState<_AboutSheet> createState() => _AboutSheetState();
}

class _AboutSheetState extends ConsumerState<_AboutSheet> {
  PackageInfo? _info;

  @override
  void initState() {
    super.initState();
    // A test or an unsupported host may have no package metadata; the sheet
    // still shows the app name instead of failing.
    PackageInfo.fromPlatform()
        .then((info) {
          if (mounted) setState(() => _info = info);
        })
        .catchError((Object _) => null);
  }

  String get _version {
    final info = _info;
    if (info == null || info.version.trim().isEmpty) return '—';
    final build = info.buildNumber.trim();
    return build.isEmpty ? info.version : '${info.version} ($build)';
  }

  /// The runtime this build runs on, in words the user can read.
  static String _platformLabel(String ohos, String android, String ios) {
    if (isOhos) return ohos;
    if (Platform.isAndroid) return android;
    if (Platform.isIOS) return ios;
    return Platform.operatingSystem;
  }

  /// Only the hub host the signed-in account talks to; the full URL carries a
  /// tenant path that does not help anyone reading an about page.
  String _serviceHost(String notSignedIn) {
    final hubUrl = ref.read(sessionProvider).active?.hubUrl ?? '';
    final uri = Uri.tryParse(hubUrl);
    if (uri == null || uri.host.isEmpty) return notSignedIn;
    return uri.host;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final s = context.S;
    final facts = <_Fact>[
      _Fact(s.aboutVersion, () async => _version),
      _Fact(
        s.aboutPlatform,
        () async => _platformLabel(
          s.aboutPlatformOhos,
          s.aboutPlatformAndroid,
          s.aboutPlatformIos,
        ),
      ),
      _Fact(s.aboutService, () async => _serviceHost(s.aboutNotSignedIn)),
      _Fact(s.aboutAccount, () async {
        final active = ref.read(sessionProvider).active;
        return active == null ? s.aboutNotSignedIn : active.label;
      }),
      _Fact(s.aboutLastUpdate, () async {
        final prefs = await SharedPreferences.getInstance();
        final stamp = prefs.getInt('mobilebot.update.lastCheck') ?? 0;
        if (stamp <= 0) return s.aboutNever;
        final at = DateTime.fromMillisecondsSinceEpoch(stamp);
        String two(int v) => v.toString().padLeft(2, '0');
        return '${at.year}-${two(at.month)}-${two(at.day)} '
            '${two(at.hour)}:${two(at.minute)}';
      }),
    ];
    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(
          AppSpacing.lg,
          AppSpacing.sm,
          AppSpacing.lg,
          AppSpacing.xxl,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              s.aboutTitle,
              textAlign: TextAlign.center,
              style: theme.textTheme.titleMedium,
            ),
            const SizedBox(height: AppSpacing.md),
            Center(child: const BrandMark(size: 72)),
            const SizedBox(height: AppSpacing.md),
            Text(
              'MaclawGo',
              textAlign: TextAlign.center,
              style: theme.textTheme.titleLarge?.copyWith(
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(height: AppSpacing.xs),
            Text(
              s.acFooter,
              textAlign: TextAlign.center,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            SectionHeader(title: s.aboutInfo, dense: false),
            Card(
              child: Column(
                children: [
                  for (var i = 0; i < facts.length; i++) ...[
                    if (i > 0) const Divider(height: 1, indent: 16, endIndent: 16),
                    _FactRow(fact: facts[i]),
                  ],
                ],
              ),
            ),
            SectionHeader(title: s.aboutLegal),
            Card(
              child: ListTile(
                leading: Icon(
                  Icons.public_outlined,
                  color: theme.colorScheme.primary,
                ),
                title: Text(s.aboutWebsite),
                subtitle: Text(kOfficialWebsiteUrl),
                trailing: const Icon(Icons.open_in_new, size: 18),
                onTap: () => openExternalUrl(kOfficialWebsiteUrl),
              ),
            ),
            Card(
              child: ListTile(
                leading: Icon(
                  Icons.description_outlined,
                  color: theme.colorScheme.primary,
                ),
                title: Text(s.aboutLicenses),
                trailing: const Icon(Icons.chevron_right, size: 20),
                onTap: () => showLicensePage(
                  context: context,
                  applicationName: 'MaclawGo',
                  applicationVersion: _version,
                ),
              ),
            ),
            const SizedBox(height: AppSpacing.lg),
            Text(
              '© ${DateTime.now().year} MaclawGo\n$kOfficialWebsiteUrl',
              textAlign: TextAlign.center,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Resolves one fact asynchronously and falls back to a dash while it loads.
class _FactRow extends StatelessWidget {
  final _Fact fact;

  const _FactRow({required this.fact});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return FutureBuilder<String>(
      future: fact.value(),
      builder: (context, snap) {
        return Padding(
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpacing.md,
            vertical: AppSpacing.sm + 2,
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                flex: 4,
                child: Text(
                  fact.label,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
              const SizedBox(width: AppSpacing.sm),
              Expanded(
                flex: 6,
                child: Text(
                  snap.hasData ? snap.data! : '—',
                  textAlign: TextAlign.right,
                  style: theme.textTheme.bodyMedium,
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}
