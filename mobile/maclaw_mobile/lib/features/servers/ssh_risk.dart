enum CommandRisk { normal, caution, dangerous }

CommandRisk classifyCommandRisk(String command) {
  final normalized = _normalizeCommand(command);
  if (normalized.isEmpty) return CommandRisk.normal;

  if (_dangerousPatterns.any((pattern) => pattern.hasMatch(normalized))) {
    return CommandRisk.dangerous;
  }

  if (_cautionPatterns.any((pattern) => pattern.hasMatch(normalized))) {
    return CommandRisk.caution;
  }

  return CommandRisk.normal;
}

String _normalizeCommand(String command) {
  return command
      .trim()
      .toLowerCase()
      .replaceAll(RegExp(r'\s+'), ' ')
      .replaceAll(RegExp(r'\s*;\s*'), '; ')
      .replaceAll(RegExp(r'\s*&&\s*'), ' && ')
      .replaceAll(RegExp(r'\s*\|\|\s*'), ' || ');
}

final List<RegExp> _dangerousPatterns = [
  RegExp(
    r'(^|[;&|]\s*)(sudo\s+)?rm\s+(?=[^\n;&|]*-[^\n;&|]*r)(?=[^\n;&|]*-[^\n;&|]*f)[^\n;&|]*(\s+--)?\s+/($|\s|\*)',
  ),
  RegExp(
    r'(^|[;&|]\s*)(sudo\s+)?rm\s+(?=[^\n;&|]*-[^\n;&|]*r)(?=[^\n;&|]*-[^\n;&|]*f)[^\n;&|]*(\s+--)?\s+/(var|etc|usr|bin|sbin|lib|lib64|home|root|opt|srv|boot|dev|proc|sys|data)\b',
  ),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?mkfs(\.|\s|$)'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?dd\s+.*\bof=/dev/'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?shutdown\b'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?reboot\b'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?halt\b'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?poweroff\b'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?systemctl\s+(restart|stop)\b'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?docker\s+system\s+prune\b'),
  RegExp(
    r'(^|[;&|]\s*)(sudo\s+)?docker\s+(rm|rmi)\s+.*\b(-f|--force)\b',
  ),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?docker\s+(rm|rmi)\s+(-f|--force)\b'),
  RegExp(
    r'(^|[;&|]\s*)(sudo\s+)?iptables\s+(-f|-x|--flush|--delete-chain)\b',
  ),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?ufw\s+(disable|reset)\b'),
  RegExp(
    r'(^|[;&|]\s*)(sudo\s+)?firewall-cmd\s+.*(--panic-on|--remove-|--permanent)',
  ),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?chmod\s+-r\s+777\s+/'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?chown\s+-r\s+[^;]*\s+/'),
  RegExp(r'(^|[;&|]\s*)\s*: ?\(\)\s*\{'),
];

final List<RegExp> _cautionPatterns = [
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?rm\s+'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?mv\s+'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?chmod\s+'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?chown\s+'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?kill(all)?\s+'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?truncate\s+'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?iptables\s+'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?ufw\s+'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?firewall-cmd\s+'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?systemctl\s+(reload|daemon-reload)\b'),
  RegExp(r'(^|[;&|]\s*)(sudo\s+)?docker\s+(restart|stop|kill|rm|rmi)\b'),
];
