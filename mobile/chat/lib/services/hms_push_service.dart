import 'dart:io';
import 'package:flutter/services.dart';
import 'api_client.dart';

/// HMS Push Kit integration for HarmonyOS devices.
/// Uses a platform channel to interact with the native HMS Push SDK.
/// On non-OHOS platforms, this is a no-op.
class HmsPushService {
  static const _channel = MethodChannel('com.maclaw.chat/hms_push');

  final ApiClient api;
  void Function(String channelId)? onNotificationTap;

  HmsPushService({required this.api});

  /// Whether we should use HMS Push (HarmonyOS device).
  static bool get isHmsDevice => !Platform.isAndroid && !Platform.isIOS;

  /// Initialize HMS Push and register token.
  Future<void> initialize() async {
    if (!isHmsDevice) return;

    // Listen for push events from native side.
    _channel.setMethodCallHandler(_handleNativeCall);

    // Request push token from HMS.
    try {
      final token = await _channel.invokeMethod<String>('getToken');
      if (token != null) {
        await _registerToken(token);
      }
    } catch (_) {
      // HMS Push not available — silently fail.
    }
  }

  Future<void> _registerToken(String token) async {
    try {
      await api.registerPushToken('hms', token);
    } catch (_) {
      // Best-effort registration.
    }
  }

  Future<dynamic> _handleNativeCall(MethodCall call) async {
    switch (call.method) {
      case 'onTokenRefresh':
        final token = call.arguments as String?;
        if (token != null) await _registerToken(token);
        break;

      case 'onMessageReceived':
        // Foreground push — handled by WS, ignore.
        break;

      case 'onNotificationTap':
        final data = call.arguments as Map<dynamic, dynamic>?;
        final channelId = data?['channel_id'] as String?;
        if (channelId != null) onNotificationTap?.call(channelId);
        break;
    }
  }
}
