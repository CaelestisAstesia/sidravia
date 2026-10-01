import 'package:flutter/material.dart';

abstract final class SidraviaColors {
  static const connected = Color(0xFF2F9E75);
  static const progress = Color(0xFF6FA7E8);
  static const warning = Color(0xFFE0A84F);
  static const danger = Color(0xFFE5767C);
  static const neutral = Color(0xFF98A4B5);
}

abstract final class SidraviaTheme {
  static const _lightInk = Color(0xFF17202B);
  static const _lightSurface = Color(0xFFF4F6F9);
  static const _darkInk = Color(0xFFE8EDF4);
  static const _darkSurface = Color(0xFF10151C);
  static const _blue = SidraviaColors.progress;

  static ThemeData light() => _build(Brightness.light);

  static ThemeData dark() => _build(Brightness.dark);

  static ThemeData _build(Brightness brightness) {
    final dark = brightness == Brightness.dark;
    final ink = dark ? _darkInk : _lightInk;
    final surface = dark ? _darkSurface : _lightSurface;
    final outline = dark ? const Color(0xFF303A48) : const Color(0xFFD8DEE8);
    final variant = dark ? const Color(0xFFAAB5C5) : const Color(0xFF5D6878);
    final scheme =
        ColorScheme.fromSeed(
          seedColor: _blue,
          brightness: brightness,
          surface: surface,
        ).copyWith(
          primary: _blue,
          secondary: dark ? const Color(0xFF9FB4CC) : const Color(0xFF52677B),
          tertiary: SidraviaColors.connected,
          error: SidraviaColors.danger,
          onSurface: ink,
          onSurfaceVariant: variant,
          outline: outline,
          outlineVariant: dark
              ? const Color(0xFF25303D)
              : const Color(0xFFE3E7ED),
        );
    final base = ThemeData(
      useMaterial3: true,
      brightness: brightness,
      colorScheme: scheme,
      fontFamily: 'HarmonyOS Sans',
    );
    final shape = RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(16),
    );
    return base.copyWith(
      scaffoldBackgroundColor: surface,
      textTheme: base.textTheme.apply(
        fontFamily: 'HarmonyOS Sans',
        bodyColor: ink,
        displayColor: ink,
      ),
      appBarTheme: AppBarTheme(
        backgroundColor: surface,
        foregroundColor: ink,
        elevation: 0,
        surfaceTintColor: Colors.transparent,
      ),
      cardTheme: CardThemeData(
        color: dark ? const Color(0xFF1A222D) : Colors.white,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: shape,
      ),
      dividerTheme: DividerThemeData(color: outline),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: dark ? const Color(0xFF18212C) : Colors.white,
        indicatorColor: _blue.withValues(alpha: dark ? 0.22 : 0.14),
        labelTextStyle: WidgetStatePropertyAll(
          base.textTheme.labelMedium?.copyWith(fontWeight: FontWeight.w600),
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: dark ? const Color(0xFF18212C) : Colors.white,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 16,
          vertical: 15,
        ),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide(color: outline),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide(color: outline),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide(color: scheme.primary, width: 1.5),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          minimumSize: const Size(112, 46),
          padding: const EdgeInsets.symmetric(horizontal: 22, vertical: 13),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(14),
          ),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          minimumSize: const Size(96, 46),
          padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 13),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(14),
          ),
        ),
      ),
    );
  }
}
