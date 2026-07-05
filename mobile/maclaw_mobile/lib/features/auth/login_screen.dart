import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/official_service.dart';
import 'auth_service.dart';
import 'session_controller.dart';

class LoginScreen extends ConsumerStatefulWidget {
  const LoginScreen({super.key});

  @override
  ConsumerState<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends ConsumerState<LoginScreen> {
  final _phoneController = TextEditingController();
  final _codeController = TextEditingController();
  PhoneLoginRequestResult? _pendingLogin;
  String? _message;
  String? _selectedHubCenterUrl;
  String? _discoveredHubUrl;
  bool _sendingCode = false;
  bool _verifying = false;

  @override
  void dispose() {
    _phoneController.dispose();
    _codeController.dispose();
    super.dispose();
  }

  Future<void> _sendCode() async {
    final phone = _phoneController.text.trim();
    if (phone.isEmpty || _sendingCode) return;
    setState(() {
      _sendingCode = true;
      _message = '正在连接 MaClaw 官方 HubCenter...';
      _selectedHubCenterUrl = null;
      _discoveredHubUrl = null;
      _pendingLogin = null;
    });
    try {
      final result = await ref
          .read(sessionControllerProvider.notifier)
          .requestPhoneLogin(phoneNumber: phone);
      if (!mounted) return;
      setState(() {
        _sendingCode = false;
        _pendingLogin = result;
        _selectedHubCenterUrl = result.hubCenterUrl;
        _discoveredHubUrl = result.hubUrl;
        final ttl =
            result.expiresMinutes > 0 ? '${result.expiresMinutes} 分钟内' : '';
        _message =
            result.message.isEmpty ? '验证码已发送，请在$ttl输入短信验证码。' : result.message;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _sendingCode = false;
        _message = '验证码发送失败：$error';
      });
    }
  }

  Future<void> _verifyCode() async {
    final pending = _pendingLogin;
    final code = _codeController.text.trim();
    if (pending == null || code.isEmpty || _verifying) return;
    setState(() {
      _verifying = true;
      _message = '正在验证手机号并进入 MaClaw Mobile...';
    });
    try {
      final ok = await ref
          .read(sessionControllerProvider.notifier)
          .verifyPhoneLoginOnHub(
            hubUrl: pending.hubUrl,
            phoneNumber: pending.phoneNumber,
            verifyCode: code,
            tenantId: pending.tenantId,
            hubCenterUrl: pending.hubCenterUrl,
          );
      if (!mounted) return;
      setState(() {
        _verifying = false;
        _message = ok ? '登录成功，已接入手机号账户的官方服务 credits。' : '验证码尚未确认，请重试。';
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _verifying = false;
        _message = '验证码验证失败：$error';
      });
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
            Center(
              child: Image.asset(
                'assets/images/maclaw_logo.png',
                width: 88,
                height: 88,
                semanticLabel: 'MaClaw',
                fit: BoxFit.contain,
              ),
            ),
            const SizedBox(height: 18),
            Text(
              '手机号注册/登录',
              style: Theme.of(context).textTheme.headlineSmall?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
            ),
            const SizedBox(height: 8),
            Text(
              'MaClaw Mobile 仅支持手机号账户接入。验证通过后，将使用该手机号账户绑定的 MaClaw 官方服务 credits 调用 LLM。',
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
                    Icon(Icons.hub_outlined, color: scheme.primary),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        'HubCenter 候选：${maclawOfficialHubCenterUrls.join(' / ')}',
                        style: Theme.of(context).textTheme.bodyMedium,
                      ),
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 12),
            if (_selectedHubCenterUrl != null || _discoveredHubUrl != null) ...[
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(14),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Icon(Icons.verified_outlined, color: scheme.primary),
                          const SizedBox(width: 10),
                          Text(
                            '官方接入状态',
                            style: Theme.of(context).textTheme.titleSmall,
                          ),
                        ],
                      ),
                      const SizedBox(height: 10),
                      if (_selectedHubCenterUrl != null)
                        _LoginInfoRow(
                          label: 'HubCenter',
                          value: _selectedHubCenterUrl!,
                        ),
                      if (_discoveredHubUrl != null)
                        _LoginInfoRow(
                          label: 'Hub',
                          value: _discoveredHubUrl!,
                        ),
                      if ((_pendingLogin?.tenantId ?? '').isNotEmpty)
                        _LoginInfoRow(
                          label: '租户',
                          value: _pendingLogin!.tenantId,
                        ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 12),
            ],
            TextField(
              controller: _phoneController,
              keyboardType: TextInputType.phone,
              autofillHints: const [AutofillHints.telephoneNumber],
              decoration: const InputDecoration(
                labelText: '手机号',
                prefixIcon: Icon(Icons.phone_outlined),
              ),
            ),
            const SizedBox(height: 12),
            FilledButton.icon(
              onPressed: _sendingCode ? null : _sendCode,
              icon: _sendingCode
                  ? const SizedBox.square(
                      dimension: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.sms_outlined),
              label: Text(_pendingLogin == null ? '发送验证码' : '重新发送验证码'),
            ),
            if (_pendingLogin != null) ...[
              const SizedBox(height: 18),
              TextField(
                controller: _codeController,
                keyboardType: TextInputType.number,
                decoration: InputDecoration(
                  labelText: _pendingLogin!.codeLength > 0
                      ? '${_pendingLogin!.codeLength} 位验证码'
                      : '验证码',
                  prefixIcon: const Icon(Icons.pin_outlined),
                ),
              ),
              const SizedBox(height: 12),
              FilledButton.icon(
                onPressed: _verifying ? null : _verifyCode,
                icon: _verifying
                    ? const SizedBox.square(
                        dimension: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.login),
                label: const Text('验证并登录'),
              ),
            ],
            if (_message != null) ...[
              const SizedBox(height: 14),
              Text(
                _message!,
                style: TextStyle(color: scheme.onSurfaceVariant),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _LoginInfoRow extends StatelessWidget {
  final String label;
  final String value;

  const _LoginInfoRow({
    required this.label,
    required this.value,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 82,
            child: Text(
              label,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: Theme.of(context).textTheme.bodyMedium,
            ),
          ),
        ],
      ),
    );
  }
}
