import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/platform/paths/product_namespace.dart';

void main() {
  test('empty namespaces select production and valid names preserve case', () {
    expect(validateProductNamespace(null), '');
    for (final name in [
      '',
      'ns02-native-a',
      'A.B_c-9',
      'isolated-1',
      'mock-test',
      'a.b_c-9',
      'a' * 64,
    ]) {
      expect(validateProductNamespace(name), name, reason: name);
    }
  });

  test('rejects reserved names, path separators and invalid characters', () {
    for (final name in [
      '   ',
      ' spaced ',
      ' production',
      'production ',
      '../invalid',
      'a/b',
      r'a\b',
      'a:b',
      'c:drive',
      '..',
      '.leading',
      '.hidden',
      'trailing.',
      'hidden.',
      'a..b',
      'production',
      'default',
      'DEFAULT',
      'prod',
      'PROD',
      'control\nx',
      'é',
      '中文',
      'a' * 65,
    ]) {
      expect(validateProductNamespace(name), isNull, reason: name);
    }
  });
}
