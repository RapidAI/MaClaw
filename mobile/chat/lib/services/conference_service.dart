import 'dart:async';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import 'api_client.dart';

/// Manages multi-party voice conference using mesh topology.
/// Each participant maintains a direct peer connection to every other.
/// Suitable for small groups (up to ~6 participants).
class ConferenceService {
  final ApiClient api;

  MediaStream? _localStream;
  final Map<String, RTCPeerConnection> _peers = {};
  final _participantsController = StreamController<List<String>>.broadcast();

  Stream<List<String>> get participants => _participantsController.stream;
  List<String> get currentParticipants => _peers.keys.toList();

  ConferenceService({required this.api});

  /// Join a conference call. Creates local audio stream and prepares
  /// to accept peer connections from other participants.
  Future<void> joinConference(String callId) async {
    _localStream = await navigator.mediaDevices.getUserMedia({
      'audio': true,
      'video': false,
    });
  }

  /// Add a new peer to the conference mesh.
  Future<RTCSessionDescription> addPeer(String peerId) async {
    final pc = await _createPeerConnection(peerId);
    _peers[peerId] = pc;
    _notifyParticipants();

    final offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    return offer;
  }

  /// Accept a peer's offer and return our answer.
  Future<RTCSessionDescription> acceptPeer(
    String peerId,
    RTCSessionDescription offer,
  ) async {
    final pc = await _createPeerConnection(peerId);
    _peers[peerId] = pc;
    _notifyParticipants();

    await pc.setRemoteDescription(offer);
    final answer = await pc.createAnswer();
    await pc.setLocalDescription(answer);
    return answer;
  }

  /// Set the remote answer for a peer we sent an offer to.
  Future<void> setPeerAnswer(
      String peerId, RTCSessionDescription answer) async {
    await _peers[peerId]?.setRemoteDescription(answer);
  }

  /// Add an ICE candidate for a specific peer.
  Future<void> addIceCandidate(
      String peerId, RTCIceCandidate candidate) async {
    await _peers[peerId]?.addCandidate(candidate);
  }

  /// Remove a peer who left the conference.
  Future<void> removePeer(String peerId) async {
    final pc = _peers.remove(peerId);
    await pc?.close();
    _notifyParticipants();
  }

  /// Toggle local microphone mute.
  void toggleMute() {
    _localStream?.getAudioTracks().forEach((t) => t.enabled = !t.enabled);
  }

  /// Leave the conference entirely.
  Future<void> leave() async {
    for (final pc in _peers.values) {
      await pc.close();
    }
    _peers.clear();
    _localStream?.getTracks().forEach((t) => t.stop());
    _localStream?.dispose();
    _localStream = null;
    _notifyParticipants();
  }

  Future<RTCPeerConnection> _createPeerConnection(String peerId) async {
    final config = <String, dynamic>{
      'iceServers': [
        {'urls': 'stun:stun.l.google.com:19302'},
      ],
    };

    final pc = await createPeerConnection(config);

    if (_localStream != null) {
      for (final track in _localStream!.getTracks()) {
        await pc.addTrack(track, _localStream!);
      }
    }

    pc.onIceCandidate = (candidate) {
      // Send ICE candidate to this specific peer via signaling.
      api.sendIceCandidate(peerId, candidate.toMap());
    };

    pc.onConnectionState = (state) {
      if (state == RTCPeerConnectionState.RTCPeerConnectionStateFailed ||
          state == RTCPeerConnectionState.RTCPeerConnectionStateDisconnected) {
        removePeer(peerId);
      }
    };

    return pc;
  }

  void _notifyParticipants() {
    _participantsController.add(_peers.keys.toList());
  }

  void dispose() {
    leave();
    _participantsController.close();
  }
}
