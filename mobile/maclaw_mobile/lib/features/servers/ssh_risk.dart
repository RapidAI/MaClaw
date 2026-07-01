enum CommandRisk { normal, caution, dangerous }

CommandRisk classifyCommandRisk(String command) {
  final normalized = command.trim().toLowerCase();
  if (normalized.isEmpty) return CommandRisk.normal;

  const dangerousPatterns = [
    'rm -rf /',
    'mkfs',
    'dd if=',
    ':(){',
    'shutdown',
    'reboot',
    'systemctl restart',
    'docker system prune',
  ];
  if (dangerousPatterns.any(normalized.contains)) {
    return CommandRisk.dangerous;
  }

  const cautionPatterns = [
    'rm ',
    'mv ',
    'chmod ',
    'chown ',
    'kill ',
    'truncate ',
    'iptables ',
    'ufw ',
  ];
  if (cautionPatterns.any(normalized.contains)) {
    return CommandRisk.caution;
  }

  return CommandRisk.normal;
}

