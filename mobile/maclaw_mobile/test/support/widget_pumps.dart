import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

/// Advance the tester through async work without [pumpAndSettle].
///
/// Flutter 3.44+ treats indeterminate progress indicators, cursor blink, and
/// realtime reconnect timers as never-settling animations, so `pumpAndSettle`
/// times out on otherwise-idle screens.
Future<void> pumpQuietly(
  WidgetTester tester, {
  Duration elapsed = const Duration(milliseconds: 300),
}) async {
  await tester.pump();
  await tester.pump(elapsed);
}

/// Drop the pumped tree so Riverpod/realtime timers are cancelled.
Future<void> disposePumpedTree(WidgetTester tester) async {
  await tester.pumpWidget(const SizedBox.shrink());
  await tester.pump(const Duration(milliseconds: 50));
}
