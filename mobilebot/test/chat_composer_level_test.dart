import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/chat_ui.dart';

import 'l10n_test_helper.dart';

Widget _composer({
  required bool listening,
  ValueNotifier<double?>? level,
}) {
  return l10nApp(
    child: Scaffold(
      body: ChatComposer(
        controller: TextEditingController(),
        hint: 'hint',
        sending: false,
        onSend: () {},
        onMic: () {},
        listening: listening,
        level: level,
      ),
    ),
  );
}

void main() {
  testWidgets('idle mic has neither pulse nor level bar', (tester) async {
    await tester.pumpWidget(_composer(listening: false));
    expect(find.byIcon(Icons.mic_none), findsOneWidget);
    expect(find.byKey(const ValueKey('listen-pulse')), findsNothing);
    expect(find.byKey(const ValueKey('listen-level')), findsNothing);
  });

  testWidgets('system listen with no level pulses instead of a meter', (
    tester,
  ) async {
    await tester.pumpWidget(_composer(listening: true));
    expect(find.byIcon(Icons.mic), findsOneWidget);
    expect(find.byKey(const ValueKey('listen-pulse')), findsOneWidget);
    expect(find.byKey(const ValueKey('listen-level-bar')), findsNothing);
    await tester.pump(const Duration(milliseconds: 450));
    expect(find.byKey(const ValueKey('listen-pulse')), findsOneWidget);
  });

  testWidgets('injected PCM level draws a bar and follows updates', (
    tester,
  ) async {
    final level = ValueNotifier<double?>(0.25);
    addTearDown(level.dispose);
    await tester.pumpWidget(_composer(listening: true, level: level));
    expect(find.byKey(const ValueKey('listen-level-bar')), findsOneWidget);
    expect(find.byKey(const ValueKey('listen-pulse')), findsNothing);

    final quiet = tester.getSize(find.byKey(const ValueKey('listen-level-bar')));
    level.value = 1;
    await tester.pump();
    final loud = tester.getSize(find.byKey(const ValueKey('listen-level-bar')));
    expect(loud.height, greaterThan(quiet.height));

    level.value = null;
    await tester.pump();
    expect(find.byKey(const ValueKey('listen-pulse')), findsOneWidget);
    expect(find.byKey(const ValueKey('listen-level-bar')), findsNothing);
  });

  testWidgets('listen status stays above the input', (tester) async {
    await tester.pumpWidget(
      l10nApp(
        child: Scaffold(
          body: ChatComposer(
            controller: TextEditingController(),
            hint: 'hint',
            sending: false,
            onSend: () {},
            onMic: () {},
            listening: true,
            listenStatus: '使用本地识别…',
          ),
        ),
      ),
    );
    expect(find.byKey(const ValueKey('listen-status')), findsOneWidget);
    expect(find.text('使用本地识别…'), findsOneWidget);
    final status = tester.getTopLeft(find.byKey(const ValueKey('listen-status')));
    final field = tester.getTopLeft(find.byType(TextField));
    expect(status.dy, lessThan(field.dy));
  });
}
