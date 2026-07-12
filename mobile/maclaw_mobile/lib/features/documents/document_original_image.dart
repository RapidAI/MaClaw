import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../auth/session_controller.dart';
import 'document_draft.dart';
import 'documents_controller.dart';

/// How long to keep decoded original image bytes after the last listener leaves.
const documentOriginalImageCacheTtl = Duration(minutes: 3);

/// Loads original image bytes for a Hub draft (authenticated download).
/// Keyed by draft id so open preview reuses one fetch.
final documentOriginalImageBytesProvider =
    FutureProvider.autoDispose.family<Uint8List?, String>((ref, draftId) async {
  final id = draftId.trim();
  if (id.isEmpty) return null;

  // Watch only image-relevant fields of the active draft. OCR/markdown updates
  // must not cancel or re-download the original image.
  final activeKey = ref.watch(
    documentsControllerProvider.select((async) {
      final d = async.valueOrNull?.draft;
      if (d == null || d.id != id) return null;
      return (
        d.hasOriginal,
        d.sourceFilename,
        d.sourceContentType,
        d.sourceDownloadUrl,
        d.sourceSize,
        d.isImageOriginal,
      );
    }),
  );

  DocumentDraft? draft;
  if (activeKey != null) {
    if (!activeKey.$6) return null;
    draft = DocumentDraft(
      id: id,
      title: '',
      template: DocumentTemplate.report,
      markdown: '',
      updatedAt: DateTime.fromMillisecondsSinceEpoch(0),
      hasOriginal: activeKey.$1,
      sourceFilename: activeKey.$2,
      sourceContentType: activeKey.$3,
      sourceDownloadUrl: activeKey.$4,
      sourceSize: activeKey.$5,
    );
  } else {
    final history = ref.read(documentDraftHistoryProvider).valueOrNull;
    if (history != null) {
      for (final item in history) {
        if (item.id == id) {
          draft = item;
          break;
        }
      }
    }
  }

  if (draft == null || !draft.isImageOriginal) {
    return null;
  }

  final client = ref.watch(apiClientProvider);
  if (client == null) return null;

  // keepAlive for the duration of the request so a brief unmount does not cancel
  // the download mid-flight; close immediately on failure so retries work.
  final link = ref.keepAlive();
  Timer? disposeTimer;
  ref.onDispose(() => disposeTimer?.cancel());
  ref.onCancel(() {
    disposeTimer?.cancel();
    disposeTimer = Timer(documentOriginalImageCacheTtl, link.close);
  });
  ref.onResume(() => disposeTimer?.cancel());

  try {
    final bytes = await client.downloadDocumentOriginal(draft);
    if (bytes.isEmpty || !documentOriginalBytesLookLikeImage(bytes)) {
      disposeTimer?.cancel();
      link.close();
      return null;
    }
    // Success: onCancel timer (above) keeps bytes warm for [documentOriginalImageCacheTtl].
    return bytes;
  } on Object {
    disposeTimer?.cancel();
    link.close();
    return null;
  }
});

/// Exported for unit tests.
bool documentOriginalBytesLookLikeImage(Uint8List bytes) {
  if (bytes.length < 4) return false;
  // PNG
  if (bytes[0] == 0x89 &&
      bytes[1] == 0x50 &&
      bytes[2] == 0x4E &&
      bytes[3] == 0x47) {
    return true;
  }
  // JPEG
  if (bytes[0] == 0xFF && bytes[1] == 0xD8) return true;
  // GIF
  if (bytes[0] == 0x47 && bytes[1] == 0x49 && bytes[2] == 0x46) return true;
  // WEBP (RIFF....WEBP)
  if (bytes.length >= 12 &&
      bytes[0] == 0x52 &&
      bytes[1] == 0x49 &&
      bytes[2] == 0x46 &&
      bytes[3] == 0x46 &&
      bytes[8] == 0x57 &&
      bytes[9] == 0x45 &&
      bytes[10] == 0x42 &&
      bytes[11] == 0x50) {
    return true;
  }
  // BMP
  if (bytes[0] == 0x42 && bytes[1] == 0x4D) return true;
  return false;
}

/// Full-width preview thumbnail for the active document card.
class DocumentOriginalImagePreview extends ConsumerWidget {
  final DocumentDraft draft;
  final double maxHeight;

  const DocumentOriginalImagePreview({
    super.key,
    required this.draft,
    this.maxHeight = 220,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (!draft.isImageOriginal) return const SizedBox.shrink();

    final asyncBytes = ref.watch(documentOriginalImageBytesProvider(draft.id));
    final scheme = Theme.of(context).colorScheme;
    return asyncBytes.when(
      loading: () => Container(
        height: 120,
        width: double.infinity,
        alignment: Alignment.center,
        decoration: BoxDecoration(
          color: scheme.surfaceContainerHighest.withValues(alpha: 0.5),
          borderRadius: BorderRadius.circular(12),
        ),
        child: const SizedBox(
          width: 24,
          height: 24,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      ),
      error: (_, __) => _ImagePreviewRetry(
        scheme: scheme,
        onRetry: () => ref.invalidate(documentOriginalImageBytesProvider(draft.id)),
      ),
      data: (bytes) {
        if (bytes == null || bytes.isEmpty) {
          return _ImagePreviewRetry(
            scheme: scheme,
            onRetry: () =>
                ref.invalidate(documentOriginalImageBytesProvider(draft.id)),
          );
        }
        // Decode near display scale to cut GPU/memory for multi-megapixel sources.
        final dpr = MediaQuery.devicePixelRatioOf(context);
        final cacheW = (dpr * 480).round().clamp(240, 1280);
        return ClipRRect(
          borderRadius: BorderRadius.circular(12),
          child: ConstrainedBox(
            constraints: BoxConstraints(maxHeight: maxHeight),
            child: InteractiveViewer(
              minScale: 1,
              maxScale: 4,
              child: Image.memory(
                bytes,
                fit: BoxFit.contain,
                width: double.infinity,
                gaplessPlayback: true,
                cacheWidth: cacheW,
                errorBuilder: (_, __, ___) => _ImagePreviewRetry(
                  scheme: scheme,
                  onRetry: () => ref.invalidate(
                    documentOriginalImageBytesProvider(draft.id),
                  ),
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}

class _ImagePreviewRetry extends StatelessWidget {
  final ColorScheme scheme;
  final VoidCallback onRetry;

  const _ImagePreviewRetry({
    required this.scheme,
    required this.onRetry,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(vertical: 16, horizontal: 12),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest.withValues(alpha: 0.45),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        children: [
          Icon(Icons.broken_image_outlined, color: scheme.onSurfaceVariant),
          const SizedBox(height: 8),
          Text(
            '图片预览加载失败',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
          ),
          const SizedBox(height: 8),
          TextButton.icon(
            onPressed: onRetry,
            icon: const Icon(Icons.refresh, size: 18),
            label: const Text('重试'),
          ),
        ],
      ),
    );
  }
}

/// Small leading marker for Hub library list rows.
///
/// Does **not** download originals by default (list may show 30+ drafts).
/// Full authenticated preview is only in [DocumentOriginalImagePreview].
class DocumentOriginalImageThumb extends ConsumerWidget {
  final DocumentDraft draft;
  final double size;

  /// When true and the original is a small image, load a real thumb.
  final bool allowDownload;

  /// Skip network thumb when original is larger than this (default 256 KiB).
  final int maxDownloadBytes;

  const DocumentOriginalImageThumb({
    super.key,
    required this.draft,
    this.size = 44,
    this.allowDownload = false,
    this.maxDownloadBytes = 256 * 1024,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    final fallback = Icon(
      draft.isImageOriginal ? Icons.image_outlined : Icons.description_outlined,
      color: scheme.primary,
    );
    final canDownload = allowDownload &&
        draft.isImageOriginal &&
        (draft.sourceSize <= 0 || draft.sourceSize <= maxDownloadBytes);
    if (!canDownload) {
      return SizedBox(
        width: size,
        height: size,
        child: DecoratedBox(
          decoration: BoxDecoration(
            color: scheme.surfaceContainerHighest.withValues(alpha: 0.45),
            borderRadius: BorderRadius.circular(8),
          ),
          child: Center(child: fallback),
        ),
      );
    }
    final asyncBytes = ref.watch(documentOriginalImageBytesProvider(draft.id));
    return SizedBox(
      width: size,
      height: size,
      child: ClipRRect(
        borderRadius: BorderRadius.circular(8),
        child: asyncBytes.when(
          loading: () => ColoredBox(
            color: scheme.surfaceContainerHighest.withValues(alpha: 0.6),
            child: const Center(
              child: SizedBox(
                width: 16,
                height: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
            ),
          ),
          error: (_, __) => Center(child: fallback),
          data: (bytes) {
            if (bytes == null || bytes.isEmpty) {
              return Center(child: fallback);
            }
            return Image.memory(
              bytes,
              fit: BoxFit.cover,
              width: size,
              height: size,
              gaplessPlayback: true,
              cacheWidth: (size * MediaQuery.devicePixelRatioOf(context))
                  .round()
                  .clamp(48, 256),
              errorBuilder: (_, __, ___) => Center(child: fallback),
            );
          },
        ),
      ),
    );
  }
}
