import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../providers/call_provider.dart';

/// Full-screen voice call UI (1v1 or conference).
class CallScreen extends StatelessWidget {
  const CallScreen({super.key});

  static Future<void> show(BuildContext context) {
    return Navigator.of(context).push(
      MaterialPageRoute(builder: (_) => const CallScreen()),
    );
  }

  String _formatDuration(Duration d) {
    final m = d.inMinutes.remainder(60).toString().padLeft(2, '0');
    final s = d.inSeconds.remainder(60).toString().padLeft(2, '0');
    if (d.inHours > 0) {
      return '${d.inHours}:$m:$s';
    }
    return '$m:$s';
  }

  String _statusText(CallState state) {
    switch (state) {
      case CallState.outgoing:
        return 'Calling...';
      case CallState.incoming:
        return 'Incoming call';
      case CallState.active:
        return 'Connected';
      case CallState.idle:
        return 'Ended';
    }
  }

  @override
  Widget build(BuildContext context) {
    return Consumer<CallProvider>(
      builder: (context, call, _) {
        // Auto-pop when call ends.
        if (call.state == CallState.idle) {
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (Navigator.of(context).canPop()) Navigator.of(context).pop();
          });
        }

        return Scaffold(
          backgroundColor: const Color(0xFF1A1A2E),
          body: SafeArea(
            child: Column(
              children: [
                const Spacer(flex: 2),
                // Avatar
                CircleAvatar(
                  radius: 48,
                  backgroundColor: Colors.white24,
                  child: Text(
                    (call.remoteName ?? '?').isNotEmpty
                        ? call.remoteName![0].toUpperCase()
                        : '?',
                    style: const TextStyle(fontSize: 36, color: Colors.white),
                  ),
                ),
                const SizedBox(height: 16),
                Text(
                  call.remoteName ?? 'Unknown',
                  style: const TextStyle(fontSize: 22, color: Colors.white),
                ),
                const SizedBox(height: 8),
                Text(
                  call.state == CallState.active
                      ? _formatDuration(call.elapsed)
                      : _statusText(call.state),
                  style: const TextStyle(fontSize: 14, color: Colors.white60),
                ),
                const Spacer(flex: 3),
                // Incoming call: accept / reject
                if (call.state == CallState.incoming)
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                    children: [
                      _CallButton(
                        icon: Icons.call_end,
                        label: 'Reject',
                        color: Colors.red,
                        onTap: () {
                          if (call.activeCallId != null) {
                            call.rejectCall(call.activeCallId!);
                          }
                        },
                      ),
                      _CallButton(
                        icon: Icons.call,
                        label: 'Accept',
                        color: Colors.green,
                        onTap: () {
                          // Accept is handled via provider with the SDP offer
                          // from the WS event — provider auto-transitions.
                        },
                      ),
                    ],
                  )
                else
                  // Active / outgoing: mute, speaker, hangup
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                    children: [
                      _CallButton(
                        icon: call.isMuted ? Icons.mic_off : Icons.mic,
                        label: call.isMuted ? 'Unmute' : 'Mute',
                        onTap: call.toggleMute,
                      ),
                      _CallButton(
                        icon: Icons.call_end,
                        label: 'Hang Up',
                        color: Colors.red,
                        onTap: call.hangup,
                      ),
                    ],
                  ),
                const SizedBox(height: 48),
              ],
            ),
          ),
        );
      },
    );
  }
}

class _CallButton extends StatelessWidget {
  final IconData icon;
  final String label;
  final Color color;
  final VoidCallback onTap;

  const _CallButton({
    required this.icon,
    required this.label,
    this.color = Colors.white24,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        GestureDetector(
          onTap: onTap,
          child: CircleAvatar(
            radius: 28,
            backgroundColor: color,
            child: Icon(icon, color: Colors.white, size: 28),
          ),
        ),
        const SizedBox(height: 8),
        Text(label,
            style: const TextStyle(color: Colors.white70, fontSize: 12)),
      ],
    );
  }
}
