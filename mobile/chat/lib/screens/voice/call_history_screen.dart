import 'package:flutter/material.dart';
import '../../models/voice_call.dart';
import '../../services/local_database.dart';

/// Displays a list of past voice calls.
class CallHistoryScreen extends StatefulWidget {
  final LocalDatabase db;
  const CallHistoryScreen({super.key, required this.db});

  @override
  State<CallHistoryScreen> createState() => _CallHistoryScreenState();
}

class _CallHistoryScreenState extends State<CallHistoryScreen> {
  List<VoiceCall> _calls = [];
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      _calls = await widget.db.getCallHistory();
    } catch (_) {}
    if (mounted) setState(() => _loading = false);
  }

  String _formatDuration(Duration? d) {
    if (d == null) return '--';
    final m = d.inMinutes.remainder(60).toString().padLeft(2, '0');
    final s = d.inSeconds.remainder(60).toString().padLeft(2, '0');
    return '$m:$s';
  }

  IconData _statusIcon(CallStatus status) {
    switch (status) {
      case CallStatus.active:
      case CallStatus.ended:
        return Icons.call;
      case CallStatus.missed:
        return Icons.call_missed;
      case CallStatus.rejected:
        return Icons.call_end;
      case CallStatus.ringing:
        return Icons.ring_volume;
    }
  }

  Color _statusColor(CallStatus status) {
    switch (status) {
      case CallStatus.ended:
        return Colors.green;
      case CallStatus.missed:
        return Colors.red;
      case CallStatus.rejected:
        return Colors.orange;
      default:
        return Colors.grey;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Call History')),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _calls.isEmpty
              ? const Center(child: Text('No calls yet'))
              : ListView.builder(
                  itemCount: _calls.length,
                  itemBuilder: (context, index) {
                    final call = _calls[index];
                    return ListTile(
                      leading: Icon(
                        _statusIcon(call.status),
                        color: _statusColor(call.status),
                      ),
                      title: Text(
                        call.type == CallType.conference
                            ? 'Conference'
                            : 'Call ${call.callerId}',
                      ),
                      subtitle: Text(
                        '${call.status.name} · ${_formatDuration(call.duration)}',
                      ),
                      trailing: Text(
                        _formatDate(call.createdAt),
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    );
                  },
                ),
    );
  }

  String _formatDate(DateTime dt) {
    final now = DateTime.now();
    if (dt.year == now.year && dt.month == now.month && dt.day == now.day) {
      return '${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
    }
    return '${dt.month}/${dt.day} ${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
  }
}
