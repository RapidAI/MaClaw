import 'dart:async';
import 'dart:math';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../features/auth/session_controller.dart';
import '../api/api_client.dart';
import '../api/mobile_bootstrap.dart';
import '../storage/secure_vault.dart';
import 'mobile_notification_service.dart';

const _pushDeviceTokenKey = 'maclaw.push.device_token';
const _pushDeviceIdKey = 'maclaw.push.device_id';

/// Register this install with Hub after login (device token for pending/webhook).
final mobilePushRegistrationProvider = Provider<void>((ref) {
  final session = ref.watch(sessionControllerProvider).valueOrNull;
  if (session?.authenticated != true) return;
  unawaited(
    registerMobilePushDevice(
      client: ref.read(apiClientProvider),
      services: session?.bootstrap?.services,
      vault: ref.read(secureVaultProvider),
    ),
  );
});

/// Pull offline completion queue and surface local notifications, then ack.
Future<void> syncMobilePushPending({
  required ApiClient? client,
  required MobileNotificationService notify,
  MobileFeatures? features,
  MobileServices? services,
}) async {
  if (client == null) return;
  if (features != null && features.pushPendingSync == false) return;

  try {
    final list = await client.listPushPending(
      path: services?.pushPendingPath ?? '/api/mobile/push/pending',
    );
    if (list.items.isEmpty) return;
    final acked = <String>[];
    for (final item in list.items) {
      final id = item.id.trim();
      if (id.isEmpty) continue;
      await notify.showTaskCompleted(
        title: item.title.isEmpty ? '任务更新' : item.title,
        body: item.body.isEmpty ? item.status : item.body,
        payload: item.payload.trim().isEmpty ? null : item.payload.trim(),
      );
      acked.add(id);
    }
    if (acked.isNotEmpty) {
      await client.ackPushPending(
        ids: acked,
        path: services?.pushPendingAckPath ?? '/api/mobile/push/pending/ack',
      );
    }
  } on Object {
    // Best-effort; online realtime remains primary when connected.
  }
}

Future<void> registerMobilePushDevice({
  required ApiClient? client,
  MobileServices? services,
  SecureVault? vault,
}) async {
  if (client == null) return;
  try {
    final resolvedVault = vault ?? const SecureVault();
    final deviceId = await _ensureStored(resolvedVault, _pushDeviceIdKey, () {
      return 'dev_${_randomHex(16)}';
    });
    final token = await _ensureStored(resolvedVault, _pushDeviceTokenKey, () {
      return 'mtok_${_randomHex(24)}';
    });
    final path = services?.pushDevicesPath ?? '/api/mobile/push/devices';
    // Without firebase_messaging, platform=device associates this install for
    // pending sync + webhook fan-out. Real FCM/APNs tokens can replace later.
    await client.registerPushDevice(
      platform: 'device',
      token: token,
      deviceId: deviceId,
      path: path,
    );
  } on Object {
    // Must not block login or shell.
  }
}

Future<void> syncMobilePushPendingFromRef(Ref ref) async {
  final session = ref.read(sessionControllerProvider).valueOrNull;
  await syncMobilePushPending(
    client: ref.read(apiClientProvider),
    notify: ref.read(mobileNotificationServiceProvider),
    features: session?.bootstrap?.features,
    services: session?.bootstrap?.services,
  );
}

Future<void> registerMobilePushDeviceFromRef(Ref ref) async {
  final session = ref.read(sessionControllerProvider).valueOrNull;
  await registerMobilePushDevice(
    client: ref.read(apiClientProvider),
    services: session?.bootstrap?.services,
    vault: ref.read(secureVaultProvider),
  );
}

Future<String> _ensureStored(
  SecureVault vault,
  String key,
  String Function() create,
) async {
  final existing = await vault.readKey(key);
  if (existing != null && existing.trim().isNotEmpty) {
    return existing.trim();
  }
  final value = create();
  await vault.writeKey(key, value);
  return value;
}

String _randomHex(int bytes) {
  final r = Random.secure();
  final buf = StringBuffer();
  for (var i = 0; i < bytes; i++) {
    buf.write(r.nextInt(256).toRadixString(16).padLeft(2, '0'));
  }
  return buf.toString();
}
