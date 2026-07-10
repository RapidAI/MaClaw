import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'app.dart';
import 'core/notifications/mobile_notification_service.dart';
import 'core/shared_intents/shared_intent_bootstrap.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final notifications = MobileNotificationService();
  await notifications.initialize();
  runApp(
    ProviderScope(
      overrides: [
        mobileNotificationServiceProvider.overrideWithValue(notifications),
      ],
      child: const SharedIntentBootstrap(child: MaClawMobileApp()),
    ),
  );
}
