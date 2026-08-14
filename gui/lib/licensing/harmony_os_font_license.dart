import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

void registerHarmonyOSFontLicense() {
  LicenseRegistry.addLicense(() async* {
    final asset = await rootBundle.loadString('assets/fonts/LICENSE.txt');
    yield LicenseEntryWithLineBreaks(const [
      'HarmonyOS Sans',
    ], decodeHarmonyOSFontLicense(asset));
  });
}

String decodeHarmonyOSFontLicense(String asset) {
  return asset.replaceFirst(RegExp(r'\u0000+$'), '');
}
