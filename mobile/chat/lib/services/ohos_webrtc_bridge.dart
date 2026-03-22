import 'dart:io';
import 'package:flutter/services.dart';

/// Platform channel bridge for HarmonyOS WebRTC.
/// On OHOS, flutter_webrtc doesn't have native support, so we bridge
/// to the OHOS WebRTC SDK via a method channel.
///
/// On Android/iOS, this class is a no-op — flutter_webrtc works natively.
class OhosWebRTCBridge {
  static const _channel = MethodChannel('com.maclaw.chat/ohos_webrtc');

  /// Whether we're running on HarmonyOS and need the bridge.
  static bool get isOhos => !Platform.isAndroid && !Platform.isIOS;

  /// Initialize the OHOS WebRTC engine.
  static Future<void> initialize() async {
    if (!isOhos) return;
    await _channel.invokeMethod('initialize');
  }

  /// Create a peer connection on the OHOS side.
  static Future<String> createPeerConnection(
      Map<String, dynamic> config) async {
    if (!isOhos) return '';
    final id = await _channel.invokeMethod<String>(
      'createPeerConnection',
      config,
    );
    return id ?? '';
  }

  /// Create an offer SDP.
  static Future<Map<String, dynamic>> createOffer(String pcId) async {
    if (!isOhos) return {};
    final result =
        await _channel.invokeMapMethod<String, dynamic>('createOffer', {
      'pcId': pcId,
    });
    return result ?? {};
  }

  /// Create an answer SDP.
  static Future<Map<String, dynamic>> createAnswer(String pcId) async {
    if (!isOhos) return {};
    final result =
        await _channel.invokeMapMethod<String, dynamic>('createAnswer', {
      'pcId': pcId,
    });
    return result ?? {};
  }

  /// Set remote description.
  static Future<void> setRemoteDescription(
      String pcId, Map<String, dynamic> sdp) async {
    if (!isOhos) return;
    await _channel.invokeMethod('setRemoteDescription', {
      'pcId': pcId,
      'sdp': sdp,
    });
  }

  /// Add ICE candidate.
  static Future<void> addIceCandidate(
      String pcId, Map<String, dynamic> candidate) async {
    if (!isOhos) return;
    await _channel.invokeMethod('addIceCandidate', {
      'pcId': pcId,
      'candidate': candidate,
    });
  }

  /// Close and dispose a peer connection.
  static Future<void> closePeerConnection(String pcId) async {
    if (!isOhos) return;
    await _channel.invokeMethod('closePeerConnection', {'pcId': pcId});
  }

  /// Get local audio stream (microphone).
  static Future<void> startLocalAudio(String pcId) async {
    if (!isOhos) return;
    await _channel.invokeMethod('startLocalAudio', {'pcId': pcId});
  }

  /// Mute/unmute local audio.
  static Future<void> setMute(bool muted) async {
    if (!isOhos) return;
    await _channel.invokeMethod('setMute', {'muted': muted});
  }

  /// Dispose the OHOS WebRTC engine.
  static Future<void> dispose() async {
    if (!isOhos) return;
    await _channel.invokeMethod('dispose');
  }
}
