import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/chat_ui.dart';

import 'l10n_test_helper.dart';

Widget _composer({
  VoidCallback? onAttach,
  VoidCallback? onMic,
  bool busy = false,
  List<Widget> attachments = const [],
}) => l10nApp(
  child: Scaffold(
    body: ChatComposer(
      controller: TextEditingController(),
      hint: '输点东西',
      sending: false,
      onSend: () {},
      onAttach: onAttach,
      onMic: onMic,
      busy: busy,
      attachments: attachments,
    ),
  ),
);

void main() {
  group('chat composer attachments', () {
    testWidgets('shows an attach button and a chip row when given both', (
      tester,
    ) async {
      var attaches = 0;
      await tester.pumpWidget(
        _composer(
          onAttach: () => attaches++,
          attachments: const [Chip(label: Text('报告.pdf'))],
        ),
      );
      expect(find.byKey(const ValueKey('composer-attachments')), findsOneWidget);
      expect(find.text('报告.pdf'), findsOneWidget);
      await tester.tap(find.byIcon(Icons.attach_file));
      expect(attaches, 1);
    });

    testWidgets('a busy upload disables attach and mic', (tester) async {
      var attaches = 0;
      var mics = 0;
      await tester.pumpWidget(
        _composer(
          onAttach: () => attaches++,
          onMic: () => mics++,
          busy: true,
        ),
      );
      await tester.tap(find.byIcon(Icons.attach_file));
      await tester.tap(find.byIcon(Icons.mic_none));
      expect(attaches, 0);
      expect(mics, 0);
      expect(
        tester
            .widget<IconButton>(
              find.ancestor(
                of: find.byIcon(Icons.attach_file),
                matching: find.byType(IconButton),
              ),
            )
            .onPressed,
        isNull,
      );
    });

    testWidgets('no chip row without attachments', (tester) async {
      await tester.pumpWidget(_composer(onAttach: () {}));
      expect(find.byKey(const ValueKey('composer-attachments')), findsNothing);
    });
  });
}
