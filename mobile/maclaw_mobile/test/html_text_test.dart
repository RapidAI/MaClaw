import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/text/html_text.dart';

void main() {
  test('unescapes named and numeric HTML entities from search snippets', () {
    final cleaned = cleanSearchSnippet(
      '1 天前&ensp;&#0183;&ensp;北京天气预报<br>及时发布',
    );
    expect(cleaned, isNot(contains('&ensp;')));
    expect(cleaned, isNot(contains('&#')));
    expect(cleaned, isNot(contains('<br>')));
    expect(cleaned, contains('北京天气预报'));
    expect(cleaned, contains('及时发布'));
    expect(cleaned, contains('·'));
  });

  test('truncates long snippets for compact UI rows', () {
    final long = '字' * 300;
    final cleaned = cleanSearchSnippet(long, maxLength: 40);
    expect(cleaned.length, lessThanOrEqualTo(41));
    expect(cleaned.endsWith('…'), isTrue);
  });

  test('citationHostLabel extracts bare host', () {
    expect(
      citationHostLabel('https://www.example.com/path?q=1'),
      'example.com',
    );
    expect(citationHostLabel(''), '');
  });
}
