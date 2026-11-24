import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';

const homeResponsiveCaptureOutput = String.fromEnvironment(
  'HOME_RESPONSIVE_OUTPUT',
);

Future<void> loadCaptureFonts({bool forCapture = false}) async {
  const manifest = String.fromEnvironment('SYSTEM_FONT_MANIFEST');
  if (manifest.isNotEmpty) {
    final fonts = jsonDecode(await File(manifest).readAsString()) as Map;
    for (final entry in fonts.entries) {
      final loader = FontLoader(entry.key as String);
      for (final path in entry.value as List) {
        loader.addFont(
          File(path as String).readAsBytes().then(ByteData.sublistView),
        );
      }
      await loader.load();
    }
  } else if (forCapture || homeResponsiveCaptureOutput.isNotEmpty) {
    throw StateError('SYSTEM_FONT_MANIFEST is required for real-font capture');
  }
  await (FontLoader(
    'MaterialIcons',
  )..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'))).load();
  const path = String.fromEnvironment(
    'HOME_SYMBOL_FONT',
    defaultValue: '/mnt/c/Windows/Fonts/seguisym.ttf',
  );
  if (await File(path).exists()) {
    await (FontLoader(
      'Segoe UI Symbol',
    )..addFont(File(path).readAsBytes().then(ByteData.sublistView))).load();
  }
}
