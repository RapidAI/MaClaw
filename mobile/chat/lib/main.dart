import 'package:flutter/material.dart';
import 'package:firebase_core/firebase_core.dart';
import 'package:provider/provider.dart';
import 'providers/call_provider.dart';
import 'providers/chat_provider.dart';
import 'providers/theme_provider.dart';
import 'screens/auth/login_screen.dart';
import 'screens/home_screen.dart';
import 'screens/voice/call_screen.dart';
import 'services/api_client.dart';
import 'services/auth_service.dart';
import 'services/hub_discovery.dart';
import 'services/push_service.dart';
import 'services/webrtc_service.dart';
import 'services/ws_client.dart';
import 'services/sync_service.dart';
import 'services/local_database.dart';
import 'theme.dart';

final _navKey = GlobalKey<NavigatorState>();
final _themeProvider = ThemeProvider();

void main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await Firebase.initializeApp();
  runApp(const MaClawChatApp());
}

class MaClawChatApp extends StatefulWidget {
  const MaClawChatApp({super.key});

  @override
  State<MaClawChatApp> createState() => _MaClawChatAppState();
}

class _MaClawChatAppState extends State<MaClawChatApp> {
  /// Hub URL discovered at runtime via HubCenter or direct probe.
  String? _hubUrl;

  /// Build WebSocket URL from hub base URL.
  /// Handles both http→ws and https→wss correctly.
  String get _wsUrl {
    final base = _hubUrl ?? '';
    final wsBase = base.startsWith('https://')
        ? base.replaceFirst('https://', 'wss://')
        : base.replaceFirst('http://', 'ws://');
    return '$wsBase/api/chat/ws';
  }

  AuthService? _auth;
  ApiClient? _api;
  late final LocalDatabase _db;
  WsClient? _ws;
  SyncService? _sync;
  ChatProvider? _chatProvider;
  CallProvider? _callProvider;
  WebRTCService? _webrtc;
  PushService? _push;

  bool _initializing = true;
  bool _loggedIn = false;

  @override
  void initState() {
    super.initState();
    _db = LocalDatabase();
    _tryRestoreSession();
  }

  Future<void> _tryRestoreSession() async {
    try {
      final savedHub = await HubDiscovery.loadHubUrl();
      if (savedHub != null && savedHub.isNotEmpty) {
        _setHubUrl(savedHub);
        final restored = await _auth!.restoreSession();
        if (!mounted) return;
        if (restored) {
          _onAuthenticated();
        } else {
          // Token invalid — clear it so we don't retry stale tokens.
          await _auth!.logout();
        }
      }
    } catch (_) {
      // Network error during restore — fall through to login screen.
    }
    if (mounted) setState(() => _initializing = false);
  }

  void _setHubUrl(String hubUrl) {
    _hubUrl = hubUrl.replaceAll(RegExp(r'/+$'), '');
    _auth = AuthService(baseUrl: _hubUrl!);
    _api = ApiClient(baseUrl: _hubUrl!);
  }

  /// Called by LoginScreen after hub discovery + successful authentication.
  void _onHubReady(String hubUrl, String hubName, AuthService auth) {
    _hubUrl = hubUrl.replaceAll(RegExp(r'/+$'), '');
    _auth = auth;
    _api = ApiClient(baseUrl: _hubUrl!);
  }

  void _onAuthenticated() {
    if (_api == null || _auth == null) return;
    _api!.setToken(_auth!.token!);

    _ws = WsClient(wsUrl: _wsUrl, tokenProvider: () => _auth?.token ?? '');
    _sync = SyncService(api: _api!, ws: _ws!, db: _db);
    _chatProvider = ChatProvider(
      api: _api!,
      sync: _sync!,
      currentUserId: _auth!.userId!,
    );

    _webrtc = WebRTCService(api: _api!);
    _callProvider = CallProvider(webrtc: _webrtc!, ws: _ws!);
    _callProvider!.addListener(_onCallStateChanged);

    _ws!.connect();
    _sync!.start();

    _push = PushService(api: _api!);
    _push!.onNotificationTap = _onPushNotificationTap;
    _push!.initialize();

    setState(() => _loggedIn = true);
  }

  void _onCallStateChanged() {
    if (_callProvider?.state == CallState.incoming) {
      final ctx = _navKey.currentContext;
      if (ctx != null) CallScreen.show(ctx);
    }
  }

  void _onPushNotificationTap(String channelId) {}

  void _onLogout() {
    _callProvider?.removeListener(_onCallStateChanged);
    _ws?.dispose();
    _sync?.dispose();
    _chatProvider?.dispose();
    _callProvider?.dispose();
    _ws = null;
    _sync = null;
    _chatProvider = null;
    _callProvider = null;
    _webrtc = null;
    _push = null;
    _auth?.logout();
    HubDiscovery.clearHub();
    setState(() {
      _loggedIn = false;
      _hubUrl = null;
      _auth = null;
      _api = null;
    });
  }

  @override
  void dispose() {
    _callProvider?.removeListener(_onCallStateChanged);
    _ws?.dispose();
    _sync?.dispose();
    _callProvider?.dispose();
    _db.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return ChangeNotifierProvider.value(
      value: _themeProvider,
      child: Consumer<ThemeProvider>(
        builder: (context, theme, _) => MaterialApp(
          navigatorKey: _navKey,
          title: 'MaClaw Chat',
          debugShowCheckedModeBanner: false,
          theme: AppTheme.light,
          darkTheme: AppTheme.dark,
          themeMode: theme.mode,
          home: _buildHome(),
        ),
      ),
    );
  }

  Widget _buildHome() {
    if (_initializing) {
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }
    if (!_loggedIn) {
      return LoginScreen(
        onLoginSuccess: _onAuthenticated,
        onHubReady: _onHubReady,
      );
    }
    return MultiProvider(
      providers: [
        ChangeNotifierProvider.value(value: _chatProvider!),
        ChangeNotifierProvider.value(value: _callProvider!),
        ChangeNotifierProvider.value(value: _themeProvider),
      ],
      child: HomeScreen(api: _api!),
    );
  }
}
