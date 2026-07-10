import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../security/mobile_redaction.dart';

final mobileNotificationServiceProvider =
    Provider<MobileNotificationService>((ref) => MobileNotificationService());

const mobileDocumentDraftNotificationPrefix = 'document-draft:';
const mobileDocumentUploadNotificationPrefix = 'document-upload:';
const mobileDocumentExportNotificationPrefix = 'document-export:';
const mobileDigitalEmployeeTaskNotificationPrefix = 'digital-employee-task:';
const mobileServerProfileNotificationPrefix = 'server-profile:';
const mobileAssistantTaskNotificationPrefix = 'assistant-task:';

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

String mobileAssistantTaskNotificationPayload(String taskId) =>
    '$mobileAssistantTaskNotificationPrefix${taskId.trim()}';

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
  if (_hasTypedNotificationId(value, mobileAssistantTaskNotificationPrefix)) {
    return '/assistant';
  }
  if (value.startsWith('http://') || value.startsWith('https://')) {
    return '/documents';
  }
  return null;
}

String mobileNotificationDisplayText(String text) {
  return redactMobileSensitiveText(text.trim());
}

bool _hasTypedNotificationId(String value, String prefix) {
  if (!value.startsWith(prefix)) return false;
  return value.substring(prefix.length).trim().isNotEmpty;
}

class MobileNotificationPermissionResult {
  final bool? androidGranted;
  final bool? iosGranted;
  final String? grantId;

  const MobileNotificationPermissionResult({
    this.androidGranted,
    this.iosGranted,
    this.grantId,
  });

  bool get hasPlatformResult => androidGranted != null || iosGranted != null;

  bool get granted =>
      androidGranted == true || iosGranted == true || !hasPlatformResult;

  String get message {
    final suffix = grantId == null || grantId!.trim().isEmpty
        ? ''
        : '（permission-grant:${grantId!.trim()}）';
    if (!hasPlatformResult) {
      return '当前平台无需额外通知授权，任务提醒已准备就绪$suffix';
    }
    if (granted) {
      return '通知权限已开启，长任务和 SSH 异常会提醒你$suffix';
    }
    return '系统未授予通知权限，请在系统设置中开启 MaClaw Mobile 通知$suffix';
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
    // Permission is requested explicitly from AccountScreen, after login.
    const darwin = DarwinInitializationSettings(
      requestAlertPermission: false,
      requestBadgePermission: false,
      requestSoundPermission: false,
    );
    const settings = InitializationSettings(android: android, iOS: darwin);
    try {
      await _plugin.initialize(
        settings: settings,
        onDidReceiveNotificationResponse: handleNotificationResponse,
      );
      _initialized = true;
    } on Object {
      // A broken optional notification provider must not block login or the
      // assistant shell. The next notification attempt may retry setup.
      _initialized = false;
    }
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
    if (mobileNotificationPayloadBasePath(payload) == null) return;
    _latestOpenedNotification = MobileNotificationOpen(
      payload: payload,
      notificationId: response.id,
      actionId: response.actionId?.trim() ?? '',
      openedAt: DateTime.now().toUtc(),
    );
  }

  Future<MobileNotificationPermissionResult> requestPermissions() async {
    bool? androidGranted;
    try {
      final android = _plugin.resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin>();
      if (android != null) {
        androidGranted = await android.requestNotificationsPermission();
      }
    } on Object {
      androidGranted = false;
    }

    bool? iosGranted;
    try {
      final ios = _plugin.resolvePlatformSpecificImplementation<
          IOSFlutterLocalNotificationsPlugin>();
      if (ios != null) {
        iosGranted = await ios.requestPermissions(
          alert: true,
          badge: true,
          sound: true,
        );
      }
    } on Object {
      iosGranted = false;
    }
    return MobileNotificationPermissionResult(
      androidGranted: androidGranted,
      iosGranted: iosGranted,
      grantId: 'notification-${DateTime.now().toUtc().millisecondsSinceEpoch}',
    );
  }

  Future<void> showTaskCompleted({
    required String title,
    required String body,
    String? payload,
  }) async {
    await initialize();
    if (!_initialized) return;
    const details = NotificationDetails(
      android: AndroidNotificationDetails(
        'maclaw_mobile_tasks',
        'MaClaw Mobile tasks',
        channelDescription:
            'Document, assistant, SSH, and digital employee updates',
        importance: Importance.high,
        priority: Priority.high,
      ),
      iOS: DarwinNotificationDetails(),
    );
    try {
      await _plugin.show(
        id: DateTime.now().millisecondsSinceEpoch.remainder(100000),
        title: mobileNotificationDisplayText(title),
        body: mobileNotificationDisplayText(body),
        notificationDetails: details,
        payload: payload,
      );
    } on Object {
      // Notifications are auxiliary feedback; task state remains in Hub/local
      // stores when the platform provider is unavailable.
    }
  }
}
