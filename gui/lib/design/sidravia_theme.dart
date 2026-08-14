import 'package:flutter/material.dart';

abstract final class SidraviaTheme {
  static const _ink = Color(0xFF172033);
  static const _blue = Color(0xFF2769C6);
  static const _cyan = Color(0xFF1D9CB9);
  static const _lilac = Color(0xFF806BCB);
  static const _surface = Color(0xFFF9FAFC);

  static ThemeData light() {
    final colorScheme =
        ColorScheme.fromSeed(
          seedColor: _blue,
          brightness: Brightness.light,
          surface: _surface,
        ).copyWith(
          primary: _blue,
          secondary: _cyan,
          tertiary: _lilac,
          onSurface: _ink,
          outline: const Color(0xFFC6CBD6),
        );
    final base = ThemeData(
      useMaterial3: true,
      colorScheme: colorScheme,
      fontFamily: 'HarmonyOS Sans',
    );
    return base.copyWith(
      scaffoldBackgroundColor: _surface,
      textTheme: base.textTheme.apply(
        fontFamily: 'HarmonyOS Sans',
        bodyColor: _ink,
        displayColor: _ink,
      ),
      appBarTheme: const AppBarTheme(
        backgroundColor: _surface,
        foregroundColor: _ink,
        elevation: 0,
        surfaceTintColor: Colors.transparent,
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: Colors.white,
        indicatorColor: colorScheme.primaryContainer,
        labelTextStyle: WidgetStatePropertyAll(
          base.textTheme.labelMedium?.copyWith(fontWeight: FontWeight.w500),
        ),
      ),
      dividerTheme: const DividerThemeData(color: Color(0xFFE0E4EB)),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: Colors.white,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: const BorderSide(color: Color(0xFFC6CBD6)),
        ),
      ),
    );
  }
}
