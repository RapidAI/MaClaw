import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

final mobileNotificationServiceProvider =
    Provider<MobileNotificationService>((ref) => MobileNotificationService());

const mobileDocumentDraftNotificationPrefix = 'document-draft:';
const mobileDocumentUploadNotificationPrefix = 'document-upload:';
const mobileDocumentExportNotificationPrefix = 'document-export:';
const mobileDigitalEmployeeTaskNotificationPrefix = 'digital-employee-task:';
const mobileServerProfileNotificationPrefix = 'server-profile:';

String mobileDocumentDraftNotificationPayload(String draftId) =>
    '$mobileDocumentDraftNotificationPrefix${draftId.trim()}';

String mobileDocumentUploadNotificationPayload(String taskId) =>
    '$mobileDocumentUploadNotificationPrefix${taskId.trim()}';

String mobileDocumentExportNotificationPayload(String jobId) =>
    '$mobileDocumentExportNotificationPrefix${jobId.trim()}';

String mobileDigitalEmployeeTaskNotificationPayload(String taskId) =>
    '$mobileDigitalEmployeeTaskNotificationPrefix${taskId.trim()}';

String mobileServerProfileNotificationPayload(String profileId) =>
    '$mobileServerProfileNotificationPrefix${profileId.trim()}';

String? mobileNotificationPayloadBasePath(String payload) {
  final value = payload.trim();
  if (_hasTypedNotificationId(value, mobileDocumentDraftNotificationPrefix) ||
      _hasTypedNotificationId(value, mobileDocumentUploadNotificationPrefix) ||
      _hasTypedNotificationId(value, mobileDocumentExportNotificationPrefix)) {
    return '/documents';
  }
  if (_hasTypedNotificationId(
    value,
    mobileDigitalEmployeeTaskNotificationPrefix,
  )) {
    return '/employees';
  }
  if (_hasTypedNotificationId(value, mobileServerProfileNotificationPrefix)) {
    return '/servers';
  }
  if (value.startsWith('http://') || value.startsWith('https://')) {
    return '/documents';
  }
  return null;
}

bool _hasTypedNotificationId(String value, String prefix) {
  if (!value.startsWith(prefix)) return false;
  return value.substring(prefix.length).trim().isNotEmpty;
}

class MobileNotificationPermissionResult {
  final bool? androidGranted;
  final bool? iosGranted;

  const MobileNotificationPermissionResult({
    this.androidGranted,
    this.iosGranted,
  });

  bool get hasPlatformResult => androidGranted != null || iosGranted != null;

  bool get granted =>
      androidGranted == true || iosGranted == true || !hasPlatformResult;

  String get message {
    if (!hasPlatformResult) {
      return '当前平台无需额外通知授权，任务提醒已准备就绪';
    }
    if (granted) {
      return '通知权限已开启，长任务和 SSH 异常会提醒你';
    }
    return '系统未授予通知权限，请在系统设置中开启 MaClaw Mobile 通知';
  }
}

class MobileNotificationOpen {
  final String payload;
  final int? notificationId;
  final String actionId;
  final DateTime openedAt;

  const MobileNotificationOpen({
    required this.payload,
    required this.notificationId,
    required this.actionId,
    required this.openedAt,
  });
}

class MobileNotificationService {
  final FlutterLocalNotificationsPlugin _plugin;
  bool _initialized = false;
  MobileNotificationOpen? _latestOpenedNotification;

  MobileNotificationService({
    FlutterLocalNotificationsPlugin? plugin,
  }) : _plugin = plugin ?? FlutterLocalNotificationsPlugin();

  Future<void> initialize() async {
    if (_initialized) return;
    const android = AndroidInitializationSettings('@mipmap/ic_launcher');
    const darwin = DarwinInitializationSettings(
      requestAlertPermission: true,
      requestBadgePermission: true,
      requestSoundPermission: true,
    );
    const settings = InitializationSettings(android: android, iOS: darwin);
    await _plugin.initialize(
      settings: settings,
      onDidReceiveNotificationResponse: handleNotificationResponse,
    );
    await requestPermissions();
    _initialized = true;
  }

  MobileNotificationOpen? get latestOpenedNotification =>
      _latestOpenedNotification;

  String? consumeLastOpenedPayload() {
    final payload = _latestOpenedNotification?.payload;
    _latestOpenedNotification = null;
    return payload;
  }

  void handleNotificationResponse(NotificationResponse response) {
    final payload = response.payload?.trim() ?? '';
    if (payload.isEmpty) return;
    _latestOpenedNotification = MobileNotificationOpen(
      payload: payload,
      notificationId: response.id,
      actionId: response.actionId?.trim() ?? '',
      openedAt: DateTime.now().toUtc(),
    );
  }

  Future<MobileNotificationPermissionResult> requestPermissions() async {
    final androidGranted = await _plugin
        .resolvePlatformSpecificImplementation<
            AndroidFlutterLocalNotificationsPlugin>()
        ?.requestNotificationsPermission();
    final iosGranted = await _plugin
        .resolvePlatformSpecificImplementation<
            IOSFlutterLocalNotificationsPlugin>()
        ?.requestPermissions(
          alert: true,
          badge: true,
          sound: true,
        );
    return MobileNotificationPermissionResult(
      androidGranted: androidGranted,
      iosGranted: iosGranted,
    );
  }

  Future<void> showTaskCompleted({
    required String title,
    required String body,
    String? payload,
  }) async {
    await initialize();
    const details = NotificationDetails(
      android: AndroidNotificationDetails(
        'maclaw_mobile_tasks',
        'MaClaw Mobile tasks',
        channelDescription:
            'Document, search, SSH, and digital employee updates',
        importance: Importance.high,
        priority: Priority.high,
      ),
      iOS: DarwinNotificationDetails(),
    );
    await _plugin.show(
      id: DateTime.now().millisecondsSinceEpoch.remainder(100000),
      title: title,
      body: body,
      notificationDetails: details,
      payload: payload,
    );
  }
}
