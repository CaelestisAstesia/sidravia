import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/licensing/harmony_os_font_license.dart';

void main() {
  test(
    'removes only trailing NUL padding from the bundled license display',
    () {
      expect(decodeHarmonyOSFontLicense('Agreement\u0000\u0000'), 'Agreement');
      expect(decodeHarmonyOSFontLicense('A\u0000B\u0000'), 'A\u0000B');
    },
  );
}
