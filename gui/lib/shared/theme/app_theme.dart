import 'package:flutter/material.dart';

abstract final class AppTheme {
  static ThemeData light() => _build(Brightness.light);
  static ThemeData dark() => _build(Brightness.dark);

  static ThemeData _build(Brightness brightness) {
    final dark = brightness == Brightness.dark;
    final scheme = ColorScheme.fromSeed(
      seedColor: const Color(0xFF4F8FD9),
      brightness: brightness,
    );
    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      fontFamily: 'HarmonyOS Sans',
      colorScheme: scheme,
      scaffoldBackgroundColor: dark
          ? const Color(0xFF171B20)
          : const Color(0xFFF8FAFC),
      dividerColor: dark ? const Color(0xFF303740) : const Color(0xFFE3E8EF),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: dark ? const Color(0xFF20262D) : Colors.white,
        border: OutlineInputBorder(borderRadius: BorderRadius.circular(9)),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(9),
          borderSide: BorderSide(color: scheme.outlineVariant),
        ),
      ),
      cardTheme: CardThemeData(
        elevation: 0,
        color: dark ? const Color(0xFF20262D) : Colors.white,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(11),
          side: BorderSide(color: scheme.outlineVariant),
        ),
      ),
    );
  }
}
