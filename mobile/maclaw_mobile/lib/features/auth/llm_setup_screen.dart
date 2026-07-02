import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

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
  final _emailController = TextEditingController();
  Timer? _pollTimer;
  MobileServiceConnectResult? _pendingConnection;
  String? _pollId;
  var _submitting = false;
  var _emailSubmitting = false;
  String? _message;
  String? _emailMessage;

  @override
  void dispose() {
    _pollTimer?.cancel();
    _redeemCodeController.dispose();
    _qrPayloadController.dispose();
    _emailController.dispose();
    super.dispose();
  }

  Future<void> _redeemCode() async {
    final code = _redeemCodeController.text.trim();
    if (code.isEmpty || _submitting) return;
    await _submit(
      () => ref
          .read(sessionControllerProvider.notifier)
          .redeemOfficialServiceCode(
            code,
          ),
      success: 'MaClaw 官方服务已接入',
    );
  }

  Future<void> _submitQr(String payload) async {
    final text = payload.trim();
    if (text.isEmpty || _submitting) return;
    await _submit(
      () =>
          ref.read(sessionControllerProvider.notifier).connectWithDesktopLlmQr(
                text,
              ),
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
          _pollId = null;
          _emailMessage = error.toString();
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

  Future<void> _requestPendingEmailLogin() async {
    final pending = _pendingConnection;
    final email = _emailController.text.trim();
    if (pending == null || email.isEmpty || _emailSubmitting) return;
    setState(() {
      _emailSubmitting = true;
      _emailMessage = '正在向所属 Hub 发送登录确认...';
    });
    try {
      final result = await ref
          .read(sessionControllerProvider.notifier)
          .requestEmailLoginOnHub(
            hubUrl: pending.hubUrl,
            hubCenterUrl: pending.hubCenterUrl,
            email: email,
          );
      if (result.pollId.isEmpty) {
        setState(() {
          _emailSubmitting = false;
          _emailMessage =
              result.message.isEmpty ? 'Hub 未返回登录轮询凭据。' : result.message;
        });
        return;
      }
      _pollId = result.pollId;
      _pollTimer?.cancel();
      _pollTimer = Timer.periodic(
        const Duration(seconds: 3),
        (_) => _pollPendingEmailLogin(),
      );
      setState(() {
        _emailSubmitting = false;
        _emailMessage = result.message.isEmpty
            ? '请在邮箱或 IM 中确认登录，确认后会自动进入 MaClaw Mobile。'
            : result.message;
      });
    } catch (error) {
      setState(() {
        _emailSubmitting = false;
        _emailMessage = '登录确认发送失败：$error';
      });
    }
  }

  Future<void> _pollPendingEmailLogin() async {
    final pending = _pendingConnection;
    final pollId = _pollId;
    if (pending == null || pollId == null || pollId.isEmpty) return;
    try {
      final ok = await ref
          .read(sessionControllerProvider.notifier)
          .pollEmailLoginOnHub(
            hubUrl: pending.hubUrl,
            hubCenterUrl: pending.hubCenterUrl,
            pollId: pollId,
          );
      if (!ok) return;
      _pollTimer?.cancel();
      if (!mounted) return;
      setState(() {
        _emailMessage = '登录已确认，正在进入 MaClaw Mobile。';
      });
    } catch (_) {
      // Keep polling; weak mobile networks commonly produce transient failures.
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
              '移动端只从 MaClaw 官方服务接入。你可以输入官方服务兑换码，或扫描 MaClaw GUI 中 LLM 配置界面的服务商二维码。',
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
            Card(
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
                      controller: _redeemCodeController,
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
                        onPressed: _submitting ? null : _redeemCode,
                        icon: const Icon(Icons.verified_outlined),
                        label: const Text('接入官方服务'),
                      ),
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 12),
            if (_pendingConnection != null) ...[
              _PendingEmailLoginCard(
                pending: _pendingConnection!,
                emailController: _emailController,
                submitting: _emailSubmitting,
                polling: _pollId != null,
                message: _emailMessage,
                onSubmit: _requestPendingEmailLogin,
              ),
              const SizedBox(height: 12),
            ],
            Card(
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
                          decoration: BoxDecoration(
                            color: scheme.surfaceContainer,
                          ),
                          child: widget.scanner ??
                              MobileScanner(
                                onDetect: _submitting ? (_) {} : _handleDetect,
                              ),
                        ),
                      ),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _qrPayloadController,
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
                        onPressed: _submitting
                            ? null
                            : () => _submitQr(_qrPayloadController.text),
                        icon: const Icon(Icons.qr_code_scanner_outlined),
                        label: const Text('接入二维码服务商'),
                      ),
                    ),
                  ],
                ),
              ),
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

class _PendingEmailLoginCard extends StatelessWidget {
  final MobileServiceConnectResult pending;
  final TextEditingController emailController;
  final bool submitting;
  final bool polling;
  final String? message;
  final VoidCallback onSubmit;

  const _PendingEmailLoginCard({
    required this.pending,
    required this.emailController,
    required this.submitting,
    required this.polling,
    required this.message,
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
            Row(
              children: [
                Icon(Icons.mark_email_read_outlined, color: scheme.primary),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    '继续完成 Hub 邮箱登录',
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
              controller: emailController,
              keyboardType: TextInputType.emailAddress,
              decoration: const InputDecoration(
                labelText: '邮箱',
                prefixIcon: Icon(Icons.mail_outline),
              ),
            ),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: submitting ? null : onSubmit,
                icon: submitting
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : Icon(
                        polling
                            ? Icons.hourglass_top_outlined
                            : Icons.login_outlined,
                      ),
                label: Text(polling ? '重新发送登录确认' : '发送登录确认'),
              ),
            ),
            if (message != null && message!.isNotEmpty) ...[
              const SizedBox(height: 10),
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
            width: 86,
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
