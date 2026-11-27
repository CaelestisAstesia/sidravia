import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

void registerBundledFontLicenses() {
  LicenseRegistry.addLicense(() async* {
    yield LicenseEntryWithLineBreaks(const [
      'Source Code Pro',
    ], await rootBundle.loadString('assets/fonts/SourceCodePro-LICENSE.md'));
    yield LicenseEntryWithLineBreaks(const [
      'Material Icons',
    ], await rootBundle.loadString('assets/fonts/MaterialIcons-LICENSE.txt'));
  });
}
