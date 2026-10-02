import 'package:flutter/material.dart';

/// Minimal application defaults; visual styling is defined incrementally.
abstract final class AppTheme {
  static ThemeData light() => _build(Brightness.light);
  static ThemeData dark() => _build(Brightness.dark);

  static ThemeData _build(Brightness brightness) => ThemeData(
    useMaterial3: true,
    brightness: brightness,
    fontFamily: 'HarmonyOS Sans',
    colorScheme: ColorScheme.fromSeed(
      seedColor: const Color(0xFF6FA7E8),
      brightness: brightness,
    ),
  );
}
