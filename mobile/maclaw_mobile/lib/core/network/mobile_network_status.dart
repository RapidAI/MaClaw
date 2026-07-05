import 'dart:async';
import 'dart:io';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/official_service.dart';

enum MobileNetworkQuality { checking, online, offline, restored }

class MobileNetworkSnapshot {
  final MobileNetworkQuality quality;
  final String message;
  final DateTime checkedAt;

  const MobileNetworkSnapshot({
    required this.quality,
    required this.message,
    required this.checkedAt,
  });

  bool get online =>
      quality == MobileNetworkQuality.online ||
      quality == MobileNetworkQuality.restored;
  bool get offline => quality == MobileNetworkQuality.offline;
  bool get restored => quality == MobileNetworkQuality.restored;
}

abstract class MobileNetworkProbe {
  Future<MobileNetworkSnapshot> check();
}

final mobileNetworkProbeProvider = Provider<MobileNetworkProbe>(
  (ref) => HubCenterNetworkProbe(),
);

final mobileNetworkStatusProvider =
    StreamProvider<MobileNetworkSnapshot>((ref) {
  final probe = ref.watch(mobileNetworkProbeProvider);
  return mobileNetworkStatusStream(probe);
});

Stream<MobileNetworkSnapshot> mobileNetworkStatusStream(
  MobileNetworkProbe probe, {
  Duration pollInterval = const Duration(seconds: 30),
}) async* {
  yield MobileNetworkSnapshot(
    quality: MobileNetworkQuality.checking,
    message: '正在检查官方 HubCenter 网络状态。',
    checkedAt: DateTime.now(),
  );
  var wasOffline = false;
  while (true) {
    final snapshot = await _safeProbeCheck(probe);
    if (snapshot.online && wasOffline) {
      yield MobileNetworkSnapshot(
        quality: MobileNetworkQuality.restored,
        message: '官方服务网络已恢复，可以继续使用 AI助手、处理文档和查看任务状态。',
        checkedAt: snapshot.checkedAt,
      );
    } else {
      yield snapshot;
    }
    wasOffline = snapshot.offline;
    await Future<void>.delayed(pollInterval);
  }
}

Future<MobileNetworkSnapshot> _safeProbeCheck(MobileNetworkProbe probe) async {
  try {
    return await probe.check();
  } catch (_) {
    return MobileNetworkSnapshot(
      quality: MobileNetworkQuality.offline,
      message: '当前网络可能不可用，官方 HubCenter 探测失败，移动端任务可能延迟恢复。',
      checkedAt: DateTime.now(),
    );
  }
}

class HubCenterNetworkProbe implements MobileNetworkProbe {
  final Future<List<InternetAddress>> Function(String host) _lookup;
  final Duration timeout;
  final List<String> hubCenterUrls;

  HubCenterNetworkProbe({
    Future<List<InternetAddress>> Function(String host)? lookup,
    this.timeout = const Duration(seconds: 4),
    this.hubCenterUrls = maclawOfficialHubCenterUrls,
  }) : _lookup = lookup ?? InternetAddress.lookup;

  @override
  Future<MobileNetworkSnapshot> check() async {
    final checkedAt = DateTime.now();
    for (final hubCenterUrl in hubCenterUrls) {
      try {
        final host = Uri.parse(hubCenterUrl).host;
        final addresses = await _lookup(host).timeout(timeout);
        final reachable =
            addresses.any((address) => address.rawAddress.isNotEmpty);
        if (reachable) {
          return MobileNetworkSnapshot(
            quality: MobileNetworkQuality.online,
            message: '官方 HubCenter 网络可达。',
            checkedAt: checkedAt,
          );
        }
      } catch (_) {
        // Try the next preset HubCenter. The UI only needs actionable network
        // state, not platform-specific DNS/socket error detail.
      }
    }
    return MobileNetworkSnapshot(
      quality: MobileNetworkQuality.offline,
      message: '当前网络可能不可用，AI助手、文档导入/导出和数字员工任务状态可能延迟。',
      checkedAt: checkedAt,
    );
  }
}
