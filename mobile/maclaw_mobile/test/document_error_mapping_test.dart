import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/documents/document_original_image.dart';
import 'package:maclaw_mobile/features/documents/documents_controller.dart';

void main() {
  group('mapDocumentStorageError', () {
    test('maps 404 / DRAFT_NOT_FOUND to Chinese message', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/x'),
        response: Response(
          requestOptions: RequestOptions(path: '/x'),
          statusCode: 404,
          data: {'code': 'DRAFT_NOT_FOUND', 'message': 'draft not found'},
        ),
        type: DioExceptionType.badResponse,
      );
      final mapped = mapDocumentStorageError(err);
      expect(mapped, isA<StateError>());
      expect(
        (mapped as StateError).message,
        contains('不存在'),
      );
    });

    test('maps quota 507', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/x'),
        response: Response(
          requestOptions: RequestOptions(path: '/x'),
          statusCode: 507,
          data: {'code': 'DOCUMENT_QUOTA_EXCEEDED'},
        ),
        type: DioExceptionType.badResponse,
      );
      final mapped = mapDocumentStorageError(err) as StateError;
      expect(mapped.message, contains('空间不足'));
    });
  });

  group('isDocumentDraftAlreadyGone', () {
    test('true for 404', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/x'),
        response: Response(
          requestOptions: RequestOptions(path: '/x'),
          statusCode: 404,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(isDocumentDraftAlreadyGone(err), isTrue);
    });

    test('false for 500', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/x'),
        response: Response(
          requestOptions: RequestOptions(path: '/x'),
          statusCode: 500,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(isDocumentDraftAlreadyGone(err), isFalse);
    });
  });

  group('isMaclawOutboundSharePath', () {
    test('detects prefix and outbound dir', () {
      expect(
        isMaclawOutboundSharePath(
          r'C:\tmp\maclaw_outbound_share\maclaw_share_a.docx',
        ),
        isTrue,
      );
      expect(isMaclawOutboundSharePath('/data/maclaw_share_report.pdf'), isTrue);
      expect(isMaclawOutboundSharePath('/data/real_import.docx'), isFalse);
    });
  });

  group('documentOriginalBytesLookLikeImage', () {
    test('detects png/jpeg magic', () {
      final png = Uint8List.fromList([0x89, 0x50, 0x4E, 0x47, 0, 0, 0, 0]);
      final jpeg = Uint8List.fromList([0xFF, 0xD8, 0xFF, 0xE0]);
      final text = Uint8List.fromList('hello'.codeUnits);
      expect(documentOriginalBytesLookLikeImage(png), isTrue);
      expect(documentOriginalBytesLookLikeImage(jpeg), isTrue);
      expect(documentOriginalBytesLookLikeImage(text), isFalse);
    });
  });
}
