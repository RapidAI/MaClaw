import '../../core/locale.dart';

/// Words for a failed 唤醒. An offline intranet computer keeps its own
/// sentence. A public Hub outage stays the shorter service line.
String desktopWakeFailureText(Object error) {
  final s = sharedL10n;
  final raw = error.toString().toLowerCase();
  if (raw.contains('node_offline') || raw.contains('这台内网电脑暂时连不上')) {
    return s.wakeNodeOffline;
  }
  if (raw.contains('no free desktop display')) return s.wakeNoFreeDisplay;
  if (raw.contains('deadline') || raw.contains('timeout')) {
    return s.wakeTimeout;
  }
  if (raw.contains('not open yet')) return s.wakeNotOpenYet;
  if (raw.contains('connection refused') ||
      raw.contains('unreachable') ||
      raw.contains('502') ||
      raw.contains('503')) {
    return s.wakeServiceUnreachable;
  }
  final text = error.toString();
  return text.isEmpty ? s.opFailedRetry : text;
}
