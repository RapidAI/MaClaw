import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../providers/theme_provider.dart';
import '../../services/api_client.dart';
import 'voiceprint_screen.dart';

/// User profile / settings screen.
class ProfileScreen extends StatelessWidget {
  final ApiClient api;
  const ProfileScreen({super.key, required this.api});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Me')),
      body: ListView(
        children: [
          const SizedBox(height: 24),
          // Avatar + name
          const Center(
            child: CircleAvatar(
              radius: 40,
              child: Icon(Icons.person, size: 40),
            ),
          ),
          const SizedBox(height: 12),
          const Center(
            child: Text('User Name', style: TextStyle(fontSize: 18)),
          ),
          const SizedBox(height: 32),
          ListTile(
            leading: const Icon(Icons.record_voice_over_outlined),
            title: const Text('声纹管理'),
            subtitle: const Text('注册/管理你的声纹'),
            onTap: () => Navigator.push(
              context,
              MaterialPageRoute(builder: (_) => VoiceprintScreen(api: api)),
            ),
          ),
          ListTile(
            leading: const Icon(Icons.dark_mode_outlined),
            title: const Text('Theme'),
            trailing: Consumer<ThemeProvider>(
              builder: (context, theme, _) => DropdownButton<ThemeMode>(
                value: theme.mode,
                underline: const SizedBox(),
                items: const [
                  DropdownMenuItem(value: ThemeMode.system, child: Text('System')),
                  DropdownMenuItem(value: ThemeMode.light, child: Text('Light')),
                  DropdownMenuItem(value: ThemeMode.dark, child: Text('Dark')),
                ],
                onChanged: (mode) {
                  if (mode != null) theme.setMode(mode);
                },
              ),
            ),
          ),
          ListTile(
            leading: const Icon(Icons.notifications_outlined),
            title: const Text('Notifications'),
            onTap: () {},
          ),
          ListTile(
            leading: const Icon(Icons.devices_outlined),
            title: const Text('My Machines'),
            onTap: () {},
          ),
          ListTile(
            leading: const Icon(Icons.info_outline),
            title: const Text('About'),
            onTap: () {},
          ),
          ListTile(
            leading: const Icon(Icons.logout, color: Colors.red),
            title: const Text('Sign Out', style: TextStyle(color: Colors.red)),
            onTap: () {},
          ),
        ],
      ),
    );
  }
}
