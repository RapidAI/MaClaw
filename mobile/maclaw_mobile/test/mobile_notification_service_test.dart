import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/notifications/mobile_notification_service.dart';

void main() {
  test('notification payload helpers map to target app tabs', () {
    expect(
      mobileNotificationPayloadBasePath(
        mobileDocumentExportNotificationPayload('job-1'),
      ),
      '/documents',
    );
    expect(
      mobileNotificationPayloadBasePath(
        mobileDigitalEmployeeTaskNotificationPayload('task-1'),
      ),
      '/employees',
    );
    expect(
      mobileNotificationPayloadBasePath(
        mobileServerProfileNotificationPayload('srv-prod'),
      ),
      '/servers',
    );
    expect(mobileNotificationPayloadBasePath('legacy-raw-id'), isNull);
  });

  test('typed notification payloads require trackable ids', () {
    expect(mobileNotificationPayloadBasePath('document-draft:'), isNull);
    expect(mobileNotificationPayloadBasePath('document-upload:   '), isNull);
    expect(mobileNotificationPayloadBasePath('document-export:'), isNull);
    expect(
      mobileNotificationPayloadBasePath('digital-employee-task: '),
      isNull,
    );
    expect(mobileNotificationPayloadBasePath('server-profile:'), isNull);
    expect(
      mobileNotificationPayloadBasePath('https://tenant.example/tasks/job-1'),
      '/documents',
    );
  });

  test('notification display text redacts common secrets', () {
    final text = mobileNotificationDisplayText(
      'SSH task failed token=raw-token password=raw-password\n'
      'Authorization: Bearer raw-bearer\n'
      '-----BEGIN PRIVATE KEY-----\nraw-private-key\n'
      '-----END PRIVATE KEY-----',
    );

    expect(text, contains('token=[REDACTED_SECRET]'));
    expect(text, contains('password=[REDACTED_SECRET]'));
    expect(text, contains('Authorization: Bearer [REDACTED_TOKEN]'));
    expect(text, contains('[REDACTED_PRIVATE_KEY]'));
    expect(text, isNot(contains('raw-token')));
    expect(text, isNot(contains('raw-password')));
    expect(text, isNot(contains('raw-bearer')));
    expect(text, isNot(contains('raw-private-key')));
  });

  test('notification open payload can be recorded and consumed', () {
    final service = MobileNotificationService();

    service.handleNotificationResponse(
      const NotificationResponse(
        notificationResponseType: NotificationResponseType.selectedNotification,
        id: 7,
        payload: '  document-export:job-1  ',
      ),
    );

    expect(service.latestOpenedNotification?.payload, 'document-export:job-1');
    expect(service.latestOpenedNotification?.notificationId, 7);
    expect(service.latestOpenedNotification?.actionId, '');
    expect(service.consumeLastOpenedPayload(), 'document-export:job-1');
    expect(service.latestOpenedNotification, isNull);
    expect(service.consumeLastOpenedPayload(), isNull);
  });

  test('notification open ignores blank payloads', () {
    final service = MobileNotificationService();

    service.handleNotificationResponse(
      const NotificationResponse(
        notificationResponseType: NotificationResponseType.selectedNotification,
        payload: '   ',
      ),
    );

    expect(service.latestOpenedNotification, isNull);
    expect(service.consumeLastOpenedPayload(), isNull);
  });

  test('notification open ignores unroutable typed payloads', () {
    final service = MobileNotificationService();

    for (final payload in const [
      'document-export:',
      'document-upload:   ',
      'digital-employee-task:',
      'server-profile:',
      'legacy-raw-id',
    ]) {
      service.handleNotificationResponse(
        NotificationResponse(
          notificationResponseType:
              NotificationResponseType.selectedNotification,
          payload: payload,
        ),
      );
    }

    expect(service.latestOpenedNotification, isNull);
    expect(service.consumeLastOpenedPayload(), isNull);
  });

  test('invalid notification open does not replace valid pending payload', () {
    final service = MobileNotificationService();

    service.handleNotificationResponse(
      const NotificationResponse(
        notificationResponseType: NotificationResponseType.selectedNotification,
        id: 7,
        payload: 'document-export:job-1',
      ),
    );
    service.handleNotificationResponse(
      const NotificationResponse(
        notificationResponseType: NotificationResponseType.selectedNotification,
        id: 8,
        payload: 'document-export:',
      ),
    );

    expect(service.latestOpenedNotification?.payload, 'document-export:job-1');
    expect(service.latestOpenedNotification?.notificationId, 7);
    expect(service.consumeLastOpenedPayload(), 'document-export:job-1');
  });

  test('notification open keeps action identity for action taps', () {
    final service = MobileNotificationService();

    service.handleNotificationResponse(
      const NotificationResponse(
        notificationResponseType:
            NotificationResponseType.selectedNotificationAction,
        id: 9,
        actionId: ' open-task ',
        payload: 'digital-employee-task:task-1',
      ),
    );

    expect(
      service.latestOpenedNotification?.payload,
      'digital-employee-task:task-1',
    );
    expect(service.latestOpenedNotification?.notificationId, 9);
    expect(service.latestOpenedNotification?.actionId, 'open-task');
  });
}
