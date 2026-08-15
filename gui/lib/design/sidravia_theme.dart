import 'package:flutter/material.dart';

abstract final class SidraviaColors {
  static const connected = Color(0xFF247A57);
  static const progress = Color(0xFF2F68B1);
  static const warning = Color(0xFF9A650E);
  static const danger = Color(0xFFB33D43);
  static const neutral = Color(0xFF667085);
}

abstract final class SidraviaTheme {
  static const _ink = Color(0xFF1D2939);
  static const _blue = SidraviaColors.progress;
  static const _surface = Color(0xFFF7F8FA);
  static const _outline = Color(0xFFD7DCE3);

  static ThemeData light() {
    final colorScheme =
        ColorScheme.fromSeed(
          seedColor: _blue,
          brightness: Brightness.light,
          surface: _surface,
        ).copyWith(
          primary: _blue,
          secondary: const Color(0xFF52677B),
          tertiary: SidraviaColors.connected,
          error: SidraviaColors.danger,
          onSurface: _ink,
          onSurfaceVariant: const Color(0xFF5E6878),
          outline: _outline,
          outlineVariant: const Color(0xFFE5E8ED),
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
        backgroundColor: _surface,
        indicatorColor: colorScheme.primary.withValues(alpha: 0.1),
        labelTextStyle: WidgetStatePropertyAll(
          base.textTheme.labelMedium?.copyWith(fontWeight: FontWeight.w500),
        ),
      ),
      dividerTheme: const DividerThemeData(color: _outline),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: Colors.white,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 16,
          vertical: 15,
        ),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: const BorderSide(color: _outline),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: const BorderSide(color: _outline),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          minimumSize: const Size(112, 44),
          padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(10),
          ),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          minimumSize: const Size(96, 44),
          padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(10),
          ),
        ),
      ),
    );
  }
}
