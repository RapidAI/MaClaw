import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:flutter_animate/flutter_animate.dart';

import '../../core/locale.dart';
import '../../l10n/gen/app_localizations.dart';
import '../../core/providers.dart';
import '../../core/ui_kit.dart';
import 'auth_api.dart';
import 'qr_login_screen.dart';

/// 登录 — 邮箱验证码或手机验证码两步登录；`mode=add` 时标题变为“添加账号”。
class LoginScreen extends ConsumerStatefulWidget {
  final bool addingAccount;

  /// Token-expired re-verification: same flow, different chrome, and the
  /// account row stays stored so a successful verify upserts the same key.
  final bool relogin;

  const LoginScreen({
    super.key,
    this.addingAccount = false,
    this.relogin = false,
  });

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
          // A pending code request is bound to its method — drop it when
          // the user switches tabs or verify would reuse the wrong one.
          _resendTimer?.cancel();
          setState(() {
            _error = '';
            _info = '';
            _request = null;
            _resendIn = 0;
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

  String _identityHint(AppLocalizations S) =>
      _method == AuthMethod.email ? S.identityHintEmail : S.identityHintPhone;

  String _identityLabel(AppLocalizations S) =>
      _method == AuthMethod.email ? S.identityLabelEmail : S.identityLabelPhone;

  Future<void> _sendCode() async {
    final identity = _identityCtrl.text.trim();
    if (identity.isEmpty) {
      setState(
        () => _error = _method == AuthMethod.email
            ? context.S.identityRequiredEmail
            : context.S.identityRequiredPhone,
      );
      return;
    }
    setState(() {
      _sending = true;
      _error = '';
      _info = '';
    });
    try {
      final request = await ref
          .read(authApiProvider)
          .requestCode(method: _method, identity: identity);
      if (!mounted) return;
      setState(() {
        _request = request;
        _info = request.message.isNotEmpty
            ? request.message
            : (_method == AuthMethod.email
                  ? context.S.codeSentEmail
                  : context.S.codeSentSms);
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
      setState(() => _error = context.S.codeRequired);
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
      final result = await ref
          .read(authApiProvider)
          .verifyCode(
            request: request,
            code: code,
            machineName: 'MaclawGo',
            clientId: clientId,
            language: language,
          );
      if (!mounted) return;
      if (result.confirmed) {
        await ref.read(sessionProvider.notifier).addAccount(result.account!);
        if (!mounted) return;
        if (widget.addingAccount && context.canPop()) {
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
            : context.S.pendingApproval;
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
    final S = context.S;
    if (text.contains('INVALID_SMS_VERIFY_CODE') ||
        text.contains('Invalid SMS verification code') ||
        text.contains('INVALID_VERIFY_CODE')) {
      return S.verifyCodeInvalid;
    }
    if (text.contains('INVALID_PHONE_NUMBER')) return S.phoneFormatInvalid;
    if (text.contains('PHONE_ALREADY_REGISTERED')) {
      return S.phoneAlreadyRegistered;
    }
    if (text.contains('PHONE_REGISTRATION_DISABLED')) {
      return S.phoneRegistrationDisabled;
    }
    if (text.contains('IP_BLOCKED')) return S.ipBlocked;
    if (text.startsWith('Invalid argument')) {
      return _method == AuthMethod.email ? S.invalidEmail : S.invalidPhone;
    }
    return text.isNotEmpty ? text : S.opFailedRetry;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: widget.addingAccount
          ? AppBar(title: Text(context.S.addAccountTitle))
          : widget.relogin
          ? AppBar(title: Text(context.S.reloginTitle))
          : null,
      body: Stack(
        children: [
          // A quiet primary wash behind the hero — depth without decoration.
          Positioned.fill(
            child: DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.center,
                  colors: [
                    theme.colorScheme.primary.withValues(alpha: 0.10),
                    theme.colorScheme.surface,
                  ],
                  stops: const [0, 0.45],
                ),
              ),
            ),
          ),
          SafeArea(
            child: Center(
              child: SingleChildScrollView(
                padding: const EdgeInsets.symmetric(
                  horizontal: 28,
                  vertical: 24,
                ),
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 420),
                  child:
                      Column(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              if (!widget.addingAccount) ...[
                                const Center(child: BrandMark(size: 88)),
                                const SizedBox(height: 18),
                                Text(
                                  'MaclawGo',
                                  textAlign: TextAlign.center,
                                  style: theme.textTheme.displaySmall,
                                ),
                                const SizedBox(height: 6),
                                Text(
                                  context.S.loginTagline,
                                  textAlign: TextAlign.center,
                                  style: theme.textTheme.bodyMedium?.copyWith(
                                    color: theme.colorScheme.onSurfaceVariant,
                                  ),
                                ),
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
                                    context.S.sessionExpired,
                                    textAlign: TextAlign.center,
                                    style: TextStyle(
                                      color: theme.colorScheme.onErrorContainer,
                                    ),
                                  ),
                                ),
                              TabBar(
                                controller: _tabCtrl,
                                tabs: [
                                  Tab(text: context.S.loginTabEmail),
                                  Tab(text: context.S.loginTabPhone),
                                ],
                              ),
                              const SizedBox(height: 20),
                              TextField(
                                controller: _identityCtrl,
                                keyboardType: _method == AuthMethod.email
                                    ? TextInputType.emailAddress
                                    : TextInputType.phone,
                                decoration: InputDecoration(
                                  labelText: _identityLabel(context.S),
                                  hintText: _identityHint(context.S),
                                  prefixIcon: Icon(
                                    _method == AuthMethod.email
                                        ? Icons.mail_outline
                                        : Icons.phone_iphone,
                                  ),
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
                                      decoration: InputDecoration(
                                        labelText: context.S.codeLabel,
                                        counterText: '',
                                        prefixIcon: const Icon(
                                          Icons.pin_outlined,
                                        ),
                                        border: const OutlineInputBorder(),
                                        helperText: _method == AuthMethod.email
                                            ? context.S.codeExpiryEmail
                                            : context.S.codeExpirySms,
                                      ),
                                      enabled: _request != null && !_verifying,
                                      onSubmitted: (_) => _verify(),
                                    ),
                                  ),
                                  const SizedBox(width: 10),
                                  SizedBox(
                                    height: 48,
                                    child: FilledButton.tonal(
                                      onPressed: _sending || _resendIn > 0
                                          ? null
                                          : _sendCode,
                                      child: _sending
                                          ? const SizedBox(
                                              width: 16,
                                              height: 16,
                                              child: CircularProgressIndicator(
                                                strokeWidth: 2,
                                              ),
                                            )
                                          : Text(
                                              _resendIn > 0
                                                  ? context.S.resendIn(
                                                      _resendIn,
                                                    )
                                                  : _request == null
                                                  ? context.S.getCode
                                                  : context.S.resendCode,
                                            ),
                                    ),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 18),
                              if (_info.isNotEmpty)
                                Padding(
                                  padding: const EdgeInsets.only(bottom: 10),
                                  child: Text(
                                    _info,
                                    style: theme.textTheme.bodySmall?.copyWith(
                                      color: theme.colorScheme.primary,
                                    ),
                                  ),
                                ),
                              if (_error.isNotEmpty)
                                Padding(
                                  padding: const EdgeInsets.only(bottom: 10),
                                  child: Text(
                                    _error,
                                    style: theme.textTheme.bodySmall?.copyWith(
                                      color: theme.colorScheme.error,
                                    ),
                                  ),
                                ),
                              FilledButton(
                                onPressed: _request == null || _verifying
                                    ? null
                                    : _verify,
                                child: _verifying
                                    ? const SizedBox(
                                        width: 20,
                                        height: 20,
                                        child: CircularProgressIndicator(
                                          strokeWidth: 2,
                                        ),
                                      )
                                    : Text(context.S.signIn),
                              ),
                              const SizedBox(height: 10),
                              OutlinedButton.icon(
                                onPressed: _verifying
                                    ? null
                                    : () => Navigator.of(context).push(
                                        MaterialPageRoute<void>(
                                          builder: (_) => const QrLoginScreen(),
                                        ),
                                      ),
                                icon: const Icon(Icons.qr_code_scanner),
                                label: Text(context.S.scanToSignIn),
                              ),
                              const SizedBox(height: 12),
                              Text(
                                context.S.loginFooter,
                                textAlign: TextAlign.center,
                                style: theme.textTheme.bodySmall?.copyWith(
                                  color: theme.colorScheme.onSurfaceVariant,
                                ),
                              ),
                            ],
                          )
                          .animate()
                          .fadeIn(duration: 320.ms)
                          .slideY(
                            begin: 0.04,
                            end: 0,
                            duration: 320.ms,
                            curve: Curves.easeOutCubic,
                          ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
