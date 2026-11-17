import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/licensing/source_code_pro_font_license.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('retained code font exposes its bundled original license', () async {
    LicenseRegistry.reset();
    addTearDown(LicenseRegistry.reset);
    registerSourceCodeProFontLicense();
    final licenses = await LicenseRegistry.licenses.toList();
    final license = licenses.singleWhere(
      (entry) => entry.packages.contains('Source Code Pro'),
    );
    final text = license.paragraphs
        .map((paragraph) => paragraph.text)
        .join('\n');
    expect(text, contains('SIL OPEN FONT LICENSE Version 1.1'));
    expect(text, contains('Reserved Font Name'));
  });
}
