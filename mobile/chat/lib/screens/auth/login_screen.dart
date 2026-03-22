import 'dart:async';
import 'package:flutter/material.dart';
import '../../services/auth_service.dart';
import '../../services/hub_discovery.dart';

/// Login screen with hub discovery.
/// Flow: enter email → discover hub via HubCenter → select hub → login → poll.
class LoginScreen extends StatefulWidget {
  /// Returns the current AuthService. Called after hub discovery so we always
  /// get the instance with the correct baseUrl (created by main.dart in
  /// _onHubDiscovered → _setHubUrl).
  final AuthService? Function() authProvider;
  final VoidCallback onLoginSuccess;
  /// Called when a hub is discovered, so main.dart can update the base URL.
  final void Function(String hubUrl, String hubName)? onHubDiscovered;

  const LoginScreen({
    super.key,
    required this.authProvider,
    required this.onLoginSuccess,
    this.onHubDiscovered,
  });

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

enum _Phase { email, hubSelect, polling }

class _LoginScreenState extends State<LoginScreen> {
  final _emailController = TextEditingController();
  final _hubUrlController = TextEditingController();
  bool _loading = false;
  String? _error;
  String? _statusMessage;
  Timer? _pollTimer;
  String? _pollId;
  _Phase _phase = _Phase.email;
  List<DiscoveredHub> _hubs = [];
  int _pollAttempts = 0;
  static const _maxPollAttempts = 200; // ~10 minutes at 3s interval

  @override
  void dispose() {
    _pollTimer?.cancel();
    _emailController.dispose();
    _hubUrlController.dispose();
    super.dispose();
  }

  AuthService get _auth {
    final a = widget.authProvider();
    if (a == null) throw StateError('AuthService not available — hub not yet discovered');
    return a;
  }

  Future<void> _discover() async {
    final email = _emailController.text.trim();
    if (email.isEmpty) return;
    final directUrl = _hubUrlController.text.trim();
    setState(() { _loading = true; _error = null; });

    try {
      if (directUrl.isNotEmpty) {
        await _connectDirectHub(directUrl, email);
      } else {
        final result = await HubDiscovery.resolve(email);
        if (result.hubs.isEmpty) {
          setState(() { _loading = false; _error = result.message.isNotEmpty ? result.message : 'No hubs found'; });
          return;
        }
        if (result.hubs.length == 1) {
          await _selectHub(result.hubs.first, email);
        } else {
          setState(() { _loading = false; _hubs = result.hubs; _phase = _Phase.hubSelect; });
        }
      }
    } on HubDiscoveryException catch (e) {
      setState(() { _loading = false; _error = e.message; });
    } catch (e) {
      setState(() { _loading = false; _error = 'Network error: $e'; });
    }
  }

  Future<void> _connectDirectHub(String hubUrl, String email) async {
    hubUrl = hubUrl.replaceAll(RegExp(r'/+$'), '');
    final probe = await HubDiscovery.probe(hubUrl, email);

    if (probe.status == 'bound') {
      await _connectToHub(hubUrl, hubUrl.split('/').last, email);
      return;
    }

    if (probe.status == 'pending_approval') {
      setState(() {
        _loading = false;
        _error = probe.message.isNotEmpty ? probe.message : 'Your enrollment is pending approval';
      });
      return;
    }

    if (probe.status == 'not_found') {
      final enrollResult = await HubDiscovery.enroll(hubUrl, email);
      final enrollStatus = enrollResult['status'] as String? ?? '';
      if (enrollStatus == 'approved') {
        await _connectToHub(hubUrl, hubUrl, email);
        return;
      }
      setState(() {
        _loading = false;
        _error = enrollResult['message'] as String? ?? 'Enrollment submitted, waiting for approval';
      });
      return;
    }

    setState(() {
      _loading = false;
      _error = probe.message.isNotEmpty ? probe.message : 'Status: ${probe.status}';
    });
  }

  Future<void> _selectHub(DiscoveredHub hub, String email) async {
    setState(() { _loading = true; _error = null; });
    await _connectToHub(hub.baseUrl, hub.name, email);
  }

  Future<void> _connectToHub(String hubUrl, String hubName, String email) async {
    hubUrl = hubUrl.replaceAll(RegExp(r'/+$'), '');

    // Save and notify parent — this creates the AuthService with correct baseUrl.
    await HubDiscovery.saveHub(hubUrl, hubName);
    widget.onHubDiscovered?.call(hubUrl, hubName);

    // Now request email login using the freshly created AuthService.
    try {
      final result = await _auth.requestEmailLogin(email);
      if (result.pollId.isEmpty) {
        setState(() {
          _loading = false;
          _error = result.message.isNotEmpty ? result.message : 'Login not available: ${result.status}';
        });
        return;
      }
      _pollId = result.pollId;
      _pollAttempts = 0;
      setState(() {
        _loading = false;
        _phase = _Phase.polling;
        _statusMessage = result.message;
      });
      _startPolling();
    } on AuthException catch (e) {
      setState(() { _loading = false; _error = e.message; });
    } catch (e) {
      setState(() { _loading = false; _error = 'Login request failed: $e'; });
    }
  }

  void _startPolling() {
    _pollTimer = Timer.periodic(const Duration(seconds: 3), (_) async {
      if (_pollId == null) return;
      _pollAttempts++;
      if (_pollAttempts > _maxPollAttempts) {
        _pollTimer?.cancel();
        if (mounted) {
          setState(() { _error = 'Login timed out. Please try again.'; _phase = _Phase.email; });
        }
        return;
      }
      try {
        final confirmed = await _auth.pollLogin(_pollId!);
        if (confirmed) {
          _pollTimer?.cancel();
          widget.onLoginSuccess();
        }
      } catch (_) {
        // Transient error — keep polling.
      }
    });
  }

  void _goBack() {
    _pollTimer?.cancel();
    setState(() {
      _phase = _Phase.email;
      _hubs = [];
      _pollId = null;
      _pollAttempts = 0;
      _statusMessage = null;
      _error = null;
    });
  }
