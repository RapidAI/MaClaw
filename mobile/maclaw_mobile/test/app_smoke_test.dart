import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:maclaw_mobile/app.dart';

void main() {
  testWidgets('renders assistant entry', (tester) async {
    await tester.pumpWidget(const ProviderScope(child: MaClawMobileApp()));
    expect(find.text('查信息'), findsWidgets);
    expect(find.textContaining('联网搜索'), findsOneWidget);
  });
}
