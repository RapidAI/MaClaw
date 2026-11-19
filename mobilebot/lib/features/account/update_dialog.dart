import 'dart:io';

import 'package:flutter/material.dart';
import 'package:open_filex/open_filex.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../core/app_update.dart';
import '../../core/locale.dart';

/// 检查更新 bottom sheet — Android only (iOS has no sideload channel; the
/// caller hides the entry there). Mirrors the desktop update dialog: check →
/// offer → download with progress → sha256 verify → hand to the installer.
Future<void> showUpdateSheet(
  BuildContext context, {
  AppUpdate? prechecked,
  AppUpdater? updater,
}) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (context) =>
        _UpdateSheet(prechecked: prechecked, updater: updater),
  );
}

class _UpdateSheet extends StatefulWidget {
  /// A silent background check may already have the result — skip the fetch.
  final AppUpdate? prechecked;

  /// Injectable for tests; production always uses the real updater.
  final AppUpdater? updater;

  const _UpdateSheet({this.prechecked, this.updater});

  @override
  State<_UpdateSheet> createState() => _UpdateSheetState();
}

enum _Phase { checking, idle, upToDate, available, downloading, ready, error }

class _UpdateSheetState extends State<_UpdateSheet> {
  late final AppUpdater _updater = widget.updater ?? AppUpdater();
  _Phase _phase = _Phase.checking;
  String _detail = '';
  AppUpdate? _update;
  double _progress = 0;
  File? _apk;
  String _currentVersion = '';

  @override
  void initState() {
    super.initState();
    PackageInfo.fromPlatform().then((info) {
      if (mounted) setState(() => _currentVersion = info.version);
    });
    if (widget.prechecked != null) {
      _update = widget.prechecked;
      _phase = _Phase.available;
    } else {
      _check();
    }
  }

  Future<void> _check() async {
    setState(() {
      _phase = _Phase.checking;
      _detail = '';
    });
    try {
      final info = await PackageInfo.fromPlatform();
      final result = await _updater.check(info.version);
      if (!mounted) return;
      if (result.failed) {
        setState(() {
          _phase = _Phase.error;
          _detail = result.latestVersion.isNotEmpty
              ? result.latestVersion
              : context.S.updCantReachServer;
        });
      } else if (result.hasUpdate) {
        setState(() {
          _update = result.update;
          _phase = _Phase.available;
        });
      } else {
        setState(() {
          _phase = _Phase.upToDate;
          _detail = result.latestVersion.isNotEmpty
              ? context.S.updUpToDateVersion(result.latestVersion)
              : context.S.updUpToDate;
        });
      }
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _phase = _Phase.error;
        _detail = error.toString();
      });
    }
  }

  Future<void> _download() async {
    final update = _update;
    if (update == null) return;
    setState(() {
      _phase = _Phase.downloading;
      _progress = 0;
      _detail = '';
    });
    try {
      final apk = await _updater.download(
        update,
        onProgress: (p) {
          if (mounted) setState(() => _progress = p);
        },
      );
      if (!mounted) return;
      setState(() {
        _apk = apk;
        _phase = _Phase.ready;
      });
      await _install();
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _phase = _Phase.error;
        _detail = error.toString();
      });
    }
  }

  Future<void> _install() async {
    final apk = _apk;
    if (apk == null) return;
    // 签名不一致时系统安装器只报“签名冲突”——提前检出并给出可操作的提示。
    final compatible = await _updater.apkSignatureCompatible(apk);
    if (!mounted) return;
    if (compatible == false) {
      setState(() {
        _phase = _Phase.error;
        _detail = context.S.updSignatureMismatch;
      });
      return;
    }
    final result = await OpenFilex.open(
      apk.path,
      type: 'application/vnd.android.package-archive',
    );
    if (!mounted) return;
    // INSTALL_FAILED / user denied the unknown-sources prompt — surface it.
    if (result.type != ResultType.done) {
      setState(() {
        _detail = result.message.isNotEmpty
            ? result.message
            : context.S.updCantOpenInstaller;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.only(
        left: 24,
        right: 24,
        top: 8,
        bottom: 24 + MediaQuery.of(context).viewInsets.bottom,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            context.S.checkUpdate,
            textAlign: TextAlign.center,
            style: theme.textTheme.titleLarge,
          ),
          const SizedBox(height: 4),
          Text(
            _currentVersion.isEmpty
                ? 'MaClawGo'
                : context.S.updCurrentVersion(_currentVersion),
            textAlign: TextAlign.center,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 20),
          switch (_phase) {
            _Phase.checking => const Padding(
              padding: EdgeInsets.symmetric(vertical: 24),
              child: Center(child: CircularProgressIndicator()),
            ),
            _Phase.upToDate => Column(
              children: [
                Icon(
                  Icons.check_circle_outline,
                  size: 40,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(height: 8),
                Text(_detail, style: theme.textTheme.bodyMedium),
              ],
            ),
            _Phase.available => Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  context.S.updNewVersion(_update!.version),
                  style: theme.textTheme.titleMedium,
                ),
                const SizedBox(height: 6),
                Text(
                  context.S.updInstallHint(updateAssetName),
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: 16),
                FilledButton.icon(
                  onPressed: _download,
                  icon: const Icon(Icons.download),
                  label: Text(context.S.updDownloadInstall),
                ),
                TextButton(
                  onPressed: () => launchUrl(Uri.parse(_update!.releaseUrl)),
                  child: Text(context.S.updViewInBrowser),
                ),
              ],
            ),
            _Phase.downloading => Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                LinearProgressIndicator(value: _progress),
                const SizedBox(height: 8),
                Text(
                  context.S.updDownloading(
                    (100 * _progress).toStringAsFixed(0),
                  ),
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodySmall,
                ),
              ],
            ),
            _Phase.ready => Column(
              children: [
                Icon(
                  Icons.system_update_alt,
                  size: 40,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(height: 8),
                Text(context.S.updOpeningInstaller),
                if (_detail.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(
                    _detail,
                    textAlign: TextAlign.center,
                    style: TextStyle(color: theme.colorScheme.error),
                  ),
                ],
              ],
            ),
            _Phase.idle => const SizedBox.shrink(),
            _Phase.error => Column(
              children: [
                Icon(
                  Icons.error_outline,
                  size: 40,
                  color: theme.colorScheme.error,
                ),
                const SizedBox(height: 8),
                Text(
                  _detail.isEmpty ? context.S.updCheckFailed : _detail,
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodyMedium,
                ),
                const SizedBox(height: 12),
                FilledButton.tonal(
                  onPressed: _check,
                  child: Text(context.S.retry),
                ),
                // 签名不一致等场景要「卸载后手动安装」——给出官网下载页。
                if (_update != null)
                  TextButton(
                    onPressed: () => launchUrl(Uri.parse(_update!.releaseUrl)),
                    child: Text(context.S.updOpenDownloadPage),
                  ),
              ],
            ),
          },
          const SizedBox(height: 8),
        ],
      ),
    );
  }
}
