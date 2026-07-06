import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/shared_intents/mobile_shared_intent.dart';
import 'package:maclaw_mobile/core/shared_intents/shared_intent_bootstrap.dart';

const _keepSourceCitation = '\u4fdd\u7559\u6765\u6e90\u5f15\u7528';
const _sharedMessageLabel = '\u5206\u4eab\u9644\u5e26\u8bf4\u660e';
const _incidentReviewText =
    '\u770b\u770b\u8fd9\u4e2a\u4e8b\u6545\u590d\u76d8\uff1a'
    'https://example.com/report?from=im';
const _incidentReviewPrefix =
    '\u770b\u770b\u8fd9\u4e2a\u4e8b\u6545\u590d\u76d8';
const _runbookMessage =
    '\u8bf7\u67e5\u8fd9\u4e2a\u94fe\u63a5 https://example.com/runbook';
const _fileMessageWithUrl =
    '\u8bf7\u6309\u8fd9\u4efd\u9644\u4ef6\u5904\u7406\uff0c'
    '\u80cc\u666f\u89c1 https://example.com/context';

void main() {
  test('classifies shared links for assistant conversation', () {
    final intent = MobileSharedIntent.fromMedia(
      value: 'https://example.com/report',
      typeName: 'url',
    );

    expect(intent.kind, MobileSharedIntentKind.link);
    expect(intent.opensAssistant, isTrue);
    expect(intent.opensDocuments, isFalse);
    expect(intent.assistantPrompt, contains(_keepSourceCitation));
  });

  test('extracts links from shared text with context', () {
    final intent = MobileSharedIntent.fromMedia(
      value: _incidentReviewText,
      typeName: 'text',
      mimeType: 'text/plain',
    );

    expect(intent.kind, MobileSharedIntentKind.link);
    expect(intent.sharedUrl, 'https://example.com/report?from=im');
    expect(intent.opensAssistant, isTrue);
    expect(
      intent.assistantPrompt,
      contains('https://example.com/report?from=im'),
    );
    expect(intent.assistantPrompt, contains(_sharedMessageLabel));
    expect(intent.assistantPrompt, contains(_incidentReviewPrefix));
  });

  test('redacts shared assistant prompt secrets', () {
    final intent = MobileSharedIntent.fromMedia(
      value: 'investigate token=raw-share-token password=raw-share-password',
      typeName: 'text',
      mimeType: 'text/plain',
    );

    expect(intent.kind, MobileSharedIntentKind.text);
    expect(intent.assistantPrompt, contains('token=[REDACTED_SECRET]'));
    expect(intent.assistantPrompt, contains('password=[REDACTED_SECRET]'));
    expect(intent.assistantPrompt, isNot(contains('raw-share-token')));
    expect(intent.assistantPrompt, isNot(contains('raw-share-password')));
  });

  test('redacts shared link credentials and message secrets', () {
    final intent = MobileSharedIntent.fromMedia(
      value: 'https://user:raw-url-password@example.com/runbook',
      typeName: 'url',
      message: 'Authorization: Bearer raw-share-bearer',
    );

    expect(intent.kind, MobileSharedIntentKind.link);
    expect(
      intent.assistantPrompt,
      contains('https://[REDACTED_CREDENTIALS]@example.com/runbook'),
    );
    expect(
      intent.assistantPrompt,
      contains('Authorization: Bearer [REDACTED_TOKEN]'),
    );
    expect(intent.assistantPrompt, isNot(contains('raw-url-password')));
    expect(intent.assistantPrompt, isNot(contains('raw-share-bearer')));
  });

  test('classifies shared images for document import', () {
    final intent = MobileSharedIntent.fromMedia(
      value: '/tmp/capture.png',
      typeName: 'image',
      mimeType: 'image/png',
    );

    expect(intent.kind, MobileSharedIntentKind.image);
    expect(intent.opensDocuments, isTrue);
    expect(intent.opensAssistant, isFalse);
  });

  test('keeps shared files and images in document import when message has URL',
      () {
    final document = MobileSharedIntent.fromMedia(
      value: '/tmp/incident.pdf',
      typeName: 'file',
      mimeType: 'application/pdf',
      message: _fileMessageWithUrl,
    );
    final image = MobileSharedIntent.fromMedia(
      value: '/tmp/site-photo.jpg',
      typeName: 'image',
      mimeType: 'image/jpeg',
      message: _fileMessageWithUrl,
    );

    expect(document.kind, MobileSharedIntentKind.file);
    expect(document.opensDocuments, isTrue);
    expect(document.opensAssistant, isFalse);
    expect(document.sharedUrl, 'https://example.com/context');
    expect(image.kind, MobileSharedIntentKind.image);
    expect(image.opensDocuments, isTrue);
    expect(image.opensAssistant, isFalse);
    expect(image.sharedUrl, 'https://example.com/context');
  });

  test('classifies office and tabular shared files for document import', () {
    const cases = [
      (
        path: '/tmp/incident.pdf',
        mimeType: 'application/pdf',
      ),
      (
        path: '/tmp/notice.docx',
        mimeType:
            'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      ),
      (
        path: '/tmp/legacy-notice.doc',
        mimeType: 'application/msword',
      ),
      (
        path: '/tmp/incident.xlsx',
        mimeType:
            'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      ),
      (
        path: '/tmp/legacy-incident.xls',
        mimeType: 'application/vnd.ms-excel',
      ),
      (
        path: '/tmp/incident.csv',
        mimeType: 'text/csv',
      ),
    ];

    for (final item in cases) {
      final intent = MobileSharedIntent.fromMedia(
        value: item.path,
        typeName: 'file',
        mimeType: item.mimeType,
      );

      expect(intent.kind, MobileSharedIntentKind.file);
      expect(intent.opensDocuments, isTrue);
      expect(intent.opensAssistant, isFalse);
      expect(intent.value, item.path);
      expect(intent.mimeType, item.mimeType);
    }
  });

  test('builds first non-empty shared payload from plugin media', () {
    final intent = MobileSharedIntent.fromPayloads(
      const [
        MobileSharedIntentPayload(value: '   ', typeName: 'file'),
        MobileSharedIntentPayload(
          value: '/tmp/incident.xlsx',
          typeName: 'file',
          mimeType:
              'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
        ),
      ],
      receivedAt: DateTime.utc(2026, 7, 2),
    );

    expect(intent, isNotNull);
    expect(intent!.kind, MobileSharedIntentKind.file);
    expect(intent.opensDocuments, isTrue);
    expect(intent.value, '/tmp/incident.xlsx');
    expect(intent.mimeType, contains('spreadsheetml'));
  });

  test('prefers shared files over leading text captions from plugin media', () {
    final intent = MobileSharedIntent.fromPayloads(
      const [
        MobileSharedIntentPayload(
          value: _fileMessageWithUrl,
          typeName: 'text',
          mimeType: 'text/plain',
        ),
        MobileSharedIntentPayload(
          value: '/tmp/incident.pdf',
          typeName: 'file',
          mimeType: 'application/pdf',
          message: _fileMessageWithUrl,
        ),
      ],
    );

    expect(intent, isNotNull);
    expect(intent!.kind, MobileSharedIntentKind.file);
    expect(intent.opensDocuments, isTrue);
    expect(intent.opensAssistant, isFalse);
    expect(intent.value, '/tmp/incident.pdf');
    expect(intent.sharedUrl, 'https://example.com/context');
  });

  test('falls back to assistant text for unsupported files with message', () {
    final intent = MobileSharedIntent.fromPayloads(
      const [
        MobileSharedIntentPayload(
          value: '/tmp/server-backup.zip',
          typeName: 'file',
          mimeType: 'application/zip',
          message: _runbookMessage,
        ),
      ],
    );

    expect(intent, isNotNull);
    expect(intent!.kind, MobileSharedIntentKind.link);
    expect(intent.opensAssistant, isTrue);
    expect(intent.opensDocuments, isFalse);
    expect(intent.value, _runbookMessage);
    expect(intent.sharedUrl, 'https://example.com/runbook');
  });

  test('prefers importable file over unsupported attachment in share batch',
      () {
    final intent = MobileSharedIntent.fromPayloads(
      const [
        MobileSharedIntentPayload(
          value: '/tmp/server-backup.zip',
          typeName: 'file',
          mimeType: 'application/zip',
          message: _fileMessageWithUrl,
        ),
        MobileSharedIntentPayload(
          value: '/tmp/incident.pdf',
          typeName: 'file',
          mimeType: 'application/pdf',
          message: _fileMessageWithUrl,
        ),
      ],
    );

    expect(intent, isNotNull);
    expect(intent!.kind, MobileSharedIntentKind.file);
    expect(intent.opensDocuments, isTrue);
    expect(intent.value, '/tmp/incident.pdf');
  });

  test('treats empty file payload paths with message as assistant text', () {
    final intent = MobileSharedIntent.fromPayloads(
      const [
        MobileSharedIntentPayload(
          value: '',
          typeName: 'file',
          mimeType: 'application/pdf',
          message: _runbookMessage,
        ),
      ],
    );

    expect(intent, isNotNull);
    expect(intent!.kind, MobileSharedIntentKind.link);
    expect(intent.opensAssistant, isTrue);
    expect(intent.opensDocuments, isFalse);
    expect(intent.value, _runbookMessage);
    expect(intent.sharedUrl, 'https://example.com/runbook');
  });

  test('uses shared message when plugin path is empty', () {
    final intent = MobileSharedIntent.fromPayloads(
      const [
        MobileSharedIntentPayload(
          value: '',
          typeName: 'text',
          mimeType: 'text/plain',
          message: _runbookMessage,
        ),
      ],
    );

    expect(intent, isNotNull);
    expect(intent!.kind, MobileSharedIntentKind.link);
    expect(intent.opensAssistant, isTrue);
    expect(intent.sharedUrl, 'https://example.com/runbook');
    expect(intent.assistantPrompt, contains(_sharedMessageLabel));
  });

  test('ignores empty shared payload batches', () {
    final intent = MobileSharedIntent.fromPayloads(
      const [
        MobileSharedIntentPayload(value: ' ', typeName: 'text', message: ' '),
      ],
    );

    expect(intent, isNull);
  });

  test('shared intent controller suppresses immediate duplicate payloads', () {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    final controller = container.read(mobileSharedIntentProvider.notifier);
    final first = MobileSharedIntent(
      id: 'initial-media',
      kind: MobileSharedIntentKind.file,
      value: '/tmp/incident.pdf',
      mimeType: 'application/pdf',
      message: _fileMessageWithUrl,
      receivedAt: DateTime.utc(2026, 7, 2, 8),
    );
    final duplicateFromStream = MobileSharedIntent(
      id: 'stream-media',
      kind: MobileSharedIntentKind.file,
      value: ' /tmp/incident.pdf ',
      mimeType: 'application/pdf',
      message: _fileMessageWithUrl,
      receivedAt: DateTime.utc(2026, 7, 2, 8, 0, 2),
    );

    expect(controller.accept(first), isTrue);
    controller.clear(first.id);
    expect(controller.accept(duplicateFromStream), isFalse);

    expect(container.read(mobileSharedIntentProvider), isNull);
  });

  test('shared intent controller allows same payload after duplicate window',
      () {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    final controller = container.read(mobileSharedIntentProvider.notifier);
    final first = MobileSharedIntent(
      id: 'first-share',
      kind: MobileSharedIntentKind.link,
      value: 'https://example.com/runbook',
      receivedAt: DateTime.utc(2026, 7, 2, 8),
    );
    final later = MobileSharedIntent(
      id: 'later-share',
      kind: MobileSharedIntentKind.link,
      value: 'https://example.com/runbook',
      receivedAt: DateTime.utc(2026, 7, 2, 8, 0, 4),
    );

    expect(controller.accept(first), isTrue);
    controller.clear(first.id);
    expect(controller.accept(later), isTrue);

    expect(container.read(mobileSharedIntentProvider)?.id, 'later-share');
  });
}
