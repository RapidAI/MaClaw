import 'dart:io';

import 'package:flutter/material.dart';
import 'package:open_filex/open_filex.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../core/app_update.dart';

/// 检查更新 bottom sheet — Android only (iOS has no sideload channel; the
/// caller hides the entry there). Mirrors the desktop update dialog: check →
/// offer → download with progress → sha256 verify → hand to the installer.
Future<void> showUpdateSheet(BuildContext context, {AppUpdate? prechecked}) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (context) => _UpdateSheet(prechecked: prechecked),
  );
}

class _UpdateSheet extends StatefulWidget {
  /// A silent background check may already have the result — skip the fetch.
  final AppUpdate? prechecked;

  const _UpdateSheet({this.prechecked});

  @override
  State<_UpdateSheet> createState() => _UpdateSheetState();
}

enum _Phase { checking, idle, upToDate, available, downloading, ready, error }

class _UpdateSheetState extends State<_UpdateSheet> {
  final _updater = AppUpdater();
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
              : '无法连接更新服务器';
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
              ? '当前已是最新版本（${result.latestVersion}）'
              : '当前已是最新版本';
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
    final result = await OpenFilex.open(
      apk.path,
      type: 'application/vnd.android.package-archive',
    );
    if (!mounted) return;
    // INSTALL_FAILED / user denied the unknown-sources prompt — surface it.
    if (result.type != ResultType.done) {
      setState(() {
        _detail = result.message.isNotEmpty ? result.message : '无法打开安装器';
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
            '检查更新',
            textAlign: TextAlign.center,
            style: theme.textTheme.titleLarge,
          ),
          const SizedBox(height: 4),
          Text(
            _currentVersion.isEmpty ? 'MaClawGo' : '当前版本 $_currentVersion',
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
                  '发现新版本 ${_update!.version}',
                  style: theme.textTheme.titleMedium,
                ),
                const SizedBox(height: 6),
                Text(
                  '将从 GitHub / Cloudflare R2 下载 $updateAssetName，'
                  '校验后由系统安装器完成安装。',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: 16),
                FilledButton.icon(
                  onPressed: _download,
                  icon: const Icon(Icons.download),
                  label: const Text('下载并安装'),
                ),
                TextButton(
                  onPressed: () => launchUrl(Uri.parse(_update!.releaseUrl)),
                  child: const Text('在浏览器中查看'),
                ),
              ],
            ),
            _Phase.downloading => Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                LinearProgressIndicator(value: _progress),
                const SizedBox(height: 8),
                Text(
                  '下载中 ${(100 * _progress).toStringAsFixed(0)}%',
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
                const Text('已下载，正在打开安装器…'),
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
                  _detail.isEmpty ? '检查更新失败' : _detail,
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodyMedium,
                ),
                const SizedBox(height: 12),
                FilledButton.tonal(onPressed: _check, child: const Text('重试')),
              ],
            ),
          },
          const SizedBox(height: 8),
        ],
      ),
    );
  }
}
