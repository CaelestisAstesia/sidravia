import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';

void main() {
  test('light theme uses the bundled HarmonyOS Sans family', () {
    final theme = SidraviaTheme.light();

    expect(theme.textTheme.bodyLarge?.fontFamily, 'HarmonyOS Sans');
    expect(theme.colorScheme.brightness, Brightness.light);
  });
}
