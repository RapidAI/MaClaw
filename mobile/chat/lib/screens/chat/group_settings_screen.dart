import 'package:flutter/material.dart';
import '../../services/api_client.dart';

/// Group channel settings: rename, view/add/remove members.
class GroupSettingsScreen extends StatefulWidget {
  final String channelId;
  final String channelName;
  final ApiClient api;

  const GroupSettingsScreen({
    super.key,
    required this.channelId,
    required this.channelName,
    required this.api,
  });

  @override
  State<GroupSettingsScreen> createState() => _GroupSettingsScreenState();
}

class _GroupSettingsScreenState extends State<GroupSettingsScreen> {
  List<Map<String, dynamic>> _members = [];
  bool _loading = true;
  late TextEditingController _nameController;

  @override
  void initState() {
    super.initState();
    _nameController = TextEditingController(text: widget.channelName);
    _loadMembers();
  }

  @override
  void dispose() {
    _nameController.dispose();
    super.dispose();
  }

  Future<void> _loadMembers() async {
    try {
      _members = await widget.api.getChannelMembers(widget.channelId);
    } catch (_) {}
    if (mounted) setState(() => _loading = false);
  }

  Future<void> _saveName() async {
    final name = _nameController.text.trim();
    if (name.isEmpty || name == widget.channelName) return;
    try {
      await widget.api.updateChannelName(widget.channelId, name);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Group name updated')),
        );
      }
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Failed to update name')),
        );
      }
    }
  }

  Future<void> _removeMember(String userId) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Remove member?'),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          TextButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('Remove', style: TextStyle(color: Colors.red))),
        ],
      ),
    );
    if (confirm != true) return;
    try {
      await widget.api.removeChannelMember(widget.channelId, userId);
      _members.removeWhere((m) => m['user_id'] == userId);
      if (mounted) setState(() {});
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Group Settings')),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              children: [
                // Group name
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: Row(
                    children: [
                      Expanded(
                        child: TextField(
                          controller: _nameController,
                          decoration: const InputDecoration(
                            labelText: 'Group name',
                            border: OutlineInputBorder(),
                          ),
                        ),
                      ),
                      const SizedBox(width: 8),
                      IconButton(
                        icon: const Icon(Icons.check),
                        onPressed: _saveName,
                      ),
                    ],
                  ),
                ),
                const Divider(),
                // Members header
                Padding(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  child: Row(
                    children: [
                      Text('Members (${_members.length})',
                          style: Theme.of(context).textTheme.titleSmall),
                      const Spacer(),
                      TextButton.icon(
                        icon: const Icon(Icons.person_add, size: 18),
                        label: const Text('Add'),
                        onPressed: () {
                          // TODO: show contact picker to add members
                        },
                      ),
                    ],
                  ),
                ),
                // Member list
                ..._members.map((m) {
                  final name = m['display_name'] as String? ??
                      m['user_id'] as String? ??
                      'Unknown';
                  final role = m['role'] as String? ?? 'member';
                  return ListTile(
                    leading: CircleAvatar(
                      child: Text(name.isNotEmpty ? name[0].toUpperCase() : '?'),
                    ),
                    title: Text(name),
                    subtitle: role == 'admin' ? const Text('Admin') : null,
                    trailing: role != 'admin'
                        ? IconButton(
                            icon: const Icon(Icons.remove_circle_outline,
                                color: Colors.red),
                            onPressed: () =>
                                _removeMember(m['user_id'] as String),
                          )
                        : null,
                  );
                }),
              ],
            ),
    );
  }
}
