import 'package:flutter/material.dart';
import 'chat/conversation_list_screen.dart';
import 'voice/contact_list_screen.dart';
import 'profile/profile_screen.dart';
import '../services/api_client.dart';

/// Root screen with bottom navigation: Chat / Voice / Profile.
class HomeScreen extends StatefulWidget {
  final ApiClient api;
  const HomeScreen({super.key, required this.api});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  int _currentIndex = 0;

  late final List<Widget> _screens = [
    const ConversationListScreen(),
    const ContactListScreen(),
    ProfileScreen(api: widget.api),
  ];

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: IndexedStack(index: _currentIndex, children: _screens),
      bottomNavigationBar: BottomNavigationBar(
        currentIndex: _currentIndex,
        onTap: (i) => setState(() => _currentIndex = i),
        items: const [
          BottomNavigationBarItem(icon: Icon(Icons.chat_bubble_outline), label: 'Chat'),
          BottomNavigationBarItem(icon: Icon(Icons.phone_outlined), label: 'Voice'),
          BottomNavigationBarItem(icon: Icon(Icons.person_outline), label: 'Me'),
        ],
      ),
    );
  }
}
