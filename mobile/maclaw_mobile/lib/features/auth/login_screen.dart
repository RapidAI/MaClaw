import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/official_service.dart';
import 'session_controller.dart';

class LoginScreen extends ConsumerStatefulWidget {
  const LoginScreen({super.key});

  @override
  ConsumerState<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends ConsumerState<LoginScreen> {
  final _emailController = TextEditingController();
  Timer? _pollTimer;
  String? _pollId;
  String? _message;
  bool _loading = false;

  @override
  void dispose() {
    _pollTimer?.cancel();
    _emailController.dispose();
    super.dispose();
  }

  Future<void> _startLogin() async {
    final email = _emailController.text.trim();
    if (email.isEmpty) return;
    setState(() {
      _loading = true;
      _message = null;
    });
    try {
      final result = await ref.read(sessionControllerProvider.notifier).requestEmailLogin(
            email: email,
          );
      if (result.pollId.isEmpty) {
        setState(() {
          _loading = false;
          _message = result.message.isEmpty ? '登录请求未返回 poll_id。' : result.message;
        });
        return;
      }
      _pollId = result.pollId;
      setState(() {
        _loading = false;
        _message = result.message.isEmpty ? '请在邮件或 IM 中确认登录。' : result.message;
      });
      _pollTimer?.cancel();
      _pollTimer = Timer.periodic(const Duration(seconds: 3), (_) => _pollLogin());
    } catch (error) {
      setState(() {
        _loading = false;
        _message = '登录请求失败：$error';
      });
    }
  }

  Future<void> _pollLogin() async {
    final pollId = _pollId;
    if (pollId == null || pollId.isEmpty) return;
    try {
      final ok = await ref.read(sessionControllerProvider.notifier).pollEmailLogin(
            pollId: pollId,
          );
      if (ok) {
        _pollTimer?.cancel();
      }
    } catch (_) {
      // Keep polling; transient network failures should not force restart.
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            const SizedBox(height: 36),
            Icon(Icons.emergency_share_outlined, size: 56, color: scheme.primary),
            const SizedBox(height: 18),
            Text('MaClaw Mobile', style: Theme.of(context).textTheme.headlineSmall),
            const SizedBox(height: 8),
            Text(
              '登录 MaClaw 官方服务后即可查信息、处理文档，并接入远程数字员工。',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 20),
            Card(
              child: Padding(
                padding: const EdgeInsets.all(14),
                child: Row(
                  children: [
                    Icon(Icons.verified_outlined, color: scheme.primary),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        '官方服务：$maclawOfficialServiceUrl',
                        style: Theme.of(context).textTheme.bodyMedium,
                      ),
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _emailController,
              keyboardType: TextInputType.emailAddress,
              decoration: const InputDecoration(
                labelText: '邮箱',
                prefixIcon: Icon(Icons.mail_outline),
              ),
            ),
            const SizedBox(height: 18),
            FilledButton.icon(
              onPressed: _loading ? null : _startLogin,
              icon: _loading
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.login),
              label: Text(_pollId == null ? '发送登录确认' : '重新发送'),
            ),
            if (_message != null) ...[
              const SizedBox(height: 14),
              Text(_message!, style: TextStyle(color: scheme.onSurfaceVariant)),
            ],
          ],
        ),
      ),
    );
  }
}
