import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';

void main() {
  test('theme exposes HarmonyOS Sans and light/dark variants', () {
    final light = SidraviaTheme.light();
    final dark = SidraviaTheme.dark();

    expect(light.textTheme.bodyLarge?.fontFamily, 'HarmonyOS Sans');
    expect(dark.textTheme.bodyLarge?.fontFamily, 'HarmonyOS Sans');
    expect(light.colorScheme.brightness, Brightness.light);
    expect(dark.colorScheme.brightness, Brightness.dark);
    expect(light.scaffoldBackgroundColor, const Color(0xFFF4F6F9));
    expect(light.colorScheme.tertiary, SidraviaColors.connected);
  });
}
