import 'dart:convert';

enum GuiAppearanceMode { system, light, dark }

enum GuiCloseBehavior { hideToTray, exit }

enum GuiAnnouncementFrequency {
  everyConnection,
  dailyFirstConnection,
  manualOnly,
}

enum GuiHiddenAnnouncementBehavior { showImmediately, notify, onRestore }

class GuiSettings {
  const GuiSettings({
    this.appearanceMode = GuiAppearanceMode.system,
    this.diagnosticsExpanded = false,
    this.closeBehavior = GuiCloseBehavior.hideToTray,
    this.startHidden = false,
    this.announcementFrequency = GuiAnnouncementFrequency.dailyFirstConnection,
    this.hiddenAnnouncementBehavior = GuiHiddenAnnouncementBehavior.notify,
    this.lastAutomaticAnnouncementDate,
  });

  final GuiAppearanceMode appearanceMode;
  final bool diagnosticsExpanded;
  final GuiCloseBehavior closeBehavior;
  final bool startHidden;
  final GuiAnnouncementFrequency announcementFrequency;
  final GuiHiddenAnnouncementBehavior hiddenAnnouncementBehavior;
  final String? lastAutomaticAnnouncementDate;
}

abstract interface class GuiSettingsStore {
  bool get persistent;
  Future<GuiSettings?> read();
  Future<void> save(GuiSettings settings);
}

class MemoryGuiSettingsStore implements GuiSettingsStore {
  GuiSettings? settings;
  @override
  bool get persistent => false;
  @override
  Future<GuiSettings?> read() async => settings;
  @override
  Future<void> save(GuiSettings value) async {
    settings = value;
  }
}

GuiSettings decodeGuiSettings(String source) {
  final value = jsonDecode(source);
  if (value is! Map<String, dynamic> || value['schemaVersion'] is! int) {
    throw const FormatException('Unsupported GUI settings');
  }

  final version = value['schemaVersion'] as int;
  final appearanceMode = value['appearanceMode'];
  if (appearanceMode is! String) {
    throw const FormatException('Invalid appearance mode');
  }
  final mode = GuiAppearanceMode.values.firstWhere(
    (m) => m.name == appearanceMode,
    orElse: () => throw const FormatException('Invalid appearance mode'),
  );

  if (version == 1 && value.length == 2) {
    return GuiSettings(appearanceMode: mode);
  }
  if (version == 2 &&
      value.length == 3 &&
      value['diagnosticsExpanded'] is bool) {
    return GuiSettings(
      appearanceMode: mode,
      diagnosticsExpanded: value['diagnosticsExpanded'] as bool,
    );
  }
  if ((version == 3 && value.length == 5 ||
          version == 4 &&
              value.length == 8 &&
              value.containsKey('lastAutomaticAnnouncementDate')) &&
      value['diagnosticsExpanded'] is bool &&
      value['closeBehavior'] is String &&
      value['startHidden'] is bool) {
    return GuiSettings(
      appearanceMode: mode,
      diagnosticsExpanded: value['diagnosticsExpanded'] as bool,
      closeBehavior: GuiCloseBehavior.values.firstWhere(
        (behavior) => behavior.name == value['closeBehavior'],
        orElse: () => throw const FormatException('Invalid close behavior'),
      ),
      startHidden: value['startHidden'] as bool,
      announcementFrequency: version == 3
          ? GuiAnnouncementFrequency.dailyFirstConnection
          : GuiAnnouncementFrequency.values.firstWhere(
              (frequency) => frequency.name == value['announcementFrequency'],
              orElse: () =>
                  throw const FormatException('Invalid announcement frequency'),
            ),
      hiddenAnnouncementBehavior: version == 3
          ? GuiHiddenAnnouncementBehavior.notify
          : GuiHiddenAnnouncementBehavior.values.firstWhere(
              (behavior) =>
                  behavior.name == value['hiddenAnnouncementBehavior'],
              orElse: () => throw const FormatException(
                'Invalid hidden announcement behavior',
              ),
            ),
      lastAutomaticAnnouncementDate: version == 3
          ? null
          : _announcementDate(value['lastAutomaticAnnouncementDate']),
    );
  }
  throw const FormatException('Unsupported GUI settings');
}

String? _announcementDate(Object? date) {
  if (date == null) return null;
  if (date is! String ||
      date.length != 10 ||
      !RegExp(r'^[0-9]{4}-[0-9]{2}-[0-9]{2}$').hasMatch(date)) {
    throw const FormatException('Invalid announcement date');
  }
  final year = int.parse(date.substring(0, 4));
  final month = int.parse(date.substring(5, 7));
  final day = int.parse(date.substring(8, 10));
  if (year < 1 || month < 1 || month > 12 || day < 1) {
    throw const FormatException('Invalid announcement date');
  }
  final leap = year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
  final days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (day > days[month - 1]) {
    throw const FormatException('Invalid announcement date');
  }
  return date;
}

String encodeGuiSettings(GuiSettings settings) =>
    '${jsonEncode({'schemaVersion': 4, 'appearanceMode': settings.appearanceMode.name, 'diagnosticsExpanded': settings.diagnosticsExpanded, 'closeBehavior': settings.closeBehavior.name, 'startHidden': settings.startHidden, 'announcementFrequency': settings.announcementFrequency.name, 'hiddenAnnouncementBehavior': settings.hiddenAnnouncementBehavior.name, 'lastAutomaticAnnouncementDate': _announcementDate(settings.lastAutomaticAnnouncementDate)})}\n';
