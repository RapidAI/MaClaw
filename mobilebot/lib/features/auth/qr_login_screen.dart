import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../core/locale.dart';
import '../../core/providers.dart';
import 'desktop_auth_qr.dart';

/// What a scanned QR is for.
enum QrScanMode {
  /// Redeem the desktop GUI's "移动认证二维码" and sign this phone in.
  desktopLogin,
  /// Authorize a third-party client (e.g. RapidEditor AI) to use the
  /// signed-in maclaw account's official service.
  thirdPartyAuthorize,
}

/// Scan-or-paste entry. In [QrScanMode.desktopLogin] it redeems the desktop
/// GUI's "移动认证二维码" and signs this phone in. In
/// [QrScanMode.thirdPartyAuthorize] it grants a third-party client access to
/// the already-signed-in account's official service (no account switch).
class QrLoginScreen extends ConsumerStatefulWidget {
  /// Test hook: replaces the camera preview.
  final Widget? scanner;

  /// Test hook: builds a custom scanner surface and gets the submit callback.
  final Widget Function(ValueChanged<String> onPayload)? scannerBuilder;

  final QrScanMode mode;

  const QrLoginScreen({
    super.key,
    this.scanner,
    this.scannerBuilder,
    this.mode = QrScanMode.desktopLogin,
  });

  @override
  ConsumerState<QrLoginScreen> createState() => _QrLoginScreenState();
}

class _QrLoginScreenState extends ConsumerState<QrLoginScreen> {
  final _manualCtrl = TextEditingController();
  var _submitting = false;
  String? _message;
  var _messageIsSuccess = false;

  @override
  void dispose() {
    _manualCtrl.dispose();
    super.dispose();
  }

  Future<void> _submit(String payload) async {
    final text = payload.trim();
    if (text.isEmpty || _submitting) return;
    setState(() {
      _submitting = true;
      _message = null;
      _messageIsSuccess = false;
    });
    try {
      if (widget.mode == QrScanMode.thirdPartyAuthorize) {
        await _submitAuthorization(text);
      } else {
        await _submitLogin(text);
      }
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _message = _errorText(error);
      });
    }
  }

  Future<void> _submitLogin(String text) async {
    late final DesktopAuthQrPayload parsed;
    try {
      parsed = parseDesktopAuthQrPayload(text);
    } on FormatException catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _message = error.message;
      });
      return;
    }
    final clientId = await ref.read(clientIdProvider.future);
    if (!mounted) return;
    final language = Localizations.localeOf(context).languageCode;
    final result = await ref.read(authApiProvider).redeemDesktopQr(
          payload: parsed,
          machineName: 'MaclawGo',
          clientId: clientId,
          language: language,
        );
    if (!mounted) return;
    if (!result.confirmed) {
      setState(() {
        _submitting = false;
        _message = result.message.isNotEmpty
            ? result.message
            : context.S.pendingApproval;
      });
      return;
    }
    await ref.read(sessionProvider.notifier).addAccount(result.account!);
    if (!mounted) return;
    // The router's session redirect also fires; going home directly keeps
    // the scan page from flashing during the handoff.
    context.go('/');
  }

  Future<void> _submitAuthorization(String text) async {
    final active = ref.read(sessionProvider).active;
    if (active == null || active.token.isEmpty) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _message = '请先登录 MaClaw 账号，再授权设备';
      });
      return;
    }
    late final ThirdPartyAuthQrPayload parsed;
    try {
      parsed = parseThirdPartyAuthQrPayload(text);
    } on FormatException catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _message = error.message;
      });
      return;
    }
    final clientName = await ref.read(authApiProvider).authorizeThirdParty(
          payload: parsed,
          accountHubUrl: active.hubUrl,
          accessToken: active.token,
        );
    if (!mounted) return;
    final name = clientName.isNotEmpty ? clientName : '第三方设备';
    setState(() {
      _submitting = false;
      _messageIsSuccess = true;
      _message = '已授权 $name 使用你的 MaClaw 官方服务，可以回到电脑端继续。';
    });
  }

  bool get _isAuthorize => widget.mode == QrScanMode.thirdPartyAuthorize;

  String _errorText(Object error) {
    final text = error.toString();
    final S = context.S;
    if (_isAuthorize) {
      if (text.contains('INVALID_QR') || text.contains('QR_EXPIRED') ||
          text.contains('INVALID_DESKTOP_QR')) {
        return '授权二维码无效或已过期，请让电脑端重新生成';
      }
      if (text.contains('UNAUTHORIZED')) {
        return '登录状态已失效，请重新登录后再授权';
      }
      if (text.contains('CLIENT_MISMATCH')) {
        return '此二维码属于另一个客户端，无法授权';
      }
    }
    if (text.contains('INVALID_DESKTOP_QR')) {
      return S.qrInvalid;
    }
    if (text.contains('QR_OWNER_UNAVAILABLE')) {
      return S.qrOwnerUnavailable;
    }
    if (text.contains('PHONE_IDENTITY_REQUIRED')) {
      return S.qrPhoneRequired;
    }
    // An older Hub has no redeem route at all.
    if (text.contains('HTTP_404') || text.contains('NOT_FOUND')) {
      return S.qrHubTooOld;
    }
    return text.isNotEmpty ? text : S.opFailedRetry;
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
      appBar: AppBar(
        title: Text(
          _isAuthorize ? '授权设备' : context.S.scanToSignIn,
        ),
      ),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 28),
          children: [
            Text(
              _isAuthorize ? '扫描电脑端的授权二维码' : context.S.qrScanTitle,
              style: theme.textTheme.titleLarge,
            ),
            const SizedBox(height: 8),
            Text(
              _isAuthorize
                  ? '确认后，该设备将可以使用你账号下的 MaClaw 官方服务。'
                  : context.S.qrScanHint,
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
                  decoration: BoxDecoration(
                    color: scheme.surfaceContainer,
                    border: Border.all(
                      color: scheme.primary.withValues(alpha: 0.45),
                      width: 1.6,
                    ),
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child:
                      widget.scannerBuilder?.call(_submit) ??
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
              decoration: InputDecoration(
                labelText: context.S.qrPasteLabel,
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
              label: Text(
                _submitting
                    ? context.S.signingIn
                    : (_isAuthorize ? '确认授权' : context.S.confirmSignIn),
              ),
            ),
            if (_message != null) ...[
              const SizedBox(height: 12),
              Text(
                _message!,
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: _messageIsSuccess ? scheme.primary : scheme.error,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
