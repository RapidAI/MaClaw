import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../core/api/desktop_llm_qr.dart';
import '../auth/session_controller.dart';

typedef LlmQrPayloadScannerBuilder = Widget Function(
  ValueChanged<String> onPayload,
);

class LlmQrAuthorizationScreen extends ConsumerStatefulWidget {
  final Widget? scanner;
  final LlmQrPayloadScannerBuilder? scannerBuilder;

  const LlmQrAuthorizationScreen({
    super.key,
    this.scanner,
    this.scannerBuilder,
  });

  @override
  ConsumerState<LlmQrAuthorizationScreen> createState() =>
      _LlmQrAuthorizationScreenState();
}

class _LlmQrAuthorizationScreenState
    extends ConsumerState<LlmQrAuthorizationScreen> {
  final _manualController = TextEditingController();
  var _submitting = false;
  String? _message;

  @override
  void dispose() {
    _manualController.dispose();
    super.dispose();
  }

  Future<void> _submit(String payload) async {
    final text = payload.trim();
    if (text.isEmpty || _submitting) return;
    try {
      parseMaclawDesktopLlmQrPayload(text);
    } on FormatException catch (error) {
      setState(() {
        _message = '授权失败：${error.message}';
      });
      return;
    }
    setState(() {
      _submitting = true;
      _message = null;
    });
    try {
      await ref
          .read(sessionControllerProvider.notifier)
          .authorizeThirdPartyLlmWithDesktopQr(text);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('第三方 LLM 授权已接入')),
      );
      Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _message = '授权失败：$error';
      });
    }
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
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      appBar: AppBar(
        title: const Text('桌面二维码授权'),
      ),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 28),
          children: [
            Text(
              '扫描 MaClaw 桌面 GUI 生成的二维码',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const SizedBox(height: 8),
            Text(
              '移动端默认使用 MaClaw 官方 LLM。只有扫描或粘贴桌面 GUI 生成的授权二维码后，才会通过你的 Hub 接入第三方 LLM。',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 16),
            AspectRatio(
              aspectRatio: 1,
              child: ClipRRect(
                borderRadius: BorderRadius.circular(8),
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
              controller: _manualController,
              minLines: 2,
              maxLines: 4,
              decoration: const InputDecoration(
                labelText: '粘贴二维码内容',
                prefixIcon: Icon(Icons.qr_code_2_outlined),
              ),
            ),
            const SizedBox(height: 12),
            FilledButton.icon(
              onPressed:
                  _submitting ? null : () => _submit(_manualController.text),
              icon: _submitting
                  ? const SizedBox.square(
                      dimension: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.verified_outlined),
              label: Text(_submitting ? '授权中' : '确认授权'),
            ),
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
