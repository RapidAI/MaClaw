import 'package:audioplayers/audioplayers.dart';

/// Singleton audio player for voice note playback.
/// Ensures only one voice note plays at a time.
class AudioPlayerService {
  static final AudioPlayerService _instance = AudioPlayerService._();
  factory AudioPlayerService() => _instance;
  AudioPlayerService._();

  final _player = AudioPlayer();
  String? _currentUrl;

  bool get isPlaying => _player.state == PlayerState.playing;
  String? get currentUrl => _currentUrl;

  Stream<PlayerState> get onStateChanged => _player.onPlayerStateChanged;

  /// Play or pause the voice note at [url].
  /// If a different note is playing, stops it first.
  Future<void> togglePlay(String url) async {
    if (_currentUrl == url && isPlaying) {
      await _player.pause();
      return;
    }

    if (_currentUrl != url) {
      await _player.stop();
      _currentUrl = url;
      await _player.play(UrlSource(url));
    } else {
      await _player.resume();
    }
  }

  Future<void> stop() async {
    await _player.stop();
    _currentUrl = null;
  }

  void dispose() {
    _player.dispose();
  }
}
