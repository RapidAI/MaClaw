import 'package:flutter/material.dart';

import '../../shared/theme.dart';

const maclawLogoAsset = 'assets/images/maclaw_logo.png';

class StartupSplashScreen extends StatelessWidget {
  const StartupSplashScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.all(32),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                DecoratedBox(
                  decoration: BoxDecoration(
                    color: scheme.primaryContainer.withValues(alpha: 0.45),
                    borderRadius: BorderRadius.circular(MaClawColors.radiusLg),
                    border: Border.all(
                      color: scheme.primary.withValues(alpha: 0.12),
                    ),
                  ),
                  child: Padding(
                    padding: const EdgeInsets.all(18),
                    child: Image.asset(
                      maclawLogoAsset,
                      width: 96,
                      height: 96,
                      fit: BoxFit.contain,
                    ),
                  ),
                ),
                const SizedBox(height: 22),
                Text(
                  'MaClaw Mobile',
                  style: text.headlineSmall?.copyWith(
                    fontWeight: FontWeight.w700,
                    letterSpacing: -0.01,
                  ),
                ),
                const SizedBox(height: 8),
                Text(
                  '正在连接 MaClaw 官方服务',
                  style: text.bodyMedium?.copyWith(
                    color: scheme.onSurfaceVariant,
                    height: 1.4,
                  ),
                ),
                const SizedBox(height: 28),
                SizedBox(
                  width: 168,
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(999),
                    child: const LinearProgressIndicator(minHeight: 4),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
