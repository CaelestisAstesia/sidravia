import 'package:flutter/material.dart';

abstract final class AppColors {
  static const bgLight = Color(0xFFF7F9FB);
  static const surfaceLight = Color(0xFFFFFFFF);
  static const surface2Light = Color(0xFFF1F4F7);
  static const inkLight = Color(0xFF18212B);
  static const mutedLight = Color(0xFF657382);
  static const lineLight = Color(0xFFDCE2E8);
  static const lineSoftLight = Color(0xFFE8EDF1);
  static const accent = Color(0xFF315F95);
  static const accentSoftLight = Color(0xFFEAF2FB);
  static const success = Color(0xFF22734F);
  static const warning = Color(0xFF9A6716);
  static const danger = Color(0xFFA54040);
  static const bgDark = Color(0xFF171B20);
  static const surfaceDark = Color(0xFF20262D);
  static const surface2Dark = Color(0xFF272E36);
  static const inkDark = Color(0xFFF2F5F7);
  static const mutedDark = Color(0xFFAAB4BF);
  static const lineDark = Color(0xFF39424C);
  static const lineSoftDark = Color(0xFF303842);
  static const accentDark = Color(0xFF9EC5F4);
  static const accentSoftDark = Color(0xFF273A4E);
  static const successDark = Color(0xFF77D4A7);
  static const warningDark = Color(0xFFE2B45F);
  static const dangerDark = Color(0xFFE78C8C);
}

abstract final class AppButtonStyles {
  static final primary = FilledButton.styleFrom(
    minimumSize: const Size(0, 42),
    padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(9)),
    textStyle: const TextStyle(
      fontSize: 13,
      fontWeight: FontWeight.w600,
      height: 20 / 13,
    ),
    backgroundColor: AppColors.accent,
    foregroundColor: Colors.white,
  );
  static final secondary = OutlinedButton.styleFrom(
    minimumSize: const Size(0, 42),
    padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(9)),
    side: const BorderSide(color: AppColors.lineLight),
    backgroundColor: AppColors.surfaceLight,
    foregroundColor: AppColors.inkLight,
    textStyle: const TextStyle(
      fontSize: 13,
      fontWeight: FontWeight.w600,
      height: 20 / 13,
    ),
  );
}

abstract final class AppTheme {
  static ThemeData light() => _build(Brightness.light);
  static ThemeData dark() => _build(Brightness.dark);

  static ThemeData _build(Brightness brightness) {
    final dark = brightness == Brightness.dark;
    final scheme = ColorScheme(
      brightness: brightness,
      primary: dark ? AppColors.accentDark : AppColors.accent,
      onPrimary: dark ? const Color(0xFF11263D) : Colors.white,
      primaryContainer: dark
          ? AppColors.accentSoftDark
          : AppColors.accentSoftLight,
      onPrimaryContainer: dark ? AppColors.inkDark : AppColors.inkLight,
      secondary: dark ? AppColors.mutedDark : AppColors.mutedLight,
      onSecondary: dark ? AppColors.inkLight : Colors.white,
      secondaryContainer: dark
          ? AppColors.surface2Dark
          : AppColors.surface2Light,
      onSecondaryContainer: dark ? AppColors.inkDark : AppColors.inkLight,
      tertiary: dark ? AppColors.warningDark : AppColors.warning,
      onTertiary: dark ? const Color(0xFF2B210D) : Colors.white,
      tertiaryContainer: dark
          ? const Color(0xFF3B3019)
          : const Color(0xFFF9F0DD),
      onTertiaryContainer: dark ? AppColors.inkDark : AppColors.inkLight,
      error: dark ? AppColors.dangerDark : AppColors.danger,
      onError: Colors.white,
      errorContainer: dark ? const Color(0xFF452527) : const Color(0xFFFBEAEA),
      onErrorContainer: dark ? AppColors.inkDark : AppColors.inkLight,
      surface: dark ? AppColors.surfaceDark : AppColors.surfaceLight,
      onSurface: dark ? AppColors.inkDark : AppColors.inkLight,
      surfaceContainerHighest: dark
          ? AppColors.surface2Dark
          : AppColors.surface2Light,
      onSurfaceVariant: dark ? AppColors.mutedDark : AppColors.mutedLight,
      outline: dark ? AppColors.lineDark : AppColors.lineLight,
      outlineVariant: dark ? AppColors.lineSoftDark : AppColors.lineSoftLight,
      shadow: const Color(0x1F172432),
      scrim: Colors.black,
      inverseSurface: dark ? AppColors.surfaceLight : AppColors.surfaceDark,
      onInverseSurface: dark ? AppColors.inkLight : AppColors.inkDark,
      inversePrimary: dark ? AppColors.accent : AppColors.accentDark,
    );
    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      fontFamily: 'HarmonyOS Sans',
      colorScheme: scheme,
      scaffoldBackgroundColor: dark ? AppColors.bgDark : AppColors.bgLight,
      dividerColor: dark ? AppColors.lineSoftDark : AppColors.lineSoftLight,
      textTheme: const TextTheme(
        bodyLarge: TextStyle(fontSize: 16, height: 1.2),
        bodyMedium: TextStyle(fontSize: 13, height: 1.55),
        bodySmall: TextStyle(fontSize: 12, height: 1.45),
      ).apply(bodyColor: scheme.onSurface, displayColor: scheme.onSurface),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: dark ? AppColors.surfaceDark : AppColors.surfaceLight,
        border: OutlineInputBorder(borderRadius: BorderRadius.circular(8)),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: BorderSide(color: scheme.outline),
        ),
      ),
      cardTheme: CardThemeData(
        elevation: 0,
        color: scheme.surface,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(11),
          side: BorderSide(color: scheme.outline),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(style: AppButtonStyles.primary),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: AppButtonStyles.secondary,
      ),
    );
  }
}
