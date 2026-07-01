import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:maclaw_mobile/app.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';

class _SignedOutSessionController extends SessionController {
  @override
  Future<SessionState> build() async => const SessionState.signedOut();
}

void main() {
  testWidgets('renders login entry before Hub authentication', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionControllerProvider.overrideWith(
            _SignedOutSessionController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();
    expect(find.text('MaClaw Mobile'), findsOneWidget);
    expect(find.textContaining('官方服务'), findsOneWidget);
    expect(find.text('发送登录确认'), findsOneWidget);
  });
}
