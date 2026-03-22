import 'dart:async';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import 'api_client.dart';

/// Manages a single WebRTC peer connection for 1v1 voice calls.
class WebRTCService {
  final ApiClient api;

  RTCPeerConnection? _pc;
  MediaStream? _localStream;
  String? _currentCallId;
  bool _muted = false;

  final _stateController = StreamController<CallConnectionState>.broadcast();
  Stream<CallConnectionState> get connectionState => _stateController.stream;

  WebRTCService({required this.api});

  bool get isMuted => _muted;
  String? get currentCallId => _currentCallId;

  /// Start an outgoing call. Returns the call ID from the server.
  Future<String> startCall({
    required String calleeId,
    String? channelId,
  }) async {
    final resp = await api.initiateCall(
      calleeId: calleeId,
      channelId: channelId,
      callType: 0, // 1v1
    );
    _currentCallId = resp['call_id'] as String;

    await _createPeerConnection();
    final offer = await _pc!.createOffer();
    await _pc!.setLocalDescription(offer);

    return _currentCallId!;
  }

  /// Accept an incoming call with the remote SDP offer.
  Future<void> acceptCall(String callId, RTCSessionDescription remoteOffer) async {
    _currentCallId = callId;
    await api.answerCall(callId, accept: true);

    await _createPeerConnection();
    await _pc!.setRemoteDescription(remoteOffer);
    final answer = await _pc!.createAnswer();
    await _pc!.setLocalDescription(answer);
  }

  /// Reject an incoming call.
  Future<void> rejectCall(String callId) async {
    await api.answerCall(callId, accept: false);
  }

  /// Add a remote ICE candidate.
  Future<void> addIceCandidate(RTCIceCandidate candidate) async {
    await _pc?.addCandidate(candidate);
  }

  /// Set the remote session description (answer from callee).
  Future<void> setRemoteDescription(RTCSessionDescription sdp) async {
    await _pc?.setRemoteDescription(sdp);
  }

  /// Get the local session description to send to the remote peer.
  Future<RTCSessionDescription?> getLocalDescription() async {
    return _pc?.getLocalDescription();
  }

  /// Toggle microphone mute.
  void toggleMute() {
    _muted = !_muted;
    _localStream?.getAudioTracks().forEach((track) {
      track.enabled = !_muted;
    });
  }

  /// Hang up the current call.
  Future<void> hangup() async {
    if (_currentCallId != null) {
      try {
        await api.hangup(_currentCallId!);
      } catch (_) {}
    }
    await _cleanup();
  }

  Future<void> _createPeerConnection() async {
    final config = <String, dynamic>{
      'iceServers': [
        {'urls': 'stun:stun.l.google.com:19302'},
      ],
    };

    _pc = await createPeerConnection(config);

    // Capture audio-only local stream.
    _localStream = await navigator.mediaDevices.getUserMedia({
      'audio': true,
      'video': false,
    });
    for (final track in _localStream!.getTracks()) {
      await _pc!.addTrack(track, _localStream!);
    }

    _pc!.onIceCandidate = (candidate) {
      if (_currentCallId != null) {
        api.sendIceCandidate(_currentCallId!, candidate.toMap());
      }
    };

    _pc!.onConnectionState = (state) {
      switch (state) {
        case RTCPeerConnectionState.RTCPeerConnectionStateConnected:
          _stateController.add(CallConnectionState.connected);
          break;
        case RTCPeerConnectionState.RTCPeerConnectionStateDisconnected:
        case RTCPeerConnectionState.RTCPeerConnectionStateFailed:
          _stateController.add(CallConnectionState.disconnected);
          hangup();
          break;
        default:
          _stateController.add(CallConnectionState.connecting);
      }
    };
  }

  Future<void> _cleanup() async {
    _localStream?.getTracks().forEach((t) => t.stop());
    _localStream?.dispose();
    _localStream = null;
    await _pc?.close();
    _pc = null;
    _currentCallId = null;
    _stateController.add(CallConnectionState.disconnected);
  }

  Future<void> dispose() async {
    await _cleanup();
    _stateController.close();
  }
}

enum CallConnectionState { connecting, connected, disconnected }
