import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../core/providers.dart';
import 'desktop_auth_qr.dart';

/// Scan-or-paste entry that redeems the desktop GUI's "移动认证二维码" —
/// logging this phone in as the same Hub account, no code needed.
class QrLoginScreen extends ConsumerStatefulWidget {
  /// Test hook: replaces the camera preview.
  final Widget? scanner;

  /// Test hook: builds a custom scanner surface and gets the submit callback.
  final Widget Function(ValueChanged<String> onPayload)? scannerBuilder;

  const QrLoginScreen({super.key, this.scanner, this.scannerBuilder});

  @override
  ConsumerState<QrLoginScreen> createState() => _QrLoginScreenState();
}

class _QrLoginScreenState extends ConsumerState<QrLoginScreen> {
  final _manualCtrl = TextEditingController();
  var _submitting = false;
  String? _message;

  @override
  void dispose() {
    _manualCtrl.dispose();
    super.dispose();
  }

  Future<void> _submit(String payload) async {
    final text = payload.trim();
    if (text.isEmpty || _submitting) return;
    late final DesktopAuthQrPayload parsed;
    try {
      parsed = parseDesktopAuthQrPayload(text);
    } on FormatException catch (error) {
      setState(() => _message = error.message);
      return;
    }
    setState(() {
      _submitting = true;
      _message = null;
    });
    try {
      final clientId = await ref.read(clientIdProvider.future);
      if (!mounted) return;
      final language = Localizations.localeOf(context).languageCode;
      final result = await ref.read(authApiProvider).redeemDesktopQr(
            payload: parsed,
            machineName: 'MaClawBot',
            clientId: clientId,
            language: language,
          );
      if (!mounted) return;
      if (!result.confirmed) {
        setState(() {
          _submitting = false;
          _message = result.message.isNotEmpty ? result.message : '登录已提交，等待审批';
        });
        return;
      }
      await ref.read(sessionProvider.notifier).addAccount(result.account!);
      if (!mounted) return;
      // The router's session redirect also fires; going home directly keeps
      // the scan page from flashing during the handoff.
      context.go('/');
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _message = _errorText(error);
      });
    }
  }

  String _errorText(Object error) {
    final text = error.toString();
    if (text.contains('INVALID_DESKTOP_QR')) {
      return '二维码无效、已使用或已过期，请在电脑端重新生成';
    }
    if (text.contains('QR_OWNER_UNAVAILABLE')) {
      return '生成该二维码的账号不可用';
    }
    if (text.contains('PHONE_IDENTITY_REQUIRED')) {
      return '该账号未绑定手机号，无法使用扫码登录';
    }
    // An older Hub has no redeem route at all.
    if (text.contains('HTTP_404') || text.contains('NOT_FOUND')) {
      return '当前 Hub 版本不支持扫码登录，请更新 Hub 或改用验证码登录';
    }
    return text.isNotEmpty ? text : '操作失败，请重试';
  }

  void _handleDetect(BarcodeCapture capture) {
    for (final barcode in capture.barcodes) {
      final value = barcode.rawValue?.trim() ?? '';
      if (value.isEmpty) continue;
      _submit(value);
      return;
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Scaffold(
      appBar: AppBar(title: const Text('扫码登录')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 28),
          children: [
            Text('扫描桌面端的"移动认证二维码"',
                style: theme.textTheme.titleLarge),
            const SizedBox(height: 8),
            Text(
              '在电脑端 MaClaw「关于 → Hub 身份 → 移动认证二维码」打开二维码，5 分钟内扫描即可登录同一账号。',
              style: theme.textTheme.bodyMedium?.copyWith(
                color: scheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 16),
            AspectRatio(
              aspectRatio: 1,
              child: ClipRRect(
                borderRadius: BorderRadius.circular(16),
                child: DecoratedBox(
                  decoration: BoxDecoration(color: scheme.surfaceContainer),
                  child: widget.scannerBuilder?.call(_submit) ??
                      widget.scanner ??
                      MobileScanner(
                        onDetect: _submitting ? (_) {} : _handleDetect,
                      ),
                ),
              ),
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _manualCtrl,
              minLines: 2,
              maxLines: 4,
              decoration: const InputDecoration(
                labelText: '或粘贴二维码内容',
                prefixIcon: Icon(Icons.qr_code_2_outlined),
              ),
            ),
            const SizedBox(height: 12),
            FilledButton.icon(
              onPressed: _submitting ? null : () => _submit(_manualCtrl.text),
              icon: _submitting
                  ? const SizedBox.square(
                      dimension: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.login),
              label: Text(_submitting ? '登录中' : '确认登录'),
            ),
            if (_message != null) ...[
              const SizedBox(height: 12),
              Text(
                _message!,
                style: theme.textTheme.bodyMedium?.copyWith(
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
