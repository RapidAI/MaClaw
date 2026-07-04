String redactMobileSensitiveText(String input) {
  var text = input;
  const secretValue = _mobileSecretValuePattern;
  text = text.replaceAllMapped(
    RegExp(
      r'-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----',
      multiLine: true,
    ),
    (_) => '[REDACTED_PRIVATE_KEY]',
  );
  text = text.replaceAllMapped(
    RegExp(
      r'\b(Authorization\s*:\s*)(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]+',
      caseSensitive: false,
    ),
    (match) => '${match.group(1)}${match.group(2)} [REDACTED_TOKEN]',
  );
  text = text.replaceAllMapped(
    RegExp(
      r'\b(password|passwd|pwd|token|api[_-]?key|secret)\s*[:=]\s*' +
          secretValue,
      caseSensitive: false,
    ),
    (match) => '${match.group(1)}=[REDACTED_SECRET]',
  );
  text = text.replaceAllMapped(
    RegExp(
      r'\b([A-Z0-9_]*(?:PASSWORD|PASSWD|PWD|TOKEN|SECRET|API_?KEY|ACCESS_KEY)[A-Z0-9_]*)\s*=\s*' +
          secretValue,
      caseSensitive: false,
    ),
    (match) => '${match.group(1)}=[REDACTED_SECRET]',
  );
  text = text.replaceAllMapped(
    RegExp(
      r'(\s--(?:password|passwd|token|api-key|api_key|secret)\s+)' +
          secretValue,
      caseSensitive: false,
    ),
    (match) => '${match.group(1)}[REDACTED_SECRET]',
  );
  text = text.replaceAllMapped(
    RegExp(
      r'\b([a-z][a-z0-9+.-]*://)([^/\s:@]+|):([^@\s/]+)@',
      caseSensitive: false,
    ),
    (match) => '${match.group(1)}[REDACTED_CREDENTIALS]@',
  );
  return text;
}

const _mobileSecretValuePattern = r"""(?:"[^"]*"|'[^']*'|[^\s,;]+)""";
