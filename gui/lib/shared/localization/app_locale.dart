import 'package:flutter/widgets.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

/// The current product UI is Simplified Chinese, independently of the OS locale.
/// Keep native entry points and development previews on the same glyph policy.
abstract final class AppLocale {
  static const locale = Locale.fromSubtags(
    languageCode: 'zh',
    scriptCode: 'Hans',
    countryCode: 'CN',
  );
  static const supportedLocales = [locale];
  static const delegates = GlobalMaterialLocalizations.delegates;
}
