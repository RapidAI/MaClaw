import 'package:flutter/material.dart';

const maclawLogoAsset = 'assets/images/maclaw_logo.png';

class StartupSplashScreen extends StatelessWidget {
  const StartupSplashScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.all(32),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Image.asset(
                  maclawLogoAsset,
                  width: 112,
                  height: 112,
                  fit: BoxFit.contain,
                ),
                const SizedBox(height: 18),
                Text(
                  'MaClaw Mobile',
                  style: Theme.of(context).textTheme.headlineSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                ),
                const SizedBox(height: 8),
                Text(
                  '正在连接 MaClaw 官方服务',
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                ),
                const SizedBox(height: 24),
                const SizedBox(
                  width: 160,
                  child: LinearProgressIndicator(),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
