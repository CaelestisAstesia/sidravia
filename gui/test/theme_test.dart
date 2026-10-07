import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  test('theme uses the bundled HarmonyOS Sans family', () {
    expect(AppTheme.light().textTheme.bodyLarge?.fontFamily, 'HarmonyOS Sans');
    expect(AppTheme.dark().brightness, Brightness.dark);
  });
}
