String mobilePermissionGrantEvidence(String permission, {DateTime? now}) {
  final normalized =
      permission.trim().toLowerCase().replaceAll(RegExp(r'[^a-z0-9]+'), '-');
  final scope = normalized.isEmpty ? 'unknown' : normalized;
  final timestamp = (now ?? DateTime.now().toUtc()).microsecondsSinceEpoch;
  return 'permission-grant:$scope-$timestamp';
}
