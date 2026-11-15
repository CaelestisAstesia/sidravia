import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

void registerSourceCodeProFontLicense() {
  LicenseRegistry.addLicense(() async* {
    yield LicenseEntryWithLineBreaks(const [
      'Source Code Pro',
    ], await rootBundle.loadString('assets/fonts/SourceCodePro-LICENSE.md'));
  });
}
