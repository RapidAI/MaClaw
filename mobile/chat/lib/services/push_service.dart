import 'dart:io';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'api_client.dart';

/// Registers device push tokens with the Hub server and handles
/// incoming push notifications (FCM on Android, APNs on iOS).
class PushService {
  final ApiClient api;
  final FirebaseMessaging _messaging = FirebaseMessaging.instance;

  /// Callback invoked when user taps a notification.
  /// Receives the channel ID to navigate to.
  void Function(String channelId)? onNotificationTap;

  PushService({required this.api});

  /// Initialize push: request permission, get token, register, listen.
  Future<void> initialize() async {
    // Request permission (iOS requires explicit prompt).
    final settings = await _messaging.requestPermission(
      alert: true,
      badge: true,
      sound: true,
    );
    if (settings.authorizationStatus == AuthorizationStatus.denied) return;

    // Get and register the FCM/APNs token.
    final token = await _messaging.getToken();
    if (token != null) {
      await _registerToken(token);
    }

    // Listen for token refresh.
    _messaging.onTokenRefresh.listen(_registerToken);

    // Handle foreground messages.
    FirebaseMessaging.onMessage.listen(_onForegroundMessage);

    // Handle notification tap when app is in background/terminated.
    FirebaseMessaging.onMessageOpenedApp.listen(_onNotificationOpened);

    // Check if app was opened from a terminated state via notification.
    final initial = await _messaging.getInitialMessage();
    if (initial != null) {
      _onNotificationOpened(initial);
    }
  }

  Future<void> _registerToken(String token) async {
    final platform = _detectPlatform();
    try {
      await api.registerPushToken(platform, token);
    } catch (_) {
      // Push registration is best-effort.
    }
  }

  void _onForegroundMessage(RemoteMessage message) {
    // In foreground, we rely on WS for real-time updates.
    // Could show a local notification banner here if desired.
  }

  void _onNotificationOpened(RemoteMessage message) {
    final channelId = message.data['channel_id'] as String?;
    if (channelId != null && onNotificationTap != null) {
      onNotificationTap!(channelId);
    }
  }

  String _detectPlatform() {
    if (Platform.isIOS) return 'apns';
    if (Platform.isAndroid) return 'fcm';
    return 'hms';
  }
}
