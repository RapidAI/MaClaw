import 'dart:io';
import 'package:flutter_image_compress/flutter_image_compress.dart';
import 'package:path_provider/path_provider.dart';

/// Compresses images before upload to reduce bandwidth.
class ImageCompressor {
  /// Max dimension (width or height) for compressed output.
  static const _maxDimension = 1920;

  /// JPEG quality (0-100).
  static const _quality = 80;

  /// Compress the image at [sourcePath] and return the compressed file path.
  /// Returns the original path if compression fails.
  static Future<String> compress(String sourcePath) async {
    try {
      final dir = await getTemporaryDirectory();
      final targetPath =
          '${dir.path}/compressed_${DateTime.now().millisecondsSinceEpoch}.jpg';

      final result = await FlutterImageCompress.compressAndGetFile(
        sourcePath,
        targetPath,
        quality: _quality,
        minWidth: _maxDimension,
        minHeight: _maxDimension,
        keepExif: false,
      );

      if (result != null && await File(result.path).exists()) {
        return result.path;
      }
      return sourcePath;
    } catch (_) {
      return sourcePath;
    }
  }
}
