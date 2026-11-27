import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/licensing/bundled_font_licenses.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('bundled fonts expose their attribution and licenses', () async {
    LicenseRegistry.reset();
    addTearDown(LicenseRegistry.reset);
    registerBundledFontLicenses();
    final licenses = await LicenseRegistry.licenses.toList();
    final license = licenses.singleWhere(
      (entry) => entry.packages.contains('Source Code Pro'),
    );
    final text = license.paragraphs
        .map((paragraph) => paragraph.text)
        .join('\n');
    expect(text, contains('SIL OPEN FONT LICENSE Version 1.1'));
    expect(text, contains('Reserved Font Name'));

    final material = licenses.singleWhere(
      (entry) => entry.packages.contains('Material Icons'),
    );
    final materialText = material.paragraphs.map((p) => p.text).join('\n');
    expect(materialText, contains('Material Icons by Google'));
    expect(materialText, contains('Attribution 4.0 International'));
  });
}
