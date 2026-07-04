import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/network/mobile_network_status.dart';
import 'package:maclaw_mobile/shared/surface.dart';

class _SequenceNetworkProbe implements MobileNetworkProbe {
  final List<Object> snapshots;
  var index = 0;

  _SequenceNetworkProbe(this.snapshots);

  @override
  Future<MobileNetworkSnapshot> check() async {
    final current = snapshots[index.clamp(0, snapshots.length - 1)];
    index += 1;
    if (current is Exception) {
      throw current;
    }
    return current as MobileNetworkSnapshot;
  }
}

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

  test('network status stream marks connectivity restored after offline',
      () async {
    final stream = mobileNetworkStatusStream(
      _SequenceNetworkProbe([
        MobileNetworkSnapshot(
          quality: MobileNetworkQuality.offline,
          message: 'offline',
          checkedAt: DateTime.utc(2026, 7, 1),
        ),
        MobileNetworkSnapshot(
          quality: MobileNetworkQuality.online,
          message: 'online',
          checkedAt: DateTime.utc(2026, 7, 1, 0, 1),
        ),
      ]),
      pollInterval: Duration.zero,
    );

    final events = await stream.take(3).toList();

    expect(events[0].quality, MobileNetworkQuality.checking);
    expect(events[1].offline, isTrue);
    expect(events[2].restored, isTrue);
    expect(events[2].online, isTrue);
    expect(events[2].message, contains('官方服务网络已恢复'));
  });

  test('network status stream converts probe errors into offline state',
      () async {
    final stream = mobileNetworkStatusStream(
      _SequenceNetworkProbe([
        const SocketException('dns failed'),
        MobileNetworkSnapshot(
          quality: MobileNetworkQuality.online,
          message: 'online',
          checkedAt: DateTime.utc(2026, 7, 1, 0, 1),
        ),
      ]),
      pollInterval: Duration.zero,
    );

    final events = await stream.take(3).toList();

    expect(events[0].quality, MobileNetworkQuality.checking);
    expect(events[1].offline, isTrue);
    expect(events[1].message, contains('HubCenter 探测失败'));
    expect(events[2].restored, isTrue);
    expect(events[2].message, contains('官方服务网络已恢复'));
  });

  testWidgets('screen scaffold shows restored network banner', (tester) async {
    final restored = MobileNetworkSnapshot(
      quality: MobileNetworkQuality.restored,
      message: '官方服务网络已恢复，可以继续搜索、处理文档和查看任务状态。',
      checkedAt: DateTime.utc(2026, 7, 1, 0, 1),
    );

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream<MobileNetworkSnapshot>.value(restored),
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

    expect(find.textContaining('官方服务网络已恢复'), findsOneWidget);
    expect(find.byIcon(Icons.wifi_outlined), findsOneWidget);
  });
}
