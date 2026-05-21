import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('renders smoke widget', (WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(home: Scaffold(body: Text('MaClaw Chat'))),
    );

    expect(find.text('MaClaw Chat'), findsOneWidget);
  });
}
