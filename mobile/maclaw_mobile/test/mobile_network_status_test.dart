import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/network/mobile_network_status.dart';
import 'package:maclaw_mobile/shared/surface.dart';

void main() {
  test('HubCenter network probe reports online when any DNS resolves',
      () async {
    final probe = HubCenterNetworkProbe(
      hubCenterUrls: const [
        'https://offline.maclaw.invalid',
        'https://hubs.maclaw.top',
      ],
      lookup: (host) async {
        if (host == 'offline.maclaw.invalid') {
          throw const SocketException('offline');
        }
        return [InternetAddress.loopbackIPv4];
      },
    );

    final snapshot = await probe.check();

    expect(snapshot.online, isTrue);
    expect(snapshot.message, contains('HubCenter'));
  });

  test('HubCenter network probe reports offline when every lookup fails',
      () async {
    final probe = HubCenterNetworkProbe(
      lookup: (_) => Future.error(const SocketException('offline')),
    );

    final snapshot = await probe.check();

    expect(snapshot.offline, isTrue);
    expect(snapshot.message, contains('当前网络可能不可用'));
  });

  testWidgets('screen scaffold shows offline network banner', (tester) async {
    final offline = MobileNetworkSnapshot(
      quality: MobileNetworkQuality.offline,
      message: '当前网络可能不可用，搜索、文档导入/导出和数字员工任务状态可能延迟。',
      checkedAt: DateTime.utc(2026, 7, 1),
    );

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream<MobileNetworkSnapshot>.value(offline),
          ),
        ],
        child: const MaterialApp(
          home: Scaffold(
            body: ScreenScaffold(
              title: '应急工具',
              subtitle: '移动端状态',
              children: [Text('正文')],
            ),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.textContaining('当前网络可能不可用'), findsOneWidget);
    expect(find.byIcon(Icons.wifi_off_outlined), findsOneWidget);
  });
}
