import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../l10n/app_strings.dart';
import '../../shared/surface.dart';
import '../../shared/theme.dart';
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
  bool _sendingCode = false;
  bool _verifying = false;
  bool _messageIsError = false;
  Timer? _resendTimer;
  int _resendSecondsRemaining = 0;

  @override
  void dispose() {
    _phoneController.dispose();
    _codeController.dispose();
    _resendTimer?.cancel();
    super.dispose();
  }

  void _startResendCooldown(int seconds) {
    _resendTimer?.cancel();
    if (seconds <= 0 || !mounted) return;
    setState(() => _resendSecondsRemaining = seconds);
    _resendTimer = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (!mounted) {
        timer.cancel();
        return;
      }
      if (_resendSecondsRemaining <= 1) {
        timer.cancel();
        setState(() => _resendSecondsRemaining = 0);
        return;
      }
      setState(() => _resendSecondsRemaining--);
    });
  }

  AppStrings get _s =>
      ref.read(appStringsProvider);

  Future<void> _sendCode() async {
    final s = _s;
    final phone = _phoneController.text.trim();
    if (_sendingCode) return;
    if (!_looksLikePhoneNumber(phone)) {
      setState(() {
        _message = s.invalidPhone;
        _messageIsError = true;
      });
      return;
    }
    setState(() {
      _sendingCode = true;
      _message = s.connectingOfficial;
      _messageIsError = false;
      // Keep previous pending (hub) if re-sending so code entry stays available.
    });
    try {
      final result = await ref
          .read(sessionControllerProvider.notifier)
          .requestPhoneLogin(phoneNumber: phone);
      if (!mounted) return;
      setState(() {
        _sendingCode = false;
        _pendingLogin = result;
        if (result.deliveryUnconfirmed) {
          _message = result.message.isEmpty ? s.codeMayBeSent : result.message;
          _messageIsError = false;
        } else {
          final ttl = result.expiresMinutes > 0
              ? (s.isZh
                  ? '${result.expiresMinutes} 分钟内'
                  : ' within ${result.expiresMinutes} min')
              : '';
          _message = result.message.isEmpty
              ? s.codeSentWithTtl(ttl)
              : result.message;
          _messageIsError = false;
        }
      });
      _startResendCooldown(
        result.resendCooldownSeconds > 0
            ? result.resendCooldownSeconds
            : PhoneLoginRequestResult.defaultResendCooldownSeconds,
      );
    } catch (error) {
      if (!mounted) return;
      final detail = _formatSendError(error);
      setState(() {
        _sendingCode = false;
        // If we already have a hub-bound pending session, keep code entry open.
        if (_pendingLogin != null && _pendingLogin!.hubUrl.isNotEmpty) {
          _message = s.sendUnconfirmed(detail);
          _messageIsError = true;
          _startResendCooldown(PhoneLoginRequestResult.defaultResendCooldownSeconds);
        } else {
          _message = s.sendCodeFailed(detail);
          _messageIsError = true;
        }
      });
    }
  }

  String _formatSendError(Object error) {
    final s = _s;
    if (error is DioException) {
      final status = error.response?.statusCode;
      final data = error.response?.data;
      String serverMsg = '';
      if (data is Map) {
        serverMsg = (data['message'] as String?)?.trim() ?? '';
        if (serverMsg.isEmpty && data['error'] is Map) {
          serverMsg =
              (Map<String, dynamic>.from(data['error'] as Map)['message']
                          as String?)
                      ?.trim() ??
                  '';
        }
      }
      if (serverMsg.isNotEmpty) {
        return status != null ? 'HTTP $status · $serverMsg' : serverMsg;
      }
      switch (error.type) {
        case DioExceptionType.connectionTimeout:
        case DioExceptionType.sendTimeout:
        case DioExceptionType.receiveTimeout:
          return s.networkTimeoutMaybeSent;
        case DioExceptionType.connectionError:
          return s.cannotConnectOfficial;
        case DioExceptionType.badResponse:
          return status != null
              ? 'HTTP $status'
              : (s.isZh ? '服务响应异常' : 'Bad server response');
        default:
          break;
      }
      final m = error.message?.trim() ?? '';
      if (m.isNotEmpty) return m;
    }
    final text = error.toString().trim();
    if (text.length > 160) return '${text.substring(0, 160)}…';
    return text.isEmpty ? s.unknownError : text;
  }

  bool _looksLikePhoneNumber(String value) {
    final trimmed = value.trim();
    if (trimmed.isEmpty) return false;
    var digits = 0;
    for (final codeUnit in trimmed.codeUnits) {
      if (codeUnit >= 48 && codeUnit <= 57) {
        digits++;
        continue;
      }
      if (codeUnit == 32 ||
          codeUnit == 43 ||
          codeUnit == 45 ||
          codeUnit == 40 ||
          codeUnit == 41) {
        continue;
      }
      return false;
    }
    return digits >= 8 && digits <= 15;
  }

  Future<void> _verifyCode() async {
    final s = _s;
    final pending = _pendingLogin;
    final code = _codeController.text.trim();
    if (pending == null || code.isEmpty || _verifying) return;
    if (pending.hubUrl.trim().isEmpty) {
      setState(() {
        _message = s.missingHubUrl;
        _messageIsError = true;
      });
      return;
    }
    setState(() {
      _verifying = true;
      _message = s.verifyingLogin;
      _messageIsError = false;
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
        _message = ok ? s.loginSuccess : s.codeNotConfirmed;
        _messageIsError = !ok;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _verifying = false;
        _message = s.verifyFailed(_formatSendError(error));
        _messageIsError = true;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final s = ref.watch(appStringsProvider);
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final showCodeEntry = _pendingLogin != null;
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 440),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(24, 28, 24, 32),
              children: [
                const SizedBox(height: 12),
                Center(
                  child: DecoratedBox(
                    decoration: BoxDecoration(
                      color: scheme.primaryContainer.withValues(alpha: 0.45),
                      borderRadius:
                          BorderRadius.circular(MaClawColors.radiusLg),
                      border: Border.all(
                        color: scheme.primary.withValues(alpha: 0.12),
                      ),
                    ),
                    child: Padding(
                      padding: const EdgeInsets.all(16),
                      child: Image.asset(
                        'assets/images/maclaw_logo.png',
                        width: 72,
                        height: 72,
                        semanticLabel: 'MaClaw',
                        fit: BoxFit.contain,
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: 22),
                Text(
                  s.loginTitle,
                  textAlign: TextAlign.center,
                  style: text.headlineSmall?.copyWith(
                    fontWeight: FontWeight.w700,
                    letterSpacing: -0.01,
                  ),
                ),
                const SizedBox(height: 24),
                Card(
                  child: Padding(
                    padding: const EdgeInsets.all(MaClawColors.spaceLg),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(
                          s.loginAccountVerify,
                          style: text.titleMedium?.copyWith(
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        const SizedBox(height: 4),
                        Text(
                          s.loginAccountHint,
                          style: text.bodySmall?.copyWith(
                            color: scheme.onSurfaceVariant,
                          ),
                        ),
                        const SizedBox(height: 16),
                        TextField(
                          controller: _phoneController,
                          keyboardType: TextInputType.phone,
                          textInputAction: TextInputAction.done,
                          autofillHints: const [AutofillHints.telephoneNumber],
                          onSubmitted: (_) {
                            if (!_sendingCode && _resendSecondsRemaining == 0) {
                              unawaited(_sendCode());
                            }
                          },
                          decoration: InputDecoration(
                            labelText: s.phoneNumber,
                            prefixIcon: const Icon(Icons.phone_outlined),
                          ),
                        ),
                        const SizedBox(height: 12),
                        FilledButton.icon(
                          onPressed: _sendingCode || _resendSecondsRemaining > 0
                              ? null
                              : _sendCode,
                          icon: _sendingCode
                              ? const SizedBox.square(
                                  dimension: 18,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                  ),
                                )
                              : const Icon(Icons.sms_outlined),
                          label: Text(
                            !showCodeEntry
                                ? s.sendCode
                                : _resendSecondsRemaining > 0
                                    ? s.resendCodeIn(_resendSecondsRemaining)
                                    : s.resendCode,
                          ),
                        ),
                        if (showCodeEntry) ...[
                          const SizedBox(height: 18),
                          Divider(color: scheme.outlineVariant),
                          const SizedBox(height: 16),
                          TextField(
                            controller: _codeController,
                            keyboardType: TextInputType.number,
                            textInputAction: TextInputAction.done,
                            autofillHints: const [
                              AutofillHints.oneTimeCode,
                            ],
                            onSubmitted: (_) {
                              if (!_verifying) unawaited(_verifyCode());
                            },
                            decoration: InputDecoration(
                              labelText: (_pendingLogin?.codeLength ?? 0) > 0
                                  ? s.nDigitCode(_pendingLogin!.codeLength)
                                  : s.verificationCode,
                              prefixIcon: const Icon(Icons.pin_outlined),
                              helperText: _pendingLogin?.deliveryUnconfirmed ==
                                      true
                                  ? s.codeEntryHelper
                                  : null,
                            ),
                          ),
                          const SizedBox(height: 12),
                          FilledButton.icon(
                            onPressed: _verifying ? null : _verifyCode,
                            icon: _verifying
                                ? const SizedBox.square(
                                    dimension: 18,
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                    ),
                                  )
                                : const Icon(Icons.login),
                            label: Text(s.verifyAndLogin),
                          ),
                        ],
                      ],
                    ),
                  ),
                ),
                if (_message != null) ...[
                  const SizedBox(height: 14),
                  StatusBanner(
                    tone: _messageIsError
                        ? StatusTone.danger
                        : (_sendingCode || _verifying
                            ? StatusTone.info
                            : StatusTone.success),
                    icon: _messageIsError
                        ? Icons.error_outline
                        : (_sendingCode || _verifying
                            ? Icons.hourglass_top_outlined
                            : Icons.check_circle_outline),
                    message: _message!,
                  ),
                ],
                const SizedBox(height: 18),
                Text(
                  s.loginFooter,
                  textAlign: TextAlign.center,
                  style: text.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
