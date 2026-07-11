import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_bootstrap.dart';
import '../auth/session_controller.dart';

/// Hub unified long-running jobs for the「后台」tab (design §5).
final mobileJobsProvider =
    FutureProvider.autoDispose<MobileJobsList?>((ref) async {
  final client = ref.watch(apiClientProvider);
  if (client == null) return null;
  try {
    return await client.listJobs();
  } on Object {
    return null;
  }
});

/// Live document quota (used/limit) from Hub — preferred over bootstrap snapshot.
final documentQuotaProvider =
    FutureProvider.autoDispose<MobileDocumentQuota?>((ref) async {
  final client = ref.watch(apiClientProvider);
  if (client == null) return null;
  try {
    return await client.getDocumentQuota();
  } on Object {
    return null;
  }
});

/// Live plan caps matrix from Hub (ops-visible, matches bootstrap entitlements).
final entitlementsCapsProvider =
    FutureProvider.autoDispose<MobileEntitlementsCaps?>((ref) async {
  final client = ref.watch(apiClientProvider);
  if (client == null) return null;
  try {
    return await client.getEntitlementsCaps();
  } on Object {
    return null;
  }
});

/// Merge live quota with bootstrap limits for UI (progress + caps).
MobileLimits mergeDocumentQuotaLimits(
  MobileLimits? bootstrapLimits,
  MobileDocumentQuota? live,
) {
  final base = bootstrapLimits ??
      const MobileLimits(maxUploadBytes: 0, maxExportJobs: 0);
  if (live == null) return base;
  final quotaBytes = live.documentQuotaBytes > 0
      ? live.documentQuotaBytes
      : base.documentQuotaBytes;
  return MobileLimits(
    maxUploadBytes: base.maxUploadBytes,
    maxExportJobs: base.maxExportJobs,
    documentQuotaBytes: quotaBytes,
    documentQuotaUsedBytes: live.documentQuotaUsedBytes,
  );
}
