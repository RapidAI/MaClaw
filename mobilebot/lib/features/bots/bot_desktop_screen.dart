import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart'
    show DeviceOrientation, SystemChrome, SystemUiMode;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:webview_flutter/webview_flutter.dart';
import 'package:webview_flutter_android/webview_flutter_android.dart';

import 'bot_api.dart';
import '../../core/locale.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart' show BrandMark, EmptyState;
import 'bot_models.dart';
import 'bots_controller.dart';
import 'desktop_wake_text.dart';

/// 电脑画面 — the mobile counterpart of the desktop stage in the GUI's
/// DesktopBotWorkspace. The watch hold pins the bot's desktop while the page
/// is open; 唤醒 starts an idle one; 交还 hands it back to the bot.
class BotDesktopScreen extends ConsumerStatefulWidget {
  final Bot bot;

  /// 进入即申请键盘 — 聊天里的「接管」按钮带 takeover=1 跳转过来。
  final bool takeover;

  const BotDesktopScreen({super.key, required this.bot, this.takeover = false});

  @override
  ConsumerState<BotDesktopScreen> createState() => _BotDesktopScreenState();
}

class _BotDesktopScreenState extends ConsumerState<BotDesktopScreen>
    with WidgetsBindingObserver {
  static const _holdRefresh = Duration(seconds: 45);

  /// The hub's keyboard claim lapses after 20s (desktopControlTTL) — renew
  /// well inside it while the takeover switch is on.
  static const _claimRefresh = Duration(seconds: 10);

  /// One generation per open — a clock-based counter like the GUI's
  /// takeDesktopWatchEpoch: Hub compares epochs across every client of this
  /// user, so a random small int always loses to the GUI's Date.now() epochs
  /// and the watch gets silently dropped.
  static int _nextEpoch = DateTime.now().millisecondsSinceEpoch;

  final int _epoch = ++_nextEpoch;
  Timer? _holdTimer;
  Timer? _loadGuardTimer;
  Timer? _claimTimer;
  bool _claiming = false;

  BotDesktop _desktop = const BotDesktop();
  bool _waking = false;
  String _error = '';
  int _reloadTick = 0;

  /// Watch-only by default; 接管 flips to interactive when Hub allows it.
  bool _controlling = false;

  /// h264 like the desktop default; 'vnc' falls back to the noVNC page.
  String _viewer = 'h264';

  /// One auto-claim after entering via the chat's 接管 button, attempted on
  /// the first watch that reports a controllable desktop. Cleared once the
  /// claim is attempted: a refusal (another device holds the keyboard) shows
  /// the busy snackbar once and must not retry every watch refresh.
  bool _takeoverPending = false;

  bool get _noComputer => widget.bot.desktopMode == 'none';

  @override
  void initState() {
    super.initState();
    if (_noComputer) return;
    // 电脑画面横屏看 — desktop aspect ratios waste portrait; restore on exit.
    SystemChrome.setPreferredOrientations([
      DeviceOrientation.landscapeLeft,
      DeviceOrientation.landscapeRight,
    ]);
    // 全屏画面：系统状态栏也收起，sticky 让首次触摸后自动恢复隐藏。
    SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky);
    _takeoverPending = widget.takeover;
    WidgetsBinding.instance.addObserver(this);
    _loadViewerPref();
    _hold(wake: false);
  }

  @override
  void dispose() {
    if (!_noComputer) {
      SystemChrome.setPreferredOrientations([DeviceOrientation.portraitUp]);
      SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
      WidgetsBinding.instance.removeObserver(this);
      _holdTimer?.cancel();
      _loadGuardTimer?.cancel();
      _claimTimer?.cancel();
      // Let the desktop idle-sleep once nobody is watching.
      unawaited(_release());
      if (_controlling) unawaited(_releaseControl());
    }
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (_noComputer) return;
    if (state == AppLifecycleState.resumed && mounted) {
      _hold();
      return;
    }
    // Backgrounded: stop renewing the watch hold — the server-side TTL
    // lapses on its own and the desktop can idle-sleep. A phone in a
    // pocket must not pin the screen awake (or keep streaming video).
    if (state == AppLifecycleState.paused ||
        state == AppLifecycleState.hidden ||
        state == AppLifecycleState.detached) {
      _holdTimer?.cancel();
      _claimTimer?.cancel();
      if (_controlling) {
        _pausedWithControl = true;
        // Drop the keyboard hold right away instead of letting another
        // device wait out the 20s TTL; on resume _hold() re-claims.
        unawaited(_releaseControl());
      }
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
          _error = context.S.dtWakeTimeout;
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
      _reconcileControl(desktop.inputGranted);
      if (_takeoverPending && desktop.isRunning && desktop.userControl) {
        _takeoverPending = false;
        unawaited(_setControlling(true));
      }
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

  /// True while the takeover switch is on but we released the claim for a
  /// background pause — the next reconcile grabs it back silently instead
  /// of reporting a loss we caused ourselves.
  bool _pausedWithControl = false;

  /// Release this device's keyboard claim — best-effort like the watch
  /// release; the 20s TTL covers a dropped call.
  Future<void> _releaseControl() async {
    try {
      await _api?.setDesktopControl(widget.bot.id, hold: false);
    } on Object {
      // Best-effort — the claim lapses on its own.
    }
  }

  /// The takeover switch is a claim, not a local flag: ask the hub for the
  /// keyboard and only flip on when this device actually got it.
  Future<void> _setControlling(bool value) async {
    final api = _api;
    if (api == null || _claiming) return;
    if (!value) {
      _claimTimer?.cancel();
      _pausedWithControl = false;
      setState(() => _controlling = false);
      unawaited(_releaseControl());
      return;
    }
    setState(() => _claiming = true);
    try {
      final granted = await api.setDesktopControl(widget.bot.id, hold: true);
      if (!mounted) return;
      if (granted) {
        setState(() => _controlling = true);
        _scheduleClaim();
      } else {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.S.dtBusyOther)));
      }
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    } finally {
      if (mounted) setState(() => _claiming = false);
    }
  }

  void _scheduleClaim() {
    _claimTimer?.cancel();
    _claimTimer = Timer(_claimRefresh, _renewClaim);
  }

  /// Re-claims inside the TTL. A refusal means another device took the
  /// keyboard since — hand the switch back to watch-only.
  Future<void> _renewClaim() async {
    if (!_controlling) return;
    final granted = await _grabControl();
    if (!mounted || granted != false) return;
    setState(() => _controlling = false);
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(context.S.dtHandedOver)));
  }

  /// One claim round-trip — shared by the switch, the renewal tick and the
  /// resume path. Returns null when the request itself failed (transient
  /// network): the switch then stays on and the next tick retries, since a
  /// dropped packet is not another machine taking the keyboard.
  Future<bool?> _grabControl() async {
    final api = _api;
    if (api == null) return null;
    try {
      final granted = await api.setDesktopControl(widget.bot.id, hold: true);
      if (_controlling) _scheduleClaim();
      return granted;
    } on Object {
      if (_controlling) _scheduleClaim();
      return null;
    }
  }

  /// Every watch refresh reports who holds the keyboard. A lost grant while
  /// the switch is on is either another device claiming it or our own claim
  /// lapsing (pause release, a missed renewal) — try one silent re-grab and
  /// only a real refusal hands the switch back to watch-only.
  void _reconcileControl(bool inputGranted) {
    if (!_controlling || inputGranted) {
      if (_controlling && _claimTimer == null) _scheduleClaim();
      return;
    }
    final wasPaused = _pausedWithControl;
    _pausedWithControl = false;
    unawaited(
      _grabControl().then((granted) {
        if (!mounted || granted != false) return;
        setState(() => _controlling = false);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              wasPaused ? context.S.dtBusyOther : context.S.dtHandedOver,
            ),
          ),
        );
      }),
    );
  }

  String _wakeFailureText(Object error) {
    final named = desktopWakeFailureText(error);
    if (named != error.toString()) return named;
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
    final viewOnly =
        query
            .split('&')
            .where((part) => part.startsWith('view_only='))
            .firstOrNull ??
        'view_only=1';
    // Watch-only on a phone gets a narrower, slower stream — the full
    // 1440px/15fps feed saturates a mobile link, so the server drops
    // delta frames and the picture fills in bit by bit between keyframes.
    // Takeover keeps the native stream for precision.
    final stream = viewOnly == 'view_only=1' ? '&w=960&fps=10' : '';
    // Android WebView rasterizes canvas.drawImage(VideoFrame) through a
    // 16-bit ordered-dither path (halftone dots) — force the fmp4 <video>
    // pipeline instead, which composites in hardware.
    final fallback = 'vnc.html${query.isNotEmpty ? '?$query' : ''}';
    return '${path.substring(0, slash + 1)}h264.html?$viewOnly$stream&mse=1&vnc=${Uri.encodeComponent(fallback)}';
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
      src = src.replaceAll(RegExp(r'view_only=\d'), 'view_only=$viewOnly');
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
            'proxy' => context.S.dtProxyAlways,
            'direct' => context.S.dtProxyDirect,
            'smart' || '' => context.S.dtProxySmart,
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
                messenger.showSnackBar(
                  SnackBar(
                    content: Text(
                      next.warning.isNotEmpty
                          ? next.warning
                          : context.S.dtSavedNextOpen,
                    ),
                  ),
                );
              }
            } on Object catch (error) {
              messenger.showSnackBar(
                SnackBar(content: Text(failureMessage(error))),
              );
            }
          }

          Future<void> computerAction(String action, String title) async {
            final S = context.S;
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
                      Text(
                        action == 'reset'
                            ? context.S.dtResetWarning
                            : context.S.dtRestartWarning,
                      ),
                      CheckboxListTile(
                        value: force,
                        onChanged: (v) => setDialog(() => force = v ?? false),
                        title: Text(context.S.dtForceSkip),
                        controlAffinity: ListTileControlAffinity.leading,
                        contentPadding: EdgeInsets.zero,
                      ),
                    ],
                  ),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: Text(context.S.cancel),
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
              final result = await api.desktopComputerAction(
                action,
                force: force,
              );
              messenger.showSnackBar(
                SnackBar(
                  content: Text(
                    result.status.isNotEmpty
                        ? '$title：${result.status}'
                        : S.dtActionDone(title),
                  ),
                ),
              );
              if (context.mounted) Navigator.pop(context);
            } on Object catch (error) {
              messenger.showSnackBar(
                SnackBar(content: Text(failureMessage(error))),
              );
            }
          }

          return SafeArea(
            child: ListView(
              shrinkWrap: true,
              padding: const EdgeInsets.fromLTRB(
                AppSpacing.xl,
                0,
                AppSpacing.xl,
                AppSpacing.xxl,
              ),
              children: [
                Text(
                  context.S.dtProxyMode,
                  style: Theme.of(context).textTheme.titleSmall,
                ),
                if (proxy.locked)
                  Padding(
                    padding: const EdgeInsets.only(
                      top: AppSpacing.xs,
                      bottom: AppSpacing.xs,
                    ),
                    child: Text(context.S.dtProxyLocked),
                  ),
                RadioGroup<String>(
                  groupValue: proxy.userMode,
                  onChanged: proxy.locked ? (_) {} : pickProxy,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      for (final mode in const ['', 'smart', 'direct', 'proxy'])
                        RadioListTile<String>(
                          value: mode,
                          enabled: !proxy.locked,
                          title: Text(
                            mode.isEmpty
                                ? context.S.dtProxyFollowTenant(
                                    proxyLabel(
                                      proxy.tenantDefault.isEmpty
                                          ? 'smart'
                                          : proxy.tenantDefault,
                                    ),
                                  )
                                : proxyLabel(mode),
                          ),
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                        ),
                    ],
                  ),
                ),
                const Divider(height: AppSpacing.xxl),
                Text(
                  context.S.dtMaintenance,
                  style: Theme.of(context).textTheme.titleSmall,
                ),
                const SizedBox(height: AppSpacing.xs),
                Wrap(
                  spacing: AppSpacing.sm,
                  runSpacing: AppSpacing.sm,
                  children: [
                    FilledButton.tonalIcon(
                      onPressed: caps.restartAllowed
                          ? () => computerAction('restart', context.S.dtRestart)
                          : null,
                      icon: const Icon(Icons.restart_alt),
                      label: Text(context.S.dtRestart),
                    ),
                    FilledButton.tonalIcon(
                      onPressed: caps.resetAllowed
                          ? () => computerAction('reset', context.S.dtReset)
                          : null,
                      icon: const Icon(Icons.restore),
                      label: Text(context.S.dtReset),
                    ),
                  ],
                ),
                if (!caps.restartAllowed && !caps.resetAllowed)
                  Padding(
                    padding: const EdgeInsets.only(top: AppSpacing.xs),
                    child: Text(
                      context.S.dtMaintenanceUnavailable,
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
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
    if (_noComputer) {
      return Scaffold(
        appBar: AppBar(
          title: Row(
            children: [
              const BrandMark(size: 26, tile: true),
              const SizedBox(width: AppSpacing.sm + 2),
              Flexible(
                child: Text(
                  context.S.dtTitle(widget.bot.title),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
        ),
        body: EmptyState(
          icon: Icons.desktop_access_disabled_outlined,
          title: context.S.dtNoComputer,
        ),
      );
    }
    final theme = Theme.of(context);
    final url = _watchUrl();
    // 横屏顶栏不占垂直空间：画面铺满全屏，操作按钮半透明悬浮在左右两侧
    // （电脑画面居中显示，两侧是黑边）。
    return Scaffold(
      body: Stack(
        children: [
          Positioned.fill(child: _buildBody(url, theme)),
          if (_desktop.attentionReason.isNotEmpty)
            Positioned(
              top: 8,
              left: 56,
              right: 56,
              child: IgnorePointer(
                child: Center(
                  child: Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 6,
                    ),
                    decoration: BoxDecoration(
                      color: theme.colorScheme.tertiaryContainer.withValues(
                        alpha: 0.85,
                      ),
                      borderRadius: BorderRadius.circular(18),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(
                          Icons.notifications_active_outlined,
                          size: 14,
                          color: theme.colorScheme.onTertiaryContainer,
                        ),
                        const SizedBox(width: 6),
                        Flexible(
                          child: Text(
                            attentionReasonLabel(
                              _desktop.attentionReason,
                              context.S,
                            ),
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.onTertiaryContainer,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          Align(
            alignment: Alignment.centerLeft,
            child: Padding(
              padding: const EdgeInsets.only(left: 8),
              child: _overlayButton(
                icon: Icons.arrow_back,
                tooltip: context.S.dtBack,
                // 冷启动直达本页（通知深链）时没有路由可弹，回 bot 页兜底，
                // 不然用户会困在横屏全屏里。
                onPressed: () {
                  if (Navigator.of(context).canPop()) {
                    Navigator.of(context).pop();
                  } else {
                    context.go(
                      '/bot/${Uri.encodeComponent(widget.bot.id)}',
                    );
                  }
                },
              ),
            ),
          ),
          Align(
            alignment: Alignment.centerRight,
            child: Padding(
              padding: const EdgeInsets.only(right: 8),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  _overlayButton(
                    icon: _controlling
                        ? Icons.touch_app
                        : Icons.touch_app_outlined,
                    tooltip: _controlling
                        ? context.S.dtControlling
                        : context.S.dtWatching,
                    active: _controlling,
                    onPressed:
                        _desktop.isRunning && _desktop.userControl && !_claiming
                        ? () => _setControlling(!_controlling)
                        : null,
                  ),
                  const SizedBox(height: 12),
                  Container(
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      color: theme.colorScheme.surface.withValues(alpha: 0.55),
                    ),
                    child: PopupMenuButton<String>(
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
                            final S = context.S;
                            final messenger = ScaffoldMessenger.of(context);
                            try {
                              await _api?.handBackDesktop(widget.bot.id);
                              messenger.showSnackBar(
                                SnackBar(content: Text(S.dtHandedBack)),
                              );
                            } on Object catch (error) {
                              messenger.showSnackBar(
                                SnackBar(content: Text(failureMessage(error))),
                              );
                            }
                          case 'computer':
                            _computerSheet();
                        }
                      },
                      itemBuilder: (context) => [
                        PopupMenuItem(
                          value: 'refresh',
                          child: Text(context.S.refresh),
                        ),
                        PopupMenuItem(
                          value: 'viewer',
                          child: Text(
                            _viewer == 'h264'
                                ? context.S.dtSwitchToVnc
                                : context.S.dtSwitchToH264,
                          ),
                        ),
                        PopupMenuItem(
                          value: 'handback',
                          child: Text(context.S.dtHandback),
                        ),
                        PopupMenuItem(
                          value: 'computer',
                          child: Text(context.S.dtComputerMaintenance),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  /// 半透明圆形悬浮按钮 — 悬在画面两侧的黑边上，不占画面空间。
  Widget _overlayButton({
    required IconData icon,
    required String tooltip,
    required VoidCallback? onPressed,
    bool active = false,
  }) {
    final scheme = Theme.of(context).colorScheme;
    return IconButton(
      onPressed: onPressed,
      tooltip: tooltip,
      style: IconButton.styleFrom(
        backgroundColor: scheme.surface.withValues(alpha: 0.55),
        foregroundColor: active ? scheme.primary : scheme.onSurface,
      ),
      icon: Icon(icon),
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
    return EmptyState(
      icon: Icons.desktop_access_disabled_outlined,
      title: _waking
          ? context.S.dtWaking
          : _error.isNotEmpty
          ? _error
          : context.S.dtNoPicture,
      action: _waking
          ? const SizedBox(
              width: 24,
              height: 24,
              child: CircularProgressIndicator(strokeWidth: 2.4),
            )
          : FilledButton.icon(
              onPressed: () => _hold(wake: true),
              icon: const Icon(Icons.power_settings_new),
              label: Text(context.S.dtWake),
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
  late final Widget _webView;
  double _progress = 0;
  String _loadError = '';

  // ── Pinch zoom ──────────────────────────────────────────────────────────
  // Raw Listener, NOT InteractiveViewer: the viewer's scale recognizer wins
  // the gesture arena on one-finger drags too (it returns early for disabled
  // pan, but the platform view still gets a cancel) — that would kill the
  // single-finger remote mouse. A Listener never claims the arena, so
  // one-finger events reach the page untouched while two fingers drive the
  // transform below.
  final _zoom = TransformationController();
  final _pointers = <int, Offset>{};
  bool _pinching = false;
  Offset _pinchFocalStart = Offset.zero;
  double _pinchDistanceStart = 1;
  Matrix4 _pinchMatrixStart = Matrix4.identity();
  Size _viewportSize = Size.zero;

  static const _minScale = 1.0;
  static const _maxScale = 4.0;

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
              setState(
                () => _loadError = context.S.dtLoadFailed(error.errorCode),
              );
            }
          },
        ),
      )
      ..loadRequest(Uri.parse(widget.url));
    _webView = _buildWebView();
  }

  @override
  void dispose() {
    _zoom.dispose();
    super.dispose();
  }

  /// The default SurfaceTexture platform-view path re-rasterizes WebView
  /// content through a GL texture — on some devices that samples at reduced
  /// depth and shows up as a halftone dot grid over the whole picture.
  /// Hybrid composition embeds the real Android WebView so it composites
  /// straight to the screen in hardware.
  Widget _buildWebView() {
    PlatformWebViewWidgetCreationParams params =
        PlatformWebViewWidgetCreationParams(
          controller: _controller.platform,
          layoutDirection: TextDirection.ltr,
          gestureRecognizers: const <Factory<OneSequenceGestureRecognizer>>{},
        );
    if (WebViewPlatform.instance is AndroidWebViewPlatform) {
      params =
          AndroidWebViewWidgetCreationParams.fromPlatformWebViewWidgetCreationParams(
            params,
            displayWithHybridComposition: true,
          );
    }
    return WebViewWidget.fromPlatformCreationParams(params: params);
  }

  void _onPointerDown(PointerDownEvent event) {
    _pointers[event.pointer] = event.localPosition;
    if (_pointers.length == 2) _beginPinch();
  }

  void _onPointerMove(PointerMoveEvent event) {
    if (!_pointers.containsKey(event.pointer)) return;
    _pointers[event.pointer] = event.localPosition;
    if (_pointers.length >= 2) {
      if (!_pinching) _beginPinch();
      _updatePinch();
    }
  }

  void _onPointerEnd(PointerEvent event) {
    _pointers.remove(event.pointer);
    if (_pointers.length < 2) _pinching = false;
  }

  void _beginPinch() {
    final pts = _pointers.values.toList();
    if (pts.length < 2) return;
    _pinching = true;
    _pinchFocalStart = (pts[0] + pts[1]) / 2;
    _pinchDistanceStart = (pts[0] - pts[1]).distance;
    _pinchMatrixStart = _zoom.value.clone();
  }

  /// M' = T(focal_now) · S(s) · T(−focal_start) · M_start — the fingers'
  /// midpoint stays glued to the same scene point while scale follows the
  /// finger distance; translation is clamped so the scaled picture keeps
  /// covering the viewport.
  void _updatePinch() {
    if (!_pinching || _pointers.length < 2) return;
    final pts = _pointers.values.toList();
    final dist = (pts[0] - pts[1]).distance;
    if (dist == 0 || _viewportSize == Size.zero) return;
    final scaleStart = _pinchMatrixStart.getMaxScaleOnAxis();
    final scale = (scaleStart * dist / _pinchDistanceStart).clamp(
      _minScale,
      _maxScale,
    );
    final focal = (pts[0] + pts[1]) / 2;
    final matrix = Matrix4.identity()
      ..translateByDouble(focal.dx, focal.dy, 0, 1)
      ..scaleByDouble(scale / scaleStart, scale / scaleStart, 1, 1)
      ..translateByDouble(-_pinchFocalStart.dx, -_pinchFocalStart.dy, 0, 1)
      ..multiply(_pinchMatrixStart);
    var translation = matrix.getTranslation();
    final maxX = 0.0;
    final minX = _viewportSize.width * (1 - scale);
    final maxY = 0.0;
    final minY = _viewportSize.height * (1 - scale);
    translation.x = translation.x.clamp(minX, maxX);
    translation.y = translation.y.clamp(minY, maxY);
    matrix.setTranslationRaw(translation.x, translation.y, 0);
    _zoom.value = matrix;
  }

  void _resetZoom() {
    _zoom.value = Matrix4.identity();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Stack(
      fit: StackFit.expand,
      children: [
        Listener(
          onPointerDown: _onPointerDown,
          onPointerMove: _onPointerMove,
          onPointerUp: _onPointerEnd,
          onPointerCancel: _onPointerEnd,
          child: ClipRect(
            child: LayoutBuilder(
              builder: (context, constraints) {
                _viewportSize = constraints.biggest;
                return AnimatedBuilder(
                  animation: _zoom,
                  builder: (context, child) =>
                      Transform(transform: _zoom.value, child: child),
                  child: SizedBox.expand(child: _webView),
                );
              },
            ),
          ),
        ),
        if (_progress < 1 && _loadError.isEmpty)
          LinearProgressIndicator(value: _progress > 0 ? _progress : null),
        AnimatedBuilder(
          animation: _zoom,
          builder: (context, _) {
            final scale = _zoom.value.getMaxScaleOnAxis();
            return scale > _minScale + 0.05
                ? Align(
                    alignment: Alignment.topRight,
                    child: Padding(
                      padding: const EdgeInsets.all(10),
                      child: Material(
                        color: scheme.inverseSurface.withValues(alpha: 0.85),
                        borderRadius: BorderRadius.circular(20),
                        child: InkWell(
                          borderRadius: BorderRadius.circular(20),
                          onTap: _resetZoom,
                          child: Padding(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 10,
                              vertical: 6,
                            ),
                            child: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Icon(
                                  Icons.close_fullscreen,
                                  size: 14,
                                  color: scheme.onInverseSurface,
                                ),
                                const SizedBox(width: 4),
                                Text(
                                  '${scale.toStringAsFixed(1)}×',
                                  style: TextStyle(
                                    fontSize: 12,
                                    fontWeight: FontWeight.w600,
                                    color: scheme.onInverseSurface,
                                  ),
                                ),
                              ],
                            ),
                          ),
                        ),
                      ),
                    ),
                  )
                : const SizedBox.shrink();
          },
        ),
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
                  child: Text(context.S.dtReload),
                ),
              ],
            ),
          ),
      ],
    );
  }
}
