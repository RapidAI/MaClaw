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
  static const _hubUrl = 'http://localhost:9388';
  static const _wsUrl = 'ws://localhost:9388/api/chat/ws';

  late final AuthService _auth;
  late final ApiClient _api;
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
    _auth = AuthService(baseUrl: _hubUrl);
    _api = ApiClient(baseUrl: _hubUrl);
    _db = LocalDatabase();
    _tryRestoreSession();
  }

  Future<void> _tryRestoreSession() async {
    final restored = await _auth.restoreSession();
    if (!mounted) return;
    if (restored) {
      _onAuthenticated();
    }
    setState(() => _initializing = false);
  }

  void _onAuthenticated() {
    _api.setToken(_auth.token!);

    _ws = WsClient(wsUrl: _wsUrl, tokenProvider: () => _auth.token ?? '');
    _sync = SyncService(api: _api, ws: _ws!, db: _db);
    _chatProvider = ChatProvider(
      api: _api,
      sync: _sync!,
      currentUserId: _auth.userId!,
    );

    _webrtc = WebRTCService(api: _api);
    _callProvider = CallProvider(webrtc: _webrtc!, ws: _ws!);

    // Listen for incoming calls to auto-show call screen.
    _callProvider!.addListener(_onCallStateChanged);

    _ws!.connect();
    _sync!.start();

    // Initialize push notifications.
    _push = PushService(api: _api);
    _push!.onNotificationTap = _onPushNotificationTap;
    _push!.initialize();

    setState(() => _loggedIn = true);
  }

  void _onCallStateChanged() {
    if (_callProvider?.state == CallState.incoming) {
      final ctx = _navKey.currentContext;
      if (ctx != null) {
        CallScreen.show(ctx);
      }
    }
  }

  void _onPushNotificationTap(String channelId) {
    // Navigate to the chat room for this channel.
    // HomeScreen / ConversationListScreen handles deep-link via this callback.
    // For now, just ensure we're on the home screen — the channel will be
    // opened by the user from the conversation list.
  }

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
    _auth.logout();
    setState(() => _loggedIn = false);
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
      return const Scaffold(
        body: Center(child: CircularProgressIndicator()),
      );
    }
    if (!_loggedIn) {
      return LoginScreen(
        auth: _auth,
        onLoginSuccess: _onAuthenticated,
      );
    }
    return MultiProvider(
      providers: [
        ChangeNotifierProvider.value(value: _chatProvider!),
        ChangeNotifierProvider.value(value: _callProvider!),
        ChangeNotifierProvider.value(value: _themeProvider),
      ],
      child: HomeScreen(api: _api),
    );
  }
}
