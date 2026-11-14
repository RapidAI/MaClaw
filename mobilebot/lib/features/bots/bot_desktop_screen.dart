import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:webview_flutter/webview_flutter.dart';

import 'bot_api.dart';
import 'bot_models.dart';
import 'bots_controller.dart';

/// 电脑画面 — the mobile counterpart of the desktop stage in the GUI's
/// DesktopBotWorkspace. The watch hold pins the bot's desktop while the page
/// is open; 唤醒 starts an idle one; 交还 hands it back to the bot.
class BotDesktopScreen extends ConsumerStatefulWidget {
  final Bot bot;

  const BotDesktopScreen({super.key, required this.bot});

  @override
  ConsumerState<BotDesktopScreen> createState() => _BotDesktopScreenState();
}

class _BotDesktopScreenState extends ConsumerState<BotDesktopScreen>
    with WidgetsBindingObserver {
  static const _holdRefresh = Duration(seconds: 45);

  final int _epoch = Random().nextInt(1 << 30);
  Timer? _holdTimer;
  Timer? _loadGuardTimer;

  BotDesktop _desktop = const BotDesktop();
  bool _waking = false;
  String _error = '';
  int _reloadTick = 0;

  /// Watch-only by default; 接管 flips to interactive when Hub allows it.
  bool _controlling = false;

  /// h264 like the desktop default; 'vnc' falls back to the noVNC page.
  String _viewer = 'h264';

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _loadViewerPref();
    _hold(wake: false);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _holdTimer?.cancel();
    _loadGuardTimer?.cancel();
    // Let the desktop idle-sleep once nobody is watching.
    unawaited(_release());
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && mounted) {
      _hold();
    }
  }

  Future<void> _loadViewerPref() async {
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getString('mobilebot.desktop.viewer');
    if (saved == 'vnc' && mounted) setState(() => _viewer = 'vnc');
  }

  Future<void> _saveViewerPref(String viewer) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('mobilebot.desktop.viewer', viewer);
  }

  BotApi? get _api => ref.read(botApiProvider);

  Future<void> _hold({bool wake = false}) async {
    final api = _api;
    if (api == null || !mounted) return;
    if (wake) setState(() => _waking = true);
    _loadGuardTimer?.cancel();
    _loadGuardTimer = Timer(const Duration(seconds: 80), () {
      if (mounted && _waking) {
        setState(() {
          _waking = false;
          _error = '等了一分钟电脑仍没有起来，请稍后再试';
        });
      }
    });
    try {
      final desktop = await api.watchDesktop(
        widget.bot.id,
        epoch: _epoch,
        wake: wake,
      );
      if (!mounted) return;
      _loadGuardTimer?.cancel();
      setState(() {
        _desktop = desktop;
        _waking = false;
        _error = '';
        if (!desktop.userControl) _controlling = false;
        _reloadTick++;
      });
      _scheduleHold();
    } on Object catch (error) {
      if (!mounted) return;
      _loadGuardTimer?.cancel();
      setState(() {
        _waking = false;
        _error = _wakeFailureText(error);
      });
      _scheduleHold();
    }
  }

  void _scheduleHold() {
    _holdTimer?.cancel();
    _holdTimer = Timer(_holdRefresh, () => _hold());
  }

  Future<void> _release() async {
    BotApi? api;
    try {
      // Called from dispose() — WidgetRef may already be torn down.
      api = _api;
    } on Object {
      return;
    }
    if (api == null) return;
    try {
      await api.watchDesktop(widget.bot.id, epoch: _epoch, release: true);
    } on Object {
      // Release is best-effort; the hold expires on its own.
    }
  }

  String _wakeFailureText(Object error) {
    final raw = error.toString().toLowerCase();
    if (raw.contains('no free desktop display')) return '没有空闲的屏幕，请稍后再试';
    if (raw.contains('deadline') || raw.contains('timeout')) {
      return '等了一分钟电脑仍没有起来';
    }
    if (raw.contains('not open yet')) return '电脑还没有打开';
    if (raw.contains('connection refused') ||
        raw.contains('unreachable') ||
        raw.contains('502') ||
        raw.contains('503')) {
      return '电脑服务暂时连不上';
    }
    return failureMessage(error);
  }

  /// Mirrors `h264FrameSrc` in the desktop GUI: swap the noVNC page for the
  /// H.264 player, keeping view_only and naming the noVNC page as fallback.
  String _viewerSrc(String vncSrc) {
    if (vncSrc.isEmpty || _viewer != 'h264') return vncSrc;
    final hashAt = vncSrc.indexOf('#');
    final base = hashAt >= 0 ? vncSrc.substring(0, hashAt) : vncSrc;
    final queryAt = base.indexOf('?');
    final path = queryAt >= 0 ? base.substring(0, queryAt) : base;
    final query = queryAt >= 0 ? base.substring(queryAt + 1) : '';
    final slash = path.lastIndexOf('/');
    if (path.substring(slash + 1) != 'vnc.html') return vncSrc;
    final viewOnly = query
            .split('&')
            .where((part) => part.startsWith('view_only='))
            .firstOrNull ??
        'view_only=1';
    final fallback = 'vnc.html${query.isNotEmpty ? '?$query' : ''}';
    return '${path.substring(0, slash + 1)}h264.html?$viewOnly&vnc=${Uri.encodeComponent(fallback)}';
  }

  String _watchUrl() {
    final api = _api;
    if (api == null || _desktop.novncUrl.isEmpty) return '';
    var src = api.absoluteUrl(_desktop.novncUrl);
    final viewOnly = _controlling && _desktop.userControl ? '0' : '1';
    final separator = src.contains('?') ? '&' : '?';
    if (!src.contains('view_only=')) {
      src = '$src${separator}view_only=$viewOnly';
    } else {
      src = src.replaceAll(
          RegExp(r'view_only=\d'), 'view_only=$viewOnly');
    }
    return _viewerSrc(src);
  }

  /// Maintenance sheet: desktop proxy mode (user-level route) and
  /// restart/reset of the caller's pooled computer.
  Future<void> _computerSheet() async {
    final api = _api;
    if (api == null) return;
    final messenger = ScaffoldMessenger.of(context);
    DesktopComputerCaps caps;
    DesktopProxyMode proxy;
    try {
      caps = await api.desktopComputer();
      proxy = await api.desktopProxyMode();
    } on Object catch (error) {
      messenger.showSnackBar(SnackBar(content: Text(failureMessage(error))));
      return;
    }
    if (!mounted) return;
    await showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (context) => StatefulBuilder(
        builder: (context, setSheet) {
          String proxyLabel(String mode) => switch (mode) {
                'proxy' => '始终代理',
                'direct' => '直连',
                'smart' || '' => '智能（自动选择）',
                _ => mode,
              };

          Future<void> pickProxy(String? mode) async {
            if (mode == null || proxy.locked) return;
            try {
              final next = await api.setDesktopProxyMode(mode);
              setSheet(() {
                proxy = next;
              });
              if (context.mounted && !next.applied) {
                messenger.showSnackBar(SnackBar(
                    content: Text(next.warning.isNotEmpty
                        ? next.warning
                        : '已保存，下次打开画面时生效')));
              }
            } on Object catch (error) {
              messenger.showSnackBar(
                  SnackBar(content: Text(failureMessage(error))));
            }
          }

          Future<void> computerAction(String action, String title) async {
            var force = false;
            final ok = await showDialog<bool>(
              context: context,
              builder: (context) => StatefulBuilder(
                builder: (context, setDialog) => AlertDialog(
                  title: Text(title),
                  content: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(action == 'reset'
                          ? '重置会把电脑恢复到初始镜像，未保存的数据将丢失。'
                          : '重启会中断电脑上正在进行的工作。'),
                      CheckboxListTile(
                        value: force,
                        onChanged: (v) =>
                            setDialog(() => force = v ?? false),
                        title: const Text('强制（跳过优雅停止）'),
                        controlAffinity: ListTileControlAffinity.leading,
                        contentPadding: EdgeInsets.zero,
                      ),
                    ],
                  ),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: const Text('取消'),
                    ),
                    FilledButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: Text(title),
                    ),
                  ],
                ),
              ),
            );
            if (ok != true || !context.mounted) return;
            try {
              final result =
                  await api.desktopComputerAction(action, force: force);
              messenger.showSnackBar(SnackBar(
                  content: Text(result.status.isNotEmpty
                      ? '$title：${result.status}'
                      : '$title完成')));
              if (context.mounted) Navigator.pop(context);
            } on Object catch (error) {
              messenger.showSnackBar(
                  SnackBar(content: Text(failureMessage(error))));
            }
          }

          return SafeArea(
            child: ListView(
              shrinkWrap: true,
              padding: const EdgeInsets.fromLTRB(20, 0, 20, 24),
              children: [
                Text('代理模式',
                    style: Theme.of(context).textTheme.titleSmall),
                if (proxy.locked)
                  const Padding(
                    padding: EdgeInsets.only(top: 4, bottom: 4),
                    child: Text('管理员已锁定代理模式，当前为租户默认。',
                        style: TextStyle(fontSize: 12)),
                  ),
                RadioGroup<String>(
                  groupValue: proxy.userMode,
                  onChanged: proxy.locked ? (_) {} : pickProxy,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      for (final mode
                          in const ['', 'smart', 'direct', 'proxy'])
                        RadioListTile<String>(
                          value: mode,
                          enabled: !proxy.locked,
                          title: Text(mode.isEmpty
                              ? '跟随租户默认（${proxyLabel(proxy.tenantDefault.isEmpty ? 'smart' : proxy.tenantDefault)}）'
                              : proxyLabel(mode)),
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                        ),
                    ],
                  ),
                ),
                const Divider(height: 24),
                Text('维护', style: Theme.of(context).textTheme.titleSmall),
                const SizedBox(height: 4),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    OutlinedButton.icon(
                      onPressed: caps.restartAllowed
                          ? () => computerAction('restart', '重启电脑')
                          : null,
                      icon: const Icon(Icons.restart_alt),
                      label: const Text('重启电脑'),
                    ),
                    OutlinedButton.icon(
                      onPressed: caps.resetAllowed
                          ? () => computerAction('reset', '重置电脑')
                          : null,
                      icon: const Icon(Icons.restore),
                      label: const Text('重置电脑'),
                    ),
                  ],
                ),
                if (!caps.restartAllowed && !caps.resetAllowed)
                  const Padding(
                    padding: EdgeInsets.only(top: 4),
                    child: Text('当前租户未开放电脑维护操作。',
                        style: TextStyle(fontSize: 12)),
                  ),
              ],
            ),
          );
        },
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final url = _watchUrl();
    return Scaffold(
      appBar: AppBar(
        title: Text('${widget.bot.title} 的电脑'),
        actions: [
          PopupMenuButton<String>(
            icon: const Icon(Icons.more_vert),
            onSelected: (value) async {
              switch (value) {
                case 'refresh':
                  _hold();
                case 'viewer':
                  final next = _viewer == 'h264' ? 'vnc' : 'h264';
                  setState(() {
                    _viewer = next;
                    _reloadTick++;
                  });
                  await _saveViewerPref(next);
                case 'handback':
                  final messenger = ScaffoldMessenger.of(context);
                  try {
                    await _api?.handBackDesktop(widget.bot.id);
                    messenger.showSnackBar(
                        const SnackBar(content: Text('已把电脑交还给 Bot')));
                  } on Object catch (error) {
                    messenger.showSnackBar(
                        SnackBar(content: Text(failureMessage(error))));
                  }
                case 'computer':
                  _computerSheet();
              }
            },
            itemBuilder: (context) => [
              const PopupMenuItem(value: 'refresh', child: Text('刷新')),
              PopupMenuItem(
                value: 'viewer',
                child:
                    Text(_viewer == 'h264' ? '切换到 noVNC 画面' : '切换到 H.264 画面'),
              ),
              const PopupMenuItem(value: 'handback', child: Text('交还电脑')),
              const PopupMenuItem(value: 'computer', child: Text('电脑维护…')),
            ],
          ),
        ],
      ),
      body: Column(
        children: [
          if (_desktop.attentionReason.isNotEmpty)
            Container(
              width: double.infinity,
              color: theme.colorScheme.tertiaryContainer,
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
              child: Text(
                _desktop.attentionReason,
                style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onTertiaryContainer),
              ),
            ),
          if (_desktop.isRunning && _desktop.userControl)
            Material(
              color: theme.colorScheme.surfaceContainerHighest,
              child: Padding(
                padding:
                    const EdgeInsets.symmetric(horizontal: 14, vertical: 4),
                child: Row(
                  children: [
                    const Icon(Icons.touch_app_outlined, size: 16),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        _controlling ? '接管中：可以操作这台电脑' : '观看模式（不操作电脑）',
                        style: theme.textTheme.bodySmall,
                      ),
                    ),
                    Switch(
                      value: _controlling,
                      onChanged: (value) =>
                          setState(() => _controlling = value),
                    ),
                  ],
                ),
              ),
            ),
          Expanded(child: _buildBody(url, theme)),
        ],
      ),
    );
  }

  Widget _buildBody(String url, ThemeData theme) {
    if (_desktop.isRunning && url.isNotEmpty) {
      // Keyed so toggling view/control or the player reloads the page.
      return DesktopWebView(
        key: ValueKey('$url#$_reloadTick#$_controlling'),
        url: url,
      );
    }
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.desktop_access_disabled_outlined,
                size: 64, color: Colors.grey.shade400),
            const SizedBox(height: 16),
            Text(
              _waking
                  ? '正在唤醒电脑…'
                  : _error.isNotEmpty
                      ? _error
                      : '电脑还没有画面',
              textAlign: TextAlign.center,
              style: theme.textTheme.bodyLarge,
            ),
            const SizedBox(height: 20),
            if (!_waking)
              FilledButton.icon(
                onPressed: () => _hold(wake: true),
                icon: const Icon(Icons.power_settings_new),
                label: const Text('唤醒电脑'),
              ),
            if (_waking) const CircularProgressIndicator(),
          ],
        ),
      ),
    );
  }
}

/// noVNC / h264.html stage. The page itself translates touch into mouse and
/// keyboard events; the token in the URL is the credential.
class DesktopWebView extends StatefulWidget {
  final String url;

  const DesktopWebView({super.key, required this.url});

  @override
  State<DesktopWebView> createState() => _DesktopWebViewState();
}

class _DesktopWebViewState extends State<DesktopWebView> {
  late final WebViewController _controller;
  double _progress = 0;
  String _loadError = '';

  @override
  void initState() {
    super.initState();
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setBackgroundColor(const Color(0xFF000000))
      ..setNavigationDelegate(
        NavigationDelegate(
          onProgress: (progress) {
            if (mounted) setState(() => _progress = progress / 100);
          },
          onWebResourceError: (error) {
            if (mounted && error.isForMainFrame == true) {
              setState(() => _loadError = '画面加载失败（${error.errorCode}）');
            }
          },
        ),
      )
      ..loadRequest(Uri.parse(widget.url));
  }

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: [
        WebViewWidget(controller: _controller),
        if (_progress < 1 && _loadError.isEmpty)
          LinearProgressIndicator(value: _progress > 0 ? _progress : null),
        if (_loadError.isNotEmpty)
          Center(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(_loadError),
                const SizedBox(height: 12),
                FilledButton.tonal(
                  onPressed: () {
                    setState(() => _loadError = '');
                    _controller.loadRequest(Uri.parse(widget.url));
                  },
                  child: const Text('重新加载'),
                ),
              ],
            ),
          ),
      ],
    );
  }
}
