import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import '../services/webrtc_service.dart';
import '../services/ws_client.dart';

/// Manages voice call state and coordinates WebRTC + WS signaling.
class CallProvider extends ChangeNotifier {
  final WebRTCService webrtc;
  final WsClient ws;

  CallState state = CallState.idle;
  String? activeCallId;
  String? remoteName;
  Duration elapsed = Duration.zero;
  bool get isMuted => webrtc.isMuted;

  Timer? _elapsedTimer;
  StreamSubscription? _wsSub;
  StreamSubscription? _connSub;

  CallProvider({required this.webrtc, required this.ws}) {
    _wsSub = ws.events.listen(_handleWsEvent);
    _connSub = webrtc.connectionState.listen(_onConnectionState);
  }

  /// Initiate an outgoing 1v1 call.
  Future<void> startCall({
    required String calleeId,
    required String calleeName,
    String? channelId,
  }) async {
    state = CallState.outgoing;
    remoteName = calleeName;
    notifyListeners();

    try {
      activeCallId = await webrtc.startCall(
        calleeId: calleeId,
        channelId: channelId,
      );
      notifyListeners();
    } catch (_) {
      state = CallState.idle;
      notifyListeners();
    }
  }

  /// Accept an incoming call.
  Future<void> acceptCall(String callId, RTCSessionDescription offer) async {
    state = CallState.active;
    activeCallId = callId;
    notifyListeners();

    await webrtc.acceptCall(callId, offer);
    _startElapsedTimer();
  }

  /// Reject an incoming call.
  Future<void> rejectCall(String callId) async {
    await webrtc.rejectCall(callId);
    _reset();
  }

  /// Hang up the active call.
  Future<void> hangup() async {
    await webrtc.hangup();
    _reset();
  }

  void toggleMute() {
    webrtc.toggleMute();
    notifyListeners();
  }

  // ── WS signaling events ───────────────────────────────────

  void _handleWsEvent(WsEvent event) {
    switch (event.type) {
      case 'call_offer':
        final callId = event.data['call_id'] as String;
        final callerName = event.data['caller_name'] as String? ?? 'Unknown';
        final sdp = event.data['sdp'] as Map<String, dynamic>?;
        if (sdp != null && state == CallState.idle) {
          activeCallId = callId;
          remoteName = callerName;
          state = CallState.incoming;
          notifyListeners();
        }
        break;

      case 'call_answer':
        final sdpMap = event.data['sdp'] as Map<String, dynamic>?;
        if (sdpMap != null) {
          final sdp = RTCSessionDescription(
            sdpMap['sdp'] as String?,
            sdpMap['type'] as String?,
          );
          webrtc.setRemoteDescription(sdp);
          state = CallState.active;
          _startElapsedTimer();
          notifyListeners();
        }
        break;

      case 'call_ice':
        final c = event.data['candidate'] as Map<String, dynamic>?;
        if (c != null) {
          webrtc.addIceCandidate(RTCIceCandidate(
            c['candidate'] as String?,
            c['sdpMid'] as String?,
            c['sdpMLineIndex'] as int?,
          ));
        }
        break;

      case 'call_hangup':
        _reset();
        break;
    }
  }

  void _onConnectionState(CallConnectionState cs) {
    if (cs == CallConnectionState.connected && state != CallState.active) {
      state = CallState.active;
      _startElapsedTimer();
      notifyListeners();
    } else if (cs == CallConnectionState.disconnected &&
        state != CallState.idle) {
      _reset();
    }
  }

  void _startElapsedTimer() {
    _elapsedTimer?.cancel();
    elapsed = Duration.zero;
    _elapsedTimer = Timer.periodic(const Duration(seconds: 1), (_) {
      elapsed += const Duration(seconds: 1);
      notifyListeners();
    });
  }

  void _reset() {
    _elapsedTimer?.cancel();
    state = CallState.idle;
    activeCallId = null;
    remoteName = null;
    elapsed = Duration.zero;
    notifyListeners();
  }

  @override
  void dispose() {
    _elapsedTimer?.cancel();
    _wsSub?.cancel();
    _connSub?.cancel();
    webrtc.dispose();
    super.dispose();
  }
}

enum CallState { idle, outgoing, incoming, active }
