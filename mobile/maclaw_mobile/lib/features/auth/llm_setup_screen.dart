import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../core/api/desktop_llm_qr.dart';
import '../../core/api/official_service.dart';
import 'auth_service.dart';
import 'session_controller.dart';
import 'startup_splash_screen.dart';

class LlmSetupScreen extends ConsumerStatefulWidget {
  final Widget? scanner;

  const LlmSetupScreen({super.key, this.scanner});

  @override
  ConsumerState<LlmSetupScreen> createState() => _LlmSetupScreenState();
}

class _LlmSetupScreenState extends ConsumerState<LlmSetupScreen> {
  final _redeemCodeController = TextEditingController();
  final _qrPayloadController = TextEditingController();
  final _phoneController = TextEditingController();
  final _codeController = TextEditingController();

  MobileServiceConnectResult? _pendingConnection;
  PhoneLoginRequestResult? _pendingPhoneLogin;
  var _submitting = false;
  var _phoneSubmitting = false;
  var _codeSubmitting = false;
  String? _message;
  String? _phoneMessage;

  @override
  void dispose() {
    _redeemCodeController.dispose();
    _qrPayloadController.dispose();
    _phoneController.dispose();
    _codeController.dispose();
    super.dispose();
  }

  Future<void> _redeemCode() async {
    final code = _redeemCodeController.text.trim();
    if (code.isEmpty || _submitting) return;
    await _submit(
      () => ref
          .read(sessionControllerProvider.notifier)
          .redeemOfficialServiceCode(code),
      success: 'MaClaw 官方服务已接入',
    );
  }

  Future<void> _submitQr(String payload) async {
    final text = payload.trim();
    if (text.isEmpty || _submitting) return;
    try {
      parseMaclawDesktopLlmQrPayload(text);
    } on FormatException {
      setState(() {
        _message = '接入失败：请扫描或粘贴 MaClaw GUI 生成的移动端 LLM 授权二维码。';
      });
      return;
    }
    await _submit(
      () => ref
          .read(sessionControllerProvider.notifier)
          .connectWithDesktopLlmQr(text),
      success: '桌面 GUI 服务商二维码已接入',
    );
  }

  Future<void> _submit(
    Future<void> Function() action, {
    required String success,
  }) async {
    setState(() {
      _submitting = true;
      _message = null;
    });
    try {
      await action();
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(success)),
      );
    } catch (error) {
      if (!mounted) return;
      if (error is MobileServiceConnectionPendingException) {
        setState(() {
          _submitting = false;
          _pendingConnection = error.result;
          _pendingPhoneLogin = null;
          _phoneMessage = error.toString();
          _message = null;
        });
        return;
      }
      setState(() {
        _submitting = false;
        _message = '接入失败：$error';
      });
    }
  }

  Future<void> _requestPendingPhoneCode() async {
    final pending = _pendingConnection;
    final phone = _phoneController.text.trim();
    if (pending == null || phone.isEmpty || _phoneSubmitting) return;
    setState(() {
      _phoneSubmitting = true;
      _phoneMessage = '正在向所属 Hub 发送短信验证码...';
    });
    try {
      final result = await ref
          .read(sessionControllerProvider.notifier)
          .requestPhoneLoginOnHub(
            hubUrl: pending.hubUrl,
            hubCenterUrl: pending.hubCenterUrl,
            tenantId: pending.tenantId,
            phoneNumber: phone,
          );
      if (!mounted) return;
      setState(() {
        _phoneSubmitting = false;
        _pendingPhoneLogin = result;
        _phoneMessage =
            result.message.isEmpty ? '验证码已发送，请输入短信验证码完成登录。' : result.message;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _phoneSubmitting = false;
        _phoneMessage = '验证码发送失败：$error';
      });
    }
  }

  Future<void> _verifyPendingPhoneLogin() async {
    final pending = _pendingConnection;
    final phoneLogin = _pendingPhoneLogin;
    final code = _codeController.text.trim();
    if (pending == null ||
        phoneLogin == null ||
        code.isEmpty ||
        _codeSubmitting) {
      return;
    }
    setState(() {
      _codeSubmitting = true;
      _phoneMessage = '正在验证手机号并进入 MaClaw Mobile...';
    });
    try {
      final ok = await ref
          .read(sessionControllerProvider.notifier)
          .verifyPhoneLoginOnHub(
            hubUrl: pending.hubUrl,
            hubCenterUrl: pending.hubCenterUrl,
            tenantId: pending.tenantId,
            phoneNumber: phoneLogin.phoneNumber,
            verifyCode: code,
          );
      if (!mounted) return;
      setState(() {
        _codeSubmitting = false;
        _phoneMessage = ok ? '登录成功，已接入手机号账户的官方服务 credits。' : '验证码尚未确认，请重试。';
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _codeSubmitting = false;
        _phoneMessage = '验证码验证失败：$error';
      });
    }
  }

  void _handleDetect(BarcodeCapture capture) {
    for (final barcode in capture.barcodes) {
      final value = barcode.rawValue?.trim() ?? '';
      if (value.isEmpty) continue;
      _submitQr(value);
      return;
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 24, 20, 32),
          children: [
            Center(
              child: Image.asset(
                maclawLogoAsset,
                width: 92,
                height: 92,
                fit: BoxFit.contain,
              ),
            ),
            const SizedBox(height: 16),
            Text(
              '配置 MaClaw LLM 服务',
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.headlineSmall?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
            ),
            const SizedBox(height: 8),
            Text(
              '移动端通过官方 HubCenter 接入。可输入官方服务兑换码，或扫描 MaClaw GUI 中 LLM 配置界面的服务商二维码。',
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 16),
            Wrap(
              alignment: WrapAlignment.center,
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final url in maclawOfficialHubCenterUrls)
                  Chip(label: Text(url)),
              ],
            ),
            const SizedBox(height: 20),
            _OfficialCodeCard(
              controller: _redeemCodeController,
              submitting: _submitting,
              onSubmit: _redeemCode,
            ),
            const SizedBox(height: 12),
            if (_pendingConnection != null) ...[
              _PendingPhoneLoginCard(
                pending: _pendingConnection!,
                phoneController: _phoneController,
                codeController: _codeController,
                codeSent: _pendingPhoneLogin != null,
                sending: _phoneSubmitting,
                verifying: _codeSubmitting,
                message: _phoneMessage,
                onSendCode: _requestPendingPhoneCode,
                onVerify: _verifyPendingPhoneLogin,
              ),
              const SizedBox(height: 12),
            ],
            _DesktopQrCard(
              controller: _qrPayloadController,
              scanner: widget.scanner,
              submitting: _submitting,
              onDetect: _handleDetect,
              onSubmit: () => _submitQr(_qrPayloadController.text),
            ),
            if (_submitting) ...[
              const SizedBox(height: 16),
              const LinearProgressIndicator(),
            ],
            if (_message != null) ...[
              const SizedBox(height: 12),
              Text(
                _message!,
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                      color: scheme.error,
                    ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _OfficialCodeCard extends StatelessWidget {
  final TextEditingController controller;
  final bool submitting;
  final VoidCallback onSubmit;

  const _OfficialCodeCard({
    required this.controller,
    required this.submitting,
    required this.onSubmit,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '1. MaClaw 官方服务兑换码',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 10),
            TextField(
              controller: controller,
              textCapitalization: TextCapitalization.characters,
              decoration: const InputDecoration(
                labelText: '兑换码',
                prefixIcon: Icon(Icons.confirmation_number_outlined),
              ),
            ),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: submitting ? null : onSubmit,
                icon: const Icon(Icons.verified_outlined),
                label: const Text('接入官方服务'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _DesktopQrCard extends StatelessWidget {
  final TextEditingController controller;
  final Widget? scanner;
  final bool submitting;
  final void Function(BarcodeCapture capture) onDetect;
  final VoidCallback onSubmit;

  const _DesktopQrCard({
    required this.controller,
    required this.scanner,
    required this.submitting,
    required this.onDetect,
    required this.onSubmit,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '2. MaClaw GUI 服务商二维码',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 8),
            Text(
              '在电脑端 MaClaw GUI 的 LLM 配置界面生成二维码后，用手机扫描或粘贴二维码内容。',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 12),
            AspectRatio(
              aspectRatio: 1,
              child: ClipRRect(
                borderRadius: BorderRadius.circular(8),
                child: DecoratedBox(
                  decoration: BoxDecoration(color: scheme.surfaceContainer),
                  child: scanner ??
                      MobileScanner(onDetect: submitting ? (_) {} : onDetect),
                ),
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: controller,
              minLines: 2,
              maxLines: 4,
              decoration: const InputDecoration(
                labelText: '粘贴二维码内容',
                prefixIcon: Icon(Icons.qr_code_2_outlined),
              ),
            ),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: OutlinedButton.icon(
                onPressed: submitting ? null : onSubmit,
                icon: const Icon(Icons.qr_code_scanner_outlined),
                label: const Text('接入二维码服务商'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _PendingPhoneLoginCard extends StatelessWidget {
  final MobileServiceConnectResult pending;
  final TextEditingController phoneController;
  final TextEditingController codeController;
  final bool codeSent;
  final bool sending;
  final bool verifying;
  final String? message;
  final VoidCallback onSendCode;
  final VoidCallback onVerify;

  const _PendingPhoneLoginCard({
    required this.pending,
    required this.phoneController,
    required this.codeController,
    required this.codeSent,
    required this.sending,
    required this.verifying,
    required this.message,
    required this.onSendCode,
    required this.onVerify,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.phone_android_outlined, color: scheme.primary),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    '继续完成手机号登录',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            _PendingInfoRow(label: 'Hub', value: pending.hubUrl),
            if (pending.tenantId.isNotEmpty)
              _PendingInfoRow(label: '租户', value: pending.tenantId),
            if (pending.hubCenterUrl.isNotEmpty)
              _PendingInfoRow(label: 'HubCenter', value: pending.hubCenterUrl),
            const SizedBox(height: 12),
            TextField(
              controller: phoneController,
              keyboardType: TextInputType.phone,
              autofillHints: const [AutofillHints.telephoneNumber],
              decoration: const InputDecoration(
                labelText: '手机号',
                prefixIcon: Icon(Icons.phone_outlined),
              ),
            ),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: sending ? null : onSendCode,
                icon: sending
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.sms_outlined),
                label: Text(codeSent ? '重新发送验证码' : '发送验证码'),
              ),
            ),
            if (codeSent) ...[
              const SizedBox(height: 12),
              TextField(
                controller: codeController,
                keyboardType: TextInputType.number,
                decoration: const InputDecoration(
                  labelText: '验证码',
                  prefixIcon: Icon(Icons.pin_outlined),
                ),
              ),
              const SizedBox(height: 12),
              SizedBox(
                width: double.infinity,
                child: FilledButton.icon(
                  onPressed: verifying ? null : onVerify,
                  icon: verifying
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.login),
                  label: const Text('验证并登录'),
                ),
              ),
            ],
            if (message != null) ...[
              const SizedBox(height: 12),
              Text(
                message!,
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _PendingInfoRow extends StatelessWidget {
  final String label;
  final String value;

  const _PendingInfoRow({
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
          Expanded(child: Text(value)),
        ],
      ),
    );
  }
}
