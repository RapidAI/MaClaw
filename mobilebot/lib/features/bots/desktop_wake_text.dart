/// Words for a failed 唤醒. An offline intranet computer keeps its own
/// sentence. A public Hub outage stays the shorter service line.
String desktopWakeFailureText(Object error) {
  final raw = error.toString().toLowerCase();
  if (raw.contains('node_offline') || raw.contains('这台内网电脑暂时连不上')) {
    return '这台内网电脑暂时连不上';
  }
  if (raw.contains('no free desktop display')) return '没有空闲的屏幕，请稍后再试';
  if (raw.contains('deadline') || raw.contains('timeout')) {
    return '等了一分钟电脑仍没有起来';
  }
  if (raw.contains('not open yet')) return '电脑还没有打开';
  if (raw.contains('connection refused') ||
      raw.contains('unreachable') ||
      raw.contains('502') ||
      raw.contains('503')) {
    return '电脑服务暂时连不上';
  }
  final text = error.toString();
  return text.isEmpty ? '操作失败，请重试' : text;
}
