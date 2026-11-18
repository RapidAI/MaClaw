import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../core/locale.dart';
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
      final result = await ref
          .read(authApiProvider)
          .redeemDesktopQr(
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
    final S = context.S;
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
      appBar: AppBar(title: Text(context.S.scanToSignIn)),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 28),
          children: [
            Text(context.S.qrScanTitle, style: theme.textTheme.titleLarge),
            const SizedBox(height: 8),
            Text(
              context.S.qrScanHint,
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
                _submitting ? context.S.signingIn : context.S.confirmSignIn,
              ),
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
