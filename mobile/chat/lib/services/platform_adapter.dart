import 'dart:io';
import 'package:flutter/services.dart';

/// Detects the runtime platform and provides OHOS-specific adaptations.
class PlatformAdapter {
  static const _channel = MethodChannel('com.maclaw.chat/platform');

  /// Cached platform detection result.
  static bool? _isOhos;

  /// Whether the app is running on HarmonyOS.
  /// Falls back to checking if it's neither Android nor iOS.
  static Future<bool> isOhos() async {
    if (_isOhos != null) return _isOhos!;
    if (Platform.isAndroid || Platform.isIOS) {
      _isOhos = false;
      return false;
    }
    try {
      _isOhos = await _channel.invokeMethod<bool>('isOhos') ?? true;
    } catch (_) {
      // If channel not available, assume OHOS for non-Android/iOS.
      _isOhos = true;
    }
    return _isOhos!;
  }

  /// Get the OHOS API version for feature gating.
  static Future<int> ohosApiVersion() async {
    try {
      return await _channel.invokeMethod<int>('getApiVersion') ?? 0;
    } catch (_) {
      return 0;
    }
  }

  /// Platform display name for UI.
  static String get platformName {
    if (Platform.isIOS) return 'iOS';
    if (Platform.isAndroid) return 'Android';
    return 'HarmonyOS';
  }
}
