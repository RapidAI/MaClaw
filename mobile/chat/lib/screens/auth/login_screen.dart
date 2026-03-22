import 'dart:async';
import 'package:flutter/material.dart';
import '../../services/auth_service.dart';

/// Email-based login screen.
/// Flow: enter email → request link → poll until confirmed.
class LoginScreen extends StatefulWidget {
  final AuthService auth;
  final VoidCallback onLoginSuccess;

  const LoginScreen({super.key, required this.auth, required this.onLoginSuccess});

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _emailController = TextEditingController();
  bool _loading = false;
  bool _polling = false;
  String? _error;
  Timer? _pollTimer;

  @override
  void dispose() {
    _pollTimer?.cancel();
    _emailController.dispose();
    super.dispose();
  }

  Future<void> _requestLogin() async {
    final email = _emailController.text.trim();
    if (email.isEmpty) return;
    setState(() { _loading = true; _error = null; });
    try {
      await widget.auth.requestEmailLogin(email);
      setState(() { _loading = false; _polling = true; });
      _startPolling(email);
    } on AuthException catch (e) {
      setState(() { _loading = false; _error = e.message; });
    } catch (e) {
      setState(() { _loading = false; _error = 'Network error'; });
    }
  }

  void _startPolling(String email) {
    _pollTimer = Timer.periodic(const Duration(seconds: 3), (_) async {
      final confirmed = await widget.auth.pollLogin(email);
      if (confirmed) {
        _pollTimer?.cancel();
        widget.onLoginSuccess();
      }
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
              if (!_polling) ...[
                TextField(
                  controller: _emailController,
                  keyboardType: TextInputType.emailAddress,
                  textInputAction: TextInputAction.go,
                  onSubmitted: (_) => _requestLogin(),
                  decoration: InputDecoration(
                    labelText: 'Email',
                    hintText: 'your@email.com',
                    prefixIcon: const Icon(Icons.email_outlined),
                    border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(12)),
                  ),
                ),
                const SizedBox(height: 16),
                SizedBox(
                  width: double.infinity,
                  height: 48,
                  child: FilledButton(
                    onPressed: _loading ? null : _requestLogin,
                    child: _loading
                        ? const SizedBox(
                            width: 20, height: 20,
                            child: CircularProgressIndicator(
                                strokeWidth: 2, color: Colors.white))
                        : const Text('Sign In'),
                  ),
                ),
              ] else ...[
                const CircularProgressIndicator(),
                const SizedBox(height: 16),
                const Text('Check your email or IM for the login link.',
                    textAlign: TextAlign.center),
                const SizedBox(height: 8),
                Text('Waiting for confirmation...',
                    style: TextStyle(color: Colors.grey[600], fontSize: 13)),
                const SizedBox(height: 24),
                TextButton(
                  onPressed: () {
                    _pollTimer?.cancel();
                    setState(() => _polling = false);
                  },
                  child: const Text('Use a different email'),
                ),
              ],
              if (_error != null) ...[
                const SizedBox(height: 12),
                Text(_error!,
                    style: const TextStyle(color: Colors.red, fontSize: 13)),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
