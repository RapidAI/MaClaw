import 'dart:async';
import 'package:flutter/material.dart';
import '../../services/auth_service.dart';
import '../../services/hub_discovery.dart';

/// Login screen with hub discovery.
/// Flow: enter email → discover hub via HubCenter → select hub → login → poll.
class LoginScreen extends StatefulWidget {
  final VoidCallback onLoginSuccess;
  /// Called when a hub is discovered and user is authenticated.
  /// main.dart uses this to set up AuthService/ApiClient with the hub URL.
  final void Function(String hubUrl, String hubName, AuthService auth)? onHubReady;

  const LoginScreen({
    super.key,
    required this.onLoginSuccess,
    this.onHubReady,
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

  /// AuthService created locally once hub is discovered.
  AuthService? _auth;

  @override
  void dispose() {
    _pollTimer?.cancel();
    _emailController.dispose();
    _hubUrlController.dispose();
    super.dispose();
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

    // Save hub and create AuthService locally with the correct baseUrl.
    await HubDiscovery.saveHub(hubUrl, hubName);
    _auth = AuthService(baseUrl: hubUrl);

    // Now request email login on this hub.
    try {
      final result = await _auth!.requestEmailLogin(email);
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
      _startPolling(hubUrl, hubName);
    } on AuthException catch (e) {
      setState(() { _loading = false; _error = e.message; });
    } catch (e) {
      setState(() { _loading = false; _error = 'Login request failed: $e'; });
    }
  }

  void _startPolling(String hubUrl, String hubName) {
    _pollTimer = Timer.periodic(const Duration(seconds: 3), (_) async {
      if (_pollId == null || _auth == null) return;
      _pollAttempts++;
      if (_pollAttempts > _maxPollAttempts) {
        _pollTimer?.cancel();
        if (mounted) {
          setState(() { _error = 'Login timed out. Please try again.'; _phase = _Phase.email; });
        }
        return;
      }
      try {
        final confirmed = await _auth!.pollLogin(_pollId!);
        if (confirmed) {
          _pollTimer?.cancel();
          // Notify parent with the authenticated AuthService.
          widget.onHubReady?.call(hubUrl, hubName, _auth!);
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
      _auth = null;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 32),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(Icons.chat_bubble_outline,
                  size: 64, color: Theme.of(context).colorScheme.primary),
              const SizedBox(height: 16),
              Text('MaClaw Chat',
                  style: Theme.of(context).textTheme.headlineMedium),
              const SizedBox(height: 48),
              if (_phase == _Phase.email) _buildEmailForm(),
              if (_phase == _Phase.hubSelect) _buildHubSelect(),
              if (_phase == _Phase.polling) _buildPolling(),
              if (_error != null) ...[
                const SizedBox(height: 12),
                Text(_error!, style: const TextStyle(color: Colors.red, fontSize: 13)),
              ],
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildEmailForm() {
    return Column(children: [
      TextField(
        controller: _emailController,
        keyboardType: TextInputType.emailAddress,
        textInputAction: TextInputAction.go,
        onSubmitted: (_) => _discover(),
        decoration: InputDecoration(
          labelText: 'Email',
          hintText: 'your@email.com',
          prefixIcon: const Icon(Icons.email_outlined),
          border: OutlineInputBorder(borderRadius: BorderRadius.circular(12)),
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: _hubUrlController,
        keyboardType: TextInputType.url,
        decoration: InputDecoration(
          labelText: 'Hub URL (optional)',
          hintText: 'Or enter a private Hub URL directly',
          prefixIcon: const Icon(Icons.dns_outlined),
          border: OutlineInputBorder(borderRadius: BorderRadius.circular(12)),
        ),
      ),
      const SizedBox(height: 16),
      SizedBox(
        width: double.infinity,
        height: 48,
        child: FilledButton(
          onPressed: _loading ? null : _discover,
          child: _loading
              ? const SizedBox(width: 20, height: 20,
                  child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
              : const Text('Discover & Sign In'),
        ),
      ),
      const SizedBox(height: 8),
      Text('Hub Center: ${HubDiscovery.defaultCenterUrl}',
          style: TextStyle(color: Colors.grey[500], fontSize: 11)),
    ]);
  }

  Widget _buildHubSelect() {
    final email = _emailController.text.trim();
    return Column(children: [
      TextButton.icon(
        onPressed: _goBack,
        icon: const Icon(Icons.arrow_back, size: 16),
        label: const Text('Back'),
      ),
      Text('Found ${_hubs.length} hubs', style: const TextStyle(fontSize: 14)),
      const SizedBox(height: 12),
      ..._hubs.map((hub) => Padding(
        padding: const EdgeInsets.only(bottom: 8),
        child: InkWell(
          onTap: _loading ? null : () => _selectHub(hub, email),
          borderRadius: BorderRadius.circular(12),
          child: Container(
            width: double.infinity,
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              border: Border.all(color: Colors.grey.shade300),
              borderRadius: BorderRadius.circular(12),
            ),
            child: Row(
              children: [
                Expanded(child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(hub.name.isNotEmpty ? hub.name : hub.hubId,
                        style: const TextStyle(fontWeight: FontWeight.w600)),
                    const SizedBox(height: 2),
                    Text('${hub.visibility} · ${hub.enrollmentMode}',
                        style: TextStyle(color: Colors.grey[600], fontSize: 12)),
                  ],
                )),
                const Icon(Icons.chevron_right),
              ],
            ),
          ),
        ),
      )),
    ]);
  }

  Widget _buildPolling() {
    return Column(children: [
      const CircularProgressIndicator(),
      const SizedBox(height: 16),
      Text(_statusMessage ?? 'Check your email or IM for the login link.',
          textAlign: TextAlign.center),
      const SizedBox(height: 8),
      Text('Waiting for confirmation...',
          style: TextStyle(color: Colors.grey[600], fontSize: 13)),
      const SizedBox(height: 24),
      TextButton(onPressed: _goBack, child: const Text('Back')),
    ]);
  }
}
