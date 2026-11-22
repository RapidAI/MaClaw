import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/features/remote/remote_attachments.dart';

void main() {
  group('remote attachments', () {
    test('messageWithAttachments keeps the person text and lists paths', () {
      final text = messageWithAttachments('看一下这个', [
        '/home/desktop/attachments/a.png',
        '',
        ' /home/desktop/attachments/b.pdf ',
      ]);
      expect(text, startsWith('看一下这个\n\n附件（2 个）：'));
      expect(text, contains('- /home/desktop/attachments/a.png'));
      expect(text, contains('- /home/desktop/attachments/b.pdf'));
    });

    test('messageWithAttachments without text is only the block', () {
      final text = messageWithAttachments('   ', ['/x/attachments/a.png']);
      expect(text, startsWith('附件：'));
      expect(text, isNot(startsWith('\n')));
    });

    test('messageWithAttachments without paths returns the plain text', () {
      expect(messageWithAttachments(' 继续 ', []), '继续');
    });

    test('a message without attachments is untouched', () {
      const raw = '帮我看下 附件：这个词在文里的用法';
      expect(messageWithAttachments(raw, []), raw);
    });

    test('looksLikeAttachmentMessage only matches the block form', () {
      expect(looksLikeAttachmentMessage('看\n\n附件：\n- /a'), isTrue);
      expect(looksLikeAttachmentMessage('附件：'), isFalse);
    });

    test('stagedProgress clamps and guards a zero total', () {
      expect(stagedProgress(0, 0), 0);
      expect(stagedProgress(5, 10), 0.5);
      expect(stagedProgress(20, 10), 1);
    });

    test('upload queue runs one job at a time in order', () async {
      final queue = RemoteUploadQueue();
      final order = <String>[];
      Future<String> job(String name) async {
        order.add('start:$name');
        await Future<void>.delayed(const Duration(milliseconds: 5));
        order.add('end:$name');
        return name;
      }

      final first = queue.add(() => job('a'));
      final second = queue.add(() => job('b'));
      expect(await first, 'a');
      expect(await second, 'b');
      expect(order, ['start:a', 'end:a', 'start:b', 'end:b']);
    });

    test('a failed upload does not block the next one', () async {
      final queue = RemoteUploadQueue();
      final failing = queue.add<int>(() async => throw StateError('boom'));
      final next = queue.add<int>(() async => 7);
      await expectLater(failing, throwsStateError);
      expect(await next, 7);
    });

    test('upload id starts with a letter and stays short', () {
      final id = remoteAttachmentUploadId(1700000000000000, '报告 v1.pdf');
      expect(RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]{7,63}$').hasMatch(id), isTrue);
      expect(id.length, lessThan(30));
      expect(
        remoteAttachmentUploadId(1, 'a'),
        isNot(remoteAttachmentUploadId(2, 'a')),
      );
      expect(
        remoteAttachmentUploadId(1, 'a'),
        isNot(remoteAttachmentUploadId(1, 'b')),
      );
    });

    test('an outgoing file starts unstaged', () {
      final file = RemoteOutgoingFile(
        name: 'a.png',
        size: 3,
        bytes: Uint8List.fromList([1, 2, 3]),
      );
      expect(file.staged, isFalse);
      expect(file.uploading, isFalse);
      file.path = '/home/desktop/attachments/a.png';
      expect(file.staged, isTrue);
    });
  });
}
