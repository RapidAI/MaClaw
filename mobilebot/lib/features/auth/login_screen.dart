import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/providers.dart';
import 'auth_api.dart';

/// 登录 — 邮箱验证码或手机验证码两步登录；`mode=add` 时标题变为“添加账号”。
class LoginScreen extends ConsumerStatefulWidget {
  final bool addingAccount;

  /// Token-expired re-verification: same flow, different chrome, and the
  /// account row stays stored so a successful verify upserts the same key.
  final bool relogin;

  const LoginScreen(
      {super.key, this.addingAccount = false, this.relogin = false});

  @override
  ConsumerState<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends ConsumerState<LoginScreen>
    with SingleTickerProviderStateMixin {
  late final TabController _tabCtrl;
  final _identityCtrl = TextEditingController();
  final _codeCtrl = TextEditingController();

  CodeRequest? _request;
  bool _sending = false;
  bool _verifying = false;
  String _error = '';
  String _info = '';
  int _resendIn = 0;
  Timer? _resendTimer;

  AuthMethod get _method =>
      _tabCtrl.index == 0 ? AuthMethod.email : AuthMethod.phone;

  @override
  void initState() {
    super.initState();
    _tabCtrl = TabController(length: 2, vsync: this)
      ..addListener(() {
        if (!_tabCtrl.indexIsChanging) {
          setState(() {
            _error = '';
            _info = '';
          });
        }
      });
  }

  @override
  void dispose() {
    _tabCtrl.dispose();
    _identityCtrl.dispose();
    _codeCtrl.dispose();
    _resendTimer?.cancel();
    super.dispose();
  }

  String get _identityHint =>
      _method == AuthMethod.email ? '邮箱地址' : '手机号';

  String get _identityLabel =>
      _method == AuthMethod.email ? '邮箱' : '手机号';

  Future<void> _sendCode() async {
    final identity = _identityCtrl.text.trim();
    if (identity.isEmpty) {
      setState(() => _error = '请输入$_identityLabel');
      return;
    }
    setState(() {
      _sending = true;
      _error = '';
      _info = '';
    });
    try {
      final request = await ref.read(authApiProvider).requestCode(
            method: _method,
            identity: identity,
          );
      if (!mounted) return;
      setState(() {
        _request = request;
        _info = request.message.isNotEmpty
            ? request.message
            : '验证码已发送，请查收${_method == AuthMethod.email ? '邮箱' : '短信'}';
        _resendIn = 60;
      });
      _startResendTimer();
    } on Object catch (error) {
      if (!mounted) return;
      setState(() => _error = _errorText(error));
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  void _startResendTimer() {
    _resendTimer?.cancel();
    _resendTimer = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (!mounted) {
        timer.cancel();
        return;
      }
      setState(() {
        if (_resendIn > 0) _resendIn--;
      });
      if (_resendIn <= 0) timer.cancel();
    });
  }

  Future<void> _verify() async {
    final request = _request;
    if (request == null) return;
    final code = _codeCtrl.text.trim();
    if (code.isEmpty) {
      setState(() => _error = '请输入验证码');
      return;
    }
    setState(() {
      _verifying = true;
      _error = '';
    });
    try {
      final clientId = await ref.read(clientIdProvider.future);
      if (!mounted) return;
      final language = Localizations.localeOf(context).languageCode;
      final result = await ref.read(authApiProvider).verifyCode(
            request: request,
            code: code,
            machineName: 'MobileBot',
            clientId: clientId,
            language: language,
          );
      if (!mounted) return;
      if (result.confirmed) {
        await ref
            .read(sessionProvider.notifier)
            .addAccount(result.account!);
        if (!mounted) return;
        if (widget.addingAccount) {
          context.pop();
        } else {
          context.go('/');
        }
        // relogin also lands via context.go — the redirect clears the
        // expired flag as soon as addAccount upserts the fresh token.
        return;
      }
      setState(() {
        _info = result.message.isNotEmpty
            ? result.message
            : '注册已提交，等待管理员审批后即可使用';
      });
    } on Object catch (error) {
      if (!mounted) return;
      setState(() => _error = _errorText(error));
    } finally {
      if (mounted) setState(() => _verifying = false);
    }
  }

  String _errorText(Object error) {
    final text = error.toString();
    if (text.contains('INVALID_SMS_VERIFY_CODE') ||
        text.contains('Invalid SMS verification code')) {
      return '验证码不正确';
    }
    if (text.contains('INVALID_PHONE_NUMBER')) return '手机号格式不正确';
    if (text.contains('PHONE_ALREADY_REGISTERED')) return '该手机号已绑定其他账号';
    if (text.contains('PHONE_REGISTRATION_DISABLED')) {
      return '当前 Hub 未开启手机注册，请改用邮箱登录';
    }
    if (text.contains('IP_BLOCKED')) return '请求被拦截，请稍后再试';
    if (text.startsWith('Invalid argument')) {
      return _method == AuthMethod.email ? '请输入有效的邮箱地址' : '请输入有效的手机号';
    }
    return text.isNotEmpty ? text : '操作失败，请重试';
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: widget.addingAccount
          ? AppBar(title: const Text('添加账号'))
          : widget.relogin
              ? AppBar(title: const Text('重新登录'))
              : null,
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.symmetric(horizontal: 28, vertical: 24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  if (!widget.addingAccount) ...[
                    Icon(Icons.smart_toy,
                        size: 64, color: theme.colorScheme.primary),
                    const SizedBox(height: 12),
                    Text('MobileBot',
                        textAlign: TextAlign.center,
                        style: theme.textTheme.headlineMedium),
                    const SizedBox(height: 6),
                    Text('随时随地管理和指挥你的桌面 Bot',
                        textAlign: TextAlign.center,
                        style: theme.textTheme.bodyMedium?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant)),
                    const SizedBox(height: 28),
                  ],
                  if (widget.relogin)
                    Container(
                      margin: const EdgeInsets.only(bottom: 16),
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: theme.colorScheme.errorContainer,
                        borderRadius: BorderRadius.circular(12),
                      ),
                      child: Text(
                        '登录已过期，请重新验证身份',
                        textAlign: TextAlign.center,
                        style: TextStyle(
                            color: theme.colorScheme.onErrorContainer),
                      ),
                    ),
                  TabBar(
                    controller: _tabCtrl,
                    tabs: const [
                      Tab(text: '邮箱登录'),
                      Tab(text: '手机登录'),
                    ],
                  ),
                  const SizedBox(height: 20),
                  TextField(
                    controller: _identityCtrl,
                    keyboardType: _method == AuthMethod.email
                        ? TextInputType.emailAddress
                        : TextInputType.phone,
                    decoration: InputDecoration(
                      labelText: _identityLabel,
                      hintText: _identityHint,
                      prefixIcon: Icon(_method == AuthMethod.email
                          ? Icons.mail_outline
                          : Icons.phone_iphone),
                      border: const OutlineInputBorder(),
                    ),
                    enabled: !_verifying,
                  ),
                  const SizedBox(height: 14),
                  Row(
                    children: [
                      Expanded(
                        child: TextField(
                          controller: _codeCtrl,
                          keyboardType: TextInputType.number,
                          maxLength: 8,
                          decoration: const InputDecoration(
                            labelText: '验证码',
                            counterText: '',
                            prefixIcon: Icon(Icons.pin_outlined),
                            border: OutlineInputBorder(),
                          ),
                          enabled: _request != null && !_verifying,
                          onSubmitted: (_) => _verify(),
                        ),
                      ),
                      const SizedBox(width: 10),
                      SizedBox(
                        height: 48,
                        child: FilledButton.tonal(
                          onPressed:
                              _sending || _resendIn > 0 ? null : _sendCode,
                          child: _sending
                              ? const SizedBox(
                                  width: 16,
                                  height: 16,
                                  child:
                                      CircularProgressIndicator(strokeWidth: 2),
                                )
                              : Text(_resendIn > 0
                                  ? '${_resendIn}s'
                                  : _request == null
                                      ? '获取验证码'
                                      : '重新发送'),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 18),
                  if (_info.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: Text(_info,
                          style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.primary)),
                    ),
                  if (_error.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: Text(_error,
                          style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.error)),
                    ),
                  FilledButton(
                    onPressed:
                        _request == null || _verifying ? null : _verify,
                    style: FilledButton.styleFrom(
                        padding: const EdgeInsets.symmetric(vertical: 14)),
                    child: _verifying
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Text('登录'),
                  ),
                  const SizedBox(height: 12),
                  Text(
                    '登录即表示同意使用该账号下的桌面 Bot 服务。\n未注册的联系方式会自动创建账号。',
                    textAlign: TextAlign.center,
                    style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
