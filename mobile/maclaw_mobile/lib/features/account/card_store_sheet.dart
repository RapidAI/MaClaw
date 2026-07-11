import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/api/api_client.dart';

/// Bottom sheet: list card-store products and create a purchase order.
Future<void> showMobileCardStoreSheet(
  BuildContext context, {
  required ApiClient client,
  required String account,
  String tenantId = '',
}) async {
  await showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (ctx) {
      return _CardStoreSheetBody(
        client: client,
        tenantId: tenantId,
        account: account,
      );
    },
  );
}

class _CardStoreSheetBody extends StatefulWidget {
  final ApiClient client;
  final String tenantId;
  final String account;

  const _CardStoreSheetBody({
    required this.client,
    required this.tenantId,
    required this.account,
  });

  @override
  State<_CardStoreSheetBody> createState() => _CardStoreSheetBodyState();
}

class _CardStoreSheetBodyState extends State<_CardStoreSheetBody> {
  late Future<MobileCardStoreCatalog> _future;
  String? _busyProductId;
  String? _lastError;

  @override
  void initState() {
    super.initState();
    _future = widget.client.listCardStoreProducts(tenantId: widget.tenantId);
  }

  Future<void> _buy(MobileCardStoreProduct product) async {
    if (widget.account.trim().isEmpty) {
      setState(() => _lastError = '当前账号无可用邮箱/手机身份，无法下单');
      return;
    }
    setState(() {
      _busyProductId = product.id;
      _lastError = null;
    });
    try {
      final order = await widget.client.createCardStoreOrder(
        productId: product.id,
        account: widget.account,
        tenantId: widget.tenantId,
      );
      if (!mounted) return;
      final pay = order.payUrl.trim().isNotEmpty
          ? order.payUrl
          : order.payQrUrl.trim();
      if (pay.isNotEmpty) {
        await Clipboard.setData(ClipboardData(text: pay));
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(
                '订单 ${order.orderNo} 已创建。支付链接已复制，请到浏览器打开完成付款。',
              ),
            ),
          );
        }
      } else if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              '订单 ${order.orderNo} 已创建（${order.status}）。请按支付指引完成付款。',
            ),
          ),
        );
      }
      if (mounted) Navigator.of(context).pop();
    } on Object catch (e) {
      if (mounted) {
        setState(() {
          _lastError = '$e';
          _busyProductId = null;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final bottom = MediaQuery.viewInsetsOf(context).bottom;
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.fromLTRB(16, 8, 16, 16 + bottom),
        child: FutureBuilder<MobileCardStoreCatalog>(
          future: _future,
          builder: (context, snap) {
            if (snap.connectionState != ConnectionState.done) {
              return const SizedBox(
                height: 180,
                child: Center(child: CircularProgressIndicator()),
              );
            }
            if (snap.hasError) {
              return Text('加载卡店失败：${snap.error}');
            }
            final catalog = snap.data;
            if (catalog == null || !catalog.enabled) {
              return const Text('当前 Hub 未开启卡店，或暂无可购产品。你仍可使用「兑换服务卡」。');
            }
            if (catalog.products.isEmpty) {
              return const Text('暂无可购产品。');
            }
            return Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  '购买服务卡',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 4),
                Text(
                  '支付模式：${catalog.paymentMode.isEmpty ? "默认" : catalog.paymentMode}',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
                if (_lastError != null) ...[
                  const SizedBox(height: 8),
                  Text(
                    _lastError!,
                    style: TextStyle(color: Theme.of(context).colorScheme.error),
                  ),
                ],
                const SizedBox(height: 12),
                Flexible(
                  child: ListView.separated(
                    shrinkWrap: true,
                    itemCount: catalog.products.length,
                    separatorBuilder: (_, __) => const Divider(height: 1),
                    itemBuilder: (context, index) {
                      final p = catalog.products[index];
                      final busy = _busyProductId == p.id;
                      return ListTile(
                        title: Text(p.label.isEmpty ? p.id : p.label),
                        subtitle: Text(
                          [
                            if (p.durationDays > 0) '${p.durationDays} 天',
                            if (p.credits > 0) '${p.credits.toStringAsFixed(0)} credits',
                            if (p.price > 0) '¥${p.price.toStringAsFixed(2)}',
                          ].join(' · '),
                        ),
                        trailing: FilledButton(
                          onPressed: busy || !p.enabled ? null : () => _buy(p),
                          child: busy
                              ? const SizedBox(
                                  width: 16,
                                  height: 16,
                                  child: CircularProgressIndicator(strokeWidth: 2),
                                )
                              : const Text('购买'),
                        ),
                      );
                    },
                  ),
                ),
              ],
            );
          },
        ),
      ),
    );
  }
}
